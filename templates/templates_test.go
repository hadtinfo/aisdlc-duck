package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
)

var locales = []i18n.Locale{i18n.VI, i18n.EN}

// viHashes là sha256 của template vi trước khi tách thư mục (config.yaml không nằm trong danh sách: có thêm locale).
var viHashes = map[string]string{
	"brd.md":               "08da51a68755b672d2ed30b41e0e4e0e39eb9adda2b76205a0b2723178440e46",
	"CLAUDE.aisdlc.md":     "76fcd17282e5da15653602dc2c5f8f94590ba816f738a8df0336a92b0b6a27b4",
	"design.md":            "2c55ce7454871432612c03a49237ed6ea40cc94d53c343003e5733327530292f",
	"cases.full.yaml":      "6fb181d39e485cf9379e5d8f51beea41afe7a68d0026643a2179b411c48883c0",
	"cases.lite.yaml":      "a07735bc240636569bf37abea9c01cbed8cc2f2c74b7c772fd1391ae40d97a69",
	"design.yaml":          "b8495e11c8e7678bc87b6baf36be61e1589cbbb37755147b0ad11a4a652163d0",
	"gitlab-ci.aisdlc.yml": "8393f047559a93d9e63c4e86bc1de3aaf7ecd9839c3100726606d2043fb869c6",
}

var javaRe = regexp.MustCompile(`(?i)mvn|maven|junit|jacoco|flyway|src/main/java|pom\.xml|AisdlcCasesTest`)

