package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ReviewCommandTest: tính năng PAY-1 lấy từ fixture của cardbuild (cùng bộ với Java, đã đổi sang Go và goose).

const cardFixtures = "../review/cardbuild/testdata"

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	must(t, filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		d := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(d, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(d, b, 0o644)
	}))
}

func reviewRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "init", "PAY-1")
	copyTree(t, filepath.Join(cardFixtures, "pay1"), filepath.Join(r.root, "aisdlc/features/PAY-1"))
	b, err := os.ReadFile(filepath.Join(cardFixtures, "00001_init.sql"))
	must(t, err)
	r.write("db/migrations/00001_init.sql", string(b))
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "pay1")
	return r
}

func TestPay1DesignPrintsTwoFlags(t *testing.T) {
	r := reviewRepo(t)
	t0 := time.Now()
	code := r.run("review", "design")
	ms := time.Since(t0).Milliseconds()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, r.err)
	}
	contains(t, r.out, "SCOPE_OVERLAP", "SCOPE_MATCHES_OLD_MIGRATION", "Review card — PAY-1 · cổng DESIGN")
	notContains(t, r.out, "\u001b")
	if ms >= 1000 {
		t.Fatalf("chậm: %d ms", ms)
	}
}

func TestJSONMatchesBuilder(t *testing.T) {
	r := reviewRepo(t)
	if code := r.run("review", "design", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.err)
	}
	var n struct {
		Gate  string            `json:"gate"`
		Flags []json.RawMessage `json:"flags"`
	}
	must(t, json.Unmarshal([]byte(r.out), &n))
	eq(t, n.Gate, "DESIGN")
	b, _ := json.Marshal(n.Flags)
	contains(t, string(b), "SCOPE_OVERLAP")
	// như Jackson INDENT_OUTPUT: "khóa" : giá trị, mảng trên một dòng, không có dòng [KEY]
	if !strings.HasPrefix(r.out, "{\n  \"gate\" : \"DESIGN\",\n") || !strings.HasSuffix(r.out, "}\n") {
		t.Fatalf("không giống Jackson:\n%s", r.out)
	}
	notContains(t, r.out, "[PAY-1]", `<`, `>`)
}

func TestRunsWithoutConsole(t *testing.T) {
	r := reviewRepo(t)
	if code := r.run("review", "brd"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.err)
	}
	contains(t, r.out, "cổng BRD")
}

func TestGateOutsideFlowIsUsageError(t *testing.T) {
	r := reviewRepo(t)
	eq(t, r.run("review", "nope"), 2)
}

func TestCasesCardHasNoAssumptionsSection(t *testing.T) {
	r := reviewRepo(t)
	if code := r.run("review", "cases"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.err)
	}
	contains(t, r.out, "cổng CASES")
	notContains(t, r.out, "Giả định", "ASSUMPTIONS_EMPTY")
}

func TestHugeLiteralNeverPrintsStackTrace(t *testing.T) {
	r := reviewRepo(t)
	r.write("db/migrations/00001_1_seed.sql", "INSERT INTO account VALUES ('1', '"+strings.Repeat("y", 200_000)+"');\n")
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "seed")
	if code := r.run("review", "design"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.err)
	}
	contains(t, r.out, "cổng DESIGN")
	notContains(t, r.out, "CARD_FAILED")
	notContains(t, r.err, "goroutine", "panic")
}

func TestJacksonIndent(t *testing.T) {
	got := string(jacksonIndent([]byte(`{"a":"x:,{[","b":[],"c":{},"d":[1,{"e":"\"}"}],"f":{"g":[null]}}`)))
	want := "{\n  \"a\" : \"x:,{[\",\n  \"b\" : [ ],\n  \"c\" : { },\n  \"d\" : [ 1, {\n    \"e\" : \"\\\"}\"\n  } ],\n" +
		"  \"f\" : {\n    \"g\" : [ null ]\n  }\n}"
	eq(t, got, want)
}
