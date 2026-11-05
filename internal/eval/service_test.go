package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/harness"
	"aisdlc/internal/lang"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

const evalCases = `scope: ["limit/**"]
budget: { files: 5, lines: 100 }
cases:
  - { id: C1, step: S1, kind: happy, title: hạn mức còn }
  - { id: C2, step: S1, kind: error, title: vượt hạn mức }
`

var evalSchema = run.NewSchemaSnapshot(map[string][]string{"transfer": {"amount:BIGINT:NOT NULL"}})

// limitCoverage: limit/limit.go dòng 3..9 mới/sửa; đo được 5, 6 (chạy), 7 (không), 9 (chạy) → 3/4.
func limitCoverage() lang.Coverage {
	return lang.Coverage{Lines: map[string]map[int]bool{"limit/limit.go": {5: true, 6: true, 7: false, 9: true}}}
}

// fakeVerifier không build/chạy gì: ghi results.json, schema.json như harness thật rồi trả VerifyResult dựng sẵn.
type fakeVerifier struct {
	rows   []run.CaseResult
	schema *run.SchemaSnapshot
	res    harness.VerifyResult
	err    error
	calls  int
	outs   []string
}

func (f *fakeVerifier) Verify(_ context.Context, key model.FeatureKey, out string) (harness.VerifyResult, error) {
	f.calls++
	f.outs = append(f.outs, out)
	if f.err != nil {
		return harness.VerifyResult{}, f.err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return harness.VerifyResult{}, err
	}
	if err := harness.WriteResults(out, key.String(), "cases", f.rows); err != nil {
		return harness.VerifyResult{}, err
	}
	if f.schema != nil {
		if err := f.schema.Write(filepath.Join(out, "schema.json")); err != nil {
			return harness.VerifyResult{}, err
		}
	}
	return f.res, nil
}

type evalEnv struct {
	t     *testing.T
	dir   string
	key   model.FeatureKey
	paths model.FeaturePaths
	git   *gitx.Repo
	flow  model.Flow
	v     *fakeVerifier
}

func clock() time.Time { return t0 }

func (e *evalEnv) sh(args ...string) {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = e.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func (e *evalEnv) write(rel, content string) {
	e.t.Helper()
	f := filepath.Join(e.dir, rel)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *evalEnv) read(rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *evalEnv) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *evalEnv) config(yaml string) { e.write("aisdlc/config.yaml", yaml) }

func (e *evalEnv) cfg() model.RepoConfig {
	e.t.Helper()
	c, err := model.LoadRepoConfig(e.dir)
	e.must(err)
	return c
}

func (e *evalEnv) gates() *gate.Service {
	return gate.NewService(e.paths, e.flow, e.cfg(), gate.NewLocalGateStore(e.paths), clock)
}

func (e *evalEnv) approve(g gate.Gate) {
	e.t.Helper()
	s := e.gates()
	_, err := s.Submit(g, nil)
	e.must(err)
	_, err = s.Approve(g, "dev", "")
	e.must(err)
}

func (e *evalEnv) tree() string {
	e.t.Helper()
	t, err := e.git.SnapshotTreeExcluding(context.Background(), e.paths.Rel(e.paths.Dir()))
	e.must(err)
	return t
}

// newEvalEnv như setUp của EvalServiceTest: tính năng LITE PAY-123, cổng Case đã duyệt tay, limit/limit.go đã sửa
// trong phạm vi.
func newEvalEnv(t *testing.T) *evalEnv {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e := &evalEnv{t: t, dir: dir, key: "PAY-123", flow: model.FlowLite,
		v: &fakeVerifier{
			rows: []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed},
				{ID: "C2", Step: "S1", Outcome: run.CaseOutcomePassed}},
			schema: &evalSchema,
			res: harness.VerifyResult{Build: lang.BuildResult{OK: true, Binary: "/x/server"},
				Coverage: lang.Coverage{NA: "chưa đo coverage"}},
		}}
	e.paths = model.FeaturePaths{RepoRoot: dir, Key: e.key}
	e.sh("init", "-q")
	e.sh("config", "user.email", "t@t")
	e.sh("config", "user.name", "t")
	e.write(".gitignore", ".aisdlc/\n")
	e.write("go.mod", "module ex\n\ngo 1.22\n")
	e.write("limit/limit.go", "package limit\n\ntype Limit struct{}\n")
	e.config("unmanaged: [\"*.md\"]\n")
	e.sh("add", "-A")
	e.sh("commit", "-q", "-m", "base")
	e.git = gitx.New(dir)
	base, err := e.git.SnapshotTree(context.Background())
	e.must(err)
	e.must(model.FeatureMeta{Key: e.key, Title: "Hạn mức", Flow: model.FlowLite, BaseTree: base, CreatedAt: t0}.
		Write(e.paths.Meta()))
	e.write("aisdlc/features/PAY-123/cases.yaml", evalCases)
	e.must(evalSchema.Write(e.paths.SchemaExpected()))
	e.approve(gate.CASES)
	// code của tính năng, trong phạm vi
	e.write("limit/limit.go", "package limit\n\ntype Limit struct {\n\tmax  int\n\tused int\n}\n\n"+
		"func (l Limit) Left() int { return l.max - l.used }\nvar x int\n")
	return e
}

