package gitx

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

// Diff liệt kê thay đổi file giữa hai tree (numstat + name-status, -M, -z).
func (r *Repo) Diff(ctx context.Context, from, to string) ([]FileChange, error) {
	type stat struct {
		added, deleted int
		binary         bool
	}
	stats := map[string]stat{}
	numOut, err := r.run(ctx, nil, "diff", "--numstat", "-M", "-z", from, to)
	if err != nil {
		return nil, err
	}
	num := strings.Split(numOut, "\x00")
	for i := 0; i < len(num); i++ {
		rec := num[i]
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\t", 3)
		if len(f) < 3 {
			return nil, &Error{Msg: "Đầu ra git diff --numstat không hợp lệ: " + rec}
		}
		path := f[2]
		if path == "" { // rename: old và new nằm ở hai phần tử tiếp theo
			if i+2 >= len(num) {
				return nil, &Error{Msg: "Đầu ra git diff --numstat không hợp lệ: " + rec}
			}
			path = num[i+2]
			i += 2
		}
		if f[0] == "-" {
			stats[path] = stat{binary: true}
			continue
		}
		a, err1 := strconv.Atoi(f[0])
		d, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			return nil, &Error{Msg: "Đầu ra git diff --numstat không hợp lệ: " + rec}
		}
		stats[path] = stat{added: a, deleted: d}
	}

	nsOut, err := r.run(ctx, nil, "diff", "--name-status", "-M", "-z", from, to)
	if err != nil {
		return nil, err
	}
	ns := strings.Split(nsOut, "\x00")
	var out []FileChange
	for i := 0; i < len(ns); i++ {
		status := ns[i]
		if status == "" {
			continue
		}
		bad := &Error{Msg: "Đầu ra git diff --name-status không hợp lệ: " + status}
		i++
		if i >= len(ns) {
			return nil, bad
		}
		path, oldPath := ns[i], ""
		c := status[0]
		if c == 'R' || c == 'C' {
			oldPath = path
			i++
			if i >= len(ns) {
				return nil, bad
			}
			path = ns[i]
		}
		var t ChangeType
		switch c {
		case 'A', 'C':
			t = Added
		case 'D':
			t = Deleted
		case 'R':
			t = Renamed
		default:
			t = Modified
		}
		s := stats[path]
		out = append(out, FileChange{Path: path, OldPath: oldPath, Type: t, Added: s.added, Deleted: s.deleted, Binary: s.binary})
	}
	return out, nil
}

// ChangedLines trả các dòng mới/sửa ở phía to, theo số dòng của phía đó.
// File không có dòng mới (ví dụ bị xóa) không xuất hiện.
func (r *Repo) ChangedLines(ctx context.Context, from, to string) (map[string]map[int]bool, error) {
	result := map[string]map[int]bool{}
	diff, err := r.run(ctx, nil, "-c", "core.quotepath=false", "diff", "-U0", "-M", "--no-color",
		"--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", from, to)
	if err != nil {
		return nil, err
	}
	current := ""
	hasCurrent := false
	// 0 = ngoài header, 1 = chờ "--- ", 2 = chờ "+++ ", 3 = trong thân diff
	state := 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			hasCurrent = false
			state = 1
		case state == 1 && strings.HasPrefix(line, "--- "):
			state = 2
		case state == 2 && strings.HasPrefix(line, "+++ "):
			state = 3
			current, hasCurrent = headerPath(line[4:])
			if hasCurrent && result[current] == nil {
				result[current] = map[int]bool{}
			}
		case state == 3 && hasCurrent && strings.HasPrefix(line, "@@"):
			if start, count, ok := parseHunk(line); ok {
				for n := start; n < start+count; n++ {
					result[current][n] = true
				}
			}
		}
	}
	for k, v := range result {
		if len(v) == 0 {
			delete(result, k)
		}
	}
	return result, nil
}

// parseHunk đọc "@@ -a[,b] +c[,d] @@..." và trả c, d (d mặc định 1).
func parseHunk(line string) (start, count int, ok bool) {
	rest, found := strings.CutPrefix(line, "@@ -")
	if !found {
		return 0, 0, false
	}
	oldR, rest, found := strings.Cut(rest, " +")
	if !found {
		return 0, 0, false
	}
	for _, p := range strings.SplitN(oldR, ",", 2) {
		if _, err := strconv.Atoi(p); err != nil {
			return 0, 0, false
		}
	}
	newR, _, found := strings.Cut(rest, " @@")
	if !found {
		return 0, 0, false
	}
	nums := strings.SplitN(newR, ",", 2)
	start, err := strconv.Atoi(nums[0])
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if len(nums) == 2 {
		if count, err = strconv.Atoi(nums[1]); err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

// headerPath lấy path từ header "+++ "; ok=false cho /dev/null (file bị xóa).
func headerPath(raw string) (string, bool) {
	p := raw
	if tab := strings.IndexByte(raw, '\t'); tab >= 0 {
		p = raw[:tab]
	}
	if p == "/dev/null" {
		return "", false
	}
	if len(p) >= 2 && strings.HasPrefix(p, "\"") && strings.HasSuffix(p, "\"") {
		p = unquote(p[1 : len(p)-1])
	}
	return strings.TrimPrefix(p, "b/"), true
}

// unquote giải mã chuỗi quote kiểu C của git (\t \n \r, \NNN bát phân là byte).
func unquote(q string) string {
	var out bytes.Buffer
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c != '\\' || i+1 >= len(q) {
			out.WriteByte(c)
			continue
		}
		i++
		n := q[i]
		if n >= '0' && n <= '7' {
			end := min(i+3, len(q))
			j, v := i, 0
			for j < end && q[j] >= '0' && q[j] <= '7' {
				v = v*8 + int(q[j]-'0')
				j++
			}
			out.WriteByte(byte(v))
			i = j - 1
			continue
		}
		switch n {
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		default:
			out.WriteByte(n)
		}
	}
	return out.String()
}
