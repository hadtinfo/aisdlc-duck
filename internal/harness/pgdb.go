package harness

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/spec"
)

// PgDB cài DB trên Postgres thật.
type PgDB struct{ pool *pgxpool.Pool }

// OpenPgDB mở pool tới url.
func OpenPgDB(ctx context.Context, url string) (*PgDB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &PgDB{pool: pool}, nil
}

// Close đóng pool.
func (d *PgDB) Close() { d.pool.Close() }

// Exec chạy sql (có thể nhiều câu lệnh).
func (d *PgDB) Exec(ctx context.Context, sql string) error {
	_, err := d.pool.Exec(ctx, sql)
	return err
}

type valueKind int

const (
	kindText valueKind = iota
	kindNull
	kindNumber // text là số thập phân
	kindTime   // text là RFC 3339 UTC
	kindDate   // text là YYYY-MM-DD
	kindJSON   // json/jsonb (object, mảng) và mảng Postgres: text là JSON gọn
)

type value struct {
	kind valueKind
	text string // dạng in
	out  any    // giá trị trả cho Violations
}

// normValue chuẩn hóa giá trị pgx: nil -> null, thời gian -> RFC 3339 UTC (date: YYYY-MM-DD), uuid -> dạng chuẩn,
// số -> chuỗi thập phân, json/jsonb object/mảng và mảng Postgres (text[], int[]...) -> JSON gọn.
func normValue(oid uint32, v any) value {
	switch x := v.(type) {
	case nil:
		return value{kindNull, "null", nil}
	case int16, int32, int64, int, uint32, uint64:
		s := fmt.Sprint(x)
		return value{kindNumber, s, x}
	case float32:
		s := strconv.FormatFloat(float64(x), 'f', -1, 32)
		return value{kindNumber, s, s}
	case float64:
		s := strconv.FormatFloat(x, 'f', -1, 64)
		return value{kindNumber, s, s}
	case pgtype.Numeric:
		if !x.Valid {
			return value{kindNull, "null", nil}
		}
		dv, err := x.Value()
		s, ok := dv.(string)
		if err != nil || !ok || x.NaN || x.InfinityModifier != 0 {
			return value{kindText, fmt.Sprint(dv), fmt.Sprint(dv)}
		}
		return value{kindNumber, s, s}
	case time.Time:
		if oid == pgtype.DateOID {
			s := x.Format(time.DateOnly)
			return value{kindDate, s, s}
		}
		s := x.UTC().Format(time.RFC3339Nano)
		return value{kindTime, s, s}
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err != nil {
			return value{kindText, fmt.Sprint(x), x}
		}
		return value{kindJSON, string(b), string(b)}
	case [16]byte:
		s := fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
		return value{kindText, s, s}
	case []byte:
		s := string(x)
		return value{kindText, s, s}
	case driver.Valuer:
		dv, err := x.Value()
		if err == nil {
			return normValue(oid, dv)
		}
	}
	return value{kindText, fmt.Sprint(v), v}
}

// scanRows đọc tối đa limit dòng (limit <= 0: không giới hạn); khóa là tên cột chữ thường.
func scanRows(rows pgx.Rows, limit int) ([]map[string]value, error) {
	defer rows.Close()
	fds := rows.FieldDescriptions()
	var out []map[string]value
	for rows.Next() {
		if limit > 0 && len(out) >= limit {
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]value, len(vals))
		for i, v := range vals {
			row[strings.ToLower(fds[i].Name)] = normValue(fds[i].DataTypeOID, v)
		}
		out = append(out, row)
	}
	rows.Close()
	return out, rows.Err()
}

