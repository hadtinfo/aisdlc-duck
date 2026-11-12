package gates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/board"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/javaish"
	"aisdlc/internal/web/review"
	"aisdlc/internal/web/signing"
	"aisdlc/internal/web/store"
)

// Câu lỗi nghiệp vụ (ApprovalService.java), dịch theo locale của request khi ghi ra.
var (
	MsgNotAllowed      = httpx.T("Chỉ chủ tính năng hoặc lead được duyệt")
	MsgPrevious        = httpx.T("Cổng trước chưa được duyệt")
	MsgNotSubmitted    = httpx.T("Cổng chưa được nộp")
	MsgChanged         = httpx.T("Nội dung đã đổi kể từ lúc nộp, cần nộp lại")
	MsgFindings        = httpx.T("Bản nộp còn phát hiện")
	MsgEvalFail        = httpx.T("Eval chưa ĐẠT")
	MsgEvalTree        = httpx.T("Eval không khớp code hiện tại, chạy lại aisdlc eval")
	MsgSteps           = httpx.T("Còn bước chưa accept")
	MsgNotWeb          = httpx.T("Repo không ở chế độ duyệt trên web")
	MsgConfirmPrevious = httpx.T("Xác nhận cổng trước trước")
)

// Giới hạn flags / assumptionsSeen lưu kèm approval và độ dài ghi chú trả lại/mở lại.
const (
	MaxItems   = 50
	MaxChars   = 500
	MaxComment = 2000
)

// Service là duyệt / trả lại / mở lại / xác nhận bản nhập và nộp từ CLI (ApprovalService + phần nộp/đọc của
// CliGateController). Mọi thao tác ghi chạy trong một tx có khóa theo tính năng; vi phạm luật trả *httpx.Error
// (409 nghiệp vụ, 403 quyền, 400 dữ liệu, 404 không có tính năng). Service cũng là board.GateView (lớp phủ cho board).
type Service struct {
	Pool  *pgxpool.Pool
	Keys  *signing.Keys
	Modes *ModeService
	Auth  *auth.Service // audit (auth.WriteAudit trong tx)
	// Hub được báo sau mỗi thao tác làm đổi trạng thái cổng của một tính năng (nil-safe).
	Hub interface{ Publish(repoID, feature string) }
	// Now phải tăng đơn điệu (giờ UTC thật trong production): bản nộp chỉ "còn hiệu lực" khi submitted_at lớn hơn
	// feedback mới nhất của cổng, nên trả lại rồi nộp lại cần thời điểm sau lớn hơn hẳn thời điểm trước. Đồng hồ đứng
	// yên hoặc lùi làm bản nộp lại bị coi như đã bị trả lại.
	Now func() time.Time
	// AfterSubmit chạy trong tx nộp, sau khi đã lưu submission (server nối Service.AutoApprove). nil = không làm gì.
	// Mọi truy vấn của nó phải đi qua tx (không dùng Pool): tx đang giữ một kết nối và khóa của tính năng.
	AfterSubmit func(ctx context.Context, tx pgx.Tx, s Submitted) error
}

// Submitted là một lần nộp vừa lưu (đầu vào của AfterSubmit).
type Submitted struct {
	Repo, Feature string
	Gate          gate.Gate
	SubmissionID  int64
	Hash          string
	Clean         bool      // không có finding
	RiskLevel     *string   // risk.level CLI gửi kèm, nil nếu không có
	At            time.Time // thời điểm nộp (cùng thời điểm cho bản tự duyệt, như now() của tx trong Java)
}

var _ board.GateView = (*Service)(nil)

// now: UTC, cắt tới micro giây (độ chính xác của timestamptz) để thứ tự so trong DB khớp với giá trị đã ghi.
func (s *Service) now() time.Time {
	t := time.Now()
	if s.Now != nil {
		t = s.Now()
	}
	return t.UTC().Truncate(time.Microsecond)
}

func (s *Service) publish(repo, feature string) {
	if s.Hub != nil {
		s.Hub.Publish(repo, feature)
	}
}

