package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

var (
	verbRe = regexp.MustCompile(`%[-+# 0]*(\[\d+\])?\d*(\.\d+)?[a-zA-Z%]`)
	// chữ tiếng Việt có dấu (so trên dạng NFC), cộng dấu tổ hợp U+0300–U+036F còn sót sau NFC
	viRe = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ\x{0300}-\x{036F}]`)
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

// marker là comment `// i18n:data <lý do>`: miễn literal tiếng Việt trên đúng một dòng (dòng của comment nếu comment
// đứng cuối dòng code, dòng ngay dưới nếu comment đứng riêng một dòng). Dành cho dữ liệu cố định không phải chữ hiển
// thị (bảng tiêu đề mục song ngữ, regex nhận cả hai ngôn ngữ).
type marker struct {
	file   string
	line   int // dòng của comment
	target int // dòng được miễn
	reason string
	used   bool
}

type scan struct {
	calls   []call
	bare    []bareLit // chuỗi tiếng Việt trần, đã bỏ en.go, struct tag và literal có marker i18n:data
	markers []marker  // mọi marker của file (cả marker thừa, thiếu lý do)
}

const dataMarker = "i18n:data"

// markersOf đọc các comment `// i18n:data <lý do>` của f; src là nội dung file để biết comment đứng riêng hay cuối dòng.
func markersOf(fset *token.FileSet, f *ast.File, src []byte, rel string) []marker {
	var out []marker
	for _, g := range f.Comments {
		for _, c := range g.List {
			text, ok := strings.CutPrefix(c.Text, "//")
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			rest, ok := strings.CutPrefix(text, dataMarker)
			if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
				continue
			}
			pos := fset.Position(c.Pos())
			lineStart := pos.Offset - (pos.Column - 1)
			standalone := strings.TrimSpace(string(src[lineStart:pos.Offset])) == ""
			target := pos.Line
			if standalone {
				target = pos.Line + 1
			}
			out = append(out, marker{file: rel, line: pos.Line, target: target, reason: strings.TrimSpace(rest)})
		}
	}
	return out
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

// testReportMethods là các method báo lỗi/ghi log của testing.TB: literal truyền thẳng vào chúng là câu cho người chạy
// test, không tới người dùng.
var testReportMethods = map[string]bool{"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "Log": true,
	"Logf": true, "Skip": true, "Skipf": true}

// isViText: literal (đã unquote) có chữ tiếng Việt, so trên dạng NFC (literal dán ở dạng NFD vẫn bị bắt).
func isViText(s string) bool { return viRe.MatchString(norm.NFC.String(s)) }

// testHelperExempt: literal truyền thẳng vào t.Fatal/Fatalf/Error/Errorf/Log/Logf/Skip/Skipf trong helper test dùng
// chung (file không phải _test.go). Chỉ áp khi package có tên kết thúc bằng "test" (webtest, harnesstest) và file
// import "testing"; receiver phải là tham số kiểu *testing.T/B/F hoặc testing.TB của hàm (hay func literal) bao quanh,
// hoặc trường `.t`/`.tb` của một biến (c.t). Receiver là tên package đã import (fmt, log, slog…) không bao giờ được miễn.
func testHelperExempt(f *ast.File) map[*ast.BasicLit]bool {
	if !strings.HasSuffix(f.Name.Name, "test") {
		return nil
	}
	imported := map[string]bool{}
	tname := ""
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := path.Base(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		imported[name] = true
		if p == "testing" {
			tname = name
		}
	}
	if tname == "" || tname == "_" || tname == "." {
		return nil
	}
	isTType := func(e ast.Expr) bool {
		if st, ok := e.(*ast.StarExpr); ok {
			sel, ok := st.X.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			id, ok := sel.X.(*ast.Ident)
			return ok && id.Name == tname && (sel.Sel.Name == "T" || sel.Sel.Name == "B" || sel.Sel.Name == "F")
		}
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == tname && sel.Sel.Name == "TB"
	}
	tParams := func(ft *ast.FuncType, into map[string]bool) {
		if ft == nil || ft.Params == nil {
			return
		}
		for _, fl := range ft.Params.List {
			if isTType(fl.Type) {
				for _, n := range fl.Names {
					into[n.Name] = true
				}
			}
		}
	}
	isTRecv := func(e ast.Expr, params map[string]bool) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return params[x.Name] && !imported[x.Name]
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			return ok && !imported[id.Name] && (x.Sel.Name == "t" || x.Sel.Name == "tb")
		}
		return false
	}
	out := map[*ast.BasicLit]bool{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		params := map[string]bool{}
		tParams(fd.Type, params)
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				tParams(x.Type, params) // t.Run(func(t *testing.T) {...})
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || !testReportMethods[sel.Sel.Name] || !isTRecv(sel.X, params) {
					return true
				}
				for _, a := range x.Args {
					if bl, ok := a.(*ast.BasicLit); ok {
						out[bl] = true
					}
				}
			}
			return true
		})
	}
	return out
}

