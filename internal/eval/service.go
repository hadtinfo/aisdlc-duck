package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/harness/harnessapi"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

// Verifier build, chạy unit test và bộ case đầy đủ của tính năng, ghi results.json/schema.json vào out
// (binary aisdlc-harness qua CLI trong bản thật). error chỉ khi không ghi được out.
type Verifier interface {
	Verify(ctx context.Context, key model.FeatureKey, out string) (harnessapi.VerifyResult, error)
}

// CIApprovalSource: approval lấy từ web khi chạy CI (webclient.HTTPCIApprovals). nil → không có web.
// Lỗi mạng/401/403 trả error nêu lý do.
type CIApprovalSource interface {
	Approvals(ctx context.Context, key model.FeatureKey, tree string) (CIApprovals, error)
}

// CIApprovals là approval còn hiệu lực của một tính năng, do web trả về cho CI (như CiApprovals.java). Mode: "local"
// hoặc "web"; Pinned: repo đã có bản ghi chế độ trên server (chưa có thì Mode chỉ là mặc định local, không phải
// quyết định của lead). EvaluatedTree: tree đã eval lúc duyệt (dùng cho REVIEW).
type CIApprovals struct {
	Mode          string
	Pinned        bool
	Approvals     map[gate.Gate]gate.Approval
	EvaluatedTree map[gate.Gate]string
}

// Service chạy eval tầng 1 của một tính năng: build, unit test và cả bộ case qua Verifier, tính hard check và soft
// check, rồi ghi eval/report.json, eval/report.md, eval/diff-by-step.md (EvalService.java).
type Service struct {
	Paths    model.FeaturePaths
	Meta     model.FeatureMeta
	Config   model.RepoConfig
	Git      *gitx.Repo
	Verifier Verifier
	Gates    *gate.Service
	Steps    *step.Service
	Specs    []spec.StepSpec
	Adapter  lang.Adapter
	Now      func() time.Time
	// Policy là chính sách rủi ro đang hiệu lực (CLI nạp); dùng để tính lại inputsHash ở CI. nil thì không kiểm được.
	Policy *risk.RiskPolicy
	// PolicyErr là lý do không có Policy (không nạp/lấy được); đi vào chi tiết RISK_LLM.
	PolicyErr string
}

// OutDir là thư mục kết quả của harness cho tính năng: <root>/.aisdlc/run/<KEY>.
func (s *Service) OutDir() string {
	return filepath.Join(s.Paths.RepoRoot, ".aisdlc", "run", s.Meta.Key.String())
}