// inTx chạy fn trong một tx đã giữ khóa của tính năng; now lấy sau khi có khóa (thứ tự thời gian theo thứ tự khóa).
func (s *Service) inTx(ctx context.Context, repo, feature string, fn func(tx pgx.Tx, now time.Time) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := store.LockFeature(ctx, tx, repo, feature); err != nil {
			return err
		}
		return fn(tx, s.now())
	})
}

func modeOf(ctx context.Context, d db, repo string) (string, error) {
	md, err := find(ctx, d, repo)
	if err != nil || md == nil {
		return ModeLocal, err
	}
	return md.Mode, nil
}

// GateState là trạng thái cổng ký gửi CLI (phong bì /api/cli/gates, CI) theo chế độ hiện tại của repo.
func (s *Service) GateState(ctx context.Context, repoID, feature string) (snapshot.FeatureGateState, error) {
	mode, err := modeOf(ctx, s.Pool, repoID)
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	return state(ctx, s.Pool, repoID, feature, mode, s.now())
}

// sign ký trạng thái cổng thành phong bì cho CLI; lỗi theo locale của request.
func (s *Service) sign(ctx context.Context, st snapshot.FeatureGateState) (snapshot.SignedEnvelope, error) {
	b, err := st.ToJSONIn(httpx.LocaleFrom(ctx))
	if err != nil {
		return snapshot.SignedEnvelope{}, err
	}
	return s.Keys.Sign(b), nil
}

// ---- tính năng: snapshot mới nhất, luồng, chủ

type featureRow struct {
	tree        any
	firstSender string
}

func readFeature(ctx context.Context, d db, repo, feature string) (*featureRow, error) {
	var raw, first string
	err := d.QueryRow(ctx, "SELECT snapshot::text, first_sender_login FROM features WHERE repo_id = $1 AND feature_key = $2",
		repo, feature).Scan(&raw, &first)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tree, err := board.ParseTree([]byte(raw))
	if err != nil {
		return nil, err
	}
	return &featureRow{tree: tree, firstSender: first}, nil
}

func field(n any, names ...string) (any, bool) {
	for _, name := range names {
		m, ok := n.(map[string]any)
		if !ok {
			return nil, false
		}
		if n, ok = m[name]; !ok {
			return nil, false
		}
	}
	return n, true
}

func textField(n any, names ...string) (string, bool) {
	v, ok := field(n, names...)
	s, isStr := v.(string)
	return s, ok && isStr
}

// elements như vòng for trên JsonNode: phần tử mảng hoặc giá trị object.
func elements(n any) []any {
	switch v := n.(type) {
	case []any:
		return v
	case map[string]any:
		out := make([]any, 0, len(v))
		for _, e := range v {
			out = append(out, e)
		}
		return out
	}
	return nil
}

// sequence: thứ tự cổng theo luồng của snapshot (LITE hoặc FULL; không có snapshot → FULL).
func sequence(tree any) []gate.Gate {
	if v, _ := field(tree, "feature", "flow"); asText(v, true) == string(model.FlowLite) {
		return gate.Sequence(model.FlowLite)
	}
	return gate.Sequence(model.FlowFull)
}

func indexOf(seq []gate.Gate, g gate.Gate) int {
	for i, x := range seq {
		if x == g {
			return i
		}
	}
	return -1
}

// currentHash là currentHash của cổng trong snapshot; nil nếu không có hoặc không phải chuỗi.
func currentHash(tree any, g gate.Gate) *string {
	gs, _ := field(tree, "gates")
	for _, n := range elements(gs) {
		m, _ := n.(map[string]any)
		if m == nil || asText(m["gate"], hasKey(m, "gate")) != string(g) {
			continue
		}
		if h, ok := m["currentHash"].(string); ok {
			return &h
		}
		return nil
	}
	return nil
}

type authorized struct {
	tree  any
	seq   []gate.Gate
	owner *string
}

func notInFlow(g gate.Gate) error {
	return bad(httpx.Tf("Cổng %s không thuộc luồng của tính năng", g))
}

