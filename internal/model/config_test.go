package model

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"aisdlc/templates"
)

func writeCfg(t *testing.T, root, yaml string) {
	t.Helper()
	p := filepath.Join(root, "aisdlc", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustParse(t *testing.T, y string) RepoConfig {
	t.Helper()
	c, err := ParseRepoConfig(y)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, muốn %#v", name, got, want)
	}
}

func TestConfigDefaultsWhenMissing(t *testing.T) {
	c, err := LoadRepoConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "BudgetWarnFactor", c.BudgetWarnFactor, 1.5)
	eq(t, "CoverageWarn", c.CoverageWarn, 0.80)
	eq(t, "Unmanaged", c.Unmanaged, []string{"*.md", "docs/**"})
	eq(t, "AlwaysAllowed", c.AlwaysAllowed, []string{})
	eq(t, "Checks", c.Checks, []CheckCommand{})
	eq(t, "BaseRef", c.BaseRef, "origin/main")
	eq(t, "CIRequireFeature", c.CIRequireFeature, true)
	eq(t, "Approvals", c.Approvals, "local")
	eq(t, "SessionApprove", c.SessionApprove, false)
	eq(t, "Language", c.Language, "go")
	eq(t, "TimeoutMinutes", c.TimeoutMinutes, 30.0)
	eq(t, "Service.Main", c.Service.Main, "./cmd/server")
	eq(t, "Service.BuildFlags", c.Service.BuildFlags, []string{})
	eq(t, "Service.HealthPath", c.Service.HealthPath, "/healthz")
	eq(t, "StartTimeoutSeconds", c.Service.StartTimeoutSeconds, 60)
	eq(t, "ShutdownTimeoutSeconds", c.Service.ShutdownTimeoutSeconds, 15)
	eq(t, "Env.Port", c.Service.Env.Port, "PORT")
	eq(t, "Env.DatabaseURL", c.Service.Env.DatabaseURL, "")
	eq(t, "Env.KafkaBrokers", c.Service.Env.KafkaBrokers, "")
	eq(t, "Env.ExternalBaseURLs", c.Service.Env.ExternalBaseURLs, []string{})
	eq(t, "ExtraEnv", c.Service.ExtraEnv, map[string]string{})
	eq(t, "Postgres.Image", c.Service.Postgres.Image, "postgres:16-alpine")
	eq(t, "Kafka.Image", c.Service.Kafka.Image, "apache/kafka:3.8.0")
	eq(t, "Kafka.Topics", c.Service.Kafka.Topics, []string{})
	eq(t, "Migrations.Dir", c.Migrations.Dir, "db/migrations")
	eq(t, "Migrations.Tool", c.Migrations.Tool, "goose")
	eq(t, "UnitTest.Packages", c.UnitTest.Packages, []string{"./..."})
	eq(t, "UnitTest.Flags", c.UnitTest.Flags, []string{})
	eq(t, "Review", c.Review, ReviewSettings{
		ScopeMaxFiles: 30, BudgetMaxFiles: 10, BudgetMaxLines: 400,
		MigrationDirs: []string{"db/migrations"},
		ValueFields:   []string{"amount", "limit", "balance", "fee", "rate", "max", "min"},
	})
}

func TestConfigOverrides(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, "budgetWarnFactor: 2.0\ntimeoutMinutes: 45\nciRequireFeature: false\n"+
		"checks:\n  - name: lint\n    cmd: go vet ./...\n"+
		"service:\n  main: ./cmd/api\n  buildFlags: [\"-tags=prod\"]\n  startTimeoutSeconds: 5\n"+
		"  env:\n    databaseUrl: DATABASE_URL\n    externalBaseUrls: [CORE_URL]\n  extraEnv: { LOG_LEVEL: warn }\n"+
		"  kafka: { topics: [a, b] }\n")
	c, err := LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "BudgetWarnFactor", c.BudgetWarnFactor, 2.0)
	eq(t, "CoverageWarn", c.CoverageWarn, 0.80)
	eq(t, "Unmanaged", c.Unmanaged, []string{"*.md", "docs/**"})
	eq(t, "Checks", c.Checks, []CheckCommand{{Name: "lint", Cmd: "go vet ./..."}})
	eq(t, "TimeoutMinutes", c.TimeoutMinutes, 45.0)
	eq(t, "CIRequireFeature", c.CIRequireFeature, false)
	eq(t, "Service.Main", c.Service.Main, "./cmd/api")
	eq(t, "BuildFlags", c.Service.BuildFlags, []string{"-tags=prod"})
	eq(t, "StartTimeout", c.Service.StartTimeoutSeconds, 5)
	eq(t, "ShutdownTimeout", c.Service.ShutdownTimeoutSeconds, 15)
	eq(t, "Env.Port", c.Service.Env.Port, "PORT")
	eq(t, "Env.DatabaseURL", c.Service.Env.DatabaseURL, "DATABASE_URL")
	eq(t, "Env.KafkaBrokers", c.Service.Env.KafkaBrokers, "")
	eq(t, "ExternalBaseURLs", c.Service.Env.ExternalBaseURLs, []string{"CORE_URL"})
	eq(t, "ExtraEnv", c.Service.ExtraEnv, map[string]string{"LOG_LEVEL": "warn"})
	eq(t, "Kafka.Topics", c.Service.Kafka.Topics, []string{"a", "b"})
	eq(t, "Kafka.Image", c.Service.Kafka.Image, "apache/kafka:3.8.0")
}

