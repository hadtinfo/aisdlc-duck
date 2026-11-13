package rewind

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/step"
)

// Deps là các phụ thuộc để thực thi quay về trên máy dev.
type Deps struct {
	Git    *gitx.Repo
	Paths  model.FeaturePaths
	Cfg    model.RepoConfig
	Steps  *step.Service
	Extras *step.ScopeExtras
	// ClearGate: local là LocalGateStore.Clear; web là no-op (server đã làm) rồi Refresh.
	ClearGate      func(gate.Gate) error
	AppendFeedback func(text string) error
	Now            func() time.Time
	By             string
}

// codePath: path thuộc phần code được khôi phục. Loại mọi thư mục tính năng (kể cả tính năng khác),
// .aisdlc/ và path unmanaged.
func codePath(cfg model.RepoConfig, p string) bool {
	if p == "" || p == ".aisdlc" || strings.HasPrefix(p, ".aisdlc/") || strings.HasPrefix(p, "aisdlc/features/") {
		return false
	}
	return !step.ScopeMatches(cfg.Unmanaged, p)
}

// codeChanges chỉ giữ phần code của các thay đổi. Rename có một phía bị loại thì chỉ giữ phía còn lại,
// để RestorePaths không bao giờ ghi/xoá path bị loại.
func codeChanges(cfg model.RepoConfig, cs []gitx.FileChange) []gitx.FileChange {
	out := []gitx.FileChange{}
	for _, c := range cs {
		newOK, oldOK := codePath(cfg, c.Path), c.OldPath != "" && codePath(cfg, c.OldPath)
		switch {
		case newOK && (c.OldPath == "" || oldOK):
			out = append(out, c)
		case newOK:
			c.OldPath = ""
			out = append(out, c)
		case oldOK:
			c.Path, c.OldPath = c.OldPath, ""
			out = append(out, c)
		}
	}
	return out
}

// codeStep là bước có snapshot đầu bước làm mốc code của kế hoạch.
func codeStep(p *Plan) string {
	if p.Target.Step != "" {
		return p.Target.Step
	}
	if len(p.DropSteps) > 0 {
		return p.DropSteps[0]
	}
	return ""
}

// ResolveCode điền OwnChanges/ForeignChanges của p. Tree mốc mất thì thử ref checkpoint; vẫn mất thì
// trả lỗi và không đổi gì trên đĩa.
func ResolveCode(ctx context.Context, d Deps, p *Plan) error {
	p.OwnChanges, p.ForeignChanges, p.Skipped = nil, nil, nil
	if p.CodeTree == "" {
		return nil
	}
	if !d.Git.ObjectExists(ctx, p.CodeTree) {
		id := codeStep(p)
		tree, ok, err := d.Git.ResolveRef(ctx, step.CheckpointRef(d.Paths.Key, id)+"^{tree}")
		if err != nil || !ok || !d.Git.ObjectExists(ctx, tree) {
			return errorf(i18n.Tf("Không còn snapshot code của mốc %s (đầu bước %s). Hãy chọn một mốc muộn hơn còn snapshot.",
				p.Target.ID, id))
		}
		p.CodeTree = tree
	}
	now, err := d.Git.SnapshotTree(ctx)
	if err != nil {
		return err
	}
	diff, err := d.Git.Diff(ctx, p.CodeTree, now)
	if err != nil {
		return err
	}
	own, err := ownPaths(ctx, d, p, now)
	if err != nil {
		return err
	}
	for _, c := range codeChanges(d.Cfg, diff) {
		c, keep := d.dropUnsnapshotted(c, p)
		if !keep {
			continue
		}
		if own[c.Path] || (c.OldPath != "" && own[c.OldPath]) {
			p.OwnChanges = append(p.OwnChanges, c)
		} else {
			p.ForeignChanges = append(p.ForeignChanges, c)
		}
	}
	return nil
}

// onDisk: path đang có trên đĩa (file, symlink hay thư mục).
func (d Deps) onDisk(rel string) bool {
	_, err := os.Lstat(filepath.Join(d.Paths.RepoRoot, filepath.FromSlash(rel)))
	return err == nil
}

// dropUnsnapshotted bỏ phía của thay đổi mà path vắng trong snapshot hiện tại nhưng vẫn có trên đĩa (file bị
// gitignore sau mốc): nội dung đó không có trong ref dự phòng nên không được ghi đè. Path bị bỏ ghi vào
// p.Skipped. keep=false khi không còn gì để khôi phục.
func (d Deps) dropUnsnapshotted(c gitx.FileChange, p *Plan) (gitx.FileChange, bool) {
	if c.OldPath != "" && d.onDisk(c.OldPath) { // nguồn rename vắng trong snapshot hiện tại
		p.Skipped = append(p.Skipped, c.OldPath)
		c.OldPath = ""
	}
	if c.Type == gitx.Deleted && d.onDisk(c.Path) {
		p.Skipped = append(p.Skipped, c.Path)
		return c, false
	}
	return c, true
}

