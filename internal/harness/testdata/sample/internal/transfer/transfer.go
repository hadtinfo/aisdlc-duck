// Package transfer là API chuyển tiền của dịch vụ mẫu.
package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Request là body của POST /api/transfers.
type Request struct {
	FromAccount string `json:"fromAccount"`
	ToAccount   string `json:"toAccount"`
	Amount      int64  `json:"amount"`
}

// Sufficient cho biết số dư có đủ để chuyển amount không.
func Sufficient(balance, amount int64) bool {
	return balance >= amount
}

// Handler xử lý POST /api/transfers.
type Handler struct {
	DB      *pgxpool.Pool
	CoreURL string      // gốc của core banking (GET /accounts/{id}/balance)
	Kafka   *kgo.Client // nil: không phát message
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) balance(ctx context.Context, account string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.CoreURL+"/accounts/"+url.PathEscape(account)+"/balance", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("core banking trả %d", resp.StatusCode)
	}
	var b struct {
		Balance int64 `json:"balance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		return 0, err
	}
	return b.Balance, nil
}

// ServeHTTP kiểm số dư, ghi giao dịch và phát transfer.created nếu có Kafka.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "BAD_REQUEST"})
		return
	}
	bal, err := h.balance(r.Context(), req.FromAccount)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"code": "CORE_BANKING", "error": err.Error()})
		return
	}
	if !Sufficient(bal, req.Amount) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "INSUFFICIENT_BALANCE"})
		return
	}
	if _, err := h.DB.Exec(r.Context(), "INSERT INTO transfer (from_account, to_account, amount) VALUES ($1, $2, $3)", req.FromAccount, req.ToAccount, req.Amount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "DB", "error": err.Error()})
		return
	}
	if h.Kafka != nil {
		from, _ := json.Marshal(req.FromAccount)
		value := fmt.Sprintf(`{"status":"CREATED","from":%s,"amount":%d}`, from, req.Amount)
		rec := &kgo.Record{Topic: "transfer.created", Key: []byte(req.FromAccount), Value: []byte(value)}
		if err := h.Kafka.ProduceSync(r.Context(), rec).FirstErr(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "KAFKA", "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "OK"})
}
