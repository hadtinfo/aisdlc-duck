package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/risk"
)

// LoadedPolicy là chính sách rủi ro đang hiệu lực; Origin là "file" hoặc "web".
type LoadedPolicy struct {
	Policy risk.RiskPolicy
	Origin string
}

// LoadPolicy là chính sách rủi ro đang hiệu lực của repo (PolicySource.load, spec §4): đã `aisdlc web login` thì chỉ
// đọc cache đã ký .aisdlc/web/policy.json (không lặng lẽ dùng file trong repo); không có web thì
// aisdlc/risk-policy.yaml, thiếu file thì mẫu generic (bỏ flow) kèm cảnh báo (quiet: không in). Không gọi mạng.
func LoadPolicy(ctx *Context, root string, quiet bool) (LoadedPolicy, error) {
	web := NewWebCache(ctx, root)
	if web.PolicyWebMode() {
		c, ok := web.Policy()
		if !ok {
			return LoadedPolicy{}, abort(2,
				"Chưa có chính sách rủi ro từ web (hoặc chữ ký sai). Chạy: aisdlc policy pull")
		}
		p, err := parsePolicy(c.YAML, "Chính sách rủi ro v"+strconv.Itoa(c.Version)+" trên web")
		if err != nil {
			return LoadedPolicy{}, err
		}
		p.Version = c.Version // phiên bản trên web là phiên bản của chính sách (YAML có thể ghi số khác)
		return LoadedPolicy{p, "web"}, nil
	}
	f := filepath.Join(root, RepoPolicyFile)
	if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
		if !quiet {
			ctx.Eprintln("Chưa có " + RepoPolicyFile + "; dùng mẫu generic. Chạy aisdlc setup --preset <" +
				strings.Join(risk.PresetNames(), "|") + "> để chép mẫu vào repo.")
		}
		p, err := risk.LatestPreset("generic")
		if err != nil {
			return LoadedPolicy{}, err
		}
		return LoadedPolicy{withoutFlow(p), "file"}, nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return LoadedPolicy{}, errors.New("Không đọc được " + RepoPolicyFile + ": " + err.Error())
	}
	p, err := parsePolicy(string(b), RepoPolicyFile)
	if err != nil {
		return LoadedPolicy{}, err
	}
	return LoadedPolicy{p, "file"}, nil
}

// TryLoadPolicy như LoadPolicy nhưng mọi lỗi (thiếu cache, chính sách hỏng) thành ok=false; không in gì.
func TryLoadPolicy(ctx *Context, root string) (l LoadedPolicy, ok bool) {
	defer func() {
		if recover() != nil {
			l, ok = LoadedPolicy{}, false
		}
	}()
	l, err := LoadPolicy(ctx, root, true)
	return l, err == nil
}

func parsePolicy(yaml, where string) (risk.RiskPolicy, error) {
	p, err := risk.ParsePolicy(yaml)
	if err == nil {
		return p, nil
	}
	var pe *risk.PolicyError
	if !errors.As(err, &pe) {
		return risk.RiskPolicy{}, err
	}
	var b strings.Builder
	b.WriteString(where + " không hợp lệ:")
	for _, x := range pe.Errors {
		b.WriteString("\n  - " + x)
	}
	return risk.RiskPolicy{}, abort(2, b.String())
}

// withoutFlow là mẫu dự phòng khi repo chưa có file chính sách: bỏ flow để mọi cổng duyệt tay. Repo bật tự duyệt bằng
// cách ghi file (setup, policy pull); nâng cấp CLI không lặng lẽ bắt đầu tự duyệt.
func withoutFlow(p risk.RiskPolicy) risk.RiskPolicy {
	p.Flow = map[string]map[string]string{}
	return p
}
