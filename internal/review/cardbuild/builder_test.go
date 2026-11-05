package cardbuild

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
)

type fx struct {
	t   *testing.T
	dir string
	in  ReviewInput
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		copyFile(t, p, filepath.Join(to, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T, key, fixture string, flow model.Flow, withV1 bool) fx {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "T")
	git(t, dir, "config", "user.email", "t@e.com")
	paths := model.FeaturePaths{RepoRoot: dir, Key: model.FeatureKey(key)}
	copyDir(t, filepath.Join("testdata", fixture), paths.Dir())
	if withV1 {
		src := filepath.Join("testdata", "00001_init.sql")
		if own := filepath.Join("testdata", fixture+"-00001_init.sql"); fileExists(own) {
			src = own
		}
		copyFile(t, src, filepath.Join(dir, "db", "migrations", "00001_init.sql"))
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	repo := gitx.New(dir)
	base, err := repo.SnapshotTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := model.LoadRepoConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta := model.FeatureMeta{Key: model.FeatureKey(key), Title: "t", Flow: flow, BaseTree: base, Owner: "o"}
	return fx{t, dir, ReviewInput{Paths: paths, Meta: meta, Flow: flow, Config: cfg, Git: repo, Adapter: golang.Adapter{}}}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func pay1(t *testing.T) fx { return setup(t, "PAY-1", "pay1", model.FlowFull, true) }

func (f fx) build(g gate.Gate) review.ReviewCard {
	f.t.Helper()
	c, err := BuildCard(f.in, g)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f fx) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f fx) commit(msg string) {
	git(f.t, f.dir, "add", "-A")
	git(f.t, f.dir, "commit", "-q", "-m", msg)
}

func toJSON(t *testing.T, c review.ReviewCard) string {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func codes(c review.ReviewCard) []string {
	out := []string{}
	for _, f := range c.Flags {
		out = append(out, f.Code)
	}
	return out
}

func findFlag(c review.ReviewCard, code string) *review.ReviewFlag {
	for i := range c.Flags {
		if c.Flags[i].Code == code {
			return &c.Flags[i]
		}
	}
	return nil
}

func noCode(t *testing.T, c review.ReviewCard, name string, banned ...string) {
	t.Helper()
	for _, b := range banned {
		if slices.Contains(codes(c), b) {
			t.Errorf("%s: không được có cờ %s (có %v)", name, b, codes(c))
		}
	}
}

func table(c review.ReviewCard, name string) *review.TablePart {
	for i := range c.Design.Tables {
		if c.Design.Tables[i].Name == name {
			return &c.Design.Tables[i]
		}
	}
	return nil
}

func step(c review.ReviewCard, id string) *review.StepPart {
	for i := range c.Design.Steps {
		if c.Design.Steps[i].ID == id {
			return &c.Design.Steps[i]
		}
	}
	return nil
}

func colNames(cols []review.SqlColumn) []string {
	out := []string{}
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func TestPay1DesignHasExactlyTwoWarnFlags(t *testing.T) {
	c := pay1(t).build(gate.DESIGN)
	var loud []string
	for _, f := range c.Flags {
		if f.Level == review.HIGH || f.Level == review.WARN {
			loud = append(loud, f.Code+":"+string(f.Level))
		}
	}
	slices.Sort(loud)
	eq(t, "loud", loud, []string{"SCOPE_MATCHES_OLD_MIGRATION:WARN", "SCOPE_OVERLAP:HIGH"})
	if f := findFlag(c, "DB_CREATE_ONLY"); f == nil || f.Level != review.INFO {
		t.Errorf("thiếu DB_CREATE_ONLY INFO: %v", c.Flags)
	}
}

func TestPay1DesignErAndPlanned(t *testing.T) {
	c := pay1(t).build(gate.DESIGN)
	daily := table(c, "daily_limit")
	eq(t, "status", daily.Status, "NEW")
	var pk review.SqlColumn
	for _, col := range daily.Columns {
		if col.Name == "account_id" {
			pk = col
		}
	}
	for _, w := range []string{"PK", "FK"} {
		if !slices.Contains(pk.Constraints, w) {
			t.Errorf("constraints %v thiếu %s", pk.Constraints, w)
		}
	}
	eq(t, "references", pk.References, "account(id)")
	account := table(c, "account")
	eq(t, "account status", account.Status, "EXISTING")
	if len(account.Columns) == 0 {
		t.Error("account không có cột")
	}
	s2 := step(c, "S2")
	if !slices.Contains(s2.PlannedFiles, "db/migrations/00002_daily_limit.sql") {
		t.Errorf("planned %v", s2.PlannedFiles)
	}
	eq(t, "S2 emptyGlobs", s2.EmptyGlobs, []string{"internal/**/limit/**"})
	eq(t, "S1 emptyGlobs", step(c, "S1").EmptyGlobs, []string{"internal/**/limit/web/**"})
}

func TestOwnDraftAlreadyCopiedIntoMigrationDirIsNotPreExisting(t *testing.T) {
	f := pay1(t)
	copyFile(t, filepath.Join(f.in.Paths.DbDrafts(), "00002_daily_limit.sql"), filepath.Join(f.dir, "db", "migrations", "00002_daily_limit.sql"))
	f.commit("copy draft")
	for _, g := range []gate.Gate{gate.DESIGN, gate.REVIEW} {
		c := f.build(g)
		noCode(t, c, string(g), "DB_TOUCHES_EXISTING")
		if x := findFlag(c, "DB_CREATE_ONLY"); x == nil || x.Level != review.INFO {
			t.Errorf("%s: thiếu DB_CREATE_ONLY INFO", g)
		}
		for _, x := range c.Flags {
			if x.Code != "SCOPE_MATCHES_OLD_MIGRATION" {
				continue
			}
			if !slices.Contains(x.Refs, "db/migrations/00001_init.sql") || slices.Contains(x.Refs, "db/migrations/00002_daily_limit.sql") {
				t.Errorf("%s: refs %v", g, x.Refs)
			}
		}
		if g == gate.DESIGN {
			eq(t, "daily status", table(c, "daily_limit").Status, "NEW")
			eq(t, "touches", len(c.Design.TouchesExisting), 0)
		}
	}
}

func TestPay1DesignParts(t *testing.T) {
	c := pay1(t).build(gate.DESIGN)
	var ids []string
	for _, s := range c.Design.Steps {
		ids = append(ids, s.ID)
	}
	eq(t, "steps", ids, []string{"S1", "S2"})
	if table(c, "daily_limit") == nil || table(c, "daily_limit").Status != "NEW" {
		t.Error("daily_limit phải là NEW")
	}
	eq(t, "touches", len(c.Design.TouchesExisting), 0)
	if len(c.Design.APILines) == 0 {
		t.Error("thiếu apiLines")
	}
	if c.Brd != nil || c.Cases != nil {
		t.Error("thẻ DESIGN không có brd/cases")
	}
}

func TestFix2HasNoWarnFlags(t *testing.T) {
	f := setup(t, "FIX-2", "fix2", model.FlowLite, false)
	c := f.build(gate.CASES)
	for _, x := range c.Flags {
		if x.Level == review.WARN || x.Level == review.HIGH {
			t.Errorf("không được có cờ lớn: %v", x)
		}
	}
	if c.Cases.RuleMatrix != nil {
		t.Error("ruleMatrix phải nil")
	}
	noCode(t, c, "fix2", "CASE_VALUE_NOT_IN_BRD", "BRD_RULE_NO_ID", "CASES_RULE_UNCOVERED")
}

func TestCasesRowsAndCoverage(t *testing.T) {
	f := pay1(t)
	c := f.build(gate.CASES)
	all, err := spec.ReadCases(f.in.Paths.Cases())
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "rows", len(c.Cases.Rows), len(all.Cases))
	if _, ok := c.Cases.Coverage.Get("S2")["happy"]; !ok {
		t.Error("coverage S2 thiếu happy")
	}
	for _, r := range c.Cases.Rows {
		if n := len([]rune(r.Given)); n > 80 {
			t.Errorf("given dài %d", n)
		}
		if len(r.Expect) < 3 || strings.Trim(r.Expect[:3], "0123456789") != "" {
			t.Errorf("expect %q", r.Expect)
		}
	}
	if c.Cases.RuleMatrix != nil {
		t.Error("ruleMatrix phải nil khi BRD không có ID")
	}
}

func TestDeterministicJson(t *testing.T) {
	f := pay1(t)
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW} {
		eq(t, string(g), toJSON(t, f.build(g)), toJSON(t, f.build(g)))
	}
	j := toJSON(t, f.build(gate.DESIGN))
	for _, banned := range []string{`"brd":`, `"cases":`} {
		if strings.Contains(j, banned) {
			t.Errorf("JSON thẻ DESIGN không được có %s", banned)
		}
	}
}

func TestMissingBrdGivesNaNotException(t *testing.T) {
	f := pay1(t)
	if err := os.Remove(f.in.Paths.Brd()); err != nil {
		t.Fatal(err)
	}
	c := f.build(gate.BRD)
	ok := false
	for _, x := range c.Flags {
		ok = ok || x.Level == review.NA && strings.Contains(x.Message, "brd.md")
	}
	if !ok {
		t.Errorf("thiếu cờ NA brd.md: %v", c.Flags)
	}
	if c.Brd != nil {
		t.Error("brd phải nil")
	}
}

func TestLegacyFeatureWithoutAssumptions(t *testing.T) {
	c := pay1(t).build(gate.BRD)
	if c.AssumptionsSection == nil || *c.AssumptionsSection {
		t.Error("assumptionsSection phải false")
	}
	if x := findFlag(c, "ASSUMPTIONS_EMPTY"); x == nil || x.Level != review.INFO || x.Message != "Agent không ghi giả định nào" {
		t.Errorf("ASSUMPTIONS_EMPTY: %v", x)
	}
	eq(t, "rules", c.Brd.Rules, 4)
	eq(t, "rulesWithId", c.Brd.RulesWithID, 0)
	if findFlag(c, "BRD_RULE_NO_ID") == nil {
		t.Error("thiếu BRD_RULE_NO_ID")
	}
}

func TestReviewCardHasFlagsOnly(t *testing.T) {
	c := pay1(t).build(gate.REVIEW)
	if c.Brd != nil || c.Design != nil || c.Cases != nil {
		t.Error("thẻ REVIEW chỉ có cờ")
	}
	for _, w := range []string{"SCOPE_OVERLAP", "DEPENDENCY_ADDED"} {
		if !slices.Contains(codes(c), w) {
			t.Errorf("thiếu %s trong %v", w, codes(c))
		}
	}
}

func TestBrokenYamlGivesNaNotException(t *testing.T) {
	f := pay1(t)
	f.write(f.in.Paths.DesignYaml(), "steps: [ {{{")
	c := f.build(gate.DESIGN)
	ok := false
	for _, x := range c.Flags {
		ok = ok || x.Level == review.NA && strings.Contains(x.Message, "design.yaml")
	}
	if !ok {
		t.Errorf("thiếu cờ NA design.yaml: %v", c.Flags)
	}
}

const brdIDs = "## Quy tắc nghiệp vụ\n- BR-1: a\n- BR-2: b\n- BR-3: c\n"

func TestBrdWithIdsHasNoCoverageFlags(t *testing.T) {
	f := pay1(t)
	f.write(f.in.Paths.Brd(), brdIDs)
	c := f.build(gate.BRD)
	noCode(t, c, "brd", "CASES_RULE_UNCOVERED", "BRD_RULE_NO_ID")
	eq(t, "rulesWithId", c.Brd.RulesWithID, 3)
}

func TestRuleMatrixAndNaturalStepOrder(t *testing.T) {
	f := pay1(t)
	f.write(f.in.Paths.Brd(), brdIDs)
	f.write(f.in.Paths.DesignYaml(), `steps:
  - { id: S10, title: x, scope: ["a/**"], budget: { files: 1, lines: 10 } }
  - { id: S2, title: y, scope: ["b/**"], budget: { files: 1, lines: 10 } }
  - { id: S1, title: z, scope: ["c/**"], budget: { files: 1, lines: 10 } }
`)
	f.write(f.in.Paths.Cases(), `cases:
  - { id: C10, step: S10, kind: happy, title: t, rules: [BR-1], then: { status: 200 } }
  - { id: C2, step: S2, kind: error, title: t, rules: [BR-1], then: { status: 400 } }
  - { id: C3, step: S2, kind: boundary, title: t, rules: [BR-2], then: { status: 200 } }
  - { id: C4, step: S2, kind: happy, title: t, rules: [BR-1], then: { status: 200 } }
`)
	cases := f.build(gate.CASES)
	m := cases.Cases.RuleMatrix
	eq(t, "keys", m.Keys(), []string{"BR-1", "BR-2", "BR-3"})
	eq(t, "BR-1 happy", m.Get("BR-1")["happy"], []string{"C4", "C10"})
	eq(t, "BR-1 error", m.Get("BR-1")["error"], []string{"C2"})
	eq(t, "BR-2 boundary", m.Get("BR-2")["boundary"], []string{"C3"})
	// M10: mọi quy tắc có đủ các loại case như coverage, loại chưa có case là danh sách rỗng
	if len(m.Get("BR-3")) != 3 {
		t.Errorf("BR-3 phải đủ 3 loại: %v", m.Get("BR-3"))
	}
	for k, v := range m.Get("BR-3") {
		if v == nil || len(v) != 0 {
			t.Errorf("BR-3 %s phải là danh sách rỗng không nil: %#v", k, v)
		}
	}
	eq(t, "BR-2 happy", len(m.Get("BR-2")["happy"]), 0)
	if x := findFlag(cases, "CASES_RULE_UNCOVERED"); x == nil || !reflect.DeepEqual(x.Refs, []string{"BR-3"}) {
		t.Errorf("CASES_RULE_UNCOVERED: %v", x)
	}
	eq(t, "coverage keys", cases.Cases.Coverage.Keys(), []string{"S1", "S2", "S10"})
	for k, v := range cases.Cases.Coverage.Get("S1") {
		eq(t, "S1 "+k, v, 0)
	}
	var ids []string
	for _, s := range f.build(gate.DESIGN).Design.Steps {
		ids = append(ids, s.ID)
	}
	eq(t, "design steps", ids, []string{"S1", "S2", "S10"})
	j := toJSON(t, cases)
	if !strings.Contains(j, `"coverage":{"S1":{`) || strings.Index(j, `"S2":{`) > strings.Index(j, `"S10":{`) {
		t.Errorf("coverage phải giữ thứ tự tự nhiên trong JSON: %s", j)
	}
}

func TestReviewCardHasNoDocNoise(t *testing.T) {
	f := pay1(t)
	_ = os.Remove(f.in.Paths.DesignMd())
	_ = os.Remove(f.in.Paths.DesignYaml())
	noCode(t, f.build(gate.REVIEW), "review", "DOC_UNREADABLE", "ASSUMPTIONS_EMPTY", "FILES_FAILED")
}

func hasNull(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	}
	return false
}

func TestJsonHasNoNullsOrValidKey(t *testing.T) {
	f := pay1(t)
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW} {
		j := toJSON(t, f.build(g))
		if strings.Contains(j, `"valid"`) {
			t.Errorf("%s: có khóa valid", g)
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(j), &v); err != nil {
			t.Fatalf("%s: %v\n%s", g, err, j)
		}
		if v["gate"] == nil {
			t.Errorf("%s: thiếu gate", g)
		}
		if hasNull(v) {
			t.Errorf("%s: JSON có null: %s", g, j)
		}
	}
}

