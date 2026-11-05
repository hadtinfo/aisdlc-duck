# Chính sách rủi ro theo repo — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mỗi repo có chính sách rủi ro (rule nhận diện + yêu cầu case theo mức) do dự án tự khai. Mỗi tính năng được phân loại mức rủi ro; cổng Case, harness và eval chặn khi thiếu yêu cầu của mức đó. Chính sách quản lý được trên web và import từ mẫu lúc `aisdlc setup`.

**Architecture:**
- Logic thuần nằm trong package mới `com.aisdlc.core.risk`: chính sách, mẫu, phân loại, bản chụp `risk.yaml`, kiểm yêu cầu, diff chính sách. Phần này không phụ thuộc CLI hay web.
- CLI đọc chính sách từ file repo hoặc từ cache ký của web, rồi ghi `risk.yaml` lúc submit.
- Harness và eval chỉ đọc `risk.yaml`.
- Web lưu các phiên bản chính sách trong Postgres, ký bản mới nhất cho CLI, và dùng chính code core để validate, phân loại và tính diff.

**Tech Stack:** Java 17, Maven multi-module, Jackson YAML, picocli, JUnit 5 + AssertJ, Spring Boot 3.3.5 + JdbcTemplate + Flyway + Postgres 16 (Testcontainers), React 18 + TS + Vite + Vitest, Playwright 1.49.1 (E2E).

**Spec:** `docs/superpowers/specs/2026-11-04-aisdlc-risk-policy-design.md`

## Global Constraints

- **Không có chuỗi nghiệp vụ trong code lõi.** `aisdlc-core/src/main/java`, `aisdlc-cli/src/main/java` và `aisdlc-junit/src/main/java` không chứa "hạch toán", "bút toán", "ledger" hay "fee" (không phân biệt hoa thường). Các từ này chỉ được nằm trong resource mẫu `risk-presets/**`.
- **Hook không gọi mạng.** Hook chỉ đọc file local.
- **Tương thích ngược:**
  - `cases.yaml` và `design.yaml` cũ vẫn hợp lệ.
  - Tính năng không có `risk.yaml` giữ nguyên hành vi hiện tại, kể cả coverage chỉ là cảnh báo mềm `coverageWarn`.
- **Lệnh cần console thật.** `aisdlc risk set` và `aisdlc risk reclassify` dùng cơ chế console thật và gõ lại feature key, giống `approve`.
- **Quy tắc test:**
  - Test không bao giờ ghi vào `~/.config/aisdlc` thật.
  - Không log token.
