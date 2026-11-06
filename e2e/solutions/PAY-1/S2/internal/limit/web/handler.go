// Package web là API kiểm tra hạn mức ngày.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/fixture/internal/limit"
)

// Handler phục vụ POST /api/v1/limits/check.
type Handler struct{ limits *limit.Service }

// New tạo Handler đọc hạn mức từ db.
func New(db *pgxpool.Pool) Handler { return Handler{limits: limit.NewService(db)} }

type checkRequest struct {
	AccountID string `json:"accountId"`
	Amount    *int64 `json:"amount"`
}

// Check: 400 VALIDATION_ERROR khi thiếu accountId hoặc amount ≤ 0; 404 LIMIT_NOT_FOUND khi chưa có hạn mức;
// còn lại 200 {allowed, remaining}.
func (h Handler) Check(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AccountID) == "" ||
		req.Amount == nil || *req.Amount <= 0 {
		write(w, http.StatusBadRequest, map[string]any{"code": "VALIDATION_ERROR"})
		return
	}
	res, found, err := h.limits.Check(r.Context(), req.AccountID, *req.Amount)
	switch {
	case err != nil:
		log.Printf("kiểm tra hạn mức: %v", err)
		write(w, http.StatusInternalServerError, map[string]any{"code": "INTERNAL_ERROR"})
	case !found:
		write(w, http.StatusNotFound, map[string]any{"code": "LIMIT_NOT_FOUND"})
	default:
		write(w, http.StatusOK, res)
	}
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
