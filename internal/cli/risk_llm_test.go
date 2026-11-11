package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
)

// Giả subagent aisdlc-risk-classifier: test ghi risk-agent.json hợp lệ cho đúng đầu vào hiện tại.

// classifies: lệnh đọc risk-agent.json (testRepo tự ghi file trước khi chạy).
func classifies(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] + " " + args[1] {
	case "submit design", "submit cases", "risk reclassify":
		return true
	}
	return false
}

// llmPolicy là chính sách có hiệu lực như CLI đọc; false khi không đọc được hoặc không dùng được để xếp loại.
func (r *testRepo) llmPolicy() (risk.RiskPolicy, bool) {
	ctx := r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{})
	l, ok := TryLoadPolicy(ctx, r.root)
	if !ok {
		// chế độ web chưa có cache chính sách: lấy như lệnh submit sẽ lấy
		NewWebCache(ctx, r.root).PolicyPullQuietly()
		l, ok = TryLoadPolicy(ctx, r.root)
	}
	if !ok || l.Policy.Usable() != nil {
		return risk.RiskPolicy{}, false
	}
	return l.Policy, true
}

func (r *testRepo) llmInputs(key string) (model.FeaturePaths, []risk.InputFile, bool) {
	paths := model.FeaturePaths{RepoRoot: r.root, Key: model.FeatureKey(key)}
	meta, err := model.ReadFeatureMeta(paths.Meta())
	if err != nil {
		return paths, nil, false
	}
	flow := meta.Flow
	if flow == "" {
		flow = model.FlowFull
	}
	files, err := risk.InputFiles(r.root, paths, flow)
	if err != nil {
		r.t.Fatal(err)
	}
	return paths, files, true
}

// autoLLM ghi risk-agent.json mức r.llmLevel cho mọi tính năng mà file thiếu, đã cũ hoặc khác mức r.llmLevel (khi
// đặt).
func (r *testRepo) autoLLM() {
	r.t.Helper()
	p, ok := r.llmPolicy()
	if !ok {
		return
	}
	es, _ := os.ReadDir(filepath.Join(r.root, "aisdlc", "features"))
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		paths, files, ok := r.llmInputs(e.Name())
		if !ok {
			continue
		}
		hash := risk.InputsHash(p, files)
		if b, err := os.ReadFile(paths.RiskAgent()); err == nil {
			if cur, err := risk.ParseLLMResult(string(b)); err == nil && cur.InputsHash == hash &&
				(r.llmLevel == "" || cur.Level == r.llmLevel) {
				continue
			}
		}
		r.writeLLM(e.Name(), r.llmLevel)
	}
}

// llmResult là kết quả hợp lệ mức level ("" = mặc định) cho đầu vào hiện tại của key. Không truyền matched thì
// level cao hơn mặc định khớp luật đầu tiên đang bật có đúng mức đó (câu trích lấy từ file đầu vào đầu tiên); không có
// luật như vậy thì dùng override.
func (r *testRepo) llmResult(key, level string, matched ...risk.LLMMatch) risk.LLMResult {
	r.t.Helper()
	p, ok := r.llmPolicy()
	if !ok {
		r.t.Fatal("chính sách không dùng được để xếp loại")
	}
	_, files, ok := r.llmInputs(key)
	if !ok {
		r.t.Fatal("không đọc được feature.yaml của " + key)
	}
	if level == "" {
		level = p.DefaultLevel
	}
	if matched == nil && p.Rank(level) > p.Rank(p.DefaultLevel) && len(files) > 0 {
		for _, rule := range p.Rules {
			if rule.Enabled && rule.Level == level {
				ev := strings.Fields(files[0].Content)
				matched = []risk.LLMMatch{{Rule: rule.ID, Evidence: strings.Join(ev[:min(3, len(ev))], " "),
					File: files[0].Rel}}
				break
			}
		}
	}
	res := risk.LLMResult{InputsHash: risk.InputsHash(p, files), Level: level, Matched: matched,
		NotMatched: []risk.LLMNotMatch{}, Summary: "Giả lập subagent trong test."}
	hit := map[string]bool{}
	for _, m := range matched {
		hit[m.Rule] = true
	}
	for _, rule := range p.Rules {
		if rule.Enabled && !hit[rule.ID] {
			res.NotMatched = append(res.NotMatched, risk.LLMNotMatch{Rule: rule.ID, Reason: "không liên quan"})
		}
	}
	if res.Matched == nil {
		res.Matched = []risk.LLMMatch{}
	}
	if rl := risk.RuleLevel(p, res); rl != level {
		res.Override = &risk.LLMOverride{From: rl, Reason: "test đặt mức " + level}
	}
	return res
}

