package gates_test

import (
	"context"
	"fmt"
	"net/http/httptest"
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