// Check so từng dòng theo thứ tự, chỉ các cột có trong kỳ vọng (tên cột không phân biệt hoa thường).
func (d *PgDB) Check(ctx context.Context, e spec.DbExpect) (string, error) {
	rows, err := d.pool.Query(ctx, e.Query)
	if err != nil {
		return "", err
	}
	actual, err := scanRows(rows, 0)
	if err != nil {
		return "", err
	}
	if len(actual) != len(e.Rows) {
		return fmt.Sprintf("số dòng: mong đợi %d, nhận %d", len(e.Rows), len(actual)), nil
	}
	for i, row := range actual {
		keys := make([]string, 0, len(e.Rows[i]))
		for k := range e.Rows[i] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			got, ok := row[strings.ToLower(k)]
			if !ok {
				return fmt.Sprintf("dòng %d: không có cột %s", i, k), nil
			}
			want := e.Rows[i][k]
			if !matches(want, got) {
				return fmt.Sprintf("dòng %d cột %s: mong đợi %s, nhận %s", i, k, fmtExpected(want), got.text), nil
			}
		}
	}
	return "", nil
}

// fmtExpected in giá trị kỳ vọng: object/mảng YAML in dạng JSON gọn như giá trị thực.
func fmtExpected(want any) string {
	switch want.(type) {
	case map[string]any, []any:
		return render(want)
	}
	return fmtValue(want)
}

// zonelessLayouts: thời điểm kỳ vọng không ghi múi giờ được hiểu là UTC (giá trị in ra luôn là UTC).
var zonelessLayouts = []string{"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"}

// parseExpectedTime: RFC 3339 (có múi giờ, kể cả cách bằng dấu cách) hoặc không múi giờ (UTC).
func parseExpectedTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	for _, l := range zonelessLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// jsonMatches so JSON chặt: kỳ vọng là chuỗi thì phân tích như văn bản JSON (không phải JSON thì so nguyên văn),
// là object/mảng YAML thì so cấu trúc. Khác CompareJSON (khớp lỏng của body HTTP): object phải có đúng các khóa đó,
// mảng đúng độ dài và thứ tự; chỉ số được so theo giá trị (1 = 1.0).
func jsonMatches(want any, got string) bool {
	var w any
	var err error
	if s, ok := want.(string); ok {
		if w, err = decode([]byte(s)); err != nil {
			return s == got
		}
	} else if w, err = normalize(want); err != nil {
		return false
	}
	g, err := decode([]byte(got))
	return err == nil && jsonEqual(w, g)
}

func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if yv, ok := y[k]; !ok || !jsonEqual(v, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return equalScalar(a, b)
}

func matches(want any, got value) bool {
	if want == nil {
		return got.kind == kindNull
	}
	if got.kind == kindJSON {
		return jsonMatches(want, got.text)
	}
	ws := fmt.Sprint(want)
	switch got.kind {
	case kindNull:
		return ws == "null"
	case kindNumber:
		g, ok := new(big.Rat).SetString(got.text)
		if !ok {
			return ws == got.text
		}
		var w *big.Rat
		switch x := want.(type) {
		case string:
			w, _ = new(big.Rat).SetString(strings.TrimSpace(x))
		case bool:
		default:
			w, _ = new(big.Rat).SetString(ws)
			if f, isF := want.(float64); isF {
				w, _ = new(big.Rat).SetString(strconv.FormatFloat(f, 'f', -1, 64))
			}
		}
		return w != nil && w.Cmp(g) == 0
	case kindTime:
		w, ok := parseExpectedTime(ws)
		g, _ := time.Parse(time.RFC3339Nano, got.text)
		if !ok {
			return ws == got.text
		}
		return w.Equal(g)
	}
	return ws == got.text
}

// Violations chạy sql trong giao dịch chỉ đọc luôn rollback, trả tối đa max dòng với giá trị đã chuẩn hóa.
func (d *PgDB) Violations(ctx context.Context, sql string, max int) ([]map[string]any, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	actual, err := scanRows(rows, max)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(actual))
	for i, r := range actual {
		out[i] = make(map[string]any, len(r))
		for k, v := range r {
			out[i][k] = v.out
		}
	}
	return out, nil
}