func (r *testRepo) writeLLMResult(key string, res risk.LLMResult) {
	r.t.Helper()
	b, err := json.MarshalIndent(res, "", "  ")
	must(r.t, err)
	r.write("aisdlc/features/"+key+"/risk-agent.json", string(b))
}

// writeLLM ghi risk-agent.json hợp lệ (xem llmResult).
func (r *testRepo) writeLLM(key, level string, matched ...risk.LLMMatch) {
	r.t.Helper()
	r.writeLLMResult(key, r.llmResult(key, level, matched...))
}

// llmRepo: banking, PAY-1 đã duyệt BRD, có migration nháp; risk-agent.json do test tự ghi.
func llmRepo(t *testing.T) *testRepo {
	r := bankingRepo(t)
	r.noAutoLLM = true
	r.brdApproved()
	return r
}

// outLines: các dòng stdout, bỏ dòng [KEY] đầu.
func (r *testRepo) outLines() []string {
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "[") {
		lines = lines[1:]
	}
	return lines
}

func TestRiskInputsPrintsHashAndWritesPolicy(t *testing.T) {
	r := llmRepo(t)
	r.ok(noConsole(), "risk", "inputs")
	lines := r.outLines()
	eqList(t, lines[:len(lines)-2], "aisdlc/features/PAY-1/brd.md", "aisdlc/features/PAY-1/db/00100_ledger.sql",
		"aisdlc/features/PAY-1/design.md", "aisdlc/features/PAY-1/design.yaml")
	eq(t, lines[len(lines)-2], "policy: .aisdlc/risk/policy-effective.yaml")
	p, _ := r.llmPolicy()
	_, files, _ := r.llmInputs("PAY-1")
	eq(t, lines[len(lines)-1], "inputsHash: "+risk.InputsHash(p, files))
	eff, err := risk.ParsePolicy(r.read(".aisdlc/risk/policy-effective.yaml"))
	must(t, err)
	eq(t, eff.Hash(), p.Hash())
}

func TestRiskInputsLiteUsesFeatureAndCases(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.ok(noConsole(), "risk", "inputs")
	lines := r.outLines()
	eqList(t, lines[:2], "aisdlc/features/PAY-1/cases.yaml", "aisdlc/features/PAY-1/feature.yaml")
}

func TestSubmitDesignBlocksWithoutRiskAgent(t *testing.T) {
	r := llmRepo(t)
	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "RISK_AGENT_MISSING", "Chưa có risk-agent.json: chạy `aisdlc risk inputs` rồi subagent "+
		"aisdlc-risk-classifier")
	eq(t, r.state(gate.DESIGN), "SUBMITTED_WITH_FINDINGS")
	eq(t, r.exists(riskFile), false)
}

func TestSubmitDesignUsesLLMLevel(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "high", risk.LLMMatch{Rule: "R4", Evidence: "CREATE TABLE ledger_entry",
		File: "aisdlc/features/PAY-1/db/00100_ledger.sql"})
	if code := r.run("submit", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	f := r.risk()
	eq(t, f.Level, "high")
	eqList(t, ruleIDs(f.Matched), "R4")
	eq(t, f.Matched[0].Level, "high")
	eq(t, f.Matched[0].On, "“CREATE TABLE ledger_entry” (aisdlc/features/PAY-1/db/00100_ledger.sql)")
	if f.LLM == nil || f.LLM.Level != "high" || f.LLM.InputsHash == "" || f.LLM.Summary == "" {
		t.Fatalf("llm = %+v", f.LLM)
	}
	if f.Agent != nil {
		t.Fatal("không ghi agent cho risk.yaml mới")
	}
	notContains(t, r.read(riskFile), "agent:")
	hasItem(t, reqIDs(f.Requirements), "idempotent")
	contains(t, r.out, "Rủi ro: high", "R4")
}

