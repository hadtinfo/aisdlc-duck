// Package gitx bọc git CLI: snapshot working tree không đụng index của dev
// và diff giữa các tree. Không phụ thuộc package nội bộ nào khác.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"aisdlc/internal/i18n"
)

// EmptyTree là hash của tree rỗng.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ChangeType là loại thay đổi của một file.
type ChangeType string

const (
	Added    ChangeType = "ADDED"
	Modified ChangeType = "MODIFIED"
	Deleted  ChangeType = "DELETED"
	Renamed  ChangeType = "RENAMED"
)

// FileChange là một thay đổi file giữa hai tree. OldPath chỉ khác rỗng khi Renamed.
type FileChange struct {
	Path, OldPath  string
	Type           ChangeType
	Added, Deleted int
	Binary         bool
}

// Lines là tổng số dòng thêm và xóa.
func (c FileChange) Lines() int { return c.Added + c.Deleted }

// Error là lỗi khi chạy git. Msg đã gồm stderr (nếu có), Stderr giữ riêng.
type Error struct {
	Msg    string
	Stderr string
	exited bool // git chạy được nhưng thoát với mã khác 0
}

func (e *Error) Error() string { return e.Msg }

// Repo là một working tree git.
type Repo struct{ root string }

// New tạo Repo cho thư mục root.
func New(root string) *Repo { return &Repo{root: root} }

// SnapshotTree trả tree hash của working tree (gồm file chưa track, trừ file
// ignore) mà không đụng index của dev.
func (r *Repo) SnapshotTree(ctx context.Context) (string, error) {
	return r.snapshot(ctx, "")
}

// SnapshotTreeExcluding như SnapshotTree nhưng bỏ thư mục dir (tương đối với
// gốc repo, dấu "/").
func (r *Repo) SnapshotTreeExcluding(ctx context.Context, dir string) (string, error) {
	return r.snapshot(ctx, dir)
}

func (r *Repo) snapshot(ctx context.Context, excludeDir string) (string, error) {
	f, err := os.CreateTemp("", "aisdlc-index*.tmp")
	if err != nil {
		return "", &Error{Msg: i18n.Tf("Không tạo được file index tạm: %s", err.Error())}
	}
	index := f.Name()
	f.Close()
	defer os.Remove(index)
	// git tự tạo; file rỗng không phải index hợp lệ
	if err := os.Remove(index); err != nil {
		return "", &Error{Msg: i18n.Tf("Không tạo được file index tạm: %s", err.Error())}
	}
	extra := []string{"GIT_INDEX_FILE=" + index}
	if r.hasHead(ctx) {
		if _, err := r.run(ctx, extra, "read-tree", "HEAD"); err != nil {
			return "", err
		}
	}
	if err := r.addAll(ctx, extra); err != nil {
		return "", err
	}
	if excludeDir != "" {
		if _, err := r.run(ctx, extra, "rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", excludeDir); err != nil {
			return "", err
		}
	}
	out, err := r.run(ctx, extra, "write-tree")
	return strings.TrimSpace(out), err
}

// atomicTempExclude loại file tạm của fsx.WriteAtomic khỏi snapshot. os.CreateTemp với mẫu
// ".<base>.tmp-*" thay "*" bằng một dãy chữ số, nên chỉ loại tên ".<base>.tmp-<số>…";
// file người dùng như ".notes.tmp-draft" vẫn vào snapshot.
const atomicTempExclude = ":(exclude,glob)**/.*.tmp-[0-9]*"

// addAll chạy "git add -A" bỏ file tạm atomic. Nếu file tạm vẫn biến mất giữa lúc git quét thì thử lại
// tối đa 5 lần; lỗi khác trả ngay.
func (r *Repo) addAll(ctx context.Context, extra []string) error {
	var err error
	for i := 0; i < 5; i++ {
		if _, err = r.run(ctx, extra, "add", "-A", "--", ".", atomicTempExclude); err == nil {
			return nil
		}
		msg := err.Error()
		if !strings.Contains(msg, "unable to stat") && !strings.Contains(msg, "No such file or directory") {
			return err
		}
		time.Sleep(15 * time.Millisecond)
	}
	return err
}

// Patch trả unified diff đầy đủ giữa hai tree.
func (r *Repo) Patch(ctx context.Context, from, to string) (string, error) {
	return r.run(ctx, nil, "-c", "core.quotepath=false", "diff", "-M", "--no-color", "--no-ext-diff",
		"--src-prefix=a/", "--dst-prefix=b/", from, to)
}

// PatchPaths như Patch nhưng chỉ gồm các đường dẫn paths (khớp nguyên văn);
// paths rỗng thì trả "" mà không gọi git.
func (r *Repo) PatchPaths(ctx context.Context, from, to string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	args := []string{"--literal-pathspecs", "-c", "core.quotepath=false", "diff", "-M", "--no-color",
		"--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", from, to, "--"}
	return r.run(ctx, nil, append(args, paths...)...)
}

