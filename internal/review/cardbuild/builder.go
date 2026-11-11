// Package cardbuild dựng review card của một cổng từ file của tính năng và git. Tách khỏi package review vì
// builder cần risk (CheckRequirements, ReadRiskFile) mà risk đã import review.
package cardbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
)

const (
	maxMatched = 20
	textMax    = 80
)

// ReviewInput là đầu vào dựng thẻ.
type ReviewInput struct {
	Paths   model.FeaturePaths
	Meta    model.FeatureMeta
	Flow    model.Flow
	Config  model.RepoConfig
	Git     *gitx.Repo
	Adapter lang.Adapter
}

type flagList struct{ items []review.ReviewFlag }

func (l *flagList) add(f ...review.ReviewFlag) { l.items = append(l.items, f...) }

var spaces = regexp.MustCompile(`[ \t\n\x0B\f\r]+`)

// BuildCard dựng thẻ của cổng g; không bao giờ trả lỗi: phần không dựng được thành cờ NA (lỗi luôn nil).
func BuildCard(in ReviewInput, g gate.Gate) (card review.ReviewCard, err error) {
	flags := &flagList{}
	defer func() {
		if r := recover(); r != nil {
			flags.add(na("CARD_FAILED", "Không dựng được thẻ "+string(g)+": "+why(panicErr(r))))
			card = review.ReviewCard{Gate: string(g), Flags: review.SortFlags(flags.items)}
			err = nil
		}
	}()
	switch g {
	case gate.BRD:
		card = brdCard(in, flags)
	case gate.DESIGN:
		card = designCard(in, flags)
	case gate.CASES:
		card = casesCard(in, flags)
	default:
		card = reviewCard(in, flags)
	}
	return card, nil
}

func panicErr(r any) error {
	if e, ok := r.(error); ok {
		return e
	}
	return fmt.Errorf("%v", r)
}

// try chạy f; panic thành lỗi để phần thẻ đó thành cờ NA thay vì làm hỏng cả thẻ.
func try(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicErr(r)
		}
	}()
	f()
	return nil
}

func why(err error) string {
	return strings.SplitN(err.Error(), "\n", 2)[0]
}

