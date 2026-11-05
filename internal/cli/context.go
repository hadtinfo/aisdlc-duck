package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
	"aisdlc/internal/policy"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

// Abort dừng lệnh với mã thoát và thông báo cho dev (in nguyên văn ra stderr), như Context.Abort của Java.
type Abort struct {
	Code int
	Msg  string
}

func (a *Abort) Error() string { return a.Msg }

// abort tạo *Abort dưới dạng error.
func abort(code int, msg string) error { return &Abort{Code: code, Msg: msg} }

// asAbort như errors.As cho *Abort.
func asAbort(err error, a **Abort) bool { return errors.As(err, a) }

// WebPhase4Msg là thông báo khi lệnh cần gọi web ở chế độ duyệt trên web (bản Go chưa có web client).
const WebPhase4Msg = "Chế độ duyệt trên web chưa có trong bản Go (giai đoạn 4)."

// statusWebPhase4Line: dòng `aisdlc status` in ở chế độ duyệt trên web.
const statusWebPhase4Line = "Bản Go này chưa nộp/duyệt được trên web (giai đoạn 4): submit, approve, reject, reopen," +
	" risk set/reclassify sẽ dừng; trạng thái chỉ đọc từ cache đã ký."

const (
	noConsoleMsg = "Lệnh này chỉ dev được chạy, trong terminal thật (không tìm thấy console). " +
		"Agent không được chạy lệnh này."
	noGrantMsg = "Lệnh này chỉ dev được chạy. Không có xác nhận trong Claude Code (hoặc đã hết" +
		" hạn). Chạy lại lệnh duyệt một mình, không kèm lệnh khác."
	maxSessionID = 100
)

// Context giữ mọi thứ một lệnh cần từ môi trường; test thay được đồng hồ, env, git, runner và console.
type Context struct {
	in      io.Reader
	out     io.Writer
	err     io.Writer
	console DevConsole
	cwd     string
	std     context.Context

	color           bool
	now             func() time.Time
	env             map[string]string
	git             func(root string) *gitx.Repo
	runner          func(root string, cfg model.RepoConfig) CaseRunner
	network         bool
	loginWarned     bool
	sessionID       string
	featureOverride model.FeatureKey
	args            []string
	banner          bool
	bannerPrinted   bool
	exit            int
}

// NewContext tạo Context với mặc định của tiến trình: env của OS, đồng hồ UTC, git thật, runner chờ harness.
func NewContext(in io.Reader, out, errw io.Writer, console DevConsole, cwd string) *Context {
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	env := osEnv()
	return &Context{
		in: in, out: out, err: errw, console: console, cwd: filepath.Clean(cwd), std: context.Background(),
		color:   colorDefault(isTerminal(out), env),
		now:     func() time.Time { return time.Now().UTC() },
		env:     env,
		git:     gitx.New,
		runner:  func(root string, _ model.RepoConfig) CaseRunner { return defaultRunner(root) },
		network: true,
		banner:  true,
	}
}

func osEnv() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// isTerminal: w là *os.File gắn với thiết bị ký tự (terminal).
func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// colorDefault: có màu khi có terminal thật và env cho phép.
func colorDefault(terminal bool, env map[string]string) bool {
	return terminal && colorAllowed(env)
}

// colorAllowed: không có NO_COLOR, TERM khác trống và khác dumb.
func colorAllowed(env map[string]string) bool {
	_, noColor := env["NO_COLOR"]
	term := env["TERM"]
	return !noColor && strings.TrimSpace(term) != "" && term != "dumb"
}

// WithNow thay đồng hồ.
func (c *Context) WithNow(f func() time.Time) *Context { c.now = f; return c }

// WithEnv thay biến môi trường (bản sao).
func (c *Context) WithEnv(e map[string]string) *Context {
	c.env = make(map[string]string, len(e))
	for k, v := range e {
		c.env[k] = v
	}
	return c
}

// WithGit thay cách mở gitx.Repo.
func (c *Context) WithGit(f func(root string) *gitx.Repo) *Context { c.git = f; return c }

// WithRunner thay CaseRunner.
func (c *Context) WithRunner(f func(root string, cfg model.RepoConfig) CaseRunner) *Context {
	c.runner = f
	return c
}

