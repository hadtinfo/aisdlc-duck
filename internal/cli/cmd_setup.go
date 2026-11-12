package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/fsx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/risk"
	"aisdlc/templates"
)

// RepoPolicyFile là chính sách rủi ro trong repo (PolicySource.REPO_FILE của Java).
const RepoPolicyFile = "aisdlc/risk-policy.yaml"

const claudeMarker = "<!-- aisdlc -->"

// migrationCandidates là các thư mục migration thường gặp, theo thứ tự ưu tiên khi dò.
var migrationCandidates = []string{"db/migrations", "migrations", "db/migration", "sql/migrations"}

// template đọc template nhúng của locale l.
func template(l i18n.Locale, name string) (string, error) {
	s, err := templates.Read(l, name)
	if err != nil {
		return "", abort(2, "Thiếu template "+name)
	}
	return s, nil
}

func newSetupCmd(ctx *Context) *cobra.Command {
	var preset, approvals, locale string
	c := &cobra.Command{
		Use:   "setup",
		Short: "Cài AISDLC vào repo dịch vụ (chạy ở gốc repo; chạy lại không đổi gì).",
		Args:  cobra.NoArgs,
	}
	c.Flags().StringVar(&preset, "preset", "generic",
		"Mẫu chính sách rủi ro chép vào aisdlc/risk-policy.yaml (generic, banking). Mặc định generic.")
	c.Flags().StringVar(&approvals, "approvals", "",
		"Nơi duyệt cổng ghi vào aisdlc/config.yaml: web hoặc local. Mặc định web nếu máy đã"+
			" `aisdlc web login`, ngược lại local.")
	c.Flags().StringVar(&locale, "locale", "",
		"Ngôn ngữ của template và tài liệu: vi hoặc en. Mặc định theo locale của repo (aisdlc/config.yaml); chưa có config thì AISDLC_LOCALE, không có thì vi.")
	c.RunE = Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
		return setup(ctx, preset, approvals, cmd.Flags().Changed("approvals"), locale, cmd.Flags().Changed("locale"))
	})
	return c
}

func setup(ctx *Context, preset, approvals string, approvalsSet bool, localeFlag string, localeSet bool) (int, error) {
	if !slices.Contains(risk.PresetNames(), preset) {
		return 0, abort(2, "Không có mẫu chính sách '"+preset+"'; các mẫu có sẵn: "+
			strings.Join(risk.PresetNames(), ", ")+".")
	}
	if approvalsSet && approvals != "web" && approvals != "local" {
		return 0, abort(2, "--approvals chỉ nhận web hoặc local, không phải '"+approvals+"'.")
	}
	// Thứ tự chọn locale: --locale, rồi AISDLC_LOCALE (sai thì bỏ qua), rồi vi.
	loc := i18n.FromEnv(ctx.Env)
	if localeSet {
		l, err := i18n.Parse(localeFlag)
		if err != nil {
			return 0, abort(2, err.Error())
		}
		loc = l
	}
	i18n.SetDefault(loc)
	_, loggedIn := LoadWebConfig(ctx)
	web := loggedIn
	if approvalsSet {
		web = approvals == "web"
	}
	root, ok := FindRepo(ctx.cwd)
	if !ok {
		root = ctx.cwd
	}

	cfg, err := template(loc, "config.yaml")
	if err != nil {
		return 0, err
	}
	cfg, hints := detectService(root, cfg)
	if web {
		cfg += "# Duyệt cổng trên web (docs/user-guide.md mục 6.3); bỏ dòng này để duyệt local (gates.json).\napprovals: web\n"
	}
	newConfig, err := createFile(ctx, root, "aisdlc/config.yaml", cfg)
	if err != nil {
		return 0, err
	}
	ci, err := template(loc, "gitlab-ci.aisdlc.yml")
	if err != nil {
		return 0, err
	}
	if _, err := createFile(ctx, root, "aisdlc/gitlab-ci.aisdlc.yml", ci); err != nil {
		return 0, err
	}
	policy, err := risk.PresetYAML(preset, risk.PresetLatestVersion(preset))
	if err != nil {
		return 0, err
	}
	if _, err := createFile(ctx, root, RepoPolicyFile, policy); err != nil {
		return 0, err
	}

	if err := appendClaudeMd(ctx, root, loc); err != nil {
		return 0, err
	}
	if err := appendGitignore(ctx, root); err != nil {
		return 0, err
	}

	if newConfig {
		for _, h := range hints {
			ctx.Println(h)
		}
		ctx.Println("")
		if web {
			ctx.Println("Chế độ duyệt: web. Dev duyệt cổng trên trang tính năng của web.")
			if !loggedIn {
				ctx.Println("  Máy này chưa có cấu hình web: chạy `aisdlc web login` trước khi submit.")
			}
			ctx.Println("  CI cần biến AISDLC_WEB_URL và AISDLC_CI_TOKEN (Masked, Protected).")
		} else {
			ctx.Println("Chế độ duyệt: local (gates.json), vì máy này chưa `aisdlc web login`.")
			ctx.Println("  Muốn duyệt trên web: thêm `approvals: web` vào aisdlc/config.yaml (docs/user-guide.md mục 6.3).")
		}
	}

	ctx.Println("")
	ctx.Println("Việc còn lại trong dịch vụ:")
	ctx.Println("  1. Kiểm tra mục service và migrations trong aisdlc/config.yaml (package main, tên biến môi trường).")
	ctx.Println("  2. Dịch vụ cần endpoint health check (service.healthPath) và tắt êm khi nhận SIGTERM" +
		" để eval đo coverage dòng mới.")
	ctx.Println("  3. Migration viết cho goose, đặt trong migrations.dir.")
	return ExitOK, nil
}

