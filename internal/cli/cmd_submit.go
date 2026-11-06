package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
	"aisdlc/internal/yamlx"
)

func newSubmitCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "submit",
		Short: "Kiểm tra tự động artifact của một cổng rồi nộp chờ dev duyệt.",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	sub := func(name, short string, body func(s *submission, f *Feature) (int, error)) *cobra.Command {
		var fo FeatureOption
		sc := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs}
		fo.AddTo(sc)
		sc.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
			warnEnvToken(ctx)
			f, err := fo.Feature(ctx)
			if err != nil {
				return 0, err
			}
			return body(&submission{ctx: ctx}, f)
		})
		return sc
	}
	c.AddCommand(
		sub("brd", "Nộp brd.md.", (*submission).brd),
		sub("design", "Nộp design.md, design.yaml và migration nháp trong db/.", (*submission).design),
		sub("cases", "Nộp cases.yaml: kiểm cấu trúc rồi chạy để chắc case fail trước khi code.", (*submission).cases),
		sub("review", "Nộp kết quả eval (phải ĐẠT) và review-agent.json.", (*submission).review),
	)
	return c
}

// submission là một lần chạy `submit`: risk.yaml vừa được ghi lại trong lệnh này (mức trước, bản mới, chính sách).
type submission struct {
	ctx            *Context
	reclassified   *risk.RiskFile
	levelBefore    string // "" khi chưa có risk.yaml
	reclassifiedBy *risk.RiskPolicy
	// riskReported: chế độ web, event RISK_CLASSIFIED của reclassified đã vào outbox (chỉ gửi một lần)
	riskReported bool
}

func (s *submission) brd(f *Feature) (int, error) {
	if err := s.precheck(f, gate.BRD); err != nil {
		return 0, err
	}
	var findings []spec.Finding
	if !exists(f.Paths.Brd()) {
		findings = append(findings, spec.Finding{Code: "BRD_MISSING", Message: "Chưa có " + f.Paths.Rel(f.Paths.Brd()) + "."})
	} else {
		b, err := os.ReadFile(f.Paths.Brd())
		if err != nil {
			return 0, err
		}
		findings = append(findings, spec.ValidateBRD(string(b))...)
	}
	s.brdRiskPreview(f)
	return s.submit(f, gate.BRD, findings, nil)
}

func (s *submission) design(f *Feature) (int, error) {
	defer s.reportRisk(f, true)
	if err := s.precheck(f, gate.DESIGN); err != nil {
		return 0, err
	}
	l, err := s.policy(f)
	if err != nil {
		return 0, err
	}
	var findings []spec.Finding
	md := ""
	if exists(f.Paths.DesignMd()) {
		b, err := os.ReadFile(f.Paths.DesignMd())
		if err != nil {
			return 0, err
		}
		md = string(b)
	}
	var d *spec.DesignSpec
	if !exists(f.Paths.DesignYaml()) {
		findings = append(findings, spec.Finding{Code: "DESIGN_YAML_INVALID",
			Message: "Chưa có " + f.Paths.Rel(f.Paths.DesignYaml()) + "."})
	} else if ds, err := spec.ReadDesign(f.Paths.DesignYaml()); err != nil {
		findings = append(findings, spec.Finding{Code: "DESIGN_YAML_INVALID", Message: "design.yaml không đọc được: " + err.Error()})
	} else {
		d = &ds
	}
	if d != nil {
		fs, err := guarded(func() ([]spec.Finding, error) { return spec.ValidateDesign(md, *d, l.Policy.Levels), nil })
		if err != nil {
			fs = []spec.Finding{{Code: "DESIGN_YAML_INVALID", Message: "design.yaml thiếu trường bắt buộc: " + err.Error()}}
		}
		findings = append(findings, fs...)
	}
	if len(findings) == 0 {
		x, err := s.migrationCheck(f, "DESIGN_MIGRATION_INVALID")
		if err != nil {
			return 0, err
		}
		if x != nil {
			findings = append(findings, *x)
		}
	}
	if len(findings) == 0 {
		if _, err := s.classify(f, l.Policy, true); err != nil {
			return 0, err
		}
	}
	return s.submit(f, gate.DESIGN, findings, nil)
}

