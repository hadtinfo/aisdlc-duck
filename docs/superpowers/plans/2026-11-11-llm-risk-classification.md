# LLM Risk Classification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace pattern-matching risk rules (keyword/path/table/endpoint/label) with rules described in words that an
independent Claude Code subagent (`aisdlc-risk-classifier`) evaluates; the CLI validates its JSON and uses it as the
feature's risk level at `submit design` (lite: `submit cases`). The BRD gate is always manual.

**Architecture:** Policy gets `schema: 2` (rules = `id`, `level`, `describe`, `enabled`); schema 1 stays parseable
read-only for history/diff and is migrated by `aisdlc policy migrate`. `classifyRisk` in `internal/cli/featurerisk.go`
stops computing and instead reads, validates and converts `risk-agent.json` into `risk.yaml`; everything downstream
(override, reclassify, requirements, flow, web events) keeps consuming `risk.RiskFile`.

**Tech Stack:** Go 1.26, cobra, yaml.v3 via `internal/yamlx`, React/TS (web UI, vitest), bash E2E.

**Spec:** `docs/superpowers/specs/2026-11-11-llm-risk-classification-design.md` (Vietnamese). Read it before any task.

## Global Constraints

- All user-facing strings stay Vietnamese (bilingual work is a later project); code comments Vietnamese like the rest.
- Policy `schema: 1` YAML/JSON must still parse (read-only) and `Hash()`/`ToJSON()` of a schema-1 policy must be
  byte-identical to today (stored web versions and old `risk.yaml` hashes depend on it).
- Preset files `presets/{generic,banking}/{1,2}.yaml` unchanged byte-for-byte; new `3.yaml` is schema 2 and is what
  `PresetLatestVersion` returns.
- Using a schema-1 policy to classify, to `setup`, or to publish on web → `*PolicyError` / exit 2 with
  `Chính sách dùng schema 1 (luật so khớp); chạy \`aisdlc policy migrate\` để chuyển sang schema 2.`
  (web mode wording: `… quản trị cần chuyển chính sách sang schema 2 trên web.`).
- Feature marker: `feature.yaml` gets `riskSchema: 2` (`RiskSchema int \`yaml:"riskSchema,omitempty"\``) at `init`.
- Legacy features (no marker) that already have `risk.yaml`, or have no `risk.yaml` and no `impact` in `design.yaml`,
  behave as today for requirements/CI; nothing re-classifies them automatically.
