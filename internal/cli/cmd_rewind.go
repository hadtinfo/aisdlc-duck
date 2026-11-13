package cli

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/rewind"
)

func newRewindCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	c := &cobra.Command{
		Use:   "rewind [mốc]", // i18n:data tên lệnh/đối số, theo thiết kế
		Short: i18n.T("Chỉ dev: quay tính năng về đầu một mốc (brd, design, cases, Sx, review), xoá mọi thứ sau đó; có ref dự phòng."),
		Args:  cobra.MaximumNArgs(1),
	}
	fo.AddTo(c)
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		if f.Mode == gate.ModeWeb {
			return 0, abort(2, i18n.T("`aisdlc rewind` chưa hỗ trợ chế độ web."))
		}
		return runRewind(ctx, f, args)
	})
	return c
}

func runRewind(ctx *Context, f *Feature, args []string) (int, error) {
	pend, hasPend, err := rewind.Pending(f.Paths)
	if err != nil {
		return 0, err
	}
	in, err := rewindInput(f)
	if err != nil {
		return 0, err
	}
	var cp rewind.Checkpoint
	if len(args) == 1 {
		if cp, err = rewind.Parse(in, args[0]); err != nil {
			return 0, abort(2, err.Error())
		}
	} else if cp, err = pickCheckpoint(ctx, f, in); err != nil {
		return 0, err
	}

	d, err := rewindDeps(ctx, f)
	if err != nil {
		return 0, err
	}
	var plan rewind.Plan
	backupRef := ""
	resume := hasPend && pend.Plan.Target.ID == cp.ID
	switch {
	case resume:
		plan, backupRef = pend.Plan, pend.BackupRef
		ctx.Println(i18n.Tf("Tiếp tục lần quay về %s đang dở (dự phòng: %s).", cp.ID, backupRef))
	default:
		if hasPend {
			ctx.Println(i18n.Tf("Cảnh báo: có lần quay về %s đang dở (rewind-pending.json); đang quay về %s thay thế. "+
				"Bản dự phòng của lần dở: %s.", pend.Plan.Target.ID, cp.ID, pend.BackupRef))
		}
		if plan, err = rewind.Build(in, cp); err != nil {
			return 0, abort(2, err.Error())
		}
		if err := rewind.ResolveCode(ctx.Std(), d, &plan); err != nil {
			return 0, abort(2, err.Error())
		}
	}

	printRewindPreview(ctx, f, plan, backupRef)
	if _, err := ctx.ConfirmDev(f.Key, invocation(ctx, "rewind"), ""); err != nil {
		return 0, err
	}
	if !resume {
		if backupRef, err = rewind.Backup(ctx.Std(), d); err != nil {
			return 0, err
		}
		ctx.Println(i18n.Tf("Bản hiện tại cất ở %s", backupRef))
	}
	// Task 9: chế độ web gọi server giữa Backup và Apply tại đây.
	if err := rewind.Apply(ctx.Std(), d, plan, backupRef); err != nil {
		return 0, err
	}
	ctx.Println(i18n.Tf("Đã quay %s về đầu %s. Tiếp: %s", f.Key.String(), cp.ID, rewindNext(cp)))
	ctx.AfterCommand(f, "rewind "+strings.ToLower(cp.ID))
	return ExitOK, nil
}

func rewindNext(cp rewind.Checkpoint) string {
	if cp.Step != "" {
		return "aisdlc step start " + cp.Step
	}
	return "/aisdlc:" + strings.ToLower(cp.ID)
}

func rewindInput(f *Feature) (rewind.Input, error) {
	recs, err := f.Steps().All()
	if err != nil {
		return rewind.Input{}, err
	}
	first, hasFirst, err := f.Gates.FirstNotOpen()
	if err != nil {
		return rewind.Input{}, err
	}
	var gerr error
	reached := func(g gate.Gate) bool {
		if hasFirst && g == first {
			return true
		}
		st, err := f.Gates.State(g)
		if err != nil {
			gerr = err
			return false
		}
		return st != gate.LockedByPrevious
	}
	in := rewind.Input{Key: f.Key, Flow: f.Flow(), Paths: f.Paths, Steps: f.Specs(), Records: recs,
		Reached: reached, RiskFromDesign: f.Flow() == model.FlowFull}
	// Thử một lượt để lỗi đọc trạng thái cổng không bị nuốt.
	for _, g := range gate.Sequence(f.Flow()) {
		reached(g)
	}
	return in, gerr
}

func rewindDeps(ctx *Context, f *Feature) (rewind.Deps, error) {
	by, err := ctx.Approver(f.Root)
	if err != nil {
		return rewind.Deps{}, err
	}
	store := gate.NewLocalGateStore(f.Paths)
	return rewind.Deps{
		Git: f.Git(), Paths: f.Paths, Cfg: f.Config, Steps: f.Steps(), Extras: f.ScopeExtras(),
		ClearGate:      store.Clear,
		AppendFeedback: func(text string) error { return appendFeedback(f.Paths, text) },
		Now:            ctx.Clock(), By: by,
	}, nil
}

