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

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
	"aisdlc/internal/policy"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
	"aisdlc/internal/webclient"
)

// Abort dừng lệnh với mã thoát và thông báo cho dev (in nguyên văn ra stderr), như Context.Abort của Java.
type Abort struct {
	Code int
	Msg  string
}

func (a *Abort) Error() string { return a.Msg }

// noConsoleMsg: lệnh chỉ dev chạy mà không có console thật.
func noConsoleMsg() string {
	return i18n.T("Lệnh này chỉ dev được chạy, trong terminal thật (không tìm thấy console). " +
		"Agent không được chạy lệnh này.")
}

// noGrantMsg: lệnh chỉ dev chạy trong Claude Code mà không có phiếu xác nhận còn hạn.
func noGrantMsg() string {
	return i18n.T("Lệnh này chỉ dev được chạy. Không có xác nhận trong Claude Code (hoặc đã hết" +
		" hạn). Chạy lại lệnh duyệt một mình, không kèm lệnh khác.")
}

// abort tạo *Abort dưới dạng error.
func abort(code int, msg string) error { return &Abort{Code: code, Msg: msg} }

// asAbort như errors.As cho *Abort.
func asAbort(err error, a **Abort) bool { return errors.As(err, a) }

const (
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
	// interrupts: nguồn Ctrl-C/SIGTERM khi harness chạy (nil: không bắt tín hiệu, như test và hook)
	interrupts *Interrupts

	color           bool
	now             func() time.Time
	env             map[string]string
	git             func(root string) *gitx.Repo
	runner          func(root string, cfg model.RepoConfig) CaseRunner
	verifier        func(root string, cfg model.RepoConfig) eval.Verifier
	network         bool
	loginWarned     bool
	sessionID       string
	featureOverride model.FeatureKey
	args            []string
	banner          bool
	bannerPrinted   bool
	exit            int
	// spawnFlush mở `aisdlc web flush` nền (nil: webclient.SpawnFlush); publishers: bộ gửi web theo repo
	spawnFlush func(root string, feature model.FeatureKey) error
	// flushFeature: tính năng (đã phân giải theo phiên) truyền cho `web flush --feature` (hook đặt trước khi mở)
	flushFeature model.FeatureKey
	publishers   map[string]*webclient.Publisher
	// webDown: đã có một lần không với tới web trong tiến trình này (lệnh sau không thử lại)
	webDown bool
	// controlRefreshed: kết quả làm mới điều khiển từ web theo tính năng trong tiến trình này (mỗi tính năng một lần)
	controlRefreshed map[model.FeatureKey]bool
}

