package harness

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

// NotConfigured là bản vi của lý do mặc định của NoMessageProbe (để so trong test); lúc chạy, reason lấy bản theo
// locale.
var NotConfigured = i18n.VI.T("chưa cấu hình Kafka")

const maxViolationRows = 5

// parallelGrace là thời gian chờ thêm sau Timeout() trước khi coi loạt request song song là quá hạn.
var parallelGrace = 5 * time.Second

// DB là cơ sở dữ liệu của dịch vụ đích.
type DB interface {
	Exec(ctx context.Context, sql string) error
	Check(ctx context.Context, e spec.DbExpect) (string, error) // "" = khớp
	// Violations chạy sql trong giao dịch chỉ đọc, trả tối đa max dòng.
	Violations(ctx context.Context, sql string, max int) ([]map[string]any, error)
}

// Stubs đăng ký stub cho case (xóa stub cũ trước).
type Stubs interface {
	Register(all []spec.StubSpec, ids []string) error
}

// MessageProbe kiểm tra message đã gửi (ví dụ Kafka).
type MessageProbe interface {
	Reset(ctx context.Context) error
	Check(ctx context.Context, want []spec.MessageExpect) (string, error)
}

// NoMessageProbe dùng khi chưa cấu hình Kafka: case có kiểm message thành ERROR với Reason.
type NoMessageProbe struct{ Reason string }

func (n NoMessageProbe) reason() string {
	if n.Reason == "" {
		return i18n.T("chưa cấu hình Kafka")
	}
	return n.Reason
}

// Reset không làm gì.
func (NoMessageProbe) Reset(context.Context) error { return nil }

// Check lỗi nếu có kỳ vọng message.
func (n NoMessageProbe) Check(_ context.Context, want []spec.MessageExpect) (string, error) {
	if len(want) == 0 {
		return "", nil
	}
	return "", errors.New(n.reason())
}

// Executor chạy từng case: reset_sql, given.sql, given.stubs, reset messages, gọi HTTP, kiểm tra, bất biến.
// Lệch đầu tiên là FAILED; lỗi khi chuẩn bị hoặc khi chạy là ERROR.
type Executor struct {
	HTTP     Caller
	DB       DB
	Stubs    Stubs
	Messages MessageProbe
	ResetSQL []string
	AllStubs []spec.StubSpec
	// Checks là bất biến (design.yaml hoặc cases.yaml) và sql-zero của risk.yaml; mỗi check chỉ chạy cho case mà
	// Invariant.AppliesTo theo StepOrder.
	Checks    []spec.Invariant
	StepOrder []string
}

// Execute chạy một case, không panic.
func (e *Executor) Execute(ctx context.Context, c spec.CaseSpec) (res run.CaseResult) {
	phase := i18n.T("chuẩn bị")
	defer func() {
		if r := recover(); r != nil {
			res = errResult(c, fmt.Sprintf("%s: %v", phase, r))
		}
	}()
	if m := incomplete(c); m != "" {
		return errResult(c, m)
	}
	if len(c.Then.Messages) > 0 {
		switch n := e.Messages.(type) {
		case NoMessageProbe:
			return errResult(c, n.reason())
		case *NoMessageProbe:
			return errResult(c, n.reason())
		}
	}
	fail := func(err error) run.CaseResult { return errResult(c, phase+": "+err.Error()) }

	phase = "reset_sql"
	for _, q := range e.ResetSQL {
		if err := e.DB.Exec(ctx, q); err != nil {
			return fail(err)
		}
	}
	phase = "given.sql"
	for _, q := range c.Given.SQL {
		if err := e.DB.Exec(ctx, q); err != nil {
			return fail(err)
		}
	}
	phase = "given.stubs"
	if err := e.Stubs.Register(e.AllStubs, c.Given.Stubs); err != nil {
		return fail(err)
	}
	phase = "reset messages"
	if e.Messages != nil {
		if err := e.Messages.Reset(ctx); err != nil {
			return fail(err)
		}
	}
	phase = i18n.T("gọi HTTP")
	var failure string
	var err error
	if c.When.Parallel > 1 {
		var b batch
		for i := 0; i < c.When.Repeat; i++ {
			if b, err = e.callParallel(ctx, *c.When); err != nil {
				return fail(err)
			}
		}
		phase = i18n.T("kiểm tra")
		if b.unfinished != "" {
			failure = b.unfinished
		} else if failure, err = e.compareParallel(ctx, *c.Then, b.statuses); err != nil {
			return fail(err)
		}
	} else {
		var r Response
		for i := 0; i < c.When.Repeat; i++ {
			if r, err = e.HTTP.Call(ctx, *c.When); err != nil {
				return fail(err)
			}
		}
		phase = i18n.T("kiểm tra")
		if failure, err = e.compare(ctx, *c.Then, r); err != nil {
			return fail(err)
		}
	}
	if failure == "" {
		phase = i18n.T("bất biến")
		failure = e.checkInvariants(ctx, c.Step)
	}
	if failure != "" {
		return run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomeFailed, Message: failure}
	}
	return run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomePassed}
}

// batch là kết quả một loạt request song song; unfinished khác rỗng khi có request không nhận được phản hồi.
type batch struct {
	statuses   []int
	unfinished string
}

type callOutcome struct {
	i    int
	resp Response
	err  error
}

// callParallel: n goroutine chờ chung một kênh rồi cùng gửi; chờ cả loạt tối đa Timeout()+5s.
func (e *Executor) callParallel(ctx context.Context, w spec.When) (batch, error) {
	n := w.Parallel
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := make(chan struct{})
	out := make(chan callOutcome, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer func() {
				if r := recover(); r != nil {
					out <- callOutcome{i: i, err: fmt.Errorf("%v", r)}
				}
			}()
			<-start
			r, err := e.HTTP.Call(ctx, w)
			out <- callOutcome{i, r, err}
		}(i)
	}
	close(start)
	limit := e.HTTP.Timeout() + parallelGrace
	timer := time.NewTimer(limit)
	defer timer.Stop()
	got := make([]*int, n)
	finished, unfinished := 0, 0
	overdue := false