func (s *submission) cases(f *Feature) (int, error) {
	defer s.reportRisk(f, true)
	if err := s.precheck(f, gate.CASES); err != nil {
		return 0, err
	}
	var findings []spec.Finding
	var info []string
	var cs *spec.CasesSpec
	if c, err := spec.ReadCases(f.Paths.Cases()); err != nil {
		findings = append(findings, spec.Finding{Code: "CASES_YAML_INVALID", Message: "cases.yaml không đọc được: " + err.Error()})
	} else {
		cs = &c
	}
	if cs != nil {
		fs, err := guarded(func() ([]spec.Finding, error) {
			return spec.ValidateCases(*cs, s.effectiveSteps(f, *cs), f.Flow()), nil
		})
		if err != nil {
			fs = []spec.Finding{{Code: "CASES_YAML_INVALID", Message: "cases.yaml thiếu trường bắt buộc: " + err.Error()}}
		}
		findings = append(findings, fs...)
		var bad *spec.Finding
		if f.Flow() == model.FlowLite {
			if bad, err = s.liteImpactLevel(f, *cs); err != nil {
				return 0, err
			}
		}
		if bad != nil {
			findings = append(findings, *bad) // không phân loại theo impact sai
		} else {
			reqs, err := s.riskRequirements(f, *cs)
			if err != nil {
				return 0, err
			}
			findings = append(findings, reqs...)
		}
	}
	if len(findings) == 0 && f.Flow() == model.FlowLite {
		x, err := s.migrationCheck(f, "CASES_MIGRATION_INVALID")
		if err != nil {
			return 0, err
		}
		if x != nil {
			findings = append(findings, *x)
		}
	}
	if len(findings) == 0 {
		// "phải fail trước khi code" chỉ áp dụng khi chưa bước nào bắt đầu; nộp lại sau reopen thì code đã có
		recs, err := f.Steps().All()
		if err != nil {
			return 0, err
		}
		beforeCode := len(recs) == 0
		if beforeCode {
			s.ctx.Println("Đang chạy bộ case qua harness (mong đợi case fail vì chưa có code)...")
		} else {
			s.ctx.Println("Đang chạy bộ case qua harness trên code hiện tại...")
		}
		// Chưa có code: chạy trên schema đã thiết kế (migration chính + nháp), case được dùng bảng mới
		var res run.CaseRunResult
		if err := s.ctx.interruptible(func() (err error) {
			res, err = s.ctx.Runner(f.Root, f.Config).RunCases(f.Key, nil, "cases-with-drafts")
			return err
		}); err != nil {
			return 0, err // bị hủy: không nộp cổng
		}
		var errs, passed []string
		anyFailed := false
		for _, c := range res.Cases {
			switch c.Outcome {
			case run.CaseOutcomeError:
				errs = append(errs, c.ID+" ("+c.Message+")")
			case run.CaseOutcomePassed:
				passed = append(passed, c.ID)
			case run.CaseOutcomeFailed:
				anyFailed = true
			}
		}
		if len(errs) > 0 {
			findings = append(findings, spec.Finding{Code: "CASES_HARNESS_ERROR",
				Message: "Harness không chạy được case: " + strings.Join(errs, ", ") + "."})
		}
		if !anyFailed && beforeCode {
			findings = append(findings, spec.Finding{Code: "CASES_NONE_FAILING",
				Message: "Mọi case đã pass trước khi code: case không kiểm được thay đổi."})
		}
		if len(passed) > 0 {
			if beforeCode {
				info = append(info, "Thông tin: case đã pass trước khi code (không phải lỗi): "+strings.Join(passed, ", "))
			} else {
				info = append(info, "Thông tin: case đã pass trên code hiện tại: "+strings.Join(passed, ", "))
			}
		}
		if len(findings) == 0 && f.Flow() == model.FlowFull {
			x, err := s.laterTableSetup(f, s.effectiveSteps(f, *cs))
			if err != nil {
				return 0, err
			}
			if x != nil {
				findings = append(findings, *x)
			}
		}
	}
	return s.submit(f, gate.CASES, findings, info)
}

