package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/harness"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
)

// fakeVerifier thay harness của `aisdlc eval` (FakeMaven.verify của Java): xóa out, ghi results.json theo
// runner.cases, harness.log và schema.json như migration-check của fakeRunner.
type fakeVerifier struct {
	runner *fakeRunner
	calls  int
	err    error // khác nil: Verify trả lỗi này
}

func (f *fakeVerifier) Verify(_ context.Context, key model.FeatureKey, out string) (harness.VerifyResult, error) {
	f.calls++
	if f.err != nil {
		return harness.VerifyResult{}, f.err
	}
	if err := os.RemoveAll(out); err != nil {
		return harness.VerifyResult{}, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return harness.VerifyResult{}, err
	}
	cases := f.runner.cases(nil)
	code := 0
	for _, c := range cases {
		if c.Outcome != run.CaseOutcomePassed {
			code = 1
		}
	}
	if err := harness.WriteResults(out, key.String(), harness.ModeCases, cases); err != nil {
		return harness.VerifyResult{}, err
	}
	if err := os.WriteFile(filepath.Join(out, HarnessLog), []byte(f.runner.log), 0o644); err != nil {
		return harness.VerifyResult{}, err
	}
	s := run.NewSchemaSnapshot(map[string][]string{"transfer_limit_usage": {"used_amount:BIGINT:NOT NULL"}})
	if err := s.Write(filepath.Join(out, "schema.json")); err != nil {
		return harness.VerifyResult{}, err
	}
	return harness.VerifyResult{Build: lang.BuildResult{OK: true}, Run: harness.RunResult{ExitCode: code, Cases: cases},
		Coverage: lang.Coverage{NA: "fakeVerifier"}}, nil
}

// evalCmdFx như setUp của EvalCommandTest: setup đã merge từ trước (commit để CI chỉ thấy thay đổi của tính năng),
// S1 chờ dev chấp nhận.
func evalCmdFx(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "setup")
	r.setupDone = true
	r.s1AwaitingDev()
	return r
}

func (r *testRepo) c2Fails() {
	r.runner.cases = func([]string) []run.CaseResult {
		return []run.CaseResult{caseResult("C1", "S1", run.CaseOutcomePassed),
			caseResult("C2", "S2", run.CaseOutcomeFailed)}
	}
}

func (r *testRepo) evalReport() map[string]any {
	r.t.Helper()
	var m map[string]any
	must(r.t, json.Unmarshal([]byte(r.read("aisdlc/features/PAY-1/eval/report.json")), &m))
	return m
}

func TestEvalWritesReportAndExitsByResult(t *testing.T) {
	r := evalCmdFx(t)
	r.allCasesPass()
	if c := r.run("eval"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	eq(t, r.verifier.calls, 1)
	contains(t, r.out, "Eval PAY-1: ĐẠT", "Đã ghi aisdlc/features/PAY-1/eval/report.json.")
	eq(t, r.evalReport()["passed"], any(true))
	if !r.exists("aisdlc/features/PAY-1/eval/report.md") {
		t.Fatal("thiếu report.md")
	}
	contains(t, r.read("aisdlc/features/PAY-1/eval/diff-by-step.md"), "## S1 — Validate request",
		"+package handler")

	r.c2Fails()
	eq(t, r.run("eval"), 1)
	contains(t, r.out, "Eval PAY-1: KHÔNG ĐẠT", "C2")
}

func TestEvalCiEvaluatesChangedFeaturesWithoutActiveFeature(t *testing.T) {
	r := evalCmdFx(t)
	must(t, os.Remove(filepath.Join(r.root, ".aisdlc/active")))
	r.allCasesPass()
	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Eval PAY-1: ĐẠT")
	eq(t, r.evalReport()["mode"], any("ci"))

	r.c2Fails()
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "Eval PAY-1: KHÔNG ĐẠT")
}

func TestEvalCiWithoutChangedFeaturesDoesNothing(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")

	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Không có tính năng nào thay đổi so với HEAD; không có gì để eval.")
	eq(t, r.verifier.calls, 0)
}

func TestEvalLocalNeedsActiveFeature(t *testing.T) {
	r := evalCmdFx(t)
	must(t, os.Remove(filepath.Join(r.root, ".aisdlc/active")))
	// một tính năng đang mở thì tự dùng nó (spec 4.2); chỉ khi không còn tính năng nào mới báo thiếu
	must(t, os.Remove(filepath.Join(r.root, "aisdlc/features/PAY-1/feature.yaml")))
	eq(t, r.run("eval"), 2)
	contains(t, r.err, "Chưa có tính năng đang làm")
	eq(t, r.verifier.calls, 0)
}

func TestEvalCiCodeChangeWithoutFeatureFails(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")
	r.write("README.md", "chỉ tài liệu\n")
	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 { // unmanaged
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}

	r.write("internal/other/other.go", "package other\n")
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "Eval KHÔNG ĐẠT: thay đổi code không thuộc tính năng AISDLC nào so với HEAD: "+
		"internal/other/other.go. Tạo tính năng bằng `aisdlc init <KEY>` (hoặc đặt ciRequireFeature: false).")
	eq(t, r.verifier.calls, 0)
}

func TestEvalCiCodeChangeWithoutFeatureListsAtMostTenFiles(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		r.write("pkg/"+n+".go", "package pkg\n")
	}
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "pkg/a.go, pkg/b.go", "pkg/j.go … và 2 file khác. Tạo tính năng bằng `aisdlc init <KEY>`")
	notContains(t, r.out, "pkg/k.go")
}

func TestEvalCiCodeChangeWithoutFeatureAllowedWhenDisabled(t *testing.T) {
	r := evalCmdFx(t)
	// Java nối thêm khóa trùng (Jackson nhận); YAML của bản Go không nhận khóa trùng nên thay tại chỗ
	r.write("aisdlc/config.yaml", strings.Replace(r.read("aisdlc/config.yaml"), "ciRequireFeature: true",
		"ciRequireFeature: false", 1))
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature + config")
	r.write("internal/other/other.go", "package other\n")

	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "Không có tính năng nào thay đổi")
}

func TestEvalCiConfigChangeDisablingFeatureRequirementWithCodeFails(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")
	// cùng MR: tắt yêu cầu tính năng + thêm code — cấu hình của chính MR không được tin
	cfg := r.read("aisdlc/config.yaml")
	if !strings.Contains(cfg, "ciRequireFeature: true") {
		t.Fatal("config mẫu thiếu ciRequireFeature: true")
	}
	r.write("aisdlc/config.yaml", strings.Replace(cfg, "ciRequireFeature: true", "ciRequireFeature: false", 1))
	r.write("internal/other/other.go", "package other\n")

	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "Eval KHÔNG ĐẠT: aisdlc/config.yaml bị sửa trong thay đổi này — sửa cấu hình phải ở MR riêng.")
}

func TestEvalCiConfigChangeMakingEverythingUnmanagedWithCodeFails(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")
	cfg := regexp.MustCompile(`(?m)^unmanaged:.*$`).ReplaceAllString(r.read("aisdlc/config.yaml"), `unmanaged: ["**"]`)
	contains(t, cfg, `unmanaged: ["**"]`)
	r.write("aisdlc/config.yaml", cfg)
	r.write("internal/other/other.go", "package other\n")

	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "sửa cấu hình phải ở MR riêng")
}

func TestEvalCiUsesBaseConfigNotChangedOneEvenWithoutCodeChange(t *testing.T) {
	r := evalCmdFx(t)
	r.gitCmd("add", "-A")
	r.gitCmd("commit", "-q", "-m", "feature")
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"# chỉ comment\n")

	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "sửa cấu hình phải ở MR riêng")
}

func TestEvalCiReportWarnsAboutUnapprovedOrChangedGates(t *testing.T) {
	r := evalCmdFx(t)
	must(t, os.Remove(filepath.Join(r.root, ".aisdlc/active")))
	r.allCasesPass()
	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "| GATE:REVIEW | WARN | chưa duyệt REVIEW |")
	notContains(t, r.out, "GATE:BRD", "GATE:DESIGN", "GATE:CASES")

	r.write("aisdlc/features/PAY-1/brd.md", validBRD+"\nThêm dòng sau khi duyệt.\n")
	if c := r.run("eval", "--ci", "--base", "HEAD"); c != 0 {
		t.Fatalf("exit %d\n%s%s", c, r.out, r.err)
	}
	contains(t, r.out, "| GATE:BRD | WARN | BRD đã đổi sau khi duyệt |", "| GATE:REVIEW | WARN | chưa duyệt REVIEW |")
	if n := strings.Count(r.read("aisdlc/features/PAY-1/eval/report.md"), "\n"); n > 40 {
		t.Errorf("report.md %d dòng", n)
	}
}

// Giai đoạn này CI chưa có nguồn approval web: config approvals: web thì fail closed (GATE:WEB), không nhắc
// `aisdlc web login` (CI không bao giờ đăng nhập web).
func TestEvalCiWebModeFailsClosedWithoutLoginWarning(t *testing.T) {
	r := evalCmdFx(t)
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	r.gitCmd("add", "aisdlc/config.yaml")
	r.gitCmd("commit", "-q", "-m", "web mode")
	r.allCasesPass()

	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "GATE:WEB", "Eval PAY-1: KHÔNG ĐẠT")
	notContains(t, r.err, "Chưa `aisdlc web login`")
}

func TestEvalVerifierErrorIsReportedNotPanicked(t *testing.T) {
	r := evalCmdFx(t)
	r.verifier.err = errors.New("không ghi được .aisdlc/run/PAY-1")
	eq(t, r.run("eval"), 2)
	contains(t, r.err, "Lỗi: không ghi được .aisdlc/run/PAY-1")
}
