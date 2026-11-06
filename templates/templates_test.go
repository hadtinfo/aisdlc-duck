package templates

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var javaRe = regexp.MustCompile(`(?i)mvn|maven|junit|jacoco|flyway|src/main/java|pom\.xml|AisdlcCasesTest`)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := FS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTemplatesHaveNoJavaMentions(t *testing.T) {
	n := 0
	fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		if m := javaRe.FindString(read(t, p)); m != "" {
			t.Errorf("%s nhắc %q", p, m)
		}
		return nil
	})
	if n < 8 {
		t.Fatalf("chỉ thấy %d template", n)
	}
}

func TestDesignTemplateScopesAreGo(t *testing.T) {
	want := map[string][]string{
		"design.yaml":     {`["internal/limit/handler/**", "cmd/server/**"]`, `["internal/limit/service/**", "db/migrations/**"]`},
		"cases.lite.yaml": {`scope: ["internal/limit/handler/*.go"]`},
	}
	for f, ws := range want {
		for _, w := range ws {
			if !strings.Contains(read(t, f), w) {
				t.Errorf("%s thiếu scope %s", f, w)
			}
		}
	}
}

func TestCITemplateUsesGoAndDind(t *testing.T) {
	s := read(t, "gitlab-ci.aisdlc.yml")
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
	s := read(t, "CLAUDE.aisdlc.md")
	for _, w := range []string{"go test", "goose", "RFC 3339", "NULL khớp `null`", "db/migrations"} {
		if !strings.Contains(s, w) {
			t.Errorf("CLAUDE.aisdlc.md thiếu %q", w)
		}
	}
}