func na(code, message string) review.ReviewFlag {
	return review.NewFlag(code, review.NA, message, "", nil)
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func ctx() context.Context { return context.Background() }

// ---- BRD ----

func brdCard(in ReviewInput, flags *flagList) review.ReviewCard {
	md, ok := read(in.Paths.Brd(), "brd.md", flags)
	var part *review.BrdPart
	assumptions := []review.Assumption{}
	section := false
	if ok {
		if err := try(func() {
			d := review.ParseBRD(md)
			withID := 0
			for _, r := range d.Rules {
				if r.ID != "" {
					withID++
				}
			}
			p := review.BrdPart{Rules: len(d.Rules), RulesWithID: withID, AcceptanceCriteria: d.AcceptanceCriteria,
				OutOfScope: nonNil(d.OutOfScope), AnsweredQuestions: nonNil(d.AnsweredQuestions)}
			a := d.Assumptions
			if a == nil {
				a = []review.Assumption{}
			}
			flags.add(review.RuleIDs(d)...)
			assumptionsFlag(a, flags)
			part, assumptions, section = &p, a, d.AssumptionsSection
		}); err != nil {
			flags.add(na("DOC_UNREADABLE", "không đọc được brd.md: "+why(err)))
		}
	}
	return review.ReviewCard{Gate: "BRD", Flags: review.SortFlags(flags.items), Assumptions: assumptions,
		AssumptionsSection: &section, Brd: part}
}

// ---- DESIGN ----

func designCard(in ReviewInput, flags *flagList) review.ReviewCard {
	files := files(in, flags)
	var doc *review.DesignDoc
	if md, ok := read(in.Paths.DesignMd(), "design.md", flags); ok {
		if err := try(func() {
			d := review.ParseDesign(md)
			assumptionsFlag(d.Assumptions, flags)
			doc = &d
		}); err != nil {
			flags.add(na("DOC_UNREADABLE", "không đọc được design.md: "+why(err)))
		}
	}
	calc := designCalc(in, files, flags)
	part := review.DesignPart{Steps: calc.stepParts, APILines: []string{}, StatusCodes: []string{},
		APIRows: []review.ApiRow{}, Tables: calc.db.tables, TouchesExisting: calc.db.touches}
	assumptions, section := []review.Assumption{}, false
	if doc != nil {
		part.APILines, part.StatusCodes = nonNil(doc.APILines), nonNil(doc.StatusCodes)
		if doc.APIRows != nil {
			part.APIRows = doc.APIRows
		}
		if doc.Assumptions != nil {
			assumptions = doc.Assumptions
		}
		section = doc.AssumptionsSection
	}
	return review.ReviewCard{Gate: "DESIGN", Flags: review.SortFlags(flags.items), Assumptions: assumptions,
		AssumptionsSection: &section, Design: &part, Risk: riskPart(readRisk(in))}
}

// designCalc là phần dùng chung của thẻ DESIGN và REVIEW: bước, SQL nháp và cờ scope/SQL (cộng cờ NA khi thiếu file).
type designResult struct {
	db        dbInfo
	stepParts []review.StepPart
}

func designCalc(in ReviewInput, allFiles []string, flags *flagList) designResult {
	res := designResult{stepParts: []review.StepPart{}}
	rs := in.Config.Review
	files := withoutOwnDraftCopies(in, allFiles, rs)
	steps := designSteps(in, flags)
	res.db = readDB(in, files, rs, flags)
	if err := try(func() {
		flags.add(review.ScopeFlags(steps, files, rs, in.Adapter.SampleFileName())...)
		res.stepParts = stepParts(steps, files, res.db.planned)
	}); err != nil {
		flags.add(na("SCOPE_FAILED", "Không tính được scope: "+why(err)))
	}
	if err := try(func() {
		names := make([]string, 0, len(res.db.existing))
		for n := range res.db.existing {
			names = append(names, n)
		}
		sort.Strings(names)
		flags.add(review.SqlFlags(res.db.drafts, names)...)
	}); err != nil {
		flags.add(na("DB_FAILED", "Không tính được cờ SQL: "+why(err)))
	}
	return res
}

// withoutOwnDraftCopies bỏ khỏi danh sách file các migration trùng tên với SQL nháp của chính tính năng (nháp đã
// được chép vào thư mục migration): chúng không phải "đã có từ trước".
func withoutOwnDraftCopies(in ReviewInput, files []string, rs model.ReviewSettings) []string {
	entries, err := os.ReadDir(in.Paths.DbDrafts())
	if err != nil {
		return files
	}
	names := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			names[e.Name()] = true
		}
	}
	out := []string{}
	for _, f := range files {
		n := f[strings.LastIndexByte(f, '/')+1:]
		if !names[n] {
			out = append(out, f)
			continue
		}
		inDir := false
		for _, d := range rs.MigrationDirs {
			p := trimSlash(d) + "/"
			if strings.HasPrefix(f, p) || strings.Contains(f, "/"+p) {
				inDir = true
			}
		}
		if !inDir {
			out = append(out, f)
		}
	}
	return out
}

func designSteps(in ReviewInput, flags *flagList) []spec.StepSpec {
	if _, err := os.Stat(in.Paths.DesignYaml()); err != nil {
		flags.add(na("DOC_UNREADABLE", "không đọc được design.yaml: file không tồn tại"))
		return nil
	}
	d, err := spec.ReadDesign(in.Paths.DesignYaml())
	if err != nil {
		flags.add(na("DOC_UNREADABLE", "không đọc được design.yaml: "+why(err)))
		return nil
	}
	return spec.EffectiveSteps(model.FlowFull, &d, spec.CasesSpec{})
}