// ---- sửa sau review tổng ----

func TestCasesAndReviewCardsCarryNoAssumptions(t *testing.T) {
	f := pay1(t)
	for _, g := range []gate.Gate{gate.CASES, gate.REVIEW} {
		c := f.build(g)
		if c.Assumptions != nil || c.AssumptionsSection != nil {
			t.Errorf("%s: không có giả định", g)
		}
		noCode(t, c, string(g), "ASSUMPTIONS_EMPTY")
		j := toJSON(t, c)
		if strings.Contains(j, `"assumptions"`) || strings.Contains(j, `"assumptionsSection"`) {
			t.Errorf("%s: JSON có giả định: %s", g, j)
		}
	}
	brd := f.build(gate.BRD)
	if brd.Assumptions == nil || brd.AssumptionsSection == nil || *brd.AssumptionsSection {
		t.Error("thẻ BRD: assumptions không nil, section false")
	}
	if f.build(gate.DESIGN).Assumptions == nil {
		t.Error("thẻ DESIGN: assumptions không nil")
	}
}

func TestCreatePlusUpdateOfExistingTableIsNotCreateOnly(t *testing.T) {
	f := pay1(t)
	f.write(filepath.Join(f.in.Paths.DbDrafts(), "00003_fix.sql"),
		"UPDATE account SET owner_name = 'x' WHERE id = '1';\nINSERT INTO account (id, owner_name) VALUES ('2', 'y');\n")
	c := f.build(gate.DESIGN)
	noCode(t, c, "design", "DB_CREATE_ONLY")
	kinds := []string{}
	for _, v := range c.Design.TouchesExisting {
		kinds = append(kinds, v.Kind)
	}
	if !slices.Contains(kinds, "UPDATE") {
		t.Errorf("touches %v", kinds)
	}
}

