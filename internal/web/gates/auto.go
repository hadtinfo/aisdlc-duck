package gates

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/board"
)

// Quy trình theo mức rủi ro ở chế độ web (AutoApprover.java, spec risk-flow §5.1, §5.4): server tự duyệt bản nộp sạch
// khi flow của mức áp cho cổng là auto, và thay thế bản tự duyệt khi mức mới đặt cổng về duyệt tay. Quyết định dùng
// internal/risk (AutoGate, LevelForGate, Stricter) với chính sách đang hiệu lực của repo.
//
// Cả hai hàm là hook chạy trong tx của người gọi (nộp / ingest), đã giữ khóa của tính năng: mọi truy vấn đi qua tx,
// không bao giờ qua Pool (giữ một kết nối và chờ kết nối thứ hai làm cạn pool).

type livePolicy struct {
	p       risk.RiskPolicy
	version int
}

// latestPolicy: chính sách đang hiệu lực của repo (risk_policy_versions mới nhất); không có hoặc hỏng → nil (không
// tự duyệt).
func latestPolicy(ctx context.Context, d db, repo string) (*livePolicy, error) {
	var version int
	var body string
	err := d.QueryRow(ctx, "SELECT version, body FROM risk_policy_versions WHERE repo_id = $1 ORDER BY version DESC LIMIT 1",
		repo).Scan(&version, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := risk.ParsePolicy(body)
	if err != nil {
		return nil, nil
	}
	return &livePolicy{p: p, version: version}, nil
}

func auditAuto(ctx context.Context, s *Service, tx pgx.Tx, action, repo, feature, target string, detail map[string]any) error {
	return s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Channel: "auto", Action: action, Repo: repo, Feature: feature,
		Target: target, Detail: detail})
}

