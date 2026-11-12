package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
)

func newRejectCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var message string
	c := &cobra.Command{
		Use:   "reject <gate> -m <text>",
		Short: i18n.T("Chỉ dev: trả lại một cổng kèm góp ý (ghi vào feedback.md)."),
		Args:  cobra.ExactArgs(1),
	}
	fo.AddTo(c)
	c.Flags().StringVarP(&message, "message", "m", "", i18n.T("Góp ý cho agent"))
	_ = c.MarkFlagRequired("message")
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		g, err := gateInFlow(f, args[0])
		if err != nil {
			return 0, err
		}
		if _, err := gateSummary(ctx, f, g); err != nil {
			return 0, err
		}
		web := f.Mode == gate.ModeWeb
		if web {
			if err := f.Web.RequireApproveToken(f, g); err != nil {
				return 0, err
			}
		}
		hash, err := f.Gates.CurrentHash(g)
		if err != nil {
			return 0, err
		}
		channel, err := ctx.ConfirmDev(f.Key, invocation(ctx, "reject"), hash)
		if err != nil {
			return 0, err
		}
		if web {
			if err := f.Web.Act("reject", f, g, &message, nil, channel); err != nil {
				return 0, err
			}
		} else {
			approver, err := ctx.Approver(f.Root)
			if err != nil {
				return 0, err
			}
			if err := f.Gates.Reject(g, approver, message); err != nil {
				return 0, err
			}
		}
		ctx.Println(i18n.Tf("Đã trả lại cổng %s; góp ý ghi vào %s.", string(g), f.Paths.Rel(f.Paths.Feedback())))
		ctx.AfterCommand(f, "reject "+strings.ToLower(string(g)))
		return ExitOK, nil
	})
	return c
}
