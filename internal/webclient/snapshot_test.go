package webclient

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

const snapCases = `cases:
  - id: C1
    step: S1
    kind: happy
    title: Chuyển tiền
    when: {method: POST, path: /transfers}
    then: {status: 200, body: {ok: true}}
`

var snapClock = time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)

type snapFix struct {
	t      *testing.T
	dir    string
	paths  model.FeaturePaths
	config model.RepoConfig
	git    *gitx.Repo
	gates  *gate.Service
	steps  *step.Service
	meta   model.FeatureMeta
	specs  []spec.StepSpec
}

func (f *snapFix) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, rel)
	mustNil(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
	mustNil(f.t, os.WriteFile(p, []byte(content), 0o644))
}

func bigText(n int) string {
	line := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde\n"
	return strings.Repeat(line, n/len(line)+1)[:n]
}

func now() time.Time { return snapClock }

func specList(n int) []spec.StepSpec {
	var out []spec.StepSpec
	titles := []string{"một", "hai"}
	for i := 1; i <= n; i++ {
		title := "b" + itoa(i)
		if n == 2 {
			title = titles[i-1]
		}
		out = append(out, spec.StepSpec{ID: "S" + itoa(i), Title: title, Scope: []string{"src/**"}, Budget: &spec.Budget{Files: 5, Lines: 10}})
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }

func newSnapFix(t *testing.T) *snapFix {
	t.Helper()
	hermeticGit(t)
	dir := t.TempDir()
	f := &snapFix{t: t, dir: dir}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "t@t")
	gitIn(t, dir, "config", "user.name", "t")
	f.paths = model.FeaturePaths{RepoRoot: dir, Key: "PAY-123"}
	cfg, err := model.LoadRepoConfig(dir)
	mustNil(t, err)
	f.config = cfg
	f.write("go.mod", "module x\n")
	f.write("src/old.txt", "o1\n")
	f.write("aisdlc/features/PAY-123/cases.yaml", snapCases)
	f.write("aisdlc/features/PAY-123/schema-expected.json", "{}\n")
	f.git = gitx.New(dir)
	f.gates = gate.NewService(f.paths, model.FlowLite, cfg, gate.NewLocalGateStore(f.paths), now)
	base, err := f.git.SnapshotTree(context.Background())
	mustNil(t, err)
	f.meta = model.FeatureMeta{Key: "PAY-123", Title: "Hạn mức", Flow: model.FlowLite, BaseTree: base, CreatedAt: snapClock, Owner: "an@corp.vn"}
	f.setSpecs(specList(2))
	_, err = f.gates.Submit(gate.CASES, nil)
	mustNil(t, err)
	_, err = f.gates.Approve(gate.CASES, "dev1", "")
	mustNil(t, err)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "init")
	return f
}

func (f *snapFix) setSpecs(s []spec.StepSpec) {
	f.specs = s
	f.steps = step.NewService(f.paths, f.git, s, f.config, now).WithGates(f.gates)
}

func runOK() run.CaseRunResult {
	return run.CaseRunResult{Cases: []run.CaseResult{{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}}}
}

func (f *snapFix) input(webMode bool) SnapshotInput {
	f.t.Helper()
	recs, err := f.steps.All()
	mustNil(f.t, err)
	cases, err := spec.ReadCases(f.paths.Cases())
	mustNil(f.t, err)
	return SnapshotInput{Paths: f.paths, Config: f.config, Meta: f.meta, Flow: model.FlowLite, Gates: f.gates,
		Store: gate.NewLocalGateStore(f.paths), Specs: f.specs, Records: recs, Cases: &cases, Git: f.git,
		Adapter: golang.Adapter{}, RepoID: "local/x", Branch: "main", ClientID: "cid", CapturedAt: snapClock,
		Sender: "An", WebMode: webMode}
}

func (f *snapFix) snapshot(webMode bool) *FeatureSnapshot {
	f.t.Helper()
	s, err := Build(f.input(webMode))
	mustNil(f.t, err)
	return s
}