- **Commit:**
  - Cuối mỗi commit message có dòng `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
  - Thông điệp UI/CLI viết tiếng Việt, theo giọng các thông điệp sẵn có.
- **Đường dẫn JDK/Maven trên máy này:** `~/.local/opt/jdk17` và `~/.local/opt/maven/bin`. Chạy `export JAVA_HOME=~/.local/opt/jdk17 PATH=~/.local/opt/jdk17/bin:~/.local/opt/maven/bin:$PATH` trước lệnh `mvn`.
- **Tên do spec định, giữ nguyên:**
  - trường case và design: `when.repeat`, `when.parallel`, `then.statusCounts`, `design.yaml: invariants`, `impact`;
  - thẻ BRD: `[biên]`;
  - file: `aisdlc/risk-policy.yaml`, `aisdlc/features/<KEY>/risk.yaml`, `.aisdlc/web/policy.json`;
  - sự kiện: `RISK_CLASSIFIED`, `RISK_OVERRIDDEN`;
  - id yêu cầu: `rule-pos-neg`, `boundary`, `idempotent`, `concurrent`, `invariant`, `coverage`, `sql-zero`;
  - loại rule: `keyword`, `path`, `table`, `endpoint`, `label`;
  - `classification.agent` nhận `raise | off`; `classification.devLower` nhận `reason | never`.
- **Mẫu.** Có 2 mẫu là `generic` và `banking`. `aisdlc setup` mặc định dùng `generic`.

## Review Focus

1. **Regex hỏng hoặc glob rỗng trong chính sách** (file sửa tay ở chế độ không web). Lệnh submit phải dừng với thông báo chỉ rõ rule nào hỏng, không được ném stacktrace. Test thêm ở Task 1 (`parse_reportsEveryError`) và Task 7 (`submitDesign_brokenPolicy_exit2WithRuleId`).
2. **Bật web nhưng `.aisdlc/web/policy.json` thiếu hoặc sai chữ ký.** `submit design` phải dừng và nhắc `aisdlc policy pull`, không được rơi về file repo. Test ở Task 7 (`load_webMode_missingCache_aborts`, `load_webMode_badSignature_aborts`).
3. **Case `parallel` gặp request chậm hoặc treo.** Case phải FAIL đúng giờ với thông điệp, không làm treo cả suite. Test ở Task 6 (`parallel_timeout_failsCaseNotSuite`).
4. **`sql-zero` hoặc bất biến cố ghi dữ liệu** (UPDATE lọt qua `WITH … UPDATE`). Kết nối read-only phải làm case đó FAIL và dữ liệu không đổi. Test ở Task 6 (`invariant_writeAttempt_failsAndRollsBack`).
5. **Hai tab web cùng xuất bản từ cùng một phiên bản gốc.** Lần xuất bản thứ hai phải nhận 409 với thông điệp "đã có phiên bản mới hơn", không được ghi đè. Test ở Task 8 (`publish_staleBase_409`).

---

### Task 1: Core — schema chính sách, thư viện yêu cầu, mẫu

**Files:**
- Create in `aisdlc-core/src/main/java/com/aisdlc/core/risk/`: `RiskPolicy.java`, `RiskRule.java`, `MatchKind.java`, `ClassificationRules.java`, `Requirement.java`, `RequirementCatalog.java`, `RiskPolicyException.java`, `Presets.java`
- Create resources:
  - `aisdlc-core/src/main/resources/risk-presets/generic/1.yaml`
  - `aisdlc-core/src/main/resources/risk-presets/banking/1.yaml`
- Test in `aisdlc-core/src/test/java/com/aisdlc/core/risk/`: `RiskPolicyTest.java`, `PresetsTest.java`, `NoDomainStringsTest.java`

**Interfaces — Produces:**
- `enum MatchKind { KEYWORD, PATH, TABLE, ENDPOINT, LABEL }`: YAML dùng chữ thường, theo cách của `CaseKind`.
- `record RiskRule(String id, MatchKind kind, List<String> match, String level, String note, boolean enabled)`: `match` nhận cả chuỗi lẫn danh sách; `enabled` mặc định `true`.
- `record ClassificationRules(AgentMode agent, DevLower devLower)` với `enum AgentMode { RAISE, OFF }` và `enum DevLower { REASON, NEVER }`. Mặc định là `RAISE` và `REASON`.
- `record Requirement(String id, Map<String,Object> params)`:
  - `int intParam(String name, int def)`, `String strParam(String name)`;
  - Jackson phẳng hóa: YAML `{ id: coverage, min: 90 }` ↔ `id` + `params={min:90}`.
- `final class RequirementCatalog`:
  - các hằng `RULE_POS_NEG="rule-pos-neg"`, `BOUNDARY="boundary"`, `IDEMPOTENT="idempotent"`, `CONCURRENT="concurrent"`, `INVARIANT="invariant"`, `COVERAGE="coverage"`, `SQL_ZERO="sql-zero"`;
  - `static boolean known(String id)`;
  - `static List<String> requiredParams(String id)`: `concurrent→[threads]`, `coverage→[min]`, `sql-zero→[name, sql]`, còn lại `[]`;
  - `static String displayName(Requirement r)`: tên tiếng Việt như trong prototype; với `sql-zero` thì dùng `name`.
- `record RiskPolicy(int schema, int version, PresetRef preset, List<String> levels, String defaultLevel, List<RiskRule> rules, ClassificationRules classification, Map<String, List<Requirement>> requirements)`:
  - `record PresetRef(String name, String version)`;
  - YAML dùng key `default` cho `defaultLevel`;
  - `static RiskPolicy parse(String yaml) throws RiskPolicyException`;
  - `static RiskPolicy fromJson(String json)`, `String toYaml()`, `String toJson()`;
  - `String hash()`: `"sha256:" + hex(sha256(toJson() chuẩn hóa))`;
  - `int rank(String level)`: trả -1 khi không có trong thang;
  - `List<Requirement> requirementsFor(String level)`: rỗng khi mức không có trong map.
- `class RiskPolicyException extends RuntimeException` có `List<String> errors()`.
- `final class Presets`:
  - `static List<String> names()` trả `["banking","generic"]`;
  - `static String latestVersion(String name)`;
  - `static String yaml(String name, String version)`;
  - `static RiskPolicy load(String name, String version)`;
  - `static RiskPolicy latest(String name)`.
  - Resource đặt theo `risk-presets/<name>/<version>.yaml`. Mỗi lần đổi mẫu thì thêm file phiên bản mới, không xóa file cũ.

- [ ] **Step 1: Viết test `RiskPolicyTest`**
  - `parse_fullExample`: YAML ví dụ ở spec §3.1 parse ra:
    - 3 mức, `defaultLevel="low"`;
    - `rules.get(3).match()` = `["ledger_*","fee_*","account_balance"]`;
    - `requirementsFor("high")` có `concurrent` với `intParam("threads",0)==8`;
    - `classification.agent()==RAISE`.
  - `parse_matchStringBecomesList`: `match: "a|b"` thành `List.of("a|b")`.
  - `parse_reportsEveryError`: một YAML có đồng thời các lỗi sau, và `errors()` chứa đủ 6 thông điệp, mỗi thông điệp nêu rõ id liên quan:
    1. regex `"(("`, thông điệp có `R1`;
    2. `match: []`;
    3. `level: urgent` không có trong thang;
    4. hai rule cùng id;
    5. yêu cầu `foo` không có trong thư viện;
    6. `coverage` thiếu `min`.
  - `parse_sqlZeroMustBeSelect`: `sql: "DELETE FROM x"` → lỗi chứa `"SELECT"`; `sql: "with t as (select 1) select * from t"` hợp lệ.
  - `parse_levelsBounds`: 1 mức → lỗi; 6 mức → lỗi; 2 mức hợp lệ.
  - `roundTrip_yamlJsonHashStable`: `parse(p.toYaml()).hash().equals(p.hash())` và `fromJson(p.toJson()).equals(p)`.
- [ ] **Step 2: Viết `PresetsTest`**
  - `bothPresetsParse`: cả hai mẫu parse không lỗi.
  - `genericHasNoBusinessRules`: mọi rule của `generic` đều có `kind == LABEL`, nhãn thuộc `{money-movement, schema-change, auth, external-api}`.
  - `bankingExtendsGeneric`: `banking` chứa mọi rule của `generic` và có ít nhất một rule `KEYWORD`.
  - `presetRefMatchesFile`: `latest("banking").preset()` = `("banking","1")`.
- [ ] **Step 3: Viết `NoDomainStringsTest`**: quét `../aisdlc-core/src/main/java`, `../aisdlc-cli/src/main/java` và `../aisdlc-junit/src/main/java` (tương đối với `user.dir` của module), khẳng định không file `.java` nào khớp `(?i)hạch toán|bút toán|ledger|\bfee\b`.
- [ ] **Step 4: Chạy để thấy fail.** `mvn -q -pl aisdlc-core test -Dtest='RiskPolicyTest,PresetsTest,NoDomainStringsTest'` → FAIL biên dịch.
- [ ] **Step 5: Cài đặt các lớp trên và viết 2 file mẫu.**
  - `generic/1.yaml`:
    - các mức `low, medium, high`;
    - rule `label`: `money-movement → high`, `schema-change → medium`, `auth → high`, `external-api → medium`;
    - yêu cầu theo mức như spec §3.1, nhưng không có `sql-zero`.
  - `banking/1.yaml`: toàn bộ `generic`, cộng thêm:
    - `keyword "hạch toán|bút toán|định khoản" → high`;
    - `keyword "phí|biểu phí|fee" → high`;
    - `path "**/accounting/**" → high`;
    - `table ["ledger_*","fee_*","account_balance"] → high`.
  - Validate trong `parse`: gom mọi lỗi rồi mới ném, không dừng ở lỗi đầu.
- [ ] **Step 6: Chạy lại.** Lệnh như Step 4 → PASS.
- [ ] **Step 7: Commit.** `feat(core): chính sách rủi ro — schema, thư viện yêu cầu, mẫu generic/banking`

### Task 2: Core — mở rộng định dạng case/thiết kế, đánh giá tác động

**Files:**
- Modify in `aisdlc-core/src/main/java/com/aisdlc/core/spec/`: `When.java`, `Then.java`, `DesignSpec.java`, `CasesSpec.java`, `DesignValidator.java`, `CasesValidator.java` (chỉ phần validate cú pháp `repeat`/`parallel`)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/spec/Impact.java`, `.../spec/Invariant.java`
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/review/DocFacts.java` (thẻ `[biên]`)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/spec/SpecExtensionsTest.java`, `.../review/DocFactsTest.java` (thêm case)

**Interfaces — Produces:**
- `When`: thêm `int repeat` và `int parallel`, mặc định 1 khi thiếu hoặc ≤ 0. Giữ constructor 4 tham số cũ để code sẵn có biên dịch được.
- `Then`: thêm `Map<Integer,Integer> statusCounts`, mặc định rỗng. Khi `when.parallel > 1`, `status` có thể bỏ trống (`0`).
- `record Impact(String level, List<String> labels, String reason)`.
- `record Invariant(String name, String sql)`.
- `DesignSpec(List<StepSpec> steps, List<Invariant> invariants, Impact impact)`.
- `CasesSpec`: thêm `Impact impact` ở cuối (luồng lite).
- `DocFacts.Rule(String id, String text, boolean boundary)`: `boundary=true` khi dòng rule kết thúc bằng `[biên]`; `text` bỏ thẻ này đi.
- `DesignValidator.validate(String designMd, DesignSpec spec, List<String> levels)`: overload mới. Khi `levels != null`:
  - finding `IMPACT_MISSING` khi `spec.impact()==null`;
  - finding `IMPACT_LEVEL` khi `impact.level` không có trong `levels`.

  Overload cũ gọi overload mới với `levels=null`.