// scanFile quét một file đã parse (kèm comment): các lời gọi T/Tf có khóa literal (Args[0]) và chuỗi tiếng Việt trần.
// Chỉ literal nằm trong Args[0] của lời gọi T/Tf mà Args[0] là khóa literal (literal chuỗi hoặc chuỗi phép +), literal
// trong struct tag, câu cho người chạy test trong helper test dùng chung (testHelperExempt), và literal trên dòng có marker `i18n:data <lý do>` mới được miễn; literal ở đối số khác, hay trong
// Args[0] không phải khóa literal, được xét như mọi literal khác. skipBare: bỏ phần chuỗi trần và marker (file catalog
// en.go, en_*.go).
func scanFile(fset *token.FileSet, f *ast.File, src []byte, rel string, skipBare bool) scan {
	var out scan
	exempt := testHelperExempt(f)
	if exempt == nil {
		exempt = map[*ast.BasicLit]bool{}
	}
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
	out.markers = markersOf(fset, f, src, rel)
	byLine := map[int]*marker{}
	for i := range out.markers {
		if m := &out.markers[i]; m.reason != "" {
			byLine[m.target] = m
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || (bl.Kind != token.STRING && bl.Kind != token.CHAR) || exempt[bl] {
			return true
		}
		if s, err := strconv.Unquote(bl.Value); err == nil && isViText(s) {
			line := fset.Position(bl.Pos()).Line
			if m := byLine[line]; m != nil {
				m.used = true
				return true
			}
			out.bare = append(out.bare, bareLit{rel, line, s})
		}
		return true
	})
	return out
}