// NewContext tạo Context với mặc định của tiến trình: env của OS, đồng hồ UTC, git thật, runner và verifier là
// harness Go (dùng ctx và đồng hồ hiện hành của Context lúc gọi).
func NewContext(in io.Reader, out, errw io.Writer, console DevConsole, cwd string) *Context {
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	env := osEnv()
	c := &Context{
		in: in, out: out, err: errw, console: console, cwd: filepath.Clean(cwd), std: context.Background(),
		color:   colorDefault(isTerminal(out), env),
		now:     func() time.Time { return time.Now().UTC() },
		env:     env,
		git:     gitx.New,
		network: true,
		banner:  true,
	}
	c.runner = func(root string, cfg model.RepoConfig) CaseRunner { return newHarnessRunner(c.std, root, cfg) }
	c.verifier = func(root string, cfg model.RepoConfig) eval.Verifier { return newHarnessVerifier(root, cfg) }
	return c
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

// WithVerifier thay eval.Verifier của `aisdlc eval`.
func (c *Context) WithVerifier(f func(root string, cfg model.RepoConfig) eval.Verifier) *Context {
	c.verifier = f
	return c
}

// WithStd thay context.Context của lệnh (Main: hủy khi Ctrl-C/SIGTERM); harness và git dùng nó.
func (c *Context) WithStd(std context.Context) *Context { c.std = std; return c }

// WithInterrupts đặt nguồn tín hiệu dùng khi harness/eval chạy (Main: OSInterrupts).
func (c *Context) WithInterrupts(in *Interrupts) *Context { c.interrupts = in; return c }

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

// truncSession cắt session_id còn tối đa maxSessionID đơn vị UTF-16 (luật của server); trống = không có.
func truncSession(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	// web nhận sessionId tối đa 100 đơn vị UTF-16 (như String.length() của Java): cắt theo đơn vị UTF-16, không
	// cắt giữa một ký tự
	return prefixUTF16(id, maxSessionID)
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

// Std là context.Context của lệnh (git, harness, eval); bị hủy khi dev bấm Ctrl-C.
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

// envValue như Env nhưng trả "" khi không đặt (cho webclient.LoadConfig).
func (c *Context) envValue(name string) string {
	v, _ := c.Env(name)
	return v
}

// Home là HOME của dev (chỉ theo env của Context, để test thay được).
func (c *Context) Home() (string, bool) { return c.Env("HOME") }

// Git mở gitx.Repo cho root.
func (c *Context) Git(root string) *gitx.Repo { return c.git(root) }

// Runner trả CaseRunner cho repo.
func (c *Context) Runner(root string, cfg model.RepoConfig) CaseRunner { return c.runner(root, cfg) }

// Verifier trả eval.Verifier cho repo.
func (c *Context) Verifier(root string, cfg model.RepoConfig) eval.Verifier {
	return c.verifier(root, cfg)
}

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
	return "", abort(2, i18n.T("Không tìm thấy aisdlc/config.yaml ở thư mục này hoặc thư mục cha. Chạy `aisdlc setup` ở gốc repo trước."))
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
		return nil, abort(2, i18n.T("Chưa có tính năng đang làm. Chạy `aisdlc init <KEY>` hoặc `aisdlc use <KEY>`."))
	}
	paths := model.FeaturePaths{RepoRoot: root, Key: key}
	if !exists(paths.Meta()) {
		// .aisdlc/active và gắn kết session không theo git: sau khi đổi nhánh có thể trỏ tới tính năng không có ở đây
		return nil, abort(2, i18n.Tf("Tính năng đang làm %s không có trên nhánh này (thiếu %s). Chạy `aisdlc use <KEY>` (tính năng đã có) hoặc `aisdlc init <KEY>` (tính năng mới).", key.String(), paths.Rel(paths.Meta())))
	}
	c.FeatureBanner(key)
	return c.FeatureAt(root, key)
}

