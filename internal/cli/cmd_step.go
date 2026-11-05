package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

// webSuffix: người accept qua lệnh chờ từ web là "<login> (web)" (PendingActions).
const webSuffix = " (web)"

func newStepCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "step",
		Short: "Làm từng bước Sx: start, finish, accept (accept chỉ dev).",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	sub := func(name, short string, body func(ctx *Context, f *Feature, id string) (int, error)) *cobra.Command {
		var fo FeatureOption
		sc := &cobra.Command{Use: name + " <Sx>", Short: short, Args: cobra.ExactArgs(1)}
		fo.AddTo(sc)
		sc.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
			f, err := fo.Feature(ctx)
			if err != nil {
				return 0, err
			}
			return body(ctx, f, args[0])
		})
		return sc
	}
	c.AddCommand(
		sub("start", "Bắt đầu (hoặc làm tiếp) bước Sx.", stepStart),
		sub("finish", "Chạy case của các bước <= Sx, kiểm phạm vi và ngân sách.", stepFinish),
		sub("accept", "Chỉ dev: chấp nhận bước Sx đang chờ duyệt.", stepAccept),
	)
	return c
}

func stepStart(ctx *Context, f *Feature, id string) (int, error) {
	steps := f.Steps()
	if _, err := steps.Start(id); err != nil {
		return 0, err
	}
	s, err := specOf(f, id)
	if err != nil {
		return 0, err
	}
	globs, err := steps.AllowedGlobs(id)
	if err != nil {
		return 0, err
	}
	ctx.Println("Bắt đầu bước " + id + " — " + s.Title)
	ctx.Println("Phạm vi: " + strings.Join(globs, ", "))
	ctx.Println("Ngân sách: " + budgetText(s.Budget))
	ctx.Println("Case của bước:")
	if cs, ok := f.Cases(); ok {
		for _, c := range cs.Cases {
			if c.Step == id {
				ctx.Println("  - " + c.ID + " [" + caseKind(c) + "] " + c.Title)
			}
		}
	}
	ctx.Println("Chỉ sửa file trong phạm vi. Xong thì chạy `aisdlc step finish " + id + "`.")
	return ExitOK, nil
}

func stepFinish(ctx *Context, f *Feature, id string) (int, error) {
	steps := f.Steps()
	upTo, err := stepsUpTo(f, id)
	if err != nil {
		return 0, err
	}
	if _, err := requireStepStatus(steps, id, step.StepInProgress); err != nil {
		return 0, err
	}
	// local: bản tự duyệt mà risk.yaml hiện tại đặt về duyệt tay bị thay thế
	if _, err := reconcile(ctx, f); err != nil {
		return 0, err
	}
	ctx.Println("Đang chạy case của " + strings.Join(upTo, ", ") + " qua harness...")
	res, err := ctx.Runner(f.Root, f.Config).RunCases(f.Key, upTo, "cases")
	if err != nil {
		return 0, err
	}
	rec, err := steps.Finish(id, res)
	if err != nil {
		return 0, err
	}
	ch := *rec.Check
	ctx.Println(stepCheckSummary(ch))
	for _, c := range res.Cases {
		if slices.Contains(ch.CasesNotPassed, c.ID) {
			ctx.Println("  - " + c.ID + " " + string(c.Outcome) + ": " + c.Message)
		}
	}
	if len(ch.OutOfScope) > 0 {
		ctx.Println("File ngoài phạm vi: dev chấp nhận bằng `aisdlc scope allow <path>`, hoặc agent hoàn tác.")
	}
	passed := len(ch.CasesNotPassed) == 0 && len(ch.OutOfScope) == 0
	level := ""
	if passed {
		if level, err = autoAcceptLevel(f, steps, id, ch); err != nil {
			return 0, err
		}
	}
	if level != "" {
		if _, err := steps.Accept(id, gate.ApprovalAuto); err != nil {
			return 0, err
		}
		ctx.Println("Đã chấp nhận bước " + id + ": tự duyệt (mức " + level + ").")
		nextStep(ctx, f, id)
	} else {
		ctx.Println("Dừng lại. Dev xem rồi chạy `aisdlc step accept " + id + "` ở terminal của mình.")
	}
	if passed {
		return ExitOK, nil
	}
	return ExitFindings, nil
}

