package snapshot

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"aisdlc/internal/gitx"
)

var (
	remoteURLRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://(?:[^/@]*@)?([^/:]+)(?::\d+)?/(.+)$`)
	remoteSCPRe = regexp.MustCompile(`^(?:[^@/:]+@)?([^/:]+):(.+)$`)
)

// RepoID là định danh repo như web thấy: remote origin chuẩn hóa về host/group/name; không có remote thì
// local/<tên thư mục>. "" khi không đọc được git (RepoIdentity.of của Java ném lỗi, WebGates coi là null).
func RepoID(ctx context.Context, git *gitx.Repo, root string) string {
	u, ok, err := git.RemoteURL(ctx, "origin")
	if err != nil {
		return ""
	}
	if ok {
		return NormalizeRemote(u)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	name := filepath.Base(filepath.Clean(abs))
	if name == "" || name == "/" || name == "." {
		name = "root"
	}
	return "local/" + name
}

// NormalizeRemote đưa URL remote (https, ssh://, scp-like) về host/group/name, host chữ thường, bỏ ".git".
func NormalizeRemote(remoteURL string) string {
	u := strings.TrimSpace(remoteURL)
	m := remoteURLRe.FindStringSubmatch(u)
	if m == nil {
		m = remoteSCPRe.FindStringSubmatch(u)
	}
	if m == nil {
		return u
	}
	p := strings.TrimLeft(m[2], "/")
	p = strings.TrimRight(p, "/")
	p = strings.TrimSuffix(p, ".git")
	return strings.ToLower(m[1]) + "/" + p
}
