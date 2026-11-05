package cli

import (
	"github.com/spf13/cobra"
)

// EvalPhase3Msg: eval tầng 1 cần harness Go (giai đoạn 3).
const EvalPhase3Msg = "aisdlc eval cần harness Go (giai đoạn 3)."

// newEvalCmd là lệnh eval tạm: đủ cờ như Java; không --ci thì vẫn phân giải tính năng trước (lỗi tính năng báo như
// lệnh khác), rồi dừng vì chưa có harness.
func newEvalCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var ci bool
	var base string
	c := &cobra.Command{
		Use:   "eval",
		Short: "Eval tầng 1: build, test và bộ case đầy đủ qua harness, ghi eval/report.json và report.md.",
		Args:  cobra.NoArgs,
	}
	fo.AddTo(c)
	c.Flags().BoolVar(&ci, "ci", false,
		"Chế độ CI: không cần tính năng active; eval mọi tính năng có thay đổi so với base")
	c.Flags().StringVar(&base, "base", "", "Nhánh/commit gốc cho CI (mặc định baseRef trong config)")
	c.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
		if !ci {
			if _, err := fo.Feature(ctx); err != nil {
				return 0, err
			}
		}
		return 0, abort(2, EvalPhase3Msg)
	})
	return c
}
