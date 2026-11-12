package risk

import (
	"embed"
	"io/fs"
	"regexp"
	"strconv"

	"aisdlc/internal/i18n"
)

// Mẫu chính sách đi kèm AISDLC: presets/<name>/<version>.yaml. Thêm file mới khi đổi mẫu, không xóa file cũ.
//
//go:embed presets
var presetFS embed.FS

var presetNames = []string{"banking", "generic"}

var versionRe = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)

// PresetNames là tên các mẫu có sẵn.
func PresetNames() []string { return append([]string(nil), presetNames...) }

func requirePreset(loc i18n.Locale, name string) error {
	if !contains(presetNames, name) {
		return policyErr(loc, loc.Tf("Không có mẫu '%s'; các mẫu có sẵn: %s", name, listText(presetNames)))
	}
	return nil
}

func presetPath(name, version string) (string, bool) {
	if !versionRe.MatchString(version) {
		return "", false
	}
	p := "presets/" + name + "/" + version + ".yaml"
	if _, err := fs.Stat(presetFS, p); err != nil {
		return "", false
	}
	return p, true
}

// PresetLatestVersion trả phiên bản cao nhất (1, 2, ... liên tục) của mẫu; "" nếu tên mẫu không có.
func PresetLatestVersion(name string) string {
	if requirePreset(i18n.VI, name) != nil {
		return ""
	}
	latest := 0
	for v := 1; ; v++ {
		if _, ok := presetPath(name, strconv.Itoa(v)); !ok {
			break
		}
		latest = v
	}
	if latest == 0 {
		return ""
	}
	return strconv.Itoa(latest)
}

// PresetYAML trả nguyên văn YAML của một phiên bản mẫu.
func PresetYAML(name, version string) (string, error) {
	return PresetYAMLIn(i18n.Default(), name, version)
}

// PresetYAMLIn như PresetYAML, lỗi theo loc.
func PresetYAMLIn(loc i18n.Locale, name, version string) (string, error) {
	if err := requirePreset(loc, name); err != nil {
		return "", err
	}
	p, ok := presetPath(name, version)
	if !ok {
		return "", policyErr(loc, loc.Tf("Không có mẫu '%s' phiên bản '%s'", name, version))
	}
	b, err := presetFS.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LoadPreset đọc một phiên bản mẫu.
func LoadPreset(name, version string) (RiskPolicy, error) {
	return LoadPresetIn(i18n.Default(), name, version)
}

// LoadPresetIn như LoadPreset, lỗi và cảnh báo theo loc.
func LoadPresetIn(loc i18n.Locale, name, version string) (RiskPolicy, error) {
	y, err := PresetYAMLIn(loc, name, version)
	if err != nil {
		return RiskPolicy{}, err
	}
	return ParsePolicyIn(loc, y)
}

// LatestPreset đọc phiên bản mới nhất của mẫu.
func LatestPreset(name string) (RiskPolicy, error) { return LatestPresetIn(i18n.Default(), name) }

// LatestPresetIn như LatestPreset, lỗi và cảnh báo theo loc.
func LatestPresetIn(loc i18n.Locale, name string) (RiskPolicy, error) {
	if err := requirePreset(loc, name); err != nil {
		return RiskPolicy{}, err
	}
	v := PresetLatestVersion(name)
	if v == "" {
		return RiskPolicy{}, policyErr(loc, loc.Tf("Mẫu '%s' không có phiên bản nào", name))
	}
	return LoadPresetIn(loc, name, v)
}