func (s *submission) effectiveSteps(f *Feature, cs spec.CasesSpec) []spec.StepSpec {
	var d *spec.DesignSpec
	if ds, ok := f.Design(); ok {
		d = &ds
	}
	return spec.EffectiveSteps(f.Flow(), d, cs)
}

// laterTableSetup: lúc `step finish` của bước đầu, migration của bước sau chưa có trong thư mục migration. Nếu bước đầu
// không được sửa thư mục migration thì chạy case của nó trên schema hiện tại: lỗi ở reset_sql/given.sql nghĩa là case
// (hoặc reset_sql) dùng bảng chỉ có sau bước đó, và sẽ ERROR mãi ở bước này.
func (s *submission) laterTableSetup(f *Feature, steps []spec.StepSpec) (*spec.Finding, error) {
	if len(steps) == 0 {
		return nil, nil
	}
	first := steps[0]
	globs := append(append([]string(nil), first.Scope...), f.Config.AlwaysAllowed...)
	entries, err := os.ReadDir(f.Paths.DbDrafts())
	if err != nil {
		return nil, nil
	}
	var drafts []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			drafts = append(drafts, e.Name())
		}
	}
	dir := strings.TrimSuffix(f.Config.Migrations.Dir, "/")
	if dir != "" {
		dir += "/"
	}
	if len(drafts) == 0 || slices.ContainsFunc(drafts, func(n string) bool { return step.ScopeMatches(globs, dir+n) }) {
		return nil, nil
	}
	s.ctx.Println("Đang chạy case của " + first.ID + " trên schema hiện tại (chưa có migration nháp)...")
	var res run.CaseRunResult
	if err := s.ctx.interruptible(func() (err error) {
		res, err = s.ctx.Runner(f.Root, f.Config).RunCases(f.Key, []string{first.ID}, "cases")
		return err
	}); err != nil {
		return nil, err
	}
	var bad []string
	for _, c := range res.Cases {
		if c.Outcome == run.CaseOutcomeError &&
			(strings.HasPrefix(c.Message, "reset_sql:") || strings.HasPrefix(c.Message, "given.sql:")) {
			bad = append(bad, c.ID)
		}
	}
	if len(bad) == 0 {
		return nil, nil
	}
	return &spec.Finding{Code: "CASES_SETUP_NEEDS_LATER_TABLE", Message: "Case " + strings.Join(bad, ", ") +
		" của bước " + first.ID + " lỗi khi chuẩn bị dữ liệu (reset_sql/given.sql) trên schema chưa có" +
		" migration của bước sau: lúc `aisdlc step finish " + first.ID + "` sẽ luôn ERROR. Bỏ bảng mới" +
		" khỏi reset_sql và khỏi case của " + first.ID + "; dọn bảng đó trong given.sql của case thuộc" +
		" bước tạo bảng."}, nil
}

