package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aisdlc/internal/fsx"
	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// Resolution là kết quả phân giải tính năng: Resolved, Ambiguous hoặc None.
type Resolution interface{ resolution() }

// Resolved: đã xác định được một tính năng.
type Resolved struct{ Key model.FeatureKey }

// Ambiguous: nhiều tính năng đang mở, chưa biết chọn cái nào.
type Ambiguous struct{ Open []model.FeatureKey }

// None: không có tính năng nào.
type None struct{}

func (Resolved) resolution()  {}
func (Ambiguous) resolution() {}
func (None) resolution()      {}

// FeatureResolver phân giải tính năng theo thứ tự: key ghi rõ, gắn kết của session, số tính năng đang mở
// (0/1/nhiều), rồi .aisdlc/active. Không hỏi, không in; Context.Feature (CLI) và hook cùng gọi.
type FeatureResolver struct{ ctx *Context }

// NewFeatureResolver tạo FeatureResolver.
func NewFeatureResolver(ctx *Context) *FeatureResolver { return &FeatureResolver{ctx: ctx} }

// OpenFeatures là các tính năng "đang mở", sắp theo key: có aisdlc/features/<KEY>/feature.yaml trong checkout,
// file đó CHƯA có ở baseRef (đã merge = đóng; không đọc được baseRef thì bỏ điều kiện này), và cổng REVIEW chưa
// APPROVED (đọc qua cùng kho cổng của CLI: chế độ web dùng cache web).
func (r *FeatureResolver) OpenFeatures(root string) ([]model.FeatureKey, error) {
	dir := filepath.Join(root, "aisdlc", "features")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(i18n.T("Không đọc được %s: %v"), dir, err)
	}
	var names []string
	for _, e := range entries {
		if isDir(filepath.Join(dir, e.Name())) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var keys []model.FeatureKey
	for _, n := range names {
		k, err := model.ParseFeatureKey(n)
		if err != nil {
			continue // không phải thư mục tính năng
		}
		if exists(model.FeaturePaths{RepoRoot: root, Key: k}.Meta()) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	merged := r.atBaseRef(root)
	var open []model.FeatureKey
	for _, k := range keys {
		if merged != nil && merged["aisdlc/features/"+k.String()+"/feature.yaml"] {
			continue
		}
		if r.reviewApproved(root, k) {
			continue
		}
		open = append(open, k)
	}
	return open, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// atBaseRef là các file dưới aisdlc/features/ ở baseRef; nil nếu không đọc được (thiếu ref, không phải repo git...).
func (r *FeatureResolver) atBaseRef(root string) map[string]bool {
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return nil
	}
	files, err := r.ctx.Git(root).ListTree(r.ctx.std, cfg.BaseRef, "aisdlc/features")
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f] = true
	}
	return set
}

// reviewApproved đọc trạng thái REVIEW không tác dụng phụ (không mạng): chế độ theo cache như hook, kho cổng là
// gates.json cục bộ hoặc phong bì ký trong cache web. Lỗi (gates.json thiếu/hỏng) thì coi là còn mở.
func (r *FeatureResolver) reviewApproved(root string, k model.FeatureKey) bool {
	paths := model.FeaturePaths{RepoRoot: root, Key: k}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return false
	}
	meta, err := model.ReadFeatureMeta(paths.Meta())
	if err != nil {
		return false
	}
	flow := meta.Flow
	if flow == "" {
		flow = model.FlowFull
	}
	web := NewWebCache(r.ctx, root)
	var store gate.GateStore
	if web.CachedMode(cfg) == gate.ModeWeb {
		store = web.Store(k)
	} else {
		store = gate.NewLocalGateStore(paths)
	}
	st, err := gate.NewService(paths, flow, cfg, store, r.ctx.now).State(gate.REVIEW)
	return err == nil && st == gate.Approved
}

// Bind (init/use): gắn session hiện tại (nếu có) và .aisdlc/active với tính năng.
func (r *FeatureResolver) Bind(root string, key model.FeatureKey) error {
	session, ok := r.ctx.ResolveSession()
	if ok {
		if err := model.WriteSession(root, session, key); err != nil {
			return err
		}
	} else if _, cc := r.ctx.Env("CLAUDECODE"); cc {
		r.ctx.Eprintln(i18n.Tf("Cảnh báo: đang chạy trong Claude Code nhưng thiếu biến AISDLC_SESSION, nên %s không được gắn riêng cho session này (hook có thể chặn vì nhiều tính năng đang mở). Khởi động lại session Claude Code, hoặc thêm `--feature %s` vào lệnh.", key.String(), key.String()))
	}
	return model.WriteActive(root, key)
}

// WarnSameBranch: từ hai tính năng đang mở trên cùng nhánh thì cảnh báo một lần (cờ .aisdlc/warned-<nhánh>):
// diff, bước và MR sẽ lẫn nhau. Không chặn.
func (r *FeatureResolver) WarnSameBranch(root string) error {
	open, err := r.OpenFeatures(root)
	if err != nil {
		return err
	}
	if len(open) < 2 {
		return nil
	}
	branch, ok, err := r.ctx.Git(root).CurrentBranch(r.ctx.std)
	if err != nil {
		return err
	}
	if !ok {
		branch = "HEAD"
	}
	flag := filepath.Join(root, ".aisdlc", "warned-"+model.SessionFileName(branch))
	if exists(flag) {
		return nil
	}
	if err := fsx.WriteAtomic(flag, nil, 0o644); err != nil {
		return err
	}
	r.ctx.Println(i18n.Tf("Cảnh báo: nhánh %s đang có nhiều tính năng mở; diff, bước và MR của chúng sẽ lẫn nhau. Nên mỗi tính năng một nhánh riêng, dùng `git worktree add` để làm song song.", branch))
	return nil
}

// Resolve phân giải tính năng; explicit/sessionID rỗng nghĩa là không có.
func (r *FeatureResolver) Resolve(root string, explicit model.FeatureKey, sessionID string) (Resolution, error) {
	if explicit != "" {
		return Resolved{Key: explicit}, nil
	}
	if strings.TrimSpace(sessionID) != "" {
		k, ok, err := model.ReadSession(root, sessionID)
		if err != nil {
			return nil, err
		}
		if ok {
			return Resolved{Key: k}, nil
		}
	}
	open, err := r.OpenFeatures(root)
	if err != nil {
		return nil, err
	}
	switch {
	case len(open) == 1:
		return Resolved{Key: open[0]}, nil
	case len(open) > 1:
		return Ambiguous{Open: open}, nil
	}
	k, ok, err := model.ReadActive(root)
	if err != nil {
		return nil, err
	}
	if ok {
		return Resolved{Key: k}, nil
	}
	return None{}, nil
}
