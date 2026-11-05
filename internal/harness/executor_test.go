package harness

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

type fakeDB struct {
	execs      []string
	execErr    map[string]error
	checks     []spec.DbExpect
	checkMsg   map[int]string
	violations map[string][]map[string]any
	violErr    map[string]error
	viol       []string
}

func (d *fakeDB) Exec(_ context.Context, q string) error {
	d.execs = append(d.execs, q)
	return d.execErr[q]
}
func (d *fakeDB) Check(_ context.Context, e spec.DbExpect) (string, error) {
	d.checks = append(d.checks, e)
	return d.checkMsg[len(d.checks)-1], nil
}
func (d *fakeDB) Violations(_ context.Context, q string, max int) ([]map[string]any, error) {
	d.viol = append(d.viol, q)
	return d.violations[q], d.violErr[q]
}

type fakeStubs struct {
	calls [][]string
	err   error
}

func (s *fakeStubs) Register(_ []spec.StubSpec, ids []string) error {
	s.calls = append(s.calls, ids)
	return s.err
}

type fakeProbe struct {
	resets int
	msg    string
	err    error
	want   []spec.MessageExpect
}

func (p *fakeProbe) Reset(context.Context) error { p.resets++; return nil }
func (p *fakeProbe) Check(_ context.Context, w []spec.MessageExpect) (string, error) {
	p.want = w
	return p.msg, p.err
}

func newServer(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/claim": // chỉ request đầu thắng
			if claimed.Add(1) == 1 {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(409)
			}
		case "/slow":
			time.Sleep(2 * time.Second)
		case "/ct":
			fmt.Fprintf(w, `{"ct":%q,"method":%q}`, r.Header.Get("Content-Type"), r.Method)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a":1,"b":{"c":2,"d":3},"list":[1,2]}`)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

var claimed atomic.Int64

type rig struct {
	ex    *Executor
	db    *fakeDB
	stubs *fakeStubs
	probe *fakeProbe
}

func newRig(t *testing.T) *rig {
	srv := newServer(t)
	r := &rig{db: &fakeDB{}, stubs: &fakeStubs{}, probe: &fakeProbe{}}
	r.ex = &Executor{HTTP: &HTTPCaller{BaseURL: srv.URL, RequestTimeout: 10 * time.Second}, DB: r.db, Stubs: r.stubs,
		Messages: r.probe, ResetSQL: []string{"DELETE FROM item"}}
	return r
}

func mk(given spec.Given, when spec.When, then spec.Then) spec.CaseSpec {
	if when.Method == "" {
		when = spec.When{Method: "POST", Path: "/api", Body: map[string]any{"q": 1}, Repeat: 1, Parallel: 1}
	}
	return spec.CaseSpec{ID: "C1", Step: "S1", Kind: spec.KindHappy, Title: "t", Given: given, When: &when, Then: &then}
}

func wh(method, path string, repeat, parallel int) spec.When {
	return spec.When{Method: method, Path: path, Repeat: repeat, Parallel: parallel}
}

func TestUnknownSQLIsError(t *testing.T) {
	r := newRig(t)
	r.db.execErr = map[string]error{"INSERT INTO nope VALUES (1)": errors.New("relation nope does not exist")}
	got := r.ex.Execute(context.Background(), mk(spec.Given{SQL: []string{"INSERT INTO nope VALUES (1)"}}, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeError || got.Message != "given.sql: relation nope does not exist" {
		t.Errorf("%+v", got)
	}
}

func TestResetSQLErrorPrefix(t *testing.T) {
	r := newRig(t)
	r.db.execErr = map[string]error{"DELETE FROM item": errors.New("boom")}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeError || got.Message != "reset_sql: boom" {
		t.Errorf("%+v", got)
	}
}

func TestBodyIsLenientSubset(t *testing.T) {
	r := newRig(t)
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200, Body: map[string]any{"b": map[string]any{"c": 2}}}))
	if got != (run.CaseResult{ID: "C1", Step: "S1", Outcome: run.CaseOutcomePassed}) {
		t.Errorf("%+v", got)
	}
}

func TestBodyMismatchIsFailed(t *testing.T) {
	r := newRig(t)
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200, Body: map[string]any{"b": map[string]any{"c": 9}}}))
	if got.Outcome != run.CaseOutcomeFailed || !strings.HasPrefix(got.Message, "body: ") || !strings.Contains(got.Message, "$.b.c") {
		t.Errorf("%+v", got)
	}
}

func TestStatusMismatchIsFailed(t *testing.T) {
	r := newRig(t)
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 201}))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "status: mong đợi 201, nhận 200" {
		t.Errorf("%+v", got)
	}
}

