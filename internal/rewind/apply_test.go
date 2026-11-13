package rewind

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

var bg = context.Background()

// fixture là repo git tạm có tính năng ROOM-1 luồng đầy đủ, S1–S3 ACCEPTED, cổng REVIEW đã nộp.
type fixture struct {
	t     *testing.T
	dir   string
	paths model.FeaturePaths
	cfg   model.RepoConfig
	git   *gitx.Repo
	specs []spec.StepSpec
	store *gate.LocalGateStore
	now   time.Time
}

type alwaysReady struct{}

func (alwaysReady) CodeReady() (bool, error) { return true, nil }

func (f *fixture) sh(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func (f *fixture) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.dir, filepath.FromSlash(rel)))
	return err == nil
}

func (f *fixture) steps() *step.Service {
	return step.NewService(f.paths, f.git, f.specs, f.cfg, func() time.Time { return f.now }).WithGates(alwaysReady{})
}

func (f *fixture) runStep(id string, edit func()) {
	f.t.Helper()
	s := f.steps()
	if _, err := s.Start(id); err != nil {
		f.t.Fatal(err)
	}
	edit()
	if _, err := s.Finish(id, run.CaseRunResult{}); err != nil {
		f.t.Fatal(err)
	}
	if _, err := s.Accept(id, "dev1"); err != nil {
		f.t.Fatal(err)
	}
}

const feat = "aisdlc/features/ROOM-1/"

func newFixture(t *testing.T) *fixture { t.Helper(); return newFixtureWith(t, nil) }

// newFixtureWith như newFixture; beforeS2 chạy ngay trước khi bắt đầu S2 (sau S1).
func newFixtureWith(t *testing.T, beforeS2 func(*fixture)) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	f := &fixture{t: t, dir: dir, paths: model.FeaturePaths{RepoRoot: dir, Key: "ROOM-1"}, git: gitx.New(dir),
		now: time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)}
	f.sh("init", "-q")
	f.sh("config", "user.email", "t@t")
	f.sh("config", "user.name", "t")
	cfg, err := model.LoadRepoConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = cfg
	f.store = gate.NewLocalGateStore(f.paths)
	for _, id := range []string{"S1", "S2", "S3"} {
		f.specs = append(f.specs, spec.StepSpec{ID: id, Title: id, Scope: []string{"src/**"},
			Budget: &spec.Budget{Files: 10, Lines: 100}})
	}
	f.write("src/a.go", "a0\n")
	f.write("x.go", "x0\n")
	f.write("README.md", "r0\n")
	f.write(feat+"feature.yaml", "key: ROOM-1\nflow: FULL\n")
	f.write(feat+"feedback.md", "# feedback\n")
	f.write(feat+"brd.md", "brd\n")
	f.write(feat+"design.md", "design\n")
	f.write(feat+"design.yaml", "steps: []\n")
	f.write(feat+"db/V1.sql", "create table t();\n")
	f.write(feat+"schema-expected.json", "{}\n")
	f.write(feat+"cases.yaml", "cases: []\n")
	f.write(feat+"risk.yaml", "level: low\n")
	f.write(feat+"risk-agent.json", "{}\n")
	f.write(feat+"gates.json", gatesJSON(gate.BRD, gate.DESIGN, gate.CASES))
	f.write("aisdlc/features/ROOM-2/steps.json", "[]\n")
	f.sh("add", "-A")
	f.sh("commit", "-qm", "init")

	f.runStep("S1", func() { f.write("src/s1.go", "s1\n") })
	if beforeS2 != nil {
		beforeS2(f)
	}
	f.runStep("S2", func() {
		f.write("src/a.go", "a2\n")
		f.write("src/b.go", "b2\n")
	})
	if err := step.NewScopeExtras(f.paths).Add("lib/extra.go", "dev1", "S2", f.now); err != nil {
		t.Fatal(err)
	}
	f.runStep("S3", func() { f.write("src/s3.go", "s3\n") })
	f.write(feat+"eval/r.json", "{}\n")
	f.write(feat+"review-agent.json", "{}\n")
	f.write(feat+"gates.json", gatesJSON(gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW))
	return f
}

