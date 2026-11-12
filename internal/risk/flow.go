package risk

import (
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/review"
)

// FlowUnit là tên cổng trong khối flow; Review không có (trả "").
func FlowUnit(g gate.Gate) string {
	switch g {
	case gate.BRD:
		return "brd"
	case gate.DESIGN:
		return "design"
	case gate.CASES:
		return "cases"
	}
	return ""
}

// AutoGate quyết định tự duyệt theo mức rủi ro (spec risk-flow §5.1). Hàm thuần, không đọc file.
// Trả ("", false) khi được tự duyệt; (lý do, true) khi phải duyệt tay (true = có lý do, như Optional.isPresent của Java).
// manual == true nghĩa là cổng cần người duyệt tay; reason là lý do.
func AutoGate(p RiskPolicy, level string, g gate.Gate, hasHighFlag bool) (reason string, manual bool) {
	return AutoGateIn(i18n.Default(), p, level, g, hasHighFlag)
}

// AutoGateIn như AutoGate, lý do theo loc.
func AutoGateIn(loc i18n.Locale, p RiskPolicy, level string, g gate.Gate, hasHighFlag bool) (reason string, manual bool) {
	unit := FlowUnit(g)
	if unit == "" {
		return loc.T("Review luôn duyệt tay"), true
	}
	if g == gate.BRD {
		return loc.T("Cổng BRD luôn duyệt tay"), true
	}
	if hasHighFlag {
		return loc.T("Review card có cờ đỏ mức HIGH"), true
	}
	if p.FlowFor(level, unit) != "auto" {
		return loc.Tf("Mức %s: cổng %s đặt duyệt tay", level, unit), true
	}
	return "", false
}

// Stricter trả mức chặt hơn trong hai mức theo thang của p; "" bị bỏ qua. Mức không có trong thang thắng
// (không biết thì không tự duyệt: FlowFor của mức lạ là duyệt tay).
func Stricter(p RiskPolicy, a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if p.Rank(a) < 0 {
		return a
	}
	if p.Rank(b) < 0 {
		return b
	}
	if p.Rank(b) > p.Rank(a) {
		return b
	}
	return a
}

// LevelForGate là mức áp cho cổng khi quyết định tự duyệt (CLI chế độ local và server chế độ web dùng chung): mức
// trong risk.yaml. BRD và Review luôn duyệt tay nên trả "" (không có mức để áp).
func LevelForGate(p RiskPolicy, g gate.Gate, riskLevel string) string {
	if g == gate.REVIEW || g == gate.BRD {
		return ""
	}
	return riskLevel
}

// AutoStep: bước tự accept được không; flow.step chụp trong risk.yaml là auto; thiếu file/khóa là duyệt tay.
func AutoStep(r *RiskFile) bool { return r != nil && r.Flow["step"] == "auto" }

// AutoFromRiskFile: CI, risk.yaml đã chụp flow của mức cuối có cho phép cổng này tự duyệt không.
func AutoFromRiskFile(r *RiskFile, g gate.Gate) bool {
	unit := FlowUnit(g)
	return unit != "" && r != nil && r.Flow[unit] == "auto"
}

// HasHighFlag: review card có cờ đỏ mức HIGH không.
func HasHighFlag(flags []review.ReviewFlag) bool {
	for _, f := range flags {
		if f.Level == review.HIGH {
			return true
		}
	}
	return false
}

// GateName là tên cổng trong thông điệp, theo locale mặc định.
func GateName(g gate.Gate) string { return GateNameIn(i18n.Default(), g) }

// GateNameIn là tên cổng trong thông điệp, theo loc.
func GateNameIn(loc i18n.Locale, g gate.Gate) string {
	switch g {
	case gate.BRD:
		return "BRD"
	case gate.DESIGN:
		return loc.T("Thiết kế")
	case gate.CASES:
		return "Case"
	}
	return "Review"
}

// SupersedeMessage: thông điệp khi bản tự duyệt của gates bị thay thế vì mức đổi sang to. "Mức tăng" chỉ khi hạng
// mức thật sự tăng theo p so với from; ngược lại (flow của mức đổi, hoặc không biết mức trước) là "Flow của mức … đổi".
func SupersedeMessage(p *RiskPolicy, from, to string, gates []gate.Gate) string {
	return SupersedeMessageIn(i18n.Default(), p, from, to, gates)
}

// SupersedeMessageIn như SupersedeMessage, theo loc (web server truyền locale của request).
func SupersedeMessageIn(loc i18n.Locale, p *RiskPolicy, from, to string, gates []gate.Gate) string {
	names := make([]string, len(gates))
	for i, g := range gates {
		names[i] = GateNameIn(loc, g)
	}
	list := strings.Join(names, ", ")
	if p != nil && from != "" && p.Rank(from) >= 0 && p.Rank(to) > p.Rank(from) {
		return loc.Tf("Mức tăng %s → %s: cổng %s cần dev duyệt lại.", from, to, list)
	}
	return loc.Tf("Flow của mức %s đổi: cổng %s cần dev duyệt lại.", to, list)
}
