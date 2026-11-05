package policy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

var clockT = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

func clock() time.Time { return clockT }

type env struct {
	t     *testing.T
	tmp   string
	paths model.FeaturePaths
	cfg   model.RepoConfig
	specs []spec.StepSpec
}

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	f := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sh(t, tmp, "init", "-q")
	sh(t, tmp, "config", "user.email", "t@t")
	sh(t, tmp, "config", "user.name", "t")
	e := &env{t: t, tmp: tmp, paths: model.FeaturePaths{RepoRoot: tmp, Key: "PAY-123"}}
	e.cfg, err = model.LoadRepoConfig(tmp)
	if err != nil {
		t.Fatal(err)
	}
	e.specs = []spec.StepSpec{
		{ID: "S1", Title: "một", Scope: []string{"src/main/**"}, Budget: &spec.Budget{Files: 5, Lines: 50}},
		{ID: "S2", Title: "hai", Scope: []string{"src/main/**"}, Budget: &spec.Budget{Files: 5, Lines: 50}},
	}
	writeFile(t, tmp, "pom.xml", "<project/>\n")
	writeFile(t, tmp, "README.md", "hi\n")
	writeFile(t, tmp, "aisdlc/features/PAY-123/cases.yaml", "cases: []\n")
	writeFile(t, tmp, "aisdlc/features/PAY-123/schema-expected.json", "{}\n")
	writeFile(t, tmp, "aisdlc/features/PAY-123/brd.md", "# BRD\n")
	writeFile(t, tmp, "aisdlc/features/PAY-123/design.md", "# D\n")
	writeFile(t, tmp, "aisdlc/features/PAY-123/design.yaml", "steps: []\n")
	return e
}

func (e *env) gates(flow model.Flow) *gate.Service {
	return gate.NewService(e.paths, flow, e.cfg, gate.NewLocalGateStore(e.paths), clock)
}

func (e *env) steps(g *gate.Service) *step.Service {
	return step.NewService(e.paths, gitx.New(e.tmp), e.specs, e.cfg, clock).WithGates(g)
}

func (e *env) active(flow model.Flow) PolicyState {
	g := e.gates(flow)
	p := e.paths
	return PolicyState{RepoRoot: e.tmp, Config: e.cfg, Active: "PAY-123", Gates: g, Steps: e.steps(g), Paths: &p}
}

func (e *env) none() PolicyState { return PolicyState{RepoRoot: e.tmp, Config: e.cfg} }

func (e *env) approve(g *gate.Service, gs ...gate.Gate) {
	e.t.Helper()
	for _, x := range gs {
		if _, err := g.Submit(x, nil); err != nil {
			e.t.Fatal(err)
		}
		if _, err := g.Approve(x, "dev1", ""); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) commit() {
	sh(e.t, e.tmp, "add", "-A")
	sh(e.t, e.tmp, "commit", "-qm", "init")
}

func (e *env) codeReadyLite() PolicyState {
	e.approve(e.gates(model.FlowLite), gate.CASES)
	e.commit()
	return e.active(model.FlowLite)
}

func (e *env) start(s PolicyState, id string) {
	e.t.Helper()
	if _, err := s.Steps.Start(id); err != nil {
		e.t.Fatal(err)
	}
}

func passedRun() run.CaseRunResult {
	return run.CaseRunResult{Cases: []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}}}
}

func wr(s PolicyState, path string) Decision {
	return PreToolUse(ToolCall{ToolName: "Write", FilePath: path}, s)
}

func bashCall(s PolicyState, cmd string) Decision {
	return PreToolUse(ToolCall{ToolName: "Bash", Command: cmd}, s)
}

func assertDeny(t *testing.T, d Decision, parts ...string) {
	t.Helper()
	if d.Allow {
		t.Fatalf("want deny, got allow")
	}
	for _, p := range parts {
		if !strings.Contains(d.Reason, p) {
			t.Errorf("reason %q lacks %q", d.Reason, p)
		}
	}
}

func assertAllow(t *testing.T, d Decision, what string) {
	t.Helper()
	if !d.Allow {
		t.Errorf("%s: want allow, got deny %q", what, d.Reason)
	}
}

