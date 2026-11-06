package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// newWebCmd là lệnh web: `web flush` (tiến trình gửi nền do hook mở) đã có; các lệnh con khác chưa có trong bản Go.
func newWebCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "web",
		Short: "Web nội bộ: status, push, login, approve-token, migrate.",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	sub := func(name, short string, hidden bool) *cobra.Command {
		return &cobra.Command{Use: name, Short: short, Hidden: hidden, Args: cobra.NoArgs,
			RunE: Wrap(ctx, func(*cobra.Command, []string) (int, error) { return 0, abort(2, WebCmdPhase4Msg) })}
	}
	c.AddCommand(
		sub("status", "Cho biết web đã bật chưa, số bản tin đang chờ, bị từ chối, lần gửi thành công cuối.", false),
		sub("push", "Dựng snapshot mọi tính năng rồi gửi cả hàng đợi.", false),
		newWebFlushCmd(ctx),
		sub("login", "Chỉ dev: lưu URL web và token ingest, ghim khóa ký của server.", false),
		sub("approve-token", "Chỉ dev: lưu token approve để duyệt từ terminal.", false),
		sub("migrate", "Đưa các bản duyệt cục bộ (gates.json) còn khớp nội dung lên web, chờ chủ tính năng xác nhận.",
			false),
	)
	return c
}

// newWebFlushCmd: tiến trình gửi nền do hook mở (WebCommand.flush): gửi hết outbox trong tối đa 5 giây. Không in gì
// (kể cả cảnh báo cấu hình và lỗi gửi), luôn exit 0.
// TODO(4b Task 4/5): làm mới chế độ, trạng thái cổng và điều khiển từ web ở đây.
func newWebFlushCmd(ctx *Context) *cobra.Command {
	return &cobra.Command{Use: "flush", Short: "Gửi outbox ở nền (hook dùng).", Hidden: true, Args: cobra.ArbitraryArgs,
		RunE: Wrap(ctx, func(*cobra.Command, []string) (int, error) {
			defer func() { _ = recover() }()
			if root, ok := FindRepo(ctx.Cwd()); ok {
				_ = ctx.newPublisher(root, io.Discard, false).Drain(ctx.Std())
			}
			return ExitOK, nil
		})}
}