func (f *snapFix) do(err error) {
	f.t.Helper()
	mustNil(f.t, err)
}

func (f *snapFix) start(id string) {
	_, err := f.steps.Start(id)
	f.do(err)
}

func (f *snapFix) finish(id string) {
	_, err := f.steps.Finish(id, runOK())
	f.do(err)
}

func (f *snapFix) accept(id string) {
	_, err := f.steps.Accept(id, "dev1")
	f.do(err)
}

func jsonOf(t *testing.T, s *FeatureSnapshot) string {
	t.Helper()
	b, err := s.JSON()
	mustNil(t, err)
	return string(b)
}

// gatesStepsCasesAndFeature
func TestSnapshotGatesStepsCasesAndFeature(t *testing.T) {
	f := newSnapFix(t)
	f.start("S1")
	f.write("src/a.txt", "a1\na2\n")
	s := f.snapshot(false)
	if s.SchemaVersion != 1 || s.Feature.Owner != "an@corp.vn" {
		t.Fatalf("%+v", s.Feature)
	}
	if len(s.Gates) != 2 || s.Gates[0].Gate != gate.CASES || s.Gates[1].Gate != gate.REVIEW {
		t.Fatalf("%+v", s.Gates)
	}
	h, _ := f.gates.CurrentHash(gate.CASES)
	if s.Gates[0].State != gate.Approved || h == "" || s.Gates[0].CurrentHash != h || s.Gates[0].Approval.Approver != "dev1" {
		t.Fatalf("%+v", s.Gates[0])
	}
	if s.Steps[0].Status != step.StepInProgress || !strings.Contains(s.Steps[0].Diff.Text, "+++ b/src/a.txt") ||
		!slices.Equal(s.Steps[0].CaseIDs, []string{"C1"}) {
		t.Fatalf("%+v", s.Steps[0])
	}
	if s.Steps[1].Status != step.StepNotStarted || s.Steps[1].Diff != nil {
		t.Fatalf("%+v", s.Steps[1])
	}
	c := s.Cases[0]
	if c.Request != "POST /transfers" || c.Expect != `200 {"ok":true}` || !strings.Contains(c.Raw, "C1") {
		t.Fatalf("%+v", c)
	}
	if s.Eval != nil || s.Review != nil || s.Truncated {
		t.Fatal(s.Eval, s.Review, s.Truncated)
	}
	j := jsonOf(t, s)
	for _, w := range []string{`"schemaVersion":1`, `"kind":"happy"`, `"capturedAt":"2026-11-03T03:00:00Z"`,
		`"repo":{"id":"local/x","branch":"main"}`, `"eval":null`, `"review":null`, `"truncated":false`,
		`"feature":{"key":"PAY-123","title":"Hạn mức","flow":"LITE","owner":"an@corp.vn","createdAt":"2026-11-03T03:00:00Z"}`} {
		if !strings.Contains(j, w) {
			t.Errorf("thiếu %s trong\n%s", w, j[:min(len(j), 600)])
		}
	}
}

// autoApprovalCarriesRule
func TestSnapshotAutoApprovalCarriesRule(t *testing.T) {
	f := newSnapFix(t)
	f.do(f.gates.Reopen(gate.CASES, "dev1"))
	_, err := f.gates.Submit(gate.CASES, nil)
	f.do(err)
	_, err = f.gates.AutoApprove(gate.CASES, "flow:low")
	f.do(err)
	s := f.snapshot(false)
	a := s.Gates[0].Approval
	if a.Approver != "auto" || a.Rule != "flow:low" {
		t.Fatalf("%+v", a)
	}
	if !strings.Contains(jsonOf(t, s), `"rule":"flow:low"`) {
		t.Fatal(jsonOf(t, s))
	}
	// feedback mở lại của cổng đi kèm
	if s.Gates[0].LastFeedback == nil || s.Gates[0].LastFeedback.Kind != "REOPENED" {
		t.Fatalf("%+v", s.Gates[0].LastFeedback)
	}
}