// authorize: chế độ web, tính năng tồn tại, (leadAllowed) owner hoặc lead, cổng thuộc luồng.
func (s *Service) authorize(ctx context.Context, tx pgx.Tx, p httpx.Principal, repo, feature string, g gate.Gate,
	leadAllowed bool) (*authorized, error) {
	mode, err := modeOf(ctx, tx, repo)
	if err != nil {
		return nil, err
	}
	if mode != ModeWeb {
		return nil, errNotWeb()
	}
	row, err := readFeature(ctx, tx, repo, feature)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, httpx.Err(http.StatusNotFound, httpx.T("Không tìm thấy tính năng"))
	}
	emails, err := board.EmailToLogin(ctx, tx) // trên tx: không lấy thêm kết nối pool khi đang giữ khóa
	if err != nil {
		return nil, err
	}
	first := row.firstSender
	owner := board.Owner(row.tree, emails, &first)
	if leadAllowed && !IsOwnerOrLead(p, owner) {
		return nil, httpx.Err(http.StatusForbidden, MsgNotAllowed)
	}
	seq := sequence(row.tree)
	if indexOf(seq, g) < 0 {
		return nil, notInFlow(g)
	}
	return &authorized{tree: row.tree, seq: seq, owner: owner}, nil
}

// IsOwnerOrLead là luật 1: chủ tính năng hoặc lead (dùng chung với điều khiển agent).
func IsOwnerOrLead(p httpx.Principal, owner *string) bool {
	return p.IsLead() || (owner != nil && p.Login == *owner)
}

func (c *authorized) before(g gate.Gate) []gate.Gate { return c.seq[:indexOf(c.seq, g)] }

// ---- duyệt

// FlagRef là một cờ rủi ro dev đã thấy khi duyệt.
type FlagRef struct {
	Code  string `json:"code"`
	Level string `json:"level"`
}

// ApproveRequest là body đã kiểm của POST /api/gates/approve.
type ApproveRequest struct {
	Repo, Feature   string
	Gate            gate.Gate
	Hash            string
	Flags           []FlagRef
	AssumptionsSeen []string
}

func conflict(msg httpx.Text) error { return httpx.Err(http.StatusConflict, msg) }

// errNotWeb: 409 repo không ở chế độ duyệt trên web, kèm mã NOT_WEB_MODE để CLI nhận ra ở mọi locale.
func errNotWeb() error {
	return httpx.ErrCode(http.StatusConflict, snapshot.ErrCodeNotWebMode, MsgNotWeb)
}

