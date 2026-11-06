package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/policy"
	"aisdlc/internal/risk"
)

func newRiskCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "risk",
		Short: "Chỉ dev: đổi mức rủi ro hoặc tính lại theo chính sách hiện tại (cần console thật).",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	var setFo, reFo FeatureOption
	var reason string
	set := &cobra.Command{
		Use:   "set <level>",
		Short: "Chỉ dev: đặt mức rủi ro tay (hạ mức phải có --reason).",
		Args:  cobra.ExactArgs(1),
	}
	setFo.AddTo(set)
	set.Flags().StringVar(&reason, "reason", "", "Lý do (bắt buộc khi hạ mức)")
	set.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := setFo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		return riskSet(ctx, f, args[0], reason)
	})
	reclassify := &cobra.Command{
		Use:   "reclassify",
		Short: "Chỉ dev: tính lại mức theo chính sách hiện tại, bỏ mức đổi tay.",
		Args:  cobra.NoArgs,
	}
	reFo.AddTo(reclassify)
	reclassify.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
		f, err := reFo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		return riskReclassify(ctx, f)
	})
	c.AddCommand(set, reclassify)
	return c
}

func riskSet(ctx *Context, f *Feature, level, reason string) (int, error) {
	// chế độ web: server cần event RISK_OVERRIDDEN để thay thế bản tự duyệt (giai đoạn 4)
	if err := ctx.WebNetwork(f); err != nil {
		return 0, err
	}
	cur, ok, err := readRisk(f)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, abort(2, "Chưa có "+f.Paths.Rel(f.Paths.Risk())+": mức rủi ro được tính lúc `aisdlc submit"+
			" design` (luồng lite: `aisdlc submit cases`).")
	}
	approved, err := f.Gates.HasApproval(gate.CASES)
	if err != nil {
		return 0, err
	}
	if approved {
		return 0, abort(2, "Cổng Case đã duyệt; mở lại cổng Case trước (`aisdlc reopen cases`).")
	}
	l, err := LoadPolicy(ctx, f.Root, false)
	if err != nil {
		return 0, err
	}
	p := l.Policy
	// mức và yêu cầu chỉ so được trong cùng bản chính sách đã dùng để phân loại
	if cur.Policy.Hash == "" || p.Hash() != cur.Policy.Hash {
		return 0, abort(2, "Chính sách đã đổi kể từ lần phân loại; chạy `aisdlc risk reclassify` trước.")
	}
	scale := "[" + strings.Join(p.Levels, ", ") + "]"
	if p.Rank(cur.Level) < 0 {
		return 0, abort(2, "Mức hiện tại '"+cur.Level+"' trong "+f.Paths.Rel(f.Paths.Risk())+
			" không có trong thang "+scale+"; chạy `aisdlc risk reclassify`.")
	}
	if p.Rank(level) < 0 {
		return 0, abort(2, "Mức '"+level+"' không có trong thang "+scale+".")
	}
	if level == cur.Level {
		return 0, abort(2, "Mức rủi ro hiện tại đã là "+level+".")
	}
	why := strings.TrimSpace(reason)
	lower := p.Rank(level) < p.Rank(cur.Level)
	if lower && p.Classification.DevLower == risk.DevLowerNever {
		return 0, abort(2, "Chính sách không cho dev hạ mức (classification.devLower: never).")
	}
	if lower && why == "" {
		return 0, abort(2, "Hạ mức ("+cur.Level+" → "+level+") phải ghi lý do: `aisdlc risk set "+level+
			" --reason \"…\"`.")
	}
	cases, err := f.Gates.State(gate.CASES)
	if err != nil {
		return 0, err
	}
	note := ""
	if why != "" {
		note = " (lý do: " + why + ")"
	}
	ctx.Println("Đổi mức rủi ro " + f.Key.String() + ": " + cur.Level + " → " + level + note)
	if err := confirmRisk(ctx, f, invocation(ctx, "risk set")); err != nil {
		return 0, err
	}
	by, err := ctx.Approver(f.Root)
	if err != nil {
		return 0, err
	}
	next := cur.WithOverride(p, level, by, why, ctx.Now())
	if err := next.Write(f.Paths.Risk()); err != nil {
		return 0, err
	}
	riskOverridden(ctx, f, cur.Level, level, why)
	ctx.Println("Đã đổi mức rủi ro thành " + level + ". Yêu cầu: " + requirementIDs(next.Requirements))
	if _, err := supersede(ctx, f, &p, cur.Level, next); err != nil {
		return 0, err
	}
	if cases == gate.AwaitingApproval || cases == gate.SubmittedWithFindings {
		// risk.yaml nằm trong hash cổng Case: bản nộp cũ không còn khớp; local thì xóa hẳn bản nộp như reopen
		if err := f.Gates.Reopen(gate.CASES, by); err != nil {
			return 0, err
		}
		ctx.Println("Cổng Case cần nộp lại: `aisdlc submit cases`.")
	}
	ctx.AfterCommand(f, "risk set "+level)
	return ExitOK, nil
}

