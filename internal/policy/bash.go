package policy

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// bashWrite: lệnh ghi (không tính chuyển hướng >, xét riêng theo đích của nó).
	bashWrite = regexp.MustCompile(`\btee\b|\bsed\s+-i|\brm\b|\bmv\b|\bcp\b|\btruncate\b` +
		`|\bgit\s+(checkout|restore|stash|reset)\b|\bperl\b|\bpython3?\b|\bdd\b`)
	// bashRedirect: chuyển hướng ghi >, >>, N>, &>, >| cùng đích của nó.
	bashRedirect = regexp.MustCompile(`(?:\d+|&)?>>?\|?\s*(&\d+-?|&-|"[^"]*"|'[^']*'|[^\s;&|<>()]+)`)
	// runDir: log/kết quả chạy của CLI: agent được đọc.
	runDir   = regexp.MustCompile(`[^\s;&|<>()'"]*\.aisdlc/run/[^\s;&|<>()'"]*`)
	bashMsg  = "): file trạng thái do CLI quản lý hoặc artifact đã duyệt. Nếu cần sửa artifact, dev chạy `aisdlc reopen <gate>`."
	devMsg   = "Lệnh này chỉ dev được chạy (approve/reject/reopen/scope allow/step accept/risk set); agent không được chạy qua Bash."
	sessMsg  = "Agent không được gán AISDLC_SESSION (đó là định danh session do hook đặt; đổi nó để lách tính năng của session). Muốn đổi tính năng, chạy `aisdlc use <KEY>`."
	sessAsgn = regexp.MustCompile("(?:^|[\\s;&|(\"'`])AISDLC_SESSION\\+?=")
	// fdDup: đích của >& / <& là số fd hoặc "-" (đóng fd), không phải tên file.
	fdDup = regexp.MustCompile(`^(\d+-?|-)$`)
)

var (
	// lockedConfig: file cấu hình chỉ dev sửa, ngoài Claude Code.
	lockedConfig = []string{configFile, riskPolicyFile}
	// stateNames: tên các file trạng thái của một tính năng (featureState).
	stateNames = []string{"gates.json", "steps.json", "scope-extra.json", "feedback.md", "risk.yaml"}
)

// BashCheck trả lý do chặn (và true) nếu lệnh Bash có vẻ ghi vào file được bảo vệ hoặc là lệnh chỉ dev.
//
// protectedPaths: đường dẫn tương đối với repoRoot hoặc tuyệt đối; kết thúc bằng "/" là thư mục. Ngoài chúng,
// luôn bảo vệ lockedConfig, .aisdlc/ và file trạng thái của mọi tính năng (featureState). cwd là thư mục làm
// việc của hook (rỗng: repoRoot). Hai cách khớp, chặn nếu một trong hai khớp: chuỗi con như bản Java, và token
// đường dẫn đã chuẩn hóa (theo dõi cd/pushd/popd giữa các đoạn lệnh).
func BashCheck(command string, protectedPaths []string, repoRoot, cwd string) (string, bool) {
	if javaBlank(command) {
		return "", false
	}
	if DevCommandMentions(command) {
		return devMsg, true
	}
	if sessAsgn.MatchString(command) {
		return sessMsg, true
	}
	prot := append([]string{}, protectedPaths...)
	for _, p := range append(append([]string{}, lockedConfig...), ".aisdlc/") {
		prot = append(prot, p, filepath.ToSlash(filepath.Join(repoRoot, p))+suffixSlash(p))
	}
	// Chuyển hướng chỉ là ghi khi đích là file được bảo vệ (bỏ qua N>&M, /dev/null, file khác)
	var rest strings.Builder
	last := 0
	for _, m := range bashRedirect.FindAllStringSubmatchIndex(command, -1) {
		target := unquote(command[m[2]:m[3]])
		if !strings.HasPrefix(target, "&") && target != "/dev/null" {
			if p, ok := protectedIn(target, prot); ok {
				return "Lệnh có vẻ ghi vào file được bảo vệ (" + p + bashMsg, true
			}
		}
		rest.WriteString(command[last:m[0]])
		rest.WriteByte(' ')
		last = m[1]
	}
	rest.WriteString(command[last:])
	others := runDir.ReplaceAllString(rest.String(), " ")
	if bashWrite.MatchString(others) {
		if p, ok := protectedIn(others, prot); ok {
			return "Lệnh có vẻ ghi vào file được bảo vệ (" + p + bashMsg, true
		}
	}
	if p, ok := newPathMatcher(prot, repoRoot).writes(command, cwd); ok {
		return "Lệnh có vẻ ghi vào file được bảo vệ (" + p + bashMsg, true
	}
	return "", false
}

func suffixSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return "/"
	}
	return ""
}

func protectedIn(text string, protectedPaths []string) (string, bool) {
	for _, p := range protectedPaths {
		if p != "" && strings.Contains(text, p) {
			return p, true
		}
	}
	return "", false
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// ---- khớp theo đường dẫn đã chuẩn hóa ----

// pathMatcher so token của lệnh với danh sách bảo vệ, sau khi chuẩn hóa token thành đường dẫn tương đối với repo.
type pathMatcher struct {
	roots []string        // repoRoot và real path của nó
	files map[string]bool // file được bảo vệ (tương đối)
	dirs  []string        // thư mục được bảo vệ (tương đối, không có "/" cuối)
	names map[string]bool // basename của mọi thứ được bảo vệ
}

func newPathMatcher(prot []string, repoRoot string) *pathMatcher {
	root := filepath.Clean(repoRoot)
	m := &pathMatcher{roots: []string{root}, files: map[string]bool{}, names: map[string]bool{}}
	if r := realize(root); r != root {
		m.roots = append(m.roots, r)
	}
	for _, n := range stateNames {
		m.names[n] = true
	}
	for _, p := range prot {
		if p == "" {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(p))
		if filepath.IsAbs(p) {
			r, ok := m.rel(p)
			if !ok {
				continue
			}
			rel = r
		}
		if rel == "" || rel == "." {
			continue
		}
		m.names[rel[strings.LastIndex(rel, "/")+1:]] = true
		if strings.HasSuffix(p, "/") {
			m.dirs = append(m.dirs, rel)
		} else {
			m.files[rel] = true
		}
	}
	return m
}

// rel: đường dẫn tuyệt đối thành tương đối với repo (dấu "/"); false nếu nằm ngoài repo.
func (m *pathMatcher) rel(abs string) (string, bool) {
	abs = filepath.Clean(abs)
	for _, a := range []string{abs, realize(abs)} {
		for _, r := range m.roots {
			if pathStartsWith(a, r) {
				return filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(a, r), "/")), true
			}
		}
	}
	return "", false
}

// protectedRel: đúng file được bảo vệ, nằm trong thư mục được bảo vệ, hoặc là file trạng thái của một tính năng.
func (m *pathMatcher) protectedRel(rel string) bool {
	if rel == "" {
		return false
	}
	if m.files[rel] || featureState.MatchString(rel) {
		return true
	}
	for _, d := range m.dirs {
		if pathStartsWith(rel, d) {
			return true
		}
	}
	return false
}

// shellWord là một từ của lệnh sau khi bỏ dấu nháy.
type shellWord struct {
	text     string
	redirect bool // đích của chuyển hướng ghi (>, >>, >|, &>, >&file, <>)
}

// shellDir: thư mục hiện tại khi duyệt lệnh; known=false khi không xác định (cd không đối số, cd "$X", cd ~).
type shellDir struct {
	cwd   string
	known bool
}

// writes duyệt lệnh theo từng đoạn (tách theo &&, ||, ;, |, &, xuống dòng và subshell), theo dõi cd/pushd/popd,
// và trả đường dẫn đã chuẩn hóa của file được bảo vệ mà một đoạn có vẻ ghi vào.
func (m *pathMatcher) writes(command, cwd string) (string, bool) {
	cur := shellDir{cwd: m.roots[0], known: true}
	if cwd != "" {
		if filepath.IsAbs(cwd) {
			cur.cwd = filepath.Clean(cwd)
		} else {
			cur.cwd = filepath.Join(m.roots[0], cwd)
		}
	}
	var dirStack, subStack []shellDir
	hit, found := "", false
	var words []shellWord
	splitShell(command, &words,
		func() { // hết một đoạn
			if !found {
				if p, ok := m.segmentWrites(words, cur); ok {
					hit, found = p, true
				}
				cur, dirStack = changeDir(words, cur, dirStack)
			}
			words = words[:0]
		},
		func() { subStack = append(subStack, cur) },
		func() {
			if n := len(subStack); n > 0 {
				cur, subStack = subStack[n-1], subStack[:n-1]
			}
		})
	return hit, found
}