// Approve duyệt cổng theo luật giai đoạn 1; channel: web | terminal | claude-code. Duyệt lại đúng bản đã duyệt là
// idempotent (200, không ghi thêm).
func (s *Service) Approve(ctx context.Context, p httpx.Principal, ip, channel string, r ApproveRequest) (snapshot.FeatureGateState, error) {
	if isBlank(r.Hash) {
		return snapshot.FeatureGateState{}, bad(httpx.T("Thiếu hash"))
	}
	var out snapshot.FeatureGateState
	changed := false
	err := s.inTx(ctx, r.Repo, r.Feature, func(tx pgx.Tx, now time.Time) error {
		c, err := s.authorize(ctx, tx, p, r.Repo, r.Feature, r.Gate, true)
		if err != nil {
			return err
		}
		// 2. cổng trước đều có approval còn hiệu lực khớp currentHash
		for _, g := range c.before(r.Gate) {
			a, err := activeApproval(ctx, tx, r.Repo, r.Feature, g)
			if err != nil {
				return err
			}
			cur := currentHash(c.tree, g)
			if a == nil || cur == nil || a.Hash != *cur {
				return conflict(MsgPrevious)
			}
		}
		// 3. có submission còn hiệu lực
		sub, err := latestSubmission(ctx, tx, r.Repo, r.Feature, r.Gate)
		if err != nil {
			return err
		}
		if sub == nil {
			return conflict(MsgNotSubmitted)
		}
		// 4. hash request = hash bản nộp = currentHash của snapshot mới nhất
		if cur := currentHash(c.tree, r.Gate); r.Hash != sub.Hash || cur == nil || r.Hash != *cur {
			return conflict(MsgChanged)
		}
		// 5. bản nộp sạch
		if len(sub.Findings) > 0 {
			return conflict(MsgFindings)
		}
		// 6. REVIEW: eval đạt trên đúng cây code hiện tại, mọi bước đã accept
		var evaluated *string
		if r.Gate == gate.REVIEW {
			if evaluated, err = requireReviewReady(c.tree); err != nil {
				return err
			}
		}
		// 7. idempotent
		active, err := activeApproval(ctx, tx, r.Repo, r.Feature, r.Gate)
		if err != nil {
			return err
		}
		if active == nil || active.Hash != r.Hash {
			flags, seen := cleanFlags(r.Flags), cleanStrings(r.AssumptionsSeen)
			fj, _ := json.Marshal(flags)
			sj, _ := json.Marshal(seen)
			id, err := saveApproval(ctx, tx, approvalInsert{SubmissionID: sub.ID, Repo: r.Repo, Feature: r.Feature, Gate: r.Gate,
				Hash: r.Hash, Approver: p.Login, Channel: channel, EvaluatedTree: evaluated, FlagsJSON: string(fj),
				AssumptionsJSON: string(sj), Confirmed: true, At: now})
			if err != nil {
				return err
			}
			if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: channel, Action: "gate.approve",
				Repo: r.Repo, Feature: r.Feature, Target: string(r.Gate), Detail: map[string]any{"approvalId": id,
					"channel": channel, "hash": r.Hash, "flags": flags, "assumptionsSeen": len(seen)}}); err != nil {
				return err
			}
			changed = true
		}
		out, err = state(ctx, tx, r.Repo, r.Feature, ModeWeb, now)
		return err
	})
	if err == nil && changed {
		s.publish(r.Repo, r.Feature)
	}
	return out, err
}

// requireReviewReady là luật 6; trả evaluatedTree để lưu kèm approval REVIEW.
func requireReviewReady(tree any) (*string, error) {
	passed, ok := field(tree, "eval", "passed")
	if b, isBool := passed.(bool); !ok || !isBool || !b {
		return nil, conflict(MsgEvalFail)
	}
	evaluated, okE := textField(tree, "eval", "evaluatedTree")
	current, okT := textField(tree, "tree")
	if !okE || !okT || evaluated != current {
		return nil, conflict(MsgEvalTree)
	}
	steps, _ := field(tree, "steps")
	for _, st := range elements(steps) {
		v, ok := field(st, "status")
		if asText(v, ok) != "ACCEPTED" {
			return nil, conflict(MsgSteps)
		}
	}
	return &evaluated, nil
}

// ---- trả lại / mở lại

// Reject trả lại cổng; comment bắt buộc khác rỗng.
func (s *Service) Reject(ctx context.Context, p httpx.Principal, ip, channel, repo, feature string, g gate.Gate,
	comment *string) (snapshot.FeatureGateState, error) {
	if comment == nil || isBlank(*comment) {
		return snapshot.FeatureGateState{}, bad(httpx.T("Thiếu comment (lý do trả lại)"))
	}
	return s.feedback(ctx, p, ip, channel, repo, feature, g, *comment, "REJECTED", "gate.reject")
}

// Reopen mở lại cổng; comment tùy chọn.
func (s *Service) Reopen(ctx context.Context, p httpx.Principal, ip, channel, repo, feature string, g gate.Gate,
	comment *string) (snapshot.FeatureGateState, error) {
	c := ""
	if comment != nil {
		c = *comment
	}
	return s.feedback(ctx, p, ip, channel, repo, feature, g, c, "REOPENED", "gate.reopen")
}