// walkGo duyệt mọi file .go không phải _test.go của module, bỏ .git, node_modules, .superpowers, e2e và testdata. File
// được parse kèm comment; src là nội dung file.
func walkGo(t *testing.T, fn func(rel string, fset *token.FileSet, f *ast.File, src []byte)) {
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
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		fn(rel, fset, f, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scanModule(t *testing.T) scan {
	t.Helper()
	var out scan
	walkGo(t, func(rel string, fset *token.FileSet, f *ast.File, src []byte) {
		r := scanFile(fset, f, src, rel, isCatalogFile(rel))
		out.calls = append(out.calls, r.calls...)
		out.bare = append(out.bare, r.bare...)
		out.markers = append(out.markers, r.markers...)
	})
	return out
}

func scanSource(t *testing.T, src string) scan {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return scanFile(fset, f, []byte(src), "fixture.go", false)
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

// TestNoBareVietnamese: mọi literal tiếng Việt trong file .go không phải test (trừ catalog) phải nằm trong khóa T/Tf,
// hoặc trên dòng có marker `// i18n:data <lý do>`. Không có allowlist theo file. Marker thiếu lý do hay không miễn
// literal nào (thừa) cũng là lỗi.
func TestNoBareVietnamese(t *testing.T) {
	s := scanModule(t)
	for _, b := range s.bare {
		t.Errorf("%s:%d: chuỗi tiếng Việt trần %q: bọc bằng T/Tf, hoặc nếu là dữ liệu (không phải chữ hiển thị) thì ghi "+
			"`// i18n:data <lý do>` cuối dòng", b.file, b.line, b.text)
	}
	for _, m := range s.markers {
		switch {
		case m.reason == "":
			t.Errorf("%s:%d: marker i18n:data thiếu lý do", m.file, m.line)
		case !m.used:
			t.Errorf("%s:%d: marker i18n:data thừa, dòng %d không có literal tiếng Việt nào: hãy xóa", m.file, m.line, m.target)
		}
	}
}

// TestWebDoesNotUsePackageLevelLocale giữ nguyên tắc: web không dùng locale toàn cục (i18n.T/Tf/Default/SetDefault);
// chỉ được gọi T/Tf trên một giá trị Locale.
func TestWebDoesNotUsePackageLevelLocale(t *testing.T) {
	banned := map[string]bool{"T": true, "Tf": true, "Default": true, "SetDefault": true}
	walkGo(t, func(rel string, fset *token.FileSet, f *ast.File, _ []byte) {
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
	var out []webUse
	for _, im := range f.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p != "aisdlc/internal/i18n" {
			continue
		}
		name = "i18n"
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "." {
			// dot-import: T/Tf/Default/SetDefault gọi không cần tiền tố, AST không phân biệt được với hàm cùng tên
			// của package; cấm luôn import.
			out = append(out, webUse{fset.Position(im.Pos()).Line, "dot-import"})
		}
	}
	if name == "" || name == "_" || name == "." {
		return out
	}
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

func TestWebGuardDetectsDotImport(t *testing.T) {
	src := `package web
import . "aisdlc/internal/i18n"
func f() { _ = T("x"); SetDefault(Default()) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "w.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := webLocaleUses(fset, f, map[string]bool{"T": true, "Tf": true, "Default": true, "SetDefault": true})
	if len(got) != 1 || got[0].name != "dot-import" || got[0].line != 2 {
		t.Errorf("muốn một lỗi dot-import ở dòng 2, được %v", got)
	}
}

func TestScannerTestingHelper(t *testing.T) {
	const helper = `package xtest

import (
	"fmt"
	"log"
	"log/slog"
	"testing"
)

type client struct{ t testing.TB }

func h(t *testing.T, c *client) {
	t.Fatalf("thông báo cho người chạy test: %v", 1)
	t.Error("cũng được miễn")
	c.t.Fatalf("trường .t của biến cũng được miễn")
	t.Run("x", func(tt *testing.T) { tt.Fatal("tham số func literal cũng được miễn") })
	_ = fmt.Sprintf("không miễn: không phải method báo lỗi")
	t.Fatal(fmt.Sprint("lồng trong lời gọi khác thì không"))
	_ = fmt.Errorf("fmt.Errorf không miễn")
	log.Fatalf("log.Fatalf không miễn")
	slog.Error("slog.Error không miễn")
	var x interface{ Errorf(string, ...any) }
	x.Errorf("receiver không phải tham số testing thì không miễn")
}
`
	bareOf := func(src string) []string {
		var out []string
		for _, b := range scanSource(t, src).bare {
			out = append(out, b.text)
		}
		return out
	}
	want := "không miễn: không phải method báo lỗi|lồng trong lời gọi khác thì không|fmt.Errorf không miễn|" +
		"log.Fatalf không miễn|slog.Error không miễn|receiver không phải tham số testing thì không miễn"
	if got := strings.Join(bareOf(helper), "|"); got != want {
		t.Errorf("package xtest: chuỗi trần = %q, muốn %q", got, want)
	}
	// package không kết thúc bằng "test" (vd. fakeweb): không miễn gì, kể cả t.Fatalf
	if n := len(bareOf(strings.Replace(helper, "package xtest", "package fakeweb", 1))); n != 10 {
		t.Errorf("package fakeweb: muốn 10 chuỗi trần, được %d", n)
	}
	// package xtest nhưng "testing" không phải gói testing thật: không miễn
	notTesting := strings.Replace(helper, `"testing"`, `testing "example.com/notesting"`, 1)
	if n := len(bareOf(notTesting)); n != 10 {
		t.Errorf("không import testing: muốn 10 chuỗi trần, được %d", n)
	}
}

// Literal ở dạng NFD (chữ cái + dấu tổ hợp) và literal rune cũng bị bắt.
func TestScannerNFDAndRune(t *testing.T) {
	src := "package x\n\nvar a = \"Vie\u0323\u0302t Nam\"\nvar b = 'đ'\nvar c = 'a'\nvar d = \"ascii\"\n"
	var lines []int
	for _, b := range scanSource(t, src).bare {
		lines = append(lines, b.line)
	}
	if fmt.Sprint(lines) != "[3 4]" {
		t.Errorf("chuỗi trần ở dòng %v, muốn [3 4] (NFD và rune)", lines)
	}
}

func TestScannerDataMarker(t *testing.T) {
	const src = `package x

var a = "trailing có marker" // i18n:data bảng song ngữ
var b = "dòng sau không được miễn"

// i18n:data regex nhận cả hai ngôn ngữ
var c = "dưới marker đứng riêng"
var d = "hai dòng dưới marker thì không"

var e = "marker thiếu lý do" // i18n:data
var g = "ascii only" // i18n:data thừa vì không có literal tiếng Việt

var h = "marker sai tên" // i18n:dataX lý do
`
	s := scanSource(t, src)
	var bare []string
	for _, b := range s.bare {
		bare = append(bare, fmt.Sprintf("%d:%s", b.line, b.text))
	}
	if got, want := strings.Join(bare, "|"),
		"4:dòng sau không được miễn|8:hai dòng dưới marker thì không|10:marker thiếu lý do|13:marker sai tên"; got != want {
		t.Errorf("chuỗi trần = %q, muốn %q", got, want)
	}
	var ms []string
	for _, m := range s.markers {
		ms = append(ms, fmt.Sprintf("%d->%d used=%v reason=%q", m.line, m.target, m.used, m.reason))
	}
	want := []string{
		`3->3 used=true reason="bảng song ngữ"`,
		`6->7 used=true reason="regex nhận cả hai ngôn ngữ"`,
		`10->10 used=false reason=""`,
		`11->11 used=false reason="thừa vì không có literal tiếng Việt"`,
	}
	if got := strings.Join(ms, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("marker =\n%s\nmuốn\n%s", got, strings.Join(want, "\n"))
	}
}

// isCatalogFile: en.go và các catalog phụ en_<khu>.go (không tính file test) — khóa là chuỗi tiếng Việt nguyên văn.
func isCatalogFile(rel string) bool {
	dir, name := path.Split(rel)
	return dir == "internal/i18n/" && !strings.HasSuffix(name, "_test.go") &&
		(name == "en.go" || strings.HasPrefix(name, "en_") && strings.HasSuffix(name, ".go"))
}
