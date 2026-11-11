package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"aisdlc/internal/fsx"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
)

// FeatureRisk: xếp loại rủi ro một tính năng từ kết quả subagent aisdlc-risk-classifier (risk-agent.json). Event rủi
// ro gửi web (RISK_CLASSIFIED, RISK_OVERRIDDEN) ở webpublish.go.

// EffectivePolicyFile là chính sách có hiệu lực `aisdlc risk inputs` ghi ra cho subagent đọc (chế độ web chính sách
// nằm trong cache đã ký, không ở aisdlc/risk-policy.yaml).
const EffectivePolicyFile = ".aisdlc/risk/policy-effective.yaml"

// riskSchemaMarker là dấu `riskSchema` mà `init` ghi vào feature.yaml: tính năng có dấu này xếp loại bằng subagent.
const riskSchemaMarker = 2

// llmRisk: tính năng đi luồng xếp loại bằng subagent (có dấu riskSchema). Tính năng cũ giữ risk.yaml đang có.
func llmRisk(f *Feature) bool { return f.Meta.RiskSchema >= riskSchemaMarker }

// requiredByDesign: tính năng cũ luồng FULL chưa có risk.yaml chỉ phải xếp loại khi design.yaml có impact.
func requiredByDesign(f *Feature) bool {
	req, _ := risk.RequiredByDesign(f.Paths.DesignYaml())
	return req
}

// usablePolicy: chính sách dùng được để xếp loại (schema 2); không thì exit 2. Chế độ web dev không tự sửa được
// chính sách nên thông báo chỉ sang quản trị.
func usablePolicy(l LoadedPolicy) error {
	err := l.Policy.Usable()
	if err == nil {
		return nil
	}
	var pe *risk.PolicyError
	msg := err.Error()
	if errors.As(err, &pe) && len(pe.Errors) > 0 {
		msg = pe.Errors[0]
	}
	if l.Origin == "web" {
		msg = "Chính sách dùng schema 1 (luật so khớp); quản trị cần chuyển chính sách sang schema 2 trên web."
	}
	return abort(2, msg)
}

// riskInputs là các file đầu vào của subagent và inputsHash của chúng với chính sách p.
func riskInputs(p risk.RiskPolicy, f *Feature) ([]risk.InputFile, string, error) {
	files, err := risk.InputFiles(f.Root, f.Paths, f.Flow())
	if err != nil {
		return nil, "", err
	}
	return files, risk.InputsHash(p, files), nil
}

// classifyRisk đọc và kiểm risk-agent.json rồi dựng risk.yaml mới (chưa override). Thiếu file → RISK_AGENT_MISSING;
// JSON sai hoặc không khớp đầu vào/chính sách → mỗi lỗi một RISK_AGENT_INVALID (chặn như mọi phát hiện khác).
func classifyRisk(p risk.RiskPolicy, f *Feature) (risk.RiskFile, []spec.Finding, error) {
	b, err := os.ReadFile(f.Paths.RiskAgent())
	if errors.Is(err, os.ErrNotExist) {
		return risk.RiskFile{}, []spec.Finding{{Code: "RISK_AGENT_MISSING", Message: "Chưa có risk-agent.json: " +
			"chạy `aisdlc risk inputs` rồi subagent aisdlc-risk-classifier (ghi kết quả vào " +
			f.Paths.Rel(f.Paths.RiskAgent()) + ")."}}, nil
	}
	if err != nil {
		return risk.RiskFile{}, nil, err
	}
	invalid := func(msgs ...string) []spec.Finding {
		out := make([]spec.Finding, len(msgs))
		for i, m := range msgs {
			out[i] = spec.Finding{Code: "RISK_AGENT_INVALID", Message: m + ". Sửa tài liệu nếu cần rồi chạy lại " +
				"`aisdlc risk inputs` và subagent aisdlc-risk-classifier; không sửa tay risk-agent.json."}
		}
		return out
	}
	res, err := risk.ParseLLMResult(string(b))
	if err != nil {
		return risk.RiskFile{}, invalid(err.Error()), nil
	}
	files, hash, err := riskInputs(p, f)
	if err != nil {
		return risk.RiskFile{}, nil, err
	}
	if errs := risk.ValidateLLMResult(p, res, hash, files); len(errs) > 0 {
		return risk.RiskFile{}, invalid(errs...), nil
	}
	return risk.FromLLM(p, res), nil, nil
}

// writeEffectivePolicy ghi chính sách có hiệu lực ra EffectivePolicyFile cho subagent đọc.
func writeEffectivePolicy(root string, p risk.RiskPolicy) error {
	y, err := p.ToYAML()
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(root, filepath.FromSlash(EffectivePolicyFile)), []byte(y), 0o644)
}

// matchedText là "R4 (“…” (file)), G1 (…)" để in.
func matchedText(ms []risk.Match) string {
	if len(ms) == 0 {
		return "không rule nào khớp"
	}
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = m.Rule + " (" + m.On + ")"
	}
	return strings.Join(parts, ", ")
}
