package eval

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/harness"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

const (
	maxListed  = 10
	coverageID = "COVERAGE_CHANGED_LINES"
	// checkWaitDelay: sau khi kill nhóm tiến trình của lệnh check, chờ pipe output đóng tối đa chừng này.
	checkWaitDelay = 5 * time.Second
)

var unsafeLogName = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// list: tối đa 10 mục, còn lại ghi "… và N mục khác".
func list(items []string) string {
	if len(items) <= maxListed {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:maxListed], ", ") + " … và " + strconv.Itoa(len(items)-maxListed) + " mục khác"
}

func (s *Service) casesAllPass(results []run.CaseResult) HardCheck {
	const id = "CASES_ALL_PASS"
	cs, err := spec.ReadCases(s.Paths.Cases())
	if err != nil {
		return HardCheck{id, false, "Không đọc được cases.yaml: " + err.Error()}
	}
	if len(cs.Cases) == 0 {
		return HardCheck{id, false, "cases.yaml không có case nào"}
	}
	byID := map[string]run.CaseOutcome{}
	for _, r := range results {
		// trùng id: giữ kết quả xấu (PASSED chỉ khi mọi dòng PASSED)
		if prev, ok := byID[r.ID]; !ok || prev == run.CaseOutcomePassed {
			byID[r.ID] = r.Outcome
		}
	}
	passed := 0
	var notPassed []string
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			notPassed = append(notPassed, id)
		}
	}
	for _, c := range cs.Cases {
		if byID[c.ID] == run.CaseOutcomePassed {
			passed++
		} else {
			add(c.ID)
		}
	}
	for _, r := range results {
		if r.Outcome != run.CaseOutcomePassed {
			add(r.ID)
		}
	}
	detail := strconv.Itoa(passed) + "/" + strconv.Itoa(len(cs.Cases)) + " case pass"
	if len(notPassed) == 0 {
		return HardCheck{id, true, detail}
	}
	return HardCheck{id, false, detail + "; không pass: " + list(notPassed)}
}

func (s *Service) casesLocked() (HardCheck, error) {
	const id = "CASES_LOCKED"
	ok, err := s.Gates.HasApproval(gate.CASES)
	if err != nil {
		return HardCheck{}, err
	}
	if !ok {
		return HardCheck{id, false, "bộ case chưa được duyệt"}, nil
	}
	match, err := s.Gates.ApprovalMatches(gate.CASES)
	if err != nil {
		return HardCheck{}, err
	}
	if match {
		return HardCheck{id, true, "Bộ case khớp hash lúc duyệt"}, nil
	}
	return HardCheck{id, false, "Bộ case đã bị sửa sau khi duyệt"}, nil
}

// buildCheck: build, unit test và harness. Case fail không làm hỏng check này (CASES_ALL_PASS báo); lỗi harness
// (hạ tầng, dịch vụ) thì có, kể cả khi mọi case pass. Lỗi kèm "; xem <harness.log>" như Java (maven.log).
func buildCheck(vr harness.VerifyResult, log string) HardCheck {
	const id = "BUILD_AND_TESTS"
	var problems []string
	if vr.TimedOut {
		problems = append(problems, "quá thời gian")
	}
	if !vr.Build.OK {
		problems = append(problems, "go build lỗi") // build hỏng thì không chạy unit test
	} else if len(vr.Unit.Failed) > 0 {
		problems = append(problems, "unit test fail: "+list(vr.Unit.Failed))
	} else if vr.Unit.Exit != 0 {
		problems = append(problems, "go test thoát mã "+strconv.Itoa(vr.Unit.Exit))
	}
	if vr.HarnessErr != "" {
		problems = append(problems, "harness lỗi: "+vr.HarnessErr)
	}
	if len(problems) == 0 {
		return HardCheck{id, true, "build và unit test đạt"}
	}
	return HardCheck{id, false, strings.Join(problems, "; ") + "; xem " + log}
}

func (s *Service) schemaMatch(out string) HardCheck {
	const id = "SCHEMA_MATCH"
	actual := filepath.Join(out, "schema.json")
	if _, err := os.Stat(actual); err != nil {
		return HardCheck{id, false, "Harness không ghi schema.json"}
	}
	if _, err := os.Stat(s.Paths.SchemaExpected()); err != nil {
		return HardCheck{id, false, "Thiếu " + s.Paths.Rel(s.Paths.SchemaExpected())}
	}
	a, err := run.ReadSchemaSnapshot(actual)
	if err != nil {
		return HardCheck{id, false, "Không đọc được schema: " + err.Error()}
	}
	e, err := run.ReadSchemaSnapshot(s.Paths.SchemaExpected())
	if err != nil {
		return HardCheck{id, false, "Không đọc được schema: " + err.Error()}
	}
	if diff := a.Diff(e); len(diff) > 0 {
		return HardCheck{id, false, "Schema lệch thiết kế: " + list(diff)}
	}
	return HardCheck{id, true, "Schema khớp thiết kế"}
}