// stepDiffExcludesFeatureStateAndUnmanagedFiles
func TestSnapshotStepDiffExcludesFeatureStateAndUnmanagedFiles(t *testing.T) {
	f := newSnapFix(t)
	f.start("S1")
	f.do(step.NewScopeExtras(f.paths).Add("README.md", "dev1", "", snapClock))
	f.write("src/a.txt", "a1\n")
	f.write("README.md", "ghi chú\n")
	f.write("docs/n.txt", "n\n")
	f.finish("S1")
	diff := f.snapshot(false).Steps[0].Diff.Text
	if !strings.Contains(diff, "+++ b/src/a.txt") {
		t.Fatal(diff)
	}
	for _, no := range []string{"steps.json", "scope-extra.json", "README.md", "docs/n.txt"} {
		if strings.Contains(diff, no) {
			t.Fatalf("có %s:\n%s", no, diff)
		}
	}
	recs, _ := f.steps.All()
	if recs[0].Check.Files != 1 {
		t.Fatal(recs[0].Check.Files)
	}
}

// stepDiffEmptyWhenOnlyStateFilesChanged
func TestSnapshotStepDiffEmptyWhenOnlyStateFilesChanged(t *testing.T) {
	f := newSnapFix(t)
	f.start("S1")
	f.finish("S1")
	if d := f.snapshot(false).Steps[0].Diff; d == nil || d.Text != "" {
		t.Fatalf("%+v", d)
	}
}

// openQuestionsFromBrd
func TestSnapshotOpenQuestionsFromBrd(t *testing.T) {
	f := newSnapFix(t)
	f.write("aisdlc/features/PAY-123/brd.md", "## Câu hỏi mở\n- [ ] Hạn mức theo ngày hay tháng?\n- [x] Xong\n")
	s := f.snapshot(false)
	if len(s.OpenQuestions) != 1 || s.OpenQuestions[0].Gate != gate.BRD || s.OpenQuestions[0].Text != "Hạn mức theo ngày hay tháng?" {
		t.Fatalf("%+v", s.OpenQuestions)
	}
}

// bigArtifactIsTruncatedByUtf8Bytes
func TestSnapshotBigArtifactIsTruncatedByUtf8Bytes(t *testing.T) {
	f := newSnapFix(t)
	f.write("aisdlc/features/PAY-123/brd.md", bigText(300*1024))
	f.write("aisdlc/features/PAY-123/design.md", strings.Repeat("ế", 100*1024)) // 3 byte mỗi ký tự
	s := f.snapshot(false)
	a := s.Artifacts.Get("brd.md")
	if len(a.Content) > 256*1024 || !a.Truncated {
		t.Fatal(len(a.Content), a.Truncated)
	}
	d := s.Artifacts.Get("design.md")
	if !d.Truncated || len(d.Content) > 256*1024 || !regexp.MustCompile(`^(ế)+$`).MatchString(d.Content) {
		t.Fatal(len(d.Content), d.Truncated)
	}
	if !s.Artifacts.Has("cases.yaml") || s.Artifacts.Has("design.yaml") || !s.Truncated {
		t.Fatal(s.Artifacts.Keys(), s.Truncated)
	}
	// thứ tự như LinkedHashMap của Java: brd.md, design.md, ..., cases.yaml
	if k := s.Artifacts.Keys(); !slices.Equal(k, []string{"brd.md", "design.md", "cases.yaml"}) {
		t.Fatal(k)
	}
}

// hugeDiffsAreCappedAndSnapshotFitsLimit
func TestSnapshotHugeDiffsAreCappedAndSnapshotFitsLimit(t *testing.T) {
	f := newSnapFix(t)
	f.start("S1")
	f.write("src/big1.txt", bigText(5*1024*1024))
	f.finish("S1")
	f.accept("S1")
	f.start("S2")
	f.write("src/big2.txt", bigText(5*1024*1024))
	f.finish("S2")
	s := f.snapshot(false)
	if n := len(jsonOf(t, s)); n > MaxSnapshotBytes {
		t.Fatal(n)
	}
	if !s.Steps[0].Diff.Truncated || !strings.Contains(s.Steps[0].Diff.Text, "+++ b/src/big1.txt") ||
		!strings.Contains(s.Steps[1].Diff.Text, "+++ b/src/big2.txt") || !s.Truncated {
		t.Fatal(s.Steps[0].Diff.Truncated, s.Truncated)
	}
}

