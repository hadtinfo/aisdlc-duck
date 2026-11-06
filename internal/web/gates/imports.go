package gates

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"aisdlc/internal/gate"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
)

// MsgReviewImport: luật 6 (eval đạt trên cây code hiện tại) chỉ kiểm được khi duyệt trên web.
const MsgReviewImport = "REVIEW phải duyệt lại trên web"

// ImportRoutes đăng ký POST /api/cli/import (Bearer ingest, ImportController): đưa approval đã có ở local lên web. Bản
// nhập chưa có hiệu lực (không vào phong bì) tới khi chủ tính năng xác nhận ở /api/gates/confirm-import.
func ImportRoutes(r chi.Router, s *Service) {
	r.With(httpx.LimitBodyWith(MaxBytes, tooLarge())).Post("/api/cli/import", s.handle(s.importApproval))
}

// CIRoutes đăng ký GET /api/ci/approvals?repo=&feature= (Bearer ci, kiểm ở auth.Middleware; CiController).
func CIRoutes(r chi.Router, s *Service) {
	r.Get("/api/ci/approvals", s.ciApprovals)
}

// ImportRequest là body đã kiểm của POST /api/cli/import.
type ImportRequest struct {
	Repo, Feature string
	Gate          gate.Gate
	Hash          string
	Approver      string    // người duyệt ở local
	At            time.Time // thời điểm duyệt ở local
	findings      []finding
	artifacts     artifacts
}

func max200(s, field string) (string, error) {
	if utf16Len(s) > 200 {
		return "", bad(field + " quá dài (tối đa 200 ký tự)")
	}
	return s, nil
}

