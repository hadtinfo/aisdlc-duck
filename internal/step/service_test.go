package step

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

var t0 = time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)

func clock() time.Time { return t0 }

type env struct {
	t     *testing.T
	dir   string
	paths model.FeaturePaths
	cfg   model.RepoConfig
	git   *gitx.Repo
	gates *gate.Service
	specs []spec.StepSpec
}

func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
}

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	f := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lines(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "line%d\n", i)
	}
	return sb.String()
}

func budgetSpec(id string, files, lines int) spec.StepSpec {
	return spec.StepSpec{ID: id, Title: id, Scope: []string{"src/**"}, Budget: &spec.Budget{Files: files, Lines: lines}}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	hermetic(t)
	dir := t.TempDir()
	sh(t, dir, "init", "-q")
	sh(t, dir, "config", "user.email", "t@t")
	sh(t, dir, "config", "user.name", "t")
	e := &env{t: t, dir: dir, paths: model.FeaturePaths{RepoRoot: dir, Key: "PAY-123"}}
	cfg, err := model.LoadRepoConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.cfg = cfg
	write(t, dir, "pom.xml", "<project/>\n")
	write(t, dir, "src/old.txt", "o1\no2\no3\n")
	write(t, dir, "src/ren.txt", lines(20))
	e.git = gitx.New(dir)
	e.gates = gate.NewService(e.paths, model.FlowLite, cfg, gate.NewLocalGateStore(e.paths), clock)
	write(t, dir, "aisdlc/features/PAY-123/cases.yaml", "cases: []\n")
	write(t, dir, "aisdlc/features/PAY-123/schema-expected.json", "{}\n")
	e.specs = []spec.StepSpec{budgetSpec("S1", 5, 10), budgetSpec("S2", 5, 10)}
	return e
}

func (e *env) write(rel, content string) { write(e.t, e.dir, rel, content) }

func (e *env) openGates() {
	e.t.Helper()
	if _, err := e.gates.Submit(gate.CASES, nil); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.gates.Approve(gate.CASES, "dev1", ""); err != nil {
		e.t.Fatal(err)
	}
	sh(e.t, e.dir, "add", "-A")
	sh(e.t, e.dir, "commit", "-qm", "init")
}

func (e *env) service() *Service {
	return NewService(e.paths, e.git, e.specs, e.cfg, clock).WithGates(e.gates)
}

func passedRun() run.CaseRunResult {
	return run.CaseRunResult{Cases: []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}}}
}