func TestHugeStringLiteralInOldMigrationDoesNotKillCard(t *testing.T) {
	f := pay1(t)
	big := strings.Repeat("x", 200_000)
	f.write(filepath.Join(f.dir, "db", "migrations", "00003_seed.sql"),
		"CREATE TABLE seed (id INT PRIMARY KEY, body TEXT DEFAULT 'a');\nINSERT INTO seed VALUES (1, '"+big+"''"+big+"');\n")
	f.commit("seed")
	c := f.build(gate.DESIGN)
	noCode(t, c, "design", "CARD_FAILED", "DB_FAILED")
	if c.Design == nil {
		t.Fatal("thiếu design")
	}
	files, err := f.in.Git.ListFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ex := review.ExistingTables(f.dir, files, f.in.Config.Review, f.in.Adapter.MigrationOrder)
	for _, k := range []string{"account", "seed"} {
		if _, ok := ex[k]; !ok {
			t.Errorf("thiếu bảng %s: %v", k, ex)
		}
	}
}

func TestCaseRuleIdsAreNormalizedLikeBrd(t *testing.T) {
	f := pay1(t)
	f.write(f.in.Paths.Brd(), "## Quy tắc nghiệp vụ\n- BR-01: a\n- **br-2**: b\n")
	f.write(f.in.Paths.Cases(), `cases:
  - { id: C1, step: S1, kind: happy, title: t, rules: [br-1], then: { status: 200 } }
  - { id: C2, step: S1, kind: error, title: t, rules: [BR-002, BR-2], then: { status: 400 } }
`)
	c := f.build(gate.CASES)
	noCode(t, c, "cases", "CASES_RULE_UNCOVERED", "CASES_RULE_UNKNOWN")
	m := c.Cases.RuleMatrix
	eq(t, "keys", m.Keys(), []string{"BR-1", "BR-2"})
	eq(t, "BR-1 happy", m.Get("BR-1")["happy"], []string{"C1"})
	eq(t, "BR-2 error", m.Get("BR-2")["error"], []string{"C2"})
	for _, r := range c.Cases.Rows {
		if r.ID == "C2" {
			eq(t, "C2 rules", r.Rules, []string{"BR-2"})
		}
	}
}

