package golang

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
)

func noCoverageData() string {
	return i18n.T("không có dữ liệu coverage (unit test và case đều không ghi)")
}

// ParseProfile đọc coverprofile (text) thành dòng → đã chạy, theo repo path.
// Import path bỏ tiền tố "<module>/"; file ngoài module bị bỏ.
func ParseProfile(r io.Reader, module string) (map[string]map[int]bool, error) {
	out := map[string]map[int]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	prefix := module + "/"
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// <file>:<l1>.<c1>,<l2>.<c2> <numStmt> <count>
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf(i18n.T("dòng profile sai: %q"), line)
		}
		file, rest := line[:colon], line[colon+1:]
		f := strings.Fields(rest)
		if len(f) != 3 {
			return nil, fmt.Errorf(i18n.T("dòng profile sai: %q"), line)
		}
		span := strings.SplitN(f[0], ",", 2)
		if len(span) != 2 {
			return nil, fmt.Errorf(i18n.T("dòng profile sai: %q"), line)
		}
		start, err1 := strconv.Atoi(strings.SplitN(span[0], ".", 2)[0])
		end, err2 := strconv.Atoi(strings.SplitN(span[1], ".", 2)[0])
		count, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf(i18n.T("dòng profile sai: %q"), line)
		}
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		path := strings.TrimPrefix(file, prefix)
		m := out[path]
		if m == nil {
			m = map[int]bool{}
			out[path] = m
		}
		for l := start; l <= end; l++ {
			m[l] = m[l] || count > 0
		}
	}
	return out, sc.Err()
}

// MergeLines gộp các bản đồ dòng bằng phép OR.
func MergeLines(maps ...map[string]map[int]bool) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	for _, in := range maps {
		for p, lines := range in {
			m := out[p]
			if m == nil {
				m = map[int]bool{}
				out[p] = m
			}
			for l, hit := range lines {
				m[l] = m[l] || hit
			}
		}
	}
	return out
}

func parseFile(path, module string) (map[string]map[int]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseProfile(f, module)
}

// Coverage gộp coverprofile của unit test với GOCOVERDIR của lúc chạy case.
func (Runtime) Coverage(ctx context.Context, repoRoot, outDir string, unitProfile, coverDir string) (lang.Coverage, error) {
	mod, err := modulePath(repoRoot)
	if err != nil {
		return lang.Coverage{NA: err.Error()}, nil
	}
	var parts []map[string]map[int]bool
	if unitProfile != "" {
		if _, serr := os.Stat(unitProfile); serr == nil {
			m, err := parseFile(unitProfile, mod)
			if err != nil {
				return lang.Coverage{NA: i18n.Tf("không đọc được coverprofile: %s", err.Error())}, nil
			}
			parts = append(parts, m)
		}
	}
	if coverDir != "" {
		if entries, _ := os.ReadDir(coverDir); len(entries) > 0 {
			goBin, err := exec.LookPath("go")
			if err != nil {
				return lang.Coverage{}, fmt.Errorf(i18n.T("không tìm thấy go trên PATH: %w"), err)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return lang.Coverage{}, err
			}
			cases := filepath.Join(outDir, "cases.cov")
			cmd := exec.CommandContext(ctx, goBin, "tool", "covdata", "textfmt", "-i", coverDir, "-o", cases)
			if b, err := cmd.CombinedOutput(); err != nil {
				msg := strings.TrimSpace(string(b))
				if msg == "" {
					msg = err.Error()
				}
				return lang.Coverage{NA: i18n.Tf("covdata lỗi: %s", msg)}, nil
			}
			m, err := parseFile(cases, mod)
			if err != nil {
				return lang.Coverage{NA: i18n.Tf("covdata lỗi: %s", err.Error())}, nil
			}
			parts = append(parts, m)
		}
	}
	if len(parts) == 0 {
		return lang.Coverage{NA: noCoverageData()}, nil
	}
	return lang.Coverage{Lines: MergeLines(parts...)}, nil
}
