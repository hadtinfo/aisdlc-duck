package harness

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

func openDB(t *testing.T, suffix string) *PgDB {
	t.Helper()
	url := freshDB(t)
	db, err := OpenPgDB(context.Background(), url+suffix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestDbCheckNormalization(t *testing.T) {
	db := openDB(t, "&TimeZone=Asia/Ho_Chi_Minh")
	ctx := context.Background()
	if err := db.Exec(ctx, `CREATE TABLE n (amount numeric(10,2), cnt bigint, ok boolean, nothing text, ts timestamptz,
		d date, u uuid, t text, FROM_ACCOUNT text, f float8);
		INSERT INTO n VALUES (100.00, 5, true, NULL, '2026-01-02 10:04:05+07', '2026-01-02',
		'123e4567-e89b-12d3-a456-426614174000', 'xin chào', 'acc-1', 1.5)`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		col  string
		want any
		ok   bool
	}{
		{"numeric yaml int", "amount", 100, true},
		{"numeric yaml float", "amount", 100.0, true},
		{"numeric string", "amount", "100.00", true},
		{"numeric string short", "amount", "100", true},
		{"numeric khác", "amount", "100.01", false},
		{"numeric không phải số", "amount", "abc", false},
		{"bigint", "cnt", 5, true},
		{"bigint string", "cnt", "5", true},
		{"bigint khác", "cnt", 6, false},
		{"float", "f", 1.5, true},
		{"bool", "ok", true, true},
		{"bool sai", "ok", false, false},
		{"null", "nothing", nil, true},
		{"null chuỗi", "nothing", "null", true},
		{"null vs giá trị", "nothing", "x", false},
		{"timestamptz UTC", "ts", "2026-01-02T03:04:05Z", true},
		{"timestamptz cùng thời điểm khác múi", "ts", "2026-01-02T10:04:05+07:00", true},
		{"timestamptz khác", "ts", "2026-01-02T03:04:06Z", false},
		{"date", "d", "2026-01-02", true},
		{"date khác", "d", "2026-01-03", false},
		{"uuid", "u", "123e4567-e89b-12d3-a456-426614174000", true},
		{"text", "t", "xin chào", true},
		{"cột hoa thường", "from_account", "acc-1", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := db.Check(ctx, spec.DbExpect{Query: "SELECT " + tc.col + " FROM n", Rows: []map[string]any{{tc.col: tc.want}}})
			if err != nil {
				t.Fatal(err)
			}
			if (msg == "") != tc.ok {
				t.Errorf("msg=%q ok=%v", msg, tc.ok)
			}
		})
	}
	// cột kỳ vọng viết HOA, cột truy vấn viết thường
	msg, err := db.Check(ctx, spec.DbExpect{Query: "SELECT from_account FROM n", Rows: []map[string]any{{"FROM_ACCOUNT": "acc-1"}}})
	if err != nil || msg != "" {
		t.Errorf("msg=%q err=%v", msg, err)
	}
}

func TestDbCheckMessages(t *testing.T) {
	db := openDB(t, "")
	ctx := context.Background()
	if err := db.Exec(ctx, "CREATE TABLE m (id int, name text); INSERT INTO m VALUES (1,'a'),(2,'b')"); err != nil {
		t.Fatal(err)
	}
	q := "SELECT id, name FROM m ORDER BY id"
	tests := []struct {
		rows []map[string]any
		want string
	}{
		{[]map[string]any{{"id": 1}}, "số dòng: mong đợi 1, nhận 2"},
		{[]map[string]any{{"id": 1}, {"zzz": 1}}, "dòng 1: không có cột zzz"},
		{[]map[string]any{{"id": 1}, {"name": "x"}}, "dòng 1 cột name: mong đợi x, nhận b"},
		{[]map[string]any{{"id": 1}, {"id": nil}}, "dòng 1 cột id: mong đợi null, nhận 2"},
		{[]map[string]any{{"id": 1, "name": "a"}, {"id": 2}}, ""},
	}
	for _, tc := range tests {
		got, err := db.Check(ctx, spec.DbExpect{Query: q, Rows: tc.rows})
		if err != nil || got != tc.want {
			t.Errorf("got %q err=%v, want %q", got, err, tc.want)
		}
	}
	if _, err := db.Check(ctx, spec.DbExpect{Query: "SELECT * FROM khong_co"}); err == nil {
		t.Error("truy vấn lỗi phải trả error")
	}
}

func TestViolationsReadOnly(t *testing.T) {
	db := openDB(t, "")
	ctx := context.Background()
	if err := db.Exec(ctx, "CREATE TABLE t (id int, amount int); INSERT INTO t VALUES (1, 7)"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Violations(ctx, "SELECT id FROM t", 5)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], map[string]any{"id": int32(1)}) {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	for _, q := range []string{
		"WITH x AS (DELETE FROM t RETURNING id) SELECT * FROM x",
		"DELETE FROM t",
	} {
		if _, err := db.Violations(ctx, q, 5); err == nil {
			t.Errorf("%q phải lỗi", q)
		}
	}
	msg, err := db.Check(ctx, spec.DbExpect{Query: "SELECT count(*) AS c FROM t", Rows: []map[string]any{{"c": 1}}})
	if err != nil || msg != "" {
		t.Errorf("dữ liệu đổi: %q %v", msg, err)
	}
	// kết nối trong pool không còn ở chế độ chỉ đọc
	if err := db.Exec(ctx, "INSERT INTO t VALUES (2, 8)"); err != nil {
		t.Errorf("ghi sau Violations: %v", err)
	}
}

func TestViolationsLimit(t *testing.T) {
	db := openDB(t, "")
	ctx := context.Background()
	if err := db.Exec(ctx, "CREATE TABLE v (id int); INSERT INTO v SELECT g FROM generate_series(1, 12) g"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Violations(ctx, "SELECT id FROM v ORDER BY id", 5)
	if err != nil || len(rows) != 5 || rows[4]["id"] != int32(5) {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func TestViolationsNormalizedValues(t *testing.T) {
	db := openDB(t, "&TimeZone=Asia/Ho_Chi_Minh")
	ctx := context.Background()
	rows, err := db.Violations(ctx, `SELECT 100.00::numeric(10,2) AS amount, NULL::text AS nothing,
		TIMESTAMPTZ '2026-01-02 10:04:05+07' AS ts, DATE '2026-01-02' AS d,
		'123e4567-e89b-12d3-a456-426614174000'::uuid AS u, 'x'::text AS t, true AS b`, 5)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	want := map[string]any{"amount": "100.00", "nothing": nil, "ts": "2026-01-02T03:04:05Z", "d": "2026-01-02",
		"u": "123e4567-e89b-12d3-a456-426614174000", "t": "x", "b": true}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("got %#v", rows[0])
	}
	if got := fmtRows(rows[:1]); !strings.Contains(got, "nothing=null") || strings.Contains(got, "<nil>") {
		t.Errorf("fmtRows=%s", got)
	}
}

// Ports của CaseExecutorRiskTest.invariant_writeAttempt_failsAndRollsBack trên Postgres thật.
func TestExecutorInvariantWriteAttemptFailsAndRollsBack(t *testing.T) {
	db := openDB(t, "")
	ctx := context.Background()
	if err := db.Exec(ctx, "CREATE TABLE t (id int, amount int)"); err != nil {
		t.Fatal(err)
	}
	r := pgRig(t, db, []spec.Invariant{{Name: "ghi-len", SQL: "WITH x AS (UPDATE t SET amount=0 RETURNING id) SELECT * FROM x"}})
	got := r.ex.Execute(ctx, noopCase([]string{"INSERT INTO t VALUES (1, 7)"}))
	if got.Outcome != run.CaseOutcomeFailed || !strings.Contains(got.Message, "ghi-len") {
		t.Fatalf("%+v", got)
	}
	msg, err := db.Check(ctx, spec.DbExpect{Query: "SELECT amount FROM t WHERE id = 1", Rows: []map[string]any{{"amount": 7}}})
	if err != nil || msg != "" {
		t.Errorf("amount đổi: %q %v", msg, err)
	}
}

// Bất biến chạm trần 5 dòng và in null cho NULL, từ đầu đến cuối với Postgres thật.
func TestExecutorInvariantCapsRowsAndPrintsNull(t *testing.T) {
	db := openDB(t, "")
	ctx := context.Background()
	if err := db.Exec(ctx, "CREATE TABLE bad (id int, note text)"); err != nil {
		t.Fatal(err)
	}
	r := pgRig(t, db, []spec.Invariant{{Name: "bad-rows", SQL: "SELECT id, note FROM bad ORDER BY id"}})
	got := r.ex.Execute(ctx, noopCase([]string{"INSERT INTO bad SELECT g, NULL FROM generate_series(1, 9) g"}))
	want := "Bất biến 'bad-rows' vi phạm: [{id=1, note=null}, {id=2, note=null}, {id=3, note=null}, {id=4, note=null}, {id=5, note=null}]"
	if got.Outcome != run.CaseOutcomeFailed || got.Message != want {
		t.Errorf("%+v", got)
	}
}

func pgRig(t *testing.T, db *PgDB, checks []spec.Invariant) *rig {
	r := newRig(t)
	r.ex.DB = db
	r.ex.ResetSQL = nil
	r.ex.Checks = checks
	return r
}

func noopCase(sql []string) spec.CaseSpec {
	c := invCase("S1")
	c.Given.SQL = sql
	return c
}