func stepParts(steps []spec.StepSpec, files, planned []string) []review.StepPart {
	out := []review.StepPart{}
	for _, st := range steps {
		// mỗi glob chỉ biên dịch một lần cho cả danh sách file
		scope := review.ScopeMatcher(st.Scope)
		matched := []string{}
		for _, f := range files {
			if scope(f) {
				matched = append(matched, f)
			}
		}
		plannedHit := []string{}
		for _, p := range planned {
			if scope(p) {
				plannedHit = append(plannedHit, p)
			}
		}
		empty := []string{}
		for _, g := range st.Scope {
			if !review.ValidGlob(g) {
				continue
			}
			one := review.ScopeMatcher([]string{g})
			// file khớp cả scope thì đã nằm trong matched: chỉ cần quét matched và planned
			any := false
			for _, f := range append(append([]string{}, matched...), plannedHit...) {
				if one(f) {
					any = true
					break
				}
			}
			if !any {
				empty = append(empty, g)
			}
		}
		shown := matched
		if len(shown) > maxMatched {
			shown = shown[:maxMatched]
		}
		out = append(out, review.StepPart{ID: st.ID, Title: st.Title, Scope: nonNil(st.Scope), Budget: st.Budget,
			MatchedCount: len(matched), MatchedFiles: shown, PlannedFiles: plannedHit, EmptyGlobs: empty})
	}
	sort.SliceStable(out, func(i, j int) bool { return review.NaturalCompare(out[i].ID, out[j].ID) < 0 })
	return out
}

// ---- DB nháp ----

type dbInfo struct {
	drafts   []review.SqlStatement
	existing map[string][]review.SqlColumn
	planned  []string
	tables   []review.TablePart
	touches  []review.SqlStatementView
}

func readDB(in ReviewInput, files []string, rs model.ReviewSettings, flags *flagList) dbInfo {
	d := dbInfo{existing: map[string][]review.SqlColumn{}, planned: []string{}, tables: []review.TablePart{},
		touches: []review.SqlStatementView{}}
	err := try(func() {
		d.existing = review.ExistingTables(in.Paths.RepoRoot, files, rs, in.Adapter.MigrationOrder)
		dir := in.Paths.DbDrafts()
		var names []string
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
					names = append(names, e.Name())
				}
			}
		}
		names = in.Adapter.MigrationOrder(names)
		target := ""
		if len(rs.MigrationDirs) > 0 {
			target = trimSlash(rs.MigrationDirs[0]) + "/"
		}
		var drafts []review.SqlStatement
		planned := []string{}
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				flags.add(na("DOC_UNREADABLE", "không đọc được "+n+": "+why(err)))
				continue
			}
			if err := try(func() { drafts = append(drafts, review.ParseSQL(string(b))...) }); err != nil {
				flags.add(na("DOC_UNREADABLE", "không đọc được "+n+": "+why(err)))
				continue
			}
			planned = append(planned, target+n)
		}
		d.drafts, d.planned = drafts, planned
		d.tables = tables(drafts, d.existing)
		d.touches = touches(drafts, d.existing)
	})
	if err != nil {
		flags.add(na("DB_FAILED", "Không đọc được SQL nháp: "+why(err)))
	}
	return d
}

func trimSlash(s string) string { return strings.TrimSuffix(s, "/") }

func unq(t string) string { return t[strings.LastIndexByte(t, '.')+1:] }

func byShortName(existing map[string][]review.SqlColumn) map[string][]review.SqlColumn {
	m := map[string][]review.SqlColumn{}
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m[unq(k)] = existing[k]
	}
	return m
}

func hasCol(cols []review.SqlColumn, name string) bool {
	for _, e := range cols {
		if strings.EqualFold(e.Name, name) {
			return true
		}
	}
	return false
}