// gatesJSON dựng gates.json với các cổng đã nộp và duyệt.
func gatesJSON(gs ...gate.Gate) string {
	subs, apps := map[string]any{}, map[string]any{}
	for _, g := range gs {
		subs[string(g)] = map[string]any{"hash": "h-" + string(g), "findings": []any{}}
		apps[string(g)] = map[string]any{"hash": "h-" + string(g), "approver": "dev1"}
	}
	b, _ := json.Marshal(map[string]any{"submissions": subs, "approvals": apps})
	return string(b)
}

func (f *fixture) reached(g gate.Gate) bool {
	_, ok, err := gate.NewLocalGateStore(f.paths).Submission(g)
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

func (f *fixture) records() []step.StepRecord {
	f.t.Helper()
	recs, err := f.steps().All()
	if err != nil {
		f.t.Fatal(err)
	}
	return recs
}

func (f *fixture) plan(cp string) Plan {
	f.t.Helper()
	in := Input{Key: f.paths.Key, Flow: model.FlowFull, Paths: f.paths, Steps: f.specs, Records: f.records(),
		Reached: f.reached, RiskFromDesign: true}
	c, err := Parse(in, cp)
	if err != nil {
		f.t.Fatal(err)
	}
	p, err := Build(in, c)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) deps() Deps {
	store := gate.NewLocalGateStore(f.paths)
	return Deps{Git: f.git, Paths: f.paths, Cfg: f.cfg, Steps: f.steps(), Extras: step.NewScopeExtras(f.paths),
		ClearGate: store.Clear,
		AppendFeedback: func(text string) error {
			fh, err := os.OpenFile(f.paths.Feedback(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			defer fh.Close()
			_, err = fh.WriteString(text)
			return err
		},
		Now: func() time.Time { return f.now }, By: "dev1"}
}

// rewind chạy đủ ResolveCode → Backup → Apply như lệnh CLI; trả plan và ref dự phòng.
func (f *fixture) rewind(cp string) (Plan, string) {
	f.t.Helper()
	p := f.plan(cp)
	d := f.deps()
	if err := ResolveCode(bg, d, &p); err != nil {
		f.t.Fatal(err)
	}
	ref, err := Backup(bg, d)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := Apply(bg, d, p, ref); err != nil {
		f.t.Fatal(err)
	}
	return p, ref
}

func (f *fixture) record(id string) step.StepRecord {
	for _, r := range f.records() {
		if r.ID == id {
			return r
		}
	}
	f.t.Fatalf("không có bản ghi %s", id)
	return step.StepRecord{}
}

// codeDiff là các file code (ngoài aisdlc/features/) khác giữa tree và working tree hiện tại.
func (f *fixture) codeDiff(tree string) []string {
	f.t.Helper()
	now, err := f.git.SnapshotTree(bg)
	if err != nil {
		f.t.Fatal(err)
	}
	cs, err := f.git.Diff(bg, tree, now)
	if err != nil {
		f.t.Fatal(err)
	}
	out := []string{}
	for _, c := range cs {
		if !strings.HasPrefix(c.Path, "aisdlc/features/") {
			out = append(out, c.Path)
		}
	}
	return out
}

func (f *fixture) rewindLines() []string {
	var out []string
	for _, l := range strings.Split(f.read(feat+"feedback.md"), "\n") {
		if strings.HasPrefix(l, "## REWIND") {
			out = append(out, l)
		}
	}
	return out
}

func TestApply_RewindToS2(t *testing.T) {
	f := newFixture(t)
	s2 := f.record("S2")
	f.write("src/s1.go", "s1-sửa-tay\n")
	_, ref := f.rewind("S2")

	if d := f.codeDiff(s2.StartTree); len(d) != 0 {
		t.Fatalf("code chưa về đầu S2: %v", d)
	}
	if f.exists("src/b.go") || f.exists("src/s3.go") || f.read("src/a.go") != "a0\n" || f.read("src/s1.go") != "s1\n" {
		t.Fatal("file sau mốc chưa được khôi phục")
	}
	recs := f.records()
	if len(recs) != 1 || recs[0].ID != "S1" {
		t.Fatalf("steps.json = %+v", recs)
	}
	if f.reached(gate.REVIEW) || !f.reached(gate.CASES) {
		t.Fatal("chỉ REVIEW được xoá")
	}
	if f.exists(feat+"eval") || f.exists(feat+"review-agent.json") || !f.exists(feat+"cases.yaml") {
		t.Fatal("artifact sai")
	}
	extras, err := step.NewScopeExtras(f.paths).Paths()
	if err != nil || len(extras) != 0 {
		t.Fatalf("scope-extra của S2 phải bị bỏ: %v %v", extras, err)
	}
	lines := f.rewindLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "S2") || !strings.Contains(lines[0], "dev1") ||
		!strings.Contains(lines[0], ref) {
		t.Fatalf("feedback: %v", lines)
	}
	if !strings.HasPrefix(ref, "refs/aisdlc/backup/ROOM-1/20261113-") {
		t.Fatalf("ref = %s", ref)
	}
}

func TestApply_RewindToBRD(t *testing.T) {
	f := newFixture(t)
	s1 := f.record("S1")
	f.rewind("brd")

	if d := f.codeDiff(s1.StartTree); len(d) != 0 {
		t.Fatalf("code chưa về đầu S1: %v", d)
	}
	ents, err := os.ReadDir(f.paths.Dir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if !slices.Equal(names, []string{"feature.yaml", "feedback.md", "gates.json", "steps.json"}) {
		t.Fatalf("thư mục tính năng còn: %v", names)
	}
	// gates.json và steps.json còn file nhưng trạng thái rỗng (như tính năng mới tạo).
	for _, g := range gate.Sequence(model.FlowFull) {
		if f.reached(g) {
			t.Fatalf("cổng %s chưa bị xoá", g)
		}
	}
	if recs := f.records(); len(recs) != 0 {
		t.Fatalf("steps.json = %+v", recs)
	}
}

func TestApply_OtherFeatureUntouched(t *testing.T) {
	f := newFixture(t)
	f.write("aisdlc/features/ROOM-2/steps.json", `[{"id":"S1"}]`)
	f.write("aisdlc/features/ROOM-2/brd.md", "room2\n")
	f.rewind("S1")
	if f.read("aisdlc/features/ROOM-2/steps.json") != `[{"id":"S1"}]` || f.read("aisdlc/features/ROOM-2/brd.md") != "room2\n" {
		t.Fatal("tính năng khác bị đụng")
	}
}

func TestApply_DirtyTreeBackedUp(t *testing.T) {
	f := newFixture(t)
	f.write("x.go", "x-dirty\n")
	_, ref := f.rewind("S2")
	if f.read("x.go") != "x0\n" {
		t.Fatalf("x.go = %q", f.read("x.go"))
	}
	if got := f.sh("show", ref+":x.go"); got != "x-dirty\n" {
		t.Fatalf("backup x.go = %q", got)
	}
	if got := f.sh("show", ref+":"+feat+"steps.json"); !strings.Contains(got, `"S3"`) {
		t.Fatal("backup phải gồm thư mục tính năng")
	}
	f.sh("gc", "-q", "--prune=now")
	if got := f.sh("show", ref+":x.go"); got != "x-dirty\n" {
		t.Fatal("backup mất sau gc")
	}
}

func TestBackup_UniqueRef(t *testing.T) {
	f := newFixture(t)
	d := f.deps()
	r1, err := Backup(bg, d)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Backup(bg, d)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != "refs/aisdlc/backup/ROOM-1/"+f.now.Format("20060102-1504") {
		t.Fatal(r1)
	}
	if r2 != r1+"-2" {
		t.Fatalf("r1=%s r2=%s", r1, r2)
	}
}

func TestApply_Idempotent(t *testing.T) {
	f := newFixture(t)
	p, ref := f.rewind("S2")
	snap1, _ := f.git.SnapshotTree(bg)
	fb1 := f.read(feat + "feedback.md")
	f.now = f.now.Add(time.Minute) // chạy lại lúc khác vẫn không ghi dòng thứ hai
	d := f.deps()
	if err := Apply(bg, d, p, ref); err != nil {
		t.Fatal(err)
	}
	snap2, _ := f.git.SnapshotTree(bg)
	if snap1 != snap2 || f.read(feat+"feedback.md") != fb1 {
		t.Fatal("Apply lần hai làm đổi kết quả")
	}
	if n := len(f.rewindLines()); n != 1 {
		t.Fatalf("có %d dòng REWIND", n)
	}
}

func changePaths(cs []gitx.FileChange) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Path)
	}
	sort.Strings(out)
	return out
}