func TestRule1NonWriteToolsAllowed(t *testing.T) {
	e := newEnv(t)
	assertAllow(t, PreToolUse(ToolCall{ToolName: "Read", FilePath: "/etc/passwd"}, e.none()), "read")
	assertAllow(t, PreToolUse(ToolCall{ToolName: "Grep"}, e.active(model.FlowLite)), "grep")
}

func TestDenyWriteRiskYaml(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertDeny(t, PreToolUse(ToolCall{ToolName: "Write", FilePath: "aisdlc/features/PAY-123/risk.yaml"}, s), "aisdlc")
}

func TestDenyWriteRiskPolicyYaml(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	d := PreToolUse(ToolCall{ToolName: "Edit", FilePath: "aisdlc/risk-policy.yaml"}, s)
	assertDeny(t, d, "Chính sách rủi ro do dev sửa (web hoặc MR), agent không được sửa")
	assertDeny(t, wr(e.none(), "aisdlc/risk-policy.yaml"), "Chính sách rủi ro")
}

func TestDenyBashRedirectToRiskYaml(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertDeny(t, bashCall(s, "echo 'level: low' > aisdlc/features/PAY-123/risk.yaml"))
	assertDeny(t, bashCall(s, "sed -i s/high/low/ aisdlc/features/PAY-123/risk.yaml"))
}

func TestRule2BashUsesHeuristics(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertDeny(t, bashCall(s, "aisdlc approve review"), "dev")
	assertDeny(t, bashCall(s, "sed -i s/a/b/ aisdlc/features/PAY-123/cases.yaml"))
	assertDeny(t, bashCall(s, "rm "+e.tmp+"/aisdlc/features/PAY-123/gates.json"))
	assertDeny(t, bashCall(s, "echo x > .aisdlc/active.json"))
	assertAllow(t, bashCall(s, "mvn -q test"), "mvn")
	assertAllow(t, bashCall(s, "aisdlc step start S1"), "start")
	assertAllow(t, bashCall(s, "aisdlc step finish S1"), "finish")
	assertAllow(t, bashCall(e.none(), "rm foo"), "rm foo")
	assertDeny(t, bashCall(e.none(), "aisdlc approve brd"))
}

// TestBashBypassThroughPreToolUse: cases.yaml đã khóa; mọi dạng ghi đều bị chặn (Java không chặn dạng cd +
// đường dẫn ngắn; bản Go chặn).
func TestBashBypassThroughPreToolUse(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	for _, c := range []string{
		"mv x aisdlc/features/PAY-123/cases.yaml",
		"cp /tmp/c aisdlc/features/PAY-123/cases.yaml",
		"echo a > aisdlc/features/PAY-123/cases.yaml",
		"tee aisdlc/features/PAY-123/cases.yaml < x",
		"sed -i s/a/b/ ./aisdlc/../aisdlc/features/PAY-123/cases.yaml",
	} {
		if bashCall(s, c).Allow {
			t.Errorf("should block: %s", c)
		}
	}
	assertDeny(t, bashCall(s, "cd aisdlc/features && sed -i s/a/b/ PAY-123/cases.yaml"), "PAY-123/cases.yaml")
	// cwd của hook là thư mục con của repo
	d := PreToolUse(ToolCall{ToolName: "Bash", Command: "sed -i s/a/b/ cases.yaml",
		Cwd: filepath.Join(e.tmp, "aisdlc/features/PAY-123")}, s)
	assertDeny(t, d, "aisdlc/features/PAY-123/cases.yaml")
	assertDeny(t, bashCall(s, "echo x > aisdlc/config.yaml"), "aisdlc/config.yaml")
}

func TestRule3OutsideRepoAllowed(t *testing.T) {
	e := newEnv(t)
	assertAllow(t, wr(e.none(), "/tmp/elsewhere/x.java"), "elsewhere")
	assertAllow(t, wr(e.active(model.FlowLite), filepath.Join(e.tmp, "../outside.java")), "outside")
}

func TestRule4StateFilesDenied(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertDeny(t, wr(s, e.tmp+"/.aisdlc/active.json"), "File trạng thái do CLI quản lý")
	assertDeny(t, wr(s, ".aisdlc/active.json"), "CLI")
	for _, f := range []string{"gates.json", "steps.json", "scope-extra.json", "feedback.md"} {
		assertDeny(t, wr(s, "aisdlc/features/PAY-123/"+f), "CLI")
	}
	assertDeny(t, wr(e.none(), ".aisdlc/active.json"), "CLI")
}

