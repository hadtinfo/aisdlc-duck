package cli

import (
	"testing"

	"aisdlc/internal/risk"
)

func schema1Repo(t *testing.T) (*testRepo, string) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	y, err := risk.PresetYAML("banking", "2")
	must(t, err)
	r.write(policyFile, y)
	return r, y
}

func TestPolicyMigrateDryRun(t *testing.T) {
	r, y := schema1Repo(t)
	r.ok(noConsole(), "policy", "migrate")
	contains(t, r.out, "SCHEMA_MIGRATED", "--write")
	eq(t, r.read(policyFile), y)
}

func TestPolicyMigrateWrite(t *testing.T) {
	r, _ := schema1Repo(t)
	r.ok(noConsole(), "policy", "migrate", "--write")
	got := r.read(policyFile)
	contains(t, got, "schema: 2", "từ khóa cũ: hạch toán|bút toán|định khoản")
	p, err := risk.ParsePolicy(got)
	must(t, err)
	eq(t, p.Schema, 2)
	must(t, p.Usable())
	// chạy lại: đã schema 2
	r.ok(noConsole(), "policy", "migrate", "--write")
	contains(t, r.out, "đã là schema 2")
}

func TestPolicyMigrateWebModeExit2(t *testing.T) {
	r, _, _ := policyWeb(t)
	r.ok(noConsole(), "policy", "pull")
	eq(t, r.run("policy", "migrate"), 2)
	contains(t, r.err, "Chế độ web: chuyển chính sách trên trang chính sách của web.")
}

func TestShowSchema2(t *testing.T) {
	r := policyRepo(t)
	r.presetV3 = true
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "policy", "show")
	contains(t, r.out, "Phân loại: devLower=reason\n",
		"  G1   high   Có dòng tiền hoặc giá trị đi qua (")
	if len(r.out) > 0 && containsStr(r.out, "agent=") {
		t.Errorf("schema 2 không in agent=:\n%s", r.out)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
