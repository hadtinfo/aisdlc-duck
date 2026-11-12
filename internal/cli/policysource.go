package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/risk"
)

// LoadedPolicy là chính sách rủi ro đang hiệu lực; Origin là "file" hoặc "web".
type LoadedPolicy struct {
	Policy risk.RiskPolicy
	Origin string
}

// LoadPolicy là chính sách rủi ro đang hiệu lực của repo (PolicySource.load, spec §4), dùng cho lệnh cấp repo
// (`aisdlc policy ...`): đã `aisdlc web login` thì chỉ đọc cache đã ký .aisdlc/web/policy.json (không lặng lẽ dùng file
// trong repo); không có web thì aisdlc/risk-policy.yaml, thiếu file thì mẫu generic (bỏ flow) kèm cảnh báo (quiet:
// không in). Không gọi mạng. Lệnh của một tính năng dùng FeaturePolicy.
func LoadPolicy(ctx *Context, root string, quiet bool) (LoadedPolicy, error) {
	return loadPolicy(ctx, root, NewWebCache(ctx, root).PolicyWebMode(), quiet)
}

// policyFromWeb là quyết định duy nhất về nguồn chính sách của một tính năng: chính sách từ web khi chế độ duyệt có
// hiệu lực (f.Mode: server ghim trước, chưa ghim theo approvals trong config.yaml — cùng quyết định với cổng và với
// eval --ci khi tính lại inputsHash) là web và máy đã đăng nhập; ngược lại file trong repo (hoặc mẫu).
func policyFromWeb(f *Feature) bool {
	return f.Mode == gate.ModeWeb && f.Web.PolicyWebMode()
}

// FeaturePolicy là chính sách rủi ro của tính năng f, nguồn theo policyFromWeb. Không gọi mạng.
func FeaturePolicy(ctx *Context, f *Feature, quiet bool) (LoadedPolicy, error) {
	return loadPolicy(ctx, f.Root, policyFromWeb(f), quiet)
}

// TryFeaturePolicy như FeaturePolicy nhưng mọi lỗi thành ok=false; không in gì.
func TryFeaturePolicy(ctx *Context, f *Feature) (l LoadedPolicy, ok bool) {
	defer func() {
		if recover() != nil {
			l, ok = LoadedPolicy{}, false
		}
	}()
	l, err := FeaturePolicy(ctx, f, true)
	return l, err == nil
}

// RepoFilePolicy là chính sách trong repo (file, thiếu thì mẫu generic bỏ flow), bỏ qua web: nguồn của chế độ local.
func RepoFilePolicy(ctx *Context, root string, quiet bool) (LoadedPolicy, error) {
	return loadPolicy(ctx, root, false, quiet)
}

// refreshWebPolicy: chế độ web có hiệu lực thì lấy lại chính sách từ web (admin có thể vừa xuất bản bản mới). Không
// lấy được mà máy có cache đã ký thì dùng cache và cảnh báo trên stderr; không có cache thì trả lỗi.
func refreshWebPolicy(ctx *Context, f *Feature) error {
	if !policyFromWeb(f) {
		return nil
	}
	var err error
	if f.Web.unreachable() {
		err = errors.New("Không lấy được chính sách từ web: web đang không truy cập được (thử lại sau).")
	} else {
		_, err = f.Web.PolicyPull()
	}
	if err == nil {
		return nil
	}
	c, ok := f.Web.Policy()
	if !ok {
		return err
	}
	ctx.Eprintln("Cảnh báo: " + strings.TrimSuffix(oneLineErr(err), ".") + "; dùng bản v" + strconv.Itoa(c.Version) +
		" đã lưu trên máy (có thể đã cũ).")
	return nil
}

func loadPolicy(ctx *Context, root string, fromWeb, quiet bool) (LoadedPolicy, error) {
	if fromWeb {
		web := NewWebCache(ctx, root)
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