// detectService dò package main (cmd/*/main.go, đúng một) và thư mục migration để điền vào config.yaml mẫu; trả
// thêm lời nhắc cho phần không dò được.
func detectService(root, cfg string) (string, []string) {
	var hints []string
	mains, _ := filepath.Glob(filepath.Join(root, "cmd", "*", "main.go"))
	if len(mains) == 1 {
		pkg := "./cmd/" + filepath.Base(filepath.Dir(mains[0]))
		cfg = strings.Replace(cfg, "  main: ./cmd/server\n", "  main: "+pkg+"\n", 1)
	} else {
		hints = append(hints, "Chưa dò được package main (cần đúng một cmd/*/main.go): sửa service.main trong"+
			" aisdlc/config.yaml.")
	}
	found := false
	for _, d := range migrationCandidates {
		if isDir(filepath.Join(root, filepath.FromSlash(d))) {
			cfg = strings.Replace(cfg, "  dir: db/migrations\n", "  dir: "+d+"\n", 1)
			found = true
			break
		}
	}
	if !found {
		hints = append(hints, "Chưa dò được thư mục migration: mặc định db/migrations; sửa migrations.dir trong"+
			" aisdlc/config.yaml nếu khác.")
	}
	return cfg, hints
}

// createFile tạo file nếu chưa có; true khi vừa tạo.
func createFile(ctx *Context, root, rel, content string) (bool, error) {
	f := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Lstat(f); err == nil {
		ctx.Println("Giữ nguyên " + rel + " (đã có).")
		return false, nil
	}
	if err := fsx.WriteAtomic(f, []byte(content), 0o644); err != nil {
		return false, err
	}
	ctx.Println("Đã tạo " + rel)
	return true, nil
}

func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

func appendClaudeMd(ctx *Context, root string, loc i18n.Locale) error {
	f := filepath.Join(root, "CLAUDE.md")
	existing, err := readOptional(f)
	if err != nil {
		return err
	}
	if strings.Contains(existing, claudeMarker) {
		ctx.Println("Giữ nguyên CLAUDE.md (đã có mục AISDLC).")
		return nil
	}
	tpl, err := template(loc, "CLAUDE.aisdlc.md")
	if err != nil {
		return err
	}
	sep := ""
	if existing != "" {
		sep = "\n\n"
		if strings.HasSuffix(existing, "\n") {
			sep = "\n"
		}
	}
	if err := fsx.WriteAtomic(f, []byte(existing+sep+tpl), 0o644); err != nil {
		return err
	}
	ctx.Println("Đã nối mục AISDLC vào CLAUDE.md.")
	return nil
}

func appendGitignore(ctx *Context, root string) error {
	f := filepath.Join(root, ".gitignore")
	ignore, err := readOptional(f)
	if err != nil {
		return err
	}
	for _, l := range strings.Split(ignore, "\n") {
		if t := strings.TrimSpace(l); t == ".aisdlc/" || t == ".aisdlc" {
			ctx.Println("Giữ nguyên .gitignore (đã có .aisdlc/).")
			return nil
		}
	}
	sep := ""
	if ignore != "" && !strings.HasSuffix(ignore, "\n") {
		sep = "\n"
	}
	if err := fsx.WriteAtomic(f, []byte(ignore+sep+".aisdlc/\n"), 0o644); err != nil {
		return err
	}
	ctx.Println("Đã thêm .aisdlc/ vào .gitignore.")
	return nil
}