- `CasesValidator`:
  - finding `CASE_PARALLEL_STATUS` khi `parallel>1` mà cả `status` lẫn `statusCounts` đều trống;
  - finding `CASE_STATUS_COUNTS` khi tổng `statusCounts` khác `parallel`.

- [ ] **Step 1: Viết test**
  - `oldYamlStillParses`: `cases.full.yaml` và `design.yaml` trong `aisdlc-cli/src/main/resources/templates` parse được; mọi `when.repeat()==1`.
  - `repeatParallelParse`: YAML có `when: {..., repeat: 2}` và `when: {..., parallel: 8}`, `then: {statusCounts: {200: 1, 409: 7}}` → giá trị đúng.
  - `designInvariantsImpact`: parse ra `invariants().get(0).name()` và `impact().labels()==["money-movement"]`.
  - `designValidator_impactRequiredWhenLevelsGiven`: thiếu impact → có `IMPACT_MISSING`; `level: urgent` → có `IMPACT_LEVEL`; `levels=null` → không có hai finding này.
  - `casesValidator_parallelNeedsStatus` và `statusCountsMustSum`.
  - `DocFactsTest.boundaryTag`: `- BR-3: Hạn mức tối đa 500 triệu/ngày [biên]` → `boundary=true`, `text="Hạn mức tối đa 500 triệu/ngày"`.
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-core test -Dtest='SpecExtensionsTest,DocFactsTest'` → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy toàn bộ test core.** `mvn -q -pl aisdlc-core test` → PASS, các test cũ không vỡ.
- [ ] **Step 5: Commit.** `feat(core): when.repeat/parallel, then.statusCounts, invariants, impact, thẻ [biên]`

### Task 3: Core — phân loại và bản chụp `risk.yaml`

**Files:**
- Create in `aisdlc-core/src/main/java/com/aisdlc/core/risk/`: `GlobOverlap.java`, `RiskInputs.java`, `RiskInputsCollector.java`, `Classifier.java`, `RiskResult.java`, `RiskFile.java`
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/model/FeaturePaths.java` (thêm `risk()`)
- Test in `aisdlc-core/src/test/java/com/aisdlc/core/risk/`: `GlobOverlapTest.java`, `ClassifierTest.java`, `RiskInputsCollectorTest.java`, `RiskFileTest.java`

**Interfaces:**
- Consumes: `RiskPolicy`, `RiskRule`, `MatchKind` (Task 1); `Impact`, `DesignSpec`, `CasesSpec`, `DocFacts` (Task 2); `SqlFacts.parse(String)`.
- Produces:
  - `FeaturePaths.risk()` → `dir().resolve("risk.yaml")`.
  - `GlobOverlap.overlaps(String ruleGlob, String scopeGlob) -> boolean`: hai glob có thể cùng khớp một đường dẫn.
  - `record RiskInputs(String text, List<String> paths, List<String> tables, List<String> endpoints, List<String> labels, String agentLevel)`. `tables` có dạng `bảng` hoặc `bảng.cột`, chữ thường. `endpoints` có dạng `METHOD /path`.
  - `RiskInputsCollector`:
    - `static RiskInputs full(FeaturePaths p, String title)`:
      - `text` = title + `brd.md` + `design.md`;
      - `paths` = scope của các bước trong `design.yaml`;
      - `tables` lấy từ `SqlFacts.parse` trên mọi file trong `p.dbDrafts()`;
      - `endpoints` lấy từ `DocFacts.design(md).apiLines()` (đã có dạng `METHOD /path`);
      - `labels` và `agentLevel` lấy từ `design.yaml impact`.
    - `static RiskInputs lite(FeaturePaths p, String title)`:
      - `text` = title + title các case;
      - `paths` = `cases.yaml scope`;
      - `tables` và `endpoints` rỗng;
      - `labels` và `agentLevel` lấy từ `cases.yaml impact`.
    - `static RiskInputs brdOnly(String title, String brd)`.
  - `record RiskResult(String level, String ruleLevel, List<Match> matched, String agentLevel)` với `record Match(String rule, String level, String on)`.
  - `Classifier.classify(RiskPolicy p, RiskInputs in) -> RiskResult`.
  - `record RiskFile(String level, PolicyRef policy, List<RiskResult.Match> matched, Impact agent, Override override, List<Requirement> requirements)`:
    - `record PolicyRef(int version, String hash)`;
    - `record Override(String from, String to, String by, String reason, Instant at)`;
    - `static RiskFile of(RiskPolicy p, RiskResult r, Impact agent)`: `requirements = p.requirementsFor(r.level())`;
    - `RiskFile withOverride(RiskPolicy p, String to, String by, String reason, Instant at)`: tính lại `requirements` theo mức `to`;
    - `static Optional<RiskFile> read(Path)`, `void write(Path)` (YAML).

**Quy tắc phân loại** (spec §3.5), đây là thuật toán `classify`:
- `ruleLevel` = max theo `rank` của `defaultLevel` và mức các rule có `enabled` mà khớp.
- `level` = max(`ruleLevel`, `agentLevel`) khi `classification.agent==RAISE` và `agentLevel` có trong thang. Trường hợp còn lại, `level` = `ruleLevel`.
- Cách khớp theo loại rule:
  - `KEYWORD`: `Pattern.compile(m, CASE_INSENSITIVE|UNICODE_CASE).matcher(text).find()`. `on` = `“<đoạn khớp>”`.
  - `PATH`: có `scope` nào mà `GlobOverlap.overlaps(m, scope)`. `on` = scope đó.
  - `TABLE`: glob tên (`*` khớp mọi ký tự trừ `.`), so với từng bảng và từng `bảng.cột`. `on` = `"bảng " + tên`.
  - `ENDPOINT`: tách method; path so theo glob, với `{x}` coi như một đoạn. `on` = endpoint.
  - `LABEL`: so bằng tuyệt đối. `on` = `"agent gắn nhãn " + nhãn`.
- Mỗi rule chỉ ghi Match đầu tiên.