// feedback thu hồi approval của cổng và mọi cổng sau trong luồng, ghi feedback (làm bản nộp cũ của cổng hết hiệu
// lực), sinh chỉ dẫn cho agent, audit.
func (s *Service) feedback(ctx context.Context, p httpx.Principal, ip, channel, repo, feature string, g gate.Gate,
	comment, kind, action string) (snapshot.FeatureGateState, error) {
	if utf16Len(comment) > MaxComment {
		return snapshot.FeatureGateState{}, bad(httpx.T("Ghi chú quá dài (tối đa 2000 ký tự)"))
	}
	var out snapshot.FeatureGateState
	err := s.inTx(ctx, repo, feature, func(tx pgx.Tx, now time.Time) error {
		c, err := s.authorize(ctx, tx, p, repo, feature, g, true)
		if err != nil {
			return err
		}
		revoked, err := revoke(ctx, tx, repo, feature, c.seq[indexOf(c.seq, g):], p.Login, now)
		if err != nil {
			return err
		}
		fb, err := saveFeedback(ctx, tx, repo, feature, g, kind, p.Login, comment, now)
		if err != nil {
			return err
		}
		text := feedbackInstruction(kind, string(g), p.Login)
		if !isBlank(comment) {
			text += ": " + javaTrim(comment)
		}
		if _, err := addInstruction(ctx, tx, repo, feature, p.Login, cutMarked(text, MaxComment), "feedback", &fb, now); err != nil {
			return err
		}
		if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: channel, Action: action,
			Repo: repo, Feature: feature, Target: string(g), Detail: map[string]any{"feedbackId": fb, "channel": channel,
				"revoked": revoked, "comment": comment}}); err != nil {
			return err
		}
		out, err = state(ctx, tx, repo, feature, ModeWeb, now)
		return err
	})
	if err == nil {
		s.publish(repo, feature)
	}
	return out, err
}

// ---- xác nhận bản nhập

// ConfirmImport: chủ tính năng (chỉ qua phiên) xác nhận bản nhập mới nhất của cổng; cổng trước phải đã có hiệu lực.
func (s *Service) ConfirmImport(ctx context.Context, p httpx.Principal, ip, repo, feature string, g gate.Gate) (snapshot.FeatureGateState, error) {
	var out snapshot.FeatureGateState
	err := s.inTx(ctx, repo, feature, func(tx pgx.Tx, now time.Time) error {
		c, err := s.authorize(ctx, tx, p, repo, feature, g, false)
		if err != nil {
			return err
		}
		if c.owner == nil || p.Login != *c.owner {
			return httpx.Err(http.StatusForbidden, httpx.T("Chỉ chủ tính năng được xác nhận bản nhập"))
		}
		// bản nhập chỉ có hiệu lực khi các cổng trước đã có hiệu lực: xác nhận theo thứ tự cổng
		for _, b := range c.before(g) {
			a, err := activeApproval(ctx, tx, repo, feature, b)
			if err != nil {
				return err
			}
			if a == nil {
				return conflict(MsgConfirmPrevious)
			}
		}
		pi, err := confirmImport(ctx, tx, repo, feature, g, p.Login, now)
		if err != nil {
			return err
		}
		if pi == nil {
			return conflict(httpx.T("Không có bản nhập chờ xác nhận"))
		}
		var origAt any
		if pi.OriginalAt != nil {
			origAt = httpx.Instant(*pi.OriginalAt)
		}
		if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "web", Action: "gate.confirm-import",
			Repo: repo, Feature: feature, Target: string(g), Detail: map[string]any{"approvalId": pi.ID, "hash": pi.Hash,
				"originalApprover": pi.OriginalApprover, "originalAt": origAt}}); err != nil {
			return err
		}
		out, err = state(ctx, tx, repo, feature, ModeWeb, now)
		return err
	})
	if err == nil {
		s.publish(repo, feature)
	}
	return out, err
}

// ---- nộp từ CLI

