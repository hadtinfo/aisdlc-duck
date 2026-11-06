// Lệnh aisdlc-web: server web AISDLC. Không đối số thì chạy server; `user ...` do task sau.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"aisdlc/internal/web"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "lệnh %q chưa được hỗ trợ\n", os.Args[1])
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := web.Run(ctx, web.ConfigFromEnv()); err != nil {
		fmt.Fprintln(os.Stderr, "aisdlc-web:", err)
		os.Exit(1)
	}
}
