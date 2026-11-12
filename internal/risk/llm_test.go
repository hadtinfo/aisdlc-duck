package risk

import (
	"strings"
	"testing"
)

func llmPolicy() RiskPolicy {
	return RiskPolicy{
		Schema: 2, Levels: []string{"low", "medium", "high"}, DefaultLevel: "low",
		Rules: []RiskRule{
			{ID: "G1", Level: "high", Describe: "d1", Enabled: true},
			{ID: "G2", Level: "medium", Describe: "d2", Enabled: true},
			{ID: "G3", Level: "high", Describe: "d3", Enabled: false},
		},
	}
}

func llmFiles() []InputFile {
	return []InputFile{{Rel: "brd.md", Content: "Hệ thống chuyển   tiền\nqua ngân hàng."}}
}

func llmGood(p RiskPolicy, files []InputFile) LLMResult {
	return LLMResult{
		InputsHash: InputsHash(p, files), Level: "high",
		Matched:    []LLMMatch{{Rule: "G1", Evidence: "chuyển tiền", File: "brd.md"}},
		NotMatched: []LLMNotMatch{{Rule: "G2", Reason: "không liên quan"}},
		Summary:    "tóm tắt",
	}
}

func hasMsg(msgs []string, frag string) bool {
	for _, m := range msgs {
		if strings.Contains(m, frag) {
			return true
		}
	}
	return false
}

func TestValidateGood(t *testing.T) {
	p, f := llmPolicy(), llmFiles()
	if m := ValidateLLMResult(p, llmGood(p, f), InputsHash(p, f), f); len(m) != 0 {
		t.Fatal(m)
	}
}

func TestValidateFailures(t *testing.T) {
	p, f := llmPolicy(), llmFiles()
	h := InputsHash(p, f)
	cases := []struct {
		name string
		mut  func(r *LLMResult)
		hash string
		frag string
	}{
		{"stale", func(r *LLMResult) {}, "other", "inputsHash không khớp"},
		{"level", func(r *LLMResult) { r.Level = "x" }, h, "mức 'x' không có trong thang"},
		{"unknown", func(r *LLMResult) {
			r.Matched = append(r.Matched, LLMMatch{Rule: "Z9", Evidence: "chuyển", File: "brd.md"})
		}, h, "luật 'Z9' không có trong chính sách"},
		{"missing", func(r *LLMResult) { r.NotMatched = nil }, h, "thiếu luật G2"},
		{"twice", func(r *LLMResult) { r.NotMatched = append(r.NotMatched, LLMNotMatch{Rule: "G1", Reason: "x"}) }, h, "G1 xuất hiện hai lần"},
		{"evidence", func(r *LLMResult) { r.Matched[0].Evidence = "không có đâu" }, h, "G1: không thấy câu trích trong brd.md"},
		{"file", func(r *LLMResult) { r.Matched[0].File = "x.md" }, h, "G1: file 'x.md' không thuộc đầu vào"},
		{"override missing", func(r *LLMResult) {
			r.Matched = nil
			r.NotMatched = append(r.NotMatched, LLMNotMatch{Rule: "G1", Reason: "x"})
		}, h, "mức high khác mức tính từ luật (low) nhưng thiếu override"},
		{"override from", func(r *LLMResult) { r.Level = "medium"; r.Override = &LLMOverride{From: "low", Reason: "r"} }, h, "override.from"},
		{"summary", func(r *LLMResult) { r.Summary = " " }, h, "summary rỗng"},
	}
	for _, c := range cases {
		r := llmGood(p, f)
		c.mut(&r)
		if m := ValidateLLMResult(p, r, c.hash, f); !hasMsg(m, c.frag) {
			t.Errorf("%s: want %q got %v", c.name, c.frag, m)
		}
	}
}