func (s *submission) review(f *Feature) (int, error) {
	if err := s.precheck(f, gate.REVIEW); err != nil {
		return 0, err
	}
	var findings []spec.Finding
	x, open, err := stepsNotAccepted(f)
	if err != nil {
		return 0, err
	}
	if open {
		findings = append(findings, x)
	}
	report := filepath.Join(f.Paths.EvalDir(), "report.json")
	if !exists(report) {
		findings = append(findings, spec.Finding{Code: "REVIEW_NO_EVAL",
			Message: "Chưa có " + f.Paths.Rel(report) + "; chạy `aisdlc eval` trước."})
	} else if r, err := eval.ReadEvalReport(report); err != nil {
		findings = append(findings, spec.Finding{Code: "REVIEW_NO_EVAL",
			Message: "Không đọc được " + f.Paths.Rel(report) + "; chạy lại `aisdlc eval`: " + err.Error()})
	} else {
		if !r.Passed {
			var failed []string
			for _, h := range r.Hard {
				if !h.Passed {
					failed = append(failed, h.ID)
				}
			}
			findings = append(findings, spec.Finding{Code: "REVIEW_EVAL_FAILED",
				Message: "Eval chưa đạt (" + strings.Join(failed, ", ") + "). Sửa rồi chạy lại `aisdlc eval`."})
		}
		// như catch RuntimeException của Java: không kiểm được eval cũ (vd. git lỗi) là phát hiện, không phải lỗi lệnh
		if stale, err := staleEval(f, r); err != nil {
			findings = append(findings, spec.Finding{Code: "REVIEW_NO_EVAL",
				Message: "Không đọc được " + f.Paths.Rel(report) + "; chạy lại `aisdlc eval`: " + err.Error()})
		} else if stale != nil {
			findings = append(findings, *stale)
		}
	}
	if !exists(f.Paths.ReviewAgent()) {
		findings = append(findings, spec.Finding{Code: "REVIEW_AGENT_MISSING", Message: "Chưa có " +
			f.Paths.Rel(f.Paths.ReviewAgent()) + " (kết quả subagent aisdlc-reviewer)."})
	} else if _, err := eval.ReadAgentReview(f.Paths.ReviewAgent()); err != nil {
		findings = append(findings, spec.Finding{Code: "REVIEW_AGENT_INVALID", Message: err.Error()})
	} else if err := eval.RefreshAgentReview(f.Paths); err != nil {
		// review chạy sau eval: đưa kết quả review vào eval/report.* trước khi tính hash để nộp
		if !slices.ContainsFunc(findings, func(x spec.Finding) bool { return x.Code == "REVIEW_NO_EVAL" }) {
			findings = append(findings, spec.Finding{Code: "REVIEW_NO_EVAL", Message: "Không cập nhật được kết quả review vào " +
				f.Paths.Rel(report) + "; chạy lại `aisdlc eval`: " + err.Error()})
		}
	}
	return s.submit(f, gate.REVIEW, findings, nil)
}

// policy là chính sách hiện tại; web chưa có cache trên máy này thì thử lấy một lần trước (lỗi → Abort như
// LoadPolicy).
func (s *submission) policy(f *Feature) (LoadedPolicy, error) {
	if f.Web.PolicyWebMode() {
		if _, ok := f.Web.Policy(); !ok {
			f.Web.PolicyPullQuietly()
		}
	}
	return LoadPolicy(s.ctx, f.Root, false)
}

// reportRisk (chế độ web): đặt event RISK_CLASSIFIED của risk.yaml vừa ghi vào outbox, một lần; flush gửi ngay (lệnh
// dừng giữa chừng sau khi phân loại, vd. harness lỗi: lần sau risk.yaml không đổi nên không còn dịp gửi).
func (s *submission) reportRisk(f *Feature, flush bool) {
	if f.Mode != gate.ModeWeb || s.reclassified == nil || s.riskReported {
		return
	}
	s.riskReported = true
	riskClassified(s.ctx, f, *s.reclassified)
	if flush {
		s.ctx.Publisher(f.Root).Flush(s.ctx.std)
	}
}

// riskLevel là mức trong risk.yaml gửi kèm bản nộp ở chế độ web (server tự duyệt theo mức này); với BRD server lấy
// mức chặt hơn của mức tạm trong thẻ và mức này, Review không bao giờ tự duyệt. Không có/hỏng risk.yaml thì "".
func riskLevel(f *Feature, g gate.Gate) string {
	if g == gate.REVIEW {
		return ""
	}
	r, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil || !ok {
		return ""
	}
	return r.Level
}

// brdRiskPreview: `submit brd` in mức tạm chỉ bằng rule keyword; không ghi file, lỗi chính sách chỉ cảnh báo.
func (s *submission) brdRiskPreview(f *Feature) {
	r, err := guarded(func() (risk.RiskResult, error) {
		l, err := s.policy(f)
		if err != nil {
			return risk.RiskResult{}, err
		}
		return brdPreview(l.Policy, f)
	})
	var a *Abort
	switch {
	case err == nil:
		line := "Rủi ro tạm (từ khóa): " + r.Level
		if len(r.Matched) > 0 {
			line += " — " + matchedText(r.Matched)
		}
		s.ctx.Println(line)
	case asAbort(err, &a):
		s.ctx.Eprintln("Cảnh báo (rủi ro tạm): " + a.Msg)
	default:
		s.ctx.Eprintln("Cảnh báo: không tính được rủi ro tạm: " + err.Error())
	}
}

