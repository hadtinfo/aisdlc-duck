package cli

import (
	"strings"
	"testing"

	"aisdlc/internal/i18n"
)

// localeRepo dựng repo đã setup, ghi thêm dòng locale (rỗng: không có khóa) vào config.
func localeRepo(t *testing.T, line string, env map[string]string) *testRepo {
	t.Helper()
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	r := newTestRepo(t)
	r.setupInit()
	cfg := r.read("aisdlc/config.yaml")
	var kept []string
	for _, l := range strings.Split(cfg, "\n") {
		if !strings.HasPrefix(l, "locale:") {
			kept = append(kept, l)
		}
	}
	cfg = strings.Join(kept, "\n")
	if line != "" {
		cfg += "\n" + line + "\n"
	}
	r.write("aisdlc/config.yaml", cfg)
	r.env = env
	return r
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func TestStatusLocaleFromConfig(t *testing.T) {
	r := localeRepo(t, "locale: en", nil)
	eq(t, r.run("status"), 0)
	eq(t, firstLine(r.out), "Locale: en")
	eq(t, i18n.Default(), i18n.EN)
}

func TestStatusLocaleFromEnvWhenKeyMissing(t *testing.T) {
	r := localeRepo(t, "", map[string]string{"AISDLC_LOCALE": "en"})
	eq(t, r.run("status"), 0)
	eq(t, firstLine(r.out), "Locale: en")
}

func TestStatusLocaleConfigBeatsEnv(t *testing.T) {
	r := localeRepo(t, "locale: vi", map[string]string{"AISDLC_LOCALE": "en"})
	eq(t, r.run("status"), 0)
	eq(t, firstLine(r.out), "Locale: vi")
	eq(t, i18n.Default(), i18n.VI)
}

func TestStatusLocaleDefaultsToVi(t *testing.T) {
	r := localeRepo(t, "", map[string]string{"AISDLC_LOCALE": "xx"})
	eq(t, r.run("status"), 0)
	eq(t, firstLine(r.out), "Locale: vi")
}

func TestBadLocaleInConfigExits2(t *testing.T) {
	r := localeRepo(t, "locale: fr", nil)
	eq(t, r.run("status"), 2)
	contains(t, r.err, "fr")
	eq(t, r.run("hook", "pre-tool-use"), 2)
	contains(t, r.err, "fr")
}

func TestBadValueElsewhereDoesNotFailLocaleStep(t *testing.T) {
	r := localeRepo(t, "locale: en", nil)
	r.write("aisdlc/config.yaml", "locale: en\ntimeoutMinutes: abc\n")
	eq(t, r.run("status"), 2)
	contains(t, r.err, "timeoutMinutes") // lỗi của khóa khác, không phải của locale
	eq(t, i18n.Default(), i18n.EN)
}

func TestStatusPrintsLocaleEvenWithoutFeature(t *testing.T) {
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	r := newTestRepo(t)
	eq(t, r.run("setup"), 0)
	eq(t, r.run("status"), 2)
	eq(t, firstLine(r.out), "Locale: vi")
}
