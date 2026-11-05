package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/lang/langs"
	"aisdlc/internal/model"
)

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/m\n\ngo 1.21\n"
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var svc = model.ServiceConfig{Main: "./cmd/server"}

const okMain = "package main\n\nfunc main() {}\n"

func TestBuildProducesCoverBinary(t *testing.T) {
	repo := writeModule(t, map[string]string{"cmd/server/main.go": okMain})
	out := t.TempDir()
	r, err := golang.Runtime{}.Build(context.Background(), repo, svc, out)
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := os.Stat(r.Binary); err != nil || filepath.Dir(r.Binary) != out {
		t.Errorf("binary %q: %v", r.Binary, err)
	}
}

func TestBuildFailureIsNotError(t *testing.T) {
	repo := writeModule(t, map[string]string{"cmd/server/main.go": "package main\n\nfunc main() {\n"})
	r, err := golang.Runtime{}.Build(context.Background(), repo, svc, t.TempDir())
	if err != nil || r.OK || r.Binary != "" || !strings.Contains(r.Output, "main.go") {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestBuildNoGoMod(t *testing.T) {
	repo := t.TempDir()
	r, err := golang.Runtime{}.Build(context.Background(), repo, svc, t.TempDir())
	if err != nil || r.OK || r.Output != "không có go.mod ở "+repo {
		t.Fatalf("%v %+v", err, r)
	}
}

var ut = model.UnitTestConfig{Packages: []string{"./..."}}

func TestUnitTestsCollectsFailures(t *testing.T) {
	repo := writeModule(t, map[string]string{
		"p/p.go":      "package p\n",
		"p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestGood(t *testing.T) {}\nfunc TestBad(t *testing.T) { t.Fatal(\"x\") }\n",
	})
	r, err := golang.Runtime{}.UnitTests(context.Background(), repo, ut, t.TempDir())
	if err != nil || r.Exit == 0 || !reflect.DeepEqual(r.Failed, []string{"example.com/m/p.TestBad"}) {
		t.Fatalf("%v %+v", err, r)
	}
	if r.Profile == "" || !strings.Contains(r.Output, "TestBad") || strings.HasPrefix(r.Output, "{") {
		t.Errorf("profile %q output %q", r.Profile, r.Output)
	}
}

func TestUnitTestsPass(t *testing.T) {
	repo := writeModule(t, map[string]string{
		"p/p.go":      "package p\n",
		"p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestGood(t *testing.T) {}\n",
	})
	r, err := golang.Runtime{}.UnitTests(context.Background(), repo, ut, t.TempDir())
	if err != nil || r.Exit != 0 || len(r.Failed) != 0 || r.Profile == "" {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestUnitTestsPackageBuildFailure(t *testing.T) {
	repo := writeModule(t, map[string]string{
		"p/p.go":      "package p\n",
		"p/p_test.go": "package p\n\nfunc TestX( {\n",
	})
	r, err := golang.Runtime{}.UnitTests(context.Background(), repo, ut, t.TempDir())
	if err != nil || r.Exit == 0 || !reflect.DeepEqual(r.Failed, []string{"example.com/m/p"}) {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestUnitTestsSubtestFailureCountsParent(t *testing.T) {
	repo := writeModule(t, map[string]string{
		"p/p.go":      "package p\n",
		"p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Run(\"sub\", func(t *testing.T) { t.Fatal(\"x\") }) }\n",
	})
	r, err := golang.Runtime{}.UnitTests(context.Background(), repo, ut, t.TempDir())
	want := []string{"example.com/m/p.TestX", "example.com/m/p.TestX/sub"}
	if err != nil || !reflect.DeepEqual(r.Failed, want) {
		t.Fatalf("%v %+v", err, r)
	}
}

const gracefulMain = `package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)
	fmt.Println("hello out")
	fmt.Fprintln(os.Stderr, "hello err")
	<-c
	os.Exit(0)
}
`

const stubbornMain = `package main

import (
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(syscall.SIGTERM)
	c := exec.Command("sleep", "60")
	if err := c.Start(); err != nil {
		panic(err)
	}
	os.WriteFile(os.Getenv("PIDFILE"), []byte(strconv.Itoa(c.Process.Pid)), 0o644)
	for {
		time.Sleep(time.Hour)
	}
}
`

func buildSvc(t *testing.T, src string) string {
	t.Helper()
	repo := writeModule(t, map[string]string{"cmd/server/main.go": src})
	r, err := golang.Runtime{}.Build(context.Background(), repo, svc, t.TempDir())
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
	return r.Binary
}

func waitFile(t *testing.T, p string) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("hết giờ chờ %s", p)
	return ""
}

func TestStartAndGracefulStopAndLog(t *testing.T) {
	bin := buildSvc(t, gracefulMain)
	logPath := filepath.Join(t.TempDir(), "svc.log")
	p, err := golang.Runtime{}.Start(context.Background(), bin, nil, t.TempDir(), logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "hello err") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !p.Stop(5 * time.Second) {
		t.Error("mong đợi thoát êm")
	}
	select {
	case <-p.Done():
	default:
		t.Error("Done chưa đóng")
	}
	if p.ExitErr() != nil {
		t.Errorf("exit err %v", p.ExitErr())
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "hello out") || !strings.Contains(string(b), "hello err") {
		t.Errorf("log %q", b)
	}
}

func TestStopKillsAfterTimeout(t *testing.T) {
	bin := buildSvc(t, stubbornMain)
	pidFile := filepath.Join(t.TempDir(), "pid")
	p, err := golang.Runtime{}.Start(context.Background(), bin, map[string]string{"PIDFILE": pidFile}, t.TempDir(), filepath.Join(t.TempDir(), "l.log"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(waitFile(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // để signal.Ignore có hiệu lực
	if p.Stop(500 * time.Millisecond) {
		t.Error("mong đợi bị SIGKILL")
	}
	dead := false
	for i := 0; i < 100 && !dead; i++ {
		dead = syscall.Kill(child, 0) != nil
		time.Sleep(25 * time.Millisecond)
	}
	if !dead {
		syscall.Kill(child, syscall.SIGKILL)
		t.Error("tiến trình con còn sống")
	}
}

func TestStartCtxCancelKillsGroup(t *testing.T) {
	bin := buildSvc(t, stubbornMain)
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	p, err := golang.Runtime{}.Start(ctx, bin, map[string]string{"PIDFILE": pidFile}, t.TempDir(), filepath.Join(t.TempDir(), "l.log"))
	if err != nil {
		t.Fatal(err)
	}
	waitFile(t, pidFile)
	cancel()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ctx hủy mà tiến trình không thoát")
	}
}

func TestRuntimeFor(t *testing.T) {
	var _ lang.Runtime
	if _, err := langs.RuntimeFor("go"); err != nil {
		t.Error(err)
	}
	_, err := langs.RuntimeFor("java")
	if err == nil || err.Error() != "Ngôn ngữ chưa hỗ trợ: java (hiện có: go)" {
		t.Errorf("got %v", err)
	}
}

const leaderExitsMain = `package main

import (
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)
	ch := exec.Command("sh", "-c", "trap '' TERM; exec sleep 60")
	if err := ch.Start(); err != nil {
		panic(err)
	}
	os.WriteFile(os.Getenv("PIDFILE"), []byte(strconv.Itoa(ch.Process.Pid)), 0o644)
	if os.Getenv("MODE") == "self" {
		os.Exit(0)
	}
	<-c
	os.Exit(0)
}
`

func waitDead(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Error("tiến trình con còn sống sau Stop")
}

// Tiến trình con bỏ qua SIGTERM, nên chỉ SIGKILL nhóm sau khi leader thoát mới diệt được.
func TestStopKillsChildrenAfterGracefulExit(t *testing.T) {
	bin := buildSvc(t, leaderExitsMain)
	pidFile := filepath.Join(t.TempDir(), "pid")
	p, err := golang.Runtime{}.Start(context.Background(), bin, map[string]string{"PIDFILE": pidFile, "MODE": "term"}, t.TempDir(), filepath.Join(t.TempDir(), "l.log"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(waitFile(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // để signal.Notify và trap có hiệu lực
	if !p.Stop(5 * time.Second) {
		t.Error("mong đợi thoát êm")
	}
	waitDead(t, child)
}

func TestStopKillsChildrenAfterLeaderAlreadyExited(t *testing.T) {
	bin := buildSvc(t, leaderExitsMain)
	pidFile := filepath.Join(t.TempDir(), "pid")
	p, err := golang.Runtime{}.Start(context.Background(), bin, map[string]string{"PIDFILE": pidFile, "MODE": "self"}, t.TempDir(), filepath.Join(t.TempDir(), "l.log"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(waitFile(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("leader không tự thoát")
	}
	time.Sleep(300 * time.Millisecond) // để trap của sh có hiệu lực
	p.Stop(time.Second)
	waitDead(t, child)
}

func TestBuildFlagsBeforePackage(t *testing.T) {
	repo := writeModule(t, map[string]string{"cmd/server/main.go": okMain})
	s := svc
	s.BuildFlags = []string{"-ldflags=-s"}
	r, err := golang.Runtime{}.Build(context.Background(), repo, s, t.TempDir())
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
}
