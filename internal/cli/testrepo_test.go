package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/snapshot"
)

// validBRD là brd.md hợp lệ (TestRepo.VALID_BRD của Java).
const validBRD = `# BRD

## Mục tiêu
Giới hạn chuyển tiền theo ngày.

## Trong phạm vi
- API kiểm tra hạn mức.

## Ngoài phạm vi
- Giao diện.

## Quy tắc nghiệp vụ
- Hạn mức 50.000.000 VND/ngày.

## Tiêu chí chấp nhận
- Vượt hạn mức trả 422.

## Câu hỏi mở
- [x] Có tính giao dịch bị hủy không? → không
`

// testNow là đồng hồ cố định của mọi test CLI (như Clock.fixed của Java).
var testNow = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

// fakeConsole là DevConsole giả: trả chuỗi cố định; nil như khi không có console thật.
type fakeConsole struct {
	typed   *string
	prompts []string
}

func (c *fakeConsole) ReadLine(prompt string) (string, bool) {
	c.prompts = append(c.prompts, prompt)
	if c.typed == nil {
		return "", false
	}
	return *c.typed, true
}

// typed là console có dev gõ s.
func typed(s string) *fakeConsole { return &fakeConsole{typed: &s} }

// noConsole như agent, pipe hoặc CI: không có terminal.
func noConsole() *fakeConsole { return &fakeConsole{} }

// fakeRunner thay harness: không chạy gì, ghi harness.log, ghi schema.json cho migration-check và trả kết quả case
// cấu hình được (FakeMaven của Java).
type fakeRunner struct {
	root          string
	calls         []string
	migrationExit int
	writeSchema   bool
	log           string
	cases         func(steps []string) []run.CaseResult
	err           error  // khác nil: RunCases trả lỗi này
	onRun         func() // gọi đầu RunCases (ví dụ giả Ctrl-C giữa lúc harness chạy)
}

func newFakeRunner(root string) *fakeRunner {
	return &fakeRunner{root: root, writeSchema: true,
		cases: func([]string) []run.CaseResult { return nil }}
}

func (f *fakeRunner) OutDir(key model.FeatureKey) string {
	return filepath.Join(f.root, ".aisdlc", "run", key.String())
}

func (f *fakeRunner) OutputTail(key model.FeatureKey, n int) string {
	return tailFile(filepath.Join(f.OutDir(key), HarnessLog), n)
}

func (f *fakeRunner) RunCases(key model.FeatureKey, steps []string, mode string) (run.CaseRunResult, error) {
	f.calls = append(f.calls, mode+":"+strings.Join(steps, ","))
	if f.onRun != nil {
		f.onRun()
	}
	if f.err != nil {
		return run.CaseRunResult{}, f.err
	}
	out := f.OutDir(key)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return run.CaseRunResult{}, err
	}
	if err := os.WriteFile(filepath.Join(out, HarnessLog), []byte(f.log), 0o644); err != nil {
		return run.CaseRunResult{}, err
	}
	if mode == "migration-check" {
		if f.writeSchema {
			s := run.NewSchemaSnapshot(map[string][]string{"transfer_limit_usage": {"used_amount:BIGINT:NOT NULL"}})
			if err := s.Write(filepath.Join(out, "schema.json")); err != nil {
				return run.CaseRunResult{}, err
			}
		}
		return run.CaseRunResult{ExitCode: f.migrationExit}, nil
	}
	r := f.cases(steps)
	code := 0
	for _, c := range r {
		if c.Outcome != run.CaseOutcomePassed {
			code = 1
		}
	}
	return run.CaseRunResult{ExitCode: code, Cases: r}, nil
}

// caseResult là CaseResult với thông điệp lỗi chuẩn khi không pass (TestRepo.c của Java).
func caseResult(id, step string, o run.CaseOutcome) run.CaseResult {
	msg := ""
	if o != run.CaseOutcomePassed {
		msg = "mong đợi 200, thực tế 404"
	}
	return run.CaseResult{ID: id, Step: step, Outcome: o, Message: msg}
}

// testRepo là repo dịch vụ Go giả trong thư mục tạm; chạy CLI trong tiến trình qua runWith (Main.run của Java).
type testRepo struct {
	t      *testing.T
	root   string
	env    map[string]string
	runner *fakeRunner
	// verifier thay harness của `aisdlc eval`; dùng chung kết quả case với runner.
	verifier *fakeVerifier
	// setupDone: setup đã chạy (và commit) trước, setupInit chỉ còn init.
	setupDone bool
	// presetFlow false (mặc định): sau setup bỏ khối flow khỏi risk-policy.yaml để mọi cổng duyệt tay.
	presetFlow bool
	// llmLevel là mức trong risk-agent.json mà testRepo tự ghi (giả subagent) trước `submit design`, `submit cases`
	// và `risk reclassify` khi file thiếu hoặc đã cũ; "" là mức mặc định của chính sách.
	llmLevel string
	// noAutoLLM: không tự ghi risk-agent.json (test về chính file đó).
	noAutoLLM bool
	// setupDefaultApprovals false (mặc định): setup không nêu --approvals thì thêm --approvals local.
	setupDefaultApprovals bool
	// git thay gitx.Repo của Context (nil = git thật).
	git func(root string) *gitx.Repo
	// extra là lệnh chỉ có trong test, thêm vào cây lệnh (vd. lệnh thăm dò Feature()).
	extra []func(ctx *Context) *cobra.Command
	// interrupts: nguồn tín hiệu giả (nil: không bắt tín hiệu); std: ctx của lệnh (nil: Background)
	interrupts *Interrupts
	std        context.Context
	// runnerStd là ctx.Std() lúc lệnh lấy runner gần nhất (ctx mà harness thật sẽ dùng)
	runnerStd context.Context
	now       time.Time
	out       string
	err       string
	live      *bytes.Buffer
	// spawned: repo mà lệnh/hook đã mở `aisdlc web flush` nền (test không mở tiến trình thật)
	spawned []string
	// spawnedFeatures: tính năng truyền kèm (`--feature`) mỗi lần mở, cùng thứ tự với spawned
	spawnedFeatures []model.FeatureKey
}