func (e *evalEnv) service() *Service {
	e.t.Helper()
	cfg := e.cfg()
	meta, err := model.ReadFeatureMeta(e.paths.Meta())
	e.must(err)
	cases, err := spec.ReadCases(e.paths.Cases())
	e.must(err)
	var design *spec.DesignSpec
	if e.flow == model.FlowFull {
		if d, err := spec.ReadDesign(e.paths.DesignYaml()); err == nil {
			design = &d
		}
	}
	gates := e.gates()
	specs := spec.EffectiveSteps(e.flow, design, cases)
	steps := step.NewService(e.paths, e.git, specs, cfg, clock).WithGates(gates)
	return &Service{Paths: e.paths, Meta: meta, Config: cfg, Git: e.git, Verifier: e.v, Gates: gates, Steps: steps,
		Specs: specs, Adapter: golang.Adapter{}, Now: clock}
}

func (e *evalEnv) run(ci bool) EvalReport {
	e.t.Helper()
	base := ""
	if ci {
		base = "HEAD"
	}
	r, err := e.service().Run(context.Background(), ci, base, nil)
	e.must(err)
	return r
}

func (e *evalEnv) risk(level string, reqs ...risk.Requirement) {
	e.t.Helper()
	e.must(risk.RiskFile{Level: level, Policy: risk.PolicyRef{Version: 3, Hash: "h"},
		Matched: []risk.Match{{Rule: "r1", Level: level, On: "src/x"}}, Requirements: reqs}.Write(e.paths.Risk()))
	e.approve(gate.CASES)
}

func req(id string, kv ...any) risk.Requirement {
	m := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return risk.NewRequirement(id, m)
}

func hardOf(t *testing.T, r EvalReport, id string) HardCheck {
	t.Helper()
	for _, h := range r.Hard {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("không có hard check %s trong %+v", id, r.Hard)
	return HardCheck{}
}

func mustSoft(t *testing.T, r EvalReport, id string) SoftCheck {
	t.Helper()
	for _, s := range r.Soft {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("không có soft check %s trong %+v", id, r.Soft)
	return SoftCheck{}
}

func hardIDs(r EvalReport) []string {
	var ids []string
	for _, h := range r.Hard {
		ids = append(ids, h.ID)
	}
	return ids
}

func softIDs(r EvalReport) []string {
	var ids []string
	for _, s := range r.Soft {
		ids = append(ids, s.ID)
	}
	return ids
}

func has(ids []string, pred func(string) bool) bool {
	for _, id := range ids {
		if pred(id) {
			return true
		}
	}
	return false
}

func is(id string) func(string) bool { return func(s string) bool { return s == id } }

func prefix(p string) func(string) bool {
	return func(s string) bool { return strings.HasPrefix(s, p) }
}

func wantContains(t *testing.T, what, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Errorf("%s %q thiếu %q", what, s, p)
		}
	}
}

func wantNotContains(t *testing.T, what, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(s, p) {
			t.Errorf("%s %q không được chứa %q", what, s, p)
		}
	}
}

func TestEvalAllGreenPasses(t *testing.T) {
	e := newEvalEnv(t)
	r := e.run(false)

	eq(t, "hard ids", hardIDs(r), []string{"CASES_ALL_PASS", "CASES_LOCKED", "BUILD_AND_TESTS", "SCHEMA_MATCH", "SCOPE"})
	for _, h := range r.Hard {
		if !h.Passed {
			t.Errorf("hard %+v", h)
		}
	}
	eq(t, "passed", r.Passed, true)
	eq(t, "feature", r.Feature, "PAY-123")
	eq(t, "mode", r.Mode, "local")
	eq(t, "at", r.At, t0)
	eq(t, "cases", hardOf(t, r, "CASES_ALL_PASS").Detail, "2/2 case pass")
	eq(t, "budget", mustSoft(t, r, "BUDGET:S1").Level, SoftNA)
	eq(t, "coverage", mustSoft(t, r, "COVERAGE_CHANGED_LINES").Level, SoftNA)
	eq(t, "agent", mustSoft(t, r, "AGENT_REVIEW").Level, SoftNA)
	eq(t, "verify calls", e.v.calls, 1)
	eq(t, "out", e.v.outs[0], filepath.Join(e.dir, ".aisdlc", "run", "PAY-123"))
	eq(t, "tree", r.EvaluatedTree, e.tree())

	back, err := ReadEvalReport(filepath.Join(e.paths.EvalDir(), "report.json"))
	e.must(err)
	eq(t, "report.json", back, r)
	md := e.read("aisdlc/features/PAY-123/eval/report.md")
	eq(t, "md line 0", strings.SplitN(md, "\n", 2)[0], "Eval PAY-123: ĐẠT")
	if _, err := os.Stat(filepath.Join(e.paths.EvalDir(), "diff-by-step.md")); err != nil {
		t.Error(err)
	}
}