// segmentWrites: đích chuyển hướng ghi luôn được xét; các từ khác chỉ xét khi đoạn có động từ ghi (bashWrite).
func (m *pathMatcher) segmentWrites(words []shellWord, d shellDir) (string, bool) {
	var plain []string
	for _, w := range words {
		if !w.redirect {
			plain = append(plain, w.text)
		}
	}
	verb := bashWrite.MatchString(strings.Join(plain, " "))
	cmdSeen := false
	for _, w := range words {
		if !w.redirect && !verb || w.redirect && w.text == "/dev/null" {
			continue
		}
		var cands []string
		switch {
		case w.redirect:
			cands = []string{w.text}
		case !cmdSeen && !assignHead.MatchString(w.text):
			cmdSeen = true // tên lệnh, không phải đích ghi
			continue
		case !strings.HasPrefix(w.text, "-"):
			cands = []string{w.text}
		}
		if i := strings.Index(w.text, "="); i >= 0 && !w.redirect {
			cands = append(cands, w.text[i+1:]) // of=..., --file=..., A=b
		}
		for _, c := range cands {
			if p, ok := m.wordWrites(c, d, w.redirect); ok {
				return p, true
			}
		}
	}
	return "", false
}

// wordWrites: token được resolve theo thư mục hiện tại, Clean, đổi sang tương đối với repo rồi so; khi thư mục
// không xác định, token tương đối trùng tên file được bảo vệ (theo basename) là đủ để chặn.
func (m *pathMatcher) wordWrites(word string, d shellDir, redirect bool) (string, bool) {
	if word == "" {
		return "", false
	}
	trimmed := strings.TrimRight(word, "/")
	base := trimmed[strings.LastIndex(trimmed, "/")+1:]
	// Khi thư mục đã biết, mọi token (kể cả không có "/" và không trùng tên, vd. `cd .aisdlc && rm active`)
	// đều được resolve: so đúng đường dẫn nên không làm tăng chặn thừa.
	if !filepath.IsAbs(word) && !d.known {
		if m.names[base] {
			return filepath.ToSlash(filepath.Clean(word)), true
		}
		return "", false
	}
	abs := word
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(d.cwd, word)
	}
	rel, ok := m.rel(abs)
	if !ok {
		return "", false
	}
	// log/kết quả chạy của CLI: agent được đọc (cp, grep...); ghi bằng chuyển hướng vẫn chặn, như bản Java
	if !redirect && pathStartsWith(rel, ".aisdlc/run") {
		return "", false
	}
	if m.protectedRel(rel) {
		return rel, true
	}
	return "", false
}

// changeDir áp dụng cd/pushd/popd của một đoạn lệnh lên thư mục hiện tại.
func changeDir(words []shellWord, d shellDir, stack []shellDir) (shellDir, []shellDir) {
	var args []string
	for _, w := range words {
		if !w.redirect {
			args = append(args, w.text)
		}
	}
	for len(args) > 0 && (assignHead.MatchString(args[0]) || args[0] == "builtin" || args[0] == "command") {
		args = args[1:]
	}
	if len(args) == 0 {
		return d, stack
	}
	cmd := args[0]
	switch cmd {
	case "popd":
		if n := len(stack); n > 0 {
			return stack[n-1], stack[:n-1]
		}
		return shellDir{}, stack
	case "pushd":
		stack = append(stack, d)
	case "cd":
	default:
		return d, stack
	}
	dir, has := "", false
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				dir, has = args[i+1], true
			}
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue // -P, -L, -e, -@
		}
		dir, has = a, true
		break
	}
	// không đối số (HOME; pushd đảo ngăn xếp), có $ hoặc `, ~, - (OLDPWD), +N: không xác định
	if !has || dir == "" || strings.ContainsAny(dir, "$`") || strings.HasPrefix(dir, "~") || dir == "-" ||
		cmd == "pushd" && strings.HasPrefix(dir, "+") {
		return shellDir{}, stack
	}
	if filepath.IsAbs(dir) {
		return shellDir{cwd: filepath.Clean(dir), known: true}, stack
	}
	if !d.known {
		return d, stack
	}
	return shellDir{cwd: filepath.Join(d.cwd, dir), known: true}, stack
}

