package cli

import (
	"strings"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/templates"
)

func restoreLocale(t *testing.T) {
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
}

func mustTemplate(t *testing.T, l i18n.Locale, name string) string {
	s, err := templates.Read(l, name)
	must(t, err)
	return s
}

func TestSetupLocaleEnWritesEnglishTemplates(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	eq(t, r.run("setup", "--locale", "en"), 0)
	contains(t, r.read("aisdlc/config.yaml"), "locale: en")
	contains(t, r.read("CLAUDE.md"), "Feature documents live in", "Agents write documents and reply to the dev in English.")
	contains(t, r.read("aisdlc/gitlab-ci.aisdlc.yml"), "AISDLC eval job")
}

func TestSetupLocaleInvalidExits2(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	eq(t, r.run("setup", "--locale", "fr"), 2)
	contains(t, r.err, "fr")
	eq(t, r.exists("aisdlc/config.yaml"), false)
}

func TestSetupLocaleFromEnv(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	r.env["AISDLC_LOCALE"] = "en"
	eq(t, r.run("setup"), 0)
	contains(t, r.read("aisdlc/config.yaml"), "locale: en")
	contains(t, r.read("CLAUDE.md"), "Feature documents live in")
}

func TestSetupLocaleFlagBeatsEnv(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	r.env["AISDLC_LOCALE"] = "en"
	eq(t, r.run("setup", "--locale", "vi"), 0)
	contains(t, r.read("aisdlc/config.yaml"), "locale: vi")
}

func TestSetupDefaultIsVietnamese(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	eq(t, r.run("setup"), 0)
	contains(t, r.read("aisdlc/config.yaml"), "locale: vi")
	eq(t, r.read("CLAUDE.md"), mustTemplate(t, i18n.VI, "CLAUDE.aisdlc.md"))
}

func TestSetupKeepsEnglishDetectedServiceLines(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	r.write("cmd/pay/main.go", "package main\n")
	r.write("migrations/.keep", "")
	eq(t, r.run("setup", "--locale", "en"), 0)
	cfg := r.read("aisdlc/config.yaml")
	contains(t, cfg, "  main: ./cmd/pay\n", "  dir: migrations\n")
	if strings.Contains(cfg, "./cmd/server") {
		t.Error("main chưa được thay")
	}
}

func TestInitUsesRepoLocaleTemplates(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	eq(t, r.run("setup", "--locale", "en"), 0)
	eq(t, r.run("init", "PAY-1"), 0)
	eq(t, r.read("aisdlc/features/PAY-1/brd.md"), mustTemplate(t, i18n.EN, "brd.md"))
	eq(t, r.read("aisdlc/features/PAY-1/design.md"), mustTemplate(t, i18n.EN, "design.md"))
	eq(t, r.read("aisdlc/features/PAY-1/cases.yaml"), mustTemplate(t, i18n.EN, "cases.full.yaml"))
	eq(t, r.run("init", "PAY-2", "--lite"), 0)
	eq(t, r.read("aisdlc/features/PAY-2/cases.yaml"), mustTemplate(t, i18n.EN, "cases.lite.yaml"))
}

func TestInitWithoutLocaleKeyUsesVietnamese(t *testing.T) {
	restoreLocale(t)
	r := setupRepo(t)
	eq(t, r.run("setup"), 0)
	// repo cũ: config không có khóa locale
	cfg := strings.Replace(r.read("aisdlc/config.yaml"), "locale: vi\n", "", 1)
	r.write("aisdlc/config.yaml", cfg)
	eq(t, r.run("init", "PAY-1"), 0)
	eq(t, r.read("aisdlc/features/PAY-1/brd.md"), mustTemplate(t, i18n.VI, "brd.md"))
}