// RequireSubmittable là điều kiện nộp của CLI: cổng thuộc luồng (snapshot mới nhất; chưa có snapshot → FULL) và mọi
// cổng trước có approval còn hiệu lực. pendingImportOk: khi nhập (Task 7), cổng trước có bản nhập chờ xác nhận cũng
// tính. Gọi trong tx đã giữ khóa của tính năng.
func RequireSubmittable(ctx context.Context, tx pgx.Tx, repo, feature string, g gate.Gate, pendingImportOk bool) error {
	row, err := readFeature(ctx, tx, repo, feature)
	if err != nil {
		return err
	}
	var tree any
	if row != nil {
		tree = row.tree
	}
	seq := sequence(tree)
	if indexOf(seq, g) < 0 {
		return notInFlow(g)
	}
	pending := map[string]bool{}
	if pendingImportOk {
		ps, err := pendingImports(ctx, tx, repo, feature)
		if err != nil {
			return err
		}
		for _, p := range ps {
			pending[p.Gate] = true
		}
	}
	for _, b := range seq {
		if b == g {
			break
		}
		a, err := activeApproval(ctx, tx, repo, feature, b)
		if err != nil {
			return err
		}
		if a == nil && !pending[string(b)] {
			return conflict(MsgPrevious)
		}
	}
	return nil
}

// SubmitRequest là body đã kiểm của POST /api/cli/submissions.
type SubmitRequest struct {
	Repo, Feature string
	Gate          gate.Gate
	Hash          string
	findings      []finding
	artifacts     artifacts
	RiskLevel     *string
}

// Submit lưu một lần nộp (ai có token ingest đều nộp được, nhưng chỉ khi mọi cổng trước đã có approval còn hiệu lực),
// audit gate.submit (kênh cli), rồi gọi AfterSubmit. Trả trạng thái cổng mới.
func (s *Service) Submit(ctx context.Context, p httpx.Principal, ip string, r SubmitRequest) (snapshot.FeatureGateState, error) {
	mode, err := modeOf(ctx, s.Pool, r.Repo)
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	if mode != ModeWeb {
		return snapshot.FeatureGateState{}, errNotWeb()
	}
	if r.artifacts.hash() != r.Hash {
		return snapshot.FeatureGateState{}, bad(httpx.T("Hash không khớp nội dung"))
	}
	err = s.inTx(ctx, r.Repo, r.Feature, func(tx pgx.Tx, now time.Time) error {
		if err := RequireSubmittable(ctx, tx, r.Repo, r.Feature, r.Gate, false); err != nil {
			return err
		}
		id, err := saveSubmission(ctx, tx, r.Repo, r.Feature, r.Gate, r.Hash, r.findings, r.artifacts.list, p.Login, now)
		if err != nil {
			return err
		}
		if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "cli", Action: "gate.submit",
			Repo: r.Repo, Feature: r.Feature, Target: string(r.Gate), Detail: map[string]any{"submissionId": id, "hash": r.Hash,
				"findings": len(r.findings)}}); err != nil {
			return err
		}
		if s.AfterSubmit != nil {
			return s.AfterSubmit(ctx, tx, Submitted{Repo: r.Repo, Feature: r.Feature, Gate: r.Gate, SubmissionID: id, Hash: r.Hash,
				Clean: len(r.findings) == 0, RiskLevel: r.RiskLevel, At: now})
		}
		return nil
	})
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	s.publish(r.Repo, r.Feature)
	return state(ctx, s.Pool, r.Repo, r.Feature, ModeWeb, s.now())
}

// ---- board.GateView