func TestDBChecksInOrderFirstMismatchReported(t *testing.T) {
	r := newRig(t)
	r.db.checkMsg = map[int]string{1: "dòng 0 lệch"}
	d := []spec.DbExpect{{Query: "q0"}, {Query: "q1"}, {Query: "q2"}}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200, DB: d}))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "db[1]: dòng 0 lệch" || len(r.db.checks) != 2 {
		t.Errorf("%+v checks=%d", got, len(r.db.checks))
	}
}

func TestPhaseOrderResetGivenStubsMessagesThenHTTP(t *testing.T) {
	r := newRig(t)
	g := spec.Given{SQL: []string{"INSERT 1"}, Stubs: []string{"ok"}}
	got := r.ex.Execute(context.Background(), mk(g, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomePassed {
		t.Fatalf("%+v", got)
	}
	if !reflect.DeepEqual(r.db.execs, []string{"DELETE FROM item", "INSERT 1"}) ||
		!reflect.DeepEqual(r.stubs.calls, [][]string{{"ok"}}) || r.probe.resets != 1 {
		t.Errorf("execs=%v stubs=%v resets=%d", r.db.execs, r.stubs.calls, r.probe.resets)
	}
}

func TestStubErrorIsError(t *testing.T) {
	r := newRig(t)
	r.stubs.err = errors.New("không có stub missing")
	got := r.ex.Execute(context.Background(), mk(spec.Given{Stubs: []string{"missing"}}, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeError || got.Message != "given.stubs: không có stub missing" {
		t.Errorf("%+v", got)
	}
}

func TestMessagesWithNoProbeIsErrorBeforeHTTP(t *testing.T) {
	r := newRig(t)
	r.ex.Messages = NoMessageProbe{Reason: NotConfigured}
	then := spec.Then{Status: 201, Messages: []spec.MessageExpect{{Topic: "topic", Value: map[string]any{}}}}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, then))
	if got.Outcome != run.CaseOutcomeError || got.Message != "chưa cấu hình Kafka" || len(r.db.execs) != 0 {
		t.Errorf("%+v execs=%v", got, r.db.execs)
	}
	r.ex.Messages = &NoMessageProbe{Reason: "nhiều ConsumerFactory"}
	if got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, then)); got.Message != "nhiều ConsumerFactory" {
		t.Errorf("%+v", got)
	}
}

func TestNoMessageProbeWithoutMessagesRuns(t *testing.T) {
	r := newRig(t)
	r.ex.Messages = NoMessageProbe{}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomePassed {
		t.Errorf("%+v", got)
	}
}

func TestMessagesMismatchIsFailed(t *testing.T) {
	r := newRig(t)
	r.probe.msg = "topic t: không có message"
	then := spec.Then{Status: 200, Messages: []spec.MessageExpect{{Topic: "t"}}}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, then))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "messages: topic t: không có message" {
		t.Errorf("%+v", got)
	}
	r.probe.msg, r.probe.err = "", errors.New("kafka down")
	got = r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, then))
	if got.Outcome != run.CaseOutcomeError || got.Message != "kiểm tra: kafka down" {
		t.Errorf("%+v", got)
	}
}

func TestIncompleteCaseIsErrorNotPanic(t *testing.T) {
	r := newRig(t)
	when := &spec.When{Method: "GET", Path: "/x", Repeat: 1, Parallel: 1}
	then := &spec.Then{Status: 200}
	broken := []struct {
		c    spec.CaseSpec
		want string
	}{
		{spec.CaseSpec{ID: "C1", Step: "S1", When: when, Then: then}, "case thiếu kind"},
		{spec.CaseSpec{ID: "C2", Kind: spec.KindHappy, Then: then}, "case thiếu when"},
		{spec.CaseSpec{ID: "C3", Kind: spec.KindHappy, When: when}, "case thiếu then"},
		{spec.CaseSpec{ID: "C4", Kind: spec.KindHappy, When: when, Then: &spec.Then{}}, "case thiếu then.status"},
		{spec.CaseSpec{ID: "C5", Kind: spec.KindHappy, When: &spec.When{Path: "/x"}, Then: then}, "case thiếu when.method"},
		{spec.CaseSpec{ID: "C6", Kind: spec.KindHappy, When: &spec.When{Method: " "}, Then: &spec.Then{}}, "case thiếu when.method, when.path, then.status"},
	}
	for _, b := range broken {
		got := r.ex.Execute(context.Background(), b.c)
		if got.ID != b.c.ID || got.Outcome != run.CaseOutcomeError || got.Message != b.want {
			t.Errorf("%s: %+v", b.c.ID, got)
		}
	}
}

func TestExecuteRecoversFromPanic(t *testing.T) {
	r := newRig(t)
	r.ex.DB = nil // nil interface: gọi sẽ panic
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, spec.When{}, spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeError || !strings.HasPrefix(got.Message, "reset_sql: ") {
		t.Errorf("%+v", got)
	}
}

