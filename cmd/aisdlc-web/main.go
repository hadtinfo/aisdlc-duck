// Lệnh aisdlc-web: server web AISDLC. Không đối số thì chạy server; `user ...` là quản trị user (UserAdminCli).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // múi giờ nhúng: TZ hoạt động trong ảnh distroless (không có /usr/share/zoneinfo); purge 03:00 theo giờ địa phương cần

	"aisdlc/internal/web"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "user" {
		return runUser(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 {
		fmt.Fprintln(stderr, httpx.OpLocale().Tf("lệnh %q chưa được hỗ trợ", args[0]))
		return 2
	}
	if err := web.Run(ctx, web.ConfigFromEnv()); err != nil {
		fmt.Fprintln(stderr, "aisdlc-web:", err)
		return 1
	}
	return 0
}

// runUser: chế độ quản trị, không mở cổng web, chỉ cần DB (migration chạy trước như Flyway của bản Java).
func runUser(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cfg := web.ConfigFromEnv()
	pool, err := store.Open(ctx, cfg.DBURL, cfg.DBUser, cfg.DBPassword)
	if err != nil {
		fmt.Fprintln(stderr, "aisdlc-web:", err)
		return 1
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		fmt.Fprintln(stderr, "aisdlc-web:", err)
		return 1
	}
	return auth.RunAdmin(ctx, pool, args, stdout)
}
