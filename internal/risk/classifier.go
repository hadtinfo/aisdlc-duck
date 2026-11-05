package risk

import (
	"regexp"
	"strings"
)

// Match là một rule đã khớp. On: nội dung đã khớp (để hiện cho dev).
type Match struct {
	Rule  string `yaml:"rule" json:"rule"`
	Level string `yaml:"level" json:"level"`
	On    string `yaml:"on" json:"on"`
}

// RiskResult là kết quả phân loại. AgentLevel rỗng khi agent không đánh giá.
type RiskResult struct {
	Level      string
	RuleLevel  string
	Matched    []Match
	AgentLevel string
}

// Classify phân loại mức rủi ro theo spec §3.5: max(default, rule khớp, mức agent nếu bật).
func Classify(p RiskPolicy, in RiskInputs) RiskResult {
	ruleLevel := p.DefaultLevel
	matched := []Match{}
	for _, r := range p.Rules {
		if !r.Enabled || r.Kind == "" {
			continue
		}
		on, ok := firstMatch(r, in)
		if !ok {
			continue
		}
		matched = append(matched, Match{Rule: r.ID, Level: r.Level, On: on})
		if p.Rank(r.Level) > p.Rank(ruleLevel) {
			ruleLevel = r.Level
		}
	}
	level := ruleLevel
	agent := in.AgentLevel
	if p.Classification.Agent == AgentRaise && agent != "" && p.Rank(agent) >= 0 && p.Rank(agent) > p.Rank(level) {
		level = agent
	}
	return RiskResult{Level: level, RuleLevel: ruleLevel, Matched: matched, AgentLevel: agent}
}

func firstMatch(r RiskRule, in RiskInputs) (string, bool) {
	sample := in.SampleFile
	if sample == "" {
		sample = "aisdlcx"
	}
	for _, m := range r.Match {
		if strings.TrimSpace(m) == "" {
			continue
		}
		switch r.Kind {
		case MatchKeyword:
			if on, ok := keyword(m, in.Text); ok {
				return on, true
			}
		case MatchPath:
			for _, s := range in.Paths {
				if Overlaps(m, s, sample) {
					return s, true
				}
			}
		case MatchTable:
			for _, t := range in.Tables {
				if tableMatches(m, t) {
					return "bảng " + t, true
				}
			}
		case MatchEndpoint:
			for _, e := range in.Endpoints {
				if endpointMatches(m, e) {
					return e, true
				}
			}
		case MatchLabel:
			for _, l := range in.Labels {
				if l == m {
					return "agent gắn nhãn " + m, true
				}
			}
		}
	}
	return "", false
}

func keyword(regex, text string) (string, bool) {
	re, err := regexp.Compile("(?i)" + regex)
	if err != nil {
		return "", false
	}
	loc := re.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	return "“" + text[loc[0]:loc[1]] + "”", true
}

// tableMatches: "*" khớp mọi ký tự trừ dấu chấm.
func tableMatches(glob, name string) bool {
	return segmentRegex(glob, "[^.]*", ".").MatchString(strings.ToLower(name))
}

func segmentRegex(glob, star, anyChar string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("(?s)^")
	for _, c := range strings.ToLower(glob) {
		switch c {
		case '*':
			sb.WriteString(star)
		case '?':
			sb.WriteString(anyChar)
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.MustCompile(sb.String())
}

func endpointMatches(rule, endpoint string) bool {
	r, okR := splitEndpoint(rule)
	e, okE := splitEndpoint(endpoint)
	if !okR || !okE {
		return false
	}
	if r[0] != "" && r[0] != "*" && !strings.EqualFold(r[0], e[0]) {
		return false
	}
	return segments(pathSegments(r[1]), 0, pathSegments(e[1]), 0)
}

// splitEndpoint trả {method, path}; method rỗng khi chỉ có path.
func splitEndpoint(s string) ([2]string, bool) {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "/") {
		return [2]string{"", t}, true
	}
	i := strings.IndexByte(t, ' ')
	if i < 0 {
		return [2]string{}, false
	}
	return [2]string{t[:i], strings.TrimSpace(t[i+1:])}, true
}

var (
	edgeSlashRe = regexp.MustCompile(`^/+|/+$`)
	slashesRe   = regexp.MustCompile(`/+`)
)

func pathSegments(path string) []string {
	p := path
	if q := strings.IndexByte(p, '?'); q >= 0 {
		p = p[:q]
	}
	p = edgeSlashRe.ReplaceAllString(p, "")
	if p == "" {
		return nil
	}
	return slashesRe.Split(p, -1)
}

func segments(rule []string, i int, ep []string, j int) bool {
	if i == len(rule) {
		return j == len(ep)
	}
	if rule[i] == "**" {
		for k := j; k <= len(ep); k++ {
			if segments(rule, i+1, ep, k) {
				return true
			}
		}
		return false
	}
	if j == len(ep) || !segmentMatches(rule[i], ep[j]) {
		return false
	}
	return segments(rule, i+1, ep, j+1)
}

// segmentMatches: "{x}" bên nào cũng là một đoạn biến, khớp mọi đoạn.
func segmentMatches(rule, seg string) bool {
	if isVar(rule) || isVar(seg) {
		return true
	}
	return segmentRegex(rule, "[^/]*", "[^/]").MatchString(strings.ToLower(seg))
}

func isVar(s string) bool { return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") }
