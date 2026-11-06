package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/fixture/internal/fee"
)

// routes khai mọi endpoint của dịch vụ; pool là Postgres từ DATABASE_URL (nil khi không đặt).
func routes(pool *pgxpool.Pool) http.Handler {
	_ = pool // chưa endpoint nào dùng DB
	r := chi.NewRouter()
	r.Mount("/", fee.Router())
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}
