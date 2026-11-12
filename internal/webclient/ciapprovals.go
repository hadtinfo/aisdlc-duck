package webclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// CIApprovalTimeout là thời hạn một lần hỏi approval của CI (HttpCiApprovalSource.java: 10 giây).
const CIApprovalTimeout = 10 * time.Second

// HTTPCIApprovals là eval.CIApprovalSource qua GET /api/ci/approvals với Bearer token CI. Repo trả định danh repo
// như web thấy (snapshot.RepoID). Lỗi chỉ nêu lý do ("HTTP 401", lỗi mạng đã bỏ token), không bao giờ chứa token.
type HTTPCIApprovals struct {
	BaseURL, Token string
	Repo           func(ctx context.Context) string
	Timeout        time.Duration // 0 thì CIApprovalTimeout
}

var _ eval.CIApprovalSource = (*HTTPCIApprovals)(nil)

type ciApprovalsBody struct {
	Mode      string  `json:"mode"`
	Pinned    *bool   `json:"pinned"`
	Approvals []ciRow `json:"approvals"`
}

type ciRow struct {
	Gate          string  `json:"gate"`
	Hash          string  `json:"hash"`
	Approver      string  `json:"approver"`
	At            *string `json:"at"`
	EvaluatedTree *string `json:"evaluatedTree"`
	Channel       string  `json:"channel"`
	Rule          *string `json:"rule"`
}

// Approvals hỏi web approval còn hiệu lực của tính năng. tree không gửi (web đã lưu evaluatedTree lúc duyệt).
func (h *HTTPCIApprovals) Approvals(ctx context.Context, key model.FeatureKey, _ string) (eval.CIApprovals, error) {
	repo := ""
	if h.Repo != nil {
		repo = h.Repo(ctx)
	}
	if repo == "" {
		return eval.CIApprovals{}, errors.New(i18n.T("không xác định được repo"))
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = CIApprovalTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := &Client{BaseURL: h.BaseURL, Token: h.Token}
	path := "/api/ci/approvals?repo=" + neturl.QueryEscape(repo) + "&feature=" + neturl.QueryEscape(key.String())
	r, err := c.Do(ctx, "GET", path, nil)
	if err != nil {
		return eval.CIApprovals{}, err
	}
	if r.Status != 200 {
		return eval.CIApprovals{}, fmt.Errorf("HTTP %d", r.Status)
	}
	out, err := parseCIApprovals(r.Body)
	if err != nil {
		return eval.CIApprovals{}, errors.New(i18n.T("phản hồi web không đọc được"))
	}
	return out, nil
}

func parseCIApprovals(body []byte) (eval.CIApprovals, error) {
	var b ciApprovalsBody
	if err := json.Unmarshal(body, &b); err != nil {
		return eval.CIApprovals{}, err
	}
	out := eval.CIApprovals{Mode: b.Mode, Approvals: map[gate.Gate]gate.Approval{}, EvaluatedTree: map[gate.Gate]string{}}
	if out.Mode == "" {
		out.Mode = "local"
	}
	// server cũ không có "pinned": chế độ web chỉ có khi đã ghim
	out.Pinned = out.Mode == "web"
	if b.Pinned != nil {
		out.Pinned = *b.Pinned
	}
	for _, a := range b.Approvals {
		g := gate.Gate(a.Gate)
		switch g {
		case gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW:
		default:
			continue // cổng lạ: bỏ qua như Java
		}
		var at time.Time
		if a.At != nil {
			t, err := time.Parse(time.RFC3339Nano, *a.At)
			if err != nil {
				return eval.CIApprovals{}, err
			}
			at = t
		}
		rule := ""
		if a.Rule != nil {
			rule = *a.Rule
		}
		out.Approvals[g] = gate.ApprovalFromWeb(g, a.Hash, a.Approver, at, a.Channel, rule)
		if a.EvaluatedTree != nil {
			out.EvaluatedTree[g] = *a.EvaluatedTree
		}
	}
	return out, nil
}