- `risk-agent.json` lives at `aisdlc/features/<KEY>/risk-agent.json` (`FeaturePaths.RiskAgent()`); agent may write it.
- One PR for the whole plan (the spec's two batches are merged so E2E stays green at every commit).
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; never `--no-verify`.

## Review Focus

1. A schema-1 policy coming from the web cache (`.aisdlc/web/policy.json`) in web mode → clear exit 2, not a panic or
   silent low level (Task 1 test `TestUsableRejectsSchema1`, Task 4 test `TestSubmitDesignSchema1PolicyAborts`).
2. `risk-agent.json` wrapped in a ```json fence or with trailing prose (agents do this) → the fence is stripped like
   `review-agent.json`; prose → clear error (Task 3 `TestParseLLMResultStripsFence`).
3. Evidence with different whitespace/NFC form than the file → still found after normalisation; evidence absent → error
   naming the rule (Task 3 `TestValidateEvidenceNormalised`).
4. Dev override exists and then a new `risk-agent.json` arrives → override kept, warning printed, like today
   (Task 4 `TestSubmitDesignKeepsOverride`).
5. Old in-progress feature (risk.yaml without `llm`, no marker) through `submit cases` and `eval --ci` → still passes
   (Task 6 `TestEvalCILegacyRiskFile`).

---

### Task 1: Policy schema 2 in `internal/risk`

**Files:**
- Modify: `internal/risk/policy.go`, `internal/risk/policydiff.go`
- Test: `internal/risk/policy_test.go`, `internal/risk/policydiff_test.go`

**Interfaces:**
- Produces:
  - `RiskRule` gains `Describe string \`yaml:"describe,omitempty" json:"describe,omitempty"\``; `Kind`, `Match`, `Note`
    stay (schema 1 only) with `omitempty` on yaml/json where needed so schema-2 output has no empty `kind`/`match`.
  - `RiskPolicy.Warnings []string \`yaml:"-" json:"-"\`` (e.g. `flow.<level>.brd bị bỏ qua: cổng BRD luôn duyệt tay`).
  - `func (p RiskPolicy) Usable() error` — nil for schema 2, `*PolicyError` with the Global Constraints message for 1.
  - `ParsePolicy`/`PolicyFromJSON` accept schema 1 and 2.
  - `DiffPolicies` emits `PolicyChange{Kind: "SCHEMA_MIGRATED", …}` first when schemas differ; a changed `describe`
    is a rule change flagged as loosening (`Loosens: true` or whatever the existing struct uses for tightness).

- [ ] **Step 1: Write failing tests**
  - `TestParseSchema2`: YAML with `schema: 2`, rules `{id: R1, level: high, describe: "Chạm sổ cái"}` parses;
    `p.Rules[0].Describe == "Chạm sổ cái"`, `Enabled == true`.
  - `TestSchema2RejectsMatchKeys`: schema 2 rule with `kind`/`match` → PolicyError containing `R1: khóa lạ`(use the
    existing unknown-key message format).
  - `TestSchema2DescribeRequired`: empty/missing `describe` → error `R1: describe không được rỗng`.
  - `TestSchema2RejectsAgentClassification`: `classification.agent` in schema 2 → unknown-key error.
  - `TestSchema2FlowBrdWarning`: `flow.high.brd: manual` → parses, `p.Flow["high"]` has no `brd`,
    `p.Warnings` has one entry.
  - `TestUsableRejectsSchema1`: parse generic preset v2 → `Usable()` returns PolicyError with `policy migrate`.
  - `TestSchema1HashUnchanged`: hash of preset generic v2 equals a golden constant (record it from `main` before
    changing code: `go test -run XXX` helper or a one-off `fmt.Println(p.Hash())`).
  - `TestDiffSchemaMigrated`: diff generic v2 → a schema-2 policy has first change `SCHEMA_MIGRATED`.
- [ ] **Step 2:** `go test ./internal/risk -run 'Schema|Usable|DiffSchema'` → FAIL.
- [ ] **Step 3: Implement.** In `checkShape`/`bind`/`validate` branch on schema (read `schema` first): schema 2 uses
  rule keys `{id, level, describe, enabled}`, top keys without `classification.agent`; `validate` accepts schema 1
  or 2. `ToJSON` writes `describe` (not kind/match/note) for schema 2; schema-1 output unchanged.
- [ ] **Step 4:** `go test ./internal/risk` → PASS (existing schema-1 tests untouched).
- [ ] **Step 5: Commit** `feat(risk): policy schema 2 with described rules`.

### Task 2: `policy migrate` and preset v3

**Files:**
- Create: `internal/risk/migrate.go`, `internal/risk/presets/generic/3.yaml`, `internal/risk/presets/banking/3.yaml`
- Modify: `internal/cli/cmd_policy.go`, `internal/cli/cmd_setup.go` (uses latest preset — check it emits schema 2),
  `internal/risk/presets_test.go`, `internal/risk/novocab_test.go`
- Test: `internal/risk/migrate_test.go`, `internal/cli/policy_migrate_test.go`

**Interfaces:**
- Consumes: Task 1 schema-2 types.
- Produces: `func MigrateToV2(p RiskPolicy) (RiskPolicy, error)`; CLI `aisdlc policy migrate [--write]`.

- [ ] **Step 1: Failing tests**
  - `TestMigrateKeywordRule`: banking v2 R1 → `Describe == "Mọi thay đổi chạm sổ cái (từ khóa cũ: hạch toán|bút toán|định khoản)"`, level kept, `Schema == 2`, `Classification.Agent` dropped, `Preset` set to nil (no longer a pristine preset).
  - Per kind suffix: keyword `từ khóa cũ`, path `đường dẫn cũ`, table `bảng cũ`, endpoint `endpoint cũ`, label
    `nhãn cũ`; multiple `match` joined with `, `; empty note → describe is just the suffix text without parentheses.
  - `TestMigrateDropsFlowBrd`; `TestMigrateIdempotentOnSchema2` (returns input unchanged).
  - `TestPresetV3IsSchema2`: `LatestPreset("generic")` and `("banking")` have `Schema == 2`, `Usable() == nil`, same
    rule IDs and levels as v2.
  - CLI `TestPolicyMigrateDryRun`: repo with schema-1 `aisdlc/risk-policy.yaml` → output contains `SCHEMA_MIGRATED`
    and `--write`, file unchanged; `TestPolicyMigrateWrite` → file now `schema: 2`, re-parse OK; in web mode →
    exit 2 `Chế độ web: chuyển chính sách trên trang chính sách của web.`
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Preset v3 files written by hand from migrate output, then reword describes into natural
  sentences (generic G1–G4; banking G1–G4 + R1–R4, e.g. R4 `"Đọc hoặc ghi bảng sổ cái, phí, số dư (ledger_*, fee_*, account_balance)"`).
  `novocab_test`: keep its intent (core code has no business words) but exclude preset files as before.
- [ ] **Step 4:** `go test ./internal/risk ./internal/cli` → PASS.
- [ ] **Step 5: Commit** `feat(risk): policy migrate and schema-2 presets v3`.

### Task 3: `risk-agent.json` model, inputs hash, validation

**Files:**
- Create: `internal/risk/llm.go`, `internal/risk/llm_test.go`
- Modify: `internal/model/paths.go` (`RiskAgent()`), `internal/model/featuremeta.go` (`RiskSchema`)

**Interfaces:**
- Produces:
  ```go
  type LLMMatch struct{ Rule, Evidence, File string }          // json: rule, evidence, file
  type LLMNotMatch struct{ Rule, Reason string }               // json: rule, reason
  type LLMOverride struct{ From, Reason string }               // json: from, reason
  type LLMResult struct {
      InputsHash string; Level string; Matched []LLMMatch; NotMatched []LLMNotMatch
      Override *LLMOverride; Summary string
  } // json: inputsHash, level, matched, notMatched, override, summary
  type InputFile struct{ Rel, Content string }                // Rel relative to repo root, "/" separators
  func InputsHash(p RiskPolicy, files []InputFile) string     // sha256 hex over p.Hash() + sorted (Rel, NFC content with \r\n→\n)
  func ParseLLMResult(raw string) (LLMResult, error)          // strips one ``` / ```json fence; strict JSON (DisallowUnknownFields)
  func ValidateLLMResult(p RiskPolicy, r LLMResult, hash string, files []InputFile) []string  // empty = valid
  func RuleLevel(p RiskPolicy, r LLMResult) string            // max(default, levels of matched rules)
  ```
  `FeaturePaths.RiskAgent() string` → `in("risk-agent.json")`; `FeatureMeta.RiskSchema int`.
- [ ] **Step 1: Failing tests** (`llm_test.go`), each asserting the message contains the given fragment:
  - stale hash → `inputsHash không khớp`; level not in scale → `mức 'x' không có trong thang`;
    unknown rule → `luật 'Z9' không có trong chính sách`; enabled rule missing from both lists → `thiếu luật G2`;
    rule in both lists → `G1 xuất hiện hai lần`; disabled rule listed → allowed (ignored);
    evidence not in file → `G1: không thấy câu trích trong brd.md`; file not among inputs → `G1: file 'x.md' không thuộc đầu vào`;
    `level != RuleLevel` and `override == nil` → `mức high khác mức tính từ luật (low) nhưng thiếu override`;
    `override.from != RuleLevel` → `override.from`; empty summary → `summary rỗng`.
  - `TestParseLLMResultStripsFence`, `TestParseLLMResultRejectsProse`.
  - `TestValidateEvidenceNormalised`: evidence with collapsed whitespace and NFD form matches NFC file text.
  - `TestInputsHashOrderIndependent` and changes when policy hash or any content changes.
- [ ] **Step 2:** FAIL. **Step 3:** implement; evidence match = `strings.Contains(norm(file), norm(evidence))`,
  `norm` = NFC (`model.NFC`) + collapse all whitespace runs to one space. **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(risk): risk-agent.json result model and validation`.

### Task 4: CLI — `risk inputs`, classification from LLM, marker, BRD manual

**Files:**
- Modify: `internal/cli/featurerisk.go` (rewrite), `internal/cli/cmd_risk.go` (add `inputs`, adapt `reclassify`),
  `internal/cli/cmd_submit.go` (drop `brdRiskPreview`, `liteImpactLevel`; classify errors become findings),
  `internal/cli/cmd_init.go` (write `RiskSchema: 2`), `internal/cli/cmd_review.go`, `internal/cli/webpublish.go`,
  `internal/cli/autoflow.go`, `internal/cli/cmd_policy.go` (`show` prints describe; no `agent=`),
  `internal/cli/cmd_status.go` if it prints risk preview,
  `internal/risk/riskfile.go` (`LLM *LLMSnapshot \`yaml:"llm,omitempty" json:"llm,omitempty"\``,
  `type LLMSnapshot struct{ Level, InputsHash, Summary string; Override *LLMOverride }`; keep `Agent` field for reading
  legacy files, never written for new ones), `internal/risk/flow.go` (BRD always manual; delete `BrdLevel`;
  `LevelForGate(p, g, riskLevel)` drops `previewLevel`), `internal/review/card.go`+`cardtext.go` (delete
  `RiskPreview`), `internal/web/gates/auto.go` (BRD manual, stop reading `riskPreview`)
