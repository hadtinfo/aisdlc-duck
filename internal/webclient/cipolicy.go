package webclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
)

// FetchCIPolicy lấy chính sách rủi ro hiện hành của repo qua GET /api/cli/policy với Bearer token CI. CI không có khóa
// ghim nên không kiểm chữ ký Ed25519 của phong bì; mô hình tin cậy giống GET /api/ci/approvals: TLS và token CI (chỉ
// đọc). Chỉ kiểm phong bì đúng cấu trúc và repoId khớp. Lỗi không chứa token.
func FetchCIPolicy(ctx context.Context, baseURL, token, repo string) (yaml string, version int, err error) {
	if repo == "" {
		return "", 0, errors.New("không xác định được repo")
	}
	ctx, cancel := context.WithTimeout(ctx, CIApprovalTimeout)
	defer cancel()
	c := &Client{BaseURL: baseURL, Token: token}
	r, err := c.Do(ctx, "GET", "/api/cli/policy?repo="+neturl.QueryEscape(repo), nil)
	if err != nil {
		return "", 0, err
	}
	if r.Status != 200 {
		return "", 0, fmt.Errorf("HTTP %d", r.Status)
	}
	var env struct {
		Payload string `json:"payload"`
	}
	if json.Unmarshal(r.Body, &env) != nil || env.Payload == "" {
		return "", 0, errors.New("phản hồi web không đọc được")
	}
	raw, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return "", 0, errors.New("phản hồi web không đọc được")
	}
	var p struct {
		RepoID  string  `json:"repoId"`
		Version int     `json:"version"`
		YAML    *string `json:"yaml"`
	}
	if json.Unmarshal(raw, &p) != nil || p.YAML == nil {
		return "", 0, errors.New("phản hồi web không đọc được")
	}
	if p.RepoID != repo {
		return "", 0, errors.New("chính sách thuộc repo khác")
	}
	return *p.YAML, p.Version, nil
}
