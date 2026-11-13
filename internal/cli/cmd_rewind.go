package cli

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/fsx"
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
	resume := hasPend && pend.Plan.Target.ID == cp.ID
	requestID := "" // web: requestId gửi server; chạy tiếp dùng lại của lần đầu
	switch {
	case resume:
		if err := rewind.CheckResume(ctx.Std(), d, pend); err != nil {
			return 0, abort(2, err.Error())
		}
		plan, requestID = pend.Plan, pend.RequestID
		ctx.Println(i18n.Tf("Tiếp tục lần quay về %s đang dở (dự phòng: %s).", cp.ID, pend.BackupRef))
	default:
		if hasPend {
			ctx.Println(i18n.Tf("Cảnh báo: có lần quay về %s đang dở (rewind-pending.json); đang quay về %s thay thế. "+
				"Bản dự phòng của lần dở: %s.", pend.Plan.Target.ID, cp.ID, pend.BackupRef))
		}
		if plan, err = rewind.Build(in, cp); err != nil {
			return 0, abort(2, err.Error())
		}
	}
	// Luôn lập lại phần code theo cây mốc: dấu đang dở có thể đã cũ (dev sửa tiếp sau khi đứt).
	if err := rewind.ResolveCode(ctx.Std(), d, &plan); err != nil {
		return 0, abort(2, err.Error())
	}

	web := f.Mode == gate.ModeWeb
	if web {
		if err := f.Web.RequireApproveToken(f, firstGate(plan)); err != nil {
			return 0, err
		}
	}
	printRewindPreview(ctx, f, plan)
	channel, err := ctx.ConfirmDev(f.Key, invocation(ctx, "rewind"), "")
	if err != nil {
		return 0, err
	}
	// Luôn cất bản hiện tại, kể cả khi chạy tiếp: phần sửa sau lần đứt không được mất.
	backupRef, err := rewind.Backup(ctx.Std(), d)
	if err != nil {
		return 0, err
	}
	ctx.Println(i18n.Tf("Bản hiện tại cất ở %s", backupRef))
	if requestID == "" {
		requestID = backupRef
	}
	if web {
		// Server trước: dấu đang dở ghi trước để đứt giữa chừng thì chạy lại dùng cùng requestId rồi Apply.
		var oldMarker []byte
		if !resume {
			oldMarker, _ = os.ReadFile(rewind.PendingFile(f.Paths)) // dấu của mốc khác (nếu có) để khôi phục khi bị từ chối
		}
		if err := rewind.WritePending(d, plan, backupRef, requestID); err != nil {
			return 0, err
		}
		definite, err := f.Web.Rewind(f, plan.Target, plan.ClearGates, requestID, channel)
		if err != nil {
			switch {
			case !definite:
				ctx.Eprintln(i18n.Tf("Không biết server đã áp dụng lần quay về chưa; chạy lại `aisdlc rewind %s` sẽ tiếp tục an toàn. Bản dự phòng: %s",
					strings.ToLower(cp.ID), backupRef))
			case resume:
				ctx.Eprintln(i18n.Tf("Server chưa nhận lần quay về nên chưa đổi gì cục bộ. Bản dự phòng: %s", backupRef))
			default:
				if len(oldMarker) > 0 {
					_ = fsx.WriteAtomic(rewind.PendingFile(f.Paths), oldMarker, 0o644)
				} else {
					_ = os.Remove(rewind.PendingFile(f.Paths))
				}
				ctx.Eprintln(i18n.Tf("Server chưa nhận lần quay về nên chưa đổi gì cục bộ. Bản dự phòng: %s", backupRef))
			}
			return 0, err
		}
	}
	if err := rewind.Apply(ctx.Std(), d, plan, backupRef); err != nil {
		return 0, err
	}
	ctx.Println(i18n.Tf("Đã quay %s về đầu %s. Tiếp: %s", f.Key.String(), cp.ID, rewindNext(cp)))
	ctx.AfterCommand(f, "rewind "+strings.ToLower(cp.ID))
	return ExitOK, nil
}

func firstGate(p rewind.Plan) gate.Gate {
	if len(p.ClearGates) > 0 {
		return p.ClearGates[0]
	}
	return p.Target.Gate
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
	clear := gate.NewLocalGateStore(f.Paths).Clear
	if f.Mode == gate.ModeWeb {
		clear = func(gate.Gate) error { return nil } // server đã xoá trạng thái; cache được làm mới sau lệnh gọi
	}
	return rewind.Deps{
		Git: f.Git(), Paths: f.Paths, Cfg: f.Config, Steps: f.Steps(), Extras: f.ScopeExtras(),
		ClearGate:      clear,
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

func printRewindPreview(ctx *Context, f *Feature, p rewind.Plan) {
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
	ctx.Println("  - " + i18n.T("Bản hiện tại sẽ được cất vào refs/aisdlc/backup/") + key + "/<yyyymmdd-hhmm>")
}