// Run: local tính phạm vi từ feature.baseTree; CI từ merge-base với baseRef ("" là baseRef của config). Ở CI chế độ
// web (server đã ghim chế độ thì theo server, chưa ghim thì theo config approvals) approval lấy từ web thay cho
// gates.json và cổng chưa duyệt/đã đổi là lỗi cứng. config approvals: web mà không có nguồn web (web nil) thì fail
// closed: GATE:WEB lỗi cứng thay cho CASES_LOCKED, không kiểm gates.json.
// error khi git/gates.json/steps.json lỗi, Verify trả lỗi (không ghi được out), ctx bị hủy (Ctrl-C) hoặc không ghi được
// report.
func (s *Service) Run(ctx context.Context, ci bool, baseRef string, web CIApprovalSource) (EvalReport, error) {
	from := s.Meta.BaseTree
	if ci {
		if baseRef == "" {
			baseRef = s.Config.BaseRef
		}
		f, err := s.Git.MergeBaseTree(ctx, baseRef)
		if err != nil {
			return EvalReport{}, err
		}
		from = f
	}
	// trước khi harness ghi gì vào working tree; bỏ thư mục tính năng để ghi report.json không đổi tree
	to, err := s.Git.SnapshotTreeExcluding(ctx, s.Paths.Rel(s.Paths.Dir()))
	if err != nil {
		return EvalReport{}, err
	}
	// approval đọc trước khi eval ghi lại eval/report.json (artifact của REVIEW)
	var webHard []HardCheck
	webMode := false
	if ci {
		if webMode, webHard, err = s.webApprovals(ctx, web, to); err != nil {
			return EvalReport{}, err
		}
	}
	var approvals []SoftCheck
	if ci && !webMode {
		if approvals, err = s.approvals(); err != nil {
			return EvalReport{}, err
		}
	}
	out := s.OutDir()
	vr, err := s.Verifier.Verify(ctx, s.Meta.Key, out)
	if err != nil {
		return EvalReport{}, err
	}
	// bị hủy (Ctrl-C) giữa lúc harness chạy: không ghi report từ kết quả dở dang
	if err := ctx.Err(); err != nil {
		return EvalReport{}, err
	}
	var selected []spec.CaseSpec
	if cs, err := spec.ReadCases(s.Paths.Cases()); err == nil {
		selected = cs.Cases
	}
	results := harnessapi.ReadResults(out, selected)

	hard := []HardCheck{}
	soft := []SoftCheck{}
	cases := s.casesAllPass(results)
	hard = append(hard, cases)
	locked, err := s.casesLocked()
	if err != nil {
		return EvalReport{}, err
	}
	hasCases, err := s.Gates.HasApproval(gate.CASES)
	if err != nil {
		return EvalReport{}, err
	}
	switch {
	case webMode:
		hard = append(hard, webHard...) // CASES_LOCKED do web thay thế (GATE:CASES)
	case ci && hasCases:
		// CI: chỉ lệch hash so với bản đã duyệt mới hạ xuống cảnh báo; chưa duyệt vẫn là lỗi cứng
		level := SoftWarn
		if locked.Passed {
			level = SoftOK
		}
		soft = append(soft, SoftCheck{locked.ID, level, locked.Detail})
	default:
		hard = append(hard, locked)
	}
	soft = append(soft, approvals...)
	if ci && !webMode {
		var gerr error
		hard = append(hard, AutoApprovals(gate.Sequence(s.Gates.Flow()), func(g gate.Gate) (gate.Approval, bool) {
			a, ok, err := s.Gates.Approval(g)
			if err != nil && gerr == nil {
				gerr = err
			}
			return a, ok && err == nil
		}, s.Paths.Risk())...)
		if gerr != nil {
			return EvalReport{}, gerr
		}
	}
	hard = append(hard, buildCheck(vr, s.Paths.Rel(filepath.Join(out, "harness.log"))), s.schemaMatch(out))
	sc, err := s.scope(ctx, from, to)
	if err != nil {
		return EvalReport{}, err
	}
	hard = append(hard, sc)
	for _, c := range s.Config.Checks {
		hard = append(hard, s.check(ctx, c, out))
	}

	budgets, err := s.budgets()
	if err != nil {
		return EvalReport{}, err
	}
	soft = append(soft, budgets...)
	var rf *risk.RiskFile
	if _, statErr := os.Stat(s.Paths.Risk()); statErr == nil {
		f, ok, err := risk.ReadRiskFile(s.Paths.Risk())
		if err != nil {
			hard = append(hard, HardCheck{"RISK_FILE", false, "Không đọc được risk.yaml: " + err.Error()})
		} else if ok {
			rf = &f
		}
	} else if s.Meta.Flow == model.FlowFull {
		// có dấu riskSchema hoặc design.yaml có impact: không phải tính năng cũ, thiếu risk.yaml không được miễn yêu cầu
		if risk.RequiredFor(s.Meta, s.Paths.DesignYaml()) {
			hard = append(hard, HardCheck{"RISK_FILE", false, "thiếu risk.yaml; chạy aisdlc submit cases"})
		}
	}
	if ci && rf != nil && s.Meta.RiskSchema >= 2 {
		hard = append(hard, s.riskLLM(*rf))
	}
	var covReq *risk.Requirement
	if rf != nil {
		for i := range rf.Requirements {
			if rf.Requirements[i].ID == risk.ReqCoverage {
				covReq = &rf.Requirements[i]
				break
			}
		}
	}
	var riskCov *HardCheck
	if covReq != nil {
		c, err := s.riskCoverage(ctx, from, to, vr.Coverage, covReq.IntParam("min", 0), rf.Level)
		if err != nil {
			return EvalReport{}, err
		}
		riskCov = &c
		hard = append(hard, c) // thay cho chỉ số mềm cùng tên để không báo trùng
	} else {
		c, err := s.coverage(ctx, from, to, vr.Coverage, s.Config.CoverageWarn, false)
		if err != nil {
			return EvalReport{}, err
		}
		soft = append(soft, c)
	}
	soft = append(soft, AgentReviewCheck(s.Paths.ReviewAgent()))

	var section *RiskSection
	if rf != nil {
		section = s.riskSection(*rf, cases, riskCov)
	}
	mode := "local"
	if ci {
		mode = "ci"
	}
	// bị hủy (Ctrl-C) giữa lúc chạy checks/coverage: không ghi report từ kết quả dở dang
	if err := ctx.Err(); err != nil {
		return EvalReport{}, err
	}
	report := NewEvalReport(s.Meta.Key.String(), s.Now().UTC(), mode, hard, soft, to, section)
	if err := s.write(report, results); err != nil {
		return EvalReport{}, err
	}
	return report, nil
}