func TestSubmitDesignRaisesToDefault(t *testing.T) {
	r := llmRepo(t)
	r.write(policyFile, strings.Replace(r.read(policyFile), "default: low", "default: medium", 1))
	r.writeLLM("PAY-1", "low")
	if code := r.run("submit", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.risk().Level, "medium")
	eq(t, r.risk().LLM.Level, "low")
}

func TestSubmitDesignStaleHash(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "")
	r.write("aisdlc/features/PAY-1/design.md", r.read("aisdlc/features/PAY-1/design.md")+"\nThêm một dòng.\n")
	eq(t, r.run("submit", "design"), 1)
	contains(t, r.out, "RISK_AGENT_INVALID", "inputsHash không khớp")
	eq(t, r.exists(riskFile), false)
}

func TestSubmitDesignInvalidJSONOneFindingPerError(t *testing.T) {
	r := llmRepo(t)
	res := r.llmResult("PAY-1", "")
	res.Level = "urgent"
	res.Summary = ""
	r.writeLLMResult("PAY-1", res)
	eq(t, r.run("submit", "design"), 1)
	eq(t, strings.Count(r.out, "RISK_AGENT_INVALID"), 2)
	contains(t, r.out, "urgent", "summary rỗng")
}

func TestSubmitDesignKeepsOverride(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "high")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "risk", "set", "medium", "--reason", "Chỉ đọc sổ cái")
	// subagent chạy lại, ra mức khác: giữ mức đổi tay, chỉ cảnh báo
	r.writeLLM("PAY-1", "low")
	if code := r.run("submit", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	f := r.risk()
	eq(t, f.Level, "medium")
	if f.Override == nil || f.Override.By != "Dev Một" {
		t.Fatalf("override = %+v", f.Override)
	}
	contains(t, r.err, "giữ mức đổi tay medium", "aisdlc risk reclassify")
}

func TestSubmitDesignSchema1PolicyAborts(t *testing.T) {
	r := llmRepo(t)
	y, err := risk.PresetYAML("banking", "2")
	must(t, err)
	r.write(policyFile, y)
	eq(t, r.run("submit", "design"), 2)
	contains(t, r.err, "schema 1", "aisdlc policy migrate")
	eq(t, r.exists(riskFile), false)
}

func TestRiskInputsSchema1PolicyAborts(t *testing.T) {
	r := llmRepo(t)
	y, err := risk.PresetYAML("banking", "2")
	must(t, err)
	r.write(policyFile, y)
	eq(t, r.run("risk", "inputs"), 2)
	contains(t, r.err, "aisdlc policy migrate")
}

func TestSubmitBrdNoRiskPreview(t *testing.T) {
	r := bankingRepo(t)
	r.ok(noConsole(), "init", "PAY-1")
	r.write("aisdlc/features/PAY-1/brd.md", strings.Replace(validBRD, "Giới hạn chuyển tiền theo ngày.",
		"Miễn phí chuyển tiền theo ngày.", 1))
	r.ok(noConsole(), "submit", "brd")
	notContains(t, r.out+r.err, "Rủi ro tạm")
	eq(t, r.exists(riskFile), false)
	r.ok(noConsole(), "review", "brd", "--json")
	notContains(t, r.out, "riskPreview")
}

func TestBrdGateAlwaysManual(t *testing.T) {
	r := newTestRepo(t)
	r.presetFlow = true // flow low: design/cases auto
	r.setupInit()
	r.write(policyFile, strings.Replace(r.read(policyFile), "low:    {", "low:    { brd: auto,", 1))
	contains(t, r.read(policyFile), "brd: auto")
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	r.ok(noConsole(), "submit", "brd")
	eq(t, r.state(gate.BRD), "AWAITING_APPROVAL")
	p, _ := r.llmPolicy()
	reason, manual := risk.AutoGate(p, "low", gate.BRD, false)
	eq(t, manual, true)
	eq(t, reason, "Cổng BRD luôn duyệt tay")
}

func TestInitWritesRiskSchemaMarker(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	m, err := model.ReadFeatureMeta(filepath.Join(r.root, "aisdlc/features/PAY-1/feature.yaml"))
	must(t, err)
	eq(t, m.RiskSchema, 2)
}

