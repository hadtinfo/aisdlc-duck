package review

import (
	"sort"
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
)

// ScopeMatchedFiles trả các file trong files thuộc scope của bước.
func ScopeMatchedFiles(st spec.StepSpec, files []string) []string {
	return matchedFiles(ScopeMatcher(st.Scope), files)
}

func matchedFiles(scope func(string) bool, files []string) []string {
	out := []string{}
	for _, f := range files {
		if scope(f) {
			out = append(out, f)
		}
	}
	return out
}

// ScopeMatcher biên dịch các glob một lần; glob sai cú pháp bị bỏ qua.
func ScopeMatcher(globs []string) func(string) bool { return step.CompileScope(globs) }

// ValidGlob cho biết glob có biên dịch được không.
func ValidGlob(g string) bool {
	_, err := step.CompileScopeErr([]string{g})
	return err == nil
}

// ScopeFlags sinh cờ về scope và budget của các bước; không phụ thuộc BRD.
// sampleFile là tên file mẫu của ngôn ngữ (adapter.SampleFileName()).
func ScopeFlags(steps []spec.StepSpec, files []string, s model.ReviewSettings, sampleFile string) []ReviewFlag {
	out := []ReviewFlag{}
	steps = dropBadGlobs(steps, &out)
	scopes := make([]func(string) bool, len(steps))
	for i, st := range steps {
		scopes[i] = ScopeMatcher(st.Scope)
	}
	for i := range steps {
		for j := i + 1; j < len(steps); j++ {
			if f, ok := overlap(steps[i], steps[j], scopes[i], scopes[j], files, sampleFile); ok {
				out = append(out, f)
			}
		}
	}
	for i, st := range steps {
		matched := matchedFiles(scopes[i], files)
		wide := false
		for _, g := range st.Scope {
			if g == "**" || g == "src/**" {
				wide = true
			}
		}
		if wide || len(matched) > s.ScopeMaxFiles {
			out = append(out, NewFlag("SCOPE_TOO_WIDE", WARN,
				i18n.Tf("%s: scope quá rộng (%d file đang khớp, ngưỡng %d)", st.ID, len(matched), s.ScopeMaxFiles),
				i18n.T("Thu hẹp scope về đúng package/thư mục của bước."), []string{st.ID}))
		}
		var old []string
		for _, f := range matched {
			if inMigrationDir(f, s) {
				old = append(old, f)
			}
		}
		if len(old) > 0 {
			sorted := sortedSet(old)
			refs := append([]string{st.ID}, sorted...)
			out = append(out, NewFlag("SCOPE_MATCHES_OLD_MIGRATION", WARN,
				i18n.Tf("%s: scope khớp migration đã có (%s)", st.ID, strings.Join(sorted, ", ")),
				i18n.T("Không sửa migration đã chạy; thêm file migration mới."), refs))
		}
		if b := st.Budget; b != nil && (b.Files > s.BudgetMaxFiles || b.Lines > s.BudgetMaxLines) {
			out = append(out, NewFlag("BUDGET_HIGH", WARN,
				i18n.Tf("%s: budget %d file / %d dòng vượt ngưỡng %d file / %d dòng", st.ID, b.Files, b.Lines, s.BudgetMaxFiles, s.BudgetMaxLines),
				i18n.T("Cân nhắc tách bước nhỏ hơn."), []string{st.ID}))
		}
	}
	return out
}

// sortedSet bỏ trùng và sắp xếp (như TreeSet).
func sortedSet(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// dropBadGlobs bỏ glob không hợp lệ khỏi từng bước (kèm cờ NA); các glob còn lại vẫn được kiểm tra.
func dropBadGlobs(steps []spec.StepSpec, out *[]ReviewFlag) []spec.StepSpec {
	clean := make([]spec.StepSpec, 0, len(steps))
	for _, st := range steps {
		ok := []string{}
		for _, g := range st.Scope {
			if ValidGlob(g) {
				ok = append(ok, g)
			} else {
				*out = append(*out, NewFlag("SCOPE_BAD_GLOB", NA,
					i18n.Tf("%s: glob không hợp lệ nên bỏ qua: %s", st.ID, g),
					i18n.T("Sửa glob trong design.yaml."), []string{st.ID, g}))
			}
		}
		c := st
		c.Scope = ok
		clean = append(clean, c)
		if len(st.Scope) == 0 {
			*out = append(*out, NewFlag("SCOPE_NO_SCOPE", NA, i18n.Tf("%s: bước không có scope", st.ID),
				i18n.T("Khai báo scope trong design.yaml để kiểm tra phạm vi."), []string{st.ID}))
		}
	}
	return clean
}

func overlap(a, b spec.StepSpec, pa, pb func(string) bool, files []string, sampleFile string) (ReviewFlag, bool) {
	area, ok := sampleHit(a, pa, pb, sampleFile)
	if !ok {
		area, ok = sampleHit(b, pb, pa, sampleFile)
	}
	if !ok {
		for _, f := range files {
			if pa(f) && pb(f) {
				area, ok = f, true
				break
			}
		}
	}
	if !ok {
		return ReviewFlag{}, false
	}
	refs := []string{a.ID, b.ID, area}
	sort.SliceStable(refs, func(i, j int) bool { return NaturalCompare(refs[i], refs[j]) < 0 })
	return NewFlag("SCOPE_OVERLAP", HIGH,
		i18n.Tf("%s và %s có scope chồng lấn ở %s", a.ID, b.ID, area),
		i18n.T("Tách scope để mỗi file chỉ thuộc một bước."), refs), true
}

// sampleHit trả glob của from mà đường dẫn mẫu sinh từ nó cũng khớp scope của to.
func sampleHit(from spec.StepSpec, fromScope, toScope func(string) bool, sampleFile string) (string, bool) {
	for _, g := range from.Scope {
		s := SampleFor(g, sampleFile)
		if fromScope(s) && toScope(s) {
			return g, true
		}
	}
	return "", false
}

// SampleFor: "**/" thành "aisdlcx/", "**" ở cuối thành "aisdlcx/<sampleFile>", "*" thành "aisdlcx", "?" thành "x".
func SampleFor(glob, sampleFile string) string {
	var sb strings.Builder
	for i := 0; i < len(glob); {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			sb.WriteString("aisdlcx/")
			i += 3
		case strings.HasPrefix(glob[i:], "**"):
			if i+2 == len(glob) {
				sb.WriteString("aisdlcx/" + sampleFile)
			} else {
				sb.WriteString("aisdlcx")
			}
			i += 2
		case glob[i] == '*':
			sb.WriteString("aisdlcx")
			i++
		case glob[i] == '?':
			sb.WriteByte('x')
			i++
		default:
			sb.WriteByte(glob[i])
			i++
		}
	}
	return sb.String()
}

func inMigrationDir(file string, s model.ReviewSettings) bool { return inDirs(file, s.MigrationDirs) }

func inDirs(file string, dirs []string) bool {
	for _, d := range dirs {
		if !strings.HasSuffix(d, "/") {
			d += "/"
		}
		if strings.HasPrefix(file, d) || strings.Contains(file, "/"+d) {
			return true
		}
	}
	return false
}