// riskLLM (CI, tính năng có dấu riskSchema): kết quả xếp loại LLM trong risk.yaml phải khớp chính sách và tài liệu
// hiện tại. Không gọi LLM; tài liệu đã khóa theo hash cổng nên so trên file hiện tại không gây lỗi giả.
func (s *Service) riskLLM(rf risk.RiskFile) HardCheck {
	const id = "RISK_LLM"
	if rf.LLM == nil {
		return HardCheck{id, false, "thiếu kết quả xếp loại LLM"}
	}
	if s.Policy == nil {
		if s.PolicyErr != "" {
			return HardCheck{id, false, s.PolicyErr}
		}
		return HardCheck{id, false, "không có chính sách rủi ro để kiểm inputsHash"}
	}
	files, err := risk.InputFiles(s.Paths.RepoRoot, s.Paths, s.Meta.Flow)
	if err != nil {
		return HardCheck{id, false, "Không đọc được tài liệu đầu vào: " + err.Error()}
	}
	if rf.LLM.InputsHash != risk.InputsHash(*s.Policy, files) {
		return HardCheck{id, false, "inputsHash không khớp tài liệu hiện tại"}
	}
	return HardCheck{id, true, "kết quả xếp loại LLM khớp tài liệu và chính sách hiện tại"}
}

// webApprovals: web trả lời sai/không trả lời không bao giờ được bỏ qua ở chế độ web. Không có nguồn web mà config
// nói web thì fail closed.
func (s *Service) webApprovals(ctx context.Context, web CIApprovalSource, evaluatedTree string) (bool, []HardCheck, error) {
	configWeb := s.Config.Approvals == "web"
	if web == nil {
		if !configWeb {
			return false, nil, nil
		}
		return true, []HardCheck{{"GATE:WEB", false,
			"Thiếu biến AISDLC_WEB_URL hoặc AISDLC_CI_TOKEN để hỏi web (chế độ web)"}}, nil
	}
	got, err := askWeb(ctx, web, s.Meta.Key, evaluatedTree)
	if err != nil {
		// có nguồn thì không có chế độ bỏ qua: config.yaml của đầu MR không được tắt kiểm tra
		return true, []HardCheck{{"GATE:WEB", false, "Không hỏi được web (" + err.Error() + "), chạy lại job"}}, nil
	}
	// server đã ghim chế độ (lead quyết) → theo server; chưa ghim → theo config.yaml (web thì fail closed)
	webMode := configWeb
	if got.Pinned {
		webMode = got.Mode == "web"
	}
	if !webMode {
		return false, nil, nil
	}
	seq := gate.Sequence(s.Gates.Flow())
	out := []HardCheck{}
	for _, g := range seq {
		id := "GATE:" + string(g)
		a, ok := got.Approvals[g]
		if !ok {
			out = append(out, HardCheck{id, false, "chưa duyệt " + string(g) + " trên web"})
			continue
		}
		h, err := s.Gates.CurrentHash(g)
		if err != nil {
			return false, nil, err
		}
		switch {
		case a.Hash != h:
			out = append(out, HardCheck{id, false, string(g) + " đã đổi sau khi duyệt"})
		case g == gate.REVIEW && evaluatedTree != got.EvaluatedTree[g]:
			out = append(out, HardCheck{id, false, "REVIEW đã đổi sau duyệt, chạy lại aisdlc eval rồi duyệt lại"})
		default:
			out = append(out, HardCheck{id, true, "đã duyệt trên web, khớp nội dung"})
		}
	}
	// bản tự duyệt của server (channel auto) cũng phải được flow chụp trong risk.yaml cho phép
	out = append(out, AutoApprovals(seq, func(g gate.Gate) (gate.Approval, bool) {
		a, ok := got.Approvals[g]
		return a, ok
	}, s.Paths.Risk())...)
	return true, out, nil
}