func TestRule5ApprovedArtifactDenied(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertDeny(t, wr(s, "aisdlc/features/PAY-123/cases.yaml"), "cases.yaml", "đã được duyệt ở cổng CASES", "`aisdlc reopen cases`")
	assertDeny(t, wr(s, e.tmp+"/aisdlc/features/PAY-123/schema-expected.json"), "CASES")
}

func TestRule6FeatureDirAllowed(t *testing.T) {
	e := newEnv(t)
	s := e.active(model.FlowLite)
	assertAllow(t, wr(s, "aisdlc/features/PAY-123/notes.txt"), "notes")
	assertAllow(t, wr(s, "aisdlc/features/PAY-123/cases.yaml"), "cases")
}

func TestRule7UnmanagedAllowed(t *testing.T) {
	e := newEnv(t)
	assertAllow(t, wr(e.active(model.FlowLite), "README.md"), "readme")
	assertAllow(t, wr(e.none(), "docs/a/b.txt"), "docs")
}

func TestRule8NoActiveFeatureDenied(t *testing.T) {
	e := newEnv(t)
	assertDeny(t, wr(e.none(), "src/main/java/A.java"), "Chưa có tính năng đang làm", "/aisdlc:init <JIRA-KEY>")
}

func TestAmbiguousFeatureDenied(t *testing.T) {
	e := newEnv(t)
	s := e.none()
	s.Ambiguous = []model.FeatureKey{"PAY-1", "PAY-2"}
	d := wr(s, "src/main/java/A.java")
	assertDeny(t, d, "Có nhiều tính năng đang mở (PAY-1, PAY-2)", "aisdlc use <KEY>")
	if d.Code != "AMBIGUOUS_FEATURE" {
		t.Error(d.Code)
	}
}

func TestRule9GatesNotOpenDenied(t *testing.T) {
	e := newEnv(t)
	assertDeny(t, wr(e.active(model.FlowLite), "src/main/java/A.java"), "CASES")
}

func TestStaleBrdBlocksCodeWrites(t *testing.T) {
	e := newEnv(t)
	e.approve(e.gates(model.FlowFull), gate.BRD, gate.DESIGN, gate.CASES)
	e.commit()
	s := e.active(model.FlowFull)
	if wr(s, "src/main/java/A.java").Allow {
		t.Error("no step started, must block")
	}
	writeFile(t, e.tmp, "aisdlc/features/PAY-123/brd.md", "# BRD sửa sau khi duyệt\n")
	assertDeny(t, wr(e.active(model.FlowFull), "src/main/java/A.java"), "BRD")
	assertDeny(t, wr(e.active(model.FlowFull), "aisdlc/features/PAY-123/brd.md"), "BRD", "aisdlc reopen brd")
}

func TestStaleBrdBlocksEvenWithStepInProgress(t *testing.T) {
	e := newEnv(t)
	g := e.gates(model.FlowFull)
	e.approve(g, gate.BRD, gate.DESIGN, gate.CASES)
	e.commit()
	if _, err := e.steps(g).Start("S1"); err != nil {
		t.Fatal(err)
	}
	assertAllow(t, wr(e.active(model.FlowFull), "src/main/java/A.java"), "in scope")
	writeFile(t, e.tmp, "aisdlc/features/PAY-123/brd.md", "# BRD v2\n")
	assertDeny(t, wr(e.active(model.FlowFull), "src/main/java/A.java"), "BRD")
}

func TestRule10NoStepInProgressDenied(t *testing.T) {
	e := newEnv(t)
	assertDeny(t, wr(e.codeReadyLite(), "src/main/java/A.java"), "aisdlc step start S1")
}

func TestRule10AwaitingDevDenied(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	if _, err := s.Steps.Finish("S1", passedRun()); err != nil {
		t.Fatal(err)
	}
	assertDeny(t, wr(s, "src/main/java/A.java"),
		"Bước S1 đang chờ dev duyệt: dev chạy `aisdlc step accept S1`, hoặc chạy `aisdlc step start S1` để sửa tiếp.")
}

