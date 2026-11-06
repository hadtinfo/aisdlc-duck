package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newScopeCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "scope",
		Short: "Chỉ dev: cho phép thêm path ngoài phạm vi bước.",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	var fo FeatureOption
	allow := &cobra.Command{
		Use:   "allow <path>",
		Short: "Chỉ dev: cho phép sửa <path> (ghi vào scope-extra.json).",
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
			return 0, abort(2, "Path không hợp lệ: "+path)
		}
		p := path
		if !filepath.IsAbs(p) {
			p = filepath.Join(ctx.Cwd(), p)
		}
		rel := f.Paths.Rel(p)
		if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return 0, abort(2, "Path nằm ngoài repo: "+path)
		}
		ctx.Println("Tính năng " + f.Key.String() + " — cho phép sửa ngoài phạm vi: " + rel)
		if _, err := ctx.ConfirmDev(f.Key, invocation(ctx, "scope allow"), ""); err != nil {
			return 0, err
		}
		approver, err := ctx.Approver(f.Root)
		if err != nil {
			return 0, err
		}
		if err := f.ScopeExtras().Add(rel, approver, ctx.Now()); err != nil {
			return 0, err
		}
		ctx.Println("Đã cho phép " + rel + ".")
		ctx.AfterCommand(f, "scope allow")
		return ExitOK, nil
	})
	c.AddCommand(allow)
	return c
}
