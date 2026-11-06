package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/cli"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

var binPath string // binary đã build một lần cho cả test binary

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aisdlc-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "aisdlc")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// hermeticEnv cô lập git và HOME khỏi máy.
func hermeticEnv(home string) []string {
	return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+home)
}

type result struct {
	code        int
	out, errOut string
}

func runCmd(t *testing.T, dir, stdin string, name string, args ...string) result {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = hermeticEnv(t.TempDir())
	c.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	err := c.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s: %v", name, err)
		}
		code = ee.ExitCode()
	}
	return result{code, o.String(), e.String()}
}

func plainDir(t *testing.T) string { return t.TempDir() }

// aisdlcMarker là thư mục giả có aisdlc/config.yaml (đủ để hook coi là repo AISDLC).
func aisdlcMarker(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "aisdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "aisdlc", "config.yaml"), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

const codePath = "src/main/java/com/example/limit/web/A.java"

func editJSON(cwd, file string) string {
	b, _ := json.Marshal(map[string]any{"session_id": "abc", "hook_event_name": "PreToolUse", "cwd": cwd,
		"tool_name": "Edit", "tool_input": map[string]any{"file_path": file}})
	return string(b)
}

func TestBinaryVersion(t *testing.T) {
	for _, flag := range []string{"-V", "--version"} {
		r := runCmd(t, plainDir(t), "", binPath, flag)
		if r.code != 0 || strings.TrimSpace(r.out) != "aisdlc 0.1.0-SNAPSHOT" {
			t.Fatalf("%s: code=%d out=%q err=%q", flag, r.code, r.out, r.errOut)
		}
	}
}

func TestBinaryHookOutsideRepoSilent(t *testing.T) {
	d := plainDir(t)
	r := runCmd(t, d, editJSON(d, filepath.Join(d, "a.go")), binPath, "hook", "pre-tool-use")
	if r.code != 0 || r.out != "" || r.errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
}

func TestBinaryHookDeniesLockedCases(t *testing.T) {
	d := aisdlcMarker(t)
	// repo AISDLC chưa có tính năng đang làm: ghi bị chặn bằng exit 0 + JSON deny (như HookCommand của Java)
	r := runCmd(t, d, editJSON(d, filepath.Join(d, "a.go")), binPath, "hook", "pre-tool-use")
	if r.code != 0 {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
	var o struct {
		H struct {
			Decision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.out), &o); err != nil || o.H.Decision != "deny" {
		t.Fatalf("không phải deny: %q (%v)", r.out, err)
	}
	// JSON hỏng trong repo AISDLC cũng bị chặn
	r = runCmd(t, d, `{"tool_name":"Write",`, binPath, "hook", "pre-tool-use")
	if r.code != 0 || !strings.Contains(r.out, `"permissionDecision":"deny"`) {
		t.Fatalf("JSON hỏng: code=%d out=%q", r.code, r.out)
	}
}

func platform() string {
	return "aisdlc-" + runtime.GOOS + "-" + runtime.GOARCH
}

func launcher(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "plugin", "bin", "aisdlc"))
	if err != nil {
		t.Fatal(err)
	}
	plug := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plug, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(plug, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := filepath.Join(plug, "bin", "aisdlc")
	if err := os.WriteFile(l, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLauncherMissingBinaryFailClosed(t *testing.T) {
	l := launcher(t)
	d := aisdlcMarker(t)
	r := runCmd(t, d, "{}", l, "hook", "pre-tool-use")
	if r.code != 2 || !strings.Contains(r.errOut, "không tìm thấy binary aisdlc cho") {
		t.Fatalf("pre-tool-use: code=%d err=%q", r.code, r.errOut)
	}
	if r := runCmd(t, d, "{}", l, "hook", "stop"); r.code != 1 {
		t.Fatalf("stop: code=%d err=%q", r.code, r.errOut)
	}
	if r := runCmd(t, plainDir(t), "{}", l, "hook", "pre-tool-use"); r.code != 0 || r.errOut != "" {
		t.Fatalf("ngoài repo: code=%d err=%q", r.code, r.errOut)
	}
	r = runCmd(t, plainDir(t), "", l, "status")
	if r.code != 2 || !strings.Contains(r.errOut, "Chạy plugin/install.sh.") {
		t.Fatalf("lệnh thường: code=%d err=%q", r.code, r.errOut)
	}
}

func TestLauncherExecsPlatformBinary(t *testing.T) {
	l := launcher(t)
	b, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(l), "..", "lib", platform()), b, 0o755); err != nil {
		t.Fatal(err)
	}
	r := runCmd(t, plainDir(t), "", l, "--version")
	if r.code != 0 || strings.TrimSpace(r.out) != "aisdlc 0.1.0-SNAPSHOT" {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
}

type typedConsole string

func (c typedConsole) ReadLine(string) (string, bool) { return string(c), true }

type noTerm struct{}

func (noTerm) ReadLine(string) (string, bool) { return "", false }

type fakeRunner struct{ root string }

func (f fakeRunner) OutDir(k model.FeatureKey) string {
	return filepath.Join(f.root, ".aisdlc", "run", k.String())
}
func (f fakeRunner) OutputTail(model.FeatureKey, int) string { return "" }
func (f fakeRunner) RunCases(k model.FeatureKey, _ []string, mode string) (run.CaseRunResult, error) {
	out := f.OutDir(k)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return run.CaseRunResult{}, err
	}
	if mode == "migration-check" {
		s := run.NewSchemaSnapshot(map[string][]string{"transfer_limit_usage": {"used_amount:BIGINT:NOT NULL"}})
		return run.CaseRunResult{}, s.Write(filepath.Join(out, "schema.json"))
	}
	fail := func(id, step string) run.CaseResult {
		return run.CaseResult{ID: id, Step: step, Outcome: run.CaseOutcomeFailed, Message: "mong đợi 200, thực tế 404"}
	}
	return run.CaseRunResult{ExitCode: 1, Cases: []run.CaseResult{fail("C1", "S1"), fail("C2", "S2")}}, nil
}

const validBRD = "# BRD\n\n## Mục tiêu\nGiới hạn chuyển tiền theo ngày.\n\n## Trong phạm vi\n- API kiểm tra hạn mức.\n\n" +
	"## Ngoài phạm vi\n- Giao diện.\n\n## Quy tắc nghiệp vụ\n- Hạn mức 50.000.000 VND/ngày.\n\n" +
	"## Tiêu chí chấp nhận\n- Vượt hạn mức trả 422.\n\n## Câu hỏi mở\n- [x] Có tính giao dịch bị hủy không? → không\n"

// stepOneRepo dựng repo git có PAY-1 đã duyệt tới cổng case, S1 đang chạy (dựng trong tiến trình bằng cli.Run).
func stepOneRepo(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "HOME": t.TempDir()} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Dev")
	git("config", "user.email", "dev@example.com")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/pay\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	step := func(console cli.DevConsole, args ...string) {
		t.Helper()
		var o, e bytes.Buffer
		ctx := cli.NewContext(strings.NewReader(""), &o, &e, console, root).
			WithEnv(map[string]string{"HOME": t.TempDir()}).
			WithRunner(func(string, model.RepoConfig) cli.CaseRunner { return fakeRunner{root} })
		if code := cli.Run(ctx, args...); code != 0 {
			t.Fatalf("%v: exit %d\n%s%s", args, code, o.String(), e.String())
		}
	}
	step(noTerm{}, "setup", "--approvals", "local")
	// bỏ khối flow để mọi cổng duyệt tay
	rp := filepath.Join(root, "aisdlc", "risk-policy.yaml")
	b, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rp, regexp.MustCompile(`(?m)^flow:\n(?:[ \t#].*(?:\n|\z))*`).ReplaceAll(b, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	step(noTerm{}, "init", "PAY-1")
	if err := os.WriteFile(filepath.Join(root, "aisdlc/features/PAY-1/brd.md"), []byte(validBRD), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"brd", "design", "cases"} {
		step(noTerm{}, "submit", g)
		step(typedConsole("PAY-1"), "approve", g)
	}
	step(noTerm{}, "step", "start", "S1")
	return root
}

func TestHookLatencyP95(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	root := stepOneRepo(t)
	in := editJSON(root, filepath.Join(root, codePath))
	// một lần gọi thử: S1 đang chạy, file trong scope → cho phép (im lặng)
	if r := runCmd(t, root, in, binPath, "hook", "pre-tool-use"); r.code != 0 || r.out != "" {
		t.Fatalf("lần thử: code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
	// Máy bận (test Docker chạy song song) làm lệch số đo: đo tối đa 3 lượt, một lượt đạt là đủ.
	const n, rounds = 60, 3
	var p95 time.Duration
	for r := 1; r <= rounds; r++ {
		d := make([]time.Duration, 0, n)
		for i := 0; i < n; i++ {
			c := exec.Command(binPath, "hook", "pre-tool-use")
			c.Dir = root
			c.Env = hermeticEnv(t.TempDir())
			c.Stdin = strings.NewReader(in)
			t0 := time.Now()
			if err := c.Run(); err != nil {
				t.Fatal(err)
			}
			d = append(d, time.Since(t0))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		p95 = d[(n*95+99)/100-1]
		t.Logf("hook pre-tool-use lượt %d: p50=%v p95=%v max=%v (n=%d)", r, d[n/2], p95, d[n-1], n)
		if p95 < 50*time.Millisecond {
			return
		}
	}
	t.Fatalf("p95 %v >= 50ms sau %d lượt", p95, rounds)
}