func TestDraftsAreReadInFlywayOrder(t *testing.T) {
	f := pay1(t)
	// version 10 xóa bảng mà version 2 tạo: đọc theo thứ tự chữ (10 trước 2) sẽ để lại bảng "mới"
	f.write(filepath.Join(f.in.Paths.DbDrafts(), "2_tmp.sql"), "CREATE TABLE tmp_x (id INT PRIMARY KEY);\n")
	f.write(filepath.Join(f.in.Paths.DbDrafts(), "10_drop_tmp.sql"), "DROP TABLE tmp_x;\n")
	c := f.build(gate.DESIGN)
	if table(c, "tmp_x") != nil {
		t.Error("tmp_x đã bị DROP, không được còn trong sơ đồ")
	}
	var tmp []string
	for _, p := range step(c, "S2").PlannedFiles {
		if strings.Contains(p, "tmp") {
			tmp = append(tmp, p)
		}
	}
	eq(t, "planned", tmp, []string{"db/migrations/2_tmp.sql", "db/migrations/10_drop_tmp.sql"})
}

func TestAlteredTableListsAddedColumns(t *testing.T) {
	c := setup(t, "ALT-1", "alter1", model.FlowFull, true).build(gate.DESIGN)
	tp := table(c, "account")
	eq(t, "status", tp.Status, "ALTERED")
	eq(t, "columns", colNames(tp.Columns), []string{"id", "owner_name"})
	eq(t, "added", colNames(tp.AddedColumns), []string{"status", "note", "customer_id"})
	eq(t, "constraints", tp.AddedColumns[0].Constraints, []string{"NOT NULL"})
}