// splitShell tách lệnh thành từ (bỏ nháy '..', "..", \x) và đoạn. end() được gọi ở cuối mỗi đoạn; open() và
// close() khi vào/ra subshell (( ), $( ), ` `), kể cả $( ) và ` ` trong nháy kép. Đây là heuristic, không phải
// parser shell đầy đủ (không hiểu heredoc, ${..}, comment): sai lệch chỉ làm chặn thừa hoặc rơi về phép so
// chuỗi con của bản Java.
func splitShell(command string, words *[]shellWord, end, open, close func()) {
	rs := []rune(command)
	var cur strings.Builder
	inWord := false
	redirectNext := false // từ kế tiếp là đích chuyển hướng ghi
	dupNext := false      // từ kế tiếp đứng sau >& hoặc <&: số fd thì bỏ
	var ctx []rune        // ngữ cảnh lồng nhau: 'p' subshell ngoặc, 'b' backtick, 'd' nháy kép
	top := func() rune {
		if len(ctx) == 0 {
			return 0
		}
		return ctx[len(ctx)-1]
	}
	flush := func() {
		if !inWord {
			return
		}
		text := cur.String()
		if !(dupNext && fdDup.MatchString(text)) {
			*words = append(*words, shellWord{text: text, redirect: redirectNext})
		}
		redirectNext, dupNext = false, false
		cur.Reset()
		inWord = false
	}
	endSeg := func() {
		flush()
		redirectNext, dupNext = false, false
		end()
	}
	fdWord := func() { // N> hoặc N<: từ đang có toàn chữ số là fd, không phải tên file
		if inWord && digits.MatchString(cur.String()) {
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		next := rune(0)
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		if top() == 'd' {
			switch {
			case ch == '"':
				ctx = ctx[:len(ctx)-1]
			case ch == '\\' && next != 0:
				i++
				cur.WriteRune(rs[i])
			case ch == '$' && next == '(':
				i++
				endSeg()
				ctx = append(ctx, 'p')
				open()
				continue
			case ch == '`':
				endSeg()
				ctx = append(ctx, 'b')
				open()
				continue
			default:
				cur.WriteRune(ch)
			}
			inWord = true
			continue
		}
		switch {
		case ch == '\\':
			if next == '\n' {
				i++
			} else if next != 0 {
				i++
				cur.WriteRune(rs[i])
				inWord = true
			}
		case ch == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				cur.WriteRune(rs[j])
				j++
			}
			i = j
			inWord = true
		case ch == '"':
			ctx = append(ctx, 'd')
			inWord = true
		case ch == '$' && next == '(':
			i++
			endSeg()
			ctx = append(ctx, 'p')
			open()
		case ch == '(':
			endSeg()
			ctx = append(ctx, 'p')
			open()
		case ch == ')':
			endSeg()
			if top() == 'p' {
				ctx = ctx[:len(ctx)-1]
				close()
			}
		case ch == '`':
			endSeg()
			if top() == 'b' {
				ctx = ctx[:len(ctx)-1]
				close()
			} else {
				ctx = append(ctx, 'b')
				open()
			}
		case ch == '>' || ch == '&' && next == '>':
			fdWord()
			flush()
			if ch == '&' {
				i++
			}
			if i+1 < len(rs) && (rs[i+1] == '>' || rs[i+1] == '|') {
				i++
			}
			if i+1 < len(rs) && rs[i+1] == '&' { // >&2, >&- (fd); >&file (ghi file)
				i++
				dupNext = true
			}
			redirectNext = true
		case ch == '<':
			fdWord()
			flush()
			switch next {
			case '>': // <> mở đọc-ghi
				i++
				redirectNext = true
			case '&':
				i++
				dupNext = true
			default:
				for i+1 < len(rs) && rs[i+1] == '<' {
					i++
				}
			}
		case ch == ';' || ch == '|' || ch == '&' || ch == '\n':
			endSeg()
		case ch == ' ' || ch == '\t' || ch == '\r':
			flush()
		default:
			cur.WriteRune(ch)
			inWord = true
		}
	}
	endSeg()
}