func (s *Service) scope(ctx context.Context, from, to string) (HardCheck, error) {
	const id = "SCOPE"
	var allowed []string
	seenGlob := map[string]bool{}
	addGlobs := func(gs []string) {
		for _, g := range gs {
			if !seenGlob[g] {
				seenGlob[g] = true
				allowed = append(allowed, g)
			}
		}
	}
	for _, sp := range s.Specs {
		gs, err := s.Steps.AllowedGlobs(sp.ID)
		if err != nil {
			return HardCheck{}, err
		}
		addGlobs(gs)
	}
	if len(s.Specs) == 0 {
		addGlobs(s.Config.AlwaysAllowed)
	}
	inScope := step.CompileScope(allowed)
	configPath := s.Paths.Rel(filepath.Join(s.Paths.RepoRoot, "aisdlc", "config.yaml"))
	changes, err := s.Git.Diff(ctx, from, to)
	if err != nil {
		return HardCheck{}, err
	}
	configChanged := false
	var outside []string
	seen := map[string]bool{}
	for _, c := range changes {
		ps := []string{c.Path}
		if c.OldPath != "" {
			ps = []string{c.OldPath, c.Path}
		}
		for _, p := range ps {
			switch {
			case p == configPath:
				configChanged = true // cấu hình đang dùng để chấm: không được đổi cùng thay đổi bị chấm
			case !s.ignored(p) && !inScope(p) && !seen[p]:
				seen[p] = true
				outside = append(outside, p)
			}
		}
	}
	var problems []string
	if configChanged {
		problems = append(problems, configPath+ConfigChanged)
	}
	if len(outside) > 0 {
		problems = append(problems, "Ngoài phạm vi: "+list(outside))
	}
	if len(problems) == 0 {
		return HardCheck{id, true, "Mọi file thay đổi đều trong phạm vi"}, nil
	}
	return HardCheck{id, false, strings.Join(problems, "; ")}, nil
}

// ignored: không tính thư mục tính năng (mọi aisdlc/features/*: CI có thể có nhiều tính năng trong một diff) và
// unmanaged.
func (s *Service) ignored(p string) bool {
	features := s.Paths.Rel(filepath.Dir(s.Paths.Dir())) + "/"
	return strings.HasPrefix(p, features) || step.ScopeMatches(s.Config.Unmanaged, p)
}

// check chạy một lệnh checks của config qua sh -c ở gốc repo, log vào out/check-<name>.log, hạn timeoutMinutes.
// Lệnh chạy trong process group riêng và bị kill cả nhóm khi quá hạn hay ctx bị hủy.
func (s *Service) check(ctx context.Context, c model.CheckCommand, out string) HardCheck {
	id := "CHECK:" + c.Name
	logPath := filepath.Join(out, "check-"+unsafeLogName.ReplaceAllString(c.Name, "_")+".log")
	rel := s.Paths.Rel(logPath)
	cctx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.TimeoutMinutes*float64(time.Minute)))
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", c.Cmd)
	cmd.Dir = s.Paths.RepoRoot
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = checkWaitDelay
	runErr := cmd.Run()
	if err := fsx.WriteAtomic(logPath, buf.Bytes(), 0o644); err != nil {
		return HardCheck{id, false, "Không ghi được " + rel + ": " + err.Error()}
	}
	if runErr == nil {
		return HardCheck{id, true, "exit 0"}
	}
	if ctx.Err() != nil { // Ctrl-C/hủy từ ngoài, không phải hết hạn của check
		return HardCheck{id, false, "bị hủy, đã dừng; xem " + rel}
	}
	if cctx.Err() != nil {
		return HardCheck{id, false, "quá thời gian, đã dừng; xem " + rel}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return HardCheck{id, false, "exit " + strconv.Itoa(exitErr.ExitCode()) + "; xem " + rel}
	}
	return HardCheck{id, false, "không chạy được: " + runErr.Error() + "; xem " + rel}
}

