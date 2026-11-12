package policy

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
)

var (
	writeTools   = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}
	featureState = regexp.MustCompile(`^aisdlc/features/[^/]+/(gates\.json|steps\.json|scope-extra\.json|feedback\.md|risk\.yaml)$`)
)

const (
	riskPolicyFile = "aisdlc/risk-policy.yaml"
	configFile     = "aisdlc/config.yaml"
)

// webConfigMsg là lý do chặn đọc/ghi cấu hình web của dev.
func webConfigMsg() string {
	return i18n.T("File cấu hình web của dev, agent không được đọc/ghi")
}

// envMsg là lý do chặn lệnh đọc biến môi trường.
func envMsg() string {
	return i18n.T("Lệnh này đọc biến môi trường, nơi có thể có token web của dev (AISDLC_WEB_TOKEN);" +
		" agent không được đọc. Cần biến nào thì nêu đúng tên biến (vd. `printenv JAVA_HOME`).")
}

// Go không có lookahead: các "(?!...)" và "(?=...)" của bản Java được đổi thành nhóm bắt ký tự theo sau
// (hoặc hết chuỗi); chỗ cần biết vị trí kết thúc thật dùng chỉ số của nhóm đó.
var (
	// webConfigText: ".config/aisdlc" như một đoạn đường dẫn (kể cả ~/, $HOME/, đường dẫn tương đối).
	webConfigText = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])\.config/+aisdlc([^A-Za-z0-9_-]|$)`)
	dotConfig     = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])\.config([^A-Za-z0-9_-]|$)`)
	webConfigFile = regexp.MustCompile(`approve-token|web-pubkey|web\.json`)
	// recursive: lệnh đọc/tìm đệ quy (cờ gộp như -rn, -Ri cũng tính).
	recursive = regexp.MustCompile(`(^|[\s;|&(` + "`" + `])(` +
		`find|rg|ag|fd|locate|rsync|tar` +
		`|e?grep\b[^;|&]*\s(-[A-Za-z]*[rR][A-Za-z]*|--recursive|--dereference-recursive)` +
		`|cp\b[^;|&]*\s(-[A-Za-z]*[rRa][A-Za-z]*|--recursive|--archive)` +
		`|zip\b[^;|&]*\s-[A-Za-z]*r[A-Za-z]*` +
		`)(\s|$)`)
	bracedVar  = regexp.MustCompile(`\$\{(\w+)\}`)
	procEnv    = regexp.MustCompile(`/proc/[^\s;|&]*environ`)
	assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S*$`)
	globMeta   = regexp.MustCompile(`[*?\[{]`)
	procDir    = regexp.MustCompile(`^/proc/([^/]*)(?:/+([^/]*))?`)
	digits     = regexp.MustCompile(`^\d+$`)
	shellC     = regexp.MustCompile(`^-[A-Za-z]*c[A-Za-z]*$`)
	cmdSplit   = regexp.MustCompile("[\\s;|&()<>`]+")
	slashRuns  = regexp.MustCompile(`/+`)
	wsRuns     = regexp.MustCompile(`[ \t\n\x0B\f\r]+`)
	quoteEnds  = regexp.MustCompile(`^['"]+|['"]+$`)
	assignHead = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// cdDotConfig: cd/pushd vào chính <HOME>/.config (HOME chèn vào chỗ %s); nhóm 1 là ký tự/hết chuỗi theo sau.
const cdDotConfig = "(?:^|[\\s;|&(`])(?:cd|pushd)\\s+(?:--\\s+)?(?:%s)/+\\.config/*([\\s;|&)`<>]|$)"

// WebConfigDecision chặn mọi tool chạm thư mục cấu hình web của dev (token ingest, token approve, khóa ghim).
// paths: mọi giá trị kiểu đường dẫn của tool (file_path, notebook_path, path, pattern, glob); command: lệnh
// Bash; configDir rỗng nghĩa là không biết; cwd rỗng là thư mục hiện tại. Không bao giờ panic: lỗi của luật
// heuristic này cố ý fail open (tính sẵn sàng; luật cổng vẫn xét).
func WebConfigDecision(paths []string, command, configDir, cwd string) (d Decision) {
	defer func() {
		if recover() != nil {
			d = Ok()
		}
	}()
	for _, v := range paths {
		if touchesWebConfig(v, configDir, cwd) {
			return Deny(webConfigMsg())
		}
	}
	for _, v := range paths {
		if readsProcEnviron(v) {
			return Deny(envMsg())
		}
	}
	if command != "" && bashTouchesWebConfig(command, configDir) {
		return Deny(webConfigMsg())
	}
	if command != "" && readsEnvironment(command) {
		return Deny(envMsg())
	}
	return Ok()
}

// readsEnvironment (heuristic, không đầy đủ: shell có vô số cách in biến môi trường): chặn lệnh nêu
// AISDLC_WEB_TOKEN, đọc /proc/<pid>/environ, hoặc in toàn bộ môi trường bằng env/printenv không đối số (chỉ có
// cờ) và export/set/declare/typeset không đối số hoặc chỉ -p/-x. "env FOO=1 cmd", "printenv HOME" vẫn được.
func readsEnvironment(command string) bool {
	if strings.Contains(command, "AISDLC_WEB_TOKEN") || procEnv.MatchString(command) {
		return true
	}
	for _, segment := range splitSegments(command) {
		var words []string
		for _, w := range wsRuns.Split(strings.TrimFunc(segment, javaTrim), -1) {
			if w == "" || len(words) == 0 && assignment.MatchString(w) {
				continue // FOO=1 env
			}
			words = append(words, quoteEnds.ReplaceAllString(w, ""))
		}
		if len(words) == 0 {
			continue
		}
		cmd := words[0][strings.LastIndex(words[0], "/")+1:]
		args := words[1:]
		switch cmd {
		case "env":
			// env chỉ in môi trường khi sau cờ và gán A=b không còn từ nào (còn từ = lệnh được chạy)
			hasCmd := false
			for i := 0; i < len(args); i++ {
				a := args[i]
				if a == "--" {
					continue
				}
				if a == "-u" || a == "-C" || a == "-S" {
					i++
					continue
				}
				if strings.HasPrefix(a, "-") || assignment.MatchString(a) {
					continue
				}
				hasCmd = true
				break
			}
			if !hasCmd {
				return true
			}
		case "printenv":
			all := true
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					all = false
				}
			}
			if all {
				return true
			}
		case "export", "set", "declare", "typeset":
			all := true
			for _, a := range args {
				if a != "-p" && a != "-x" && a != "-px" && a != "-xp" {
					all = false
				}
			}
			if all {
				return true
			}
		case "bash", "sh", "zsh", "dash", "ksh":
			for i := 0; i < len(args); i++ {
				if shellC.MatchString(args[i]) && i+1 < len(args) {
					rest := strings.Join(args[i+1:], " ")
					if readsEnvironment(quoteEnds.ReplaceAllString(rest, "")) {
						return true
					}
					break
				}
			}
		}
	}
	return false
}

