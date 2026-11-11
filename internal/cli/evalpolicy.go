package cli

import (
	"strconv"
	"strings"

	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// evalPolicy là chính sách để eval tính lại inputsHash. CI chế độ web (config approvals: web) lấy chính sách hiện hành
// từ web bằng token CI (checkout mới không có cache .aisdlc/web), không bao giờ lặng lẽ dùng file/mẫu trong repo. Còn
// lại dùng chính sách như CLI (LoadPolicy). Lỗi trả thành chuỗi cho chi tiết RISK_LLM.
func evalPolicy(ctx *Context, f *Feature, ci bool) (*risk.RiskPolicy, string) {
	if ci && f.Config.Approvals == "web" {
		const pre = "không lấy được chính sách từ web: "
		url, okURL := ctx.Env("AISDLC_WEB_URL")
		token, okTok := ctx.Env("AISDLC_CI_TOKEN")
		if !okURL || !okTok {
			return nil, pre + "thiếu biến AISDLC_WEB_URL hoặc AISDLC_CI_TOKEN"
		}
		repo := snapshot.RepoID(ctx.Std(), f.Git(), f.Root)
		yaml, version, err := webclient.FetchCIPolicy(ctx.Std(), url, token, repo)
		if err != nil {
			return nil, pre + oneLineErr(err)
		}
		p, err := parsePolicy(yaml, "Chính sách rủi ro v"+strconv.Itoa(version)+" trên web")
		if err != nil {
			return nil, pre + oneLineErr(err)
		}
		p.Version = version // phiên bản trên web là phiên bản của chính sách
		return &p, ""
	}
	l, err := LoadPolicy(ctx, f.Root, true)
	if err != nil {
		return nil, "không nạp được chính sách rủi ro: " + oneLineErr(err)
	}
	return &l.Policy, ""
}

func oneLineErr(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }
