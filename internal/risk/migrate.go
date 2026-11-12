package risk

// Nhãn gợi ý cho từng loại khớp của luật schema 1, ghi vào describe để người đọc biết luật cũ khớp gì.
var legacyKindText = map[MatchKind]string{
	MatchKeyword:  "từ khóa cũ",
	MatchPath:     "đường dẫn cũ",
	MatchTable:    "bảng cũ",
	MatchEndpoint: "endpoint cũ",
	MatchLabel:    "nhãn cũ",
}

// MigrateToV2 chuyển chính sách schema 1 sang schema 2: mỗi luật thành describe = note + gợi ý từ match, bỏ
// classification.agent và cột flow.brd, bỏ tham chiếu mẫu (không còn là bản mẫu nguyên vẹn). Schema 2 trả nguyên.
func MigrateToV2(p RiskPolicy) (RiskPolicy, error) {
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
		r.Describe = legacyDescribe(r)
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
	if err := out.Usable(); err != nil {
		return RiskPolicy{}, err
	}
	return out, nil
}

func legacyDescribe(r RiskRule) string {
	kind, ok := legacyKindText[r.Kind]
	if !ok {
		kind = "luật cũ"
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
