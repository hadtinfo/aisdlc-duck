// Package web là API kiểm tra hạn mức ngày.
package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler phục vụ POST /api/v1/limits/check.
type Handler struct{}

// New tạo Handler; S2 dùng db để đọc hạn mức.
func New(_ *pgxpool.Pool) Handler { return Handler{} }

type checkRequest struct {
	AccountID string `json:"accountId"`
	Amount    *int64 `json:"amount"`
}

// Check kiểm tra request: thiếu accountId hoặc amount ≤ 0 thì 400 VALIDATION_ERROR.
func (h Handler) Check(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AccountID) == "" || req.Amount == nil || *req.Amount <= 0 {
		write(w, http.StatusBadRequest, map[string]any{"code": "VALIDATION_ERROR"})
		return
	}
	w.WriteHeader(http.StatusNotImplemented) // S2 tính hạn mức
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
