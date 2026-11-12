package gates_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/web"
)

// TestGateActionsDoNotStarveSmallPool: thao tác cổng chạy trong tx giữ một kết nối và khóa tính năng; mọi truy vấn
// trong tx phải đi qua tx. Với pool chỉ 2 kết nối và nhiều thao tác đồng thời trên cùng tính năng, nếu tx nào lấy thêm
// kết nối pool (vd. đọc email → login) thì bản giữ khóa chờ pool mãi còn các bản kia giữ kết nối chờ khóa: treo.
func TestGateActionsDoNotStarveSmallPool(t *testing.T) {
	fx := newGfx(t, "pl")
	a := fx.feat(t, "r/pl-a", "K-1", "FULL")
	a.submitOk("BRD")
	b := fx.feat(t, "r/pl-b", "K-1", "LITE")
	b.openGates("CASES")

	cfg, err := pgxpool.ParseConfig(fx.e.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 2
	small, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	srv := httptest.NewServer(web.NewHandler(web.Deps{Pool: small, Keys: fx.keys, Now: tickingClock()}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const n = 8
	res := make([]httpResult, 2*n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			res[i] = postTo(ctx, srv.URL, "/api/gates/approve", a.approveBody("BRD", a.hash("BRD")), sessionHdr(fx.leadS.ID, fx.leadS.CSRF))
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"repo":"%s","feature":"%s","gate":"CASES","comment":"lần %d"}`, b.repo, b.key, i)
			res[n+i] = postTo(ctx, srv.URL, "/api/gates/reopen", body, sessionHdr(fx.ownerS.ID, fx.ownerS.CSRF))
		}(i)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	for i, r := range res {
		if r.err != nil || r.status != 200 {
			t.Fatalf("request %d sau %v: %d %v %s", i, time.Since(began), r.status, r.err, r.body)
		}
	}
	if c := fx.approvals(t, "r/pl-a", "BRD"); c != 1 {
		t.Fatal(c)
	}
	if c := fx.count(t, "SELECT count(*) FROM feedback WHERE repo_id = 'r/pl-b'"); c != n {
		t.Fatal(c)
	}
}

// TestAutoHooksDoNotStarveSmallPool: AutoApprove (trong tx nộp) và OnRiskChanged (trong tx ingest) cũng chỉ truy vấn
// qua tx. Nộp đồng thời (tự duyệt) xen với sự kiện rủi ro (thay thế) trên cùng tính năng, pool 2 kết nối: không treo.
func TestAutoHooksDoNotStarveSmallPool(t *testing.T) {
	fx := newGfx(t, "pa")
	a := fx.autoFeat(t, "r/pa-a", "K-1", genericPolicy(t), cards("[]"))
	a.openGates("BRD")
	// Thiết kế tự duyệt ở low: các lần thay thế sau có bản để thu hồi
	if r := a.submitWithRisk("DESIGN", "low"); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	if c := fx.count(t, "SELECT count(*) FROM approvals WHERE repo_id = 'r/pa-a' AND channel = 'auto'"); c != 1 {
		t.Fatal(c)
	}

	cfg, err := pgxpool.ParseConfig(fx.e.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 2
	small, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	srv := httptest.NewServer(web.NewHandler(web.Deps{Pool: small, Keys: fx.keys, Now: tickingClock()}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const n = 8
	res := make([]httpResult, 2*n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			b := a.submissionBody("DESIGN", "[]")
			b = b[:strings.LastIndex(b, "}")] + `,"risk":{"level":"low"}}`
			res[i] = postTo(ctx, srv.URL, "/api/cli/submissions", b, bearerHdr(fx.ingest))
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			level := []string{"high", "low"}[i%2]
			body := fmt.Sprintf(`{"repoId":"%s","events":[{"type":"RISK_CLASSIFIED","at":"2026-11-06T10:00:00Z","feature":"%s",`+
				`"detail":"{\"level\":\"%s\",\"policyVersion\":1}"}]}`, a.repo, a.key, level)
			res[n+i] = postTo(ctx, srv.URL, "/api/ingest/events", body, bearerHdr(fx.ingest))
		}(i)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	for i, r := range res {
		if r.err != nil || r.status != 200 {
			t.Fatalf("request %d sau %v: %d %v %s", i, time.Since(began), r.status, r.err, r.body)
		}
	}
	if c := fx.count(t, "SELECT count(*) FROM approvals WHERE repo_id = 'r/pa-a' AND channel = 'auto' AND revoked_at IS NULL"); c > 1 {
		t.Fatal("nhiều bản tự duyệt còn hiệu lực:", c)
	}
}
