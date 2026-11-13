package gates

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
)

// Chỉ dẫn "quay về mốc" gửi agent, lưu tiếng Việt như mọi chỉ dẫn server (xem feedbacktext.go).
var rewindText = i18n.VI.T("Tính năng %s đã được quay về đầu %s bởi %s. Mọi thứ sau mốc đã bị xoá; bắt đầu lại bằng %s.")

// restartCommand: lệnh bắt đầu lại từ mốc (brd/design/cases/review → skill; Sx → aisdlc step start Sx).
func restartCommand(checkpoint string) string {
	switch strings.ToLower(checkpoint) {
	case "brd", "design", "cases", "review":
		return "/aisdlc:" + strings.ToLower(checkpoint)
	}
	return "aisdlc step start " + checkpoint
}

var stepCheckpoint = regexp.MustCompile(`^[sS][0-9]+$`)

// checkpointGate: cổng đầu phải xoá của mốc (brd→BRD, design→DESIGN, cases→CASES, Sx/review→REVIEW).
func checkpointGate(checkpoint string) (gate.Gate, bool) {
	switch strings.ToLower(checkpoint) {
	case "brd":
		return gate.BRD, true
	case "design":
		return gate.DESIGN, true
	case "cases":
		return gate.CASES, true
	case "review":
		return gate.REVIEW, true
	}
	return gate.REVIEW, stepCheckpoint.MatchString(checkpoint)
}

// RewindInstruction là câu chỉ dẫn agent sau khi quay về mốc.
func RewindInstruction(feature, checkpoint, login string) string {
	return i18n.VI.Tf(rewindText, feature, checkpoint, login, restartCommand(checkpoint))
}

// Rewind khép lần làm hiện tại của tính năng ở web: lưu snapshot vào feature_rewinds, thu hồi approval của các cổng
// từ mốc trở đi, đánh dấu submissions/feedback của các cổng đó là đã khép, tăng attempt, huỷ pending_actions, ghi
// chỉ dẫn agent + audit. Cùng requestID gọi lại: không đổi gì, trả trạng thái hiện tại.
func (s *Service) Rewind(ctx context.Context, p httpx.Principal, ip, channel, repo, feature, checkpoint, requestID string,
	gates []gate.Gate) (snapshot.FeatureGateState, error) {
	if p.IsSession() { // chỉ CLI (token approve) được quay về; web không có thao tác này
		return snapshot.FeatureGateState{}, httpx.Err(http.StatusForbidden, httpx.T("Chỉ quay về được từ CLI (aisdlc rewind)"))
	}
	if len(gates) == 0 {
		return snapshot.FeatureGateState{}, bad(httpx.T("Thiếu gates"))
	}
	if isBlank(checkpoint) {
		return snapshot.FeatureGateState{}, bad(httpx.T("Thiếu checkpoint"))
	}
	if g, ok := checkpointGate(checkpoint); !ok {
		return snapshot.FeatureGateState{}, bad(httpx.T("checkpoint phải là brd, design, cases, review hoặc Sx"))
	} else if gates[0] != g {
		return snapshot.FeatureGateState{}, bad(httpx.T("checkpoint không khớp cổng đầu của gates"))
	}
	if n := utf8.RuneCountInString(requestID); n < 1 || n > 200 {
		return snapshot.FeatureGateState{}, bad(httpx.T("requestId phải dài 1–200 ký tự"))
	}
	var out snapshot.FeatureGateState
	changed := false
	err := s.inTx(ctx, repo, feature, func(tx pgx.Tx, now time.Time) error {
		mode, err := modeOf(ctx, tx, repo)
		if err != nil {
			return err
		}
		if mode != ModeWeb {
			return errNotWeb()
		}
		row, err := readFeature(ctx, tx, repo, feature)
		if err != nil {
			return err
		}
		if row == nil {
			return conflict(httpx.T("Tính năng chưa được đồng bộ lên web; chạy aisdlc status rồi thử lại"))
		}
		c, err := s.authorize(ctx, tx, p, repo, feature, gates[0], true)
		if err != nil {
			return err
		}
		from := indexOf(c.seq, gates[0])
		want := c.seq[from:]
		if len(want) != len(gates) {
			return bad(httpx.T("Các cổng phải liên tiếp từ mốc đến hết luồng"))
		}
		for i := range want {
			if want[i] != gates[i] {
				return bad(httpx.T("Các cổng phải liên tiếp từ mốc đến hết luồng"))
			}
		}
		var seen int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM feature_rewinds WHERE repo_id = $1 AND feature_key = $2 AND request_id = $3",
			repo, feature, requestID).Scan(&seen); err != nil {
			return err
		}
		if seen == 0 {
			changed = true
			if err := doRewind(ctx, tx, repo, feature, checkpoint, requestID, p.Login, now, gates); err != nil {
				return err
			}
			if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: channel, Action: "feature.rewind",
				Repo: repo, Feature: feature, Target: checkpoint, Detail: map[string]any{"requestId": requestID,
					"gates": gateNames(gates), "channel": channel}}); err != nil {
				return err
			}
		}
		out, err = state(ctx, tx, repo, feature, ModeWeb, now)
		return err
	})
	if err == nil && changed {
		s.publish(repo, feature)
	}
	return out, err
}

