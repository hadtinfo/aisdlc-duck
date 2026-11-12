package i18n

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateAllowlist = flag.Bool("update-allowlist", false, "ghi lại testdata/bare-vietnamese-allow.txt")

const allowPath = "testdata/bare-vietnamese-allow.txt"

var (
	verbRe = regexp.MustCompile(`%[-+# 0]*(\[\d+\])?\d*(\.\d+)?[a-zA-Z%]`)
	viRe   = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)
)

type call struct {
	file string
	line int
	key  string
}

type bareLit struct {
	file string
	line int
	text string
}

type scan struct {
	calls []call
	bare  []bareLit // chuỗi tiếng Việt trần, đã bỏ en.go và struct tag
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không tìm thấy go.mod")
		}
		dir = parent
	}
}

// literalKey gộp literal chuỗi hoặc phép + của các literal; ok=false nếu không phải.
func literalKey(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok1 := literalKey(x.X)
		r, ok2 := literalKey(x.Y)
		return l + r, ok1 && ok2
	case *ast.ParenExpr:
		return literalKey(x.X)
	}
	return "", false
}

func scanModule(t *testing.T) scan {
	t.Helper()
	root := moduleRoot(t)
	skip := map[string]bool{".git": true, "node_modules": true, ".superpowers": true, "e2e": true}
	var out scan
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		inCall := map[*ast.BasicLit]bool{} // literal nằm trong đối số lời gọi dịch
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Field:
				if x.Tag != nil {
					inCall[x.Tag] = true
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "T" && sel.Sel.Name != "Tf") || len(x.Args) == 0 {
					return true
				}
				for _, a := range x.Args {
					ast.Inspect(a, func(m ast.Node) bool {
						if bl, ok := m.(*ast.BasicLit); ok {
							inCall[bl] = true
						}
						return true
					})
				}
				if key, ok := literalKey(x.Args[0]); ok {
					out.calls = append(out.calls, call{rel, fset.Position(x.Pos()).Line, key})
				}
			}
			return true
		})
		if rel == "internal/i18n/en.go" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING || inCall[bl] {
				return true
			}
			if s, err := strconv.Unquote(bl.Value); err == nil && viRe.MatchString(s) {
				out.bare = append(out.bare, bareLit{rel, fset.Position(bl.Pos()).Line, s})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCatalogComplete(t *testing.T) {
	for _, c := range scanModule(t).calls {
		tr, ok := en[c.key]
		if !ok {
			t.Errorf("%s:%d: thiếu bản tiếng Anh cho khóa %q", c.file, c.line, c.key)
			continue
		}
		if a, b := strings.Join(verbRe.FindAllString(c.key, -1), "|"), strings.Join(verbRe.FindAllString(tr, -1), "|"); a != b {
			t.Errorf("%s:%d: động từ %% lệch ở khóa %q (khóa: %s, bản dịch: %s)", c.file, c.line, c.key, a, b)
		}
	}
}

func TestCatalogNoOrphans(t *testing.T) {
	used := map[string]bool{}
	for _, c := range scanModule(t).calls {
		used[c.key] = true
	}
	for k := range en {
		if !used[k] {
			t.Errorf("mục catalog thừa, không lời gọi literal nào dùng: %q", k)
		}
	}
}

type allowEntry struct{ path, reason string }

func readAllow(t *testing.T, root string) []allowEntry {
	t.Helper()
	f, err := os.Open(allowPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var out []allowEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, reason, _ := strings.Cut(line, "#")
		out = append(out, allowEntry{strings.TrimSpace(p), strings.TrimSpace(reason)})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// waveOf xếp file vào đợt giao theo gói (xem mục "Chia đợt giao" của spec).
func waveOf(path string) int {
	parts := strings.Split(path, "/")
	if parts[0] == "cmd" {
		return 2
	}
	if parts[0] != "internal" || len(parts) < 2 {
		return 5
	}
	switch parts[1] {
	case "cli", "policy", "gate", "step", "spec", "model", "snapshot", "gitx", "yamlx", "fsx":
		return 2
	case "harness", "eval", "review", "risk", "citest", "lang", "run", "webclient":
		return 3
	case "web":
		return 4
	}
	return 5
}

func TestNoBareVietnamese(t *testing.T) {
	s := scanModule(t)
	root := moduleRoot(t)
	byFile := map[string][]bareLit{}
	for _, b := range s.bare {
		byFile[b.file] = append(byFile[b.file], b)
	}
	allow := readAllow(t, root)

	if *updateAllowlist {
		var lines []string
		kept := map[string]bool{}
		for _, e := range allow {
			if _, bad := byFile[e.path]; bad && !strings.HasPrefix(e.reason, "chưa chuyển (đợt") {
				kept[e.path] = true
				lines = append(lines, e.path+"  # "+e.reason)
			}
		}
		for p := range byFile {
			if !kept[p] {
				lines = append(lines, fmt.Sprintf("%s  # chưa chuyển (đợt %d)", p, waveOf(p)))
			}
		}
		sort.Strings(lines)
		body := "# Allowlist chuỗi tiếng Việt trần. Mỗi dòng: <đường dẫn>  # <lý do>.\n# Sinh bằng: go test ./internal/i18n -run TestNoBareVietnamese -update-allowlist\n" +
			strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(allowPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	allowed := map[string]bool{}
	for _, e := range allow {
		allowed[e.path] = true
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.path))); err != nil {
			t.Errorf("allowlist trỏ tới file không tồn tại: %s", e.path)
		} else if len(byFile[e.path]) == 0 {
			t.Errorf("allowlist thừa, %s không còn chuỗi tiếng Việt trần: hãy xóa dòng", e.path)
		}
		if e.reason == "" {
			t.Errorf("allowlist thiếu lý do: %s", e.path)
		}
	}
	for _, b := range s.bare {
		if !allowed[b.file] {
			t.Errorf("%s:%d: chuỗi tiếng Việt trần %q, hãy bọc bằng T/Tf hoặc thêm vào allowlist có lý do", b.file, b.line, b.text)
		}
	}
}
