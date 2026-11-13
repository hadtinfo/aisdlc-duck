package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/i18n"
)

// CommitTree tạo commit không cha trỏ tới tree, trả sha commit. Nếu git chưa
// có danh tính thì dùng danh tính mặc định của aisdlc để vẫn tạo được commit.
func (r *Repo) CommitTree(ctx context.Context, tree, msg string) (string, error) {
	var extra []string
	if _, err := r.UserName(ctx); err != nil {
		extra = append(extra, "GIT_AUTHOR_NAME=aisdlc", "GIT_COMMITTER_NAME=aisdlc")
	}
	if _, err := r.UserEmail(ctx); err != nil {
		extra = append(extra, "GIT_AUTHOR_EMAIL=aisdlc@localhost", "GIT_COMMITTER_EMAIL=aisdlc@localhost")
	}
	out, err := r.run(ctx, extra, "commit-tree", tree, "-m", msg)
	return strings.TrimSpace(out), err
}

// UpdateRef trỏ ref tới sha (tạo nếu chưa có).
func (r *Repo) UpdateRef(ctx context.Context, ref, sha string) error {
	_, err := r.run(ctx, nil, "update-ref", ref, sha)
	return err
}

// ResolveRef trả sha của ref; ok=false nếu ref không tồn tại.
func (r *Repo) ResolveRef(ctx context.Context, ref string) (string, bool, error) {
	out, err := r.run(ctx, nil, "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return "", false, absentOrFatal(err)
	}
	sha := strings.TrimSpace(out)
	return sha, sha != "", nil
}

// ObjectExists cho biết object sha có trong kho git không.
func (r *Repo) ObjectExists(ctx context.Context, sha string) bool {
	_, err := r.run(ctx, nil, "cat-file", "-e", sha)
	return err == nil
}

// RestorePaths đưa các file trong changes về đúng trạng thái ở tree: path có
// trong tree thì ghi lại nội dung (giữ mode thực thi), không có thì xóa khỏi
// đĩa. Rename xử lý cả OldPath. Không đụng index và HEAD.
func (r *Repo) RestorePaths(ctx context.Context, tree string, changes []FileChange) error {
	for _, c := range changes {
		paths := []string{c.Path}
		if c.OldPath != "" {
			paths = append(paths, c.OldPath)
		}
		for _, p := range paths {
			if err := r.restoreOne(ctx, tree, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repo) restoreOne(ctx context.Context, tree, p string) error {
	dst := filepath.Join(r.root, filepath.FromSlash(p))
	out, err := r.run(ctx, nil, "--literal-pathspecs", "ls-tree", "-z", tree, "--", p)
	if err != nil {
		return err
	}
	entry := strings.TrimRight(out, "\x00")
	if entry == "" { // không có trong tree: xóa khỏi đĩa
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return &Error{Msg: i18n.Tf("Không xóa được %s: %s", p, err.Error())}
		}
		return nil
	}
	mode, _, _ := strings.Cut(entry, " ")
	content, err := r.run(ctx, nil, "cat-file", "blob", tree+":"+p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return &Error{Msg: i18n.Tf("Không ghi được %s: %s", p, err.Error())}
	}
	if mode == "120000" { // symlink
		_ = os.Remove(dst)
		if err := os.Symlink(content, dst); err != nil {
			return &Error{Msg: i18n.Tf("Không ghi được %s: %s", p, err.Error())}
		}
		return nil
	}
	perm := os.FileMode(0o644)
	if mode == "100755" {
		perm = 0o755
	}
	if err := os.WriteFile(dst, []byte(content), perm); err != nil {
		return &Error{Msg: i18n.Tf("Không ghi được %s: %s", p, err.Error())}
	}
	return os.Chmod(dst, perm)
}
