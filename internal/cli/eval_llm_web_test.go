package cli

import (
	"context"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient/fakeweb"
)

// webPolicyCI dựng checkout CI mới ở chế độ web (không có .aisdlc/web), web giả phục vụ chính sách phiên bản 77 (khác
// phiên bản của file trong repo nên băm khác). risk.yaml được ghi lại với inputsHash tính theo chính sách của web.
func webPolicyCI(t *testing.T) (*testRepo, *fakeweb.Server) {
	r := evalCmdFx(t)
	r.webModeConfig()
	web := r.ciWeb(`{"mode":"local","pinned":true,"approvals":[]}`)
	p, ok := r.llmPolicy()
	if !ok {
		t.Fatal("không có chính sách")
	}
	yaml, err := p.ToYAML()
	must(t, err)
	repo := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	web.Update(func(s *fakeweb.Server) {
		s.State.Repo, s.PolicyYAML, s.PolicyVersion = repo, yaml, 77
	})
	p.Version = 77
	paths, files, _ := r.llmInputs("PAY-1")
	rf, _, err := risk.ReadRiskFile(paths.Risk())
	must(t, err)
	rf.LLM.InputsHash = risk.InputsHash(p, files)
	must(t, rf.Write(paths.Risk()))
	if r.exists(".aisdlc/web") {
		t.Fatal("checkout mới không được có cache web")
	}
	return r, web
}

func TestEvalCiWebModeUsesWebPolicyForInputsHash(t *testing.T) {
	r, web := webPolicyCI(t)
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 0)
	notContains(t, r.out, "RISK_LLM | KHÔNG ĐẠT")
	contains(t, r.out, "RISK_LLM | ĐẠT")
	reqs := web.RequestsFor("GET", "/api/cli/policy")
	eq(t, len(reqs), 1)
	eq(t, reqs[0].Auth, "Bearer ci-tok")
}

func TestEvalCiWebModePolicyFetchFailsNoSilentFallback(t *testing.T) {
	r, web := webPolicyCI(t)
	web.Update(func(s *fakeweb.Server) {
		s.Override["GET /api/cli/policy"] = fakeweb.JSONResp(403, `{"error":"forbidden"}`)
	})
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "RISK_LLM | KHÔNG ĐẠT | không lấy được chính sách từ web: HTTP 403")
	notContains(t, r.out, "inputsHash không khớp")
	if strings.Contains(r.out+r.err, "ci-tok") {
		t.Fatal("lộ token")
	}
}

func TestEvalCiInvalidPolicyFileSurfacesRealError(t *testing.T) {
	r := evalCmdFx(t)
	r.write(RepoPolicyFile, "schema: [hỏng\n")
	r.write("internal/other/x.go", "package other\n")
	eq(t, r.run("eval", "--ci", "--base", "HEAD"), 1)
	contains(t, r.out, "RISK_LLM | KHÔNG ĐẠT | không nạp được chính sách rủi ro:")
	contains(t, r.out, RepoPolicyFile)
}