func TestEvalFailingCaseFailsCasesAllPass(t *testing.T) {
	e := newEvalEnv(t)
	e.v.rows[1] = run.CaseResult{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "mong đợi 422, thực tế 200"}

	r := e.run(false)

	eq(t, "passed", r.Passed, false)
	c := hardOf(t, r, "CASES_ALL_PASS")
	eq(t, "cases", c.Passed, false)
	wantContains(t, "detail", c.Detail, "1/2", "C2")
	md := e.read("aisdlc/features/PAY-123/eval/report.md")
	if !strings.HasPrefix(md, "Eval PAY-123: KHÔNG ĐẠT") {
		t.Errorf("md %q", md)
	}
	wantContains(t, "md", md, "C2", "mong đợi 422, thực tế 200")
}

func TestEvalCaseMissingFromResultsFailsCasesAllPass(t *testing.T) {
	e := newEvalEnv(t)
	e.v.rows = e.v.rows[:1] // harness chỉ ghi C1: không được coi là "1/1 pass"

	r := e.run(false)

	c := hardOf(t, r, "CASES_ALL_PASS")
	eq(t, "cases", c.Passed, false)
	wantContains(t, "detail", c.Detail, "1/2", "C2")
	wantContains(t, "md", e.read("aisdlc/features/PAY-123/eval/report.md"), harness.NoRow)
}

func TestEvalEmptyResultsFailCasesAllPass(t *testing.T) {
	e := newEvalEnv(t)
	e.v.rows = nil

	r := e.run(false)

	c := hardOf(t, r, "CASES_ALL_PASS")
	eq(t, "cases", c.Passed, false)
	wantContains(t, "detail", c.Detail, "0/2")
}

func TestEvalEditedCasesFailsLockLocallyButWarnsInCi(t *testing.T) {
	e := newEvalEnv(t)
	e.write("aisdlc/features/PAY-123/cases.yaml", evalCases+"# sửa sau khi duyệt\n")

	local := e.run(false)
	eq(t, "locked", hardOf(t, local, "CASES_LOCKED").Passed, false)
	eq(t, "local passed", local.Passed, false)

	ci := e.run(true)
	eq(t, "mode", ci.Mode, "ci")
	if has(hardIDs(ci), is("CASES_LOCKED")) {
		t.Errorf("hard %v", hardIDs(ci))
	}
	eq(t, "soft locked", mustSoft(t, ci, "CASES_LOCKED").Level, SoftWarn)
	if !ci.Passed {
		t.Errorf("ci %+v", ci.Hard)
	}
}

func TestEvalCiWarnsOnMissingOrDriftedApprovalOfOtherGates(t *testing.T) {
	e := newEvalEnv(t)
	ci := e.run(true)
	eq(t, "review", mustSoft(t, ci, "GATE:REVIEW"), SoftCheck{"GATE:REVIEW", SoftWarn, "chưa duyệt REVIEW"})
	if has(softIDs(ci), is("GATE:CASES")) {
		t.Errorf("soft %v", softIDs(ci))
	}
	eq(t, "ci passed", ci.Passed, true)
	local := e.run(false)
	if has(softIDs(local), is("GATE:REVIEW")) {
		t.Errorf("local soft %v", softIDs(local))
	}

	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("DAT"), ""))
	e.approve(gate.REVIEW)
	ci = e.run(true)
	if has(softIDs(ci), is("GATE:REVIEW")) {
		t.Errorf("soft %v", softIDs(ci))
	}

	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("CAN_SUA"), ""))
	ci = e.run(true)
	eq(t, "drift", mustSoft(t, ci, "GATE:REVIEW"), SoftCheck{"GATE:REVIEW", SoftWarn, "REVIEW đã đổi sau khi duyệt"})
	eq(t, "ci passed", ci.Passed, true)
}

func TestEvalWebModeWithoutSourceNamesVariables(t *testing.T) {
	e := newEvalEnv(t)
	e.config("unmanaged: [\"*.md\"]\napprovals: web\n")
	e.sh("add", "aisdlc/config.yaml")
	e.sh("commit", "-q", "-m", "web mode")

	r, err := e.service().Run(context.Background(), true, "HEAD", nil)
	e.must(err)

	w := hardOf(t, r, "GATE:WEB")
	eq(t, "web", w, HardCheck{"GATE:WEB", false, "Thiếu biến AISDLC_WEB_URL hoặc AISDLC_CI_TOKEN để hỏi web (chế độ web)"})
	wantContains(t, "detail", w.Detail, "AISDLC_WEB_URL", "AISDLC_CI_TOKEN")
	eq(t, "passed", r.Passed, false)
	// GATE:WEB thay cho CASES_LOCKED; không kiểm gates.json
	if has(hardIDs(r), is("CASES_LOCKED")) || has(softIDs(r), is("CASES_LOCKED")) || has(softIDs(r), prefix("GATE:")) ||
		has(hardIDs(r), prefix("GATE_AUTO")) {
		t.Errorf("hard %v soft %v", hardIDs(r), softIDs(r))
	}
	// local thì không hỏi web
	if has(hardIDs(e.run(false)), prefix("GATE:")) {
		t.Error("local không được có GATE:*")
	}
}