// FileAt trả nội dung file path trong tree/commit treeish; ok=false nếu không có file đó.
func (r *Repo) FileAt(ctx context.Context, treeish, path string) (string, bool, error) {
	out, err := r.run(ctx, nil, "ls-tree", treeish, "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	c, err := r.run(ctx, nil, "cat-file", "blob", treeish+":"+path)
	if err != nil {
		return "", false, err
	}
	return c, true, nil
}

// ListFiles liệt kê file trong repo (đã track và chưa track, trừ file ignore và
// file đã xóa), dùng "/", đã sắp xếp, không trùng.
func (r *Repo) ListFiles(ctx context.Context) ([]string, error) {
	a, err := r.run(ctx, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	d, err := r.run(ctx, nil, "ls-files", "-z", "--deleted")
	if err != nil {
		return nil, err
	}
	deleted := map[string]bool{}
	for _, p := range nul(d) {
		deleted[p] = true
	}
	set := map[string]bool{}
	for _, p := range nul(a) {
		if !deleted[p] {
			set[p] = true
		}
	}
	return sortedKeys(set), nil
}

// ListTree liệt kê mọi file trong tree/commit treeish, đã sắp xếp. Nếu có under
// thì chỉ lấy các file dưới những đường dẫn đó (tính từ gốc repo).
func (r *Repo) ListTree(ctx context.Context, treeish string, under ...string) ([]string, error) {
	args := []string{"ls-tree", "-r", "-z", "--name-only"}
	if len(under) == 0 {
		args = append(args, treeish)
	} else {
		args = append(args, "--full-tree", treeish, "--")
		args = append(args, under...)
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range nul(out) {
		set[p] = true
	}
	return sortedKeys(set), nil
}

// MergeBaseTree trả tree của merge-base giữa HEAD và ref.
func (r *Repo) MergeBaseTree(ctx context.Context, ref string) (string, error) {
	base, err := r.run(ctx, nil, "merge-base", "HEAD", ref)
	if err != nil {
		return "", err
	}
	out, err := r.run(ctx, nil, "rev-parse", strings.TrimSpace(base)+"^{tree}")
	return strings.TrimSpace(out), err
}

// UserName trả user.name của git; lỗi nếu chưa đặt.
func (r *Repo) UserName(ctx context.Context) (string, error) {
	out, err := r.run(ctx, nil, "config", "user.name")
	return strings.TrimSpace(out), err
}

// UserEmail trả user.email của git; lỗi nếu chưa đặt.
func (r *Repo) UserEmail(ctx context.Context) (string, error) {
	out, err := r.run(ctx, nil, "config", "user.email")
	return strings.TrimSpace(out), err
}

// RemoteURL trả URL của remote name; ok=false nếu không có remote đó.
func (r *Repo) RemoteURL(ctx context.Context, name string) (string, bool, error) {
	out, err := r.run(ctx, nil, "config", "--get", "remote."+name+".url")
	if err != nil {
		return "", false, absentOrFatal(err)
	}
	u := strings.TrimSpace(out)
	return u, u != "", nil
}

// CurrentBranch trả tên nhánh hiện tại; ok=false khi HEAD detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, bool, error) {
	out, err := r.run(ctx, nil, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", false, absentOrFatal(err)
	}
	b := strings.TrimSpace(out)
	return b, b != "", nil
}

// absentOrFatal: git thoát với mã khác 0 nghĩa là "không có" (như Java bắt
// GitException); chỉ lỗi không chạy được git hoặc ctx bị hủy mới trả lên.
func absentOrFatal(err error) error {
	var ge *Error
	if errors.As(err, &ge) && ge.exited {
		return nil
	}
	return err
}

func (r *Repo) hasHead(ctx context.Context) bool {
	_, err := r.run(ctx, nil, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

func nul(out string) []string {
	var r []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			r = append(r, f)
		}
	}
	return r
}

func sortedKeys(m map[string]bool) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}

// run chạy git trong thư mục gốc; extra là biến thêm vào môi trường kế thừa.
func (r *Repo) run(ctx context.Context, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	if len(extra) > 0 {
		cmd.Env = append(os.Environ(), extra...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ctx.Err() == nil {
			se := strings.TrimSpace(stderr.String())
			return "", &Error{
				Msg:    fmt.Sprintf(i18n.T("git %s thất bại (%d): %s"), strings.Join(args, " "), ee.ExitCode(), se),
				Stderr: se,
				exited: true,
			}
		}
		return "", &Error{Msg: i18n.Tf("Không chạy được git: %s", err.Error())}
	}
	return stdout.String(), nil
}