// autoAcceptLevel là mức rủi ro khi bước được tự accept (spec risk-flow §5.3): case qua, phạm vi và ngân sách sạch,
// không có file nhờ dev cho phép thêm, và flow.step chụp trong risk.yaml là auto. Không có risk.yaml thì dev accept
// ("" = không tự accept).
func autoAcceptLevel(f *Feature, steps *step.Service, id string, ch step.StepCheck) (string, error) {
	if ch.OverBudget {
		return "", nil
	}
	r, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil || !ok || !risk.AutoStep(&r) {
		return "", nil
	}
	extras, err := steps.UsesScopeExtras(id)
	if err != nil || extras {
		return "", err
	}
	return r.Level, nil
}

func nextStep(ctx *Context, f *Feature, id string) {
	specs := f.Specs()
	i := slices.IndexFunc(specs, func(s spec.StepSpec) bool { return s.ID == id })
	if i >= 0 && i+1 < len(specs) {
		ctx.Println("Bước kế: `aisdlc step start " + specs[i+1].ID + "`.")
	} else {
		ctx.Println("Đã xong mọi bước.")
	}
}

func stepAccept(ctx *Context, f *Feature, id string) (int, error) {
	steps := f.Steps()
	recs, err := steps.All()
	if err != nil {
		return 0, err
	}
	for _, x := range recs {
		if x.ID != id || x.Status != step.StepAccepted || x.AcceptedBy == "" {
			continue
		}
		if x.AcceptedBy == gate.ApprovalAuto {
			ctx.Println("Bước " + id + " đã được tự duyệt theo mức rủi ro.")
			return ExitOK, nil
		}
		if by, ok := strings.CutSuffix(x.AcceptedBy, webSuffix); ok {
			ctx.Println("Bước " + id + " đã được accept từ web (" + by + ").")
			return ExitOK, nil
		}
		break
	}
	rec, err := requireStepStatus(steps, id, step.StepAwaitingDev)
	if err != nil {
		return 0, err
	}
	ctx.Println("Tính năng " + f.Key.String() + " — bước " + id)
	if rec.Check != nil {
		ctx.Println(stepCheckSummary(*rec.Check))
	}
	if _, err := ctx.ConfirmDev(f.Key, invocation(ctx, "step accept"), ""); err != nil {
		return 0, err
	}
	approver, err := ctx.Approver(f.Root)
	if err != nil {
		return 0, err
	}
	if _, err := steps.Accept(id, approver); err != nil {
		return 0, err
	}
	ctx.Println("Đã chấp nhận bước " + id + " (" + approver + ").")
	nextStep(ctx, f, id)
	return ExitOK, nil
}

func budgetText(b *spec.Budget) string {
	if b == nil {
		return "chưa khai báo"
	}
	return strconv.Itoa(b.Files) + " file / " + strconv.Itoa(b.Lines) + " dòng"
}

func caseKind(c spec.CaseSpec) string {
	if c.Kind == "" {
		return "?"
	}
	return strings.ToLower(string(c.Kind))
}

func specOf(f *Feature, id string) (spec.StepSpec, error) {
	for _, s := range f.Specs() {
		if s.ID == id {
			return s, nil
		}
	}
	return spec.StepSpec{}, abort(2, "Không có bước "+id+" trong thiết kế.")
}

// stepsUpTo là các bước từ đầu đến id (gồm id).
func stepsUpTo(f *Feature, id string) ([]string, error) {
	if _, err := specOf(f, id); err != nil {
		return nil, err
	}
	var out []string
	for _, s := range f.Specs() {
		out = append(out, s.ID)
		if s.ID == id {
			break
		}
	}
	return out, nil
}

func requireStepStatus(steps *step.Service, id string, status step.StepStatus) (step.StepRecord, error) {
	recs, err := steps.All()
	if err != nil {
		return step.StepRecord{}, err
	}
	cur := step.StepNotStarted
	if i := slices.IndexFunc(recs, func(r step.StepRecord) bool { return r.ID == id }); i >= 0 {
		if recs[i].Status == status {
			return recs[i], nil
		}
		cur = recs[i].Status
	}
	return step.StepRecord{}, abort(2, "Bước "+id+" đang ở trạng thái "+string(cur)+", cần "+string(status)+".")
}