// splitSegments tách lệnh thành đoạn. "(" chỉ là ranh giới subshell khi đứng đầu hoặc sau khoảng trắng/toán
// tử; sau tên ("set(x)", "env(1)") là gọi hàm: từ đứng ngay trước không tính là lệnh.
func splitSegments(command string) []string {
	var out []string
	var cur []rune
	rs := []rune(command)
	for i, ch := range rs {
		switch {
		case ch == '(':
			prev := ' '
			if i > 0 {
				prev = rs[i-1]
			}
			if prev == '$' { // $( ... )
				out = append(out, string(cur))
				cur = cur[:0]
				continue
			}
			if !javaIsWhitespace(prev) && !strings.ContainsRune(";|&(`)", prev) { // tên( : gọi hàm
				k := len(cur)
				for k > 0 && !javaIsWhitespace(cur[k-1]) {
					k--
				}
				cur = cur[:k]
			}
			out = append(out, string(cur))
			cur = cur[:0]
		case strings.ContainsRune(";|&)`\n", ch):
			out = append(out, string(cur))
			cur = cur[:0]
		default:
			cur = append(cur, ch)
		}
	}
	return append(out, string(cur))
}

// readsProcEnviron: giá trị đường dẫn của tool đọc có thể trỏ tới /proc/<pid>/environ.
func readsProcEnviron(value string) bool {
	v := strings.TrimFunc(strings.ReplaceAll(value, `\`, "/"), javaTrim)
	if procEnv.MatchString(v) {
		return true
	}
	m := procDir.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	s1 := m[1]
	pidLike := s1 == "self" || s1 == "thread-self" || digits.MatchString(s1) || globMeta.MatchString(s1)
	if !pidLike {
		return false
	}
	if m[2] == "" {
		return true // /proc/self, /proc/1234, /proc/*
	}
	return globMatches(m[2], "environ")
}

func touchesWebConfig(value, configDir, cwd string) bool {
	return touchesWebConfig1(strings.ReplaceAll(value, `\`, "/"), configDir, cwd) ||
		touchesWebConfig1(strings.ReplaceAll(value, `\`, ""), configDir, cwd)
}

func touchesWebConfig1(v, configDir, cwd string) bool {
	if webConfigText.MatchString(v) {
		return true
	}
	if configDir == "" {
		return false
	}
	loc := globMeta.FindStringIndex(v)
	glob := loc != nil
	literal := v
	if glob {
		literal = v[:loc[0]]
	}
	p, ok := resolveWebPath(literal, configDir, cwd)
	if !ok {
		return false
	}
	dir := absClean(configDir)
	realDir := realize(dir)
	realP := realize(p)
	// trong thư mục cấu hình, hoặc là thư mục cha của nó (Grep/Glob tìm đệ quy, mẫu glob mở rộng vào trong)
	if pathStartsWith(p, dir) || pathStartsWith(realP, realDir) {
		return true
	}
	home := mustParent(mustParent(dir))
	if glob {
		// phần chữ trước ký tự đại diện là tiền tố (chuỗi) của thư mục cấu hình: ~/.config/ais*/..., ~/.c*/...
		prefix := strings.HasPrefix(dir, p) || strings.HasPrefix(realDir, realP)
		if !prefix {
			return false
		}
		rest := v[len(literal):]
		lastSegment := literal[strings.LastIndex(literal, "/")+1:]
		return len(p) > len(home) || strings.HasPrefix(lastSegment, ".") ||
			strings.Contains(rest, ".config") || strings.Contains(rest, "aisdlc")
	}
	ancestor := pathStartsWith(dir, p) || pathStartsWith(realDir, realP)
	if !ancestor {
		return false
	}
	// Grep/Glob với path là HOME hoặc ~/.config (tìm đệ quy vào thư mục cấu hình)
	return pathStartsWith(p, home) && isDir(p)
}

// resolveWebPath trả đường dẫn tuyệt đối đã chuẩn hóa ("~/" và "$HOME/" theo HOME suy ra từ thư mục cấu hình).
func resolveWebPath(literal, configDir, cwd string) (string, bool) {
	home, ok := parentOf(absClean(configDir))
	if !ok {
		return "", false
	}
	if home, ok = parentOf(home); !ok {
		return "", false
	}
	if strings.ContainsRune(literal, 0) {
		return "", false
	}
	s := literal
	switch {
	case s == "~" || strings.HasPrefix(s, "~/"):
		s = home + s[1:]
	case strings.HasPrefix(s, "$HOME/") || s == "$HOME":
		s = home + s[5:]
	case strings.HasPrefix(s, "${HOME}"):
		s = home + s[7:]
	}
	if !filepath.IsAbs(s) {
		base := cwd
		if base == "" {
			wd, err := os.Getwd()
			if err != nil {
				return "", false
			}
			base = wd
		}
		s = filepath.Join(absClean(base), s)
	}
	return filepath.Clean(s), true
}

func bashTouchesWebConfig(command, configDir string) bool {
	return bashTouchesWebConfig1(strings.ReplaceAll(command, `\`, "/"), configDir) ||
		bashTouchesWebConfig1(strings.ReplaceAll(command, `\`, ""), configDir)
}

// bashTouchesWebConfig1: heuristic cho lệnh Bash, gắn với cây HOME/.config (không phải cả HOME: repo AISDLC nằm
// dưới HOME và có thư mục aisdlc/). Chặn khi: nêu .config/aisdlc; một từ đi vào HOME/.config bằng ký tự đại diện
// có thể khớp aisdlc (hoặc vào thư mục ẩn khớp .config rồi khớp aisdlc); tên file cấu hình web kèm tham chiếu
// HOME/.config hoặc trong lệnh tìm kiếm; lệnh đọc/tìm đệ quy với gốc là chính HOME hoặc HOME/.config.
func bashTouchesWebConfig1(command, configDir string) bool {
	c := bracedVar.ReplaceAllString(command, "$$${1}")
	if webConfigText.MatchString(c) {
		return true
	}
	// cd ~/.config && cat aisdlc/web.json
	if dotConfig.MatchString(c) && strings.Contains(c, "aisdlc") {
		return true
	}
	homes := []string{"~", "$HOME"}
	if configDir != "" {
		dir := absClean(configDir)
		abs := filepath.ToSlash(dir)
		real := filepath.ToSlash(realize(dir))
		if strings.Contains(c, abs) || strings.Contains(c, real) {
			return true
		}
		home := mustParent(mustParent(dir))
		homes = append(homes, filepath.ToSlash(home), filepath.ToSlash(realize(home)))
	}
	// cd ~/.config && cat */*: sau khi vào HOME/.config, ký tự đại diện hoặc lệnh aisdlc có thể chạm aisdlc/
	unquoted := strings.NewReplacer(`"`, "", "'", "").Replace(c)
	quoted := make([]string, len(homes))
	for i, h := range homes {
		quoted[i] = regexp.QuoteMeta(h)
	}
	cd := regexp.MustCompile(strings.Replace(cdDotConfig, "%s", strings.Join(quoted, "|"), 1))
	for pos := 0; pos <= len(unquoted); {
		m := cd.FindStringSubmatchIndex(unquoted[pos:])
		if m == nil {
			break
		}
		end := pos + m[2] // vị trí bắt đầu ký tự theo sau (lookahead của bản Java)
		rest := unquoted[end:]
		if globMeta.MatchString(rest) || strings.Contains(rest, "aisdlc") {
			return true
		}
		if end <= pos {
			break
		}
		pos = end
	}
	homeRef := strings.Contains(c, ".config")
	rootAncestor := false // một từ chính là HOME hoặc HOME/.config
	for _, tok := range cmdSplit.Split(c, -1) {
		t := assignHead.ReplaceAllString(quoteEnds.ReplaceAllString(tok, ""), "")
		for _, h := range homes {
			if t != h && !strings.HasPrefix(t, h+"/") {
				continue
			}
			homeRef = true
			seg := slashRuns.Split(strings.TrimLeft(t[len(h):], "/"), -1)
			s1, s2 := "", ""
			if len(seg) > 0 {
				s1 = seg[0]
			}
			if len(seg) > 1 {
				s2 = seg[1]
			}
			if s1 == "" || s1 == ".config" && s2 == "" {
				rootAncestor = true // ~, $HOME, /home/u, ~/.config
			} else if globMatches(s1, ".config") {
				if s2 == "" {
					rootAncestor = true // ~/.c* chỉ tới chính ~/.config
				} else if globMeta.MatchString(s1+"/"+s2) && globMatches(s2, "aisdlc") {
					return true // ~/.config/ais*/..., ~/.c*/a*/..., ~/.config/*/approve-token
				}
			}
		}
	}
	names := webConfigFile.MatchString(c)
	rec := recursive.MatchString(c)
	// tên file cấu hình web kèm tham chiếu HOME/.config, hoặc trong lệnh tìm kiếm
	if names && (homeRef || rec) {
		return true
	}
	// grep -rn aisdlc ~, find $HOME -path '*aisdlc*', grep -r "" ~/.config, rg approve ~/.config
	return rec && rootAncestor
}

// globMatches: một đoạn glob kiểu shell (* ? [..] {a,b}) có thể khớp name không; mẫu hỏng thì coi là có.
func globMatches(glob, name string) bool {
	if !globMeta.MatchString(glob) {
		return glob == name
	}
	g := []rune(glob)
	var re strings.Builder
	re.WriteString(`(?s)^(?:`)
	for i := 0; i < len(g); i++ {
		switch ch := g[i]; ch {
		case '*':
			re.WriteString(".*")
		case '?':
			re.WriteString(".")
		case '[':
			j := indexRune(g, ']', i+1)
			if j < 0 {
				return true
			}
			cls := string(g[i+1 : j])
			if strings.HasPrefix(cls, "!") {
				cls = "^" + cls[1:]
			}
			re.WriteString("[" + strings.ReplaceAll(cls, `\`, `\\`) + "]")
			i = j
		case '{':
			j := indexRune(g, '}', i+1)
			if j < 0 {
				return true
			}
			re.WriteString("(?:")
			for k, alt := range strings.Split(string(g[i+1:j]), ",") {
				if k > 0 {
					re.WriteByte('|')
				}
				re.WriteString(regexp.QuoteMeta(alt))
			}
			re.WriteByte(')')
			i = j
		default:
			re.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	re.WriteString(`)$`)
	r, err := regexp.Compile(re.String())
	if err != nil {
		return true
	}
	return r.MatchString(name)
}

func indexRune(rs []rune, want rune, from int) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == want {
			return i
		}
	}
	return -1
}

// ---- đường dẫn ----

func absClean(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return a
}

// parentOf: thư mục cha; false khi đã là gốc (Java: getParent() null).
func parentOf(p string) (string, bool) {
	d := filepath.Dir(p)
	return d, d != p
}

// mustParent như getParent() của Java, panic (NPE) khi không có; chỉ gọi trong WebConfigDecision (đã recover).
func mustParent(p string) string {
	d, ok := parentOf(p)
	if !ok {
		panic("no parent: " + p)
	}
	return d
}

// pathStartsWith: p bắt đầu bằng prefix theo từng đoạn (Path.startsWith); cả hai đã chuẩn hóa.
func pathStartsWith(p, prefix string) bool {
	if p == prefix {
		return true
	}
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(p, prefix)
	}
	return strings.HasPrefix(p, prefix+"/")
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// realize: real path của tổ tiên tồn tại sâu nhất, nối lại với các đoạn chưa tồn tại.
func realize(abs string) string {
	existing := abs
	rest := ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		name := filepath.Base(existing)
		if rest == "" {
			rest = name
		} else {
			rest = name + "/" + rest
		}
		parent, ok := parentOf(existing)
		if !ok {
			return abs
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return abs
	}
	if rest == "" {
		return real
	}
	return filepath.Join(real, rest)
}

func repoRoot(s PolicyState) string { return absClean(s.RepoRoot) }

func realRoot(s PolicyState) string {
	r, err := filepath.EvalSymlinks(s.RepoRoot)
	if err != nil {
		return repoRoot(s)
	}
	return absClean(r)
}

var errInvalidPath = errors.New("invalid path")

// relative trả đường dẫn tương đối (dấu "/"); inside=false nếu nằm ngoài repo; lỗi nếu path hỏng.
func relative(s PolicyState, file string) (rel string, inside bool, err error) {
	if strings.ContainsRune(file, 0) {
		return "", false, errInvalidPath
	}
	root := repoRoot(s)
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, file)
	}
	real := realize(filepath.Clean(abs))
	rr := realRoot(s)
	if !pathStartsWith(real, rr) {
		return "", false, nil
	}
	if real == rr {
		return "", true, nil
	}
	return filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(real, rr), "/")), true, nil
}

// stateFiles: các file trạng thái của feature đang active (đường dẫn tương đối) và thư mục .aisdlc.
func stateFiles(s PolicyState) []string {
	out := []string{".aisdlc/"}
	if p := s.Paths; p != nil {
		for _, f := range []string{p.Gates(), p.Steps(), p.ScopeExtra(), p.Feedback(), p.Risk()} {
			out = append(out, p.Rel(f))
		}
	}
	return out
}

type approvedArtifact struct {
	rel  string
	gate gate.Gate
}

// approvedArtifacts: artifact (đường dẫn tương đối) và cổng, chỉ gồm các cổng đã có approval (APPROVED hoặc
// STALE), theo thứ tự cổng; artifact trùng thì cổng sau thắng (như LinkedHashMap.put).
func approvedArtifacts(s PolicyState) ([]approvedArtifact, error) {
	if s.Gates == nil || s.Paths == nil {
		return nil, nil
	}
	var out []approvedArtifact
	for _, g := range gate.Sequence(s.Gates.Flow()) {
		has, err := s.Gates.HasApproval(g)
		if err != nil {
			return nil, err
		}
		if !has {
			continue
		}
		for _, a := range g.Artifacts(*s.Paths, s.Gates.Flow()) {
			out = append(out, approvedArtifact{s.Paths.Rel(a), g})
		}
	}
	return out, nil
}

func lookupApproved(list []approvedArtifact, rel string) (gate.Gate, bool) {
	var g gate.Gate
	found := false
	for _, a := range list {
		if a.rel == rel {
			g, found = a.gate, true
		}
	}
	return g, found
}

func errDeny(err error) Decision {
	return Deny(i18n.Tf("aisdlc lỗi: %s", strings.Join(strings.Fields(err.Error()), " ")))
}

// PreToolUse quyết định cho một lần gọi tool trước khi chạy. Lỗi đọc trạng thái (gates.json, steps.json...)
// thì chặn (fail closed): bản Java ném exception và hook không cho tool chạy.
func PreToolUse(call ToolCall, s PolicyState) Decision {
	tool := call.ToolName
	bash := tool == "Bash"
	if !bash && !writeTools[tool] {
		return Ok()
	}

	if s.Control != nil && s.Control.Pause != nil { // 0: dev tạm dừng tại web
		return DenyCode("PAUSED", i18n.Tf("Dev đã tạm dừng tại web: %s. Dừng lại và chờ. Khi dev báo tiếp tục thì thử lại.", s.Control.Pause.By))
	}

	if bash {
		approved, err := approvedArtifacts(s)
		if err != nil {
			return errDeny(err)
		}
		prot := stateFiles(s)
		for _, a := range approved {
			prot = append(prot, a.rel)
		}
		prot = append(prot, lockedConfig...)
		both := append([]string{}, prot...)
		root := repoRoot(s)
		rr := realRoot(s)
		for _, r := range prot {
			both = append(both, filepath.ToSlash(filepath.Join(root, r)), filepath.ToSlash(filepath.Join(rr, r)))
		}
		if reason, blocked := BashCheck(call.Command, both, root, call.Cwd); blocked {
			return Deny(reason)
		}
		return Ok()
	}

	if javaBlank(call.FilePath) {
		return Deny(i18n.T("Không xác định được đường dẫn file của lệnh ghi; từ chối để an toàn."))
	}
	rel, inside, err := relative(s, call.FilePath)
	if err != nil {
		return Deny(i18n.Tf("Đường dẫn file không hợp lệ: %s; từ chối để an toàn.", call.FilePath))
	}
	if !inside {
		return Ok() // 3
	}

	// file cấu hình chỉ dev sửa: chặn bất kể cổng và bước. risk-policy.yaml giữ nguyên văn câu của bản Java.
	if rel == riskPolicyFile {
		return DenyCode("CONFIG_LOCKED", i18n.Tf("Chính sách rủi ro do dev sửa (web hoặc MR), agent không được sửa: %s", riskPolicyFile))
	}
	if rel == configFile {
		return DenyCode("CONFIG_LOCKED", i18n.Tf("%s chỉ dev sửa, ngoài Claude Code.", configFile))
	}

	if rel == ".aisdlc" || strings.HasPrefix(rel, ".aisdlc/") || featureState.MatchString(rel) || contains(stateFiles(s), rel) {
		return Deny(i18n.Tf("File trạng thái do CLI quản lý, agent không được sửa trực tiếp: %s. Dùng lệnh `aisdlc` tương ứng.", rel)) // 4
	}

	approved, err := approvedArtifacts(s)
	if err != nil {
		return errDeny(err)
	}
	if g, ok := lookupApproved(approved, rel); ok { // 5
		return Deny(i18n.Tf("%s đã được duyệt ở cổng %s; nếu cần sửa, dev chạy `aisdlc reopen %s`", rel, string(g), strings.ToLower(string(g))))
	}

	if s.Active != "" && strings.HasPrefix(rel, "aisdlc/features/"+string(s.Active)+"/") {
		return Ok() // 6
	}
	if step.ScopeMatches(s.Config.Unmanaged, rel) {
		return Ok() // 7
	}
	if s.Active == "" && len(s.Ambiguous) > 0 {
		keys := make([]string, len(s.Ambiguous))
		for i, k := range s.Ambiguous {
			keys[i] = string(k)
		}
		return DenyCode("AMBIGUOUS_FEATURE", i18n.Tf("Có nhiều tính năng đang mở (%s). Chạy `aisdlc use <KEY>` (hỏi dev nếu chưa biết); nếu vẫn bị chặn (session thiếu AISDLC_SESSION), thêm `--feature <KEY>` vào lệnh aisdlc hoặc khởi động lại session.", strings.Join(keys, ", ")))
	}
	if s.Active == "" {
		return Deny(i18n.T("Chưa có tính năng đang làm (trên nhánh này). Chạy `aisdlc use <KEY>` nếu tính năng đã có, hoặc /aisdlc:init <JIRA-KEY>")) // 8
	}
	if s.Gates == nil || s.Steps == nil {
		return Deny(i18n.Tf("Thiếu trạng thái cổng/bước của tính năng %s; từ chối để an toàn.", string(s.Active)))
	}
	ready, err := s.Gates.CodeReady()
	if err != nil {
		return errDeny(err)
	}
	if !ready { // 9
		first := "?"
		g, ok, err := s.Gates.FirstNotOpen()
		if err != nil {
			return errDeny(err)
		}
		if ok {
			first = string(g)
		}
		return Deny(i18n.Tf("Chưa được viết code: cổng %s chưa được duyệt (dev cần duyệt cổng này trước).", first))
	}
	cur, has, err := s.Steps.Current()
	if err != nil {
		return errDeny(err)
	}
	if !has || cur.Status != step.StepInProgress { // 10
		if has {
			return Deny(i18n.Tf("Bước %s đang chờ dev duyệt: dev chạy `aisdlc step accept %s`, hoặc chạy `aisdlc step start %s` để sửa tiếp.", cur.ID, cur.ID, cur.ID))
		}
		next, err := nextStepID(s)
		if err != nil {
			return errDeny(err)
		}
		return Deny(i18n.Tf("Chưa có bước nào đang chạy. Chạy `aisdlc step start %s`.", next))
	}
	id := cur.ID
	globs, err := s.Steps.AllowedGlobs(id)
	if err != nil {
		return errDeny(err)
	}
	if !step.ScopeMatches(globs, rel) { // 11
		return DenyCode("OUT_OF_SCOPE", i18n.Tf("Ngoài phạm vi %s (%s). Dev có thể chạy `aisdlc scope allow %s`", id, strings.Join(globs, ", "), rel))
	}
	return Ok() // 12
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Stop quyết định cho hook Stop: chặn dừng khi còn bước đang chạy. Lỗi đọc trạng thái thì cho dừng (hook Stop
// không phải hàng rào bảo vệ).
func Stop(stopHookActive bool, s PolicyState) Decision {
	if stopHookActive || s.Steps == nil {
		return Ok()
	}
	if s.Control != nil && s.Control.Pause != nil {
		return Ok()
	}
	cur, has, err := s.Steps.Current()
	if err != nil {
		return Ok()
	}
	if has && cur.Status == step.StepInProgress {
		return Deny(i18n.Tf("Bước %s chưa kết thúc. Chạy `aisdlc step finish %s` và báo kết quả cho dev trước khi dừng.", cur.ID, cur.ID))
	}
	return Ok()
}

// InstructionContext: mỗi chỉ dẫn một khối "Chỉ dẫn của dev (HH:mm): <text>", nối bằng dòng trống; rỗng thì "".
// Chỉ dẫn feedback (trả lại / mở lại) web lưu tiếng Việt: dịch theo locale của repo; ghi chú của người duyệt giữ
// nguyên văn.
func InstructionContext(items []snapshot.Instruction, zone *time.Location) string {
	if zone == nil {
		zone = time.UTC
	}
	loc := i18n.Default()
	blocks := make([]string, 0, len(items))
	for _, i := range items {
		blocks = append(blocks, i18n.Tf("Chỉ dẫn của dev (%s): %s", i.At.In(zone).Format("15:04"),
			snapshot.LocalizeFeedback(i.Text, loc)))
	}
	return strings.Join(blocks, "\n\n")
}

// SessionContext là quy tắc AISDLC và trạng thái hiện tại, đưa vào ngữ cảnh đầu session.
func SessionContext(s PolicyState) string {
	var sb strings.Builder
	sb.WriteString(i18n.T("Quy tắc AISDLC:\n- Quy trình: BRD -> Thiết kế -> Case -> Code từng bước -> Review; mỗi cổng do dev duyệt.\n- Chưa qua cổng thiết kế/case thì không viết code sản phẩm (hook sẽ chặn).\n- Không sửa artifact đã duyệt; cần sửa thì dev chạy `aisdlc reopen <gate>`.\n- Không sửa file trạng thái (gates.json, steps.json, .aisdlc/**), kể cả qua Bash.\n- Chỉ dev chạy: approve, reject, reopen, scope allow, step accept.\n- Bắt đầu bước bằng `aisdlc step start Sx`; chỉ sửa file trong phạm vi bước.\n- Xong bước chạy `aisdlc step finish Sx`, báo kết quả cho dev rồi dừng chờ `step accept`.\n- Cần mở rộng phạm vi: nhờ dev chạy `aisdlc scope allow <path>`."))
	if s.Active == "" {
		if len(s.Ambiguous) > 0 {
			keys := make([]string, len(s.Ambiguous))
			for i, k := range s.Ambiguous {
				keys[i] = string(k)
			}
			sb.WriteString(i18n.Tf("\nCó nhiều tính năng đang mở (%s); session này chưa gắn tính năng nào: chạy `aisdlc use <KEY>` (hỏi dev nếu chưa biết).", strings.Join(keys, ", ")))
		}
		return sb.String()
	}
	sb.WriteString(i18n.Tf("\nTrạng thái: tính năng %s", string(s.Active)))
	var gs []string
	if s.Gates != nil {
		for _, g := range gate.Sequence(s.Gates.Flow()) {
			st, err := s.Gates.State(g)
			state := string(st)
			if err != nil {
				state = "?"
			}
			gs = append(gs, string(g)+"="+state)
		}
	}
	sb.WriteString(i18n.Tf("; cổng: %s", strings.Join(gs, ", ")))
	cur := i18n.T("chưa có bước đang chạy")
	if s.Steps != nil {
		r, ok, err := s.Steps.Current()
		if err != nil {
			cur = "?"
		} else if ok {
			cur = r.ID + " " + string(r.Status)
		}
	}
	sb.WriteString(i18n.Tf("; bước: %s", cur))
	return sb.String()
}

func nextStepID(s PolicyState) (string, error) {
	all, err := s.Steps.All()
	if err != nil {
		return "", err
	}
	n := 0
	for _, r := range all {
		if r.Status == step.StepAccepted {
			n++
		}
	}
	return "S" + strconv.Itoa(n+1), nil
}
