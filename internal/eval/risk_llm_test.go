package eval

import (
	"testing"

	"aisdlc/internal/model"
	"aisdlc/internal/risk"
)

// llmEnv: tính năng LITE có dấu riskSchema 2, risk.yaml có (hoặc không có) khối llm với inputsHash cho trước.
func llmEnv(t *testing.T, marker bool, llm func(hash string) *risk.LLMSnapshot) (*evalEnv, *Service) {
	t.Helper()
	e := newEvalEnv(t)
	if marker {
		meta, err := model.ReadFeatureMeta(e.paths.Meta())
		e.must(err)
		meta.RiskSchema = 2
		e.must(meta.Write(e.paths.Meta()))
	}
	p, err := risk.LatestPreset("generic")
	e.must(err)
	files, err := risk.InputFiles(e.dir, e.paths, model.FlowLite)
	e.must(err)
	rf := risk.RiskFile{Level: "low", Policy: risk.PolicyRef{Version: p.Version, Hash: p.Hash()},
		LLM: llm(risk.InputsHash(p, files))}
	e.must(rf.Write(e.paths.Risk()))
	e.approve("CASES")
	svc := e.service()
	svc.Policy = &p
	return e, svc
}

func runLLM(t *testing.T, svc *Service) (HardCheck, bool) {
	t.Helper()
	r, err := svc.Run(t.Context(), true, "HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range r.Hard {
		if h.ID == "RISK_LLM" {
			return h, true
		}
	}
	return HardCheck{}, false
}

func TestEvalCIRiskInputsHashMatches(t *testing.T) {
	_, svc := llmEnv(t, true, func(h string) *risk.LLMSnapshot { return &risk.LLMSnapshot{Level: "low", InputsHash: h} })
	if h, ok := runLLM(t, svc); !ok || !h.Passed {
		t.Fatalf("RISK_LLM phải đạt: %+v ok=%v", h, ok)
	}
}

func TestEvalCIRiskInputsHashStale(t *testing.T) {
	_, svc := llmEnv(t, true, func(string) *risk.LLMSnapshot { return &risk.LLMSnapshot{Level: "low", InputsHash: "cu"} })
	h, ok := runLLM(t, svc)
	if !ok || h.Passed || h.Detail != "inputsHash không khớp tài liệu hiện tại" {
		t.Fatalf("RISK_LLM phải hỏng: %+v ok=%v", h, ok)
	}
}

func TestEvalCILegacyRiskFile(t *testing.T) {
	_, svc := llmEnv(t, false, func(string) *risk.LLMSnapshot { return nil })
	if h, ok := runLLM(t, svc); ok && !h.Passed {
		t.Fatalf("risk.yaml cũ không được lỗi RISK_LLM: %+v", h)
	}
}

func TestEvalCIMarkerWithoutLLM(t *testing.T) {
	_, svc := llmEnv(t, true, func(string) *risk.LLMSnapshot { return nil })
	h, ok := runLLM(t, svc)
	if !ok || h.Passed || h.Detail != "thiếu kết quả xếp loại LLM" {
		t.Fatalf("RISK_LLM phải hỏng: %+v ok=%v", h, ok)
	}
}