// classify tính lại và ghi risk.yaml khi khác bản đang có. Đã có mức đổi tay thì giữ nguyên; nếu chính sách hoặc dữ
// kiện đã đổi kể từ lần ghi thì chỉ cảnh báo. Trả bản đang hiệu lực.
func (s *submission) classify(f *Feature, p risk.RiskPolicy, print bool) (risk.RiskFile, error) {
	fresh, err := classifyRisk(p, f)
	if err != nil {
		return risk.RiskFile{}, err
	}
	old, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil {
		ok = false // file hỏng (chỉ CLI ghi): tính lại
	}
	if ok && old.Override != nil {
		if old.Policy != fresh.Policy || !sameYAML(old.Matched, fresh.Matched) || !sameYAML(old.Agent, fresh.Agent) ||
			fresh.Level != old.Override.From {
			s.ctx.Eprintln("Cảnh báo: chính sách hoặc thiết kế đã đổi kể từ lần ghi " + f.Paths.Rel(f.Paths.Risk()) +
				" (tính lại sẽ ra " + fresh.Level + "); giữ mức đổi tay " + old.Level + " của " + old.Override.By +
				". Dev chạy `aisdlc risk reclassify` để tính lại.")
		}
		return old, nil
	}
	if !ok || !sameYAML(old, fresh) {
		if s.reclassified == nil {
			s.levelBefore = ""
			if ok {
				s.levelBefore = old.Level
			}
		}
		s.reclassified, s.reclassifiedBy = &fresh, &p
		if err := fresh.Write(f.Paths.Risk()); err != nil {
			return risk.RiskFile{}, err
		}
		// web: event gửi sau bản nộp (server thay thế bản tự duyệt khi nhận event; cổng trước phải còn hiệu lực
		// lúc nộp, như chế độ local) — xem submit
		if f.Mode != gate.ModeWeb {
			riskClassified(s.ctx, f, fresh)
		}
		print = true
	}
	if print {
		s.ctx.Println(fmt.Sprintf("Rủi ro: %s (chính sách v%d) — %s", fresh.Level, fresh.Policy.Version,
			matchedText(fresh.Matched)))
	}
	return fresh, nil
}

// sameYAML: a và b ghi ra cùng YAML (equals của record Java; nil và rỗng như nhau như khi đọc lại file).
func sameYAML(a, b any) bool {
	x, err1 := yamlx.Dump(a)
	y, err2 := yamlx.Dump(b)
	if err1 != nil || err2 != nil {
		return reflect.DeepEqual(a, b)
	}
	return string(x) == string(y)
}

// riskRequirements: cổng Case FULL bỏ qua chỉ khi là tính năng cũ (không có risk.yaml và design.yaml không có
// impact); thiếu risk.yaml mà design có impact thì phân loại và ghi lại như `submit design`. LITE luôn phân loại.
// Mỗi yêu cầu thiếu thành một finding RISK_REQ.
func (s *submission) riskRequirements(f *Feature, cs spec.CasesSpec) ([]spec.Finding, error) {
	full := f.Flow() == model.FlowFull
	if full && !exists(f.Paths.Risk()) {
		if req, _ := risk.RequiredByDesign(f.Paths.DesignYaml()); !req {
			return nil, nil
		}
	}
	l, err := s.policy(f)
	if err != nil {
		return nil, err
	}
	rf, err := s.classify(f, l.Policy, false)
	if err != nil {
		return nil, err
	}
	var brd *review.BrdDoc
	var doc *review.DesignDoc
	var d *spec.DesignSpec
	if full {
		if exists(f.Paths.Brd()) {
			b, err := os.ReadFile(f.Paths.Brd())
			if err != nil {
				return nil, err
			}
			x := review.ParseBRD(string(b))
			brd = &x
		}
		if exists(f.Paths.DesignMd()) {
			b, err := os.ReadFile(f.Paths.DesignMd())
			if err != nil {
				return nil, err
			}
			x := review.ParseDesign(string(b))
			doc = &x
		}
		if ds, ok := f.Design(); ok {
			d = &ds
		}
	}
	return risk.RequirementFindings(rf, risk.CheckRequirements(rf, cs, d, brd, doc)), nil
}