- [ ] **Step 1: Viết test**
  - `GlobOverlapTest`:
    - `("**/accounting/**", "src/main/java/**/accounting/*.java")` → true;
    - `("**/accounting/**", "src/main/java/**/fee/**")` → false;
    - `("**/report/**", "src/**")` → true;
    - `("src/*.java", "src/a/b.java")` → false.
  - `ClassifierTest`, dùng `Presets.latest("banking")`:
    - `keywordMatchesUnicodeCaseInsensitive`: text "Ghi nhận BÚT TOÁN" → level `high`, matched có rule của nhóm hạch toán.
    - `tableGlob`: tables `["ledger_entry"]` → high, `on="bảng ledger_entry"`.
    - `endpointGlobWithPathVariable`: rule `POST /payments/**`, endpoint `POST /payments/{id}/confirm` → khớp; `GET /payments/1` → không khớp.
    - `disabledRuleIgnored`.
    - `agentRaises`: không rule nào khớp, `agentLevel="high"` → `level="high"`, `ruleLevel="low"`.
    - `agentCannotLower`: rule high, `agentLevel="low"` → high.
    - `agentOff`: policy có `agent: off`, `agentLevel="high"` → `low`.
    - `unknownAgentLevelIgnored`.
  - `RiskInputsCollectorTest`: dựng feature tạm trong `@TempDir`, có `brd.md`, `design.md` (bảng API có `POST /fee-waivers`), `design.yaml` (scope + impact) và `db/V9__x.sql` (`CREATE TABLE fee_waiver_rule (...)`), rồi kiểm từng trường của `full(...)`. Thêm một biến thể cho `lite(...)`.
  - `RiskFileTest`:
    - `writeReadRoundTrip`;
    - `withOverrideRecomputesRequirements`: high → low thì `requirements` thành rỗng hoặc bằng danh sách của `low`.
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-core test -Dtest='GlobOverlapTest,ClassifierTest,RiskInputsCollectorTest,RiskFileTest'` → FAIL.
- [ ] **Step 3: Cài đặt.** `GlobOverlap` có thể đi theo cách sẵn có trong `ScopeFlags` nếu ở đó đã có logic glob-giao-glob. Nếu chưa có, dùng phép xấp xỉ của spec §3.2: thay `**` và `*` của `scopeGlob` bằng một đoạn mẫu `x`, rồi khớp với `ruleGlob`, và làm cả chiều ngược lại.
- [ ] **Step 4: Chạy.** Lệnh như Step 2 → PASS.
- [ ] **Step 5: Commit.** `feat(core): phân loại rủi ro và bản chụp risk.yaml`

### Task 4: Core — kiểm yêu cầu ở cổng Case, khóa `risk.yaml`, hook

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/risk/RequirementChecker.java`, `.../risk/RequirementStatus.java`
- Modify:
  - `aisdlc-core/src/main/java/com/aisdlc/core/gate/Gate.java`: artifact của `CASES` thêm `p.risk()`.
  - `aisdlc-core/src/main/java/com/aisdlc/core/policy/HookPolicy.java`
  - `aisdlc-core/src/main/java/com/aisdlc/core/review/CaseFlags.java` (hoặc `ReviewCardBuilder.java`): cờ đỏ theo từng yêu cầu thiếu.
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/risk/RequirementCheckerTest.java`, `.../policy/HookPolicyTest.java` (thêm case), `.../gate/GateTest.java` (thêm case)

**Interfaces:**
- Consumes: `RiskFile`, `Requirement`, `RequirementCatalog`, `DocFacts.Rule.boundary()`, `When.repeat/parallel`, `DesignSpec.invariants()`.
- Produces:
  - `record RequirementStatus(String id, String name, Status status, String detail)` với `enum Status { OK, MISSING, LATER }`.
  - `RequirementChecker.check(RiskFile risk, CasesSpec cases, DesignSpec design, DocFacts.BrdDoc brd, DocFacts.DesignDoc designDoc) -> List<RequirementStatus>`. Ba tham số `design`, `brd`, `designDoc` nhận `null` ở luồng lite.
  - `RequirementChecker.findings(List<RequirementStatus>) -> List<Finding>`: mỗi mục `MISSING` thành một finding `RISK_REQ`, message dạng `"Mức <level> yêu cầu <name>: <detail>"`.

**Quy tắc** (spec §3.3):
- `rule-pos-neg`: mỗi `brd.rules()` có ≥1 case `HAPPY` và ≥1 case `ERROR` hoặc `BOUNDARY` có `rules` chứa id đó. `detail` liệt kê các id còn thiếu. Lite (`brd==null`) → `LATER`, kèm detail "không có BRD".
- `boundary`: mỗi rule có `boundary` có ≥ `min` (mặc định 1) case `BOUNDARY`.
- `idempotent`:
  - Lấy các dòng `apiLines` có method POST/PUT/PATCH/DELETE.
  - Mỗi endpoint cần ≥1 case có `when.repeat>=2` với method trùng và path khớp. `{x}` trong bảng API khớp mọi đoạn.
  - Không có `apiLines` thì chỉ cần ≥1 case như vậy trong cả bộ.
- `concurrent`: ≥1 case có `when.parallel >= threads`.
- `invariant`: `design.invariants().size() >= min`. Lite → `MISSING`, detail "luồng lite không có design.yaml".
- `coverage` → `LATER`, detail "kiểm ở Review".
- `sql-zero` → `LATER`, detail "harness chạy sau mọi case".

**Hook:**
- `FEATURE_STATE` thêm `risk\.yaml`.
- Chặn ghi `aisdlc/risk-policy.yaml` với thông điệp riêng: "Chính sách rủi ro do dev sửa (web hoặc MR), agent không được sửa".
- `.aisdlc/web/policy.json` đã nằm dưới `.aisdlc/`, tự động bị chặn.

- [ ] **Step 1: Viết test**
  - `RequirementCheckerTest`, mỗi quy tắc có một test đủ và một test thiếu:
    - `rulePosNeg_missingNegativeForBR2` → `MISSING`, detail chứa `BR-2`;
    - `idempotent_perMutatingEndpoint`: API có `POST /fee-waivers` và `GET /fee-waivers`, chỉ có case repeat cho POST → `OK`;
    - `idempotent_noApiTable_anyRepeatCase`;
    - `concurrent_threadsThreshold`: case `parallel 4`, yêu cầu 8 → `MISSING`;
    - `invariant_lite_missing`;
    - `coverageAndSqlZeroAreLater`;
    - `noRiskRequirements_empty`.
  - `HookPolicyTest`: `denyWriteRiskYaml`, `denyWriteRiskPolicyYaml`, `denyBashRedirectToRiskYaml` (theo mẫu test Bash sẵn có).
  - `GateTest.casesArtifactsIncludeRisk`: khi có `risk.yaml`, file nằm trong `Gate.CASES.artifacts(...)`; khi không có file thì hash giữ nguyên như hiện tại (tương thích ngược).
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-core test -Dtest='RequirementCheckerTest,HookPolicyTest,GateTest'` → FAIL.
- [ ] **Step 3: Cài đặt.** Review card cổng Case: thêm một `ReviewFlag` cho mỗi mục `MISSING`. Card nhận thêm danh sách `RequirementStatus` (null khi không có `risk.yaml`) để hiện bảng ĐẠT / THIẾU / SAU. Card thiết kế hiện `level` và `matched`. Đi theo mẫu của `ReviewCardBuilder` và hợp đồng JSON `reviewCards`; trường mới là tùy chọn.
- [ ] **Step 4: Chạy.** `mvn -q -pl aisdlc-core test` → PASS.
- [ ] **Step 5: Commit.** `feat(core): kiểm yêu cầu rủi ro ở cổng Case, khóa risk.yaml, hook chặn chính sách`

### Task 5: Core — eval: coverage cứng và mục Rủi ro

**Files:**
- Modify in `aisdlc-core/src/main/java/com/aisdlc/core/eval/`: `EvalService.java` (khoảng dòng 106–126 và `coverage(...)` ở dòng 336), `EvalReport.java`, `ReportMarkdown.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/eval/EvalServiceTest.java` (hoặc test coverage sẵn có), `ReportMarkdownTest.java`

