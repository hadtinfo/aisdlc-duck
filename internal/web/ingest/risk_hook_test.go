package ingest_test

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"aisdlc/internal/web"
	"aisdlc/internal/web/ingest"
	"aisdlc/internal/web/webtest"
)

// TestRiskEventCallsRiskChanged: sự kiện RISK_* gọi hook RiskChanged (AutoApprover.onRiskChanged) trong tx ingest với
// (repo, feature, mức trước, mức mới): mức trước là risk_level của server; NULL thì RISK_OVERRIDDEN lấy detail.from,
// RISK_CLASSIFIED để rỗng (= null của Java). Mức mới rỗng/khoảng trắng hoặc detail hỏng: không gọi. Hook chạy trong tx
// (thấy risk_level mới), lỗi của hook hủy cả lô.
func TestRiskEventCallsRiskChanged(t *testing.T) {
	var mu sync.Mutex
	var calls [][4]string
	var fail error
	var seenLevel []string
	e := webtest.New(t, func(d web.Deps) http.Handler {
		svc := &ingest.Service{Pool: d.Pool, Now: d.Now, RiskChanged: func(ctx context.Context, tx pgx.Tx, repo, feature, from, to string) error {
			var lv *string
			if err := tx.QueryRow(ctx, "SELECT risk_level FROM features WHERE repo_id = $1 AND feature_key = $2", repo, feature).Scan(&lv); err != nil && err != pgx.ErrNoRows {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, [4]string{repo, feature, from, to})
			if lv != nil {
				seenLevel = append(seenLevel, *lv)
			}
			return fail
		}}
		r := chi.NewRouter()
		ingest.Routes(r, svc)
		return r
	})
	w := e.Client(t)
	post := func(typ, detail string) int {
		return w.Do("POST", "/api/ingest/events", evJSON(typ, detail), map[string]string{"Content-Type": "application/json"}).Status
	}
	got := func() [][4]string {
		mu.Lock()
		defer mu.Unlock()
		return append([][4]string(nil), calls...)
	}
	repo := "gitlab.corp/pay/svc"
	if _, err := e.Pool.Exec(context.Background(), "INSERT INTO repos(id) VALUES ($1)", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(context.Background(), "INSERT INTO features(repo_id, feature_key, snapshot, captured_at, client_id, "+
		"first_sender_login, last_sender_login) VALUES ($1, 'PAY-1', '{}', now(), 'A', 'x', 'x')", repo); err != nil {
		t.Fatal(err)
	}

	// risk_level NULL: RISK_OVERRIDDEN lấy from trong detail
	post("RISK_OVERRIDDEN", `{"from":"low","to":"medium","reason":"r"}`)
	// đã biết medium: mức trước là của server, không phải detail.from
	post("RISK_OVERRIDDEN", `{"from":"xx","to":"high"}`)
	post("RISK_CLASSIFIED", `{"level":"low","policyVersion":1}`)
	// mức mới rỗng/khoảng trắng hoặc detail hỏng: không gọi
	post("RISK_CLASSIFIED", `{"level":"  "}`)
	post("RISK_OVERRIDDEN", `{"from":"low"}`)
	post("RISK_CLASSIFIED", `not json`)
	want := [][4]string{{repo, "PAY-1", "low", "medium"}, {repo, "PAY-1", "medium", "high"}, {repo, "PAY-1", "high", "low"}}
	if c := got(); !reflect.DeepEqual(c, want) {
		t.Fatal(c)
	}
	if !reflect.DeepEqual(seenLevel, []string{"medium", "high", "low"}) {
		t.Fatal(seenLevel)
	}

	// RISK_CLASSIFIED khi chưa biết mức: from rỗng (null)
	if _, err := e.Pool.Exec(context.Background(), "UPDATE features SET risk_level = NULL"); err != nil {
		t.Fatal(err)
	}
	post("RISK_CLASSIFIED", `{"level":"high","policyVersion":1}`)
	if c := got(); c[len(c)-1] != [4]string{repo, "PAY-1", "", "high"} {
		t.Fatal(c)
	}

	// lỗi của hook hủy cả lô (sự kiện không được ghi, risk_level không đổi)
	var before int
	_ = e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM events").Scan(&before)
	mu.Lock()
	fail = context.DeadlineExceeded
	mu.Unlock()
	if st := post("RISK_CLASSIFIED", `{"level":"low","policyVersion":1}`); st != 500 {
		t.Fatal(st)
	}
	var after int
	var lv string
	_ = e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM events").Scan(&after)
	_ = e.Pool.QueryRow(context.Background(), "SELECT risk_level FROM features").Scan(&lv)
	if after != before || lv != "high" {
		t.Fatal(before, after, lv)
	}
}