// dropsOldestDiffsWhenOverTotalLimit
func TestSnapshotDropsOldestDiffsWhenOverTotalLimit(t *testing.T) {
	f := newSnapFix(t)
	f.setSpecs(specList(20))
	for i := 1; i <= 20; i++ {
		id := "S" + itoa(i)
		f.start(id)
		f.write("src/f"+itoa(i)+".txt", bigText(600*1024))
		f.finish(id)
		f.accept(id)
	}
	s := f.snapshot(false)
	if n := len(jsonOf(t, s)); n > MaxSnapshotBytes {
		t.Fatal(n)
	}
	if s.Steps[0].Diff.Text != "" || !s.Steps[0].Diff.Truncated || !strings.Contains(s.Steps[19].Diff.Text, "+++ b/src/f20.txt") || !s.Truncated {
		t.Fatal(len(s.Steps[0].Diff.Text), s.Steps[0].Diff.Truncated, s.Truncated)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	mustNil(t, err)
}

func cardKeys(s *FeatureSnapshot) []string {
	if s.ReviewCards == nil {
		return nil
	}
	return s.ReviewCards.Keys()
}

// includesReviewCards
func TestSnapshotIncludesReviewCards(t *testing.T) {
	f := newSnapFix(t)
	// luồng nhẹ: thẻ CASES và REVIEW, không có DESIGN
	if k := cardKeys(f.snapshot(false)); !slices.Equal(k, []string{"CASES", "REVIEW"}) {
		t.Fatal(k)
	}
	// luồng đầy đủ với fixture PAY-1 (design chồng scope với code cũ)
	copyTree(t, filepath.Join("..", "review", "cardbuild", "testdata", "pay1"), f.paths.Dir())
	b, err := os.ReadFile(filepath.Join("..", "review", "cardbuild", "testdata", "00001_init.sql"))
	mustNil(t, err)
	f.write("db/migrations/00001_init.sql", string(b))
	gitIn(t, f.dir, "add", "-A")
	gitIn(t, f.dir, "commit", "-qm", "pay1")
	fullGates := gate.NewService(f.paths, model.FlowFull, f.config, gate.NewLocalGateStore(f.paths), now)
	fullMeta := f.meta
	fullMeta.Flow = model.FlowFull
	full := func() *FeatureSnapshot {
		in := SnapshotInput{Paths: f.paths, Config: f.config, Meta: fullMeta, Flow: model.FlowFull, Gates: fullGates,
			Store: gate.NewLocalGateStore(f.paths), Git: gitx.New(f.dir), Adapter: golang.Adapter{}, RepoID: "local/x",
			Branch: "main", ClientID: "cid", CapturedAt: snapClock, Sender: "An"}
		s, err := Build(in)
		mustNil(t, err)
		return s
	}
	// ngay sau init: chỉ cổng đầu tiên đã tới lượt nên chỉ có thẻ BRD
	if k := cardKeys(full()); !slices.Equal(k, []string{"BRD"}) {
		t.Fatal(k)
	}
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN} {
		_, err := fullGates.Submit(g, nil)
		mustNil(t, err)
		_, err = fullGates.Approve(g, "dev1", "")
		mustNil(t, err)
	}
	snap := full()
	if snap.SchemaVersion != 1 {
		t.Fatal(snap.SchemaVersion)
	}
	// REVIEW còn khóa vì CASES chưa duyệt
	if k := cardKeys(snap); !slices.Equal(k, []string{"BRD", "DESIGN", "CASES"}) {
		t.Fatal(k)
	}
	found := false
	for _, fl := range snap.ReviewCards.Get("DESIGN").Flags {
		found = found || fl.Code == "SCOPE_OVERLAP"
	}
	if !found {
		t.Fatalf("%+v", snap.ReviewCards.Get("DESIGN").Flags)
	}
	j := jsonOf(t, snap)
	if !strings.Contains(j, `"reviewCards":{`) || !strings.Contains(j, "SCOPE_OVERLAP") {
		t.Fatal(j[:min(len(j), 400)])
	}
}

