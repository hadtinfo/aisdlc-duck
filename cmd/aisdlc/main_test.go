package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/cli"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
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
	post := strings.Replace(in, `"PreToolUse"`, `"PostToolUse"`, 1)
	// ghi ngoài phạm vi S1: deny (JSON ra stdout) + event SCOPE_DENIED + mở `web flush` thật
	outOfScope := editJSON(root, filepath.Join(root, "README2.txt"))
	dead := []string{"AISDLC_WEB_URL=" + closedURL(t), "AISDLC_WEB_TOKEN=t0k"}
	backoff := filepath.Join(root, ".aisdlc", "web", "backoff-until")
	cases := []struct {
		name, event, in string
		vars            []string
		before          func()
	}{
		{"web tắt", "pre-tool-use", in, nil, nil},
		{"web bật nhưng chết", "pre-tool-use", in, dead, nil},
		{"pre-tool-use deny ngoài phạm vi, web bật nhưng chết", "pre-tool-use", outOfScope, dead,
			func() { _ = os.Remove(backoff) }},
		// post-tool-use ghi outbox và mở `web flush` thật; flush đầu gặp cổng đóng thì bật backoff
		{"post-tool-use web bật nhưng chết", "post-tool-use", post, dead, func() { _ = os.Remove(backoff) }},
		{"post-tool-use đang backoff", "post-tool-use", post, dead, func() {
			if err := os.MkdirAll(filepath.Dir(backoff), 0o755); err != nil {
				t.Fatal(err)
			}
			until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			if err := os.WriteFile(backoff, []byte(until), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.before != nil {
				c.before()
			}
			hookP95(t, root, c.event, c.in, c.vars)
		})
	}
	es, err := os.ReadDir(filepath.Join(root, ".aisdlc", "web", "outbox"))
	if err != nil || len(es) == 0 {
		t.Fatalf("post-tool-use không ghi outbox: %v", err)
	}
	waitFlushDone(t, root)
}

// waitFlushDone chờ (tối đa 10 giây) mọi tiến trình `web flush` nền đã mở ở repo root xong hẳn: khóa gửi rảnh và
// không còn tiến trình `aisdlc web flush` nào có thư mục làm việc là root (sau khi nhả khóa gửi, tiến trình còn làm
// mới chế độ, cổng, điều khiển và chính sách — vẫn ghi vào .aisdlc/web). Gọi trước khi thư mục repo tạm bị xóa.
func waitFlushDone(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l, ok, err := webclient.TryFlushLock(root); err == nil && ok {
			l.Close()
			if len(flushProcesses(root)) == 0 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("web flush nền chưa xong sau 10 giây (còn %v)", flushProcesses(root))
}

// flushProcesses là pid các tiến trình `<binPath> web flush` đang chạy ở thư mục root (đọc /proc; Linux).
func flushProcesses(root string) []int {
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		want = root
	}
	es, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || !strings.HasSuffix(string(cmdline), "\x00web\x00flush\x00") {
			continue
		}
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil && (cwd == want || cwd == root) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// hookP95 gọi hook 60 lần mỗi lượt, p95 phải dưới 50 ms. Máy bận (test Docker chạy song song) làm lệch số đo: đo
// tối đa 3 lượt, một lượt đạt là đủ. vars thêm vào môi trường của hook (cấu hình web). Stdout phải trống, trừ khi
// input là ghi ngoài phạm vi (README2.txt): khi đó stdout phải là JSON deny.
func hookP95(t *testing.T, root, event, in string, vars []string) {
	t.Helper()
	const n, rounds = 60, 3
	var p95 time.Duration
	for r := 1; r <= rounds; r++ {
		d := make([]time.Duration, 0, n)
		for i := 0; i < n; i++ {
			c := exec.Command(binPath, "hook", event)
			c.Dir = root
			c.Env = append(hermeticEnv(t.TempDir()), vars...)
			c.Stdin = strings.NewReader(in)
			var o bytes.Buffer
			c.Stdout = &o
			t0 := time.Now()
			if err := c.Run(); err != nil {
				t.Fatal(err)
			}
			d = append(d, time.Since(t0))
			deny := strings.Contains(in, "README2.txt")
			if got := o.String(); (!deny && got != "") || (deny && !strings.Contains(got, `"permissionDecision":"deny"`)) {
				t.Fatalf("hook %s stdout: %q", event, got)
			}
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		p95 = d[(n*95+99)/100-1]
		t.Logf("hook %s lượt %d: p50=%v p95=%v max=%v (n=%d)", event, r, d[n/2], p95, d[n-1], n)
		if p95 < 50*time.Millisecond {
			return
		}
	}
	t.Fatalf("p95 %v >= 50ms sau %d lượt", p95, rounds)
}

// closedURL là địa chỉ một cổng TCP vừa đóng: web "bật nhưng chết".
func closedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return "http://" + addr
}

// Web treo (nhận kết nối, không bao giờ trả lời): hook post-tool-use của binary thật vẫn trả về ngay, vì chỉ ghi
// outbox rồi mở `aisdlc web flush` tách rời (không giữ stdout/stderr của hook); tiến trình nền tự dừng ở hạn 5 giây.
func TestPostToolUseFastWhenWebHangs(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	// repo trước fakeweb: cleanup chạy ngược thứ tự, nên web đóng trước (nhả request đang treo), rồi chờ tiến trình
	// nền xong việc, cuối cùng mới xóa thư mục repo
	root := stepOneRepo(t)
	backoff := filepath.Join(root, ".aisdlc", "web", "backoff-until")
	t.Cleanup(func() {
		// web đã đóng: tiến trình gửi nền gặp lỗi kết nối, ghi backoff rồi nhả khóa
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(backoff); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		waitFlushDone(t, root)
	})
	web := fakeweb.New(t)
	web.Update(func(s *fakeweb.Server) { s.Hang = true })
	in := strings.Replace(editJSON(root, filepath.Join(root, codePath)), `"PreToolUse"`, `"PostToolUse"`, 1)
	vars := []string{"AISDLC_WEB_URL=" + web.URL(), "AISDLC_WEB_TOKEN=t0k"}
	run := func() (time.Duration, result) {
		c := exec.Command(binPath, "hook", "post-tool-use")
		c.Dir = root
		c.Env = append(hermeticEnv(t.TempDir()), vars...)
		c.Stdin = strings.NewReader(in)
		var o, e bytes.Buffer
		c.Stdout, c.Stderr = &o, &e // ống như Claude Code: Run chỉ xong khi mọi tiến trình giữ đầu ghi đã đóng nó
		t0 := time.Now()
		err := c.Run()
		d := time.Since(t0)
		code := 0
		if x, ok := err.(*exec.ExitError); ok {
			code = x.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return d, result{code, o.String(), e.String()}
	}
	d, r := run()
	if r.code != 0 || r.out != "" || r.errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", r.code, r.out, r.errOut)
	}
	if d >= 200*time.Millisecond {
		t.Fatalf("post-tool-use mất %v khi web treo (phải < 200ms)", d)
	}
	// tiến trình gửi nền đã tới web (và đang treo ở đó); hook kế tiếp không chờ nó
	deadline := time.Now().Add(3 * time.Second)
	for len(web.Requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(web.Requests()) == 0 {
		t.Fatal("web flush nền không gửi gì")
	}
	d2, r2 := run()
	if r2.code != 0 || r2.out != "" || d2 >= 200*time.Millisecond {
		t.Fatalf("lần 2: %v code=%d out=%q", d2, r2.code, r2.out)
	}
	t.Logf("post-tool-use khi web treo: %v, %v", d, d2)
}