- Delete: `internal/risk/classifier.go` matching code (`Classify`, `firstMatch`, matchers), `internal/risk/inputs.go`
  `Collect*` and helpers, their tests; keep `Match` type (used by `RiskFile.Matched`), keep `globoverlap.go` if still referenced.
- Test: `internal/cli/risk_llm_test.go` (new), adapt existing tests in `internal/cli`, `internal/risk`, `internal/web/gates`.

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `func riskInputFiles(f *Feature) ([]risk.InputFile, error)` — FULL: `brd.md`, `design.md`, `design.yaml`,
    every file under the feature's draft `db/` dir, sorted; LITE: `feature.yaml` (title) and `cases.yaml`.
  - `aisdlc risk inputs` prints, in order: one line per input path (repo-relative), `aisdlc/.aisdlc-policy` line
    `policy: .aisdlc/risk/policy-effective.yaml`, then `inputsHash: <hex>`; writes the effective policy YAML to
    `.aisdlc/risk/policy-effective.yaml`. Policy not `Usable()` → exit 2.
  - `classifyRisk(p, f) (risk.RiskFile, []spec.Finding, error)`: reads `RiskAgent()`; missing →
    finding `RISK_AGENT_MISSING` (`Chưa có risk-agent.json: chạy \`aisdlc risk inputs\` rồi subagent aisdlc-risk-classifier`);
    parse/validate errors → one `RISK_AGENT_INVALID` finding per message. Valid → `RiskFile` with
    `Level = Stricter(default, llm.level)`, `Matched` = `{Rule, Level (rule's), On: "“evidence” (file)"}`, `LLM` snapshot.
  - `submit design` (FULL) and `submit cases` (LITE, and FULL when feature has marker) return those findings
    (blocking, like other findings). `submit cases` FULL: if `risk.yaml.LLM.InputsHash` ≠ current hash → finding
    `RISK_STALE` asking to re-run the classifier and `aisdlc risk reclassify`.
  - Legacy path: feature without marker → `submit design/cases` keep current behaviour for an existing `risk.yaml`
    (no reclassification); without marker and without `risk.yaml` and no `impact` → skipped as today.
  - `aisdlc risk reclassify`: same confirmation/reopen flow, but source is `classifyRisk` (errors → exit 1 with findings).
