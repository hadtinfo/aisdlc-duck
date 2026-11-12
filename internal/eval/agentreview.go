package eval

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"aisdlc/internal/i18n"
)

// Các giá trị hợp lệ của review-agent.json.
var (
	Criteria   = []string{"sequence", "error-handling", "over-engineering", "naming", "security"}
	Labels     = []string{"DAT", "CAN_SUA", "NGHIEM_TRONG"}
	Severities = []string{"high", "medium", "low"}
)

// Criterion là kết quả một tiêu chí.
type Criterion struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// Issue là một vấn đề agent tìm thấy.
type Issue struct {
	Step     string `json:"step"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// AgentReview là kết quả agent review (review-agent.json): mỗi tiêu chí một nhãn kèm lý do (spec §11.3).
type AgentReview struct {
	Summary  string      `json:"summary"`
	Criteria []Criterion `json:"criteria"`
	Issues   []Issue     `json:"issues"`
}

// rawReview giữ con trỏ để phân biệt thiếu (null của Jackson) với rỗng và để phát hiện phần tử null.
type rawReview struct {
	Summary  *string `json:"summary"`
	Criteria []*struct {
		ID     *string `json:"id"`
		Label  *string `json:"label"`
		Reason *string `json:"reason"`
	} `json:"criteria"`
	Issues []*struct {
		Step     *string `json:"step"`
		File     *string `json:"file"`
		Line     int     `json:"line"`
		Severity *string `json:"severity"`
		Category *string `json:"category"`
		Message  *string `json:"message"`
	} `json:"issues"`
}

func sv(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// show in giá trị trong thông báo lỗi; thiếu thì "null" như Java.
func show(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}

// ReadAgentReview đọc và kiểm tra review-agent.json. Lỗi có thông báo tiếng Việt như bản Java.
func ReadAgentReview(path string) (AgentReview, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return AgentReview{}, errors.New(i18n.Tf("review-agent.json không đọc được: %s", err.Error()))
	}
	var raw *rawReview
	if err := json.Unmarshal(b, &raw); err != nil {
		return AgentReview{}, errors.New(i18n.Tf("review-agent.json không phải JSON hợp lệ: %s", err.Error()))
	}
	if raw == nil {
		return AgentReview{}, errors.New(i18n.T("review-agent.json rỗng."))
	}
	r := AgentReview{Summary: sv(raw.Summary), Criteria: []Criterion{}, Issues: []Issue{}}
	var errs []string
	seen := map[string]bool{}
	for _, c := range raw.Criteria {
		if c == nil {
			errs = append(errs, i18n.T("criteria có phần tử rỗng"))
			continue
		}
		r.Criteria = append(r.Criteria, Criterion{sv(c.ID), sv(c.Label), sv(c.Reason)})
		if c.ID == nil || !slices.Contains(Criteria, *c.ID) {
			errs = append(errs, i18n.Tf("tiêu chí lạ '%s' (chỉ %s)", show(c.ID), strings.Join(Criteria, ", ")))
		} else if seen[*c.ID] {
			errs = append(errs, i18n.Tf("tiêu chí '%s' xuất hiện hơn một lần", *c.ID))
		} else {
			seen[*c.ID] = true
		}
		if c.Label == nil || !slices.Contains(Labels, *c.Label) {
			errs = append(errs, i18n.Tf("tiêu chí '%s': label '%s' không hợp lệ (DAT, CAN_SUA, NGHIEM_TRONG)", show(c.ID), show(c.Label)))
		}
		if c.Reason == nil || strings.TrimSpace(*c.Reason) == "" {
			errs = append(errs, i18n.Tf("tiêu chí '%s': reason không được rỗng", show(c.ID)))
		}
	}
	for _, id := range Criteria {
		if !seen[id] {
			errs = append(errs, i18n.Tf("thiếu tiêu chí '%s'", id))
		}
	}
	for i, x := range raw.Issues {
		if x == nil {
			errs = append(errs, i18n.Tf("issues[%d] rỗng", i))
			continue
		}
		r.Issues = append(r.Issues, Issue{sv(x.Step), sv(x.File), x.Line, sv(x.Severity), sv(x.Category), sv(x.Message)})
		if x.Severity == nil || !slices.Contains(Severities, *x.Severity) {
			errs = append(errs, i18n.Tf("issues[%d]: severity '%s' không hợp lệ (high, medium, low)", i, show(x.Severity)))
		}
		if x.Category == nil || (!slices.Contains(Criteria, *x.Category) && *x.Category != "other") {
			errs = append(errs, i18n.Tf("issues[%d]: category '%s' không hợp lệ (5 tiêu chí hoặc other)", i, show(x.Category)))
		}
	}
	if len(errs) > 0 {
		return AgentReview{}, errors.New(i18n.Tf("review-agent.json không hợp lệ: %s.", strings.Join(errs, "; ")))
	}
	return r, nil
}

// PassedCount là số tiêu chí đạt nhãn DAT.
func (r AgentReview) PassedCount() int {
	n := 0
	for _, c := range r.Criteria {
		if c.Label == "DAT" {
			n++
		}
	}
	return n
}