func TestHTTPCallerDefaultsContentTypeAndKeepsExplicit(t *testing.T) {
	r := newRig(t)
	c := r.ex.HTTP.(*HTTPCaller)
	res, err := c.Call(context.Background(), spec.When{Method: " post ", Path: "ct", Body: map[string]any{"q": 1}})
	if err != nil || !strings.Contains(res.Body, `"ct":"application/json"`) || !strings.Contains(res.Body, `"method":"POST"`) {
		t.Errorf("%+v %v", res, err)
	}
	res, _ = c.Call(context.Background(), spec.When{Method: "GET", Path: "/ct", Headers: map[string]string{"content-type": "text/plain"}})
	if !strings.Contains(res.Body, `"ct":"text/plain"`) {
		t.Errorf("%+v", res)
	}
	if (&HTTPCaller{}).Timeout() != 60*time.Second {
		t.Error("default timeout")
	}
}

func TestRepeatRunsSequentiallyGivenOnce(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer srv.Close()
	r := newRig(t)
	r.ex.HTTP = &HTTPCaller{BaseURL: srv.URL}
	g := spec.Given{SQL: []string{"INSERT 1"}}
	got := r.ex.Execute(context.Background(), mk(g, wh("POST", "/hit", 3, 1), spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomePassed || n.Load() != 3 || len(r.db.execs) != 2 {
		t.Errorf("%+v n=%d execs=%v", got, n.Load(), r.db.execs)
	}
}

func TestParallelStatusCounts(t *testing.T) {
	claimed.Store(0)
	r := newRig(t)
	then := spec.Then{StatusCounts: map[int]int{200: 1, 409: 3}, DB: []spec.DbExpect{{Query: "q"}}}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("POST", "/claim", 1, 4), then))
	if got.Outcome != run.CaseOutcomePassed || len(r.db.checks) != 1 {
		t.Errorf("%+v", got)
	}
}

func TestParallelStatusCountsMismatchFails(t *testing.T) {
	claimed.Store(0)
	r := newRig(t)
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("POST", "/claim", 1, 4), spec.Then{StatusCounts: map[int]int{200: 4}}))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "statusCounts: mong đợi {200=4}, nhận {200=1, 409=3}" {
		t.Errorf("%+v", got)
	}
}

func TestParallelWithoutStatusCountsAllMustEqualStatus(t *testing.T) {
	claimed.Store(0)
	r := newRig(t)
	ok := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("POST", "/other", 1, 3), spec.Then{Status: 200, Body: map[string]any{"ignored": true}}))
	if ok.Outcome != run.CaseOutcomePassed {
		t.Errorf("%+v", ok)
	}
	bad := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("POST", "/claim", 1, 3), spec.Then{Status: 200}))
	if bad.Outcome != run.CaseOutcomeFailed || !strings.HasPrefix(bad.Message, "status: mong đợi 200, nhận [") {
		t.Errorf("%+v", bad)
	}
}

func TestParallelTimeoutFailsCaseNotSuite(t *testing.T) {
	r := newRig(t)
	r.ex.HTTP = &HTTPCaller{BaseURL: r.ex.HTTP.(*HTTPCaller).BaseURL, RequestTimeout: 500 * time.Millisecond}
	t0 := time.Now()
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("GET", "/slow", 1, 4), spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeFailed || !strings.HasPrefix(got.Message, "parallel: 4/4 request chưa xong sau ") {
		t.Errorf("%+v", got)
	}
	if time.Since(t0) > 8*time.Second {
		t.Error("too slow")
	}
	next := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("GET", "/fast", 1, 2), spec.Then{Status: 200}))
	if next.Outcome != run.CaseOutcomePassed {
		t.Errorf("%+v", next)
	}
}

func TestParallelHardDeadline(t *testing.T) {
	// Caller treo hơn Timeout()+grace: đếm là chưa xong, báo theo hạn của cả loạt
	old := parallelGrace
	parallelGrace = 100 * time.Millisecond
	defer func() { parallelGrace = old }()
	r := newRig(t)
	r.ex.HTTP = hangCaller{}
	got := r.ex.Execute(context.Background(), mk(spec.Given{}, wh("GET", "/x", 1, 2), spec.Then{Status: 200}))
	if got.Outcome != run.CaseOutcomeFailed || got.Message != "parallel: 2/2 request chưa xong sau 0s" {
		t.Errorf("%+v", got)
	}
}

type hangCaller struct{}

func (hangCaller) Call(ctx context.Context, _ spec.When) (Response, error) {
	<-ctx.Done()
	return Response{}, ctx.Err()
}
func (hangCaller) Timeout() time.Duration { return 0 }
