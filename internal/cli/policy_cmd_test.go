package cli

import (
	"testing"

	"aisdlc/internal/risk"
)

// PolicyCommandTest (phần pull qua web ở web_cmd_test.go).

func policyRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.env["HOME"] = t.TempDir()
	return r
}

func TestShowListsDescribedRules(t *testing.T) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "policy", "show")
	contains(t, r.out, "Chính sách v0 (file) — mẫu generic v3\n",
		"Thang mức: low < medium < high (mặc định: low)\n",
		"Phân loại: devLower=reason\n",
		"Rule:\n  G1   high   Có dòng tiền hoặc giá trị đi qua (chuyển tiền, thanh toán, hoàn tiền, trừ hoặc cộng số dư)\n",
		"  G2   medium Đổi cấu trúc dữ liệu",
		"Yêu cầu theo mức:\n  low: (không có)\n",
		"  high: rule-pos-neg, boundary, idempotent, concurrent threads=8, invariant, coverage min=90\n")
	notContains(t, r.out, "agent=", "Nhãn cho impact.labels")
}

func TestPullWithoutWebExit2(t *testing.T) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	eq(t, r.run("policy", "pull"), 2)
	contains(t, r.err, "aisdlc web login")
}

func TestDiffPresetPrintsChanges(t *testing.T) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	// Java so với banking v3 chỉ có trong test resources; bản Go so v1 với bản mới nhất thật (v3: schema 2, thêm flow)
	y, err := risk.PresetYAML("banking", "1")
	must(t, err)
	r.write(policyFile, y)
	r.ok(noConsole(), "policy", "diff-preset")
	contains(t, r.out, "Mẫu banking v1 → v3: ", " thay đổi\n",
		"  SCHEMA_MIGRATED schema: 1 → 2\n", "  CHANGE_FLOW low/design: manual → auto (nới)\n",
		"Không tự áp dụng: chọn mục cần lấy rồi sửa aisdlc/risk-policy.yaml qua MR")
}

func TestDiffPresetUpToDate(t *testing.T) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	r.ok(noConsole(), "policy", "diff-preset")
	contains(t, r.out, "mới nhất")
}

func TestDiffPresetWithoutPresetExit2(t *testing.T) {
	r := policyRepo(t)
	r.ok(noConsole(), "setup")
	r.write(policyFile, "schema: 1\nlevels: [low, high]\ndefault: low\nrules: []\n")
	eq(t, r.run("policy", "diff-preset"), 2)
	contains(t, r.err, "Chính sách không ghi `preset: { name, version }`; không có mẫu để so.")
}
