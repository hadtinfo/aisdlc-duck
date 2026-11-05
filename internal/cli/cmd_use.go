package cli

import (
	"github.com/spf13/cobra"

	"aisdlc/internal/model"
)

func newUseCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "use KEY",
		Short: "Đổi tính năng đang làm.",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		root, err := ctx.RepoRoot()
		if err != nil {
			return 0, err
		}
		k, err := model.ParseFeatureKey(args[0])
		if err != nil {
			return 0, err
		}
		f, err := ctx.FeatureAt(root, k)
		if err != nil {
			return 0, err
		}
		resolver := NewFeatureResolver(ctx)
		if err := resolver.Bind(root, f.Key); err != nil {
			return 0, err
		}
		ctx.Println("Đang làm tính năng " + f.Key.String() + " (" + string(f.Flow()) + ").")
		return ExitOK, resolver.WarnSameBranch(root)
	})
	return c
}