wait:
	for finished < n {
		select {
		case o := <-out:
			finished++
			if o.err == nil {
				st := o.resp.Status
				got[o.i] = &st
			} else if isTimeout(o.err) {
				unfinished++
			} else {
				return batch{}, o.err
			}
		case <-timer.C:
			overdue = true
			break wait
		case <-ctx.Done():
			return batch{}, ctx.Err()
		}
	}
	unfinished += n - finished
	var statuses []int
	for _, s := range got {
		if s != nil {
			statuses = append(statuses, *s)
		}
	}
	if unfinished == 0 {
		return batch{statuses: statuses}, nil
	}
	after := e.HTTP.Timeout()
	if overdue {
		after = limit
	}
	return batch{statuses, i18n.Tf("parallel: %d/%d request chưa xong sau %ds", unfinished, n, int64(after.Seconds()))}, nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// compareParallel: statusCounts chỉ đếm (không theo thứ tự); không có thì mọi status phải bằng then.status.
func (e *Executor) compareParallel(ctx context.Context, then spec.Then, statuses []int) (string, error) {
	if len(then.StatusCounts) > 0 {
		got := map[int]int{}
		for _, s := range statuses {
			got[s]++
		}
		if !equalCounts(then.StatusCounts, got) {
			return i18n.Tf("statusCounts: mong đợi %s, nhận %s", fmtCounts(then.StatusCounts), fmtCounts(got)), nil
		}
	} else {
		for _, s := range statuses {
			if s != then.Status {
				return i18n.Tf("status: mong đợi %d, nhận %s", then.Status, fmtInts(statuses)), nil
			}
		}
	}
	return e.compareDBAndMessages(ctx, then)
}

func equalCounts(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// fmtCounts in như TreeMap.toString của Java: {200=1, 409=1}.
func fmtCounts(m map[int]int) string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%d=%d", k, m[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func fmtInts(v []int) string {
	parts := make([]string, len(v))
	for i, s := range v {
		parts[i] = fmt.Sprint(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (e *Executor) compare(ctx context.Context, then spec.Then, r Response) (string, error) {
	if then.Status != r.Status {
		return i18n.Tf("status: mong đợi %d, nhận %d", then.Status, r.Status), nil
	}
	if then.Body != nil {
		if m := CompareJSON(then.Body, r.Body); m != "" {
			return "body: " + m, nil
		}
	}
	return e.compareDBAndMessages(ctx, then)
}

func (e *Executor) compareDBAndMessages(ctx context.Context, then spec.Then) (string, error) {
	for i, d := range then.DB {
		m, err := e.DB.Check(ctx, d)
		if err != nil {
			return "", err
		}
		if m != "" {
			return fmt.Sprintf("db[%d]: %s", i, m), nil
		}
	}
	if e.Messages == nil {
		return "", nil
	}
	m, err := e.Messages.Check(ctx, then.Messages)
	if err != nil {
		return "", err
	}
	if m != "" {
		return "messages: " + m, nil
	}
	return "", nil
}

// checkInvariants: có dòng vi phạm hoặc truy vấn lỗi (kể cả cố ghi trên kết nối chỉ đọc) thì case FAILED.
func (e *Executor) checkInvariants(ctx context.Context, caseStep string) string {
	for _, inv := range e.Checks {
		if !inv.AppliesTo(caseStep, e.StepOrder) {
			continue
		}
		rows, err := e.DB.Violations(ctx, inv.SQL, maxViolationRows)
		if err != nil {
			return i18n.Tf("Bất biến '%s' lỗi: %s", inv.Name, err.Error())
		}
		if len(rows) > 0 {
			return i18n.Tf("Bất biến '%s' vi phạm: %s", inv.Name, fmtRows(rows))
		}
	}
	return ""
}

// fmtRows in như List<Map>.toString của Java: [{id=2}, {id=3}], khóa tăng dần.
func fmtRows(rows []map[string]any) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kv := make([]string, len(keys))
		for j, k := range keys {
			kv[j] = k + "=" + fmtValue(r[k])
		}
		parts[i] = "{" + strings.Join(kv, ", ") + "}"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// incomplete: CasesValidator không bắt các trường này; thiếu thì báo ERROR thay vì panic.
func incomplete(c spec.CaseSpec) string {
	var missing []string
	if c.Kind == "" {
		missing = append(missing, "kind")
	}
	if c.When == nil {
		missing = append(missing, "when")
	} else {
		if strings.TrimSpace(c.When.Method) == "" {
			missing = append(missing, "when.method")
		}
		if strings.TrimSpace(c.When.Path) == "" {
			missing = append(missing, "when.path")
		}
	}
	if c.Then == nil {
		missing = append(missing, "then")
	} else if c.Then.Status <= 0 && !(c.When != nil && c.When.Parallel > 1 && len(c.Then.StatusCounts) > 0) {
		missing = append(missing, "then.status")
	}
	if len(missing) == 0 {
		return ""
	}
	return i18n.Tf("case thiếu %s", strings.Join(missing, ", "))
}

func errResult(c spec.CaseSpec, msg string) run.CaseResult {
	return run.CaseResult{ID: c.ID, Step: c.Step, Outcome: run.CaseOutcomeError, Message: msg}
}

// fmtValue in giá trị như Java String.valueOf: nil là null.
func fmtValue(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprint(v)
}
