package cli

import (
	"os"
	"strings"

	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
)

// FeatureRisk của Java: phân loại rủi ro một tính năng theo chính sách hiện tại và mức tạm của BRD. Event rủi ro gửi
// web (RISK_CLASSIFIED, RISK_OVERRIDDEN) ở webpublish.go.

// classifyRisk là bản chụp mới (chưa override) theo luồng: FULL dùng BRD, thiết kế, migration nháp; LITE dùng
// cases.yaml.
func classifyRisk(p risk.RiskPolicy, f *Feature) (risk.RiskFile, error) {
	title := f.Meta.Title
	if f.Flow() == model.FlowLite {
		var impact *spec.Impact
		if exists(f.Paths.Cases()) {
			cs, err := spec.ReadCases(f.Paths.Cases())
			if err != nil {
				return risk.RiskFile{}, err
			}
			impact = cs.Impact
		}
		in, err := risk.CollectLite(f.Paths, title, f.Adapter)
		if err != nil {
			return risk.RiskFile{}, err
		}
		return risk.NewRiskFile(p, risk.Classify(p, in), impact), nil
	}
	var impact *spec.Impact
	if exists(f.Paths.DesignYaml()) {
		d, err := spec.ReadDesign(f.Paths.DesignYaml())
		if err != nil {
			return risk.RiskFile{}, err
		}
		impact = d.Impact
	}
	in, err := risk.CollectFull(f.Paths, title, f.Adapter)
	if err != nil {
		return risk.RiskFile{}, err
	}
	return risk.NewRiskFile(p, risk.Classify(p, in), impact), nil
}

// brdPreview là mức tạm chỉ bằng rule keyword trên tiêu đề và brd.md (không ghi file).
func brdPreview(p risk.RiskPolicy, f *Feature) (risk.RiskResult, error) {
	brd := ""
	if exists(f.Paths.Brd()) {
		b, err := os.ReadFile(f.Paths.Brd())
		if err != nil {
			return risk.RiskResult{}, err
		}
		brd = string(b)
	}
	return risk.Classify(p, risk.CollectBrdOnly(f.Meta.Title, brd)), nil
}

// withBrdPreview là thẻ BRD kèm riskPreview; không đọc được chính sách thì giữ nguyên thẻ. Không lỗi, không in.
func withBrdPreview(ctx *Context, f *Feature, card review.ReviewCard) (out review.ReviewCard) {
	if card.Gate != "BRD" {
		return card
	}
	defer func() {
		if recover() != nil {
			out = card
		}
	}()
	l, ok := TryLoadPolicy(ctx, f.Root)
	if !ok {
		return card
	}
	r, err := brdPreview(l.Policy, f)
	if err != nil {
		return card
	}
	ms := make([]review.RiskMatch, 0, len(r.Matched))
	for _, m := range r.Matched {
		ms = append(ms, review.RiskMatch{Rule: m.Rule, Level: m.Level, On: m.On})
	}
	return card.WithRiskPreview(review.RiskPreview{Level: r.Level, Matched: ms})
}

// matchedText là "R4 (bảng order_item), G1 (…)" để in.
func matchedText(ms []risk.Match) string {
	if len(ms) == 0 {
		return "không rule nào khớp"
	}
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = m.Rule + " (" + m.On + ")"
	}
	return strings.Join(parts, ", ")
}