func TestResolveCode_ForeignSplit(t *testing.T) {
	f := newFixture(t)
	f.write("src/manual.go", "tay\n")
	f.write("README.md", "unmanaged\n")
	f.write(".aisdlc/cache.json", "{}\n")
	f.write("aisdlc/features/ROOM-2/brd.md", "khác\n")
	p := f.plan("S2")
	if err := ResolveCode(bg, f.deps(), &p); err != nil {
		t.Fatal(err)
	}
	if got := changePaths(p.OwnChanges); !slices.Equal(got, []string{"src/a.go", "src/b.go", "src/s3.go"}) {
		t.Fatalf("own = %v", got)
	}
	if got := changePaths(p.ForeignChanges); !slices.Equal(got, []string{"src/manual.go"}) {
		t.Fatalf("foreign = %v", got)
	}
}

func TestResolveCode_InProgressIsOwn(t *testing.T) {
	f := newFixture(t)
	f.write(feat+"gates.json", gatesJSON(gate.BRD, gate.DESIGN, gate.CASES))
	// làm lại S3 dở: tạo bước S4 đang IN_PROGRESS
	f.specs = append(f.specs, spec.StepSpec{ID: "S4", Title: "S4", Scope: []string{"src/**"}})
	if _, err := f.steps().Start("S4"); err != nil {
		t.Fatal(err)
	}
	f.write("src/s4.go", "s4\n")
	p := f.plan("S3")
	if err := ResolveCode(bg, f.deps(), &p); err != nil {
		t.Fatal(err)
	}
	if got := changePaths(p.OwnChanges); !slices.Equal(got, []string{"src/s3.go", "src/s4.go"}) {
		t.Fatalf("own = %v", got)
	}
	if len(p.ForeignChanges) != 0 {
		t.Fatalf("foreign = %v", changePaths(p.ForeignChanges))
	}
}

