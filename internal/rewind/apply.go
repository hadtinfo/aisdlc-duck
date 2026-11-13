package rewind

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	p.OwnChanges, p.ForeignChanges = nil, nil
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
		if own[c.Path] || (c.OldPath != "" && own[c.OldPath]) {
			p.OwnChanges = append(p.OwnChanges, c)
		} else {
			p.ForeignChanges = append(p.ForeignChanges, c)
		}
	}
	return nil
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
			c == filepath.Clean(d.Paths.Meta()) || c == filepath.Clean(d.Paths.Feedback()) {
			return errorf(i18n.Tf("Từ chối xoá %s: nằm ngoài các artifact của tính năng %s", path, string(d.Paths.Key)))
		}
	}
	if p.CodeTree != "" {
		changes := codeChanges(d.Cfg, append(append([]gitx.FileChange{}, p.OwnChanges...), p.ForeignChanges...))
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
	if len(p.DropSteps) > 0 {
		if err := d.Steps.Truncate(p.DropSteps[0]); err != nil {
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
	return appendRewind(d, p, backupRef)
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
