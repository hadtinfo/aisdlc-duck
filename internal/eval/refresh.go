package eval

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/fsx"
	"aisdlc/internal/model"
)

// RefreshAgentReview cập nhật chỉ số AGENT_REVIEW trong eval/report.json và report.md theo review-agent.json
// hiện tại, không chạy lại harness (review chạy sau eval vì cần diff-by-step.md). Mọi thứ khác giữ nguyên.
// Không có report.json thì không làm gì.
func RefreshAgentReview(p model.FeaturePaths) error {
	jsonPath := filepath.Join(p.EvalDir(), "report.json")
	if _, err := os.Stat(jsonPath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	r, err := ReadEvalReport(jsonPath)
	if err != nil {
		return err
	}
	row := AgentReviewCheck(p.ReviewAgent())
	soft := make([]SoftCheck, 0, len(r.Soft)+1)
	replaced := false
	for _, s := range r.Soft {
		if s.ID == row.ID {
			soft = append(soft, row)
			replaced = true
		} else {
			soft = append(soft, s)
		}
	}
	if !replaced {
		soft = append(soft, row)
	}
	r.Soft = soft
	if err := r.Write(jsonPath); err != nil {
		return err
	}
	mdPath := filepath.Join(p.EvalDir(), "report.md")
	b, err := os.ReadFile(mdPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("Không ghi được " + mdPath + ": " + err.Error())
	}
	return fsx.WriteAtomic(mdPath, []byte(WithSoftRow(string(b), row)), 0o644)
}

// AgentReviewCheck tóm tắt review-agent.json thành một chỉ số mềm (NA nếu chưa có, WARN nếu có lỗi nghiêm trọng).
func AgentReviewCheck(path string) SoftCheck {
	const id = "AGENT_REVIEW"
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return SoftCheck{id, SoftNA, "chưa có review-agent.json"}
	}
	r, err := ReadAgentReview(path)
	if err != nil {
		return SoftCheck{id, SoftWarn, err.Error()}
	}
	parts := []string{strconv.Itoa(r.PassedCount()) + "/" + strconv.Itoa(len(r.Criteria)) + " ĐẠT"}
	serious := false
	for _, label := range []string{"CAN_SUA", "NGHIEM_TRONG"} {
		var ids []string
		for _, c := range r.Criteria {
			if c.Label == label {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			shown := "CẦN_SỬA"
			if label == "NGHIEM_TRONG" {
				shown = "NGHIÊM_TRỌNG"
				serious = true
			}
			parts = append(parts, strconv.Itoa(len(ids))+" "+shown+" ("+strings.Join(ids, ", ")+")")
		}
	}
	high, medium := 0, 0
	for _, i := range r.Issues {
		switch i.Severity {
		case "high":
			high++
		case "medium":
			medium++
		}
	}
	detail := strings.Join(parts, ", ") + "; " + strconv.Itoa(high) + " high, " + strconv.Itoa(medium) + " medium"
	level := SoftOK
	if serious || high > 0 {
		level = SoftWarn
	}
	return SoftCheck{id, level, detail}
}