func TestPlan_MissingTree(t *testing.T) {
	f := newFixture(t)
	p := f.plan("S2")
	f.sh("update-ref", "-d", step.CheckpointRef(f.paths.Key, "S2"))
	f.sh("reflog", "expire", "--expire=now", "--all")
	f.sh("gc", "-q", "--prune=now")
	if f.git.ObjectExists(bg, p.CodeTree) {
		t.Skip("tree đầu S2 vẫn còn sau gc (dùng chung object) — không dựng được tình huống")
	}
	before, _ := f.git.SnapshotTree(bg)
	err := ResolveCode(bg, f.deps(), &p)
	if err == nil || !strings.Contains(err.Error(), "S2") || !strings.Contains(err.Error(), "mốc") {
		t.Fatalf("err = %v", err)
	}
	after, _ := f.git.SnapshotTree(bg)
	if before != after {
		t.Fatal("ResolveCode lỗi mà vẫn đổi file")
	}
	if !f.exists("src/s3.go") || !f.exists(feat+"eval/r.json") {
		t.Fatal("file bị đổi")
	}
}

func TestResolveCode_FallsBackToCheckpointRef(t *testing.T) {
	f := newFixture(t)
	p := f.plan("S2")
	want := p.CodeTree
	p.CodeTree = strings.Repeat("1", 40) // StartTree hỏng/mất nhưng ref checkpoint còn
	if err := ResolveCode(bg, f.deps(), &p); err != nil {
		t.Fatal(err)
	}
	if p.CodeTree != want {
		t.Fatalf("CodeTree = %s, muốn %s", p.CodeTree, want)
	}
}

func TestApply_RefusesPathOutsideFeature(t *testing.T) {
	f := newFixture(t)
	p := f.plan("review")
	p.DeletePaths = append(p.DeletePaths, filepath.Join(f.dir, "aisdlc/features/ROOM-2"))
	if err := Apply(bg, f.deps(), p, "refs/aisdlc/backup/ROOM-1/x"); err == nil {
		t.Fatal("phải từ chối xoá ngoài thư mục tính năng")
	}
	if !f.exists("aisdlc/features/ROOM-2/steps.json") || !f.exists(feat+"eval/r.json") {
		t.Fatal("không được xoá gì khi kế hoạch có path lạ")
	}
}