func TestValidateDisabledRuleIgnored(t *testing.T) {
	p, f := llmPolicy(), llmFiles()
	r := llmGood(p, f)
	r.NotMatched = append(r.NotMatched, LLMNotMatch{Rule: "G3", Reason: "tắt"})
	if m := ValidateLLMResult(p, r, InputsHash(p, f), f); len(m) != 0 {
		t.Fatal(m)
	}
	// a matched disabled rule does not raise the level
	r.Matched = append(r.Matched, LLMMatch{Rule: "G3", Evidence: "chuyển", File: "brd.md"})
	if got := RuleLevel(p, LLMResult{Matched: []LLMMatch{{Rule: "G3"}}}); got != "low" {
		t.Fatal(got)
	}
}

func TestRuleLevel(t *testing.T) {
	p := llmPolicy()
	if got := RuleLevel(p, LLMResult{Matched: []LLMMatch{{Rule: "G2"}, {Rule: "G1"}}}); got != "high" {
		t.Fatal(got)
	}
	if got := RuleLevel(p, LLMResult{}); got != "low" {
		t.Fatal(got)
	}
}

func TestValidateOverrideOK(t *testing.T) {
	p, f := llmPolicy(), llmFiles()
	r := llmGood(p, f)
	r.Level = "medium"
	r.Override = &LLMOverride{From: "high", Reason: "r"}
	if m := ValidateLLMResult(p, r, InputsHash(p, f), f); len(m) != 0 {
		t.Fatal(m)
	}
}

func TestValidateEvidenceNormalised(t *testing.T) {
	p := llmPolicy()
	f := []InputFile{{Rel: "brd.md", Content: "Chuyển tiền\r\nliên ngân hàng"}}
	r := llmGood(p, f)
	r.Matched[0].Evidence = "Chuye\u0302\u0309n  tiền liên\tngân hàng" // NFD
	if m := ValidateLLMResult(p, r, InputsHash(p, f), f); len(m) != 0 {
		t.Fatal(m)
	}
}

func TestParseLLMResultStripsFence(t *testing.T) {
	raw := "```json\n{\"inputsHash\":\"h\",\"level\":\"low\",\"matched\":[],\"notMatched\":[],\"summary\":\"s\"}\n```\n"
	r, err := ParseLLMResult(raw)
	if err != nil || r.Level != "low" || r.InputsHash != "h" {
		t.Fatal(r, err)
	}
	if _, err := ParseLLMResult("```\n{\"level\":\"low\"}\n```"); err != nil {
		t.Fatal(err)
	}
}

func TestParseLLMResultRejectsProse(t *testing.T) {
	for _, raw := range []string{"Đây là kết quả: {\"level\":\"low\"}", `{"level":"low","extra":1}`, `{"level":"low"} thêm`} {
		if _, err := ParseLLMResult(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestInputsHash(t *testing.T) {
	p := llmPolicy()
	a := []InputFile{{Rel: "a.md", Content: "x"}, {Rel: "b.md", Content: "y"}}
	b := []InputFile{a[1], a[0]}
	if InputsHash(p, a) != InputsHash(p, b) {
		t.Fatal("order matters")
	}
	if InputsHash(p, a) != InputsHash(p, []InputFile{{Rel: "a.md", Content: "x"}, {Rel: "b.md", Content: "y"}}) {
		t.Fatal("unstable")
	}
	if InputsHash(p, a) == InputsHash(p, []InputFile{a[0], {Rel: "b.md", Content: "z"}}) {
		t.Fatal("content ignored")
	}
	p2 := llmPolicy()
	p2.Rules[0].Describe = "đổi"
	if InputsHash(p, a) == InputsHash(p2, a) {
		t.Fatal("policy ignored")
	}
	if InputsHash(p, a) != InputsHash(p, []InputFile{{Rel: "a.md", Content: "x"}, {Rel: "b.md", Content: "y"}}) {
		t.Fatal()
	}
	if InputsHash(p, []InputFile{{Rel: "a.md", Content: "x\r\ny"}}) != InputsHash(p, []InputFile{{Rel: "a.md", Content: "x\ny"}}) {
		t.Fatal("CRLF")
	}
}