func read(t *testing.T, l i18n.Locale, name string) string {
	t.Helper()
	s, err := Read(l, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func eachLocale(t *testing.T, f func(t *testing.T, l i18n.Locale)) {
	for _, l := range locales {
		t.Run(string(l), func(t *testing.T) { f(t, l) })
	}
}

func TestTemplatesHaveNoJavaMentions(t *testing.T) {
	n := 0
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, err := FS.ReadFile(p)
		if err != nil {
			return err
		}
		if m := javaRe.FindString(string(b)); m != "" {
			t.Errorf("%s nhắc %q", p, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 16 {
		t.Fatalf("chỉ thấy %d template", n)
	}
}

func TestDesignTemplateScopesAreGo(t *testing.T) {
	want := map[string][]string{
		"design.yaml":     {`["internal/limit/handler/**", "cmd/server/**"]`, `["internal/limit/service/**", "db/migrations/**"]`},
		"cases.lite.yaml": {`scope: ["internal/limit/handler/*.go"]`},
	}
	eachLocale(t, func(t *testing.T, l i18n.Locale) {
		for f, ws := range want {
			for _, w := range ws {
				if !strings.Contains(read(t, l, f), w) {
					t.Errorf("%s thiếu scope %s", f, w)
				}
			}
		}
	})
}

func TestCITemplateUsesGoAndDind(t *testing.T) {
	eachLocale(t, func(t *testing.T, l i18n.Locale) { checkCI(t, read(t, l, "gitlab-ci.aisdlc.yml")) })
}

func checkCI(t *testing.T, s string) {
	t.Helper()
	for _, w := range []string{"image: golang:", "docker:dind", "DOCKER_HOST: tcp://docker:2375",
		"TESTCONTAINERS_HOST_OVERRIDE: docker", "TESTCONTAINERS_RYUK_DISABLED", "go.sum", "aisdlc eval --ci"} {
		if !strings.Contains(s, w) {
			t.Errorf("CI thiếu %q", w)
		}
	}
	if strings.Contains(s, "mvn") {
		t.Error("CI còn mvn")
	}
	// aisdlc chạy harness bằng binary riêng aisdlc-harness, tìm cạnh nó: job phải tải cả hai vào cùng thư mục.
	for _, w := range []string{"AISDLC_HARNESS_BIN", "-o /usr/local/bin/aisdlc-harness \"$AISDLC_HARNESS_BIN\""} {
		if !strings.Contains(s, w) {
			t.Errorf("CI thiếu %q", w)
		}
	}
}

func TestClaudeTemplateDescribesGoFlow(t *testing.T) {
	want := map[i18n.Locale][]string{
		i18n.VI: {"go test", "goose", "RFC 3339", "NULL khớp `null`", "db/migrations"},
		i18n.EN: {"go test", "goose", "RFC 3339", "NULL matches `null`", "db/migrations", "in English"},
	}
	eachLocale(t, func(t *testing.T, l i18n.Locale) {
		s := read(t, l, "CLAUDE.aisdlc.md")
		for _, w := range want[l] {
			if !strings.Contains(s, w) {
				t.Errorf("CLAUDE.aisdlc.md thiếu %q", w)
			}
		}
	})
}

func TestTemplatesHaveNoImpactBlock(t *testing.T) {
	eachLocale(t, func(t *testing.T, l i18n.Locale) {
		for _, n := range []string{"design.yaml", "cases.full.yaml", "cases.lite.yaml"} {
			if strings.Contains(read(t, l, n), "impact:") {
				t.Errorf("%s còn khối impact", n)
			}
		}
	})
}

func TestViTemplatesUnchanged(t *testing.T) {
	for name, want := range viHashes {
		sum := sha256.Sum256([]byte(read(t, i18n.VI, name)))
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("vi/%s đổi nội dung: sha256 %s, cần %s", name, got, want)
		}
	}
}

func names(t *testing.T, dir string) map[string]bool {
	t.Helper()
	es, err := fs.ReadDir(FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range es {
		out[e.Name()] = true
	}
	return out
}

func TestTemplateSetsMatch(t *testing.T) {
	vi, en := names(t, "vi"), names(t, "en")
	if len(vi) != 8 {
		t.Errorf("vi có %d file, cần 8", len(vi))
	}
	for n := range vi {
		if !en[n] {
			t.Errorf("en thiếu %s", n)
		}
	}
	for n := range en {
		if !vi[n] {
			t.Errorf("vi thiếu %s", n)
		}
	}
}

// vietnameseRe khớp chữ cái có dấu đặc trưng của tiếng Việt (không khớp mũi tên, dấu chấm giữa dùng trong template en).
var vietnameseRe = regexp.MustCompile(`[ăâđêôơưĂÂĐÊÔƠƯàáạảãằắặẳẵầấậẩẫèéẹẻẽềếệểễìíịỉĩòóọỏõồốộổỗờớợởỡùúụủũừứựửữỳýỵỷỹ]`)

func TestEnTemplatesHaveNoVietnamese(t *testing.T) {
	for n := range names(t, "en") {
		if m := vietnameseRe.FindString(read(t, i18n.EN, n)); m != "" {
			t.Errorf("en/%s có ký tự tiếng Việt %q", n, m)
		}
	}
}

func TestEnTemplatesPassValidators(t *testing.T) {
	brd := read(t, i18n.EN, "brd.md")
	for _, id := range []spec.SectionID{spec.SecGoal, spec.SecInScope, spec.SecOutOfScope, spec.SecRules,
		spec.SecAcceptance, spec.SecAssumptions, spec.SecOpenQuestions} {
		h := "\n## " + spec.Heading(id, i18n.EN) + "\n"
		if !strings.Contains(brd, h) {
			t.Errorf("en/brd.md thiếu mục %q", h)
		}
		brd = strings.Replace(brd, h, h+"Filled in.\n", 1)
	}
	i18n.SetDefault(i18n.EN)
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	for _, f := range spec.ValidateBRD(brd) {
		if f.Code == "BRD_MISSING_SECTION" || f.Code == "BRD_EMPTY_SECTION" {
			t.Errorf("ValidateBRD: %s %s", f.Code, f.Message)
		}
	}
	// design.md: các mục theo bảng ID (Assumptions, Open questions) và các mục riêng của thiết kế.
	design := read(t, i18n.EN, "design.md")
	for _, h := range []string{"Sequence", "API", "DB", spec.Heading(spec.SecAssumptions, i18n.EN),
		spec.Heading(spec.SecOpenQuestions, i18n.EN)} {
		if !strings.Contains(design, "\n## "+h+"\n") {
			t.Errorf("en/design.md thiếu mục %q", h)
		}
	}
	dir := t.TempDir()
	for _, n := range []string{"design.yaml", "cases.full.yaml", "cases.lite.yaml"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(read(t, i18n.EN, n)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d, err := spec.ReadDesign(filepath.Join(dir, "design.yaml"))
	if err != nil || len(d.Steps) != 2 {
		t.Errorf("design.yaml: %v, %d bước", err, len(d.Steps))
	}
	for _, n := range []string{"cases.full.yaml", "cases.lite.yaml"} {
		c, err := spec.ReadCases(filepath.Join(dir, n))
		if err != nil || len(c.Cases) == 0 {
			t.Errorf("%s: %v, %d case", n, err, len(c.Cases))
		}
	}
}

func TestConfigTemplatesHaveLocale(t *testing.T) {
	eachLocale(t, func(t *testing.T, l i18n.Locale) {
		s := read(t, l, "config.yaml")
		if !regexp.MustCompile(`(?m)^locale: ` + string(l) + `$`).MatchString(s) {
			t.Errorf("config.yaml thiếu dòng locale: %s", l)
		}
		c, err := model.ParseRepoConfig(s)
		if err != nil {
			t.Fatal(err)
		}
		if c.Locale != l {
			t.Errorf("Locale = %q, cần %q", c.Locale, l)
		}
	})
}