**Interfaces:**
- Consumes: `RiskFile.read(paths.risk())`, `RequirementCatalog.COVERAGE`, `SQL_ZERO`.
- Produces:
  - `HardCheck` id `RISK_COVERAGE`, chỉ có khi `risk.yaml` có yêu cầu `coverage`. Dùng cùng base và cùng `ChangedLines.coverage(...)` với kiểm tra mềm; ngưỡng là `min/100.0`. Khi đã có `RISK_COVERAGE` thì bỏ soft check coverage để không báo trùng.
  - `EvalReport.risk`, nullable: `record RiskSection(String level, int policyVersion, List<String> matched, String override, List<RequirementStatus> requirements)`. Trạng thái `coverage` và `sql-zero` lấy từ kết quả eval: `sql-zero` OK khi mọi case PASS, vì harness làm case FAIL khi vi phạm (Task 6).
  - `ReportMarkdown` thêm mục `## Rủi ro` ngay sau phần tóm tắt.

- [ ] **Step 1: Viết test**
  - `coverageHardWhenRiskRequires`: risk `coverage min 90`, coverage thực 0.85 → `RISK_COVERAGE` failed và eval tổng là FAIL.
  - `coverageSoftWithoutRisk`: không có `risk.yaml` → hành vi y như cũ, không có `RISK_COVERAGE`.
  - `reportHasRiskSection`: markdown chứa `## Rủi ro`, tên mức và danh sách yêu cầu.
- [ ] **Step 2: Chạy để thấy fail. Step 3: Cài đặt. Step 4: `mvn -q -pl aisdlc-core test` → PASS.**
- [ ] **Step 5: Commit.** `feat(core): eval coverage cứng theo mức rủi ro, mục Rủi ro trong report`

### Task 6: Harness — repeat, parallel, bất biến, sql-zero

**Files:**
- Modify in `aisdlc-junit/src/main/java/com/aisdlc/junit/`: `CaseExecutor.java`, `HttpCaller.java`, `DbChecker.java`, `AisdlcCaseSuite.java` (đọc `design.yaml invariants` và `risk.yaml` `sql-zero`)
- Test: các test sẵn có của module `aisdlc-junit` (Testcontainers + WireMock), thêm `CaseExecutorRiskTest.java`

**Interfaces:**
- Consumes: `When.repeat/parallel`, `Then.statusCounts`, `Invariant`, `RiskFile`, `RequirementCatalog.SQL_ZERO`.
- Produces:
  - `CaseExecutor` có constructor mới nhận thêm `List<Invariant> checks`. `AisdlcCaseSuite` gộp danh sách này từ `design.invariants()` và các yêu cầu `sql-zero` (`name`, `sql`) của `risk.yaml`.
  - `DbChecker.violations(String sql, int limit) -> List<Map<String,Object>>`:
    - lấy một kết nối riêng, `setReadOnly(true)`, `setAutoCommit(false)`;
    - chạy câu truy vấn, đọc tối đa `limit` dòng;
    - luôn `rollback()`;
    - mọi `SQLException` (kể cả lỗi do cố ghi) được ném lên cho case FAIL.

**Hành vi** (spec §5):
- `reset_sql`, `given.sql` và `given.stubs` chạy một lần mỗi case.
- `repeat=n`: gọi tuần tự n lần; so `then` với response cuối.
- `parallel=n`:
  - dùng `ExecutorService` n luồng và một `CountDownLatch` để cùng xuất phát;
  - mỗi request dùng timeout sẵn có của `HttpCaller`;
  - chờ tất cả với hạn = timeout + 5s; quá hạn thì case FAIL với thông điệp `"parallel: <k>/<n> request chưa xong sau <t>s"`;
  - so `statusCounts`, hoặc mọi status đều bằng `then.status`; bỏ qua `then.body`;
  - `then.db` kiểm sau khi tất cả xong.
- Sau phần kiểm `then` (kể cả case `error`), chạy từng check. Có dòng vi phạm thì case FAIL với thông điệp `"Bất biến '<name>' vi phạm: <tối đa 5 dòng>"`.

