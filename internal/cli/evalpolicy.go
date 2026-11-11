package cli

import (
	"context"
	"strconv"
	"strings"

	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// Nguồn chính sách cho eval để tính lại inputsHash. Chọn nguồn do eval.Service quyết, cùng quyết định chế độ hiệu
// lực với cổng approval: local dùng chính sách như CLI (LoadPolicy: file/mẫu), web (CI) dùng webEvalPolicy (token CI),
// không bao giờ rơi về file/mẫu. Lỗi thành chuỗi cho chi tiết RISK_LLM.

// localEvalPolicy là chính sách ở chế độ local.
func localEvalPolicy(ctx *Context, f *Feature) (*risk.RiskPolicy, string) {
	l, err := LoadPolicy(ctx, f.Root, true)
	if err != nil {
		return nil, "không nạp được chính sách rủi ro: " + oneLineErr(err)
	}
	return &l.Policy, ""
}

// webEvalPolicy trả hàm lấy chính sách từ web (CI); nil khi thiếu biến môi trường.
func webEvalPolicy(ctx *Context, f *Feature) func(context.Context) (*risk.RiskPolicy, error) {
	url, okURL := ctx.Env("AISDLC_WEB_URL")
	token, okTok := ctx.Env("AISDLC_CI_TOKEN")
	if !okURL || !okTok {
		return nil
	}
	return func(c context.Context) (*risk.RiskPolicy, error) {
		repo := snapshot.RepoID(c, f.Git(), f.Root)
		yaml, version, err := webclient.FetchCIPolicy(c, url, token, repo)
		if err != nil {
			return nil, &flatErr{err}
		}
		p, err := parsePolicy(yaml, "Chính sách rủi ro v"+strconv.Itoa(version)+" trên web")
		if err != nil {
			return nil, &flatErr{err}
		}
		p.Version = version // phiên bản trên web là phiên bản của chính sách
		return &p, nil
	}
}

type flatErr struct{ err error }

func (e *flatErr) Error() string { return oneLineErr(e.err) }

func oneLineErr(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }
