package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newReopenCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	c := &cobra.Command{
		Use:   "reopen <gate>",
		Short: "Chỉ dev: mở lại một cổng để sửa artifact (xóa bản nộp và bản duyệt).",
		Args:  cobra.ExactArgs(1),
	}
	fo.AddTo(c)
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		if err := ctx.WebNetwork(f); err != nil {
			return 0, err
		}
		g, err := gateInFlow(f, args[0])
		if err != nil {
			return 0, err
		}
		if _, err := gateSummary(ctx, f, g); err != nil {
			return 0, err
		}
		hash, err := f.Gates.CurrentHash(g)
		if err != nil {
			return 0, err
		}
		if _, err := ctx.ConfirmDev(f.Key, invocation(ctx, "reopen"), hash); err != nil {
			return 0, err
		}
		approver, err := ctx.Approver(f.Root)
		if err != nil {
			return 0, err
		}
		if err := f.Gates.Reopen(g, approver); err != nil {
			return 0, err
		}
		ctx.Println("Đã mở lại cổng " + string(g) + "; sửa artifact rồi `aisdlc submit " +
			strings.ToLower(string(g)) + "`.")
		return ExitOK, nil
	})
	return c
}