func cols(c []review.SqlColumn) []review.SqlColumn {
	out := make([]review.SqlColumn, 0, len(c))
	for _, x := range c {
		if x.Constraints == nil {
			x.Constraints = []string{}
		}
		out = append(out, x)
	}
	return out
}

var refClean = regexp.MustCompile("[`\"\\[\\]]")
var refParen = regexp.MustCompile(`\(.*$`)

func tables(drafts []review.SqlStatement, existing map[string][]review.SqlColumn) []review.TablePart {
	ex := byShortName(existing)
	out := map[string]review.TablePart{}
	for _, st := range drafts {
		if st.Table == "" {
			continue
		}
		t := unq(st.Table)
		_, known := ex[t]
		switch st.Kind {
		case "CREATE_TABLE":
			if known {
				out[t] = review.TablePart{Name: t, Status: "ALTERED", Columns: cols(ex[t])}
			} else {
				out[t] = review.TablePart{Name: t, Status: "NEW", Columns: cols(st.Columns)}
			}
		case "ALTER_TABLE", "TRUNCATE":
			draftNew, hasNew := out[t]
			if !known && hasNew && draftNew.Status == "NEW" && st.Kind == "ALTER_TABLE" {
				merged := append([]review.SqlColumn{}, draftNew.Columns...)
				for _, c := range st.Columns {
					if !hasCol(merged, c.Name) {
						merged = append(merged, c)
					}
				}
				out[t] = review.TablePart{Name: t, Status: "NEW", Columns: cols(merged)}
			}
			if known {
				prev, hasPrev := out[t]
				added := []review.SqlColumn{}
				if hasPrev && prev.Status == "ALTERED" && prev.AddedColumns != nil {
					added = append(added, prev.AddedColumns...)
				}
				if st.Kind == "ALTER_TABLE" {
					for _, c := range st.Columns {
						if !hasCol(ex[t], c.Name) && !hasCol(added, c.Name) {
							added = append(added, c)
						}
					}
				}
				tp := review.TablePart{Name: t, Status: "ALTERED", Columns: cols(ex[t])}
				if len(added) > 0 {
					tp.AddedColumns = cols(added)
				}
				out[t] = tp
			}
		case "DROP":
			if known {
				out[t] = review.TablePart{Name: t, Status: "DROPPED", Columns: cols(ex[t])}
			} else {
				delete(out, t)
			}
		}
	}
	names := sortedKeys(out)
	for _, n := range names {
		tp := out[n]
		all := append(append([]review.SqlColumn{}, tp.Columns...), tp.AddedColumns...)
		for _, c := range all {
			if strings.TrimSpace(c.References) == "" {
				continue
			}
			ref := unq(strings.ToLower(strings.TrimSpace(refParen.ReplaceAllString(refClean.ReplaceAllString(c.References, ""), ""))))
			if _, have := out[ref]; !have {
				if _, ok := ex[ref]; ok {
					out[ref] = review.TablePart{Name: ref, Status: "EXISTING", Columns: cols(ex[ref])}
				}
			}
		}
	}
	res := make([]review.TablePart, 0, len(out))
	for _, n := range sortedKeys(out) {
		res = append(res, out[n])
	}
	return res
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func touches(drafts []review.SqlStatement, existing map[string][]review.SqlColumn) []review.SqlStatementView {
	ex := byShortName(existing)
	out := []review.SqlStatementView{}
	for _, st := range drafts {
		if st.Table == "" {
			continue
		}
		if _, ok := ex[unq(st.Table)]; !ok {
			continue
		}
		if st.Kind == "INSERT" || st.Kind == "OTHER" {
			continue
		}
		out = append(out, review.SqlStatementView{Kind: st.Kind, Table: st.Table, Text: st.Text})
	}
	return out
}

// ---- CASES ----

func casesCard(in ReviewInput, flags *flagList) review.ReviewCard {
	cases, ok := readCases(in, flags)
	if !ok {
		return review.ReviewCard{Gate: "CASES", Flags: review.SortFlags(flags.items)}
	}
	lite := in.Flow == model.FlowLite
	var brd *review.BrdDoc
	brdMd, haveBrd := "", false
	if !lite {
		if brdMd, haveBrd = read(in.Paths.Brd(), "brd.md", flags); haveBrd {
			if err := try(func() {
				d := review.ParseBRD(brdMd)
				brd = &d
			}); err != nil {
				flags.add(na("DOC_UNREADABLE", "không đọc được brd.md: "+why(err)))
			}
		}
	}
	if err := try(func() {
		if lite {
			files := files(in, flags)
			flags.add(review.ScopeFlags(spec.EffectiveSteps(model.FlowLite, nil, cases), files, in.Config.Review,
				in.Adapter.SampleFileName())...)
		} else if haveBrd {
			flags.add(review.ValuesNotInBrd(cases, brdMd, in.Config.Review.ValueFields)...)
			if brd != nil {
				flags.add(review.RuleIDs(*brd)...)
				flags.add(review.RuleCoverage(*brd, cases)...)
			}
		}
	}); err != nil {
		flags.add(na("CASES_FLAGS_FAILED", "Không tính được cờ của case: "+why(err)))
	}
	var part *review.CasesPart
	if err := try(func() {
		steps := []string{"S1"}
		if !lite {
			steps = declaredSteps(in)
		}
		p := casesPart(cases, brd, lite, steps)
		part = &p
	}); err != nil {
		flags.add(na("CASES_FAILED", "Không dựng được bảng case: "+why(err)))
	}
	rf := readRisk(in)
	var reqs []review.RiskRequirement
	if rf != nil {
		if err := try(func() {
			var design *spec.DesignSpec
			var dd *review.DesignDoc
			if !lite {
				design, dd = designSpec(in), designDoc(in)
			}
			st := risk.CheckRequirements(*rf, cases, design, brd, dd)
			reqs = make([]review.RiskRequirement, 0, len(st))
			for _, x := range st {
				reqs = append(reqs, review.RiskRequirement{ID: x.ID, Name: x.Name, Status: string(x.Status), Detail: x.Detail})
			}
			for _, f := range risk.RequirementFindings(*rf, st) {
				flags.add(review.NewFlag(f.Code, review.HIGH, f.Message, "", nil))
			}
		}); err != nil {
			reqs = nil
			flags.add(na("RISK_FAILED", "Không kiểm được yêu cầu rủi ro: "+why(err)))
		}
	} else if !lite {
		if _, err := os.Stat(in.Paths.Risk()); errors.Is(err, os.ErrNotExist) {
			if risk.RequiredFor(in.Meta, in.Paths.DesignYaml()) {
				// có dấu riskSchema hoặc design.yaml có impact: không phải tính năng cũ, không được lặng lẽ miễn yêu cầu
				flags.add(review.NewFlag("RISK_FILE", review.HIGH, "thiếu risk.yaml; chạy aisdlc submit cases", "", nil))
			}
		}
	}
	return review.ReviewCard{Gate: "CASES", Flags: review.SortFlags(flags.items), Cases: part,
		Risk: riskPart(rf), RiskRequirements: reqs}
}

// ---- rủi ro (risk.yaml); không có file thì mọi trường rủi ro là nil ----

func readRisk(in ReviewInput) *risk.RiskFile {
	rf, ok, err := risk.ReadRiskFile(in.Paths.Risk())
	if err != nil || !ok {
		return nil
	}
	return &rf
}

func riskPart(r *risk.RiskFile) *review.RiskPart {
	if r == nil {
		return nil
	}
	matched := make([]review.RiskMatch, 0, len(r.Matched))
	for _, m := range r.Matched {
		matched = append(matched, review.RiskMatch{Rule: m.Rule, Level: m.Level, On: m.On})
	}
	p := &review.RiskPart{Level: r.Level, PolicyVersion: r.Policy.Version, Matched: matched}
	if o := r.Override; o != nil {
		ro := review.RiskOverride{From: o.From, To: o.To, By: o.By, Reason: o.Reason}
		if !o.At.IsZero() {
			ro.At = review.InstantString(o.At)
		}
		p.Override = &ro
	}
	if a := r.Agent; a != nil {
		labels := append([]string{}, a.Labels...)
		p.Agent = &review.RiskAgent{Level: a.Level, Labels: labels, Reason: a.Reason}
	}
	return p
}

func designSpec(in ReviewInput) *spec.DesignSpec {
	if _, err := os.Stat(in.Paths.DesignYaml()); err != nil {
		return nil
	}
	d, err := spec.ReadDesign(in.Paths.DesignYaml())
	if err != nil {
		return nil
	}
	return &d
}

func designDoc(in ReviewInput) *review.DesignDoc {
	b, err := os.ReadFile(in.Paths.DesignMd())
	if err != nil {
		return nil
	}
	var d *review.DesignDoc
	if try(func() { x := review.ParseDesign(string(b)); d = &x }) != nil {
		return nil
	}
	return d
}

func readCases(in ReviewInput, flags *flagList) (spec.CasesSpec, bool) {
	if _, err := os.Stat(in.Paths.Cases()); err != nil {
		flags.add(na("DOC_UNREADABLE", "không đọc được cases.yaml: file không tồn tại"))
		return spec.CasesSpec{}, false
	}
	c, err := spec.ReadCases(in.Paths.Cases())
	if err != nil {
		flags.add(na("DOC_UNREADABLE", "không đọc được cases.yaml: "+why(err)))
		return spec.CasesSpec{}, false
	}
	return c, true
}

var caseKinds = []spec.CaseKind{spec.KindHappy, spec.KindError, spec.KindBoundary}

func zeroKinds() map[string]int {
	m := map[string]int{}
	for _, k := range caseKinds {
		m[string(k)] = 0
	}
	return m
}

func emptyKinds() map[string][]string {
	m := map[string][]string{}
	for _, k := range caseKinds {
		m[string(k)] = []string{}
	}
	return m
}

func casesPart(cases spec.CasesSpec, brd *review.BrdDoc, lite bool, stepIDs []string) review.CasesPart {
	rows := []review.CaseRow{}
	cov := map[string]map[string]int{}
	for _, id := range stepIDs {
		cov[id] = zeroKinds()
	}
	for _, c := range cases.Cases {
		kind := string(c.Kind)
		rules := c.Rules
		if rules == nil {
			rules = []string{}
		}
		rows = append(rows, review.CaseRow{ID: c.ID, Step: c.Step, Kind: kind, Title: c.Title, Given: given(c),
			Expect: expect(c), Rules: rules})
		step := c.Step
		if step == "" {
			step = "?"
		}
		if cov[step] == nil {
			cov[step] = zeroKinds()
		}
		cov[step][kind]++
	}
	coverage := review.Coverage{}
	for _, s := range naturalSorted(sortedKeys(cov)) {
		coverage = append(coverage, review.CoverageRow{Step: s, Counts: cov[s]})
	}
	var matrix review.RuleMatrix
	if !lite && brd != nil && anyRuleID(brd) {
		m := map[string]map[string][]string{}
		for _, r := range brd.Rules {
			if r.ID != "" && m[r.ID] == nil {
				m[r.ID] = emptyKinds()
			}
		}
		for _, c := range cases.Cases {
			for _, rid := range c.Rules {
				if byKind := m[rid]; byKind != nil {
					byKind[string(c.Kind)] = append(byKind[string(c.Kind)], c.ID)
				}
			}
		}
		matrix = review.RuleMatrix{}
		for _, rid := range naturalSorted(sortedKeys(m)) {
			for _, l := range m[rid] {
				sort.SliceStable(l, func(i, j int) bool { return review.NaturalCompare(l[i], l[j]) < 0 })
			}
			matrix = append(matrix, review.RuleRow{Rule: rid, ByKind: m[rid]})
		}
	}
	return review.CasesPart{Rows: rows, Coverage: coverage, RuleMatrix: matrix}
}

func anyRuleID(brd *review.BrdDoc) bool {
	for _, r := range brd.Rules {
		if r.ID != "" {
			return true
		}
	}
	return false
}

func naturalSorted(l []string) []string {
	sort.SliceStable(l, func(i, j int) bool { return review.NaturalCompare(l[i], l[j]) < 0 })
	return l
}

// declaredSteps là id bước khai báo trong design.yaml; rỗng khi không đọc được (chỉ để điền hàng 0 case).
func declaredSteps(in ReviewInput) []string {
	d := designSpec(in)
	if d == nil {
		return nil
	}
	var out []string
	for _, s := range d.Steps {
		if s.ID != "" {
			out = append(out, s.ID)
		}
	}
	return out
}

func given(c spec.CaseSpec) string {
	parts := append([]string{}, c.Given.SQL...)
	for _, s := range c.Given.Stubs {
		parts = append(parts, "stub "+s)
	}
	return cut(spaces.ReplaceAllString(strings.Join(parts, "; "), " "))
}

func expect(c spec.CaseSpec) string {
	if c.Then == nil {
		return ""
	}
	var sb strings.Builder
	if c.Then.Body != nil {
		sb.WriteString(compactJSON(c.Then.Body))
	}
	for _, d := range c.Then.DB {
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString("db: " + d.Query)
	}
	rest := ""
	if sb.Len() > 0 {
		rest = " " + cut(spaces.ReplaceAllString(sb.String(), " "))
	}
	return fmt.Sprint(c.Then.Status) + rest
}

func compactJSON(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(b.String(), "\n")
}

func cut(s string) string {
	if utf8.RuneCountInString(s) <= textMax {
		return s
	}
	return string([]rune(s)[:textMax-1]) + "…"
}

// ---- REVIEW ----

func reviewCard(in ReviewInput, flags *flagList) review.ReviewCard {
	files := files(in, flags)
	if err := try(func() {
		if in.Meta.BaseTree == "" {
			flags.add(na("DEPENDENCY_ADDED", "Không có base tree nên không so được dependency"))
		} else {
			flags.add(review.DependencyFlags(ctx(), in.Adapter, in.Git, in.Meta.BaseTree, in.Paths.RepoRoot, files)...)
		}
	}); err != nil {
		flags.add(na("DEPENDENCY_ADDED", "Không tính được dependency: "+why(err)))
	}
	if in.Flow == model.FlowFull {
		// chỉ lấy cờ scope/SQL của DESIGN; bỏ cờ giả định và cờ thiếu file của cổng DESIGN
		tmp := &flagList{}
		designCalc(in, files, tmp)
		for _, f := range tmp.items {
			if strings.HasPrefix(f.Code, "SCOPE_") || strings.HasPrefix(f.Code, "DB_") {
				flags.add(f)
			}
		}
	}
	return review.ReviewCard{Gate: "REVIEW", Flags: review.SortFlags(flags.items)}
}

// ---- chung ----

func files(in ReviewInput, flags *flagList) []string {
	var out []string
	err := try(func() {
		fs, err := in.Git.ListFiles(ctx())
		if err != nil {
			panic(err)
		}
		out = fs
	})
	if err != nil {
		flags.add(na("FILES_FAILED", "Không đọc được danh sách file từ git: "+why(err)))
		return []string{}
	}
	return out
}

func read(path, name string, flags *flagList) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		flags.add(na("DOC_UNREADABLE", "không đọc được "+name))
		return "", false
	}
	return string(b), true
}

func assumptionsFlag(a []review.Assumption, flags *flagList) {
	if len(a) == 0 {
		flags.add(review.NewFlag("ASSUMPTIONS_EMPTY", review.INFO, "Agent không ghi giả định nào", "", nil))
	}
}
