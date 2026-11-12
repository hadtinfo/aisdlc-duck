package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/i18n"
)

func newDiffCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var byStep, patch bool
	c := &cobra.Command{
		Use:   "diff",
		Short: i18n.T("Diff nhóm theo bước Sx (numstat; --patch để kèm diff đầy đủ)."),
		Args:  cobra.NoArgs,
	}
	fo.AddTo(c)
	c.Flags().BoolVar(&byStep, "by-step", false, i18n.T("Nhóm theo bước"))
	c.Flags().BoolVar(&patch, "patch", false, i18n.T("Kèm `git diff startTree endTree` của từng bước"))
	_ = c.MarkFlagRequired("by-step")
	c.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		titles := map[string]string{}
		for _, s := range f.Specs() {
			if _, ok := titles[s.ID]; !ok {
				titles[s.ID] = s.Title
			}
		}
		recs, err := f.Steps().All()
		if err != nil {
			return 0, err
		}
		text, err := eval.DiffByStep(recs, titles, f.Git(), patch)
		if err != nil {
			return 0, err
		}
		fmt.Fprint(ctx.Out(), text)
		return ExitOK, nil
	})
	return c
}