// approvals: CI, cảnh báo cổng (trừ CASES, đã có CASES_LOCKED) chưa duyệt hoặc đã đổi sau khi duyệt.
func (s *Service) approvals() ([]SoftCheck, error) {
	out := []SoftCheck{}
	for _, g := range gate.Sequence(s.Gates.Flow()) {
		if g == gate.CASES {
			continue
		}
		id := "GATE:" + string(g)
		ok, err := s.Gates.HasApproval(g)
		if err != nil {
			return nil, err
		}
		if !ok {
			out = append(out, SoftCheck{id, SoftWarn, "chưa duyệt " + string(g)})
			continue
		}
		match, err := s.Gates.ApprovalMatches(g)
		if err != nil {
			return nil, err
		}
		if !match {
			out = append(out, SoftCheck{id, SoftWarn, string(g) + " đã đổi sau khi duyệt"})
		}
	}
	return out, nil
}

// AutoApprovals: CI, mỗi bản tự duyệt còn hiệu lực (gates.json ở chế độ local, approval channel = auto của web ở
// chế độ web) phải được flow chụp trong risk.yaml cho phép (BRD cũng xét theo mức cuối trong risk.yaml). Không có
// bản tự duyệt thì không có check nào.
func AutoApprovals(seq []gate.Gate, approval func(gate.Gate) (gate.Approval, bool), riskPath string) []HardCheck {
	out := []HardCheck{}
	var rf *risk.RiskFile
	read := false
	for _, g := range seq {
		a, ok := approval(g)
		if !ok || a.Superseded || !a.IsAuto() {
			continue
		}
		if !read {
			read = true
			if f, ok, err := risk.ReadRiskFile(riskPath); err == nil && ok {
				rf = &f
			}
		}
		id := "GATE_AUTO:" + string(g)
		if risk.AutoFromRiskFile(rf, g) {
			out = append(out, HardCheck{id, true, "tự duyệt (mức " + rf.Level + "), khớp flow trong risk.yaml"})
		} else {
			out = append(out, HardCheck{id, false, "Bản tự duyệt " + string(g) + " không khớp flow trong risk.yaml"})
		}
	}
	return out
}

// write ghi eval/report.json, report.md và diff-by-step.md.
func (s *Service) write(report EvalReport, results []run.CaseResult) error {
	dir := s.Paths.EvalDir()
	if err := report.Write(filepath.Join(dir, "report.json")); err != nil {
		return err
	}
	notPassed := []run.CaseResult{}
	for _, c := range results {
		if c.Outcome != run.CaseOutcomePassed {
			notPassed = append(notPassed, c)
		}
	}
	titles := map[string]string{}
	for _, sp := range s.Specs {
		if _, ok := titles[sp.ID]; !ok {
			titles[sp.ID] = sp.Title
		}
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, "report.md"), []byte(RenderReport(report, notPassed)), 0o644); err != nil {
		return errors.New("Không ghi được " + dir + ": " + err.Error())
	}
	recs, err := s.Steps.All()
	if err != nil {
		return err
	}
	diff, err := DiffByStep(recs, titles, s.Git, true)
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, "diff-by-step.md"), []byte(diff), 0o644); err != nil {
		return errors.New("Không ghi được " + dir + ": " + err.Error())
	}
	return nil
}

// askWeb gọi nguồn web; panic của nguồn là lỗi (fail closed, như RuntimeException của Java), không làm sập eval.
func askWeb(ctx context.Context, web CIApprovalSource, key model.FeatureKey, tree string) (got CIApprovals, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("lỗi nội bộ: %v", r)
		}
	}()
	return web.Approvals(ctx, key, tree)
}