func TestEvalUnchangedCasesInCiIsOkSoftCheck(t *testing.T) {
	e := newEvalEnv(t)
	eq(t, "locked", mustSoft(t, e.run(true), "CASES_LOCKED").Level, SoftOK)
}

func TestEvalOldTestFailureFailsBuild(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Unit = lang.UnitResult{Exit: 1, Failed: []string{"ex/limit.TestBoom", "ex/limit.TestWorks"}}

	b := hardOf(t, e.run(false), "BUILD_AND_TESTS")

	eq(t, "passed", b.Passed, false)
	eq(t, "detail", b.Detail, "unit test fail: ex/limit.TestBoom, ex/limit.TestWorks; xem .aisdlc/run/PAY-123/harness.log")
}

func TestEvalUnitFailuresListedUpToTen(t *testing.T) {
	e := newEvalEnv(t)
	var failed []string
	for _, c := range "abcdefghijkl" {
		failed = append(failed, "ex/limit.Test"+string(c))
	}
	e.v.res.Unit = lang.UnitResult{Exit: 1, Failed: failed}

	b := hardOf(t, e.run(false), "BUILD_AND_TESTS")

	eq(t, "detail", b.Detail, "unit test fail: "+strings.Join(failed[:10], ", ")+" … và 2 mục khác; xem .aisdlc/run/PAY-123/harness.log")
}

func TestEvalOnlySuiteFailureDoesNotFailBuild(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Run = harness.RunResult{ExitCode: 1}
	e.v.rows[1] = run.CaseResult{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "mong đợi 200"}

	r := e.run(false)

	b := hardOf(t, r, "BUILD_AND_TESTS")
	if !b.Passed {
		t.Errorf("build %+v", b)
	}
	eq(t, "cases", hardOf(t, r, "CASES_ALL_PASS").Passed, false)
}

func TestEvalNonZeroExitWithoutTestFailuresFailsBuild(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Unit = lang.UnitResult{Exit: 2} // ví dụ package không biên dịch được: không có test nào fail

	b := hardOf(t, e.run(false), "BUILD_AND_TESTS")

	eq(t, "passed", b.Passed, false)
	eq(t, "detail", b.Detail, "go test thoát mã 2; xem .aisdlc/run/PAY-123/harness.log")
}

func TestEvalBuildFailure(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Build = lang.BuildResult{OK: false, Output: "limit.go:3: syntax error"}
	e.v.res.Unit = lang.UnitResult{Exit: 1} // không được báo thêm "go test thoát mã" khi build hỏng
	e.v.rows = []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomeError, Message: "build lỗi"},
		{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeError, Message: "build lỗi"}}

	r := e.run(false)

	eq(t, "build", hardOf(t, r, "BUILD_AND_TESTS"), HardCheck{"BUILD_AND_TESTS", false, "go build lỗi; xem .aisdlc/run/PAY-123/harness.log"})
	eq(t, "cases", hardOf(t, r, "CASES_ALL_PASS").Passed, false)
	eq(t, "passed", r.Passed, false)
}

func TestEvalTimeoutFailsBuild(t *testing.T) {
	e := newEvalEnv(t)
	msg := harness.TimeoutMessage(30)
	e.v.res.TimedOut = true
	e.v.res.Coverage = lang.Coverage{NA: msg}
	e.v.rows = []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomeError, Message: msg},
		{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeError, Message: msg}}

	r := e.run(false)

	eq(t, "build", hardOf(t, r, "BUILD_AND_TESTS"), HardCheck{"BUILD_AND_TESTS", false, "quá thời gian; xem .aisdlc/run/PAY-123/harness.log"})
	eq(t, "coverage", mustSoft(t, r, "COVERAGE_CHANGED_LINES"), SoftCheck{"COVERAGE_CHANGED_LINES", SoftNA, msg})
	eq(t, "passed", r.Passed, false)
}

func TestEvalVerifyErrorIsReturned(t *testing.T) {
	e := newEvalEnv(t)
	e.v.err = errors.New("Không ghi được out")

	_, err := e.service().Run(context.Background(), false, "", nil)

	if err == nil || !strings.Contains(err.Error(), "Không ghi được out") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.EvalDir(), "report.json")); err == nil {
		t.Error("không được ghi report.json khi Verify lỗi")
	}
}

