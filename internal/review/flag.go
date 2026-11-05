// Package review chứa các dữ kiện xác định cho review card: cờ, đọc brd/design, scope và SQL nháp.
package review

import (
	"math/big"
	"regexp"
	"strings"
)

// Level là mức của một cờ review.
type Level string

const (
	HIGH Level = "HIGH"
	WARN Level = "WARN"
	INFO Level = "INFO"
	NA   Level = "NA"
)

// ReviewFlag là một cờ của review card. Refs là id bước, đường dẫn hoặc tên bảng, đã sắp xếp; không bao giờ nil.
type ReviewFlag struct {
	Code    string   `json:"code"`
	Level   Level    `json:"level"`
	Message string   `json:"message"`
	Hint    string   `json:"hint"`
	Refs    []string `json:"refs"`
}

// NewFlag tạo cờ, sao chép refs (nil thành rỗng).
func NewFlag(code string, level Level, message, hint string, refs []string) ReviewFlag {
	return ReviewFlag{code, level, message, hint, append([]string{}, refs...)}
}

var natRe = regexp.MustCompile(`\d+|\D+`)

func isDigit(s string) bool { return s[0] >= '0' && s[0] <= '9' }

// NaturalCompare so sánh chuỗi có số theo giá trị số: S2 trước S10, BR-2 trước BR-10.
func NaturalCompare(a, b string) int {
	x, y := natRe.FindAllString(a, -1), natRe.FindAllString(b, -1)
	for i := 0; i < len(x) && i < len(y); i++ {
		p, q := x[i], y[i]
		var c int
		if isDigit(p) && isDigit(q) {
			bp, _ := new(big.Int).SetString(p, 10)
			bq, _ := new(big.Int).SetString(q, 10)
			c = bp.Cmp(bq)
		} else {
			c = strings.Compare(p, q)
		}
		if c != 0 {
			return c
		}
	}
	return strings.Compare(a, b)
}
