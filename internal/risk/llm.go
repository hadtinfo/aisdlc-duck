package risk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"aisdlc/internal/model"
)

// LLMMatch là một luật khớp kèm câu trích dẫn từ đầu vào.
type LLMMatch struct {
	Rule     string `json:"rule"`
	Evidence string `json:"evidence"`
	File     string `json:"file"`
}

// LLMNotMatch là một luật không khớp kèm lý do.
type LLMNotMatch struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// LLMOverride ghi việc subagent chủ động lệch khỏi mức tính từ luật.
type LLMOverride struct {
	From   string `json:"from"`
	Reason string `json:"reason"`
}

// LLMResult là nội dung risk-agent.json.
type LLMResult struct {
	InputsHash string        `json:"inputsHash"`
	Level      string        `json:"level"`
	Matched    []LLMMatch    `json:"matched"`
	NotMatched []LLMNotMatch `json:"notMatched"`
	Override   *LLMOverride  `json:"override,omitempty"`
	Summary    string        `json:"summary"`
}

// InputFile là một file đầu vào của subagent; Rel tính từ gốc repo, dùng dấu "/".
type InputFile struct{ Rel, Content string }

func normContent(s string) string {
	return model.NFC(strings.ReplaceAll(s, "\r\n", "\n"))
}

// InputsHash băm chính sách và nội dung các file đầu vào (không phụ thuộc thứ tự).
func InputsHash(p RiskPolicy, files []InputFile) string {
	fs := append([]InputFile(nil), files...)
	sort.Slice(fs, func(i, j int) bool { return fs[i].Rel < fs[j].Rel })
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", p.Hash())
	for _, f := range fs {
		c := normContent(f.Content)
		fmt.Fprintf(h, "%d:%s\n%d:%s\n", len(f.Rel), f.Rel, len(c), c)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ParseLLMResult đọc JSON chặt (bỏ một hàng rào ```), từ chối trường lạ và chữ thừa.
func ParseLLMResult(raw string) (LLMResult, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		nl := strings.IndexByte(s, '\n')
		if nl < 0 || !strings.HasSuffix(s, "```") {
			return LLMResult{}, fmt.Errorf("risk-agent.json: hàng rào ``` không hợp lệ")
		}
		s = strings.TrimSpace(strings.TrimSuffix(s[nl+1:], "```"))
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	var r LLMResult
	if err := dec.Decode(&r); err != nil {
		return LLMResult{}, fmt.Errorf("risk-agent.json không phải JSON hợp lệ: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return LLMResult{}, fmt.Errorf("risk-agent.json có nội dung thừa sau JSON")
	}
	return r, nil
}

// RuleLevel là mức cao nhất trong các luật đã khớp (luật có và đang bật), không thấp hơn mức mặc định.
func RuleLevel(p RiskPolicy, r LLMResult) string {
	best := p.DefaultLevel
	for _, m := range r.Matched {
		for _, rule := range p.Rules {
			if rule.ID == m.Rule && rule.Enabled && p.Rank(rule.Level) > p.Rank(best) {
				best = rule.Level
			}
		}
	}
	return best
}

func normText(s string) string {
	return strings.Join(strings.Fields(model.NFC(s)), " ")
}

// ValidateLLMResult trả danh sách lỗi (rỗng nghĩa là hợp lệ).
func ValidateLLMResult(p RiskPolicy, r LLMResult, hash string, files []InputFile) []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }

	if r.InputsHash != hash {
		add("inputsHash không khớp đầu vào hiện tại (kết quả đã cũ, cần chạy lại)")
	}
	levelOK := p.Rank(r.Level) >= 0
	if !levelOK {
		add("mức '%s' không có trong thang %v", r.Level, p.Levels)
	}

	byID := map[string]RiskRule{}
	for _, rule := range p.Rules {
		byID[rule.ID] = rule
	}
	seen := map[string]bool{}
	see := func(id string) bool {
		if _, ok := byID[id]; !ok {
			add("luật '%s' không có trong chính sách", id)
			return false
		}
		if seen[id] {
			add("luật %s xuất hiện hai lần", id)
			return false
		}
		seen[id] = true
		return true
	}

	norm := map[string]string{}
	for _, f := range files {
		norm[f.Rel] = normText(f.Content)
	}
	for _, m := range r.Matched {
		if !see(m.Rule) {
			continue
		}
		text, ok := norm[m.File]
		if !ok {
			add("%s: file '%s' không thuộc đầu vào", m.Rule, m.File)
			continue
		}
		ev := normText(m.Evidence)
		if ev == "" || !strings.Contains(text, ev) {
			add("%s: không thấy câu trích trong %s", m.Rule, m.File)
		}
	}
	for _, n := range r.NotMatched {
		see(n.Rule)
	}
	for _, rule := range p.Rules {
		if rule.Enabled && !seen[rule.ID] {
			add("thiếu luật %s: phải nằm trong matched hoặc notMatched", rule.ID)
		}
	}

	if levelOK {
		rl := RuleLevel(p, r)
		if r.Level != rl && r.Override == nil {
			add("mức %s khác mức tính từ luật (%s) nhưng thiếu override", r.Level, rl)
		}
		if r.Override != nil {
			if r.Override.From != rl {
				add("override.from '%s' phải bằng mức tính từ luật (%s)", r.Override.From, rl)
			}
			if strings.TrimSpace(r.Override.Reason) == "" {
				add("override.reason rỗng")
			}
		}
	}
	if strings.TrimSpace(r.Summary) == "" {
		add("summary rỗng")
	}
	return errs
}
