package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// Nguồn chính sách của `risk inputs` theo chế độ duyệt có hiệu lực (như eval --ci), không theo việc máy đã đăng nhập.

// riskWebRepo: repo banking, tính năng PAY-1 (brd.md + migration nháp), máy đã `aisdlc web login` với server giả
// trả chế độ serverMode và chính sách generic v7 (khác file trong repo). configWeb: config.yaml ghi approvals: web.
func riskWebRepo(t *testing.T, configWeb bool, serverMode string) (*testRepo, *fakeweb.Server) {
	r := bankingRepo(t)
	r.noAutoLLM = true
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.write("aisdlc/features/PAY-1/db/00100_ledger.sql", ledgerSQL)
	if configWeb {
		r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	}
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-1")
	web.Update(func(s *fakeweb.Server) {
		s.Mode = serverMode
		s.PolicyYAML, s.PolicyVersion = presetVersionYAML(t, "generic", "3"), 7
	})
	home := t.TempDir()
	r.env["HOME"] = home
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return r, web
}

// inputsHash chạy `risk inputs` và trả giá trị dòng inputsHash.
func (r *testRepo) inputsHash() string {
	r.t.Helper()
	r.ok(noConsole(), "risk", "inputs")
	lines := r.outLines()
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "inputsHash: ") {
		r.t.Fatalf("thiếu dòng inputsHash:\n%s", r.out)
	}
	return strings.TrimPrefix(last, "inputsHash: ")
}

// hashWith là inputsHash của PAY-1 với chính sách YAML (version > 0: phiên bản trên web như LoadPolicy đặt).
func (r *testRepo) hashWith(yaml string, version int) string {
	r.t.Helper()
	p, err := risk.ParsePolicy(yaml)
	must(r.t, err)
	if version > 0 {
		p.Version = version
	}
	_, files, _ := r.llmInputs("PAY-1")
	return risk.InputsHash(p, files)
}

// ciLocalHash là inputsHash eval --ci tính ở chế độ local (localEvalPolicy).
func (r *testRepo) ciLocalHash() string {
	r.t.Helper()
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	f, err := ctx.FeatureAt(r.root, model.FeatureKey("PAY-1"))
	must(r.t, err)
	p, perr := localEvalPolicy(ctx, f)
	if p == nil {
		r.t.Fatal(perr)
	}
	_, files, _ := r.llmInputs("PAY-1")
	return risk.InputsHash(*p, files)
}

func TestRiskInputsLoggedInConfigLocalUsesRepoFile(t *testing.T) {
	r, _ := riskWebRepo(t, false, "local")
	got := r.inputsHash()
	eq(t, got, r.hashWith(r.read(RepoPolicyFile), 0))
	eq(t, got, r.ciLocalHash())
}

func TestRiskInputsLoggedInPinnedLocalUsesRepoFile(t *testing.T) {
	r, _ := riskWebRepo(t, true, "local")
	got := r.inputsHash()
	eq(t, got, r.hashWith(r.read(RepoPolicyFile), 0))
	eq(t, got, r.ciLocalHash())
}

func TestRiskInputsWebModeRefreshesPolicyEachRun(t *testing.T) {
	r, web := riskWebRepo(t, true, "web")
	v7 := presetVersionYAML(t, "generic", "3")
	eq(t, r.inputsHash(), r.hashWith(v7, 7))
	// admin xuất bản bản mới: lần chạy sau lấy lại, không dùng cache cũ
	v8 := strings.Replace(v7, "Có dòng tiền", "Có dòng tiền (bản 8)", 1)
	web.Update(func(s *fakeweb.Server) { s.PolicyYAML, s.PolicyVersion = v8, 8 })
	eq(t, r.inputsHash(), r.hashWith(v8, 8))
	// web không truy cập được mà có cache: dùng cache, cảnh báo trên stderr
	web.Update(func(s *fakeweb.Server) { s.Drop = true })
	eq(t, r.inputsHash(), r.hashWith(v8, 8))
	contains(t, r.err, "Không lấy được chính sách từ web", "dùng bản v8 đã lưu trên máy")
}
