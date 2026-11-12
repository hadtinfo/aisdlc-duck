package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/yamlx"
)

// CheckCommand là một lệnh lint/quét bảo mật của repo.
type CheckCommand struct{ Name, Cmd string }

// ReviewSettings là ngưỡng review lấy từ mục review: của aisdlc/config.yaml.
type ReviewSettings struct {
	ScopeMaxFiles, BudgetMaxFiles, BudgetMaxLines int
	MigrationDirs, ValueFields                    []string
}

// ServiceEnv khai tên các biến môi trường dịch vụ đọc.
type ServiceEnv struct {
	Port, DatabaseURL, KafkaBrokers string
	ExternalBaseURLs                []string
}

// ContainerImage là image container (và topic cho Kafka) harness khởi động.
type ContainerImage struct {
	Image  string
	Topics []string
}

// ServiceConfig là phần service của config.
type ServiceConfig struct {
	Main                                        string
	BuildFlags                                  []string
	HealthPath                                  string
	StartTimeoutSeconds, ShutdownTimeoutSeconds int
	Env                                         ServiceEnv
	ExtraEnv                                    map[string]string
	Postgres, Kafka                             ContainerImage
}

// MigrationsConfig là phần migrations của config.
type MigrationsConfig struct{ Dir, Tool string }

// UnitTestConfig là phần unitTest của config.
type UnitTestConfig struct{ Packages, Flags []string }

// RepoConfig là nội dung aisdlc/config.yaml, đã điền mặc định.
type RepoConfig struct {
	Language         string
	Locale           i18n.Locale // ngôn ngữ của repo: vi (mặc định) hoặc en
	TimeoutMinutes   float64
	Service          ServiceConfig
	Migrations       MigrationsConfig
	UnitTest         UnitTestConfig
	AlwaysAllowed    []string
	Unmanaged        []string
	BudgetWarnFactor float64
	CoverageWarn     float64
	Checks           []CheckCommand
	BaseRef          string
	CIRequireFeature bool
	Review           ReviewSettings
	Approvals        string // "web" | "local"
	SessionApprove   bool
}

func defaultReview(migrationDir string) ReviewSettings {
	return ReviewSettings{
		ScopeMaxFiles: 30, BudgetMaxFiles: 10, BudgetMaxLines: 400,
		MigrationDirs: []string{migrationDir},
		ValueFields:   []string{"amount", "limit", "balance", "fee", "rate", "max", "min"},
	}
}

// LoadRepoConfig đọc aisdlc/config.yaml; không có file thì trả mặc định.
func LoadRepoConfig(repoRoot string) (RepoConfig, error) {
	file := filepath.Join(repoRoot, "aisdlc", "config.yaml")
	if _, err := os.Stat(file); err != nil {
		return configFrom(nil)
	}
	var n any
	if err := yamlx.Read(file, &n); err != nil {
		return RepoConfig{}, err
	}
	return configFrom(n)
}

// ParseRepoConfig đọc cấu hình từ nội dung config.yaml; chuỗi rỗng/blank thì trả mặc định.
func ParseRepoConfig(yaml string) (RepoConfig, error) {
	if strings.TrimSpace(yaml) == "" {
		return configFrom(nil)
	}
	var n any
	if err := yamlx.Parse([]byte(yaml), &n); err != nil {
		return RepoConfig{}, err
	}
	return configFrom(n)
}

// rd gom lỗi giá trị sai kiểu; lỗi đầu tiên được giữ.
type rd struct{ err error }

func (r *rd) bad(path string, v any) {
	if r.err == nil {
		r.err = fmt.Errorf(i18n.T("Giá trị %s không hợp lệ: %v"), path, v)
	}
}

func configFrom(n any) (RepoConfig, error) {
	r := &rd{}
	c := r.config(n)
	if r.err != nil {
		return RepoConfig{}, r.err
	}
	return c, nil
}

