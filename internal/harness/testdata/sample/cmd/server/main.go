// Dịch vụ mẫu cho test harness: POST /api/transfers, GET /healthz.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"

	"example.com/sample/internal/transfer"
)

func main() {
	if os.Getenv("SAMPLE_EXIT_AT_START") == "1" {
		fmt.Println("boom")
		os.Exit(3)
	}
	ctx := context.Background()
	h := &transfer.Handler{CoreURL: os.Getenv("CORE_BANKING_URL")}
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		defer pool.Close()
		h.DB = pool
	}
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		cl, err := kgo.NewClient(kgo.SeedBrokers(brokers), kgo.AllowAutoTopicCreation())
		if err != nil {
			log.Fatalf("kafka: %v", err)
		}
		defer cl.Close()
		h.Kafka = cl
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/transfers", h)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: "127.0.0.1:" + os.Getenv("PORT"), Handler: mux}

	if os.Getenv("SAMPLE_IGNORE_SIGTERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
		go func() {
			<-sig
			sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(sctx)
		}()
	}
	log.Printf("lắng nghe %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server: %v", err)
		return
	}
	log.Print("đã tắt")
}