- [ ] **Step 1: Viết test**
  - `repeat_dbCheckedAfterLast`: endpoint đếm số lần gọi, `repeat 2` → DB ghi 2, `then.db` kỳ vọng 2 → PASS.
  - `parallel_statusCounts`: endpoint chỉ cho một request thắng (bảng có unique key), `parallel 4`, `statusCounts {200:1, 409:3}` → PASS.
  - `parallel_timeout_failsCaseNotSuite`: stub WireMock trễ quá timeout → case FAIL với thông điệp `parallel:`, case kế tiếp vẫn chạy.
  - `invariant_violationFailsCase`: bất biến `SELECT id FROM t WHERE amount < 0` và dữ liệu có một dòng âm → FAIL, thông điệp chứa tên bất biến.
  - `invariant_writeAttempt_failsAndRollsBack`: check `WITH x AS (UPDATE t SET amount=0 RETURNING id) SELECT * FROM x` → case FAIL, dữ liệu không đổi.
  - `sqlZeroFromRiskYaml`: suite đọc `risk.yaml` có `sql-zero` và chạy check đó.
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-junit -am test -Dtest='CaseExecutorRiskTest' -Dsurefire.failIfNoSpecifiedTests=false` → FAIL.
- [ ] **Step 3: Cài đặt. Step 4: `mvn -q -pl aisdlc-junit -am verify -Dskip.ui` → PASS.**
- [ ] **Step 5: Commit.** `feat(junit): repeat, parallel, bất biến và sql-zero chạy sau mọi case`

### Task 7: CLI và plugin — nguồn chính sách, setup, policy/risk, ghi `risk.yaml`, sự kiện

**Files:**
- Create:
  - `aisdlc-cli/src/main/java/com/aisdlc/cli/risk/PolicySource.java`
  - `aisdlc-core/src/main/java/com/aisdlc/core/web/PolicyCache.java`
  - `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/PolicyCommand.java`
  - `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/RiskCommand.java`
- Modify:
  - in `aisdlc-cli/src/main/java/com/aisdlc/cli/`: `commands/SetupCommand.java`, `commands/SubmitCommand.java`, `commands/StatusCommand.java`, `web/WebControl.java` (hoặc `BackgroundFlush`: tải chính sách khi flush), `web/Event.java` (javadoc type), lớp đăng ký subcommand gốc
  - `plugin/commands/design.md`, `plugin/commands/cases.md`
  - in `aisdlc-cli/src/main/resources/templates/`: `CLAUDE.aisdlc.md`, `design.yaml`, `cases.lite.yaml`, `brd.md`
- Test in `aisdlc-cli/src/test/java/com/aisdlc/cli/`: `risk/PolicySourceTest.java`, `commands/SetupCommandTest.java`, `commands/SubmitRiskTest.java`, `commands/RiskCommandTest.java`, `commands/PolicyCommandTest.java`

**Interfaces:**
- Consumes: Task 1–4; `SignedEnvelope`, `PinnedKey`, `WebClient`, `WebConfig`, cơ chế console thật của `ApproveCommand`; `Outbox`/`WebPublisher` để gửi `Event`.
- Produces:
  - `PolicyCache`:
    - `static Path file(Path repoRoot)` → `.aisdlc/web/policy.json`;
    - `static Optional<CachedPolicy> read(Path file, PublicKey pinned, String repoId)`;
    - `record CachedPolicy(String repoId, int version, String yaml)`: payload JSON đã ký.
    - Sai chữ ký hoặc `repoId` khác → `Optional.empty()`.
  - `PolicySource.load(Context ctx, Path root) -> Loaded` với `record Loaded(RiskPolicy policy, String origin)`, `origin` ∈ `"file"|"web"`:
    - Có cấu hình web (`aisdlc web login` cho repo này) → đọc `PolicyCache`. Thiếu hoặc không hợp lệ → `Context.Abort(2, "Chưa có chính sách rủi ro từ web (hoặc chữ ký sai). Chạy: aisdlc policy pull")`.
    - Không có web → `aisdlc/risk-policy.yaml`. Thiếu file → `Presets.latest("generic")` kèm cảnh báo trên stderr `"Chưa có aisdlc/risk-policy.yaml; dùng mẫu generic. Chạy aisdlc setup --preset …"`.
    - `RiskPolicyException` → `Abort(2, …)`, liệt kê từng lỗi.
  - `SetupCommand`: option `--preset <name>` mặc định `generic`. Tên không có trong `Presets.names()` → `Abort(2)`. Gọi `create(root, "aisdlc/risk-policy.yaml", Presets.yaml(name, latestVersion))`, và theo hành vi sẵn có của `create` thì không ghi đè file đã có.
  - `aisdlc policy show`: in thang mức, các rule và danh sách nhãn mà rule `label` dùng, để agent đọc.
  - `aisdlc policy pull`: `GET /api/cli/policy?repo=<repoId>` rồi ghi `PolicyCache`.
    - Nếu web trả 404 (repo chưa có chính sách), CLI gửi `POST /api/cli/policy/import` với body `{repo, yaml}` lấy từ `aisdlc/risk-policy.yaml` (hoặc mẫu generic), rồi pull lại.
    - In `"Chính sách v<N> (web)"`.
  - `aisdlc policy diff-preset`: in `PolicyDiff.diff(Presets.load(name, preset.version), Presets.latest(name))`. `PolicyDiff` thuộc Task 8, nên task này phải làm Task 8 bước core trước hoặc tạo `PolicyDiff` ở đây. **Ruling: tạo `PolicyDiff` trong Task 7 (core) và Task 8 dùng lại.**
  - `PolicyDiff.diff(RiskPolicy from, RiskPolicy to) -> List<PolicyChange>` với `record PolicyChange(String kind, String key, String before, String after, boolean loosening)`:
    - `kind` ∈ `ADD_RULE, REMOVE_RULE, CHANGE_RULE, ADD_REQ, REMOVE_REQ, CHANGE_PARAM, CHANGE_CLASSIFICATION`;
    - `loosening=true` khi: bỏ yêu cầu; giảm `min`, `threads` hoặc `min` của coverage; tắt hoặc xóa rule; hạ mức của rule; `agent` từ `RAISE` thành `OFF`; `devLower` từ `NEVER` thành `REASON`.
  - `SubmitCommand`:
    - `submit brd`: tính `Classifier.classify(policy, RiskInputsCollector.brdOnly(...))`, in `"Rủi ro tạm (từ khóa): <level>"` và đưa vào review card BRD (trường tùy chọn `riskPreview: {level, matched}`), không ghi file. Lỗi chính sách ở bước này chỉ cảnh báo.
    - `submit design`: gọi `DesignValidator.validate(md, spec, policy.levels())`; không có finding thì ghi `RiskFile.of(...)` vào `paths.risk()`, gửi `Event("RISK_CLASSIFIED", …, detail=JSON {level, policyVersion, matched:[ruleIds]})` và in mức cùng các rule đã khớp.
    - `submit cases`:
      - FULL: nếu `risk.yaml` có `policy.hash` khác hash chính sách hiện tại, hoặc thiết kế đã đổi kể từ lần ghi, và đã có `override`, thì giữ nguyên override và chỉ in cảnh báo. Không có `override` thì tính lại và ghi.
      - LITE: tính bằng `RiskInputsCollector.lite`, ghi `risk.yaml`.
      - Sau đó thêm `RequirementChecker.findings(...)` vào `findings`.
  - `aisdlc risk set <level> --reason <text>`:
    - cần console thật;
    - thiếu `risk.yaml` → `Abort(2)`;
    - cổng `CASES` đã duyệt → `Abort(2, "Cổng Case đã duyệt; mở lại cổng Case trước")`;
    - hạ mức mà thiếu `--reason`, hoặc `devLower==NEVER` → `Abort(2)`;
    - ghi `withOverride(...)`, gửi `RISK_OVERRIDDEN` với detail `{from,to,reason}`;
    - nếu cổng Case đang chờ duyệt thì đưa về trạng thái phải nộp lại, theo cách `reopen` hoặc `reject` sẵn có.
  - `aisdlc risk reclassify`:
    - cần console thật;
    - tính lại theo chính sách hiện tại và bỏ override;
    - nếu cổng Case đã duyệt thì `GateService.reopen(Gate.CASES, approver)` trước;
    - in diff mức và yêu cầu cũ → mới.
  - Web sync: mỗi lần flush nền hoặc `aisdlc status` có web, gọi pull chính sách (lỗi mạng thì im lặng giữ cache cũ). `aisdlc status` in `"Rủi ro: <level> (chính sách v<N>)"`. Ở chế độ web, nếu file `aisdlc/risk-policy.yaml` khác bản web thì nhắc.
  - Template:
    - `design.yaml` thêm khối `impact:` mẫu và `invariants: []` có chú thích;
    - `cases.lite.yaml` thêm `impact:`;
    - `brd.md` thêm ví dụ thẻ `[biên]`;
    - `plugin/commands/design.md` dặn agent: chạy `aisdlc policy show`, điền `impact` (level, labels, reason), khai `invariants` khi tính năng ghi dữ liệu tiền hoặc trạng thái;
    - `plugin/commands/cases.md` dặn agent: đọc `risk.yaml` (chỉ đọc) và viết đủ case theo `requirements` (repeat, parallel, boundary).

- [ ] **Step 1: Viết test**
  - `PolicySourceTest`:
    - `fileMode_readsRepoFile`;
    - `fileMode_missingFile_genericWithWarning`;
    - `load_webMode_missingCache_aborts`;
    - `load_webMode_badSignature_aborts`;
    - `webMode_validCache`.

    Dùng `HOME` tạm và cặp khóa sinh trong test, giống các test `WebControl` sẵn có.
  - `SetupCommandTest`:
    - `presetBankingWritesPolicy`;
    - `rerunDoesNotOverwrite`;
    - `unknownPresetExit2`.
  - `SubmitRiskTest`, trên repo tạm có feature FULL dựng sẵn và mẫu banking:
    - `submitDesign_writesRiskYaml` (level `high`, `matched` có rule bảng);
    - `submitDesign_missingImpact_blocked`;
    - `submitDesign_brokenPolicy_exit2WithRuleId`;
    - `submitCases_missingIdempotent_blocked` (thông điệp có `"gửi trùng"`);
    - `submitCases_lite_classifies`;
    - `noRiskFileLegacyFeature_casesUnchanged`: cổng Design đã duyệt từ trước và không có `risk.yaml` → không ghi, không thêm finding.
  - `RiskCommandTest`:
    - `lowerWithoutReason_exit2`;
    - `lowerWithReason_writesOverrideAndEvent`;
    - `afterCasesApproved_exit2`;
    - `reclassify_reopensCases`;
    - `nonConsole_exit2`.
  - `PolicyCommandTest`:
    - `show_listsLabels`;
    - `pull_importsWhen404ThenCaches` (server HTTP giả, theo mẫu test WebClient sẵn có);
    - `diffPreset_printsChanges`.
  - `PolicyDiffTest` (core):
    - `loosening_requirementRemoved`;
    - `loosening_paramDecreased`;
    - `notLoosening_ruleAdded`;
    - `loosening_ruleLevelLowered`.
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-cli -am test -Dskip.ui -Dtest='PolicySourceTest,SetupCommandTest,SubmitRiskTest,RiskCommandTest,PolicyCommandTest,PolicyDiffTest' -Dsurefire.failIfNoSpecifiedTests=false` → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy toàn bộ.** `mvn -q -pl aisdlc-cli -am verify -Dskip.ui` → PASS. Sau đó build lại jar và chạy test plugin: `mvn -q -pl aisdlc-cli -am package -DskipTests -Dskip.ui && cp aisdlc-cli/target/aisdlc-cli-*-all.jar plugin/lib/aisdlc-cli.jar` (đi theo đúng tên jar mà `plugin/install.sh` dùng), rồi `bash plugin/test/plugin-files-test.sh` → PASS.
- [ ] **Step 5: Commit.** `feat(cli): chính sách rủi ro — setup --preset, policy show/pull/diff-preset, risk set/reclassify, risk.yaml lúc submit`