// hermeticGit cô lập git khỏi cấu hình của máy.
func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("HOME", t.TempDir())
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	hermeticGit(t)
	root := t.TempDir()
	r := &testRepo{t: t, root: root, env: map[string]string{}, now: testNow}
	r.gitCmd("init", "-q", "-b", "main")
	r.gitCmd("config", "user.name", "Dev Một")
	r.gitCmd("config", "user.email", "dev@example.com")
	r.write("go.mod", "module example.com/pay\n\ngo 1.26\n")
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "init")
	r.runner = newFakeRunner(root)
	r.verifier = &fakeVerifier{runner: r.runner}
	return r
}

// context dựng Context như khi chạy lệnh; out/err ghi vào o/e.
func (r *testRepo) context(console DevConsole, stdin string, o, e *bytes.Buffer) *Context {
	ctx := NewContext(strings.NewReader(stdin), o, e, console, r.root).
		WithEnv(r.env).
		WithNow(func() time.Time { return r.now }).
		WithVerifier(func(string, model.RepoConfig) eval.Verifier { return r.verifier })
	ctx.WithRunner(func(string, model.RepoConfig) CaseRunner { r.runnerStd = ctx.Std(); return r.runner })
	ctx.WithFlushSpawner(func(root string, feature model.FeatureKey) error {
		r.spawned = append(r.spawned, root)
		r.spawnedFeatures = append(r.spawnedFeatures, feature)
		return nil
	})
	if r.interrupts != nil {
		ctx.WithInterrupts(r.interrupts)
	}
	if r.std != nil {
		ctx.WithStd(r.std)
	}
	if r.git != nil {
		ctx.WithGit(r.git)
	}
	return ctx
}

func (r *testRepo) run(args ...string) int { return r.exec(noConsole(), "", args...) }

func (r *testRepo) runC(console DevConsole, args ...string) int { return r.exec(console, "", args...) }

func (r *testRepo) runWithStdin(stdin string, args ...string) int {
	return r.exec(noConsole(), stdin, args...)
}

// currentOut là stdout của lệnh đang chạy (console giả xem được những gì đã in trước lời nhắc).
func (r *testRepo) currentOut() string { return r.live.String() }

func (r *testRepo) exec(console DevConsole, stdin string, args ...string) int {
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	r.live = o
	ctx := r.context(console, stdin, o, e)
	if !r.setupDefaultApprovals && len(args) > 0 && args[0] == "setup" &&
		!slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--approvals") }) {
		args = append(append([]string(nil), args...), "--approvals", "local")
	}
	if !r.noAutoLLM && classifies(args) {
		r.autoLLM()
	}
	code := runWith(ctx, r.extra, args...)
	if code == 0 && len(args) > 0 && args[0] == "setup" {
		if !r.presetFlow {
			r.dropFlow()
		}
	}
	r.out, r.err = o.String(), e.String()
	return code
}

var flowBlock = regexp.MustCompile(`(?m)^flow:\n(?:[ \t#].*(?:\n|\z))*`)

// dropFlow bỏ khối flow: (đến hết file hoặc khóa gốc kế tiếp) khỏi aisdlc/risk-policy.yaml.
func (r *testRepo) dropFlow() {
	f := filepath.Join(r.root, "aisdlc/risk-policy.yaml")
	b, err := os.ReadFile(f)
	if err != nil {
		return
	}
	if err := os.WriteFile(f, flowBlock.ReplaceAll(b, nil), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) write(rel, content string) {
	r.t.Helper()
	f := filepath.Join(r.root, rel)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) read(rel string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.root, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *testRepo) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(r.root, rel))
	return err == nil
}

func (r *testRepo) gitCmd(args ...string) {
	r.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = r.root
	if out, err := c.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ok chạy lệnh và đòi exit 0.
func (r *testRepo) ok(console DevConsole, args ...string) {
	r.t.Helper()
	if code := r.runC(console, args...); code != 0 {
		r.t.Fatalf("%s: exit %d\n%s%s", strings.Join(args, " "), code, r.out, r.err)
	}
}

// setupInit là setup + init PAY-1 (TestRepo.t() của Java).
func (r *testRepo) setupInit() {
	r.t.Helper()
	if !r.setupDone {
		r.ok(noConsole(), "setup")
	}
	r.ok(noConsole(), "init", "PAY-1")
}

// pinKey ghim khóa công khai của web dưới home (như `aisdlc web login`), trả cặp khóa để ký cache.
func pinKey(t *testing.T, home string) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := PinnedKeyFile(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(snapshot.EncodePublicKey(pub)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// writeSigned ghi phong bì ký payload vào rel.
func (r *testRepo) writeSigned(rel string, payload []byte, priv ed25519.PrivateKey) {
	r.t.Helper()
	b, err := jsonMarshal(snapshot.Sign(payload, priv, "k"))
	if err != nil {
		r.t.Fatal(err)
	}
	r.write(rel, string(b))
}

// must dừng test khi err khác nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, x := range subs {
		if !strings.Contains(s, x) {
			t.Fatalf("thiếu %q trong:\n%s", x, s)
		}
	}
}

func notContains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, x := range subs {
		if strings.Contains(s, x) {
			t.Fatalf("không được có %q trong:\n%s", x, s)
		}
	}
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
