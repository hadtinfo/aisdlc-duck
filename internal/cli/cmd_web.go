package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newWebCmd là lệnh web tạm: đủ lệnh con như Java, lệnh con nào cũng dừng vì bản Go chưa có web client.
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
		sub("flush", "Gửi outbox ở nền (hook dùng).", true),
		sub("login", "Chỉ dev: lưu URL web và token ingest, ghim khóa ký của server.", false),
		sub("approve-token", "Chỉ dev: lưu token approve để duyệt từ terminal.", false),
		sub("migrate", "Đưa các bản duyệt cục bộ (gates.json) còn khớp nội dung lên web, chờ chủ tính năng xác nhận.",
			false),
	)
	return c
}