### Task 8: Web backend — phiên bản chính sách, nhập lần đầu, API ký, phân loại thử

**Files:**
- Create:
  - `aisdlc-web/src/main/resources/db/migration/V4__risk_policy.sql`
  - in `aisdlc-web/src/main/java/com/aisdlc/web/risk/`: `PolicyRepository.java`, `PolicyService.java`, `PolicyController.java`
- Modify: `aisdlc-web/src/main/java/com/aisdlc/web/ingest/EventStore.java` (nhận `RISK_CLASSIFIED`, `RISK_OVERRIDDEN`, cập nhật cột trên `features`), board API (thêm `riskLevel` vào item tính năng)
- Test in `aisdlc-web/src/test/java/com/aisdlc/web/risk/`: `PolicyControllerIT.java`, `PolicyServiceTest.java`; `EventStoreIT` thêm case

**Schema:**
```sql
CREATE TABLE risk_policy_versions (
  repo_id text NOT NULL, version int NOT NULL, body text NOT NULL, hash text NOT NULL,
  author text NOT NULL, reason text NOT NULL, source text NOT NULL CHECK (source IN ('import','web','preset-update')),
  created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (repo_id, version));
ALTER TABLE features ADD COLUMN risk_level text, ADD COLUMN risk_policy_version int;
```

**Interfaces:**
- Consumes: `RiskPolicy`, `Presets`, `PolicyDiff`, `Classifier`, `RiskInputs`, `SignedEnvelope.sign`; khóa ký và xác thực token mà `/api/cli/control` đang dùng.
- Produces (HTTP):
  - **Cho CLI** (cùng xác thực token với `/api/cli/control`):
    - `GET /api/cli/policy?repo=` → `SignedEnvelope` có payload `{repoId, version, yaml}`; 404 khi repo chưa có phiên bản nào.
    - `POST /api/cli/policy/import` với `{repo, yaml}` → 201 và tạo v1 (`source=import`, `author` = user của token, `reason="Nhập từ aisdlc/risk-policy.yaml"`). Repo đã có phiên bản → 200, không đổi gì. YAML hỏng → 400 kèm `errors`.
  - **Cho UI** (session + CSRF, giống `/api/control/**`):
    - `GET /api/policy?repo=` → `{version, policy(JSON), hash, presetUpdate: {name, from, to, changes:[PolicyChange]} | null, featureCounts: {<version>: n}}`.
    - `GET /api/policy/versions?repo=` → danh sách `{version, author, reason, source, createdAt, changes:[PolicyChange so với bản trước]}`, mới nhất trước.
    - `POST /api/policy/publish` với `{repo, baseVersion, policy(JSON), reason}`:
      - `reason` rỗng → 400;
      - `RiskPolicy` lỗi → 400 kèm `errors`;
      - `baseVersion` khác bản mới nhất → 409 với `{message:"Đã có phiên bản mới hơn (v<N>); tải lại để xem."}`;
      - thành công → 201 `{version, loosening:[PolicyChange]}`, `source` là `web`, hoặc `preset-update` khi client gửi `fromPreset=true`.
    - `POST /api/policy/validate` với `{policy}` → `{errors:[], changes:[PolicyChange so với bản đang hiệu lực]}`. Hộp xác nhận xuất bản dùng endpoint này.
    - `POST /api/policy/classify` với `{policy, inputs: RiskInputs}` → `{result: RiskResult, requirements:[{id, name, params}]}`.
- `EventStore`:
  - `RISK_CLASSIFIED` → cập nhật `features.risk_level` và `risk_policy_version`;
  - `RISK_OVERRIDDEN` → cập nhật `risk_level`.

- [ ] **Step 1: Viết test** (Testcontainers Postgres, dùng `PostgresSupport` sẵn có)
  - `import_createsV1_onceOnly`.
  - `cliGet_signedVerifiesWithPinnedKey`.
  - `cliGet_404WhenNone`.
  - `publish_createsNextVersion`.
  - `publish_staleBase_409`.
  - `publish_invalidPolicy_400WithErrors`.
  - `publish_requiresReason`.
  - `publish_requiresSessionAndCsrf`: token CLI → 401/403.
  - `classify_matchesCore`: so kết quả HTTP với `Classifier.classify` trên cùng input.
  - `presetUpdate_whenBundledNewer`: chèn v1 có `preset.version="0"`, giả lập bằng cách ghi YAML có version cũ, rồi kiểm `presetUpdate.changes` không rỗng.
  - `eventsUpdateFeatureRisk`.
- [ ] **Step 2: Chạy.** `mvn -q -pl aisdlc-web -am verify -Dskip.ui -Dit.test='PolicyControllerIT,EventStoreIT'` (theo cấu hình failsafe sẵn có của module) → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: `mvn -q -pl aisdlc-web -am verify -Dskip.ui` → PASS.**
- [ ] **Step 5: Commit.** `feat(web): phiên bản chính sách rủi ro, nhập lần đầu, API ký cho CLI, phân loại thử`

### Task 9: Web UI — trang chính sách và hiển thị trên tính năng