func TestRule10AfterAcceptNamesNextStep(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	if _, err := s.Steps.Finish("S1", passedRun()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Steps.Accept("S1", "dev1"); err != nil {
		t.Fatal(err)
	}
	assertDeny(t, wr(s, "src/main/java/A.java"), "aisdlc step start S2")
}

func TestRule11OutOfScopeDenied(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	assertDeny(t, wr(s, "src/other/B.java"), "Ngoài phạm vi S1", "src/main/**", "aisdlc scope allow src/other/B.java")
}

func TestRule12InScopeAllowed(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	assertAllow(t, wr(s, "src/main/java/A.java"), "rel")
	assertAllow(t, wr(s, e.tmp+"/src/main/java/A.java"), "abs")
	assertAllow(t, PreToolUse(ToolCall{ToolName: "Edit", FilePath: "./src/main/../main/java/A.java"}, s), "dots")
}

func TestWriteToolWithoutPathDeniedInActiveRepo(t *testing.T) {
	e := newEnv(t)
	assertDeny(t, PreToolUse(ToolCall{ToolName: "Edit"}, e.active(model.FlowLite)), "đường dẫn")
}

func TestStopBlocksWhileStepInProgress(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	assertAllow(t, Stop(false, s), "no step")
	e.start(s, "S1")
	assertDeny(t, Stop(false, s), "Bước S1 chưa kết thúc. Chạy `aisdlc step finish S1` và báo kết quả cho dev trước khi dừng.")
	if _, err := s.Steps.Finish("S1", passedRun()); err != nil {
		t.Fatal(err)
	}
	assertAllow(t, Stop(false, s), "finished")
	assertAllow(t, Stop(false, e.none()), "none")
}

func TestStopAllowsWhenStopHookActive(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	assertAllow(t, Stop(true, s), "active")
}

func TestSessionContextHasRulesAndState(t *testing.T) {
	e := newEnv(t)
	bare := SessionContext(e.none())
	if !strings.Contains(bare, "AISDLC") || strings.Contains(bare, "PAY-123") {
		t.Error(bare)
	}
	if n := len(strings.Split(bare, "\n")); n > 15 {
		t.Error(n)
	}
	s := e.codeReadyLite()
	e.start(s, "S1")
	ctx := SessionContext(s)
	for _, p := range []string{"PAY-123", "CASES", "S1", "IN_PROGRESS"} {
		if !strings.Contains(ctx, p) {
			t.Errorf("lacks %s: %s", p, ctx)
		}
	}
}

func viaLink(real PolicyState, link string) PolicyState {
	real.RepoRoot = link
	return real
}

func TestSymlinkedRepoRootStillAppliesRules(t *testing.T) {
	e := newEnv(t)
	link := filepath.Join(e.tmp, "link")
	if err := os.Symlink(e.tmp, link); err != nil {
		t.Fatal(err)
	}
	s := viaLink(e.active(model.FlowLite), link)
	assertDeny(t, wr(s, e.tmp+"/src/main/java/A.java"), "CASES")
	assertDeny(t, wr(e.active(model.FlowLite), link+"/src/main/java/A.java"), "CASES")
	assertDeny(t, wr(s, e.tmp+"/src/main/new/deep/B.java"), "CASES")
	assertDeny(t, wr(s, e.tmp+"/.aisdlc/active.json"), "CLI")
}

func TestSymlinkedPathToApprovedArtifactDenied(t *testing.T) {
	e := newEnv(t)
	real := e.codeReadyLite()
	link := filepath.Join(e.tmp, "link")
	if err := os.Symlink(e.tmp, link); err != nil {
		t.Fatal(err)
	}
	assertDeny(t, wr(viaLink(real, link), e.tmp+"/aisdlc/features/PAY-123/cases.yaml"), "CASES")
	assertDeny(t, bashCall(viaLink(real, link), "rm "+e.tmp+"/aisdlc/features/PAY-123/gates.json"))
}

func TestInvalidPathDenied(t *testing.T) {
	e := newEnv(t)
	assertDeny(t, wr(e.active(model.FlowLite), "src/main/\x00/A.java"), "không hợp lệ")
}

// ---- thư mục cấu hình web của dev: agent không được đọc/ghi ----

func (e *env) home() string {
	home := filepath.Join(e.tmp, "home")
	if err := os.MkdirAll(filepath.Join(home, ".config", "aisdlc"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	return home
}

func (e *env) webCfg(home string, paths []string, command string) Decision {
	return WebConfigDecision(paths, command, filepath.Join(home, ".config", "aisdlc"), e.tmp)
}

func TestHardenedHeuristics(t *testing.T) {
	e := newEnv(t)
	home := e.home()
	for _, c := range []string{"env FOO=1", "env -i FOO=1", "env -u PATH", "env --", "FOO=1 env", "(set)", `bash -c "env"`} {
		assertDeny(t, e.webCfg(home, nil, c), "biến môi trường")
	}
	for _, c := range []string{"env FOO=1 mvn -q test", "env -u X java -version", `python3 -c "print(set(x))"`,
		`node -e "console.log(env(1))"`, "printenv JAVA_HOME", "set -euo pipefail; mvn test", `echo 'a\nb'`} {
		assertAllow(t, e.webCfg(home, nil, c), c)
	}
	for _, p := range []string{"/proc/self/environ", "/proc/1234/environ", "/proc/self", "/proc/*/environ"} {
		assertDeny(t, e.webCfg(home, []string{p}, ""), "biến môi trường")
	}
	assertAllow(t, e.webCfg(home, []string{"/proc/cpuinfo"}, ""), "cpuinfo")
	assertDeny(t, e.webCfg(home, nil, `cat ~/.config/ai\sdlc/approve-token`), "File cấu hình web")
	assertDeny(t, e.webCfg(home, nil, `cat ~/.con\fig/aisdlc/web.json`), "File cấu hình web")
	assertDeny(t, e.webCfg(home, []string{`~/.config/ai\sdlc/web.json`}, ""), "File cấu hình web")
}

func TestWebConfigDirDeniedForPathsAndBash(t *testing.T) {
	e := newEnv(t)
	home := e.home()
	dir := filepath.Join(home, ".config", "aisdlc")
	for _, c := range []struct{ p, msg string }{
		{dir + "/web.json", "File cấu hình web của dev"},
		{"~/.config/aisdlc/approve-token", "agent không được đọc/ghi"},
		{home + "/.config/x/../aisdlc/web.json", "File cấu hình web"},
		{dir, "File cấu hình web"},
		{filepath.Join(home, ".config"), "File cấu hình web"},
		{home + "/.config/ais*/web.json", "File cấu hình web"},
		{"~/.c*/a*/web.json", "File cấu hình web"},
		{"~/*/aisdlc/*", "File cấu hình web"},
	} {
		assertDeny(t, e.webCfg(home, []string{c.p}, ""), c.msg)
	}
	for _, c := range []string{
		"cat ~/.config/aisdlc/approve-token", "cat $HOME/.config/aisdlc/web.json | base64", "cat " + dir + "/web.json",
		"cd ~/.config && cat aisdlc/web.json",
		"cat ~/.config/*/approve-token", "cat ~/.config/ais*/approve-token", "cat ~/.c*/a*/web.json",
		"cat $HOME/*/*/web-pubkey", "find ~ -path '*aisdlc*' -name approve-token -exec cat {} +",
		"find $HOME -path '*aisdlc*' -exec cat {} +", "find " + home + " -path '*aisdlc*'",
		"grep -r token ~/.config/ais?lc", "grep -r aisdlc ~", "rg -uu --hidden aisdlc ~",
		"find / -name approve-token", `cat "$HOME/.config/aisdlc/web.json"`,
		"x=~/.config/aisdlc; cat $x/web.json", "grep -rn aisdlc ~", `grep -r "" ~/.config`, "rg approve ~/.config",
		"cat ${HOME}/.config/aisdlc/web.json", "grep -Ri token $HOME", "tar czf /tmp/x.tgz ~/.config",
		"cp -r ~/.config /tmp/c", "ls ~/.config/*/",
	} {
		d := e.webCfg(home, nil, c)
		if d.Allow || !strings.Contains(d.Reason, "File cấu hình web") {
			t.Errorf("%q: %+v", c, d)
		}
	}
	link := filepath.Join(e.tmp, "cfglink")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	assertDeny(t, e.webCfg(home, []string{link + "/web.json"}, ""), "File cấu hình web")
}

func TestWebConfigRuleAllowsOrdinaryPaths(t *testing.T) {
	e := newEnv(t)
	home := e.home()
	for _, p := range []string{filepath.Join(e.tmp, "README.md"), "src/**/*.java", "**/*.java", e.tmp,
		filepath.Join(home, ".config", "other", "x")} {
		assertAllow(t, e.webCfg(home, []string{p}, ""), p)
	}
	assertAllow(t, e.webCfg(home, nil, "mvn -q test && aisdlc status"), "mvn")
	h := home
	for _, c := range []string{"cat README.md", "ls ~/.config", "grep -rn TODO src", "find . -name '*.java'",
		"git log --oneline -5", "aisdlc submit brd", "ls ~/projects",
		"ls " + h + "/proj/src/*.java", "rm -f " + h + "/proj/target/*.class",
		"rg -n TODO " + h + "/git/aisdlc/src", "find " + h + "/proj/aisdlc/features -name '*.md'",
		"grep -r X ~/proj/aisdlc/features", "ls ${HOME}/projects", "ls ~/*.txt", "ls ~/.c*", "cat ~/.config/git/config"} {
		assertAllow(t, e.webCfg(home, nil, c), c)
	}
	assertAllow(t, e.webCfg(home, nil, ""), "empty")
	assertAllow(t, e.webCfg(home, []string{"a\x00b"}, ""), "nul")
}

func TestCdIntoDotConfigThenGlobOrAisdlcDenied(t *testing.T) {
	e := newEnv(t)
	home := e.home()
	for _, c := range []string{
		"cd ~/.config && cat */*", "cd ~/.config/ && cat */approve-tok*", "cd $HOME/.config; ls -la ai*",
		"cd ${HOME}/.config && cat ./*/*", `cd "$HOME/.config" && head -c 200 */w*`,
		"cd " + home + "/.config && cat */*", "pushd ~/.config >/dev/null && cat */*", "cd ~/.config && aisdlc status",
		"cd ~/.config&cd ~/.config&&cat */*",
	} {
		d := e.webCfg(home, nil, c)
		if d.Allow || !strings.Contains(d.Reason, "File cấu hình web") {
			t.Errorf("%q: %+v", c, d)
		}
	}
	for _, c := range []string{"cd ~/proj && ls *.java", "cd ~/.config && ls", "cd ~/.config/git && cat config",
		"cd " + home + "/proj && cat src/*.java && aisdlc status", "ls *.java && cd ~/.config"} {
		assertAllow(t, e.webCfg(home, nil, c), c)
	}
}

func TestReadingWebTokenFromEnvironmentDenied(t *testing.T) {
	e := newEnv(t)
	home := e.home()
	for _, c := range []string{
		"echo $AISDLC_WEB_TOKEN", `echo "${AISDLC_WEB_TOKEN}" | base64`,
		`curl -H "Authorization: Bearer $AISDLC_WEB_TOKEN" http://web/api/login`,
		"printenv AISDLC_WEB_TOKEN", "printenv", "printenv | grep AISDLC", "env", "env | grep -i token",
		"/usr/bin/env", "ls && env", "cat /proc/self/environ",
	} {
		d := e.webCfg(home, nil, c)
		if d.Allow || !strings.Contains(d.Reason, "biến môi trường") {
			t.Errorf("%q: %+v", c, d)
		}
	}
	for _, c := range []string{"env FOO=1 mvn -q test", "env -u HOME aisdlc status", "printenv HOME", "echo $HOME",
		"AISDLC_WEB_URL=x aisdlc status", "mvn -Denv=dev test", "cat .env.example"} {
		assertAllow(t, e.webCfg(home, nil, c), c)
	}
}

func withControl(s PolicyState, c *snapshot.FeatureControl) PolicyState {
	s.Control = c
	return s
}

func paused(p bool) *snapshot.FeatureControl {
	t := time.Date(2026, 11, 4, 2, 5, 0, 0, time.UTC)
	c := &snapshot.FeatureControl{Repo: "r", Feature: "PAY-123", IssuedAt: t}
	if p {
		c.Pause = &snapshot.Pause{By: "an", At: t}
	}
	return c
}

func TestPausedDeniesWrites(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	assertAllow(t, wr(s, "src/main/java/A.java"), "before pause")
	p := withControl(s, paused(true))
	d := wr(p, "src/main/java/A.java")
	if d.Allow || d.Reason != "Dev đã tạm dừng tại web: an. Dừng lại và chờ. Khi dev báo tiếp tục thì thử lại." || d.Code != "PAUSED" {
		t.Errorf("%+v", d)
	}
	b := bashCall(p, "mvn -q test")
	if b.Allow || b.Code != "PAUSED" {
		t.Errorf("%+v", b)
	}
	assertAllow(t, PreToolUse(ToolCall{ToolName: "Read", FilePath: "src/main/java/A.java"}, p), "read")
	assertAllow(t, wr(withControl(s, paused(false)), "src/main/java/A.java"), "unpaused")
}

func TestPausedStopAllowed(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	if Stop(false, withControl(s, paused(false))).Allow || Stop(false, s).Allow {
		t.Error("must block")
	}
	assertAllow(t, Stop(false, withControl(s, paused(true))), "paused")
}

func TestOutOfScopeHasCode(t *testing.T) {
	e := newEnv(t)
	s := e.codeReadyLite()
	e.start(s, "S1")
	d := wr(s, "src/other/B.java")
	if d.Allow || d.Code != "OUT_OF_SCOPE" {
		t.Errorf("%+v", d)
	}
}

func TestInstructionContextFormat(t *testing.T) {
	at := time.Date(2026, 11, 4, 2, 5, 0, 0, time.UTC)
	zone := time.FixedZone("ICT", 7*3600)
	out := InstructionContext([]snapshot.Instruction{{ID: 1, By: "an", At: at, Text: "a"}, {ID: 2, By: "an", At: at, Text: "b"}}, zone)
	if out != "Chỉ dẫn của dev (09:05): a\n\nChỉ dẫn của dev (09:05): b" {
		t.Error(out)
	}
	if InstructionContext(nil, time.UTC) != "" {
		t.Error("empty")
	}
}

func TestDecisionConstructors(t *testing.T) {
	if d := Ok(); !d.Allow || d.Reason != "" || d.Code != "" {
		t.Error(d)
	}
	if d := Deny("x"); d.Allow || d.Reason != "x" || d.Code != "" {
		t.Error(d)
	}
	if d := DenyCode("C", "x"); d.Allow || d.Reason != "x" || d.Code != "C" {
		t.Error(d)
	}
}

func TestErrDenyMatchesJavaCatchAll(t *testing.T) {
	d := errDeny(errors.New("hỏng\n  file  gates.json"))
	if d.Reason != "aisdlc lỗi: hỏng file gates.json" {
		t.Errorf("reason = %q", d.Reason)
	}
}

func TestEditConfigYamlDenied(t *testing.T) {
	e := newEnv(t)
	g := e.gates(model.FlowFull)
	e.approve(g, gate.Sequence(model.FlowFull)...)
	e.commit()
	s := e.active(model.FlowFull)
	e.start(s, "S1")
	open := s
	open.Config.Unmanaged = []string{"**"} // kể cả khi luật unmanaged cho mọi file
	for _, tool := range []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} {
		for _, p := range []string{"aisdlc/config.yaml", "aisdlc/features/../config.yaml", filepath.Join(e.tmp, "aisdlc/config.yaml")} {
			for _, st := range []PolicyState{s, open, e.none()} {
				d := PreToolUse(ToolCall{ToolName: tool, FilePath: p}, st)
				if d.Allow || d.Code != "CONFIG_LOCKED" {
					t.Errorf("%s %s: %+v", tool, p, d)
				}
				if d.Reason != "aisdlc/config.yaml chỉ dev sửa, ngoài Claude Code." {
					t.Errorf("%s %s: %q", tool, p, d.Reason)
				}
			}
		}
		for _, st := range []PolicyState{e.none(), s, open} {
			d := PreToolUse(ToolCall{ToolName: tool, FilePath: "aisdlc/risk-policy.yaml"}, st)
			if d.Allow || d.Code != "CONFIG_LOCKED" {
				t.Errorf("%s risk-policy: %+v", tool, d)
			}
		}
	}
}
