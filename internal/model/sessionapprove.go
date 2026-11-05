package model

import (
	"context"

	"aisdlc/internal/gitx"
)

// SessionApproveSetting cho biết có được duyệt cổng trong Claude Code hay không.
type SessionApproveSetting struct {
	Enabled bool
	Reason  string
}

// ReadSessionApprove chỉ đọc aisdlc/config.yaml ở baseRef (đã merge), không đọc working tree,
// để agent không tự bật bằng cách sửa config cục bộ. Không bao giờ trả lỗi.
func ReadSessionApprove(ctx context.Context, git *gitx.Repo, working RepoConfig) SessionApproveSetting {
	ref := working.BaseRef
	unreadable := SessionApproveSetting{Reason: "không đọc được aisdlc/config.yaml ở " + ref}
	yaml, ok, err := git.FileAt(ctx, ref, "aisdlc/config.yaml")
	if err != nil || !ok {
		return unreadable
	}
	c, err := ParseRepoConfig(yaml)
	if err != nil {
		return unreadable
	}
	if c.SessionApprove {
		return SessionApproveSetting{Enabled: true, Reason: "theo " + ref}
	}
	return SessionApproveSetting{Reason: "chưa bật ở " + ref}
}