// WithColor bật/tắt màu ANSI khi in review card.
func (c *Context) WithColor(b bool) *Context { c.color = b; return c }

// WithSessionID đặt session_id của input hook (cắt ở 100 ký tự như web nhận; trống = không có).
func (c *Context) WithSessionID(id string) *Context {
	c.sessionID = truncSession(id)
	return c
}

// WithFeatureOverride đặt tính năng ghi rõ trên lệnh (--feature); thắng mọi cách phân giải khác.
func (c *Context) WithFeatureOverride(k model.FeatureKey) *Context { c.featureOverride = k; return c }

// Offline dùng cho hook: từ đây không gọi mạng, chỉ đọc cache; không in dòng [KEY].
func (c *Context) Offline() *Context { c.network = false; return c }

// WithoutBanner dùng cho lệnh --json: không in dòng [KEY] để stdout vẫn là JSON thuần.
func (c *Context) WithoutBanner() *Context { c.banner = false; return c }

func (c *Context) withArgs(a []string) { c.args = append([]string(nil), a...) }

func truncSession(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	if r := []rune(id); len(r) > maxSessionID {
		return string(r[:maxSessionID])
	}
	return id
}

// In là stdin của lệnh.
func (c *Context) In() io.Reader { return c.in }

// Out là stdout của lệnh.
func (c *Context) Out() io.Writer { return c.out }

// Err là stderr của lệnh.
func (c *Context) Err() io.Writer { return c.err }

// Println in một dòng ra stdout.
func (c *Context) Println(s string) { fmt.Fprintln(c.out, s) }

// Eprintln in một dòng ra stderr.
func (c *Context) Eprintln(s string) { fmt.Fprintln(c.err, s) }

// Console là nguồn xác nhận của dev.
func (c *Context) Console() DevConsole { return c.console }

// Cwd là thư mục hiện tại (tuyệt đối).
func (c *Context) Cwd() string { return c.cwd }

// Std là context.Context cho các lệnh gọi git.
func (c *Context) Std() context.Context { return c.std }

// Now là thời điểm hiện tại theo đồng hồ của Context (UTC).
func (c *Context) Now() time.Time { return c.now() }

// Clock là đồng hồ của Context (đưa cho gate.Service, step.Service).
func (c *Context) Clock() func() time.Time { return c.now }

// Color cho biết có in màu ANSI không.
func (c *Context) Color() bool { return c.color }

// Network false trong hook (Offline).
func (c *Context) Network() bool { return c.network }

// SessionID là session_id của hook ("" ở lệnh CLI).
func (c *Context) SessionID() string { return c.sessionID }