func TestFkInAddedColumnPullsReferencedExistingTable(t *testing.T) {
	c := setup(t, "ALT-1", "alter1", model.FlowFull, true).build(gate.DESIGN)
	cust := table(c, "customer")
	eq(t, "status", cust.Status, "EXISTING")
	eq(t, "columns", colNames(cust.Columns), []string{"id", "name"})
}

func TestAlterOnTableCreatedInDraftsMergesIntoNewColumns(t *testing.T) {
	c := setup(t, "ALT-2", "alter2", model.FlowFull, true).build(gate.DESIGN)
	note := table(c, "note")
	eq(t, "status", note.Status, "NEW")
	eq(t, "columns", colNames(note.Columns), []string{"id", "body"})
	if note.AddedColumns != nil {
		t.Error("addedColumns phải nil")
	}
}

func TestNewTableHasNoAddedColumnsField(t *testing.T) {
	c := pay1(t).build(gate.DESIGN)
	if table(c, "daily_limit").AddedColumns != nil {
		t.Error("addedColumns phải nil")
	}
	if strings.Contains(toJSON(t, c), "addedColumns") {
		t.Error("JSON không được có addedColumns")
	}
}

func writeRisk(t *testing.T, f fx) {
	t.Helper()
	rf := risk.RiskFile{
		Level:   "high",
		Policy:  risk.PolicyRef{Version: 3, Hash: "h"},
		Matched: []risk.Match{{Rule: "r1", Level: "high", On: "kw"}},
		Agent:   &spec.Impact{Level: "medium", Labels: []string{"schema-change"}, Reason: "Thêm bảng mới"},
		Override: &risk.Override{From: "medium", To: "high", By: "dev", Reason: "lý do",
			At: time.Unix(0, 0).UTC()},
		Requirements: []risk.Requirement{
			risk.NewRequirement("concurrent", map[string]any{"threads": 8}),
			risk.NewRequirement("coverage", map[string]any{"min": 90}),
		},
	}
	if err := rf.Write(f.in.Paths.Risk()); err != nil {
		t.Fatal(err)
	}
}