// isoInstant như Instant.parse rồi OffsetDateTime.parse: yyyy-MM-ddTHH:mm[:ss[.f{1,9}]] kèm Z hoặc ±HH:mm[:ss].
var isoInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,9})?)?(Z|[+-]\d{2}:\d{2}(:\d{2})?)$`)

func instant(s string) (time.Time, error) {
	fail := bad("at không phải thời điểm ISO-8601: " + s)
	if !isoInstant.MatchString(s) {
		return time.Time{}, fail
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05.999999999Z07:00:00",
		"2006-01-02T15:04Z07:00:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fail
}

func (s *Service) importApproval(r *http.Request, p httpx.Principal) (snapshot.FeatureGateState, error) {
	var none snapshot.FeatureGateState
	b, err := body(r)
	if err != nil {
		return none, err
	}
	repo, feature, gname, err := target(b)
	if err != nil {
		return none, err
	}
	g, err := parseGate(gname)
	if err != nil {
		return none, err
	}
	hash, err := text(b, "hash")
	if err != nil {
		return none, err
	}
	approver, err := text(b, "approver")
	if err == nil {
		approver, err = max200(approver, "approver")
	}
	if err != nil {
		return none, err
	}
	atText, err := text(b, "at")
	if err == nil {
		atText, err = max200(atText, "at")
	}
	if err != nil {
		return none, err
	}
	at, err := instant(atText)
	if err != nil {
		return none, err
	}
	fv, fp := b["findings"]
	fs, err := findings(fv, fp)
	if err != nil {
		return none, err
	}
	arts, err := parseArtifacts(b["artifacts"])
	if err != nil {
		return none, err
	}
	return s.Import(r.Context(), p, clientIP(r), ImportRequest{Repo: repo, Feature: feature, Gate: g, Hash: hash,
		Approver: approver, At: at, findings: fs, artifacts: arts})
}

// Import nhập duyệt từ local (ApprovalService.importApproval): tạo submission + approval channel=import chưa xác nhận
// (chưa có hiệu lực, không vào phong bì) tới khi chủ tính năng xác nhận. Cổng trước có bản nhập chờ xác nhận cũng
// tính là đã duyệt (web migrate gửi liền mọi cổng); xác nhận thì theo thứ tự cổng.
func (s *Service) Import(ctx context.Context, p httpx.Principal, ip string, r ImportRequest) (snapshot.FeatureGateState, error) {
	var out snapshot.FeatureGateState
	mode, err := modeOf(ctx, s.Pool, r.Repo)
	if err != nil {
		return out, err
	}
	if mode != ModeWeb {
		return out, conflict(MsgNotWeb)
	}
	if r.Gate == gate.REVIEW {
		return out, conflict(MsgReviewImport)
	}
	if r.artifacts.hash() != r.Hash {
		return out, bad("Hash không khớp nội dung")
	}
	if len(r.findings) > 0 {
		return out, conflict(MsgFindings)
	}
	err = s.inTx(ctx, r.Repo, r.Feature, func(tx pgx.Tx, now time.Time) error {
		if err := RequireSubmittable(ctx, tx, r.Repo, r.Feature, r.Gate, true); err != nil {
			return err
		}
		sub, err := saveSubmission(ctx, tx, r.Repo, r.Feature, r.Gate, r.Hash, r.findings, r.artifacts.list, p.Login, now)
		if err != nil {
			return err
		}
		approver, at := r.Approver, r.At
		id, err := saveApproval(ctx, tx, approvalInsert{SubmissionID: sub, Repo: r.Repo, Feature: r.Feature, Gate: r.Gate,
			Hash: r.Hash, Approver: p.Login, Channel: "import", FlagsJSON: "[]", AssumptionsJSON: "[]",
			OriginalApprover: &approver, OriginalAt: &at, Confirmed: false, At: now})
		if err != nil {
			return err
		}
		if err := s.Auth.WriteAudit(ctx, tx, auth.AuditEntry{Login: p.Login, IP: ip, Channel: "cli", Action: "gate.import",
			Repo: r.Repo, Feature: r.Feature, Target: string(r.Gate), Detail: map[string]any{"approvalId": id,
				"submissionId": sub, "hash": r.Hash, "originalApprover": r.Approver, "originalAt": httpx.Instant(r.At)}}); err != nil {
			return err
		}
		out, err = state(ctx, tx, r.Repo, r.Feature, ModeWeb, now)
		return err
	})
	if err != nil {
		return snapshot.FeatureGateState{}, err
	}
	s.publish(r.Repo, r.Feature) // bản nhập chờ xác nhận hiện trên trang tính năng
	return out, nil
}

// CIApproval là một approval còn hiệu lực trong phản hồi /api/ci/approvals (thứ tự khóa như CiController; at,
// evaluatedTree, rule có thể null).
type CIApproval struct {
	Gate          string  `json:"gate"`
	Hash          string  `json:"hash"`
	Approver      string  `json:"approver"`
	At            *string `json:"at"`
	EvaluatedTree *string `json:"evaluatedTree"`
	Channel       string  `json:"channel"`
	Rule          *string `json:"rule"`
}

// CIApprovals là phản hồi /api/ci/approvals: JSON thường (không ký) mà HttpCiApprovalSource (CLI) đọc thành
// eval.CIApprovals. Pinned: repo đã có bản ghi chế độ (CLI/CI theo server); chưa có thì "local" chỉ là mặc định, CI
// theo config.yaml. Approvals: không gồm bản đã thu hồi hay nhập chưa xác nhận; bản tự duyệt của server có
// channel = auto và rule (CI kiểm lại theo flow trong risk.yaml).
type CIApprovals struct {
	Mode      string       `json:"mode"`
	Pinned    bool         `json:"pinned"`
	Approvals []CIApproval `json:"approvals"`
}

// CIState là approval còn hiệu lực của tính năng cho CI (CiController.approvals).
func (s *Service) CIState(ctx context.Context, repo, feature string) (CIApprovals, error) {
	out := CIApprovals{Mode: ModeLocal, Approvals: []CIApproval{}}
	md, err := find(ctx, s.Pool, repo)
	if err != nil {
		return out, err
	}
	if md != nil {
		out.Mode, out.Pinned = md.Mode, true
	}
	if out.Mode != ModeWeb {
		return out, nil
	}
	for _, g := range allGates {
		a, err := activeApproval(ctx, s.Pool, repo, feature, g)
		if err != nil {
			return out, err
		}
		if a == nil {
			continue
		}
		at := httpx.Instant(a.At)
		out.Approvals = append(out.Approvals, CIApproval{Gate: a.Gate, Hash: a.Hash, Approver: a.Approver, At: &at,
			EvaluatedTree: a.EvaluatedTree, Channel: a.Channel, Rule: a.Rule})
	}
	return out, nil
}

func (s *Service) ciApprovals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, err := param(q.Get("repo"), "repo")
	if err == nil {
		var feature string
		if feature, err = param(q.Get("feature"), "feature"); err == nil {
			var out CIApprovals
			if out, err = s.CIState(r.Context(), repo, feature); err == nil {
				httpx.WriteJSON(w, http.StatusOK, out)
				return
			}
		}
	}
	httpx.WriteError(w, err)
}