// Env trả biến môi trường; ok=false khi không có hoặc chỉ có khoảng trắng.
func (c *Context) Env(name string) (string, bool) {
	v, ok := c.env[name]
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

// Home là HOME của dev (chỉ theo env của Context, để test thay được).
func (c *Context) Home() (string, bool) { return c.Env("HOME") }

// Git mở gitx.Repo cho root.
func (c *Context) Git(root string) *gitx.Repo { return c.git(root) }

// Runner trả CaseRunner cho repo.
func (c *Context) Runner(root string, cfg model.RepoConfig) CaseRunner { return c.runner(root, cfg) }

// Invocation là lệnh chỉ dev đang chạy, dựng từ đối số nguyên văn, nếu động từ đúng là verb.
func (c *Context) Invocation(verb string) (policy.DevCommand, bool) {
	d, ok := policy.DevCommandFromArgs(c.args)
	if !ok || d.Verb != verb {
		return policy.DevCommand{}, false
	}
	return d, true
}

// ResolveSession: session_id của hook, hoặc AISDLC_SESSION (đã trim, cắt 100 ký tự) ở lệnh CLI.
func (c *Context) ResolveSession() (string, bool) {
	if c.sessionID != "" {
		return c.sessionID, true
	}
	v, ok := c.Env("AISDLC_SESSION")
	if !ok {
		return "", false
	}
	v = truncSession(strings.TrimSpace(v))
	return v, v != ""
}

// RepoRoot là thư mục cha gần nhất của cwd có aisdlc/config.yaml.
func (c *Context) RepoRoot() (string, error) {
	if r, ok := FindRepo(c.cwd); ok {
		return r, nil
	}
	return "", abort(2, "Không tìm thấy aisdlc/config.yaml ở thư mục này hoặc thư mục cha. "+
		"Chạy `aisdlc setup` ở gốc repo trước.")
}

// Feature là tính năng của lệnh theo FeatureResolver; nhiều tính năng đang mở thì hỏi (có console) hoặc exit 2.
func (c *Context) Feature() (*Feature, error) {
	root, err := c.RepoRoot()
	if err != nil {
		return nil, err
	}
	session, _ := c.ResolveSession()
	res, err := NewFeatureResolver(c).Resolve(root, c.featureOverride, session)
	if err != nil {
		return nil, err
	}
	var key model.FeatureKey
	switch r := res.(type) {
	case Resolved:
		key = r.Key
	case Ambiguous:
		if key, err = c.choose(r.Open); err != nil {
			return nil, err
		}
	default:
		return nil, abort(2, "Chưa có tính năng đang làm. Chạy `aisdlc init <KEY>` hoặc `aisdlc use <KEY>`.")
	}
	paths := model.FeaturePaths{RepoRoot: root, Key: key}
	if !exists(paths.Meta()) {
		// .aisdlc/active và gắn kết session không theo git: sau khi đổi nhánh có thể trỏ tới tính năng không có ở đây
		return nil, abort(2, "Tính năng đang làm "+key.String()+" không có trên nhánh này (thiếu "+
			paths.Rel(paths.Meta())+"). Chạy `aisdlc use <KEY>` (tính năng đã có) hoặc "+
			"`aisdlc init <KEY>` (tính năng mới).")
	}
	c.FeatureBanner(key)
	return c.FeatureAt(root, key)
}

func (c *Context) choose(open []model.FeatureKey) (model.FeatureKey, error) {
	names := make([]string, len(open))
	var prompt strings.Builder
	prompt.WriteString("Có nhiều tính năng đang mở:\n")
	for i, k := range open {
		names[i] = k.String()
		fmt.Fprintf(&prompt, "  %d) %s\n", i+1, k)
	}
	prompt.WriteString("Chọn số (hoặc dùng --feature <KEY>): ")
	list := strings.Join(names, ", ")
	typed, ok := c.console.ReadLine(prompt.String())
	if !ok {
		return "", abort(2, "Có nhiều tính năng đang mở: "+list+". Ghi rõ --feature <KEY>.")
	}
	t := strings.TrimSpace(typed)
	if n, err := strconv.Atoi(t); err == nil && n >= 1 && n <= len(open) {
		return open[n-1], nil
	}
	for _, k := range open {
		if k.String() == t {
			return k, nil
		}
	}
	return "", abort(2, "Lựa chọn không hợp lệ: \""+t+"\" (các tính năng đang mở: "+list+").")
}

// FeatureBanner in [KEY] làm dòng đầu stdout, một lần mỗi lệnh (không in ở hook và lệnh --json).
func (c *Context) FeatureBanner(key model.FeatureKey) {
	if !c.banner || c.bannerPrinted || !c.network {
		return
	}
	c.bannerPrinted = true
	c.Println("[" + key.String() + "]")
}

// FeatureAt dựng tính năng key trong repo root.
func (c *Context) FeatureAt(root string, key model.FeatureKey) (*Feature, error) {
	paths := model.FeaturePaths{RepoRoot: root, Key: key}
	if !exists(paths.Meta()) {
		return nil, abort(2, "Không có tính năng "+key.String()+" (thiếu "+paths.Rel(paths.Meta())+").")
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return nil, err
	}
	meta, err := model.ReadFeatureMeta(paths.Meta())
	if err != nil {
		return nil, err
	}
	adapter, err := langs.For(cfg.Language)
	if err != nil {
		return nil, err
	}
	f := &Feature{Root: root, Config: cfg, Key: key, Paths: paths, Meta: meta, Adapter: adapter, ctx: c}
	f.Web = NewWebCache(c, root)
	f.Mode = f.Web.CachedMode(cfg)
	if f.Mode == gate.ModeWeb {
		f.Store = f.Web.Store(key)
	} else {
		f.Store = gate.NewLocalGateStore(paths)
	}
	f.Gates = gate.NewService(paths, f.Flow(), cfg, f.Store, c.now)
	if c.network && f.Mode == gate.ModeWeb && !f.Web.LoggedIn() {
		if !c.loginWarned {
			c.Eprintln("Chưa `aisdlc web login` trên máy này: repo duyệt trên web, mọi cổng coi như" +
				" chưa duyệt.")
		}
		c.loginWarned = true
	}
	f.Control = f.Web.Control(key)
	return f, nil
}

// WebNetwork: lệnh cần gọi web (nộp/duyệt qua web, làm mới, gửi) ở chế độ duyệt trên web thì dừng, vì bản Go chưa
// có web client (giai đoạn 4). Chế độ local trả nil.
func (c *Context) WebNetwork(f *Feature) error {
	if f != nil && f.Mode == gate.ModeWeb {
		return abort(2, WebPhase4Msg)
	}
	return nil
}

// GateOf đọc tên cổng (không phân biệt hoa thường).
func GateOf(name string) (gate.Gate, error) {
	g := gate.Gate(strings.ToUpper(name))
	switch g {
	case gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW:
		return g, nil
	}
	return "", abort(2, "Cổng không hợp lệ: "+name+" (brd, design, cases, review).")
}

// ConfirmDev xác nhận lệnh chỉ dev. Có console thật: bắt gõ lại đúng feature key (kênh terminal, trả ""). Không có
// console: chỉ khi sessionApprove bật ở baseRef, tiêu thụ phiếu cho phép một lần mà hook ghi khi dev bấm Yes trong
// Claude Code — khớp session, tính năng, lệnh (action) và gateHash ("" với lệnh không gắn cổng) — rồi trả
// gate.ApprovalClaudeCode. action rỗng (Verb == ""): lệnh không có phiếu, chỉ console thật. Không thì exit 3.
func (c *Context) ConfirmDev(key model.FeatureKey, action policy.DevCommand, gateHash string) (string, error) {
	if typed, ok := c.console.ReadLine("Gõ lại feature key (" + key.String() + ") để xác nhận: "); ok {
		if t := strings.TrimSpace(typed); t != key.String() {
			return "", abort(3, "Key không khớp ("+t+" ≠ "+key.String()+"); đã hủy.")
		}
		return "", nil
	}
	root, err := c.RepoRoot()
	if err != nil {
		return "", err
	}
	if action.Verb == "" {
		return "", abort(3, noConsoleMsg)
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return "", err
	}
	if !model.ReadSessionApprove(c.std, c.Git(root), cfg).Enabled {
		return "", abort(3, noConsoleMsg)
	}
	session, ok := c.ResolveSession()
	if !ok {
		return "", abort(3, noGrantMsg)
	}
	granted, err := policy.ConsumeGrant(root, session, key.String(), action.GrantKey(), gateHash, c.now())
	if err != nil || !granted { // như DevGrants.consume: mọi lỗi là không có phiếu
		return "", abort(3, noGrantMsg)
	}
	return gate.ApprovalClaudeCode, nil
}

// AskDev hỏi dev qua console thật; không có terminal (agent, pipe, CI) thì exit 3.
func (c *Context) AskDev(prompt string) (string, error) {
	typed, ok := c.console.ReadLine(prompt)
	if !ok {
		return "", abort(3, noConsoleMsg)
	}
	return strings.TrimSpace(typed), nil
}

// Approver là git config user.name. Tên "auto" (mọi kiểu chữ) dành cho bản tự duyệt.
func (c *Context) Approver(root string) (string, error) {
	name, err := c.Git(root).UserName(c.std)
	if err != nil {
		name = ""
	}
	if strings.TrimSpace(name) == "" {
		return "", abort(2, "Chưa cấu hình `git config user.name`; cần tên người duyệt.")
	}
	if strings.EqualFold(strings.TrimSpace(name), gate.ApprovalAuto) {
		return "", abort(2, "Tên người duyệt \""+strings.TrimSpace(name)+"\" (git config user.name) dành cho bản"+
			" tự duyệt; đặt tên thật của dev bằng `git config user.name \"<tên>\"` rồi chạy lại.")
	}
	return name, nil
}

// Feature là một tính năng trong repo cùng các service của nó.
type Feature struct {
	Root    string
	Config  model.RepoConfig
	Key     model.FeatureKey
	Paths   model.FeaturePaths
	Meta    model.FeatureMeta
	Mode    gate.ApprovalMode // LOCAL (gates.json) hay WEB (cache phong bì ký)
	Store   gate.GateStore
	Gates   *gate.Service
	Adapter lang.Adapter
	// Control là điều khiển từ web (tạm dừng, chỉ dẫn, lệnh chờ) đọc từ cache đã ký; nil nếu không có.
	Control *snapshot.FeatureControl
	// Web đọc cấu hình web, khóa ghim và các cache đã ký của repo (không gọi mạng).
	Web *WebCache
	ctx *Context
}

// Flow là luồng của tính năng (mặc định FULL).
func (f *Feature) Flow() model.Flow {
	if f.Meta.Flow == "" {
		return model.FlowFull
	}
	return f.Meta.Flow
}

// Git mở gitx.Repo của repo.
func (f *Feature) Git() *gitx.Repo { return f.ctx.Git(f.Root) }

// Design đọc design.yaml (chỉ luồng FULL); false khi không có hoặc không đọc được.
func (f *Feature) Design() (spec.DesignSpec, bool) {
	if f.Flow() != model.FlowFull || !exists(f.Paths.DesignYaml()) {
		return spec.DesignSpec{}, false
	}
	d, err := spec.ReadDesign(f.Paths.DesignYaml())
	if err != nil {
		return spec.DesignSpec{}, false
	}
	return d, true
}

// Cases đọc cases.yaml; false khi không có hoặc không đọc được.
func (f *Feature) Cases() (spec.CasesSpec, bool) {
	if !exists(f.Paths.Cases()) {
		return spec.CasesSpec{}, false
	}
	c, err := spec.ReadCases(f.Paths.Cases())
	if err != nil {
		return spec.CasesSpec{}, false
	}
	return c, true
}

// Specs là các bước hiệu lực; rỗng nếu design.yaml/cases.yaml chưa đọc được (để không chặn việc sửa chúng).
func (f *Feature) Specs() []spec.StepSpec {
	if f.Flow() == model.FlowLite {
		c, ok := f.Cases()
		if !ok {
			return nil
		}
		return spec.EffectiveSteps(model.FlowLite, nil, c)
	}
	d, ok := f.Design()
	if !ok {
		return nil
	}
	return spec.EffectiveSteps(model.FlowFull, &d, spec.CasesSpec{})
}

// ScopeExtras là file được dev cho phép thêm (scope-extra.json).
func (f *Feature) ScopeExtras() *step.ScopeExtras { return step.NewScopeExtras(f.Paths) }

// Steps là dịch vụ bước, đã gắn cổng (WithGates).
func (f *Feature) Steps() *step.Service {
	return step.NewService(f.Paths, f.Git(), f.Specs(), f.Config, f.ctx.now).WithGates(f.Gates)
}

// PendingActions là lệnh chờ trong cache điều khiển chưa xử lý trên máy này (theo sổ cục bộ).
func (f *Feature) PendingActions() []snapshot.Action {
	if f.Control == nil {
		return nil
	}
	_, done := readLedger(f.Root, f.Key)
	var out []snapshot.Action
	for _, a := range f.Control.Actions {
		if !done[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// UndeliveredInstructions là chỉ dẫn trong cache điều khiển chưa giao cho agent trên máy này.
func (f *Feature) UndeliveredInstructions() []snapshot.Instruction {
	if f.Control == nil {
		return nil
	}
	delivered, _ := readLedger(f.Root, f.Key)
	var out []snapshot.Instruction
	for _, i := range f.Control.Instructions {
		if !delivered[i.ID] {
			out = append(out, i)
		}
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// jsonMarshal mã hóa JSON gọn, không thoát <, >, & (như Jackson).
func jsonMarshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