func (s *Service) budgets() ([]SoftCheck, error) {
	recs, err := s.Steps.All()
	if err != nil {
		return nil, err
	}
	byID := map[string]step.StepRecord{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	out := []SoftCheck{}
	for _, sp := range s.Specs {
		id := "BUDGET:" + sp.ID
		r, ok := byID[sp.ID]
		if !ok || r.Check == nil {
			out = append(out, SoftCheck{id, SoftNA, "chưa có kết quả step finish"})
			continue
		}
		c := r.Check
		detail := strconv.Itoa(c.Files) + "/" + strconv.Itoa(c.Budget.Files) + " file, " + strconv.Itoa(c.Lines) +
			"/" + strconv.Itoa(c.Budget.Lines) + " dòng"
		level := SoftOK
		if c.OverBudget {
			level = SoftWarn
		}
		out = append(out, SoftCheck{id, level, detail})
	}
	return out, nil
}

// coverage là chỉ số mềm coverage trên dòng mới/sửa. strict (chỉ RISK_COVERAGE): file nguồn đổi ở package không
// được đo tính là chưa phủ.
func (s *Service) coverage(ctx context.Context, from, to string, cov lang.Coverage, warn float64, strict bool) (SoftCheck, error) {
	if cov.NA != "" {
		return SoftCheck{coverageID, SoftNA, cov.NA}, nil
	}
	changed, err := s.Git.ChangedLines(ctx, from, to)
	if err != nil {
		return SoftCheck{}, err
	}
	c := ComputeChangedCoverage(changed, cov, s.Adapter.IsSource, strict)
	if c.NA != "" {
		return SoftCheck{coverageID, SoftNA, c.NA}, nil
	}
	detail := strconv.Itoa(int(math.Round(100*float64(c.Covered)/float64(c.Measurable)))) + "% (" +
		strconv.Itoa(c.Covered) + "/" + strconv.Itoa(c.Measurable) + " dòng mới/sửa)"
	if c.UnmeasuredCount > 0 {
		named := c.Unmeasured
		if c.UnmeasuredCount > maxNamedUnmeasured {
			named = named[:maxNamedUnmeasured]
		}
		detail += "; chưa đo: " + strings.Join(named, ", ")
		if c.UnmeasuredCount > maxNamedUnmeasured {
			detail += " (+" + strconv.Itoa(c.UnmeasuredCount-maxNamedUnmeasured) + ")"
		}
	}
	level := SoftOK
	if float64(c.Covered)/float64(c.Measurable) < warn {
		level = SoftWarn
	}
	return SoftCheck{coverageID, level, detail}, nil
}

// riskCoverage là coverage cứng: cùng base với chỉ số mềm, ngưỡng min/100; khác ở chỗ file nguồn đổi mà không
// được đo tính là chưa phủ (không lặng lẽ bỏ qua code chưa đo).
func (s *Service) riskCoverage(ctx context.Context, from, to string, cov lang.Coverage, min int, level string) (HardCheck, error) {
	const id = "RISK_COVERAGE"
	minS := strconv.Itoa(min)
	if cov.NA != "" {
		return HardCheck{id, false, "không có báo cáo coverage (" + cov.NA + "); mức " + level +
			" yêu cầu coverage ≥ " + minS + "%"}, nil
	}
	c, err := s.coverage(ctx, from, to, cov, float64(min)/100, true)
	if err != nil {
		return HardCheck{}, err
	}
	need := "yêu cầu tối thiểu " + minS + "%"
	switch c.Level {
	case SoftOK:
		return HardCheck{id, true, c.Detail + "; " + need}, nil
	case SoftNA:
		return HardCheck{id, true, "không có dòng mới đo được; " + need}, nil
	}
	d := c.Detail
	if at := strings.Index(d, "; chưa đo: "); at >= 0 {
		return HardCheck{id, false, d[:at] + " < " + minS + "%" + d[at:]}, nil
	}
	return HardCheck{id, false, d + " < " + minS + "%"}, nil
}

// riskSection: yêu cầu ở cổng Case dùng lại bộ kiểm cổng; coverage và sql-zero lấy từ kết quả eval.
func (s *Service) riskSection(rf risk.RiskFile, casesCheck HardCheck, riskCov *HardCheck) *RiskSection {
	var checked []risk.RequirementStatus
	if cs, err := spec.ReadCases(s.Paths.Cases()); err == nil {
		var design *spec.DesignSpec
		var brd *review.BrdDoc
		var doc *review.DesignDoc
		if s.Meta.Flow != model.FlowLite {
			if d, err := spec.ReadDesign(s.Paths.DesignYaml()); err == nil {
				design = &d
			}
			if b, err := os.ReadFile(s.Paths.Brd()); err == nil {
				x := review.ParseBRD(string(b))
				brd = &x
			}
			if b, err := os.ReadFile(s.Paths.DesignMd()); err == nil {
				x := review.ParseDesign(string(b))
				doc = &x
			}
		}
		checked = risk.CheckRequirements(rf, cs, design, brd, doc)
	} else {
		checked = risk.CheckRequirements(rf, spec.CasesSpec{}, nil, nil, nil)
	}
	out := []risk.RequirementStatus{}
	for _, st := range checked {
		switch {
		case st.ID == risk.ReqCoverage && riskCov != nil:
			st.Status, st.Detail = risk.RequirementOK, ""
			if !riskCov.Passed {
				st.Status, st.Detail = risk.RequirementMissing, riskCov.Detail
			}
		case st.ID == risk.ReqSQLZero:
			st.Status, st.Detail = risk.RequirementOK, ""
			if !casesCheck.Passed {
				st.Status, st.Detail = risk.RequirementMissing, "có case không pass ("+casesCheck.Detail+")"
			}
		}
		out = append(out, st)
	}
	matched := []string{}
	for _, m := range rf.Matched {
		matched = append(matched, m.Rule)
	}
	var override *string
	if o := rf.Override; o != nil {
		v := o.From + " → " + o.To + " (" + o.By + ")"
		override = &v
	}
	return &RiskSection{Level: rf.Level, PolicyVersion: rf.Policy.Version, Matched: matched, Override: override,
		Requirements: out}
}
