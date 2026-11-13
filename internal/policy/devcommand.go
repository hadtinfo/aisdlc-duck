package policy

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DevCommand là lệnh aisdlc chỉ dev được chạy (duyệt/từ chối/mở lại/cho phép phạm vi/nâng mức rủi ro).
type DevCommand struct {
	Verb string
	Args []string
}

var (
	devOnly = regexp.MustCompile(`\baisdlc\s+(approve|reject|reopen|rewind|scope\s+allow|step\s+accept|risk\s+set)\b`)
	// unsafeChars: ký tự/cấu trúc làm lệnh không còn là lệnh đơn hoặc bị shell khai triển.
	unsafeChars = regexp.MustCompile("[;&|`<>\\n\\r$\\\\*?\\[\\]{}~()#]|\\beval\\b")
	// tokenRe: một token: hoặc cả token nằm trong cặp nháy (không chứa nháy), hoặc không có nháy nào.
	tokenRe  = regexp.MustCompile(`^(?:"([^"']*)"|'([^"']*)'|([^\s"']+))`)
	verbRe   = regexp.MustCompile(`^(?:approve|reject|reopen|rewind|scope allow|step accept|risk set)$`)
	removeQ  = strings.NewReplacer("'", "", `"`, "")
	javaTrim = func(r rune) bool { return r <= 0x20 }
)

// javaIsWhitespace như Character.isWhitespace của Java.
func javaIsWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\u000B', '\f', '\r', 0x1C, 0x1D, 0x1E, 0x1F:
		return true
	case ' ', ' ', ' ':
		return false
	}
	return r > 0x7f && unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp) || r == ' '
}

func javaBlank(s string) bool {
	for _, r := range s {
		if !javaIsWhitespace(r) {
			return false
		}
	}
	return true
}

// DevCommandMentions: có nhắc tới lệnh chỉ dev ở bất kỳ đâu trong chuỗi bash (kể cả dạng lồng/ghép/có nháy).
func DevCommandMentions(bash string) bool {
	return devOnly.MatchString(removeQ.Replace(bash))
}

// ParseDevCommand chỉ nhận một lệnh đơn, launcher là đúng từ aisdlc; mọi dạng khác trả về false.
func ParseDevCommand(bash string) (DevCommand, bool) {
	if !DevCommandMentions(bash) {
		return DevCommand{}, false
	}
	s := strings.TrimFunc(bash, javaTrim)
	if s == "" || unsafeChars.MatchString(s) {
		return DevCommand{}, false
	}
	var tokens []string
	pos := 0
	for pos < len(s) {
		m := tokenRe.FindStringSubmatch(s[pos:])
		if m == nil {
			return DevCommand{}, false // nháy không cân hoặc nháy dính vào token
		}
		switch {
		case m[1] != "" || strings.HasPrefix(m[0], `"`):
			tokens = append(tokens, m[1])
		case m[2] != "" || strings.HasPrefix(m[0], "'"):
			tokens = append(tokens, m[2])
		default:
			tokens = append(tokens, m[3])
		}
		pos += len(m[0])
		if pos < len(s) {
			r, _ := utf8.DecodeRuneInString(s[pos:])
			if !javaIsWhitespace(r) {
				return DevCommand{}, false
			}
			for pos < len(s) {
				r, n := utf8.DecodeRuneInString(s[pos:])
				if !javaIsWhitespace(r) {
					break
				}
				pos += n
			}
		}
	}
	if len(tokens) < 2 || tokens[0] != "aisdlc" {
		return DevCommand{}, false
	}
	return DevCommandFromArgs(tokens[1:])
}

// DevCommandFromArgs: lệnh chỉ dev từ đối số dòng lệnh của CLI (không gồm aisdlc), cùng cách tách động từ
// như ParseDevCommand; lệnh không phải chỉ dev (kể cả risk reclassify) trả về false.
func DevCommandFromArgs(argv []string) (DevCommand, bool) {
	if len(argv) == 0 {
		return DevCommand{}, false
	}
	verb := argv[0]
	next := 1
	if len(argv) > 1 && verbRe.MatchString(verb+" "+argv[1]) {
		verb = verb + " " + argv[1]
		next = 2
	}
	if !verbRe.MatchString(verb) {
		return DevCommand{}, false
	}
	return DevCommand{Verb: verb, Args: append([]string{}, argv[next:]...)}, true
}

// Key là dạng đọc được: động từ và đối số cách nhau một dấu cách.
func (c DevCommand) Key() string {
	if len(c.Args) == 0 {
		return c.Verb
	}
	return c.Verb + " " + strings.Join(c.Args, " ")
}

// GrantKey là khóa so khớp phiếu duyệt: mảng JSON [verb, args...], phân biệt được ranh giới đối số;
// hook và CLI cùng dùng.
func (c DevCommand) GrantKey() string {
	a := append([]string{c.Verb}, c.Args...)
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(a)
	return strings.TrimSuffix(sb.String(), "\n")
}