- [ ] **Step 1: Failing tests** (`risk_llm_test.go`, use existing CLI test helpers in `internal/cli`):
  - `TestRiskInputsPrintsHashAndWritesPolicy`.
  - `TestSubmitDesignBlocksWithoutRiskAgent` (finding `RISK_AGENT_MISSING`, gate not submitted).
  - `TestSubmitDesignUsesLLMLevel` (valid JSON high → `risk.yaml` level high, `llm.inputsHash` set).
  - `TestSubmitDesignRaisesToDefault` (LLM `low`, policy default `medium` → `medium`).
  - `TestSubmitDesignStaleHash` (edit `design.md` after writing JSON → `RISK_AGENT_INVALID` with `inputsHash không khớp`).
  - `TestSubmitDesignKeepsOverride`, `TestSubmitDesignSchema1PolicyAborts` (exit 2, `policy migrate`).
  - `TestSubmitBrdNoRiskPreview` (output has no `Rủi ro tạm`), `TestBrdGateAlwaysManual` (low level + flow auto →
    BRD still manual; reason `Cổng BRD luôn duyệt tay`).
  - `TestInitWritesRiskSchemaMarker`; `TestLegacyFeatureKeepsRiskFile`.
- [ ] **Step 2:** FAIL. **Step 3:** implement; then `go build ./... && go vet ./...` and fix every compile break from
  deleted APIs (web `internal/web/policy/service.go` `Classify` → Task 7 deletes it; in this task stub it to return
  an error `Thử phân loại không còn hỗ trợ` so the build stays green).