// Overlay phủ trạng thái cổng của server lên snapshot (GateOverlay.apply + markSuperseded) cho repo chế độ web.
func (s *Service) Overlay(ctx context.Context, repoID, key string, snap json.RawMessage, now time.Time) (json.RawMessage, error) {
	st, err := state(ctx, s.Pool, repoID, key, ModeWeb, now)
	if err != nil {
		return nil, err
	}
	tree, err := board.ParseTree(snap)
	if err != nil {
		return nil, err
	}
	out := Apply(tree, st, httpx.LocaleFrom(ctx))
	sup, err := supersededGates(ctx, s.Pool, repoID, key)
	if err != nil {
		return nil, err
	}
	MarkSuperseded(out, sup)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// SubmittedPaths: đường dẫn artifact của bản nộp còn hiệu lực mới nhất mỗi cổng.
func (s *Service) SubmittedPaths(ctx context.Context, repoID, key string) (map[string][]string, error) {
	return submittedPaths(ctx, s.Pool, repoID, key)
}

// PendingImports: bản nhập từ local đang chờ chủ tính năng xác nhận.
func (s *Service) PendingImports(ctx context.Context, repoID, key string) ([]board.PendingImport, error) {
	ps, err := pendingImports(ctx, s.Pool, repoID, key)
	if err != nil {
		return nil, err
	}
	out := make([]board.PendingImport, 0, len(ps))
	for _, p := range ps {
		bp := board.PendingImport{Gate: p.Gate, Hash: p.Hash, OriginalApprover: p.OriginalApprover}
		if p.OriginalAt != nil {
			at := httpx.Instant(*p.OriginalAt)
			bp.OriginalAt = &at
		}
		out = append(out, bp)
	}
	return out, nil
}

// Resubmission so hai lần nộp gần nhất của cổng (gate đã kiểm hợp lệ): dưới 2 lần → {"n": n}.
func (s *Service) Resubmission(ctx context.Context, repoID, key, gateName string) (any, error) {
	g, err := parseGate(gateName)
	if err != nil {
		return nil, err
	}
	n, err := submissionCount(ctx, s.Pool, repoID, key, g)
	if err != nil {
		return nil, err
	}
	if n < 2 {
		return map[string]int{"n": n}, nil
	}
	last, err := lastSubmissions(ctx, s.Pool, repoID, key, g, 2)
	if err != nil {
		return nil, err
	}
	if len(last) < 2 {
		return map[string]int{"n": n}, nil
	}
	fbs, err := feedbackBetween(ctx, s.Pool, repoID, key, g, last[1].At, last[0].At)
	if err != nil {
		return nil, err
	}
	return review.Compare(string(g), n, last[1], last[0], fbs, httpx.LocaleFrom(ctx)), nil
}

// ---- lọc flags / assumptionsSeen: bỏ phần tử sai kiểu hoặc chứa NUL, cắt 500 ký tự, tối đa 50

// parseStrings: từ JSON chỉ giữ phần tử chuỗi; không phải mảng → rỗng.
func parseStrings(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return cleanStrings(out)
}

// parseFlags: từ JSON chỉ giữ object có code và level là chuỗi.
func parseFlags(v any) []FlagRef {
	arr, _ := v.([]any)
	var out []FlagRef
	for _, e := range arr {
		o, ok := e.(map[string]any)
		if !ok {
			continue
		}
		code, ok1 := o["code"].(string)
		level, ok2 := o["level"].(string)
		if ok1 && ok2 {
			out = append(out, FlagRef{Code: code, Level: level})
		}
	}
	return cleanFlags(out)
}

func cleanStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if len(out) == MaxItems {
			break
		}
		if !strings.ContainsRune(s, 0) {
			out = append(out, cut(s, MaxChars))
		}
	}
	return out
}

func cleanFlags(in []FlagRef) []FlagRef {
	out := []FlagRef{}
	for _, f := range in {
		if len(out) == MaxItems {
			break
		}
		if strings.ContainsRune(f.Code, 0) || strings.ContainsRune(f.Level, 0) {
			continue
		}
		out = append(out, FlagRef{Code: cut(f.Code, MaxChars), Level: cut(f.Level, MaxChars)})
	}
	return out
}

// cutMarked: dài hơn max (ký tự UTF-16) thì cắt còn max-1 rồi thêm "…".
func cutMarked(s string, max int) string {
	if utf16Len(s) <= max {
		return s
	}
	return cut(s, max-1) + "…"
}

// cut cắt còn tối đa max ký tự UTF-16, không tách đôi cặp surrogate (như String.substring của Java).
func cut(s string, max int) string {
	n := 0
	for i, r := range s {
		l := utf16.RuneLen(r)
		if n+l > max {
			return s[:i]
		}
		n += l
	}
	return s
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func javaTrim(s string) string { return javaish.Trim(s) }