// treeOnlyWhenReviewOpen
func TestSnapshotTreeOnlyWhenReviewOpen(t *testing.T) {
	f := newSnapFix(t)
	// luồng nhẹ, CASES đã duyệt → REVIEW mở → có tree (cây code trừ thư mục tính năng), chỉ ở chế độ web
	f.write("src/new.txt", "n\n")
	open := f.snapshot(true)
	ctx := context.Background()
	want, err := f.git.SnapshotTreeExcluding(ctx, "aisdlc/features/PAY-123")
	mustNil(t, err)
	all, err := f.git.SnapshotTree(ctx)
	mustNil(t, err)
	if open.Tree == "" || open.Tree != want || open.Tree == all {
		t.Fatal(open.Tree, want, all)
	}
	// luồng đầy đủ, BRD chưa duyệt → REVIEW khóa → không có tree trong JSON
	fullGates := gate.NewService(f.paths, model.FlowFull, f.config, gate.NewLocalGateStore(f.paths), now)
	if st, _ := fullGates.State(gate.REVIEW); st != gate.LockedByPrevious {
		t.Fatal(st)
	}
	in := f.input(true)
	in.Flow, in.Gates, in.Specs, in.Records, in.Cases = model.FlowFull, fullGates, nil, nil, nil
	locked, err := Build(in)
	mustNil(t, err)
	if locked.Tree != "" || strings.Contains(jsonOf(t, locked), `"tree"`) {
		t.Fatal(locked.Tree)
	}
}

// noTreeInLocalModeEvenWhenReviewOpen
func TestSnapshotNoTreeInLocalModeEvenWhenReviewOpen(t *testing.T) {
	f := newSnapFix(t)
	f.write("src/new.txt", "n\n")
	local := f.snapshot(false)
	if local.Tree != "" || strings.Contains(jsonOf(t, local), `"tree"`) {
		t.Fatal(local.Tree)
	}
}

// oversizedReviewCardsAreDroppedLast
func TestSnapshotOversizedReviewCardsAreDroppedLast(t *testing.T) {
	f := newSnapFix(t)
	base := f.snapshot(false)
	huge := review.ReviewCard{Gate: "CASES", Flags: []review.ReviewFlag{},
		Assumptions: []review.Assumption{{Text: bigText(MaxSnapshotBytes + 10)}}}
	big := *base
	big.ReviewCards = newOrdered[review.ReviewCard]()
	big.ReviewCards.Put("CASES", huge)
	big.Truncated = false
	fit, err := fitToLimit(&big)
	mustNil(t, err)
	if fit.ReviewCards != nil || !fit.Truncated {
		t.Fatal(fit.ReviewCards, fit.Truncated)
	}
	j := jsonOf(t, fit)
	if len(j) > MaxSnapshotBytes || strings.Contains(j, "reviewCards") {
		t.Fatal(len(j))
	}
}

func TestTruncateUTF8(t *testing.T) {
	if got := truncateUTF8("aế", 3); got != "a" {
		t.Fatalf("%q", got)
	}
	if got := truncateUTF8("aế", 4); got != "aế" {
		t.Fatalf("%q", got)
	}
	if got := truncateUTF8("a😀b", 4); got != "a" {
		t.Fatalf("%q", got)
	}
}

func TestSnapshotJSONDoesNotEscapeHTML(t *testing.T) {
	f := newSnapFix(t)
	f.write("aisdlc/features/PAY-123/brd.md", "a <b> & c\n")
	j := jsonOf(t, f.snapshot(false))
	if !strings.Contains(j, `a <b> & c`) {
		t.Fatal("escape HTML khác Jackson")
	}
	if bytes.Contains([]byte(j), []byte("\n")) {
		t.Fatal("JSON có xuống dòng thừa")
	}
}