func TestRiskFieldsAbsentWithoutRiskYaml(t *testing.T) {
	f := pay1(t)
	for _, g := range []gate.Gate{gate.DESIGN, gate.CASES} {
		c := f.build(g)
		if c.Risk != nil || c.RiskRequirements != nil {
			t.Errorf("%s: không có rủi ro", g)
		}
		j := toJSON(t, c)
		if strings.Contains(j, `"risk"`) || strings.Contains(j, "riskRequirements") {
			t.Errorf("%s: JSON có rủi ro: %s", g, j)
		}
		noCode(t, c, string(g), "RISK_REQ")
	}
}

func TestCasesCard_designWithImpactButNoRiskYaml_flagged(t *testing.T) {
	f := pay1(t)
	b, _ := os.ReadFile(f.in.Paths.DesignYaml())
	f.write(f.in.Paths.DesignYaml(), string(b)+"\nimpact: { level: high, labels: [], reason: r }\n")
	c := f.build(gate.CASES)
	n := 0
	for _, x := range c.Flags {
		if x.Code == "RISK_FILE" {
			n++
			if x.Level != review.HIGH || !strings.Contains(x.Message, "thiếu risk.yaml") || !strings.Contains(x.Message, "aisdlc submit cases") {
				t.Errorf("RISK_FILE: %v", x)
			}
		}
	}
	eq(t, "số cờ RISK_FILE", n, 1)
	noCode(t, f.build(gate.DESIGN), "design", "RISK_FILE")
}

func TestRiskFieldsOnDesignAndCasesCards(t *testing.T) {
	f := pay1(t)
	writeRisk(t, f)
	d := f.build(gate.DESIGN)
	eq(t, "level", d.Risk.Level, "high")
	eq(t, "policyVersion", d.Risk.PolicyVersion, 3)
	eq(t, "matched", len(d.Risk.Matched), 1)
	eq(t, "override.from", d.Risk.Override.From, "medium")
	eq(t, "override.at", d.Risk.Override.At, "1970-01-01T00:00:00Z")
	eq(t, "agent", *d.Risk.Agent, review.RiskAgent{Level: "medium", Labels: []string{"schema-change"}, Reason: "Thêm bảng mới"})
	if d.RiskRequirements != nil {
		t.Error("thẻ DESIGN không có riskRequirements")
	}
	c := f.build(gate.CASES)
	eq(t, "cases level", c.Risk.Level, "high")
	var got []string
	for _, r := range c.RiskRequirements {
		got = append(got, r.ID+":"+r.Status)
	}
	eq(t, "requirements", got, []string{"concurrent:MISSING", "coverage:LATER"})
	n := 0
	for _, x := range c.Flags {
		if x.Code == "RISK_REQ" {
			n++
			eq(t, "RISK_REQ level", x.Level, review.HIGH)
		}
	}
	eq(t, "số RISK_REQ", n, 1)
	j := toJSON(t, c)
	for _, w := range []string{`"riskRequirements"`, `"policyVersion":3`,
		`"agent":{"level":"medium","labels":["schema-change"],"reason":"Thêm bảng mới"}`} {
		if !strings.Contains(j, w) {
			t.Errorf("JSON thiếu %s: %s", w, j)
		}
	}
	if f.build(gate.BRD).Risk != nil {
		t.Error("thẻ BRD không có risk")
	}
}