func isStepErr(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

func mustRec(t *testing.T) func(StepRecord, error) StepRecord {
	return func(r StepRecord, err error) StepRecord {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}

func mustAll(t *testing.T, s *Service) []StepRecord {
	t.Helper()
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func paths2(cs []gitx.FileChange) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return out
}

func TestCannotStartBeforeCodeReady(t *testing.T) {
	e := newEnv(t)
	sh(t, e.dir, "add", "-A")
	sh(t, e.dir, "commit", "-qm", "init")
	if _, err := e.service().Start("S1"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}

func TestStartRefusesWithoutGates(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := NewService(e.paths, e.git, e.specs, e.cfg, clock)
	if _, err := s.Start("S1"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}

func TestCannotStartS2BeforeS1Accepted(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	mustRec(t)(s.Finish("S1", passedRun()))
	if _, err := s.Start("S2"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
	mustRec(t)(s.Accept("S1", "dev1"))
	if r := mustRec(t)(s.Start("S2")); r.Status != StepInProgress {
		t.Errorf("status %s", r.Status)
	}
	if _, err := s.Start("S9"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}

func TestCannotSkipToS2(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	if _, err := e.service().Start("S2"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}

func TestPersistsAcrossInstances(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	mustRec(t)(e.service().Start("S1"))
	again := e.service()
	cur, ok, err := again.Current()
	if err != nil || !ok || cur.ID != "S1" {
		t.Fatalf("current: %v %v %v", cur, ok, err)
	}
	if n := len(mustAll(t, again)); n != 1 {
		t.Errorf("all = %d", n)
	}
}

func TestFinishCountsUntrackedDeletedRenamed(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("src/new.txt", "n1\nn2\nn3\nn4\nn5\n")
	if err := os.Remove(filepath.Join(e.dir, "src/old.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(e.dir, "src/ren.txt"), filepath.Join(e.dir, "src/renamed.txt")); err != nil {
		t.Fatal(err)
	}
	r := mustRec(t)(s.Finish("S1", passedRun()))
	if r.Status != StepAwaitingDev || r.Check.Files != 3 || r.Check.Lines != 8 {
		t.Errorf("status=%s files=%d lines=%d", r.Status, r.Check.Files, r.Check.Lines)
	}
	var types []string
	for _, c := range r.Changes {
		types = append(types, string(c.Type))
	}
	slices.Sort(types)
	if !reflect.DeepEqual(types, []string{"ADDED", "DELETED", "RENAMED"}) {
		t.Errorf("types %v", types)
	}
}

func TestOverBudgetUsesFactor(t *testing.T) {
	e := newEnv(t)
	e.specs = []spec.StepSpec{budgetSpec("S1", 5, 10)}
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("src/a.txt", lines(15))
	if mustRec(t)(s.Finish("S1", passedRun())).Check.OverBudget {
		t.Error("15 dòng không được vượt")
	}
	mustRec(t)(s.Accept("S1", "dev1"))

	s2 := e.service()
	if err := os.Remove(e.paths.Steps()); err != nil {
		t.Fatal(err)
	}
	mustRec(t)(s2.Start("S1"))
	e.write("src/b.txt", lines(16))
	if !mustRec(t)(s2.Finish("S1", passedRun())).Check.OverBudget {
		t.Error("31 dòng phải vượt")
	}
}

func TestOutOfScopeListsPathAndExtrasWhitelist(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("pom.xml", "<project>\n<x/>\n</project>\n")
	e.write("src/ok.txt", "x\n")
	r := mustRec(t)(s.Finish("S1", passedRun()))
	if !reflect.DeepEqual(r.Check.OutOfScope, []string{"pom.xml"}) {
		t.Errorf("outOfScope %v", r.Check.OutOfScope)
	}

	extras := NewScopeExtras(e.paths)
	if err := extras.Add("pom.xml", "dev1", t0); err != nil {
		t.Fatal(err)
	}
	if p, _ := extras.Paths(); !slices.Contains(p, "pom.xml") {
		t.Errorf("paths %v", p)
	}
	g, err := s.AllowedGlobs("S1")
	if err != nil || !slices.Contains(g, "pom.xml") || !slices.Contains(g, "src/**") {
		t.Errorf("globs %v %v", g, err)
	}
	if err := os.Remove(e.paths.Steps()); err != nil {
		t.Fatal(err)
	}
	s = e.service()
	mustRec(t)(s.Start("S1"))
	e.write("pom.xml", "<project>\n<y/>\n</project>\n")
	if out := mustRec(t)(s.Finish("S1", passedRun())).Check.OutOfScope; len(out) != 0 {
		t.Errorf("outOfScope %v", out)
	}
}

func TestUsesScopeExtras(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("pom.xml", "<project>\n<x/>\n</project>\n")
	e.write("src/ok.txt", "x\n")
	mustRec(t)(s.Finish("S1", passedRun()))
	if u, err := s.UsesScopeExtras("S1"); err != nil || u {
		t.Errorf("uses=%v err=%v", u, err)
	}
	if err := NewScopeExtras(e.paths).Add("pom.xml", "dev1", t0); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UsesScopeExtras("S1"); err != nil || !u {
		t.Errorf("uses=%v err=%v", u, err)
	}
	if u, _ := s.UsesScopeExtras("S2"); u {
		t.Error("S2 chưa làm")
	}
}

func TestFeatureDirChangesIgnored(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("aisdlc/features/PAY-123/feedback.md", "note\n")
	e.write("README.md", "doc\n")
	e.write("src/a.txt", "x\n")
	r := mustRec(t)(s.Finish("S1", passedRun()))
	if got := paths2(r.Changes); !reflect.DeepEqual(got, []string{"src/a.txt"}) {
		t.Errorf("changes %v", got)
	}
}

func TestCheckReportsCasesNotPassed(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	res := run.CaseRunResult{ExitCode: 1, Cases: []run.CaseResult{
		{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed},
		{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "sai"}}}
	c := mustRec(t)(s.Finish("S1", res)).Check
	if c.CasesPassed != 1 || !reflect.DeepEqual(c.CasesNotPassed, []string{"C2"}) {
		t.Errorf("check %+v", c)
	}
}

func TestRestartAwaitingStepKeepsStartTree(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	start := mustRec(t)(s.Start("S1")).StartTree
	e.write("src/a.txt", "1\n2\n")
	mustRec(t)(s.Finish("S1", passedRun()))
	again := mustRec(t)(s.Start("S1"))
	if again.Status != StepInProgress || again.StartTree != start || again.EndTree != "" || again.Check != nil {
		t.Errorf("again %+v", again)
	}
	e.write("src/b.txt", "3\n")
	done := mustRec(t)(s.Finish("S1", passedRun()))
	if done.Check.Files != 2 || done.Check.Lines != 3 {
		t.Errorf("check %+v", done.Check)
	}
}

func TestCannotRestartAcceptedStep(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	mustRec(t)(s.Finish("S1", passedRun()))
	mustRec(t)(s.Accept("S1", "dev1"))
	if _, err := s.Start("S1"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}

func TestBinaryFileCountsAsOneFileZeroLines(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	if err := os.WriteFile(filepath.Join(e.dir, "src/img.bin"), []byte{0, 1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	r := mustRec(t)(s.Finish("S1", passedRun()))
	if r.Check.Files != 1 || r.Check.Lines != 0 || !r.Changes[0].Binary {
		t.Errorf("r %+v", r)
	}
}

func TestUnmanagedFilesExcluded(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	e.write("docs/x.md", "doc\n")
	e.write("src/a.txt", "x\n")
	r := mustRec(t)(s.Finish("S1", passedRun()))
	if got := paths2(r.Changes); !reflect.DeepEqual(got, []string{"src/a.txt"}) {
		t.Errorf("changes %v", got)
	}
}

func TestOverBudgetTriggersOnFilesDimension(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	for i := 0; i < 7; i++ {
		e.write(fmt.Sprintf("src/f%d.txt", i), "x\n")
	}
	if mustRec(t)(s.Finish("S1", passedRun())).Check.OverBudget {
		t.Error("7 file chưa vượt")
	}
	mustRec(t)(s.Start("S1"))
	e.write("src/f7.txt", "x\n")
	if !mustRec(t)(s.Finish("S1", passedRun())).Check.OverBudget {
		t.Error("8 file phải vượt")
	}
}

func TestStepsJSONFieldNames(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	s := e.service()
	mustRec(t)(s.Start("S1"))
	b, err := os.ReadFile(e.paths.Steps())
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	r := raw[0]
	for _, k := range []string{"id", "status", "startTree", "endTree", "changes", "check", "acceptedBy"} {
		if _, ok := r[k]; !ok {
			t.Errorf("thiếu khóa %s", k)
		}
	}
	if r["endTree"] != nil || r["check"] != nil || r["status"] != "IN_PROGRESS" {
		t.Errorf("record %v", r)
	}
}

func TestStepsJSONAtomicUnderConcurrentStart(t *testing.T) {
	e := newEnv(t)
	e.specs = []spec.StepSpec{budgetSpec("S1", 50, 1000)}
	// hai tính năng cùng repo
	other := model.FeaturePaths{RepoRoot: e.dir, Key: "PAY-456"}
	write(t, e.dir, "aisdlc/features/PAY-456/cases.yaml", "cases: []\n")
	write(t, e.dir, "aisdlc/features/PAY-456/schema-expected.json", "{}\n")
	gates2 := gate.NewService(other, model.FlowLite, e.cfg, gate.NewLocalGateStore(other), clock)
	for _, g := range []*gate.Service{e.gates, gates2} {
		if _, err := g.Submit(gate.CASES, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Approve(gate.CASES, "dev1", ""); err != nil {
			t.Fatal(err)
		}
	}
	sh(t, e.dir, "add", "-A")
	sh(t, e.dir, "commit", "-qm", "init")

	svcs := []*Service{
		NewService(e.paths, e.git, e.specs, e.cfg, clock).WithGates(e.gates),
		NewService(other, e.git, e.specs, e.cfg, clock).WithGates(gates2),
	}
	files := []string{e.paths.Steps(), other.Steps()}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := range svcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				if _, err := svcs[i].Start("S1"); err != nil {
					errs <- fmt.Errorf("start %d: %w", n, err)
					return
				}
				b, err := os.ReadFile(files[i])
				var recs []StepRecord
				if err == nil {
					err = json.Unmarshal(b, &recs)
				}
				if err != nil {
					errs <- fmt.Errorf("steps.json không parse được sau start %d: %w", n, err)
					return
				}
				if _, err := svcs[i].Finish("S1", passedRun()); err != nil {
					errs <- fmt.Errorf("finish %d: %w", n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		var recs []StepRecord
		if err == nil {
			err = json.Unmarshal(b, &recs)
		}
		if err != nil || len(recs) != 1 || recs[0].Status != StepAwaitingDev {
			t.Errorf("%s: %v %+v", f, err, recs)
		}
	}
}

func TestStartRefusesWithTypedNilGates(t *testing.T) {
	e := newEnv(t)
	e.openGates()
	var nilGates *gate.Service
	s := NewService(e.paths, e.git, e.specs, e.cfg, clock).WithGates(nilGates)
	if _, err := s.Start("S1"); !isStepErr(err) {
		t.Errorf("want StepError, got %v", err)
	}
}