- [ ] **Step 4:** `AISDLC_SKIP_DOCKER=1 go test ./...` → PASS.
- [ ] **Step 5: Commit** `feat(risk): classify from risk-agent.json; BRD gate always manual`.

### Task 5: Drop `impact` from design.yaml / cases.yaml

**Files:**
- Modify: `internal/spec/designvalidator.go`, cases validator in `internal/spec`, `templates/design.yaml`,
  `templates/cases.full.yaml`, `templates/cases.lite.yaml`, `internal/eval/service.go` and
  `internal/review/cardbuild/builder.go` (`RequiredByDesign` → `risk.RequiredFor(meta model.FeatureMeta, designYaml string) bool`
  = marker set OR legacy impact present), `internal/risk/riskfile.go`
- Test: `internal/spec/*_test.go`, `templates/templates_test.go`

**Interfaces:** Produces `risk.RequiredFor`. `spec.Impact` stays parseable.

- [ ] **Step 1: Failing tests:** design.yaml with `impact` → finding code `IMPACT_DEPRECATED` severity warning (use the
  validator's existing non-blocking mechanism; if none exists, print a warning line instead and assert output);
  missing `impact` → no `IMPACT_MISSING` error anymore; templates contain no `impact:`.
- [ ] **Step 2–4:** FAIL → implement → `AISDLC_SKIP_DOCKER=1 go test ./...` PASS.
- [ ] **Step 5: Commit** `feat(spec): impact block is no longer used`.

### Task 6: `eval --ci` check

**Files:** Modify `internal/eval/service.go` (and/or `internal/citest`); Test `internal/eval/*_test.go`.

- [ ] **Step 1: Failing tests:** `TestEvalCIRiskInputsHashMatches` (PASS), `TestEvalCIRiskInputsHashStale` → hard
  check `RISK_LLM` false with `inputsHash không khớp tài liệu hiện tại`, `TestEvalCILegacyRiskFile` (no `llm`, no
  marker → no `RISK_LLM` failure), `TestEvalCIMarkerWithoutLLM` → fails `thiếu kết quả xếp loại LLM`.
- [ ] **Step 2–4:** implement using `riskInputFiles` logic moved to a shared place (`internal/risk` taking
  `model.FeaturePaths` + flow) so eval and CLI share it. PASS.
- [ ] **Step 5: Commit** `feat(eval): CI verifies LLM risk result against current documents`.

### Task 7: Web — policy page, migrate draft, no try-classify

**Files:**
- Modify: `internal/web/policy/service.go` (`Classify` + handler removed; `presetUpdate`/`Versions` handle schema 1
  read-only; `Publish`/`Validate` require `Usable()`; new `MigrateDraft(ctx, repo) (policyJSON string, changes, error)`),
  `internal/web/policy/handlers.go` (`POST /api/repos/{repo}/policy/migrate-draft`, auth as Validate),
  `web/ui/src/components/RiskRules.tsx` (columns id, level, describe, enabled), delete `RiskTry.tsx` and its use in
  `web/ui/src/pages/RiskPolicy.tsx`, add a "Chuyển sang schema 2" button visible when live policy `schema == 1`
  that loads the draft into the editor, `web/ui/src/risk.ts` types
- Test: `internal/web/policy/service_test.go`, `policy_it_test.go` (Docker), `web/ui/src/risk.test.ts`, `RiskPanel.test.tsx`

- [ ] **Step 1: Failing tests:** service `TestMigrateDraft` (schema-1 live → draft schema 2 JSON); `TestPublishRejectsSchema1`;
  `Versions` over a schema-1 then schema-2 history returns `SCHEMA_MIGRATED`; UI test renders `describe`, no try box.
- [ ] **Step 2–4:** implement; `go test ./internal/web/...` (Docker available locally: run full), `cd web/ui && npm test -- --run && npx tsc --noEmit` PASS.
- [ ] **Step 5: Commit** `feat(web): schema-2 policy page and migration draft`.

### Task 8: Plugin subagent, prompts, review card block

**Files:**
- Create: `plugin/agents/aisdlc-risk-classifier.md` (frontmatter `tools: Read, Grep, Glob`; prompt: read only the
  paths given, judge each enabled rule by meaning of `describe`, quote evidence verbatim, output only the JSON schema
  from Task 3, override needs reason, never guess file content).
- Modify: `plugin/commands/design.md` (remove impact instructions; new step before `submit design`: run
  `aisdlc risk inputs`, call subagent `aisdlc:aisdlc-risk-classifier` with exactly those paths + inputsHash, write its
  JSON verbatim (strip fence) to `aisdlc/features/<KEY>/risk-agent.json`; on RISK_AGENT_* findings fix docs and
  re-run subagent, never hand-edit JSON), `plugin/commands/cases.md` (same for lite; remove impact), `plugin/commands/brd.md`
  (nothing about risk preview), `plugin/test/plugin-files-test.sh` (new agent file listed),
  `internal/review/card.go` + `cardtext.go` + `internal/review/cardbuild/builder.go` (Design card, lite: Cases card,
  gets `RiskLLM *RiskLLMBlock` json `riskLLM,omitempty` from `risk.yaml`/`risk-agent.json`: level, matched with
  evidence, notMatched, override, summary; flag `RISK_LLM_OVERRIDE` level WARN when override present),
  `web/ui/src/components/` new `RiskLLM.tsx` rendered in `GatePanel`/`Review` page.
- Test: `internal/review/cardbuild` test for block + flag; `cardtext_test`; vitest for `RiskLLM.tsx`; `bash plugin/test/plugin-files-test.sh`.
- [ ] Steps 1–5 as usual. Commit `feat(plugin): aisdlc-risk-classifier subagent and LLM block on review card`.

### Task 9: E2E, docs, changelog

**Files:**
- Modify: `e2e/fixture-service/aisdlc/risk-policy.yaml` (schema 2; E1 `describe: "Chặn giao dịch theo hạn mức"`),
  `e2e/solutions/{PAY-1,FIX-2,FEE-3}/risk-agent.json` (new fixtures; PAY-1 matches E1 → high with evidence quoting
  "hạn mức" from its brd.md), `e2e/run-e2e.sh` and helpers (before `submit design`/lite `submit cases`: run
  `aisdlc risk inputs`, substitute `inputsHash` into the fixture with `jq`, write `risk-agent.json`; remove BRD
  auto-approve expectations; add a negative check: submit design without risk-agent.json → `RISK_AGENT_MISSING`),
  `docs/user-guide.md` §risk + `docs/huong-dan-su-dung.md` same, `docs/docs-check.sh` runnable blocks if they touch
  risk, `CHANGELOG.md` Unreleased **Breaking**: policy schema 2, `impact` removed, BRD always manual; **Added**:
  `aisdlc-risk-classifier`, `aisdlc risk inputs`, `aisdlc policy migrate`.
- [ ] Run `make e2e` (needs Docker; long) and `bash docs/docs-check.sh` → PASS. Commit `test(e2e): LLM risk classification flow; docs`.

### Task 10: Branch review and PR

- [ ] `make check` and `cd web/ui && npm test -- --run` PASS; whole-branch review; push `feat/llm-risk`; open PR
  "feat(risk): classify risk with an LLM subagent" against `main` with summary + Breaking notes.
