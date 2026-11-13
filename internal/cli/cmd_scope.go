package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/i18n"
)

func newScopeCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "scope",
		Short: i18n.T("Chỉ dev: cho phép thêm path ngoài phạm vi bước."),
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	var fo FeatureOption
	allow := &cobra.Command{
		Use:   "allow <path>",
		Short: i18n.T("Chỉ dev: cho phép sửa <path> (ghi vào scope-extra.json)."),
		Args:  cobra.ExactArgs(1),
	}
	fo.AddTo(allow)
	allow.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		path := args[0]
		if strings.ContainsRune(path, 0) {
			return 0, abort(2, i18n.Tf("Path không hợp lệ: %s", path))
		}
		p := path
		if !filepath.IsAbs(p) {
			p = filepath.Join(ctx.Cwd(), p)
		}
		rel := f.Paths.Rel(p)
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return 0, abort(2, i18n.Tf("Path nằm ngoài repo: %s", path))
		}
		ctx.Println(i18n.Tf("Tính năng %s — cho phép sửa ngoài phạm vi: %s", f.Key.String(), rel))
		if _, err := ctx.ConfirmDev(f.Key, invocation(ctx, "scope allow"), ""); err != nil {
			return 0, err
		}
		approver, err := ctx.Approver(f.Root)
		if err != nil {
			return 0, err
		}
		cur, has, err := f.Steps().Current()
		if err != nil {
			return 0, err
		}
		stepID := ""
		if has {
			stepID = cur.ID
		}
		if err := f.ScopeExtras().Add(rel, approver, stepID, ctx.Now()); err != nil {
			return 0, err
		}
		ctx.Println(i18n.Tf("Đã cho phép %s.", rel))
		ctx.AfterCommand(f, "scope allow")
		return ExitOK, nil
	})
	c.AddCommand(allow)
	return c
}