func (c *Context) choose(open []model.FeatureKey) (model.FeatureKey, error) {
	names := make([]string, len(open))
	var prompt strings.Builder
	prompt.WriteString(i18n.T("Có nhiều tính năng đang mở:\n"))
	for i, k := range open {
		names[i] = k.String()
		fmt.Fprintf(&prompt, "  %d) %s\n", i+1, k)
	}
	prompt.WriteString(i18n.T("Chọn số (hoặc dùng --feature <KEY>): "))
	list := strings.Join(names, ", ")
	typed, ok := c.console.ReadLine(prompt.String())
	if !ok {
		return "", abort(2, i18n.Tf("Có nhiều tính năng đang mở: %s. Ghi rõ --feature <KEY>.", list))
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
	return "", abort(2, i18n.Tf("Lựa chọn không hợp lệ: \"%s\" (các tính năng đang mở: %s).", t, list))
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
		return nil, abort(2, i18n.Tf("Không có tính năng %s (thiếu %s).", key.String(), paths.Rel(paths.Meta())))
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
	// lệnh CLI hỏi web chế độ duyệt khi chưa có cache hợp lệ; hook (Offline) chỉ đọc cache
	if c.network {
		f.Mode = f.Web.Mode(cfg)
	} else {
		f.Mode = f.Web.CachedMode(cfg)
	}
	if f.Mode == gate.ModeWeb {
		f.Store = f.Web.Store(key)
	} else {
		f.Store = gate.NewLocalGateStore(paths)
	}
	f.Gates = gate.NewService(paths, f.Flow(), cfg, f.Store, c.now)
	if c.network && f.Mode == gate.ModeWeb {
		if !f.Web.LoggedIn() {
			if !c.loginWarned {
				c.Eprintln(i18n.T("Chưa `aisdlc web login` trên máy này: repo duyệt trên web, mọi cổng coi như chưa duyệt."))
			}
			c.loginWarned = true
		} else if _, err := refreshGates(c, f); err != nil {
			return nil, err
		}
	}
	f.WebControl = NewWebControl(f.Web)
	f.applyControl()
	f.Control = f.WebControl.Cached(key)
	if f.snapshotPending {
		// vừa áp dụng lệnh từ web: web thấy trạng thái bước mới ở lần gửi kế tiếp (hook: không in gì)
		if p := c.Publisher(root); p.Enabled() {
			p.Stage(key.String(), func() ([]byte, error) { return c.featureSnapshot(p, f) }, !c.network)
		}
	}
	return f, nil
}

// GateOf đọc tên cổng (không phân biệt hoa thường).
func GateOf(name string) (gate.Gate, error) {
	g := gate.Gate(strings.ToUpper(name))
	switch g {
	case gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW:
		return g, nil
	}
	return "", abort(2, i18n.Tf("Cổng không hợp lệ: %s (brd, design, cases, review).", name))
}

// ConfirmDev xác nhận lệnh chỉ dev. Có console thật: bắt gõ lại đúng feature key (kênh terminal, trả ""). Không có
// console: chỉ khi sessionApprove bật ở baseRef, tiêu thụ phiếu cho phép một lần mà hook ghi khi dev bấm Yes trong
// Claude Code — khớp session, tính năng, lệnh (action) và gateHash ("" với lệnh không gắn cổng) — rồi trả
// gate.ApprovalClaudeCode. action rỗng (Verb == ""): lệnh không có phiếu, chỉ console thật. Không thì exit 3.
func (c *Context) ConfirmDev(key model.FeatureKey, action policy.DevCommand, gateHash string) (string, error) {
	if typed, ok := c.console.ReadLine(i18n.Tf("Gõ lại feature key (%s) để xác nhận: ", key.String())); ok {
		if t := strings.TrimSpace(typed); t != key.String() {
			return "", abort(3, i18n.Tf("Key không khớp (%s ≠ %s); đã hủy.", t, key.String()))
		}
		return "", nil
	}
	root, err := c.RepoRoot()
	if err != nil {
		return "", err
	}
	if action.Verb == "" {
		return "", abort(3, noConsoleMsg())
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return "", err
	}
	if !model.ReadSessionApprove(c.std, c.Git(root), cfg).Enabled {
		return "", abort(3, noConsoleMsg())
	}
	session, ok := c.ResolveSession()
	if !ok {
		return "", abort(3, noGrantMsg())
	}
	granted, err := policy.ConsumeGrant(root, session, key.String(), action.GrantKey(), gateHash, c.now())
	if err != nil || !granted { // như DevGrants.consume: mọi lỗi là không có phiếu
		return "", abort(3, noGrantMsg())
	}
	return gate.ApprovalClaudeCode, nil
}

// AskDev hỏi dev qua console thật; không có terminal (agent, pipe, CI) thì exit 3.
func (c *Context) AskDev(prompt string) (string, error) {
	typed, ok := c.console.ReadLine(prompt)
	if !ok {
		return "", abort(3, noConsoleMsg())
	}
	return strings.TrimSpace(typed), nil
}

// AskDevSecret hỏi dev một giá trị bí mật (token) qua console thật, không hiện chữ; không có terminal thì exit 3.
func (c *Context) AskDevSecret(prompt string) (string, error) {
	sc, ok := c.console.(SecretConsole)
	if !ok {
		return "", abort(3, noConsoleMsg())
	}
	typed, ok := sc.ReadSecret(prompt)
	if !ok {
		return "", abort(3, noConsoleMsg())
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
		return "", abort(2, i18n.T("Chưa cấu hình `git config user.name`; cần tên người duyệt."))
	}
	if strings.EqualFold(strings.TrimSpace(name), gate.ApprovalAuto) {
		return "", abort(2, i18n.Tf("Tên người duyệt \"%s\" (git config user.name) dành cho bản tự duyệt; đặt tên thật của dev bằng `git config user.name \"<tên>\"` rồi chạy lại.", strings.TrimSpace(name)))
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
	// Control là điều khiển từ web (tạm dừng, chỉ dẫn, lệnh chờ) đọc từ cache đã ký sau khi áp lệnh chờ; nil nếu
	// không có.
	Control *snapshot.FeatureControl
	// WebControl làm mới và đọc điều khiển từ web của repo.
	WebControl *WebControl
	// ControlOffline: lệnh CLI đã thử làm mới điều khiển từ web và thất bại vì web không với tới được.
	ControlOffline bool
	// ControlBusy (hook): không lấy được control.lock trong thời hạn nên lệnh chờ chưa xét (PreToolUse ghi thì chặn).
	ControlBusy bool
	// snapshotPending: đã áp dụng lệnh từ web khi dựng; FeatureAt đặt snapshot vào outbox.
	snapshotPending bool
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
	done := NewControlLedger(f.Root, f.Key, 0).Done()
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
	return f.WebControl.Undelivered(f.Key)
}

// applyControl: lệnh CLI có mạng làm mới điều khiển trước (mỗi tính năng một lần mỗi tiến trình); rồi áp dụng lệnh
// chờ trong cache dưới khóa sổ cục bộ, mỗi kết quả ghi sổ và một event ACTION_RESULT. Lệnh CLI in kết quả ra stderr;
// hook không in gì. Điều khiển từ web không bao giờ làm hỏng lệnh hay hook: lỗi bỏ qua, trừ hết hạn chờ khóa
// (ControlBusy).
func (f *Feature) applyControl() {
	defer func() { _ = recover() }()
	c := f.ctx
	if !c.network && !exists(snapshot.ControlCacheFile(f.Root, f.Key)) {
		return // hook: không có cache thì khỏi đọc khóa ghim
	}
	if !f.Web.LoggedIn() {
		return
	}
	if c.network {
		ok, done := c.controlRefreshed[f.Key]
		if !done {
			ok = f.WebControl.Refresh(f.Key)
			if c.controlRefreshed == nil {
				c.controlRefreshed = map[model.FeatureKey]bool{}
			}
			c.controlRefreshed[f.Key] = ok
		}
		f.ControlOffline = !ok && f.WebControl.Unreachable()
	}
	fc := f.WebControl.Cached(f.Key)
	if fc == nil || len(fc.Actions) == 0 {
		return
	}
	results, err := NewControlLedger(f.Root, f.Key, c.lockBudget()).Apply(func(done map[int64]bool) []step.PendingResult {
		return step.ApplyPending(*fc, f.Steps(), f.ScopeExtras(), done, c.now())
	})
	if err != nil {
		f.ControlBusy = isControlBusy(err)
		return
	}
	if len(results) == 0 {
		return
	}
	p := c.Publisher(f.Root)
	sender := ""
	if p.Enabled() {
		sender = webclient.Sender(f.Git())
	}
	for _, r := range results {
		var a *snapshot.Action
		for i := range fc.Actions {
			if fc.Actions[i].ID == r.ID {
				a = &fc.Actions[i]
				break
			}
		}
		e := webclient.Event{Type: "ACTION_RESULT", At: c.now(), Feature: f.Key.String(), Sender: sender,
			Detail: resultJSON(r), SessionID: c.SessionID()}
		if a != nil {
			e.Step, e.Path = a.Step, a.Path
		}
		p.Emit(e)
		if r.Status == step.PendingApplied {
			f.snapshotPending = true
			if c.network && a != nil {
				what := i18n.Tf("đã accept %s", a.Step)
				if a.Kind == "ALLOW_SCOPE" {
					what = i18n.Tf("đã cho phép %s ở %s", a.Path, a.Step)
				}
				c.Eprintln(i18n.Tf("Áp dụng từ web: %s (%s)", what, a.By))
			}
		} else if c.network {
			c.Eprintln(i18n.Tf("Lệnh từ web hết hiệu lực: %s", r.Reason))
		}
	}
}

// resultJSON là detail của event ACTION_RESULT: {"id":..,"status":..,"reason":..|null}.
func resultJSON(r step.PendingResult) string {
	var reason *string
	if r.Reason != "" {
		reason = &r.Reason
	}
	b, err := jsonMarshal(struct {
		ID     int64   `json:"id"`
		Status string  `json:"status"`
		Reason *string `json:"reason"`
	}{r.ID, r.Status, reason})
	if err != nil {
		panic(err)
	}
	return string(b)
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