func TestApply_ResumeAfterCrash(t *testing.T) {
	f := newFixture(t)
	s2 := f.record("S2")
	p := f.plan("S2")
	d := f.deps()
	if err := ResolveCode(bg, d, &p); err != nil {
		t.Fatal(err)
	}
	ref, err := Backup(bg, d)
	if err != nil {
		t.Fatal(err)
	}
	// sự cố sau Truncate: ghi feedback lỗi
	broken := d
	broken.AppendFeedback = func(string) error { return os.ErrPermission }
	if err := Apply(bg, broken, p, ref); err == nil {
		t.Fatal("muốn lỗi")
	}
	if recs := f.records(); len(recs) != 1 {
		t.Fatalf("Truncate phải đã chạy: %+v", recs)
	}
	if !f.exists(feat + "rewind-pending.json") {
		t.Fatal("thiếu dấu đang dở")
	}

	// chạy lại qua đường lập kế hoạch thật: mốc S2 vẫn hợp lệ dù bản ghi đã mất
	in := Input{Key: f.paths.Key, Flow: model.FlowFull, Paths: f.paths, Steps: f.specs, Records: f.records(),
		Reached: f.reached, RiskFromDesign: true}
	cp, err := Parse(in, "s2")
	if err != nil {
		t.Fatal(err)
	}
	pr, ok, err := Pending(f.paths)
	if err != nil || !ok || pr.Plan.Target.ID != cp.ID || pr.BackupRef != ref || pr.By != "dev1" {
		t.Fatalf("pending = %+v %v %v", pr, ok, err)
	}
	if pr.Plan.CodeTree != s2.StartTree || len(pr.Plan.OwnChanges) == 0 {
		t.Fatalf("kế hoạch lưu thiếu: %+v", pr.Plan)
	}
	if err := Apply(bg, f.deps(), pr.Plan, pr.BackupRef); err != nil {
		t.Fatal(err)
	}
	if f.exists(feat + "rewind-pending.json") {
		t.Fatal("dấu đang dở phải bị xoá khi xong")
	}
	if l := f.rewindLines(); len(l) != 1 || !strings.Contains(l[0], ref) {
		t.Fatalf("feedback: %v", l)
	}
	if d := f.codeDiff(s2.StartTree); len(d) != 0 {
		t.Fatalf("code: %v", d)
	}
	if _, ok, _ := Pending(f.paths); ok {
		t.Fatal("còn pending")
	}
	if _, err := Parse(in, "S3"); err == nil {
		// in.Records cũ không có S3 → S3 không hợp lệ; chỉ mốc đang dở được thêm
		t.Fatal("S3 không được hợp lệ")
	}
}

func TestApply_ResumeGateCheckpoint(t *testing.T) {
	f := newFixture(t)
	p := f.plan("design")
	d := f.deps()
	if err := ResolveCode(bg, d, &p); err != nil {
		t.Fatal(err)
	}
	ref, _ := Backup(bg, d)
	broken := d
	broken.AppendFeedback = func(string) error { return os.ErrPermission }
	if err := Apply(bg, broken, p, ref); err == nil {
		t.Fatal("muốn lỗi")
	}
	in := Input{Key: f.paths.Key, Flow: model.FlowFull, Paths: f.paths, Steps: f.specs, Records: f.records(),
		Reached: f.reached, RiskFromDesign: true}
	if _, err := Parse(in, "design"); err != nil {
		t.Fatalf("mốc design đang dở phải hợp lệ: %v", err)
	}
	if _, err := Parse(in, "cases"); err == nil {
		t.Fatal("cases đã bị xoá, không hợp lệ")
	}
}