func (r *rd) config(n any) RepoConfig {
	svc := get(n, "service")
	env := get(svc, "env")
	mig := get(n, "migrations")
	ut := get(n, "unitTest")
	migDir := str(mig, "dir", "db/migrations")
	loc, err := i18n.Parse(str(n, "locale", ""))
	if err != nil && r.err == nil {
		r.err = err
	}
	return RepoConfig{
		Locale:         loc,
		Language:       str(n, "language", "go"),
		TimeoutMinutes: r.positive(n, "timeoutMinutes", "timeoutMinutes", 30),
		Service: ServiceConfig{
			Main:                   str(svc, "main", "./cmd/server"),
			BuildFlags:             strs(svc, "buildFlags", nil),
			HealthPath:             str(svc, "healthPath", "/healthz"),
			StartTimeoutSeconds:    r.seconds(svc, "service.startTimeoutSeconds", "startTimeoutSeconds", 60),
			ShutdownTimeoutSeconds: r.seconds(svc, "service.shutdownTimeoutSeconds", "shutdownTimeoutSeconds", 15),
			Env: ServiceEnv{
				Port:             str(env, "port", "PORT"),
				DatabaseURL:      str(env, "databaseUrl", ""),
				KafkaBrokers:     str(env, "kafkaBrokers", ""),
				ExternalBaseURLs: strs(env, "externalBaseUrls", nil),
			},
			ExtraEnv: strMap(svc, "extraEnv"),
			Postgres: ContainerImage{Image: str(get(svc, "postgres"), "image", "postgres:16-alpine")},
			Kafka: ContainerImage{
				Image:  str(get(svc, "kafka"), "image", "apache/kafka:3.8.0"),
				Topics: strs(get(svc, "kafka"), "topics", nil),
			},
		},
		Migrations:       MigrationsConfig{Dir: migDir, Tool: str(mig, "tool", "goose")},
		UnitTest:         UnitTestConfig{Packages: strs(ut, "packages", []string{"./..."}), Flags: strs(ut, "flags", nil)},
		AlwaysAllowed:    strs(n, "alwaysAllowed", nil),
		Unmanaged:        strs(n, "unmanaged", []string{"*.md", "docs/**"}),
		BudgetWarnFactor: r.num(n, "budgetWarnFactor", "budgetWarnFactor", 1.5),
		CoverageWarn:     r.num(n, "coverageWarn", "coverageWarn", 0.80),
		Checks:           checks(n),
		BaseRef:          str(n, "baseRef", "origin/main"),
		CIRequireFeature: r.boolean(n, "ciRequireFeature", "ciRequireFeature", true),
		Review:           r.review(get(n, "review"), migDir),
		Approvals:        approvals(n),
		SessionApprove:   r.boolean(n, "sessionApprove", "sessionApprove", false),
	}
}

func approvals(n any) string {
	if strings.ToLower(strings.TrimSpace(str(n, "approvals", "local"))) == "web" {
		return "web"
	}
	return "local"
}

func (r *rd) review(rv any, migDir string) ReviewSettings {
	d := defaultReview(migDir)
	return ReviewSettings{
		ScopeMaxFiles:  int(r.num(rv, "review.scopeMaxFiles", "scopeMaxFiles", float64(d.ScopeMaxFiles))),
		BudgetMaxFiles: int(r.num(rv, "review.budgetMaxFiles", "budgetMaxFiles", float64(d.BudgetMaxFiles))),
		BudgetMaxLines: int(r.num(rv, "review.budgetMaxLines", "budgetMaxLines", float64(d.BudgetMaxLines))),
		MigrationDirs:  strs(rv, "migrationDirs", d.MigrationDirs),
		ValueFields:    strs(rv, "valueFields", d.ValueFields),
	}
}

// get trả giá trị khóa k của node mapping; khóa vắng hoặc null thì trả nil.
func get(n any, k string) any {
	switch m := n.(type) {
	case map[string]any:
		return m[k]
	case map[any]any:
		return m[k]
	}
	return nil
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func str(n any, k, def string) string {
	v := get(n, k)
	if v == nil {
		return def
	}
	return text(v)
}

func (r *rd) boolean(n any, path, k string, def bool) bool {
	switch b := get(n, k).(type) {
	case nil:
		return def
	case bool:
		return b
	case string:
		if p, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
			return p
		}
	}
	r.bad(path, get(n, k))
	return def
}

func (r *rd) num(n any, path, k string, def float64) float64 {
	switch v := get(n, k).(type) {
	case nil:
		return def
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	r.bad(path, get(n, k))
	return def
}

// positive như num nhưng giá trị phải lớn hơn 0 (thời hạn 0 hay âm làm harness dừng ngay hoặc chạy mãi).
func (r *rd) positive(n any, path, k string, def float64) float64 {
	v := r.num(n, path, k, def)
	if v <= 0 {
		r.mustBePositive(path, get(n, k))
		return def
	}
	return v
}

// seconds là số giây nguyên lớn hơn 0 (phần lẻ bị bỏ, như int() của num).
func (r *rd) seconds(n any, path, k string, def int) int {
	v := int(r.num(n, path, k, float64(def)))
	if v <= 0 {
		r.mustBePositive(path, get(n, k))
		return def
	}
	return v
}

func (r *rd) mustBePositive(path string, v any) {
	if r.err == nil {
		r.err = fmt.Errorf(i18n.T("Giá trị %s không hợp lệ: %v (phải lớn hơn 0)"), path, v)
	}
}

func strs(n any, k string, def []string) []string {
	v := get(n, k)
	if v == nil {
		if def == nil {
			return []string{}
		}
		return append([]string{}, def...)
	}
	out := []string{}
	if l, ok := v.([]any); ok {
		for _, e := range l {
			out = append(out, text(e))
		}
	}
	return out
}

func strMap(n any, k string) map[string]string {
	out := map[string]string{}
	switch m := get(n, k).(type) {
	case map[string]any:
		for key, v := range m {
			out[key] = text(v)
		}
	case map[any]any:
		for key, v := range m {
			out[fmt.Sprint(key)] = text(v)
		}
	}
	return out
}

func checks(n any) []CheckCommand {
	out := []CheckCommand{}
	if l, ok := get(n, "checks").([]any); ok {
		for _, e := range l {
			out = append(out, CheckCommand{Name: str(e, "name", ""), Cmd: str(e, "cmd", "")})
		}
	}
	return out
}
