package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/step"
)

const (
	evalFeaturesDir = "aisdlc/features/"
	evalConfigPath  = "aisdlc/config.yaml"
	evalListMax     = 10
)

// newEvalCmd: eval tầng 1 (EvalCommand.java). Không --ci: eval tính năng đang làm. --ci: eval mọi tính năng có thay
// đổi so với merge-base của base; không có tính năng nào thì không tin cấu hình của chính thay đổi (config.yaml bị
// sửa luôn fail, unmanaged và ciRequireFeature lấy ở base). Exit 0 khi mọi report đạt, 1 khi không.
func newEvalCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var ci bool
	var base string
	c := &cobra.Command{
		Use:   "eval",
		Short: "Eval tầng 1: build, unit test và bộ case đầy đủ qua harness, ghi eval/report.json và report.md.",
		Args:  cobra.NoArgs,
	}
	fo.AddTo(c)
	c.Flags().BoolVar(&ci, "ci", false,
		"Chế độ CI: không cần tính năng active; eval mọi tính năng có thay đổi so với base")
	c.Flags().StringVar(&base, "base", "", "Nhánh/commit gốc cho CI (mặc định baseRef trong config)")
	c.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
		if !ci {
			f, err := fo.Feature(ctx)
			if err != nil {
				return 0, err
			}
			return runEval(ctx, f, false, base)
		}
		return evalCI(ctx, base)
	})
	return c
}

func evalCI(ctx *Context, base string) (int, error) {
	// CI không bao giờ `aisdlc web login`: không nhắc đăng nhập (quietWebLogin của Java)
	ctx.loginWarned = true
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	ref := base
	if ref == "" {
		cfg, err := model.LoadRepoConfig(root)
		if err != nil {
			return 0, err
		}
		ref = cfg.BaseRef
	}
	git := ctx.Git(root)
	mergeBase, err := git.MergeBaseTree(ctx.Std(), ref)
	if err != nil {
		return 0, err
	}
	tree, err := git.SnapshotTree(ctx.Std())
	if err != nil {
		return 0, err
	}
	changes, err := git.Diff(ctx.Std(), mergeBase, tree)
	if err != nil {
		return 0, err
	}
	keys, err := changedFeatures(root, changes)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return evalNoFeature(ctx, git, mergeBase, ref, changes)
	}
	code := ExitOK
	for _, k := range keys {
		f, err := ctx.FeatureAt(root, k)
		if err != nil {
			return 0, err
		}
		c, err := runEval(ctx, f, true, base)
		if err != nil {
			return 0, err
		}
		code = max(code, c)
	}
	return code, nil
}

// evalNoFeature: CI không có tính năng nào thay đổi. Không tin cấu hình của chính thay đổi này: sửa config.yaml
// luôn fail, ciRequireFeature và unmanaged lấy từ config ở base.
func evalNoFeature(ctx *Context, git *gitx.Repo, mergeBase, ref string, changes []gitx.FileChange) (int, error) {
	if slices.ContainsFunc(changes, func(c gitx.FileChange) bool {
		return slices.Contains(changePaths(c), evalConfigPath)
	}) {
		ctx.Println("Eval KHÔNG ĐẠT: " + evalConfigPath + eval.ConfigChanged + ".")
		return ExitFindings, nil
	}
	text, _, err := git.FileAt(ctx.Std(), mergeBase, evalConfigPath)
	if err != nil {
		return 0, err
	}
	baseCfg, err := model.ParseRepoConfig(text)
	if err != nil {
		return 0, err
	}
	if managed := managedChanges(changes, baseCfg); len(managed) > 0 && baseCfg.CIRequireFeature {
		more := ""
		if len(managed) > evalListMax {
			more = fmt.Sprintf(" … và %d file khác", len(managed)-evalListMax)
		}
		ctx.Println("Eval KHÔNG ĐẠT: thay đổi code không thuộc tính năng AISDLC nào so với " + ref + ": " +
			strings.Join(managed[:min(evalListMax, len(managed))], ", ") + more +
			". Tạo tính năng bằng `aisdlc init <KEY>` (hoặc đặt ciRequireFeature: false).")
		return ExitFindings, nil
	}
	ctx.Println("Không có tính năng nào thay đổi so với " + ref + "; không có gì để eval.")
	return ExitOK, nil
}

// runEval eval một tính năng, in report.md; ExitOK khi report đạt, ExitFindings khi không.
func runEval(ctx *Context, f *Feature, ci bool, base string) (int, error) {
	ctx.Println("Đang eval " + f.Key.String() + " (build, unit test, bộ case đầy đủ qua harness)...")
	// CI: approval web lấy từ nguồn web của CI (giai đoạn 4), không từ cache ký; gates.json dùng khi repo ở local
	gates := f.Gates
	if ci {
		gates = gate.NewService(f.Paths, f.Flow(), f.Config, gate.NewLocalGateStore(f.Paths), ctx.Clock())
	}
	svc := &eval.Service{Paths: f.Paths, Meta: f.Meta, Config: f.Config, Git: f.Git(),
		Verifier: ctx.Verifier(f.Root, f.Config), Gates: gates, Steps: f.Steps(), Specs: f.Specs(),
		Adapter: f.Adapter, Now: ctx.Clock()}
	// chưa có nguồn approval web (giai đoạn 4): CI chế độ web thì Service fail closed (GATE:WEB)
	var r eval.EvalReport
	if err := ctx.interruptible(func() (err error) {
		r, err = svc.Run(ctx.Std(), ci, base, nil)
		return err
	}); err != nil {
		return 0, err // bị hủy: eval.Service không ghi report
	}
	dir := f.Paths.EvalDir()
	md, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		return 0, err
	}
	fmt.Fprint(ctx.Out(), string(md))
	ctx.Println("Đã ghi " + f.Paths.Rel(filepath.Join(dir, "report.json")) + ".")
	ctx.AfterCommand(f, "eval")
	if r.Passed {
		return ExitOK, nil
	}
	return ExitFindings, nil
}

// managedChanges: file thay đổi không thuộc thư mục tính năng nào và không thuộc unmanaged, đã sắp xếp.
func managedChanges(changes []gitx.FileChange, cfg model.RepoConfig) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range changes {
		for _, p := range changePaths(c) {
			if !strings.HasPrefix(p, evalFeaturesDir) && !step.ScopeMatches(cfg.Unmanaged, p) && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return out
}

func changePaths(c gitx.FileChange) []string {
	if c.OldPath == "" {
		return []string{c.Path}
	}
	return []string{c.Path, c.OldPath}
}

// changedFeatures: thư mục aisdlc/features/<KEY> có feature.yaml và có file thay đổi so với merge-base, theo tên.
func changedFeatures(root string, changes []gitx.FileChange) ([]model.FeatureKey, error) {
	changed := map[string]bool{}
	for _, c := range changes {
		for _, p := range changePaths(c) {
			if rest, ok := strings.CutPrefix(p, evalFeaturesDir); ok {
				if name, _, ok := strings.Cut(rest, "/"); ok && name != "" {
					changed[name] = true
				}
			}
		}
	}
	dir := filepath.Join(root, filepath.FromSlash(evalFeaturesDir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // như Java: chưa có thư mục tính năng thì không có gì để eval
	}
	if err != nil {
		return nil, err // không đọc được (quyền...): fail closed, không coi như không có tính năng
	}
	var out []model.FeatureKey
	for _, e := range entries { // ReadDir đã sắp theo tên
		if !changed[e.Name()] || !exists(filepath.Join(dir, e.Name(), "feature.yaml")) {
			continue
		}
		k, err := model.ParseFeatureKey(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}
