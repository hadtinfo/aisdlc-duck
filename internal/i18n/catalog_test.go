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

// scanFile quét một file đã parse: các lời gọi T/Tf có khóa literal (Args[0]) và chuỗi tiếng Việt trần. Chỉ literal
// nằm trong Args[0] của lời gọi T/Tf mà Args[0] là khóa literal (literal chuỗi hoặc chuỗi phép +) và literal trong
// struct tag mới được miễn; literal ở đối số khác, hay trong Args[0] không phải khóa literal, được xét như mọi
// literal khác. skipBare: bỏ phần chuỗi trần (en.go).
func scanFile(fset *token.FileSet, f *ast.File, rel string, skipBare bool) scan {
	var out scan
	exempt := map[*ast.BasicLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			if x.Tag != nil {
				exempt[x.Tag] = true
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "T" && sel.Sel.Name != "Tf") || len(x.Args) == 0 {
				return true
			}
			key, ok := literalKey(x.Args[0])
			if !ok {
				return true
			}
			ast.Inspect(x.Args[0], func(m ast.Node) bool {
				if bl, ok := m.(*ast.BasicLit); ok {
					exempt[bl] = true
				}
				return true
			})
			out.calls = append(out.calls, call{rel, fset.Position(x.Pos()).Line, key})
		}
		return true
	})
	if skipBare {
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING || exempt[bl] {
			return true
		}
		if s, err := strconv.Unquote(bl.Value); err == nil && viRe.MatchString(s) {
			out.bare = append(out.bare, bareLit{rel, fset.Position(bl.Pos()).Line, s})
		}
		return true
	})
	return out
}