func appendFeedback(p model.FeaturePaths, text string) error {
	if err := os.MkdirAll(p.Dir(), 0o755); err != nil {
		return abort(1, i18n.Tf("Không ghi được feedback.md: %s", err.Error()))
	}
	fh, err := os.OpenFile(p.Feedback(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return abort(1, i18n.Tf("Không ghi được feedback.md: %s", err.Error()))
	}
	defer fh.Close()
	if _, err := fh.WriteString(text); err != nil {
		return abort(1, i18n.Tf("Không ghi được feedback.md: %s", err.Error()))
	}
	return nil
}

func checkpointLabel(cp rewind.Checkpoint) string {
	switch cp.ID {
	case "brd":
		return "BRD"
	case "design":
		return i18n.T("Thiết kế")
	case "cases":
		return i18n.T("Case")
	case "review":
		return "Review"
	}
	return cp.ID
}

// deletionSummary: tóm tắt phần bị xoá của mốc cp cho danh sách chọn.
func deletionSummary(f *Feature, in rewind.Input, cp rewind.Checkpoint) string {
	pl, err := rewind.Build(in, cp)
	if err != nil {
		return ""
	}
	var parts []string
	seen := map[string]bool{}
	for _, p := range pl.DeletePaths {
		n := f.Paths.Rel(p)
		n = n[strings.LastIndex(n, "/")+1:]
		if !seen[n] {
			seen[n] = true
			parts = append(parts, n)
		}
	}
	if n := len(pl.DropSteps); n > 0 {
		parts = append(parts, i18n.Tf("code %s", stepRange(pl.DropSteps)))
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + i18n.T("xoá: ") + strings.Join(parts, ", ")
}

func stepRange(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return ids[0] + "–" + ids[len(ids)-1]
}

func pickCheckpoint(ctx *Context, f *Feature, in rewind.Input) (rewind.Checkpoint, error) {
	cps := rewind.Checkpoints(in)
	if len(cps) == 0 {
		return rewind.Checkpoint{}, abort(2, i18n.T("Tính năng chưa có mốc nào để quay về."))
	}
	ctx.Println(i18n.Tf("? Quay về mốc nào (%s)", f.Key.String()))
	for i, cp := range cps {
		ctx.Println("  " + strconv.Itoa(i+1) + ". " + i18n.Tf("Đầu %s", checkpointLabel(cp)) + deletionSummary(f, in, cp))
	}
	s, err := ctx.AskDev(i18n.T("Chọn mốc (số): "))
	if err != nil {
		return rewind.Checkpoint{}, err
	}
	n, convErr := strconv.Atoi(s)
	if convErr != nil || n < 1 || n > len(cps) {
		return rewind.Checkpoint{}, abort(2, i18n.Tf("Lựa chọn %q không hợp lệ (1–%d).", s, len(cps)))
	}
	return cps[n-1], nil
}

func printRewindPreview(ctx *Context, f *Feature, p rewind.Plan, backupRef string) {
	key := f.Key.String()
	ctx.Println("")
	ctx.Println(i18n.Tf("Sẽ quay %s về đầu %s:", key, checkpointLabel(p.Target)))
	if len(p.DropSteps) > 0 {
		ctx.Println("  - " + i18n.Tf("Bỏ bước %s", strings.Join(p.DropSteps, ", ")))
	}
	if len(p.ClearGates) > 0 {
		names := make([]string, len(p.ClearGates))
		for i, g := range p.ClearGates {
			names[i] = string(g)
		}
		ctx.Println("  - " + i18n.Tf("Mở lại cổng %s", strings.Join(names, ", ")))
	}
	for _, path := range p.DeletePaths {
		ctx.Println("  - " + i18n.Tf("Xoá %s", f.Paths.Rel(path)))
	}
	if p.CodeTree != "" {
		all := append(append([]gitx.FileChange{}, p.OwnChanges...), p.ForeignChanges...)
		changed, removed := 0, 0
		for _, c := range all {
			if c.Type == gitx.Added {
				removed++
			} else {
				changed++
			}
		}
		if len(all) == 0 {
			ctx.Println("  - " + i18n.Tf("Code đã đúng snapshot đầu %s: không có file nào đổi.", checkpointLabel(p.Target)))
		} else {
			ctx.Println("  - " + i18n.Tf("Code về snapshot đầu %s: %d file đổi, %d file mới bị xoá", checkpointLabel(p.Target), changed, removed))
		}
		for _, c := range p.OwnChanges {
			ctx.Println("      " + c.Path)
		}
		if n := len(p.ForeignChanges); n > 0 {
			ctx.Println(i18n.Tf("Cảnh báo: %d file đổi ngoài các bước của %s (merge/pull, sửa tay, tính năng khác) cũng sẽ về như mốc:", n, key))
			for _, c := range p.ForeignChanges {
				ctx.Println("      " + c.Path)
			}
		}
		if len(p.Skipped) > 0 {
			ctx.Println(i18n.Tf("Không khôi phục %d file bị git bỏ qua (nội dung hiện tại không nằm trong bản dự phòng):", len(p.Skipped)))
			for _, s := range p.Skipped {
				ctx.Println("      " + s)
			}
		}
	}
	if backupRef != "" {
		ctx.Println("  - " + i18n.Tf("Bản dự phòng đã có: %s", backupRef))
	} else {
		ctx.Println("  - " + i18n.T("Bản hiện tại sẽ được cất vào refs/aisdlc/backup/") + key + "/<yyyymmdd-hhmm>")
	}
}
