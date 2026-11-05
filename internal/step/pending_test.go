package step

import (
	"os"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
)

var now = time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)

type pend struct {
	*env
	extras *ScopeExtras
	steps  *Service
}

func newPend(t *testing.T) *pend {
	e := newEnv(t)
	extras := NewScopeExtras(e.paths)
	if _, err := e.gates.Submit(gate.CASES, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.gates.Approve(gate.CASES, "dev1", ""); err != nil {
		t.Fatal(err)
	}
	sh(t, e.dir, "add", "-A")
	sh(t, e.dir, "commit", "-qm", "init")
	steps := NewService(e.paths, gitx.New(e.dir), []spec.StepSpec{budgetSpec("S1", 5, 10)}, e.cfg, clock).WithGates(e.gates)
	return &pend{e, extras, steps}
}

func (p *pend) finishS1() string {
	p.t.Helper()
	mustRec(p.t)(p.steps.Start("S1"))
	p.write("src/a.txt", "x\n")
	return mustRec(p.t)(p.steps.Finish("S1", passedRun())).EndTree
}

func ctl(a ...snapshot.Action) snapshot.FeatureControl {
	return snapshot.FeatureControl{Repo: "r", Feature: "PAY-123", IssuedAt: now, Actions: a}
}

func acceptAct(id int64, endTree string) snapshot.Action {
	exp := now.Add(time.Hour)
	return snapshot.Action{ID: id, Kind: "ACCEPT_STEP", Step: "S1", EndTree: endTree, By: "an", At: now, ExpiresAt: &exp}
}

func allowAct(id int64, step, path string) snapshot.Action {
	exp := now.Add(time.Hour)
	return snapshot.Action{ID: id, Kind: "ALLOW_SCOPE", Step: step, Path: path, By: "an", At: now, ExpiresAt: &exp}
}

func (p *pend) run(c snapshot.FeatureControl) []PendingResult {
	return ApplyPending(c, p.steps, p.extras, map[int64]bool{}, now)
}

func TestAcceptApplies(t *testing.T) {
	p := newPend(t)
	end := p.finishS1()
	r := p.run(ctl(acceptAct(1, end)))
	if len(r) != 1 || r[0].Status != PendingApplied {
		t.Fatalf("r %+v", r)
	}
	all := mustAll(t, p.steps)
	if all[0].Status != StepAccepted || all[0].AcceptedBy != "an (web)" {
		t.Errorf("rec %+v", all[0])
	}
}

func TestAcceptIdempotent(t *testing.T) {
	p := newPend(t)
	end := p.finishS1()
	p.run(ctl(acceptAct(1, end)))
	r := p.run(ctl(acceptAct(1, end)))
	if r[0].Status != PendingApplied || mustAll(t, p.steps)[0].Status != StepAccepted {
		t.Errorf("r %+v", r)
	}
}

func TestAcceptStaleWhenEndTreeChanged(t *testing.T) {
	p := newPend(t)
	p.finishS1()
	before, _ := os.ReadFile(p.paths.Steps())
	r := p.run(ctl(acceptAct(1, "OLDTREE")))
	if r[0].Status != PendingStale || !strings.Contains(r[0].Reason, "đã đổi sau khi dev bấm") {
		t.Errorf("r %+v", r)
	}
	after, _ := os.ReadFile(p.paths.Steps())
	if string(before) != string(after) {
		t.Error("steps.json bị đổi")
	}
}

func TestAcceptStaleWhenInProgress(t *testing.T) {
	p := newPend(t)
	mustRec(t)(p.steps.Start("S1"))
	r := p.run(ctl(acceptAct(1, "T")))
	if r[0].Status != PendingStale || !strings.Contains(r[0].Reason, "IN_PROGRESS") {
		t.Errorf("r %+v", r)
	}
}

func TestAllowScopeApplies(t *testing.T) {
	p := newPend(t)
	r := p.run(ctl(allowAct(1, "S1", "src/x/A.java")))
	if r[0].Status != PendingApplied {
		t.Fatalf("r %+v", r)
	}
	if ps, _ := p.extras.Paths(); len(ps) != 1 || ps[0] != "src/x/A.java" {
		t.Errorf("paths %v", ps)
	}
	b, _ := os.ReadFile(p.paths.ScopeExtra())
	if !strings.Contains(string(b), "an (web)") {
		t.Errorf("file %s", b)
	}
}

func TestAllowScopeUnknownStepStale(t *testing.T) {
	p := newPend(t)
	r := p.run(ctl(allowAct(1, "S9", "src/x/A.java")))
	if r[0].Status != PendingStale {
		t.Errorf("r %+v", r)
	}
	if ps, _ := p.extras.Paths(); len(ps) != 0 {
		t.Errorf("paths %v", ps)
	}
}

func TestAllowScopeBadPathStale(t *testing.T) {
	p := newPend(t)
	bad := []string{"../x", "/etc/passwd", ".aisdlc/a", "a/../b", "", "**", ".aisdl?/**", "{.aisdlc,src}/**",
		"./.aisdlc/x", "C:/x", "a//b", "a/./b"}
	var as []snapshot.Action
	for i, b := range bad {
		as = append(as, allowAct(int64(i+1), "S1", b))
	}
	r := p.run(ctl(as...))
	if len(r) != 12 {
		t.Fatalf("len %d", len(r))
	}
	for _, x := range r {
		if x.Status != PendingStale {
			t.Errorf("%+v", x)
		}
	}
	if _, err := os.Stat(p.paths.ScopeExtra()); !os.IsNotExist(err) {
		t.Error("scope-extra.json không được tồn tại")
	}
}

func TestExpiredStale(t *testing.T) {
	p := newPend(t)
	end := p.finishS1()
	r := ApplyPending(ctl(acceptAct(1, end)), p.steps, p.extras, map[int64]bool{}, now.Add(2*time.Hour))
	if r[0].Status != PendingStale || r[0].Reason != "Lệnh đã hết hạn (7 ngày)" {
		t.Errorf("r %+v", r)
	}
	if mustAll(t, p.steps)[0].Status != StepAwaitingDev {
		t.Error("bước không được đổi")
	}
}

func TestSkipsDone(t *testing.T) {
	p := newPend(t)
	r := ApplyPending(ctl(allowAct(1, "S1", "src/a"), allowAct(2, "S1", "src/b")), p.steps, p.extras,
		map[int64]bool{1: true}, now)
	if len(r) != 1 || r[0].ID != 2 {
		t.Fatalf("r %+v", r)
	}
	if ps, _ := p.extras.Paths(); len(ps) != 1 || ps[0] != "src/b" {
		t.Errorf("paths %v", ps)
	}
}