func TestEvalExtraColumnFailsSchemaMatch(t *testing.T) {
	e := newEvalEnv(t)
	s := run.NewSchemaSnapshot(map[string][]string{"transfer": {"amount:BIGINT:NOT NULL", "note:VARCHAR:NULL"}})
	e.v.schema = &s

	c := hardOf(t, e.run(false), "SCHEMA_MATCH")

	eq(t, "passed", c.Passed, false)
	wantContains(t, "detail", c.Detail, "+transfer.note")
}

func TestEvalMissingSchemaDumpFailsSchemaMatch(t *testing.T) {
	e := newEvalEnv(t)
	e.v.schema = nil

	c := hardOf(t, e.run(false), "SCHEMA_MATCH")

	eq(t, "check", c, HardCheck{"SCHEMA_MATCH", false, "Harness không ghi schema.json"})
}

func TestEvalBashWrittenOutOfScopeFileFailsScope(t *testing.T) {
	e := newEvalEnv(t)
	// tạo thẳng trên đĩa (như `cat > ...` qua Bash), không qua hook, chưa track
	e.write("scripts/hack.sh", "echo hi\n")
	e.write("README.md", "unmanaged\n")

	s := hardOf(t, e.run(false), "SCOPE")

	eq(t, "passed", s.Passed, false)
	wantContains(t, "detail", s.Detail, "scripts/hack.sh")
	wantNotContains(t, "detail", s.Detail, "README.md", "limit.go", "aisdlc/features")
}

func TestEvalScopeExtraAllowsFile(t *testing.T) {
	e := newEvalEnv(t)
	e.write("scripts/hack.sh", "echo hi\n")
	e.must(step.NewScopeExtras(e.paths).Add("scripts/hack.sh", "dev", t0))

	s := hardOf(t, e.run(false), "SCOPE")

	if !s.Passed {
		t.Errorf("scope %+v", s)
	}
}

func TestEvalCiScopeUsesMergeBaseOfBaseRef(t *testing.T) {
	e := newEvalEnv(t)
	e.write("scripts/hack.sh", "echo hi\n")

	s := hardOf(t, e.run(true), "SCOPE")

	eq(t, "passed", s.Passed, false)
	wantContains(t, "detail", s.Detail, "scripts/hack.sh")
}

func TestEvalCheckCommandNonZeroFails(t *testing.T) {
	e := newEvalEnv(t)
	e.config("unmanaged: [\"*.md\"]\nchecks:\n  - { name: lint, cmd: \"echo lỗi lint; exit 3\" }\n  - { name: sec, cmd: \"true\" }\n")

	r := e.run(false)

	lint := hardOf(t, r, "CHECK:lint")
	eq(t, "lint", lint, HardCheck{"CHECK:lint", false, "exit 3; xem .aisdlc/run/PAY-123/check-lint.log"})
	wantContains(t, "log", e.read(".aisdlc/run/PAY-123/check-lint.log"), "lỗi lint")
	eq(t, "sec", hardOf(t, r, "CHECK:sec"), HardCheck{"CHECK:sec", true, "exit 0"})
	eq(t, "passed", r.Passed, false)
}

func TestEvalCoverageOfChangedLines(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()

	r := e.run(false)

	eq(t, "coverage", mustSoft(t, r, "COVERAGE_CHANGED_LINES"),
		SoftCheck{"COVERAGE_CHANGED_LINES", SoftWarn, "75% (3/4 dòng mới/sửa)"})
}

// Dịch vụ phớt lờ SIGTERM: coverage N/A kèm lý do, kết quả case vẫn giữ.
func TestEvalCoverageNAKeepsReasonAndCaseResults(t *testing.T) {
	e := newEvalEnv(t)
	why := "dịch vụ bị SIGKILL sau 15 giây vì không tắt êm: không có coverage lúc chạy case"
	e.v.res.Coverage = lang.Coverage{NA: why}

	r := e.run(false)

	eq(t, "coverage", mustSoft(t, r, "COVERAGE_CHANGED_LINES"), SoftCheck{"COVERAGE_CHANGED_LINES", SoftNA, why})
	eq(t, "cases", hardOf(t, r, "CASES_ALL_PASS").Passed, true)
	eq(t, "passed", r.Passed, true)
}

func TestEvalCoverageHardWhenRiskRequires(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.risk("high", req("coverage", "min", 90))

	r := e.run(false)

	c := hardOf(t, r, "RISK_COVERAGE")
	eq(t, "passed", c.Passed, false)
	eq(t, "detail", c.Detail, "75% (3/4 dòng mới/sửa) < 90%")
	eq(t, "report", r.Passed, false)
	if has(softIDs(r), is("COVERAGE_CHANGED_LINES")) {
		t.Errorf("soft %v", softIDs(r))
	}
}

func TestEvalRiskFileUnreadableHardFail(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.write("aisdlc/features/PAY-123/risk.yaml", "level: [unclosed\n  : :")

	r := e.run(false)

	c := hardOf(t, r, "RISK_FILE")
	eq(t, "passed", c.Passed, false)
	wantContains(t, "detail", c.Detail, "Không đọc được risk.yaml: ")
	eq(t, "report", r.Passed, false)
}

