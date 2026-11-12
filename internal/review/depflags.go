package review

import (
	"context"
	"strings"

	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
)

// DependencyFlags so dependency giữa base tree và working tree qua adapter ngôn ngữ (Java đọc pom.xml).
// Adapter báo không so được thì ra cờ mức NA kèm lý do của adapter.
func DependencyFlags(ctx context.Context, adapter lang.Adapter, git *gitx.Repo, baseTree, repoRoot string, files []string) []ReviewFlag {
	ch := adapter.DependencyChanges(ctx, lang.TreeSource(git, baseTree), lang.DirSource(repoRoot, files))
	if ch.NA != "" {
		return []ReviewFlag{NewFlag("DEPENDENCY_ADDED", NA, ch.NA, "", nil)}
	}
	if len(ch.Added) == 0 {
		return nil
	}
	return []ReviewFlag{NewFlag("DEPENDENCY_ADDED", WARN,
		i18n.Tf("Thêm dependency mới: %s", strings.Join(ch.Added, ", ")),
		i18n.T("Xác nhận thư viện được phép dùng và đã có trong thiết kế."), ch.Added)}
}
