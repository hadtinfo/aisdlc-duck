package fee

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Router trả router chi với GET /api/v1/fees?amount=.
func Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/fees", handle)
	return r
}

func handle(w http.ResponseWriter, r *http.Request) {
	amount, err := strconv.ParseInt(r.URL.Query().Get("amount"), 10, 64)
	if err != nil || amount < 0 {
		write(w, http.StatusBadRequest, map[string]any{"code": "VALIDATION_ERROR"})
		return
	}
	write(w, http.StatusOK, map[string]any{"amount": amount, "fee": Fee(amount)})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