// TestApply_ResumeGateCheckpointWithoutSpecs: chạy lại lần quay về mốc cổng sau khi design.yaml đã bị xoá
// (spec dựng lại từ đĩa rỗng) vẫn hoàn tất và bỏ mọi bản ghi bước.
func TestApply_ResumeGateCheckpointWithoutSpecs(t *testing.T) {
	for _, crash := range []string{"clearGate", "feedback"} {
		t.Run(crash, func(t *testing.T) {
			f := newFixture(t)
			p := f.plan("design")
			d := f.deps()
			if err := ResolveCode(bg, d, &p); err != nil {
				t.Fatal(err)
			}
			ref, _ := Backup(bg, d)
			broken := d
			if crash == "clearGate" {
				broken.ClearGate = func(gate.Gate) error { return os.ErrPermission }
			} else {
				broken.AppendFeedback = func(string) error { return os.ErrPermission }
			}
			if err := Apply(bg, broken, p, ref); err == nil {
				t.Fatal("muốn lỗi")
			}
			if f.exists(feat + "design.yaml") {
				t.Fatal("design.yaml phải đã bị xoá")
			}
			pr, ok, err := Pending(f.paths)
			if err != nil || !ok {
				t.Fatalf("pending: %v %v", ok, err)
			}
			f.specs = nil // như CLI: spec đọc lại từ design.yaml đã mất
			if err := Apply(bg, f.deps(), pr.Plan, pr.BackupRef); err != nil {
				t.Fatalf("chạy lại phải xong: %v", err)
			}
			if recs := f.records(); len(recs) != 0 {
				t.Fatalf("còn bản ghi: %+v", recs)
			}
			if f.exists(feat + "rewind-pending.json") {
				t.Fatal("dấu đang dở phải bị xoá")
			}
		})
	}
}

// TestApply_StepCheckpointDropsStaleRecords: bản ghi của bước không còn trong thiết kế cũng bị bỏ khi quay về Sk.
func TestApply_StepCheckpointDropsStaleRecords(t *testing.T) {
	f := newFixture(t)
	recs := f.records()
	recs = append(recs, step.StepRecord{ID: "S9", Status: step.StepAccepted})
	b, _ := json.MarshalIndent(recs, "", "  ")
	f.write(feat+"steps.json", string(b))
	f.rewind("S2")
	got := []string{}
	for _, r := range f.records() {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"S1"}) {
		t.Fatalf("bản ghi còn lại = %v, muốn [S1]", got)
	}
}

func TestApply_DeleteRefusesPendingMarker(t *testing.T) {
	f := newFixture(t)
	p := f.plan("review")
	p.DeletePaths = append(p.DeletePaths, PendingFile(f.paths))
	if err := Apply(bg, f.deps(), p, "refs/aisdlc/backup/ROOM-1/x"); err == nil {
		t.Fatal("không được xoá dấu đang dở qua DeletePaths")
	}
}

func TestResolveCode_IgnoredFileNotOverwritten(t *testing.T) {
	// .env có trong snapshot đầu S2 (chưa bị ignore lúc đó), sau đó mới bị gitignore
	f2 := newFixtureWith(t, func(f *fixture) { f.write(".env", "secret1\n") })
	f2.write(".gitignore", ".env\n")
	f2.write(".env", "secret2\n")
	p := f2.plan("S2")
	d := f2.deps()
	if err := ResolveCode(bg, d, &p); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Skipped, ".env") {
		t.Fatalf("skipped = %v", p.Skipped)
	}
	for _, c := range append(append([]gitx.FileChange{}, p.OwnChanges...), p.ForeignChanges...) {
		if c.Path == ".env" || c.OldPath == ".env" {
			t.Fatal(".env không được nằm trong thay đổi khôi phục")
		}
	}
	ref, _ := Backup(bg, d)
	// kể cả kế hoạch bị sửa tay đưa .env trở lại, Apply vẫn bỏ qua path trong Skipped
	p.ForeignChanges = append(p.ForeignChanges, gitx.FileChange{Path: ".env", Type: gitx.Deleted})
	if err := Apply(bg, d, p, ref); err != nil {
		t.Fatal(err)
	}
	if got := f2.read(".env"); got != "secret2\n" {
		t.Fatalf(".env = %q", got)
	}
	if f2.exists("src/b.go") {
		t.Fatal("phần còn lại vẫn phải khôi phục")
	}
}