**Files:**
- Create in `aisdlc-web/ui/src/`:
  - `pages/RiskPolicy.tsx`
  - in `components/`: `RiskRules.tsx`, `RiskRequirements.tsx`, `RiskTry.tsx`, `RiskVersions.tsx`, `RiskPanel.tsx`
  - `risk.ts` (gọi API, kiểu dữ liệu, logic nháp), `risk.test.ts`
- Modify in `aisdlc-web/ui/src/`: `App.tsx` (route), `routes.ts` (thêm `riskHref(repoId)` và `parseRiskPath`), `pages/Repos.tsx` (link "Chính sách rủi ro"), `pages/Board.tsx` (cột mức), `pages/Review.tsx` (gắn `RiskPanel`), `types.ts`
- Test: `risk.test.ts`, `routes.test.ts` (thêm case)

**Interfaces:**
- Consumes: các API của Task 8; `reviewCards` có thêm danh sách yêu cầu (Task 4).
- Produces:
  - route `/risk/<repoId nhiều đoạn>`, tab theo hash `#rules | #levels | #try | #history`, mặc định `#rules`;
  - `risk.ts`:
    - `loadPolicy(repo)`, `publish(repo, baseVersion, policy, reason)`, `validate(policy)`, `classify(policy, inputs)`, `versions(repo)`;
    - `draftChanges(base, draft): string[]`: mô tả từng thay đổi để hiện trên thanh nháp;
    - nháp lưu `localStorage` theo key `aisdlc.risk.draft.<repoId>`, mọi truy cập bọc try/catch.

**UI:** theo prototype https://claude.ai/artifact/11WPT7XtbgHMVxBBS21oXw và spec §6, ở chế độ dev tự xuất bản. Dùng token màu và các lớp sẵn có trong `styles.css`.
- **Rule:** bảng có nguồn "mẫu <tên>" khi id rule có trong mẫu gốc, ngược lại là "dự án". Hộp thoại thêm/sửa. Bật/tắt. Khối nguồn phân loại.
- **Yêu cầu:** ma trận checkbox × mức, có ô số tham số. Form `sql-zero` gồm tên và SQL.
- **Thử phân loại:** form inputs, gọi `classify` (debounce 300 ms) với policy nháp.
- **Phiên bản:**
  - dòng thời gian, mỗi phiên bản kèm số tính năng;
  - thông báo mẫu mới khi `presetUpdate` khác null;
  - mỗi `change` có nút "Áp dụng vào nháp" hoặc "Bỏ qua" (bỏ qua chỉ ẩn trong phiên làm việc).
- **Thanh nháp:** các nút "Bỏ nháp", "Xem thay đổi" và "Xuất bản". Xuất bản mở hộp thoại:
  - gọi `validate`;
  - lý do bắt buộc;
  - nếu có `loosening` thì liệt kê dưới tiêu đề "Thay đổi này nới yêu cầu";
  - gặp 409 thì hiện thông điệp từ server và nút "Tải lại".
- **`RiskPanel` trên trang tính năng:** mức, nguồn và danh sách ĐẠT / THIẾU / SAU lấy từ review card. **Ruling (lệch spec §3.5 "hoặc nút trên web"):** đợt này không có nút đổi mức trên web, vì `risk set` ghi file trong repo và cần console thật. Panel hiện lệnh `aisdlc risk set …` bằng `CopyCommand` sẵn có. Nút trên web làm sau qua pending action nếu cần.
- **Nút "Tải YAML"** trên trang Phiên bản: hiện `policy.toYaml()` trong khung chọn được, kèm nút Copy; không dùng `<a download>`. Dùng để dev commit lại `aisdlc/risk-policy.yaml` nếu muốn (spec §4.2).
- **Board:** thêm cột "Rủi ro" dạng pill.

- [ ] **Step 1: Viết test Vitest**
  - `draftChanges_describesRuleEditAndParam`.
  - `draftStorage_survivesThrowingLocalStorage`.
  - `parseRiskPath_multiSegmentRepo`.
  - `riskHref_neverEncodesSlash`.
- [ ] **Step 2: `cd aisdlc-web/ui && npx vitest run` → FAIL. Step 3: Cài đặt. Step 4: `npx vitest run && npx tsc --noEmit` → PASS, rồi `mvn -q -pl aisdlc-web -am verify` (có build UI) → PASS.**
- [ ] **Step 5: Commit.** `feat(ui): trang chính sách rủi ro (rule, yêu cầu, thử phân loại, phiên bản) và panel rủi ro trên tính năng`

### Task 10: E2E và tài liệu

**Files:**
- Modify:
  - `e2e/run-e2e.sh`, `e2e/web-*.mjs` (thêm bước kiểm trang `/risk`)
  - `e2e/fixture-service/aisdlc/risk-policy.yaml` (tạo mới)
  - `e2e/solutions/PAY-1/**`: thêm `impact` vào `design.yaml`, thêm một case `repeat: 2` và một bất biến
  - `docs/README.vi.md` (mục mới "Chính sách rủi ro"), `README.md`: bảng trạng thái thêm dòng "Chính sách rủi ro"; dòng giai đoạn 3 đổi "Đang làm" thành "Tạm hoãn"

**Kịch bản:**
1. Fixture dùng mẫu `generic` cộng một rule `keyword` khớp một từ chắc chắn có trong BRD của PAY-1 (đọc `e2e/solutions/PAY-1/brd.md` để chọn) → `high`. Mức `high` yêu cầu `idempotent` và `invariant`, không yêu cầu `concurrent`, để E2E chạy nhanh.
2. Sau `submit design`: `risk.yaml` tồn tại với `level: high`.
3. Nộp `cases.yaml` **chưa có** case `repeat` → `submit cases` exit ≠ 0, stderr chứa `"gửi trùng"`.
4. Chép bản có case `repeat` → submit qua. Luồng tiếp tục như cũ đến eval.
5. `eval/report.md` chứa `## Rủi ro`.
6. `WEB=1`:
   - trước bước 2 chạy `aisdlc policy pull`, nhập lần đầu tạo v1 trên web;
   - Playwright mở `/risk/<repo>#rules` và thấy rule của fixture;
   - trang tính năng PAY-1 có panel rủi ro "Cao".
7. `WEB=0`: đọc chính sách từ file.

- [ ] **Step 1: Viết kịch bản và dữ liệu như trên.**
- [ ] **Step 2: Chạy.** `WEB=0 e2e/run-e2e.sh` → PASS; `e2e/run-e2e.sh` → PASS; `SCREENSHOTS=1 e2e/run-e2e.sh` → PASS, kèm ảnh trang `/risk`.
- [ ] **Step 3: Cập nhật tài liệu.** `docs/README.vi.md`: chính sách là gì, `setup --preset`, sửa trên web hoặc trong file, `impact` và `invariants`, `risk set/reclassify`, bảng thư viện yêu cầu.
- [ ] **Step 4: Chạy tổng.** `mvn -q verify` và `bash plugin/test/plugin-files-test.sh` → PASS.
- [ ] **Step 5: Commit.** `test(e2e): luồng rủi ro cao chặn thiếu case gửi trùng; docs chính sách rủi ro`