// ownPaths: path do các bước bị bỏ đổi (Changes, cộng diff của bước IN_PROGRESS bị bỏ).
func ownPaths(ctx context.Context, d Deps, p *Plan, now string) (map[string]bool, error) {
	own := map[string]bool{}
	add := func(cs []gitx.FileChange) {
		for _, c := range cs {
			own[c.Path] = true
			if c.OldPath != "" {
				own[c.OldPath] = true
			}
		}
	}
	recs, err := d.Steps.All()
	if err != nil {
		return nil, err
	}
	drop := map[string]bool{}
	for _, id := range p.DropSteps {
		drop[id] = true
	}
	for _, r := range recs {
		if !drop[r.ID] {
			continue
		}
		add(r.Changes)
		if r.Status == step.StepInProgress && r.StartTree != "" && d.Git.ObjectExists(ctx, r.StartTree) {
			cs, err := d.Git.Diff(ctx, r.StartTree, now)
			if err != nil {
				return nil, err
			}
			add(cs)
		}
	}
	return own, nil
}

// BackupPrefix là tiền tố ref dự phòng của tính năng.
func BackupPrefix(key model.FeatureKey) string { return "refs/aisdlc/backup/" + string(key) + "/" }

// Backup chụp toàn bộ working tree (gồm thư mục tính năng), bọc thành commit và ghi ref dự phòng
// refs/aisdlc/backup/<KEY>/<yyyymmdd-hhmm>; trùng thì thêm -2, -3, ...
func Backup(ctx context.Context, d Deps) (string, error) {
	tree, err := d.Git.SnapshotTree(ctx)
	if err != nil {
		return "", err
	}
	at := d.Now()
	commit, err := d.Git.CommitTree(ctx, tree, "aisdlc backup "+string(d.Paths.Key)+" "+at.Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	base := BackupPrefix(d.Paths.Key) + at.Format("20060102-1504")
	ref := base
	for n := 2; ; n++ {
		_, ok, err := d.Git.ResolveRef(ctx, ref)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		ref = base + "-" + strconv.Itoa(n)
	}
	if err := d.Git.UpdateRef(ctx, ref, commit); err != nil {
		return "", err
	}
	return ref, nil
}

// Apply thực hiện bước 5–7 của thiết kế: khôi phục code, xoá artifact, xoá trạng thái cổng/bước/scope,
// ghi feedback. Idempotent: chạy lại sau lỗi giữa chừng sẽ hoàn tất phần còn lại.
func Apply(ctx context.Context, d Deps, p Plan, backupRef string) error {
	// Kiểm trước khi đụng gì: chỉ được xoá bên trong thư mục của chính tính năng.
	dir := filepath.Clean(d.Paths.Dir())
	for _, path := range p.DeletePaths {
		c := filepath.Clean(path)
		if !filepath.IsAbs(c) || !strings.HasPrefix(c, dir+string(filepath.Separator)) ||
			c == filepath.Clean(d.Paths.Meta()) || c == filepath.Clean(d.Paths.Feedback()) ||
			c == filepath.Clean(PendingFile(d.Paths)) {
			return errorf(i18n.Tf("Từ chối xoá %s: nằm ngoài các artifact của tính năng %s", path, string(d.Paths.Key)))
		}
	}
	// Ghi dấu đang dở trước thay đổi đầu tiên: chạy lại sau sự cố dùng lại kế hoạch và ref dự phòng này.
	if err := WritePending(d, p, backupRef); err != nil {
		return err
	}
	if p.CodeTree != "" {
		changes := skipPaths(codeChanges(d.Cfg, append(append([]gitx.FileChange{}, p.OwnChanges...), p.ForeignChanges...)),
			p.Skipped)
		if err := d.Git.RestorePaths(ctx, p.CodeTree, changes); err != nil {
			return err
		}
	}
	for _, path := range p.DeletePaths {
		if err := os.RemoveAll(path); err != nil {
			return errorf(i18n.Tf("Không xoá được %s: %s", d.Paths.Rel(path), err.Error()))
		}
	}
	if d.ClearGate != nil {
		for _, g := range p.ClearGates {
			if err := d.ClearGate(g); err != nil {
				return err
			}
		}
	}
	// Bỏ bản ghi theo ID, không theo spec: lần chạy lại mốc cổng xảy ra khi design.yaml đã bị xoá.
	switch {
	case p.Target.Step != "":
		if err := d.Steps.DropRecords(p.Target.Step, p.DropSteps); err != nil {
			return err
		}
	case p.Target.Gate != gate.REVIEW:
		if err := d.Steps.DropRecords("", nil); err != nil {
			return err
		}
	}
	if len(p.ScopeSteps) > 0 {
		if _, err := os.Stat(d.Paths.ScopeExtra()); err == nil {
			if err := d.Extras.RemoveSteps(p.ScopeSteps); err != nil {
				return err
			}
		}
	}
	if err := appendRewind(d, p, backupRef); err != nil {
		return err
	}
	if err := os.Remove(PendingFile(d.Paths)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errorf(i18n.Tf("Không xoá được %s: %s", d.Paths.Rel(PendingFile(d.Paths)), err.Error()))
	}
	return nil
}

// skipPaths bỏ khỏi changes mọi phía có path nằm trong skipped (phòng thủ thêm cho Apply).
func skipPaths(cs []gitx.FileChange, skipped []string) []gitx.FileChange {
	if len(skipped) == 0 {
		return cs
	}
	skip := map[string]bool{}
	for _, s := range skipped {
		skip[s] = true
	}
	out := []gitx.FileChange{}
	for _, c := range cs {
		if skip[c.OldPath] {
			c.OldPath = ""
		}
		if skip[c.Path] {
			if c.OldPath == "" {
				continue
			}
			c.Path, c.OldPath = c.OldPath, ""
		}
		out = append(out, c)
	}
	return out
}

// PendingRewind là lần quay về đã bắt đầu Apply nhưng chưa xong (rewind-pending.json).
type PendingRewind struct {
	BackupRef string    `json:"backupRef"`
	By        string    `json:"by"`
	At        time.Time `json:"at"`
	Plan      Plan      `json:"plan"`
}

// PendingFile là dấu lần quay về đang dở trong thư mục tính năng.
func PendingFile(p model.FeaturePaths) string { return filepath.Join(p.Dir(), "rewind-pending.json") }

// Pending đọc dấu lần quay về đang dở; ok=false nếu không có. CLI dùng để chạy tiếp với Plan và
// BackupRef đã lưu thay vì lập kế hoạch lại từ trạng thái đã bị xoá dở.
func Pending(p model.FeaturePaths) (PendingRewind, bool, error) {
	b, err := os.ReadFile(PendingFile(p))
	if errors.Is(err, os.ErrNotExist) {
		return PendingRewind{}, false, nil
	}
	if err != nil {
		return PendingRewind{}, false, errorf(i18n.Tf("Không đọc được %s: %s", p.Rel(PendingFile(p)), err.Error()))
	}
	var pr PendingRewind
	if err := json.Unmarshal(b, &pr); err != nil || pr.Plan.Target.ID == "" {
		return PendingRewind{}, false, errorf(i18n.Tf("%s hỏng; xoá file này rồi chạy lại aisdlc rewind", p.Rel(PendingFile(p))))
	}
	return pr, true, nil
}

// WritePending ghi dấu đang dở (rewind-pending.json). Chế độ web ghi trước khi gọi server để lần chạy lại
// dùng cùng ref dự phòng làm requestId.
func WritePending(d Deps, p Plan, backupRef string) error {
	b, err := json.MarshalIndent(PendingRewind{BackupRef: backupRef, By: d.By, At: d.Now(), Plan: p}, "", "  ")
	if err != nil {
		return errorf(i18n.Tf("Không ghi được %s: %s", d.Paths.Rel(PendingFile(d.Paths)), err.Error()))
	}
	if err := os.MkdirAll(d.Paths.Dir(), 0o755); err != nil {
		return errorf(i18n.Tf("Không ghi được %s: %s", d.Paths.Rel(PendingFile(d.Paths)), err.Error()))
	}
	if err := fsx.WriteAtomic(PendingFile(d.Paths), b, 0o644); err != nil {
		return errorf(i18n.Tf("Không ghi được %s: %s", d.Paths.Rel(PendingFile(d.Paths)), err.Error()))
	}
	return nil
}

// appendRewind ghi dòng REWIND vào feedback.md, trừ khi tiêu đề "## " cuối đã là REWIND cùng mốc và cùng ref.
func appendRewind(d Deps, p Plan, backupRef string) error {
	if d.AppendFeedback == nil {
		return nil
	}
	if last := lastHeading(d.Paths.Feedback()); strings.HasPrefix(last, "## REWIND") &&
		strings.Contains(last, " "+p.Target.ID+" ") && strings.HasSuffix(last, " "+backupRef+")") {
		return nil
	}
	at := d.Now().UTC().Format(time.RFC3339Nano) // cùng định dạng tiêu đề feedback của gate.Service
	return d.AppendFeedback(i18n.Tf("## REWIND — quay về %s bởi %s lúc %s (dự phòng: %s)\n\n",
		p.Target.ID, d.By, at, backupRef))
}

func lastHeading(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	last := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), "\r "); strings.HasPrefix(l, "## ") {
			last = l
		}
	}
	return last
}
