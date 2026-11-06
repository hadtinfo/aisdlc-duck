// Dịch vụ mẫu e2e: route khai trong routes.go. Tắt êm khi nhận SIGTERM để coverage được ghi.
// FIXTURE_IGNORE_SIGTERM=1: bỏ qua SIGTERM (nhánh âm tính của e2e: harness phải SIGKILL, coverage N/A).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	var pool *pgxpool.Pool
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		var err error
		if pool, err = pgxpool.New(ctx, dsn); err != nil {
			log.Fatalf("database: %v", err)
		}
		defer pool.Close()
	}
	srv := &http.Server{Addr: "127.0.0.1:" + os.Getenv("PORT"), Handler: routes(pool)}

	sig := make(chan os.Signal, 1)
	if os.Getenv("FIXTURE_IGNORE_SIGTERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
		signal.Notify(sig, os.Interrupt)
	} else {
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	}
	go func() {
		<-sig
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("lắng nghe %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server: %v", err)
	}
}