func doRewind(ctx context.Context, tx pgx.Tx, repo, feature, checkpoint, requestID, login string, now time.Time, gates []gate.Gate) error {
	names := gateNames(gates)
	var id int64
	if err := tx.QueryRow(ctx, "INSERT INTO feature_rewinds(repo_id, feature_key, from_attempt, to_attempt, checkpoint, by_login, at, snapshot, request_id) "+
		"SELECT repo_id, feature_key, attempt, attempt + 1, $3, $4, $5, snapshot, $6 FROM features WHERE repo_id = $1 AND feature_key = $2 RETURNING id",
		repo, feature, checkpoint, login, now, requestID).Scan(&id); err != nil {
		return err
	}
	if _, err := revoke(ctx, tx, repo, feature, gates, login, now); err != nil {
		return err
	}
	for _, t := range []string{"submissions", "feedback"} {
		if _, err := tx.Exec(ctx, "UPDATE "+t+" SET rewind_id = $1 WHERE repo_id = $2 AND feature_key = $3 AND gate = ANY($4) AND rewind_id IS NULL",
			id, repo, feature, names); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE features SET attempt = attempt + 1 WHERE repo_id = $1 AND feature_key = $2", repo, feature); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE pending_actions SET status = 'CANCELLED', resolved_at = $3, reason = 'rewind' "+
		"WHERE repo_id = $1 AND feature_key = $2 AND status = 'PENDING'", repo, feature, now); err != nil {
		return err
	}
	// chỉ dẫn chưa giao của lần làm cũ không được tới agent nữa (không đặt delivered_at: chưa hề giao)
	if _, err := tx.Exec(ctx, "UPDATE instructions SET rewind_id = $1 WHERE repo_id = $2 AND feature_key = $3 "+
		"AND delivered_at IS NULL AND rewind_id IS NULL", id, repo, feature); err != nil {
		return err
	}
	fb, err := saveFeedback(ctx, tx, repo, feature, gates[0], "REWOUND", login, "", now)
	if err != nil {
		return err
	}
	_, err = addInstruction(ctx, tx, repo, feature, login, RewindInstruction(feature, checkpoint, login), "feedback", &fb, now)
	return err
}

func (s *Service) rewind(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	var none snapshot.FeatureGateState
	b, err := body(r)
	if err != nil {
		return none, err
	}
	repo, err := text(b, "repo")
	if err != nil {
		return none, err
	}
	feature, err := text(b, "feature")
	if err != nil {
		return none, err
	}
	checkpoint, err := text(b, "checkpoint")
	if err != nil {
		return none, err
	}
	requestID, err := text(b, "requestId")
	if err != nil {
		return none, err
	}
	raw, _ := b["gates"].([]any)
	gs := make([]gate.Gate, 0, len(raw))
	for _, n := range raw {
		name, _ := n.(string)
		g, err := parseGate(name)
		if err != nil {
			return none, err
		}
		gs = append(gs, g)
	}
	return s.Rewind(r.Context(), p, clientIP(r), channel(p, b), repo, feature, checkpoint, requestID, gs)
}