func (e *evalEnv) full(design string) {
	e.t.Helper()
	base, err := e.git.SnapshotTree(context.Background())
	e.must(err)
	e.must(model.FeatureMeta{Key: e.key, Title: "Hạn mức", Flow: model.FlowFull, BaseTree: base, CreatedAt: t0}.
		Write(e.paths.Meta()))
	e.write("aisdlc/features/PAY-123/design.yaml", design)
}

func TestEvalFullWithImpactButNoRiskFileHardFail(t *testing.T) {
	e := newEvalEnv(t)
	e.full("steps: []\nimpact: { level: high, labels: [], reason: r }\n")

	r := e.run(false)

	eq(t, "risk file", hardOf(t, r, "RISK_FILE"),
		HardCheck{"RISK_FILE", false, "thiếu risk.yaml; chạy aisdlc submit cases"})
	eq(t, "passed", r.Passed, false)
}

func TestEvalFullLegacyWithoutImpactNoRiskFileCheck(t *testing.T) {
	e := newEvalEnv(t)
	e.full("steps: []\n")

	r := e.run(false)

	if has(hardIDs(r), is("RISK_FILE")) {
		t.Errorf("hard %v", hardIDs(r))
	}
}

func TestEvalRiskCoverageNoCoverageFailsWithDistinctMessage(t *testing.T) {
	e := newEvalEnv(t)
	e.risk("high", req("coverage", "min", 90))

	r := e.run(false)

	eq(t, "check", hardOf(t, r, "RISK_COVERAGE"), HardCheck{"RISK_COVERAGE", false,
		"không có báo cáo coverage (chưa đo coverage); mức high yêu cầu coverage ≥ 90%"})
}

func TestEvalRiskCoverageNoNewLinesPasses(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.write("limit/limit.go", "package limit\n\ntype Limit struct{}\n")
	e.risk("high", req("coverage", "min", 90))

	c := hardOf(t, e.run(false), "RISK_COVERAGE")

	eq(t, "check", c, HardCheck{"RISK_COVERAGE", true, "không có dòng mới đo được; yêu cầu tối thiểu 90%"})
}

func TestEvalCoverageHardPassesAtOrAboveMin(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.risk("high", req("coverage", "min", 70))

	r := e.run(false)

	eq(t, "check", hardOf(t, r, "RISK_COVERAGE"),
		HardCheck{"RISK_COVERAGE", true, "75% (3/4 dòng mới/sửa); yêu cầu tối thiểu 70%"})
	eq(t, "passed", r.Passed, true)
}

func TestEvalRiskCoverageFileInPackageWithoutCoverageCountsAsUncovered(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage() // chỉ package limit được đo
	e.write("svc/other/other.go", "package other\n\ntype Other struct {\n\ta int\n}\n")
	e.risk("high", req("coverage", "min", 70))

	r := e.run(false)

	// limit.go 3/4 đo được + svc/other/other.go 5 dòng, package không được đo → 3/9
	eq(t, "check", hardOf(t, r, "RISK_COVERAGE"), HardCheck{"RISK_COVERAGE", false,
		"33% (3/9 dòng mới/sửa) < 70%; chưa đo: svc/other/other.go"})
	eq(t, "passed", r.Passed, false)
}

func TestEvalRiskCoverageOnlyFilesInUnmeasuredPackageFails(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.write("limit/limit.go", "package limit\n\ntype Limit struct{}\n")
	e.write("svc/other/other.go", "package other\n")
	e.risk("high", req("coverage", "min", 70))

	c := hardOf(t, e.run(false), "RISK_COVERAGE")

	eq(t, "passed", c.Passed, false)
	wantContains(t, "detail", c.Detail, "chưa đo: svc/other/other.go")
}

func TestEvalRiskCoverageUnmeasuredFilesNamedUpToFive(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		e.write("svc/"+n+"/x.go", "package "+n+"\n")
	}
	e.risk("high", req("coverage", "min", 70))

	c := hardOf(t, e.run(false), "RISK_COVERAGE")

	eq(t, "detail", c.Detail, "27% (3/11 dòng mới/sửa) < 70%; chưa đo: svc/a/x.go, svc/b/x.go, svc/c/x.go, "+
		"svc/d/x.go, svc/e/x.go (+2)")
}

func TestEvalRiskCoverageCodeLessFilesInMeasuredPackageIgnored(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage() // package limit có đo; file chỉ có kiểu/hằng không có block coverage
	e.write("limit/types.go", "package limit\n\ntype ID int\n\nconst Max = 10\n")
	e.risk("high", req("coverage", "min", 70))

	c := hardOf(t, e.run(false), "RISK_COVERAGE")

	eq(t, "check", c, HardCheck{"RISK_COVERAGE", true, "75% (3/4 dòng mới/sửa); yêu cầu tối thiểu 70%"})
}