// Tính năng cũ (không có dấu riskSchema) đã có risk.yaml: submit design/cases giữ nguyên, không cần risk-agent.json.
func TestLegacyFeatureKeepsRiskFile(t *testing.T) {
	r := llmRepo(t)
	meta := "aisdlc/features/PAY-1/feature.yaml"
	r.write(meta, strings.Replace(r.read(meta), "riskSchema: 2\n", "", 1))
	legacy := "level: medium\npolicy:\n  version: 1\n  hash: abc\nmatched:\n  - rule: R4\n    level: high\n" +
		"    on: bảng ledger_entry\nagent:\n  level: medium\n  labels: []\n  reason: cũ\noverride: null\n" +
		"requirements: []\n"
	r.write(riskFile, legacy)
	if code := r.run("submit", "design"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.read(riskFile), legacy)
	r.ok(typed("PAY-1"), "approve", "design")
	r.runner.cases = failingCases
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	eq(t, r.read(riskFile), legacy)
	notContains(t, r.out, "RISK_AGENT", "RISK_STALE")
}

func TestSubmitCasesStaleRiskBlocks(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	r.write(policyFile, strings.Replace(r.read(policyFile), "min: 90", "min: 95", 1)) // policy đổi: kết quả cũ
	r.writeLLM("PAY-1", "")
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "RISK_STALE", "aisdlc risk reclassify")
	r.ok(typed("PAY-1"), "risk", "reclassify")
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
}

func TestReclassifyInvalidAgentExit1(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "")
	r.ok(noConsole(), "submit", "design")
	must(t, os.Remove(filepath.Join(r.root, "aisdlc/features/PAY-1/risk-agent.json")))
	eq(t, r.runC(typed("PAY-1"), "risk", "reclassify"), 1)
	contains(t, r.out+r.err, "RISK_AGENT_MISSING")
}

func TestSubmitCasesLiteClassifiesFromLLM(t *testing.T) {
	r := bankingRepo(t)
	r.noAutoLLM = true
	r.ok(noConsole(), "init", "PAY-1", "--lite")
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "RISK_AGENT_MISSING")
	r.writeLLM("PAY-1", "high")
	eq(t, r.run("submit", "cases"), 1) // chỉ còn phát hiện RISK_REQ của mức high
	eq(t, r.risk().Level, "high")
	contains(t, r.out, "RISK_REQ")
}

// Dev đã đổi mức tay, rồi thiết kế đổi và subagent chạy lại: giữ mức đổi tay, risk.yaml gắn kết quả mới nên cổng Case
// không báo RISK_STALE.
func TestOverrideThenDesignChangeRefreshesLLM(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "high")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "risk", "set", "low", "--reason", "Chỉ đọc sổ cái")
	r.write("aisdlc/features/PAY-1/design.md", r.read("aisdlc/features/PAY-1/design.md")+"\nThêm một dòng.\n")
	r.writeLLM("PAY-1", "medium")
	r.ok(noConsole(), "submit", "design")
	contains(t, r.err, "giữ mức đổi tay low")
	f := r.risk()
	eq(t, f.Level, "low")
	if f.Override == nil || f.Override.From != "medium" || f.Override.By != "Dev Một" {
		t.Fatalf("override = %+v", f.Override)
	}
	eq(t, f.LLM.Level, "medium")
	r.ok(typed("PAY-1"), "approve", "design")
	r.runner.cases = failingCases
	if code := r.run("submit", "cases"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, r.out, r.err)
	}
	notContains(t, r.out, "RISK_STALE")
	eq(t, r.risk().Level, "low")
}

func TestMarkerFeatureRiskFileWithoutLLMIsStale(t *testing.T) {
	r := llmRepo(t)
	r.writeLLM("PAY-1", "")
	r.ok(noConsole(), "submit", "design")
	r.ok(typed("PAY-1"), "approve", "design")
	y := r.read(riskFile)
	r.write(riskFile, y[:strings.Index(y, "llm:")]+y[strings.Index(y, "\noverride:")+1:])
	r.runner.cases = failingCases
	eq(t, r.run("submit", "cases"), 1)
	contains(t, r.out, "RISK_STALE", "chưa có kết quả xếp loại LLM")
}