func TestApprovalsMode(t *testing.T) {
	eq(t, "web", mustParse(t, "approvals: web\n").Approvals, "web")
	eq(t, "no key", mustParse(t, "ciRequireFeature: true\n").Approvals, "local")
	eq(t, "empty", mustParse(t, "").Approvals, "local")
	eq(t, "lung tung", mustParse(t, "approvals: lung tung\n").Approvals, "local")
}

func TestApprovalsNormalized(t *testing.T) {
	eq(t, "WEB", mustParse(t, "approvals: \" WEB \"\n").Approvals, "web")
	eq(t, "foo", mustParse(t, "approvals: foo\n").Approvals, "local")
}

func TestReviewSettingsDefaultWhenMissing(t *testing.T) {
	eq(t, "Review", mustParse(t, "ciRequireFeature: true\n").Review, defaultReview("db/migrations"))
}

func TestReviewSettingsRead(t *testing.T) {
	r := mustParse(t, "review:\n  scopeMaxFiles: 5\n").Review
	eq(t, "ScopeMaxFiles", r.ScopeMaxFiles, 5)
	eq(t, "BudgetMaxFiles", r.BudgetMaxFiles, 10)
}

func TestConfigParsesYamlTextWithDefaults(t *testing.T) {
	c := mustParse(t, "ciRequireFeature: false\nunmanaged: [\"**\"]\n")
	eq(t, "CIRequireFeature", c.CIRequireFeature, false)
	eq(t, "Unmanaged", c.Unmanaged, []string{"**"})
	eq(t, "BaseRef", c.BaseRef, "origin/main")
	eq(t, "empty CIRequireFeature", mustParse(t, "").CIRequireFeature, true)
	eq(t, "empty Unmanaged", mustParse(t, "").Unmanaged, []string{"*.md", "docs/**"})
	eq(t, "blank", mustParse(t, "  \n\n").BaseRef, "origin/main")
}

func TestConfigNullValuesUseDefaults(t *testing.T) {
	c := mustParse(t, "baseRef: null\nservice:\n  main: ~\nreview:\n  scopeMaxFiles: null\n")
	eq(t, "BaseRef", c.BaseRef, "origin/main")
	eq(t, "Main", c.Service.Main, "./cmd/server")
	eq(t, "ScopeMaxFiles", c.Review.ScopeMaxFiles, 30)
}

func TestConfigIgnoresLegacyJavaKeys(t *testing.T) {
	c := mustParse(t, "mavenCmd: mvn\ncasesTestClass: X\nmavenTimeoutMinutes: 99\ncasesTestPath: p\n")
	eq(t, "TimeoutMinutes", c.TimeoutMinutes, 30.0)
	eq(t, "Language", c.Language, "go")
}

func TestMigrationDirsDefaultFollowsMigrationsDir(t *testing.T) {
	c := mustParse(t, "migrations: {dir: sql}\n")
	eq(t, "MigrationDirs", c.Review.MigrationDirs, []string{"sql"})
	c = mustParse(t, "migrations: {dir: sql}\nreview:\n  migrationDirs: [x]\n")
	eq(t, "explicit", c.Review.MigrationDirs, []string{"x"})
}

func TestConfigSyntaxError(t *testing.T) {
	if _, err := ParseRepoConfig("a: [\n"); err == nil {
		t.Fatal("muốn lỗi")
	}
}

func TestTemplateConfigParses(t *testing.T) {
	data, err := templates.FS.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseRepoConfig(string(data))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "Language", c.Language, "go")
	eq(t, "Main", c.Service.Main, "./cmd/server")
	eq(t, "Env.DatabaseURL", c.Service.Env.DatabaseURL, "DATABASE_URL")
	eq(t, "Env.KafkaBrokers", c.Service.Env.KafkaBrokers, "")
}

func TestWrongTypedValuesAreErrors(t *testing.T) {
	for y, key := range map[string]string{
		"timeoutMinutes: abc":                  "timeoutMinutes",
		"service:\n  startTimeoutSeconds: 60s": "service.startTimeoutSeconds",
		"review:\n  scopeMaxFiles: abc":        "review.scopeMaxFiles",
		"coverageWarn: [1]":                    "coverageWarn",
		"ciRequireFeature: maybe":              "ciRequireFeature",
		"sessionApprove: 3":                    "sessionApprove",
	} {
		_, err := ParseRepoConfig(y)
		if err == nil || !strings.Contains(err.Error(), "Giá trị "+key+" không hợp lệ") {
			t.Errorf("%q: err = %v", y, err)
		}
		root := t.TempDir()
		writeCfg(t, root, y)
		if _, err := LoadRepoConfig(root); err == nil || !strings.Contains(err.Error(), "Giá trị "+key+" không hợp lệ") {
			t.Errorf("Load %q: err = %v", y, err)
		}
	}
	if !strings.Contains(func() string { _, e := ParseRepoConfig("timeoutMinutes: abc"); return e.Error() }(), ": abc") {
		t.Error("error must show the value")
	}
}

func TestNullAndNumericStringsStillAccepted(t *testing.T) {
	c := mustParse(t, "timeoutMinutes: null\nservice:\n  startTimeoutSeconds: \"45\"\nsessionApprove: \"true\"")
	if c.TimeoutMinutes != 30 || c.Service.StartTimeoutSeconds != 45 || !c.SessionApprove {
		t.Errorf("%+v", c)
	}
}

func TestExtraEnvNonStringKeys(t *testing.T) {
	c := mustParse(t, "service:\n  extraEnv: {1: x, A: y}")
	if !reflect.DeepEqual(c.Service.ExtraEnv, map[string]string{"1": "x", "A": "y"}) {
		t.Errorf("%v", c.Service.ExtraEnv)
	}
}
