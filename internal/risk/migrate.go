package risk

import "aisdlc/internal/i18n"

// legacyKindText là nhãn gợi ý cho từng loại khớp của luật schema 1, ghi vào describe để người đọc biết luật cũ
// khớp gì.
func legacyKindText(loc i18n.Locale, k MatchKind) (string, bool) {
	switch k {
	case MatchKeyword:
		return loc.T("từ khóa cũ"), true
	case MatchPath:
		return loc.T("đường dẫn cũ"), true
	case MatchTable:
		return loc.T("bảng cũ"), true
	case MatchEndpoint:
		return loc.T("endpoint cũ"), true
	case MatchLabel:
		return loc.T("nhãn cũ"), true
	}
	return "", false
}

// MigrateToV2 chuyển chính sách schema 1 sang schema 2: mỗi luật thành describe = note + gợi ý từ match, bỏ
// classification.agent và cột flow.brd, bỏ tham chiếu mẫu (không còn là bản mẫu nguyên vẹn). Schema 2 trả nguyên.
func MigrateToV2(p RiskPolicy) (RiskPolicy, error) { return MigrateToV2In(i18n.Default(), p) }

// MigrateToV2In như MigrateToV2, gợi ý trong describe và lỗi theo loc.
func MigrateToV2In(loc i18n.Locale, p RiskPolicy) (RiskPolicy, error) {
	if p.Schema == 2 {
		return p, nil
	}
	out := p
	out.Schema = 2
	out.Preset = nil
	out.Warnings = nil
	out.Classification = ClassificationRules{DevLower: p.Classification.DevLower}
	out.Rules = make([]RiskRule, len(p.Rules))
	for i, r := range p.Rules {
		r.Describe = legacyDescribe(loc, r)
		r.Kind, r.Match, r.Note = "", nil, ""
		out.Rules[i] = r
	}
	if p.Flow != nil {
		out.Flow = map[string]map[string]string{}
		for level, m := range p.Flow {
			c := map[string]string{}
			for u, v := range m {
				if u != "brd" {
					c[u] = v
				}
			}
			out.Flow[level] = c
		}
	}
	if err := out.UsableIn(loc); err != nil {
		return RiskPolicy{}, err
	}
	return out, nil
}

func legacyDescribe(loc i18n.Locale, r RiskRule) string {
	kind, ok := legacyKindText(loc, r.Kind)
	if !ok {
		kind = loc.T("luật cũ")
	}
	hint := ""
	for i, m := range r.Match {
		if i > 0 {
			hint += ", "
		}
		hint += m
	}
	suffix := kind
	if hint != "" {
		suffix += ": " + hint
	}
	if r.Note == "" {
		return suffix
	}
	return r.Note + " (" + suffix + ")"
}