func TestEvalSoftCoverageIgnoresUnmeasuredFiles(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.write("svc/other/other.go", "package other\n\ntype Other struct {\n\ta int\n}\n")

	r := e.run(false)

	eq(t, "coverage", mustSoft(t, r, "COVERAGE_CHANGED_LINES"),
		SoftCheck{"COVERAGE_CHANGED_LINES", SoftWarn, "75% (3/4 dòng mới/sửa)"})
}

func TestEvalCoverageSoftWithoutRisk(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()

	r := e.run(false)

	if has(hardIDs(r), is("RISK_COVERAGE")) {
		t.Errorf("hard %v", hardIDs(r))
	}
	eq(t, "level", mustSoft(t, r, "COVERAGE_CHANGED_LINES").Level, SoftWarn)
	if r.Risk != nil {
		t.Errorf("risk %+v", r.Risk)
	}
	wantNotContains(t, "report.json", e.read("aisdlc/features/PAY-123/eval/report.json"), `"risk"`)
}

func reqStates(k *RiskSection) []string {
	var out []string
	for _, s := range k.Requirements {
		out = append(out, s.ID+":"+string(s.Status))
	}
	return out
}

func TestEvalReportRiskSectionResolvesLaterRequirements(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.v.rows[1] = run.CaseResult{ID: "C2", Step: "S1", Outcome: run.CaseOutcomeFailed, Message: "x"}
	e.risk("high", req("coverage", "min", 90), req("sql-zero", "name", "Tổng bằng 0", "sql", "select 0"),
		req("concurrent", "threads", 4))

	r := e.run(false)

	if r.Risk == nil {
		t.Fatal("thiếu risk")
	}
	eq(t, "level", r.Risk.Level, "high")
	eq(t, "policy", r.Risk.PolicyVersion, 3)
	eq(t, "matched", r.Risk.Matched, []string{"r1"})
	eq(t, "reqs", reqStates(r.Risk), []string{"coverage:MISSING", "sql-zero:MISSING", "concurrent:MISSING"})
	eq(t, "coverage detail", r.Risk.Requirements[0].Detail, "75% (3/4 dòng mới/sửa) < 90%")
	wantContains(t, "sql-zero detail", r.Risk.Requirements[1].Detail, "có case không pass (1/2 case pass")
	back, err := ReadEvalReport(filepath.Join(e.paths.EvalDir(), "report.json"))
	e.must(err)
	eq(t, "risk back", back.Risk, r.Risk)
	wantContains(t, "md", e.read("aisdlc/features/PAY-123/eval/report.md"), "## Rủi ro")
}

func TestEvalReportRiskSectionOkWhenCasesPassAndCoverageMet(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Coverage = limitCoverage()
	e.risk("medium", req("coverage", "min", 70), req("sql-zero", "name", "Tổng bằng 0", "sql", "select 0"))

	r := e.run(false)

	eq(t, "reqs", reqStates(r.Risk), []string{"coverage:OK", "sql-zero:OK"})
}

func TestEvalAgentReviewSeriousCriterionWarns(t *testing.T) {
	e := newEvalEnv(t)
	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("NGHIEM_TRONG"), issue("medium", "security")))

	r := e.run(false)

	eq(t, "agent", mustSoft(t, r, "AGENT_REVIEW"),
		SoftCheck{"AGENT_REVIEW", SoftWarn, "4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 0 high, 1 medium"})
	eq(t, "passed", r.Passed, true)
}

func TestEvalAgentReviewAllGoodIsOk(t *testing.T) {
	e := newEvalEnv(t)
	criteria := strings.Replace(fiveCriteria("DAT"), criterion("error-handling", "DAT"), criterion("error-handling", "CAN_SUA"), 1)
	e.write("aisdlc/features/PAY-123/review-agent.json",
		reviewJSON(criteria, issue("medium", "error-handling")+","+issue("medium", "other")))

	r := e.run(false)

	eq(t, "agent", mustSoft(t, r, "AGENT_REVIEW"),
		SoftCheck{"AGENT_REVIEW", SoftOK, "4/5 ĐẠT, 1 CẦN_SỬA (error-handling); 0 high, 2 medium"})
}

func TestEvalRefreshAgentReviewRewritesOnlyThatRowWithoutHarness(t *testing.T) {
	e := newEvalEnv(t)
	before := e.run(false)
	eq(t, "before", mustSoft(t, before, "AGENT_REVIEW").Level, SoftNA)
	mdBefore := e.read("aisdlc/features/PAY-123/eval/report.md")
	e.write("aisdlc/features/PAY-123/review-agent.json", reviewJSON(fiveCriteria("NGHIEM_TRONG"), issue("high", "security")))

	e.must(RefreshAgentReview(e.paths))

	after, err := ReadEvalReport(filepath.Join(e.paths.EvalDir(), "report.json"))
	e.must(err)
	eq(t, "verify calls", e.v.calls, 1)
	eq(t, "agent", mustSoft(t, after, "AGENT_REVIEW"),
		SoftCheck{"AGENT_REVIEW", SoftWarn, "4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 1 high, 0 medium"})
	eq(t, "hard", after.Hard, before.Hard)
	eq(t, "soft ids", softIDs(after), softIDs(before))
	eq(t, "tree", after.EvaluatedTree, before.EvaluatedTree)
	eq(t, "at", after.At, before.At)
	eq(t, "passed", after.Passed, before.Passed)
	md := e.read("aisdlc/features/PAY-123/eval/report.md")
	wantContains(t, "md", md, "| AGENT_REVIEW | WARN | 4/5 ĐẠT, 1 NGHIÊM_TRỌNG (security); 1 high, 0 medium |")
	wantNotContains(t, "md", md, "chưa có review-agent.json")
	eq(t, "md lines", strings.Count(md, "\n"), strings.Count(mdBefore, "\n"))
}