func riskReclassify(ctx *Context, f *Feature) (int, error) {
	// chế độ web: mở lại cổng Case và báo mức mới đều qua web (giai đoạn 4)
	if err := ctx.WebNetwork(f); err != nil {
		return 0, err
	}
	l, err := LoadPolicy(ctx, f.Root, false)
	if err != nil {
		return 0, err
	}
	p := l.Policy
	old, hasOld, err := readRisk(f)
	if err != nil {
		return 0, err
	}
	reopen, err := f.Gates.HasApproval(gate.CASES)
	if err != nil {
		return 0, err
	}
	fresh, err := classifyRisk(p, f)
	if err != nil {
		return 0, err
	}
	from, versions, reqs := "(chưa có)", "", ""
	if hasOld {
		from = old.Level
		versions = "v" + strconv.Itoa(old.Policy.Version) + " → "
		reqs = requirementIDs(old.Requirements) + " → "
	}
	ctx.Println("Tính lại mức rủi ro " + f.Key.String() + ": " + from + " → " + fresh.Level + " (chính sách " +
		versions + "v" + strconv.Itoa(p.Version) + ")")
	ctx.Println("Yêu cầu: " + reqs + requirementIDs(fresh.Requirements))
	ctx.Println("Rule khớp: " + matchedText(fresh.Matched))
	if reopen {
		ctx.Println("Cổng Case đã duyệt: lệnh này sẽ mở lại cổng Case.")
	}
	if err := confirmRisk(ctx, f, policy.DevCommand{}); err != nil { // reclassify: chỉ console thật (không có phiếu)
		return 0, err
	}
	if reopen {
		by, err := ctx.Approver(f.Root)
		if err != nil {
			return 0, err
		}
		if err := f.Gates.Reopen(gate.CASES, by); err != nil {
			return 0, err
		}
	}
	if err := fresh.Write(f.Paths.Risk()); err != nil {
		return 0, err
	}
	riskClassified(ctx, f, fresh)
	tail := ""
	if reopen {
		tail = " Cổng Case đã mở lại; nộp lại `aisdlc submit cases`."
	}
	ctx.Println("Đã ghi " + f.Paths.Rel(f.Paths.Risk()) + " mức " + fresh.Level + "." + tail)
	before := ""
	if hasOld {
		before = old.Level
	}
	if _, err := supersede(ctx, f, &p, before, fresh); err != nil {
		return 0, err
	}
	ctx.AfterCommand(f, "risk reclassify")
	return ExitOK, nil
}

// confirmRisk: console thật + gõ lại feature key như approve, hoặc phiếu cho phép của Claude Code cho action;
// không được thì exit 2 (không phải 3 như các lệnh chỉ dev khác).
func confirmRisk(ctx *Context, f *Feature, action policy.DevCommand) error {
	_, err := ctx.ConfirmDev(f.Key, action, "")
	var a *Abort
	if asAbort(err, &a) {
		return abort(2, a.Msg)
	}
	return err
}

func readRisk(f *Feature) (risk.RiskFile, bool, error) {
	r, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil {
		return risk.RiskFile{}, false, abort(2, "Không đọc được "+f.Paths.Rel(f.Paths.Risk())+": "+err.Error())
	}
	return r, ok, nil
}

func requirementIDs(reqs []risk.Requirement) string {
	if len(reqs) == 0 {
		return "(không có)"
	}
	ids := make([]string, len(reqs))
	for i, q := range reqs {
		ids[i] = q.ID
	}
	return strings.Join(ids, ", ")
}
