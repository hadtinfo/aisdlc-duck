package ingest

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxSessionID = 100

var eventTypes = map[string]bool{"COMMAND": true, "FILE_EDIT": true, "SESSION_START": true, "SESSION_STOP": true,
	"INSTRUCTION_DELIVERED": true, "ACTION_RESULT": true, "SCOPE_DENIED": true, "RISK_CLASSIFIED": true, "RISK_OVERRIDDEN": true}

type eventRow struct {
	feature, step, path, detail, sessionID *string
	typ                                    string
	at                                     time.Time
	outOfScope                             bool
}

type effect struct {
	feature *string
	typ     string
	at      time.Time
	detail  *string
}

// requireVersion: lô v1 (không có version) hoặc v2; khác thì 400.
func requireVersion(v any) error {
	if isMissing(v) || v == nil {
		return nil
	}
	if i, ok := isInt(v); !ok || (i != 1 && i != 2) {
		return badRequest("version phải là 1 hoặc 2")
	}
	return nil
}

func sessionID(e any) (*string, error) {
	n := path(e, "sessionId")
	if isMissing(n) || n == nil {
		return nil, nil
	}
	if s, ok := n.(string); ok && utf16Len(s) <= maxSessionID {
		return &s, nil
	}
	return nil, badRequest("sessionId phải là chuỗi tối đa " + strconv.Itoa(maxSessionID) + " ký tự")
}

