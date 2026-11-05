package policy

import (
	"regexp"
	"strings"
)

var (
	// bashWrite: lệnh ghi (không tính chuyển hướng >, xét riêng theo đích của nó).
	bashWrite = regexp.MustCompile(`\btee\b|\bsed\s+-i|\brm\b|\bmv\b|\bcp\b|\btruncate\b` +
		`|\bgit\s+(checkout|restore|stash|reset)\b|\bperl\b|\bpython3?\b|\bdd\b`)
	// bashRedirect: chuyển hướng ghi >, >>, N>, &>, >| cùng đích của nó.
	bashRedirect = regexp.MustCompile(`(?:\d+|&)?>>?\|?\s*(&\d+-?|&-|"[^"]*"|'[^']*'|[^\s;&|<>()]+)`)
	// runDir: log/kết quả chạy của CLI: agent được đọc.
	runDir   = regexp.MustCompile(`[^\s;&|<>()'"]*\.aisdlc/run/[^\s;&|<>()'"]*`)
	bashMsg  = "): file trạng thái do CLI quản lý hoặc artifact đã duyệt. Nếu cần sửa artifact, dev chạy `aisdlc reopen <gate>`."
	devMsg   = "Lệnh này chỉ dev được chạy (approve/reject/reopen/scope allow/step accept/risk set); agent không được chạy qua Bash."
	sessMsg  = "Agent không được gán AISDLC_SESSION (đó là định danh session do hook đặt; đổi nó để lách tính năng của session). Muốn đổi tính năng, chạy `aisdlc use <KEY>`."
	sessAsgn = regexp.MustCompile("(?:^|[\\s;&|(\"'`])AISDLC_SESSION\\+?=")
)

// BashCheck trả lý do chặn (và true) nếu lệnh Bash có vẻ ghi vào file được bảo vệ hoặc là lệnh chỉ dev.
// repoRoot không dùng (giữ chữ ký như Java).
func BashCheck(command string, protectedPaths []string, repoRoot string) (string, bool) {
	if javaBlank(command) {
		return "", false
	}
	if DevCommandMentions(command) {
		return devMsg, true
	}
	if sessAsgn.MatchString(command) {
		return sessMsg, true
	}
	// Chuyển hướng chỉ là ghi khi đích là file được bảo vệ (bỏ qua N>&M, /dev/null, file khác)
	var rest strings.Builder
	last := 0
	for _, m := range bashRedirect.FindAllStringSubmatchIndex(command, -1) {
		target := unquote(command[m[2]:m[3]])
		if !strings.HasPrefix(target, "&") && target != "/dev/null" {
			if p, ok := protectedIn(target, protectedPaths); ok {
				return "Lệnh có vẻ ghi vào file được bảo vệ (" + p + bashMsg, true
			}
		}
		rest.WriteString(command[last:m[0]])
		rest.WriteByte(' ')
		last = m[1]
	}
	rest.WriteString(command[last:])
	others := runDir.ReplaceAllString(rest.String(), " ")
	if bashWrite.MatchString(others) {
		if p, ok := protectedIn(others, protectedPaths); ok {
			return "Lệnh có vẻ ghi vào file được bảo vệ (" + p + bashMsg, true
		}
	}
	return "", false
}

func protectedIn(text string, protectedPaths []string) (string, bool) {
	for _, p := range protectedPaths {
		if p != "" && strings.Contains(text, p) {
			return p, true
		}
	}
	return "", false
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