// liteImpactLevel: luồng lite, impact.level trong cases.yaml phải thuộc thang của chính sách (như design.yaml ở FULL).
func (s *submission) liteImpactLevel(f *Feature, cs spec.CasesSpec) (*spec.Finding, error) {
	if cs.Impact == nil {
		return nil, nil
	}
	l, err := s.policy(f)
	if err != nil {
		return nil, err
	}
	levels := l.Policy.Levels
	if slices.Contains(levels, cs.Impact.Level) {
		return nil, nil
	}
	return &spec.Finding{Code: "IMPACT_LEVEL", Message: "impact.level \"" + cs.Impact.Level +
		"\" không thuộc các mức hợp lệ [" + strings.Join(levels, ", ") + "]."}, nil
}

// stepsNotAccepted: REVIEW_STEPS_NOT_ACCEPTED (open=true) nếu có bước (theo thiết kế/cases) chưa được dev chấp nhận.
func stepsNotAccepted(f *Feature) (x spec.Finding, open bool, err error) {
	recs, err := f.Steps().All()
	if err != nil {
		return spec.Finding{}, false, err
	}
	status := map[string]step.StepStatus{}
	for _, r := range recs {
		status[r.ID] = r.Status
	}
	var notDone []string
	for _, sp := range f.Specs() {
		st, ok := status[sp.ID]
		if !ok {
			st = step.StepNotStarted
		}
		if st != step.StepAccepted {
			notDone = append(notDone, sp.ID+" ("+string(st)+")")
		}
	}
	if len(notDone) == 0 {
		return spec.Finding{}, false, nil
	}
	return spec.Finding{Code: "REVIEW_STEPS_NOT_ACCEPTED", Message: "Bước chưa được dev chấp nhận: " +
		strings.Join(notDone, ", ") + ". Làm xong từng bước và chờ dev `aisdlc step accept` trước khi review."}, true, nil
}

// staleEval: REVIEW_EVAL_STALE nếu working tree (trừ thư mục tính năng) khác tree đã eval; nil khi còn mới.
func staleEval(f *Feature, r eval.EvalReport) (*spec.Finding, error) {
	now, err := f.Git().SnapshotTreeExcluding(f.ctx.Std(), f.Paths.Rel(f.Paths.Dir()))
	if err != nil {
		return nil, err
	}
	if now == r.EvaluatedTree {
		return nil, nil
	}
	return &spec.Finding{Code: "REVIEW_EVAL_STALE", Message: "code đã đổi sau lần eval — chạy lại aisdlc eval."}, nil
}

func (s *submission) precheck(f *Feature, g gate.Gate) error {
	if !slices.Contains(gate.Sequence(f.Flow()), g) {
		return abort(2, "Cổng "+string(g)+" không thuộc luồng "+string(f.Flow())+".")
	}
	// lệnh trước có thể đã ghi risk.yaml mức mới rồi hỏng trước khi thay thế bản tự duyệt; thay thế trước khi kiểm
	// khóa: cổng trước nay chờ dev thì dừng ngay, không chạy harness
	if _, err := reconcile(s.ctx, f); err != nil {
		return err
	}
	st, err := f.Gates.State(g)
	if err != nil {
		return err
	}
	if st == gate.LockedByPrevious {
		return abort(2, "Cổng "+string(g)+" bị khóa: cổng trước chưa được duyệt.")
	}
	return nil
}

