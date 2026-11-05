package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		return configFrom(nil), nil
	}
	var n any
	if err := yamlx.Read(file, &n); err != nil {
		return RepoConfig{}, err
	}
	return configFrom(n), nil
}

// ParseRepoConfig đọc cấu hình từ nội dung config.yaml; chuỗi rỗng/blank thì trả mặc định.
func ParseRepoConfig(yaml string) (RepoConfig, error) {
	if strings.TrimSpace(yaml) == "" {
		return configFrom(nil), nil
	}
	var n any
	if err := yamlx.Parse([]byte(yaml), &n); err != nil {
		return RepoConfig{}, err
	}
	return configFrom(n), nil
}

func configFrom(n any) RepoConfig {
	svc := get(n, "service")
	env := get(svc, "env")
	mig := get(n, "migrations")
	ut := get(n, "unitTest")
	migDir := str(mig, "dir", "db/migrations")
	return RepoConfig{
		Language:       str(n, "language", "go"),
		TimeoutMinutes: num(n, "timeoutMinutes", 30),
		Service: ServiceConfig{
			Main:                   str(svc, "main", "./cmd/server"),
			BuildFlags:             strs(svc, "buildFlags", nil),
			HealthPath:             str(svc, "healthPath", "/healthz"),
			StartTimeoutSeconds:    int(num(svc, "startTimeoutSeconds", 60)),
			ShutdownTimeoutSeconds: int(num(svc, "shutdownTimeoutSeconds", 15)),
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
		BudgetWarnFactor: num(n, "budgetWarnFactor", 1.5),
		CoverageWarn:     num(n, "coverageWarn", 0.80),
		Checks:           checks(n),
		BaseRef:          str(n, "baseRef", "origin/main"),
		CIRequireFeature: boolean(n, "ciRequireFeature", true),
		Review:           review(get(n, "review"), migDir),
		Approvals:        approvals(n),
		SessionApprove:   boolean(n, "sessionApprove", false),
	}
}

func approvals(n any) string {
	if strings.ToLower(strings.TrimSpace(str(n, "approvals", "local"))) == "web" {
		return "web"
	}
	return "local"
}

func review(r any, migDir string) ReviewSettings {
	d := defaultReview(migDir)
	return ReviewSettings{
		ScopeMaxFiles:  int(num(r, "scopeMaxFiles", float64(d.ScopeMaxFiles))),
		BudgetMaxFiles: int(num(r, "budgetMaxFiles", float64(d.BudgetMaxFiles))),
		BudgetMaxLines: int(num(r, "budgetMaxLines", float64(d.BudgetMaxLines))),
		MigrationDirs:  strs(r, "migrationDirs", d.MigrationDirs),
		ValueFields:    strs(r, "valueFields", d.ValueFields),
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

func boolean(n any, k string, def bool) bool {
	switch b := get(n, k).(type) {
	case bool:
		return b
	case string:
		if p, err := strconv.ParseBool(strings.TrimSpace(b)); err == nil {
			return p
		}
	}
	return def
}

func num(n any, k string, def float64) float64 {
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
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	}
	return 0
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
	if m, ok := get(n, k).(map[string]any); ok {
		for key, v := range m {
			out[key] = text(v)
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
