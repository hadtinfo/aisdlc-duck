package step

import (
	"fmt"
	"regexp"
	"strings"
)

const maxStarSegments = 8

// ScopeMatches cho biết relPath có khớp glob nào trong globs không (ngữ nghĩa glob của java.nio, thêm "**/" khớp 0 thư mục).
// Glob sai cú pháp được bỏ qua (không khớp); dùng CompileScopeErr để biết lỗi.
func ScopeMatches(globs []string, relPath string) bool {
	return CompileScope(globs)(relPath)
}

// CompileScope biên dịch sẵn mọi glob một lần để dùng lại cho nhiều file. Glob sai cú pháp bị bỏ qua.
func CompileScope(globs []string) func(string) bool {
	res := make([]*regexp.Regexp, 0, len(globs))
	for _, g := range globs {
		for _, v := range variants(g) {
			if re, err := globRegexp(v); err == nil {
				res = append(res, re)
			}
		}
	}
	return matcher(res)
}

// CompileScopeErr như CompileScope nhưng trả lỗi ngay khi có glob sai cú pháp.
func CompileScopeErr(globs []string) (func(string) bool, error) {
	var res []*regexp.Regexp
	for _, g := range globs {
		for _, v := range variants(g) {
			re, err := globRegexp(v)
			if err != nil {
				return nil, err
			}
			res = append(res, re)
		}
	}
	return matcher(res), nil
}

func matcher(res []*regexp.Regexp) func(string) bool {
	return func(relPath string) bool {
		p := normalizePath(relPath)
		for _, re := range res {
			if re.MatchString(p) {
				return true
			}
		}
		return false
	}
}

// normalizePath gộp "//" và bỏ "/" cuối, như Path.of của Java.
func normalizePath(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// variants: "**/" khớp cả 0 thư mục, thử mọi tổ hợp bỏ từng "**/".
func variants(glob string) []string {
	var at []int
	for i := strings.Index(glob, "**/"); i >= 0 && len(at) < maxStarSegments; {
		at = append(at, i)
		j := strings.Index(glob[i+3:], "**/")
		if j < 0 {
			break
		}
		i = i + 3 + j
	}
	seen := map[string]bool{}
	var out []string
	for mask := 0; mask < 1<<len(at); mask++ {
		var sb strings.Builder
		prev := 0
		for k, pos := range at {
			sb.WriteString(glob[prev:pos])
			if mask&(1<<k) == 0 {
				sb.WriteString("**/")
			}
			prev = pos + 3
		}
		sb.WriteString(glob[prev:])
		if s := sb.String(); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func classRune(sb *strings.Builder, r rune) {
	switch r {
	case '\\', ']', '[', '^', '-', '&', '~':
		sb.WriteByte('\\')
	}
	sb.WriteRune(r)
}

// globRegexp dịch glob sang regexp theo sun.nio.fs.Globs (Unix).
func globRegexp(glob string) (*regexp.Regexp, error) {
	g := []rune(glob)
	var sb strings.Builder
	sb.WriteString(`(?s)^`)
	inGroup := false
	next := func(i int) rune {
		if i < len(g) {
			return g[i]
		}
		return -1
	}
	for i := 0; i < len(g); {
		c := g[i]
		i++
		switch c {
		case '\\':
			if i == len(g) {
				return nil, fmt.Errorf("No character to escape: %s", glob)
			}
			sb.WriteString(regexp.QuoteMeta(string(g[i])))
			i++
		case '[':
			neg := false
			var cls strings.Builder
			if next(i) == '^' {
				cls.WriteString(`\^`)
				i++
			} else {
				if next(i) == '!' {
					neg = true
					i++
				}
				if next(i) == '-' {
					cls.WriteString(`\-`)
					i++
				}
			}
			hasRangeStart := false
			var last rune
			var cur rune = -1
			for i < len(g) {
				cur = g[i]
				i++
				if cur == ']' {
					break
				}
				if cur == '/' {
					return nil, fmt.Errorf("Explicit 'name separator' in class: %s", glob)
				}
				if cur == '-' {
					if !hasRangeStart {
						return nil, fmt.Errorf("Invalid range: %s", glob)
					}
					e := next(i)
					i++
					if e == -1 || e == ']' {
						cls.WriteString(`\-`)
						cur = e
						break
					}
					if e < last {
						return nil, fmt.Errorf("Invalid range: %s", glob)
					}
					cls.WriteByte('-')
					classRune(&cls, e)
					hasRangeStart = false
					cur = e
					continue
				}
				classRune(&cls, cur)
				hasRangeStart = true
				last = cur
			}
			if cur != ']' {
				return nil, fmt.Errorf("Missing ']': %s", glob)
			}
			if cls.Len() == 0 {
				return nil, fmt.Errorf("Empty character class: %s", glob)
			}
			if neg {
				sb.WriteString("[^/" + cls.String() + "]")
			} else {
				sb.WriteString("[" + cls.String() + "]")
			}
		case '{':
			if inGroup {
				return nil, fmt.Errorf("Cannot nest groups: %s", glob)
			}
			sb.WriteString("(?:(?:")
			inGroup = true
		case '}':
			if inGroup {
				sb.WriteString("))")
				inGroup = false
			} else {
				sb.WriteString(`\}`)
			}
		case ',':
			if inGroup {
				sb.WriteString(")|(?:")
			} else {
				sb.WriteByte(',')
			}
		case '*':
			if next(i) == '*' {
				sb.WriteString(".*")
				i++
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if inGroup {
		return nil, fmt.Errorf("Missing '}': %s", glob)
	}
	sb.WriteString(`$`)
	return regexp.Compile(sb.String())
}
