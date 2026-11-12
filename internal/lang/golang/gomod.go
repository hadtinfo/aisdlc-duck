package golang

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
)

func isGoMod(p string) bool { return p == "go.mod" || strings.HasSuffix(p, "/go.mod") }

func entries(ctx context.Context, src lang.FileSource) (map[string]bool, int, error) {
	files, err := src.List(ctx)
	if err != nil {
		return nil, 0, err
	}
	set := map[string]bool{}
	n := 0
	for _, f := range files {
		if !isGoMod(f) {
			continue
		}
		n++
		data, err := src.Read(ctx, f)
		if err != nil {
			return nil, n, err
		}
		mf, err := modfile.Parse(f, data, nil)
		if err != nil {
			return nil, n, err
		}
		for _, r := range mf.Require {
			set[r.Mod.Path] = true
		}
		for _, r := range mf.Replace {
			set[fmt.Sprintf("replace %s => %s", ref(r.Old.Path, r.Old.Version), ref(r.New.Path, r.New.Version))] = true
		}
	}
	return set, n, nil
}

func ref(path, version string) string {
	if version == "" {
		return path
	}
	return path + " " + version
}

// DependencyChanges: module require và replace có ở now mà không có ở base.
func (Adapter) DependencyChanges(ctx context.Context, base, now lang.FileSource) lang.DependencyChanges {
	nowSet, n, err := entries(ctx, now)
	if err == nil && n == 0 {
		return lang.DependencyChanges{NA: i18n.T("Repo không có go.mod, bỏ qua kiểm tra dependency.")}
	}
	var baseSet map[string]bool
	if err == nil {
		baseSet, _, err = entries(ctx, base)
	}
	if err != nil {
		why := strings.SplitN(err.Error(), "\n", 2)[0]
		return lang.DependencyChanges{NA: i18n.Tf("Không đọc được go.mod hoặc base tree: %s", why)}
	}
	var added []string
	for k := range nowSet {
		if !baseSet[k] {
			added = append(added, k)
		}
	}
	sort.Strings(added)
	return lang.DependencyChanges{Added: added}
}