func TestEvalConfigChangedInThisChangeFailsScope(t *testing.T) {
	e := newEvalEnv(t)
	// nới unmanaged để mọi file đều "không quản lý": eval không được tin cấu hình nó đang chấm
	e.config("unmanaged: [\"**\"]\n")
	e.write("scripts/hack.sh", "echo hi\n")

	s := hardOf(t, e.run(false), "SCOPE")

	eq(t, "passed", s.Passed, false)
	wantContains(t, "detail", s.Detail, "aisdlc/config.yaml bị sửa trong thay đổi này — sửa cấu hình phải ở MR riêng")
	eq(t, "ci", hardOf(t, e.run(true), "SCOPE").Passed, false)
}

func TestEvalSuiteFailureWhileEveryCasePassedFailsBuild(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.HarnessErr = "không dừng được container"

	r := e.run(false)

	eq(t, "cases", hardOf(t, r, "CASES_ALL_PASS").Passed, true)
	eq(t, "build", hardOf(t, r, "BUILD_AND_TESTS"),
		HardCheck{"BUILD_AND_TESTS", false, "harness lỗi: không dừng được container; xem .aisdlc/run/PAY-123/harness.log"})
}

func TestEvalExitZeroStillReadsUnitFailures(t *testing.T) {
	e := newEvalEnv(t)
	e.v.res.Unit = lang.UnitResult{Exit: 0, Failed: []string{"ex/limit.TestWorks"}}

	b := hardOf(t, e.run(false), "BUILD_AND_TESTS")

	eq(t, "passed", b.Passed, false)
	wantContains(t, "detail", b.Detail, "ex/limit.TestWorks")
}

func TestEvalUnapprovedCasesIsHardFailEvenInCi(t *testing.T) {
	e := newEvalEnv(t)
	e.must(os.Remove(e.paths.Gates()))

	ci := e.run(true)

	c := hardOf(t, ci, "CASES_LOCKED")
	eq(t, "check", c, HardCheck{"CASES_LOCKED", false, "bộ case chưa được duyệt"})
	if has(softIDs(ci), is("CASES_LOCKED")) {
		t.Errorf("soft %v", softIDs(ci))
	}
	eq(t, "ci passed", ci.Passed, false)
	eq(t, "local", hardOf(t, e.run(false), "CASES_LOCKED").Detail, "bộ case chưa được duyệt")
}

func TestEvalEvaluatedTreeIgnoresFeatureDirButTracksCode(t *testing.T) {
	e := newEvalEnv(t)
	r := e.run(false)
	// report.json vừa được ghi vào thư mục tính năng: không làm đổi tree
	eq(t, "tree", e.tree(), r.EvaluatedTree)
	e.write("limit/limit.go", "package limit\n\nvar changed int\n")
	if e.tree() == r.EvaluatedTree {
		t.Error("tree phải đổi khi code đổi")
	}
}

func TestEvalReportJSONFormat(t *testing.T) {
	e := newEvalEnv(t)
	e.run(false)
	var m map[string]any
	e.must(json.Unmarshal([]byte(e.read("aisdlc/features/PAY-123/eval/report.json")), &m))
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	for _, k := range []string{"feature", "at", "mode", "hard", "soft", "passed", "evaluatedTree"} {
		if _, ok := m[k]; !ok {
			t.Errorf("thiếu khóa %s trong %v", k, keys)
		}
	}
	if !reflect.DeepEqual(m["at"], "2026-11-02T03:00:00Z") {
		t.Errorf("at %v", m["at"])
	}
}

func TestEvalCheckCommandTimeoutKillsGroup(t *testing.T) {
	e := newEvalEnv(t)
	e.config("unmanaged: [\"*.md\"]\ntimeoutMinutes: 0.01\nchecks:\n  - { name: \"slow/x\", cmd: \"sleep 30 & sleep 30\" }\n")

	start := time.Now()
	c := hardOf(t, e.run(false), "CHECK:slow/x")

	eq(t, "check", c, HardCheck{"CHECK:slow/x", false, "quá thời gian, đã dừng; xem .aisdlc/run/PAY-123/check-slow_x.log"})
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("chờ %v: nhóm tiến trình không bị kill", d)
	}
}