// walkGo duyệt mọi file .go không phải _test.go của module, bỏ .git, node_modules, .superpowers, e2e và testdata.
func walkGo(t *testing.T, fn func(rel string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	root := moduleRoot(t)
	skip := map[string]bool{".git": true, "node_modules": true, ".superpowers": true, "e2e": true, "testdata": true}
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
		fn(rel, fset, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scanModule(t *testing.T) scan {
	t.Helper()
	var out scan
	walkGo(t, func(rel string, fset *token.FileSet, f *ast.File) {
		r := scanFile(fset, f, rel, rel == "internal/i18n/en.go")
		out.calls = append(out.calls, r.calls...)
		out.bare = append(out.bare, r.bare...)
	})
	return out
}

func scanSource(t *testing.T, src string) scan {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return scanFile(fset, f, "fixture.go", false)
}

func TestScannerFixture(t *testing.T) {
	const src = `package x

type S struct {
	A string ` + "`json:\"tên\"`" + `
}

func f(loc L) {
	_ = "trần"
	_ = i18n.T("Khóa " + "gộp")
	_ = i18n.Tf("Khóa %s", "đối số thứ hai")
	_ = i18n.T(prefix + "phần Việt")
	_ = loc.T("Khóa literal")
}
`
	s := scanSource(t, src)
	var bare []string
	for _, b := range s.bare {
		bare = append(bare, b.text)
	}
	sort.Strings(bare)
	if got, want := strings.Join(bare, "|"), "phần Việt|trần|đối số thứ hai"; got != want {
		t.Errorf("chuỗi trần = %q, muốn %q (tag bỏ qua, khóa literal được miễn)", got, want)
	}
	var keys []string
	for _, c := range s.calls {
		keys = append(keys, c.key)
	}
	sort.Strings(keys)
	if got, want := strings.Join(keys, "|"), "Khóa %s|Khóa gộp|Khóa literal"; got != want {
		t.Errorf("khóa = %q, muốn %q (a+b phải gộp, khóa không literal không tính)", got, want)
	}
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

func readAllow(t *testing.T) []allowEntry {
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

// waveOf xếp file vào đợt giao theo gói (xem mục "Chia đợt giao" của spec): aisdlc 2, aisdlc-harness 3, aisdlc-web 4.
func waveOf(path string) int {
	parts := strings.Split(path, "/")
	if parts[0] == "cmd" && len(parts) > 1 {
		switch parts[1] {
		case "aisdlc":
			return 2
		case "aisdlc-harness":
			return 3
		case "aisdlc-web":
			return 4
		}
		return 5
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

var pendingRe = regexp.MustCompile(`^chưa chuyển \(đợt (\d+)\)$`)

func TestNoBareVietnamese(t *testing.T) {
	s := scanModule(t)
	root := moduleRoot(t)
	byFile := map[string][]bareLit{}
	for _, b := range s.bare {
		byFile[b.file] = append(byFile[b.file], b)
	}
	allow := readAllow(t)

	if *updateAllowlist {
		// Chỉ gỡ dòng thừa (file không còn chuỗi trần hoặc không còn tồn tại); không bao giờ thêm dòng mới.
		var lines []string
		for _, e := range allow {
			if len(byFile[e.path]) > 0 {
				lines = append(lines, e.path+"  # "+e.reason)
			}
		}
		sort.Strings(lines)
		body := "# Allowlist chuỗi tiếng Việt trần. Mỗi dòng: <đường dẫn>  # <lý do>.\n" +
			"# Chỉ thêm tay, kèm lý do; `go test ./internal/i18n -run TestNoBareVietnamese -update-allowlist` chỉ gỡ dòng thừa.\n" +
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
		if m := pendingRe.FindStringSubmatch(e.reason); m != nil && m[1] != strconv.Itoa(waveOf(e.path)) {
			t.Errorf("allowlist: %s ghi đợt %s, spec xếp vào đợt %d", e.path, m[1], waveOf(e.path))
		}
	}
	for _, b := range s.bare {
		if !allowed[b.file] {
			t.Errorf("%s:%d: chuỗi tiếng Việt trần %q, hãy bọc bằng T/Tf hoặc thêm vào allowlist có lý do", b.file, b.line, b.text)
		}
	}
}

// TestWebDoesNotUsePackageLevelLocale giữ nguyên tắc: web không dùng locale toàn cục (i18n.T/Tf/Default/SetDefault);
// chỉ được gọi T/Tf trên một giá trị Locale.
func TestWebDoesNotUsePackageLevelLocale(t *testing.T) {
	banned := map[string]bool{"T": true, "Tf": true, "Default": true, "SetDefault": true}
	walkGo(t, func(rel string, fset *token.FileSet, f *ast.File) {
		if !strings.HasPrefix(rel, "internal/web/") && !strings.HasPrefix(rel, "cmd/aisdlc-web/") {
			return
		}
		for _, line := range webLocaleUses(fset, f, banned) {
			t.Errorf("%s:%d: web không được dùng %s của package i18n (locale toàn cục)", rel, line.line, line.name)
		}
	})
}

type webUse struct {
	line int
	name string
}

func webLocaleUses(fset *token.FileSet, f *ast.File, banned map[string]bool) []webUse {
	name := ""
	for _, im := range f.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p != "aisdlc/internal/i18n" {
			continue
		}
		name = "i18n"
		if im.Name != nil {
			name = im.Name.Name
		}
	}
	if name == "" || name == "_" || name == "." {
		return nil
	}
	var out []webUse
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && banned[sel.Sel.Name] {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name {
				out = append(out, webUse{fset.Position(sel.Pos()).Line, name + "." + sel.Sel.Name})
			}
		}
		return true
	})
	return out
}

func TestWebGuardDetectsAliasedUse(t *testing.T) {
	src := `package web
import loc "aisdlc/internal/i18n"
func f(l loc.Locale) { _ = loc.Tf("x"); _ = l.T("y"); loc.SetDefault(l); _ = loc.Default() }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "w.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := webLocaleUses(fset, f, map[string]bool{"T": true, "Tf": true, "Default": true, "SetDefault": true})
	if len(got) != 3 {
		t.Errorf("muốn 3 lần dùng bị cấm (Tf, SetDefault, Default), được %v", got)
	}
}
