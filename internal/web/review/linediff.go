// Package review so các lần nộp cổng (diff dòng, gắn góp ý với thay đổi; port review/*.java). Gói lá: không phụ thuộc
// gates (gates dùng kiểu của gói này).
package review

import (
	"strconv"
	"strings"

	"aisdlc/internal/i18n"
)

const (
	diffContext = 3
	maxProduct  = 4_000_000
)

// Counts là số dòng thêm/xóa của một patch.
type Counts struct{ Added, Removed int }

// Op là một dòng của diff: Kind ' ' giống, '-' xóa, '+' thêm.
type Op struct {
	Kind byte
	Line string
}

// Unified là diff dòng kiểu git ("--- a/<path>", "+++ b/<path>", "@@ -l,n +l,n @@"), 3 dòng ngữ cảnh (LineDiff.unified).
// Giống nhau → "". a hoặc b nil = file vắng (/dev/null). Phần giữa (sau khi bỏ tiền tố/hậu tố chung) có tích số dòng
// > 4_000_000 → chỉ header + dòng "@@ file quá lớn để so @@" (theo loc).
func Unified(path string, a, b *string, loc i18n.Locale) string {
	var x, y []string
	if a != nil {
		x = Lines(*a)
	}
	if b != nil {
		y = Lines(*b)
	}
	if a != nil && b != nil && equalLines(x, y) {
		return ""
	}
	var out strings.Builder
	out.WriteString("--- ")
	if a == nil {
		out.WriteString("/dev/null")
	} else {
		out.WriteString("a/" + path)
	}
	out.WriteString("\n+++ ")
	if b == nil {
		out.WriteString("/dev/null")
	} else {
		out.WriteString("b/" + path)
	}
	out.WriteByte('\n')
	ops, ok := Diff(x, y)
	if !ok {
		out.WriteString(loc.T("@@ file quá lớn để so @@") + "\n")
		return out.String()
	}
	hunks(ops, &out)
	return out.String()
}

// Diff là chuỗi thao tác LCS biến x thành y (bỏ tiền tố/hậu tố chung trước khi so). ok = false khi phần giữa quá lớn
// (tích số dòng > 4_000_000).
func Diff(x, y []string) (ops []Op, ok bool) {
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	n, m := len(x)-pre-suf, len(y)-pre-suf
	if int64(n)*int64(m) > maxProduct {
		return nil, false
	}
	for i := 0; i < pre; i++ {
		ops = append(ops, Op{' ', x[i]})
	}
	w := m + 1
	t := make([]int32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[pre+i] == y[pre+j] {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[pre+i] == y[pre+j]:
			ops = append(ops, Op{' ', x[pre+i]})
			i++
			j++
		case j >= m || (i < n && t[(i+1)*w+j] >= t[i*w+j+1]):
			ops = append(ops, Op{'-', x[pre+i]})
			i++
		default:
			ops = append(ops, Op{'+', y[pre+j]})
			j++
		}
	}
	for k := 0; k < suf; k++ {
		ops = append(ops, Op{' ', x[len(x)-suf+k]})
	}
	return ops, true
}

func hunks(ops []Op, out *strings.Builder) {
	size := len(ops)
	oldPos, newPos := make([]int, size+1), make([]int, size+1)
	for k, o := range ops {
		oldPos[k+1], newPos[k+1] = oldPos[k], newPos[k]
		if o.Kind != '+' {
			oldPos[k+1]++
		}
		if o.Kind != '-' {
			newPos[k+1]++
		}
	}
	k := 0
	for k < size {
		for k < size && ops[k].Kind == ' ' {
			k++
		}
		if k >= size {
			break
		}
		start := max(0, k-diffContext)
		end := k // op thay đổi cuối cùng của hunk
		cur := k
		for cur < size {
			if ops[cur].Kind != ' ' {
				end = cur
				cur++
				continue
			}
			run := cur
			for run < size && ops[run].Kind == ' ' {
				run++
			}
			if run < size && run-cur <= 2*diffContext {
				cur = run
			} else {
				break
			}
		}
		stop := min(size, end+1+diffContext)
		oc, nc := oldPos[stop]-oldPos[start], newPos[stop]-newPos[start]
		os, ns := oldPos[start], newPos[start]
		if oc != 0 {
			os++
		}
		if nc != 0 {
			ns++
		}
		out.WriteString("@@ -" + strconv.Itoa(os) + "," + strconv.Itoa(oc) + " +" + strconv.Itoa(ns) + "," + strconv.Itoa(nc) + " @@\n")
		for q := start; q < stop; q++ {
			out.WriteByte(ops[q].Kind)
			out.WriteString(ops[q].Line)
			out.WriteByte('\n')
		}
		k = stop
	}
}

// Lines tách dòng theo '\n'; "" → không dòng; dòng cuối không có '\n' vẫn được giữ.
func Lines(s string) []string {
	var out []string
	for s != "" {
		nl := strings.IndexByte(s, '\n')
		if nl < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:nl])
		s = s[nl+1:]
	}
	return out
}

func equalLines(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// CountLines đếm dòng '+'/'-' của phần thân patch (bỏ header "---"/"+++" và dòng "@@") (LineDiff.counts).
func CountLines(patch string) Counts {
	var c Counts
	inHunk := false
	for _, l := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(l, "+"):
			c.Added++
		case inHunk && strings.HasPrefix(l, "-"):
			c.Removed++
		}
	}
	return c
}