// AutoApprove là AfterSubmit của Service (AutoApprover.afterSubmit): vừa lưu bản nộp sub của cổng; tự duyệt khi bản nộp
// sạch, review card trong snapshot mới nhất ứng với đúng nội dung này và không có cờ HIGH, và flow của mức áp cho cổng
// là auto. Mức: risk.LevelForGate (risk.level CLI gửi kèm), rồi nâng lên features.risk_level của server nếu mức đó
// chặt hơn. Mức không có trong chính sách → audit gate.auto-skip, không tự duyệt. BRD và Review luôn duyệt tay.
func (s *Service) AutoApprove(ctx context.Context, tx pgx.Tx, sub Submitted) error {
	if sub.Gate == gate.REVIEW || sub.Gate == gate.BRD || !sub.Clean {
		return nil
	}
	pol, err := latestPolicy(ctx, tx, sub.Repo)
	if err != nil || pol == nil {
		return err
	}
	var raw string
	var serverLevel *string
	err = tx.QueryRow(ctx, "SELECT snapshot::text, risk_level FROM features WHERE repo_id = $1 AND feature_key = $2",
		sub.Repo, sub.Feature).Scan(&raw, &serverLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tree, err := board.ParseTree([]byte(raw))
	if err != nil {
		return err
	}
	if cur := currentHash(tree, sub.Gate); cur == nil || *cur != sub.Hash { // thẻ không ứng với bản nộp
		return nil
	}
	card, _ := field(tree, "reviewCards", string(sub.Gate))
	if _, ok := card.(map[string]any); !ok { // không có thẻ: không biết cờ, để dev duyệt
		return nil
	}
	known := func(level string) (bool, error) {
		if pol.p.Rank(level) >= 0 {
			return true, nil
		}
		return false, auditAuto(ctx, s, tx, "gate.auto-skip", sub.Repo, sub.Feature, string(sub.Gate), map[string]any{
			"message":      i18n.VI.Tf("auto: mức %s không có trong chính sách v%d", level, pol.version), // audit lưu tiếng Việt
			"submissionId": sub.SubmissionID})
	}
	requested := ""
	if sub.RiskLevel != nil {
		requested = *sub.RiskLevel
		if ok, err := known(requested); !ok {
			return err
		}
	}
	level := risk.LevelForGate(pol.p, sub.Gate, requested)
	if level == "" {
		return nil
	}
	if ok, err := known(level); !ok {
		return err
	}
	// mức server đã biết (event rủi ro) chặt hơn thì lấy mức server
	if serverLevel != nil {
		if ok, err := known(*serverLevel); !ok {
			return err
		}
		level = risk.Stricter(pol.p, level, *serverLevel)
	}
	if _, manual := risk.AutoGate(pol.p, level, sub.Gate, hasHighFlag(card)); manual {
		return nil
	}
	active, err := activeApproval(ctx, tx, sub.Repo, sub.Feature, sub.Gate)
	if err != nil {
		return err
	}
	if active != nil && active.Hash == sub.Hash {
		return nil
	}
	rule := "flow:" + level
	id, err := saveAutoApproval(ctx, tx, sub.SubmissionID, sub.Repo, sub.Feature, sub.Gate, sub.Hash, rule, sub.At)
	if err != nil {
		return err
	}
	return auditAuto(ctx, s, tx, "gate.auto-approve", sub.Repo, sub.Feature, string(sub.Gate), map[string]any{
		"approvalId": id, "submissionId": sub.SubmissionID, "hash": sub.Hash, "rule": rule, "policyVersion": pol.version})
}

// flagKeys là các trường của ReviewFlag; Jackson (FAIL_ON_UNKNOWN_PROPERTIES) từ chối khóa khác.
var flagKeys = map[string]bool{"code": true, "message": true, "hint": true, "level": true, "refs": true}

// hasHighFlag: cờ của thẻ có mức HIGH không. Như mapper.convertValue(card.flags, List<ReviewFlag>) của Java: thiếu hoặc
// null → không cờ; cấu trúc lạ (không phải mảng, phần tử không phải object, khóa lạ, mức lạ, chuỗi gặp object/mảng,
// refs không phải mảng chuỗi) → coi như có cờ HIGH.
func hasHighFlag(card any) bool {
	flags, ok := card.(map[string]any)["flags"]
	if !ok || flags == nil {
		return false
	}
	arr, ok := flags.([]any)
	if !ok {
		return true
	}
	flat := make([]review.ReviewFlag, 0, len(arr))
	for _, e := range arr {
		if e == nil {
			continue
		}
		o, ok := e.(map[string]any)
		if !ok {
			return true
		}
		for k, v := range o {
			if !flagKeys[k] {
				return true
			}
			switch k {
			case "code", "message", "hint":
				if !scalarOrNull(v) {
					return true
				}
			case "refs":
				if v == nil {
					continue
				}
				refs, ok := v.([]any)
				if !ok {
					return true
				}
				for _, r := range refs {
					if r == nil || !scalarOrNull(r) { // List.copyOf từ chối phần tử null
						return true
					}
				}
			case "level":
				if v == nil {
					continue
				}
				lv, ok := v.(string)
				switch review.Level(lv) {
				case review.HIGH, review.WARN, review.INFO, review.NA:
				default:
					ok = false
				}
				if !ok {
					return true
				}
				flat = append(flat, review.ReviewFlag{Level: review.Level(lv)})
			}
		}
	}
	return risk.HasHighFlag(flat)
}

// scalarOrNull: Jackson đổi được sang String (chuỗi, số, bool, null); object/mảng thì không.
func scalarOrNull(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// OnRiskChanged là RiskChanged của ingest (AutoApprover.onRiskChanged): mức rủi ro của tính năng vừa đổi from → to
// (RISK_CLASSIFIED / RISK_OVERRIDDEN; from "" = chưa biết, như null của Java). Bản tự duyệt còn hiệu lực của cổng mà
// flow của mức mới là duyệt tay bị thu hồi (revoked_by = auto-supersede), cổng về chờ dev duyệt; audit
// gate.auto-supersede. Không có chính sách thì mọi cổng là duyệt tay. Bản duyệt tay không đổi.
func (s *Service) OnRiskChanged(ctx context.Context, tx pgx.Tx, repo, feature, from, to string) error {
	if isBlank(to) {
		return nil
	}
	live, err := liveAutoApprovals(ctx, tx, repo, feature)
	if err != nil || len(live) == 0 {
		return err
	}
	pol, err := latestPolicy(ctx, tx, repo)
	if err != nil {
		return err
	}
	var manual []gate.Gate
	before, known := from, from != ""
	for _, g := range allGates {
		rule, ok := live[g]
		if !ok {
			continue
		}
		unit := risk.FlowUnit(g)
		if pol != nil && unit != "" && pol.p.FlowFor(to, unit) == "auto" {
			continue
		}
		manual = append(manual, g)
		if !known && rule != nil && strings.HasPrefix(*rule, "flow:") {
			before, known = strings.TrimPrefix(*rule, "flow:"), true
		}
	}
	if len(manual) == 0 {
		return nil
	}
	if _, err := supersedeAuto(ctx, tx, repo, feature, manual, s.now()); err != nil {
		return err
	}
	var p *risk.RiskPolicy
	if pol != nil {
		p = &pol.p
	}
	names := gateNames(manual)
	d := map[string]any{"message": risk.SupersedeMessage(p, before, to, manual), "from": nil, "to": to, "gates": names}
	if known {
		d["from"] = before
	}
	if pol != nil {
		d["policyVersion"] = pol.version
	}
	return auditAuto(ctx, s, tx, "gate.auto-supersede", repo, feature, strings.Join(names, ","), d)
}

// liveAutoApprovals: bản tự duyệt còn hiệu lực (channel auto, chưa thu hồi) của tính năng: cổng → rule (có thể nil).
func liveAutoApprovals(ctx context.Context, d db, repo, feature string) (map[gate.Gate]*string, error) {
	rows, err := d.Query(ctx, "SELECT gate, rule FROM approvals WHERE repo_id = $1 AND feature_key = $2 AND channel = 'auto' "+
		"AND revoked_at IS NULL ORDER BY id", repo, feature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[gate.Gate]*string{}
	for rows.Next() {
		var g string
		var rule *string
		if err := rows.Scan(&g, &rule); err != nil {
			return nil, err
		}
		out[gate.Gate(g)] = rule
	}
	return out, rows.Err()
}