// migrationCheck chạy migration nháp; thành công thì chép schema.json thành schema-expected.json.
func (s *submission) migrationCheck(f *Feature, code string) (*spec.Finding, error) {
	s.ctx.Println("Đang chạy migration nháp qua harness (migration-check)...")
	var runner CaseRunner
	var res run.CaseRunResult
	if err := s.ctx.interruptible(func() (err error) {
		runner = s.ctx.Runner(f.Root, f.Config)
		res, err = runner.RunCases(f.Key, nil, "migration-check")
		return err
	}); err != nil {
		return nil, err // bị hủy: không chép schema-expected.json, không nộp cổng
	}
	schema := filepath.Join(runner.OutDir(f.Key), "schema.json")
	if res.ExitCode != 0 || !exists(schema) {
		missing := ""
		if !exists(schema) {
			missing = ", không có schema.json"
		}
		return &spec.Finding{Code: code, Message: fmt.Sprintf("Migration nháp không chạy được (exit %d%s). "+
			"20 dòng cuối output harness:\n%s", res.ExitCode, missing, runner.OutputTail(f.Key, 20))}, nil
	}
	b, err := os.ReadFile(schema)
	if err == nil {
		err = fsx.WriteAtomic(f.Paths.SchemaExpected(), b, 0o644)
	}
	if err != nil {
		return &spec.Finding{Code: code, Message: "Không chép được schema.json: " + err.Error()}, nil
	}
	s.ctx.Println("Đã ghi " + f.Paths.Rel(f.Paths.SchemaExpected()) + ".")
	return nil, nil
}

func (s *submission) submit(f *Feature, g gate.Gate, findings []spec.Finding, info []string) (int, error) {
	web := f.Mode == gate.ModeWeb
	if web {
		// server cần snapshot để biết luồng (thiếu snapshot thì coi là FULL) và thẻ review để tự duyệt
		p := s.ctx.Publisher(f.Root)
		p.SendSnapshot(s.ctx.std, f.Key.String(), func() ([]byte, error) { return s.ctx.featureSnapshot(p, f) })
		err := f.Web.Submit(f, g, findings, riskLevel(f, g))
		// event mức mới đi sau bản nộp (gửi ở AfterCommand); nộp hỏng thì vẫn báo mức (gửi ngay)
		s.reportRisk(f, err != nil)
		if err != nil {
			return 0, err
		}
	} else if _, err := f.Gates.Submit(g, findings); err != nil {
		return 0, err
	}
	for _, x := range info {
		s.ctx.Println(x)
	}
	// mức mới: bản tự duyệt của cổng nay phải duyệt tay bị thay thế (sau khi nộp: cổng trước còn mở lúc nộp)
	if s.reclassified != nil {
		if _, err := supersede(s.ctx, f, s.reclassifiedBy, s.levelBefore, *s.reclassified); err != nil {
			return 0, err
		}
	}
	auto := ""
	if len(findings) == 0 {
		var err error
		if auto, err = afterSubmit(s.ctx, f, g); err != nil {
			return 0, err
		}
	}
	name := strings.ToLower(string(g))
	s.ctx.AfterCommand(f, "submit "+name)
	onWeb := ""
	if web {
		onWeb = " lên web"
	}
	if auto != "" {
		s.ctx.Println("Đã nộp cổng " + string(g) + onWeb + ", không có phát hiện: " + auto + ".")
		return ExitOK, nil
	}
	if len(findings) == 0 {
		if web {
			s.ctx.Println("Đã nộp cổng " + string(g) + " lên web, không có phát hiện. Dev duyệt trên web (hoặc " +
				"`aisdlc approve " + name + "` ở terminal của mình).")
		} else {
			s.ctx.Println("Đã nộp cổng " + string(g) + ", không có phát hiện. Dev duyệt bằng `aisdlc approve " +
				name + "` ở terminal của mình.")
		}
		return ExitOK, nil
	}
	s.ctx.Println(fmt.Sprintf("Cổng %s có %d phát hiện:", g, len(findings)))
	for _, x := range findings {
		s.ctx.Println("  - " + x.Code + ": " + strings.ReplaceAll(x.Message, "\n", "\n      "))
	}
	s.ctx.Println("Sửa rồi chạy lại `aisdlc submit " + name + "`.")
	return ExitFindings, nil
}