// AppendEvents ghi một lô sự kiện (EventStore.append). Lô sai thì trả lỗi 400 và không ghi gì; trả số sự kiện đã ghi.
func (s *Service) AppendEvents(ctx context.Context, login string, batch any) (int, error) {
	if err := requireText(path(batch, "repoId"), "repoId"); err != nil {
		return 0, err
	}
	events, ok := path(batch, "events").([]any)
	if !ok {
		return 0, badRequest("Thiếu events")
	}
	if err := requireVersion(path(batch, "version")); err != nil {
		return 0, err
	}
	repoID := path(batch, "repoId").(string)

	var rows []eventRow
	var effects []effect
	var features []string
	seen := map[string]bool{}
	for _, e := range events {
		typ := asText(path(e, "type"))
		if !eventTypes[typ] {
			return 0, badRequest("type không hợp lệ: " + typ)
		}
		if err := requireText(path(e, "at"), "events[].at"); err != nil {
			return 0, err
		}
		at, err := parseInstant(path(e, "at").(string))
		if err != nil {
			return 0, err
		}
		feature := text(e, "feature")
		if feature != nil && !seen[*feature] {
			seen[*feature] = true
			features = append(features, *feature)
		}
		sid, err := sessionID(e)
		if err != nil {
			return 0, err
		}
		r := eventRow{feature: feature, typ: typ, at: at, step: text(e, "step"), path: text(e, "path"),
			outOfScope: asBoolean(path(e, "outOfScope")), detail: text(e, "detail"), sessionID: sid}
		rows = append(rows, r)
		if typ == "INSTRUCTION_DELIVERED" || typ == "ACTION_RESULT" || strings.HasPrefix(typ, "RISK_") {
			effects = append(effects, effect{feature, typ, at, r.detail})
		}
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	if _, err := tx.Exec(ctx, "INSERT INTO repos(id, last_seen_at) VALUES ($1,$2) ON CONFLICT (id) DO UPDATE SET last_seen_at = $2",
		repoID, now); err != nil {
		return 0, err
	}
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue("INSERT INTO events(repo_id, feature_key, type, at, step, path, out_of_scope, sender_login, detail, session_id, received_at) "+
			"VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", repoID, r.feature, r.typ, r.at, r.step, r.path, r.outOfScope, login, r.detail, r.sessionID, now)
	}
	br := tx.SendBatch(ctx, b)
	for range rows {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	// Khóa mọi tính năng có hiệu ứng RISK_* theo thứ tự (repo, feature) trước khi áp dụng: hai lô chạm các tính năng theo
	// thứ tự ngược nhau không thể chờ vòng tròn (deadlock). lockFeature trong applyEffect sau đó là no-op (đã giữ khóa).
	var riskFeatures []string
	for _, f := range effects {
		if f.feature != nil && f.detail != nil && strings.HasPrefix(f.typ, "RISK_") {
			riskFeatures = append(riskFeatures, *f.feature)
		}
	}
	sort.Strings(riskFeatures)
	for i, ft := range riskFeatures {
		if i > 0 && ft == riskFeatures[i-1] {
			continue
		}
		if err := lockFeature(ctx, tx, repoID, ft); err != nil {
			return 0, err
		}
	}
	for _, f := range effects {
		if err := s.applyEffect(ctx, tx, repoID, f); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	// Chỉ phát cho sự kiện gắn tính năng.
	for _, f := range features {
		s.changed(repoID, f)
	}
	return len(rows), nil
}

// asLong như canConvertToLong()+isNumber()+asLong(): số nguyên vừa int64, hoặc số thực trong miền int64 (cắt phần lẻ).
func asLong(n any) (int64, bool) {
	num, ok := n.(json.Number)
	if !ok {
		return 0, false
	}
	if v, err := strconv.ParseInt(string(num), 10, 64); err == nil {
		return v, true
	}
	f, err := strconv.ParseFloat(string(num), 64)
	if err != nil || math.IsNaN(f) || f < -9.223372036854775808e18 || f >= 9.223372036854775808e18 {
		return 0, false
	}
	if strings.ContainsAny(string(num), ".eE") {
		return int64(f), true
	}
	return 0, false // số nguyên quá lớn
}

// applyEffect: hiệu ứng lên bảng điều khiển; detail hỏng thì bỏ qua (sự kiện vẫn lưu).
func (s *Service) applyEffect(ctx context.Context, tx pgx.Tx, repo string, f effect) error {
	if f.feature == nil || f.detail == nil {
		return nil
	}
	feature, detail := *f.feature, *f.detail
	switch {
	case f.typ == "INSTRUCTION_DELIVERED":
		id, err := strconv.ParseInt(strings.TrimFunc(detail, func(r rune) bool { return r <= ' ' }), 10, 64)
		if err != nil {
			return nil
		}
		_, err = tx.Exec(ctx, "UPDATE instructions SET delivered_at = $1 WHERE id = $2 AND repo_id = $3 AND feature_key = $4 "+
			"AND delivered_at IS NULL", f.at, id, repo, feature)
		return err
	case f.typ == "ACTION_RESULT":
		d, _, err := parseTree([]byte(detail))
		if err != nil {
			return nil
		}
		status := asText(path(d, "status"))
		id, ok := asLong(path(d, "id"))
		if !ok || (status != "APPLIED" && status != "STALE") {
			return nil
		}
		var reason *string
		if r, ok := path(d, "reason").(string); ok {
			reason = &r
		}
		// APPLIED thắng: một bản clone khác báo STALE trước thì máy thật báo APPLIED vẫn ghi đè; STALE không ghi đè APPLIED
		from := "status = 'PENDING'"
		if status == "APPLIED" {
			from = "status IN ('PENDING', 'STALE')"
		}
		_, err = tx.Exec(ctx, "UPDATE pending_actions SET status = $1, reason = $2, resolved_at = $3 WHERE id = $4 AND repo_id = $5 "+
			"AND feature_key = $6 AND "+from, status, reason, f.at, id, repo, feature)
		return err
	case strings.HasPrefix(f.typ, "RISK_"):
		d, _, err := parseTree([]byte(detail))
		if err != nil {
			return nil
		}
		// cùng khóa với nộp/duyệt của tính năng: thay thế không xen giữa một lần nộp và tự duyệt
		if err := lockFeature(ctx, tx, repo, feature); err != nil {
			return err
		}
		var before *string // risk_level có thể NULL
		err = tx.QueryRow(ctx, "SELECT risk_level FROM features WHERE repo_id = $1 AND feature_key = $2", repo, feature).Scan(&before)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err := applyRisk(ctx, tx, repo, feature, f.typ, d); err != nil {
			return err
		}
		var to *string
		if f.typ == "RISK_CLASSIFIED" {
			to = text(d, "level")
		} else {
			to = text(d, "to")
		}
		if before == nil && f.typ == "RISK_OVERRIDDEN" {
			before = text(d, "from")
		}
		// mức mới đặt cổng về duyệt tay: thay thế bản tự duyệt (cổng về chờ dev duyệt)
		if s.RiskChanged != nil && to != nil && !isBlank(*to) {
			from := ""
			if before != nil {
				from = *before
			}
			return s.RiskChanged(ctx, tx, repo, feature, from, *to)
		}
	}
	return nil
}

// applyRisk cập nhật cột rủi ro của tính năng (không làm gì nếu hàng chưa có hoặc detail thiếu trường).
func applyRisk(ctx context.Context, tx pgx.Tx, repo, feature, typ string, d any) error {
	switch typ {
	case "RISK_CLASSIFIED":
		lv, ok := path(d, "level").(string)
		if !ok || isBlank(lv) {
			return nil
		}
		var v *int64 // chế độ file: không có phiên bản web
		if pv, ok := isInt(path(d, "policyVersion")); ok && pv > 0 {
			v = &pv
		}
		_, err := tx.Exec(ctx, "UPDATE features SET risk_level = $1, risk_policy_version = $2 WHERE repo_id = $3 AND feature_key = $4",
			lv, v, repo, feature)
		return err
	case "RISK_OVERRIDDEN":
		to, ok := path(d, "to").(string)
		if !ok || isBlank(to) {
			return nil
		}
		_, err := tx.Exec(ctx, "UPDATE features SET risk_level = $1 WHERE repo_id = $2 AND feature_key = $3", to, repo, feature)
		return err
	}
	return nil
}
