# AISDLC giai đoạn 1: bộ công cụ Claude Code + eval tầng 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Một plugin Claude Code + CLI `aisdlc` + thư viện JUnit giúp team Spring Boot đi theo quy trình BRD → Thiết kế → Case → Code từng bước → Review, có cổng duyệt do dev mở, hook chặn agent làm sai quy trình, và báo cáo eval tầng 1. Chưa có web.

**Architecture:** Mọi logic nằm trong một CLI Java (`aisdlc-cli`, jar shaded). Hook của plugin và slash command đều gọi CLI này. Trạng thái tính năng nằm trong repo dịch vụ ở `aisdlc/features/<KEY>/` (commit cùng code). Trạng thái chỉ dùng cục bộ (feature đang active) nằm ở `.aisdlc/` (gitignore). Bộ case `cases.yaml` được chạy bởi thư viện `aisdlc-junit`: một lớp test tham số hóa nằm trong repo dịch vụ, đọc YAML lúc chạy, gọi HTTP thật vào app Spring Boot với DB Testcontainers và WireMock, rồi ghi `results.json` cùng `schema.json` để CLI đọc.

**Tech Stack:** Java 17, Maven (multi-module), picocli 4.7.6, Jackson 2.17.2 (+ jackson-dataformat-yaml), JUnit 5.10.3, AssertJ 3.26.3, Spring Boot 3.3.5 (chỉ cho `aisdlc-junit` và fixture), Testcontainers 1.20.3 (PostgreSQL 16 trong fixture), WireMock 3.9.1 (`org.wiremock:wiremock-standalone`), JSONAssert 1.5.3, maven-shade-plugin 3.6.0, Claude Code plugin (commands, agents, hooks).

**Spec:** `docs/superpowers/specs/2026-11-02-aisdlc-design.md`

## Global Constraints

- Chỉ giai đoạn 1: **không** có web, không gọi Jira/GitLab/Confluence trừ kiểm tra ticket Jira tùy chọn (Task 7). Spec §9.
- Môi trường đích: Maven, Flyway, Docker có trên máy dev và GitLab runner, máy dev Linux (spec §7c.4: giả định, phải xác minh trên repo thí điểm ở Task 13).
- Mã nguồn và tài liệu không gửi ra dịch vụ ngoài mạng nội bộ (spec §8). CLI không gọi mạng ngoài, chỉ gọi Jira nội bộ nếu cấu hình.
- Thông báo cho dev (CLI, hook, báo cáo) viết bằng **tiếng Việt**, ngắn gọn. Mã và định danh viết bằng tiếng Anh.
- Lệnh chỉ dev được chạy (`approve`, `reject`, `reopen`, `step accept`, `scope allow`) **phải** đọc xác nhận từ console thật (`System.console() != null`) và bắt dev gõ lại feature key. Không có cờ hay biến môi trường nào bỏ qua được bước này.
- Agent không được sửa artifact của cổng đã duyệt, kể cả qua Bash (spec §7b). Mô hình đe dọa: agent mắc lỗi thật thà, không phải tấn công có chủ đích.
- Ngưỡng mặc định: cảnh báo ngân sách khi vượt **1,5 lần**; cảnh báo coverage dòng mới hoặc sửa dưới **80%** (spec §6).
- Kiểm tra sau mỗi bước chỉ chạy case của các bước ≤ bước hiện tại. Bộ đầy đủ chạy khi `eval` và trong CI (spec §8).
- CI chỉ chạy kiểm tra tất định, không gọi Claude. Hash lệch trong CI chỉ **cảnh báo** ở giai đoạn 1 (spec §7b).
- Luồng nhẹ (`--lite`) bỏ BRD và thiết kế, nhưng bắt buộc có cases được duyệt và khóa, kèm `scope` và `budget` khai báo trong `cases.yaml` (spec §7c.3).
- Agent dừng sau mỗi bước `Sx`. Bước kế chỉ start được khi dev đã `step accept` bước trước (spec §7c.2).
- Plugin cài toàn cục không được ảnh hưởng repo không dùng AISDLC: hook phải no-op khi không tìm thấy `aisdlc/config.yaml`.

## Review Focus

1. **Tiêu đề BRD tiếng Việt bị lưu ở dạng Unicode NFD hoặc có khoảng trắng thừa** (do một số editor): dev vẫn kỳ vọng kiểm tra mục bắt buộc pass. Test nằm ở Task 3.
2. **Agent sửa file đã khóa qua Bash** (`cat > …/cases.yaml`, `sed -i`, `git checkout -- …`, `rm`): dev kỳ vọng bị chặn như khi dùng Edit/Write. Test nằm ở Task 6.
3. **File mới chưa track, file xóa, đổi tên, file binary**: phải được tính đúng vào phạm vi và ngân sách, không bị bỏ sót hay làm crash. Test nằm ở Task 2 và Task 5.
4. **Repo không dùng AISDLC hoặc hook nhận JSON hỏng**: repo ngoài thì no-op hoàn toàn. Repo AISDLC thì PreToolUse cho ghi file phải *đóng* (deny) kèm lý do đọc được, không im lặng cho qua. Test nằm ở Task 6.
5. **Dev sửa artifact sau khi đã duyệt** (ví dụ sửa BRD khi đang code): cổng chuyển STALE, mọi cổng sau coi như đóng, hook chặn ghi code cho tới khi duyệt lại. Test nằm ở Task 4 và Task 6.

---

## Cấu trúc file

```
aisdlc/                                  (repo này)
  pom.xml                                parent, quản lý version
  .gitignore                             target/, .superpowers/, plugin/lib/
  .claude-plugin/marketplace.json        marketplace cục bộ trỏ tới plugin/
  aisdlc-core/                           model, git, spec, gate, step, policy, eval (không phụ thuộc Spring)
    src/main/java/com/aisdlc/core/
      model/   FeatureKey, Flow, FeatureMeta, FeaturePaths, RepoConfig, CheckCommand, ActiveFeature, Hashing, Text
      git/     GitRepo, FileChange, ChangeType
      spec/    DesignSpec, StepSpec, Budget, CasesSpec, CaseSpec(+Given/When/Then/DbExpect/MessageExpect), StubSpec, CaseKind, Finding,
               BrdValidator, DesignValidator, CasesValidator, EffectiveSteps
      gate/    Gate, GateState, Approval, Submission, GateStore, LocalGateStore, GateService
      step/    StepStatus, StepRecord, StepCheck, StepService, ScopeMatcher, ScopeExtras
      run/     CaseOutcome, CaseResult, CaseRunResult, SchemaSnapshot, MavenRunner
      policy/  ToolCall, Decision, HookPolicy, BashHeuristics
      eval/    HardCheck, SoftCheck, EvalReport, EvalService, ChangedLines, JacocoCoverage, AgentReview, ReportMarkdown
  aisdlc-cli/                            picocli + resources/templates; build jar shaded aisdlc-cli.jar
    src/main/java/com/aisdlc/cli/  Main, DevConsole, SystemDevConsole, RepoLocator, commands/*Command, hook/*Hook
    src/main/resources/templates/  brd.md, design.md, design.yaml, cases.full.yaml, cases.lite.yaml,
                                   config.yaml, AisdlcCasesTest.java.tpl, gitlab-ci.aisdlc.yml, CLAUDE.aisdlc.md
  aisdlc-junit/                          thư viện test cho repo dịch vụ
    src/main/java/com/aisdlc/junit/  AisdlcCaseSuite, CaseExecutor, HttpCaller, DbChecker, StubRegistry,
                                     MessageProbe, KafkaMessageProbe, SchemaDumper, ResultWriter, HarnessOptions
    src/test/java/...                app Spring Boot mẫu nhỏ + test của harness
  plugin/                                Claude Code plugin
    .claude-plugin/plugin.json
    hooks/hooks.json
    commands/  init.md brd.md design.md cases.md step.md eval.md review.md status.md
    agents/    aisdlc-reviewer.md
    bin/aisdlc                           wrapper: java -jar ../lib/aisdlc-cli.jar
    install.sh                           build + copy jar vào plugin/lib + symlink ~/.local/bin/aisdlc
  e2e/
    fixture-service/                     repo Spring Boot mẫu (Postgres, Flyway, 1 endpoint cần làm)
    solutions/                           file "agent viết" cho từng bước, dùng trong e2e
    run-e2e.sh
  docs/README.vi.md                      hướng dẫn cho dev
```

Bố cục trong **repo dịch vụ** sau `aisdlc setup` và `aisdlc init PAY-123`:

```
aisdlc/config.yaml
aisdlc/gitlab-ci.aisdlc.yml
aisdlc/features/PAY-123/
  feature.yaml  brd.md  design.md  design.yaml  db/*.sql  cases.yaml
  schema-expected.json  gates.json  steps.json  scope-extra.json  feedback.md
  eval/report.json  eval/report.md  eval/diff-by-step.md  review-agent.json
src/test/java/<pkg>/AisdlcCasesTest.java
.aisdlc/active                           (gitignore)
CLAUDE.md                                (được nối thêm mục AISDLC)
```

---

### Task 1: Khung repo + model cơ bản

**Files:**
- Create: `pom.xml`, `aisdlc-core/pom.xml`, `aisdlc-cli/pom.xml`, `aisdlc-junit/pom.xml`, `.gitignore`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/model/{FeatureKey,Flow,FeatureMeta,FeaturePaths,RepoConfig,CheckCommand,ActiveFeature,Hashing,Text,Yaml}.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/model/ModelTest.java`

**Interfaces:**
- Produces:
  - `record FeatureKey(String value)`: constructor ném `IllegalArgumentException` nếu không khớp `^[A-Z][A-Z0-9]+-\d+$`.
  - `enum Flow { FULL, LITE }`
  - `record FeatureMeta(FeatureKey key, String title, Flow flow, String baseTree, Instant createdAt)` với `static FeatureMeta read(Path)` và `void write(Path)`. File YAML có các khóa `key,title,flow,baseTree,createdAt`.
  - `final class FeaturePaths`: `FeaturePaths(Path repoRoot, FeatureKey key)`. Các method trả `Path`: `repoRoot()`, `dir()` = `aisdlc/features/<KEY>`, `meta()` feature.yaml, `brd()`, `designMd()`, `designYaml()`, `dbDrafts()` db/, `cases()`, `schemaExpected()`, `gates()`, `steps()`, `scopeExtra()`, `feedback()`, `evalDir()`, `reviewAgent()`. Thêm `String rel(Path)`: đường dẫn tương đối repo, dùng `/`.
  - `record CheckCommand(String name, String cmd)`
  - `record RepoConfig(String mavenCmd, String casesTestClass, String casesTestPath, List<String> alwaysAllowed, List<String> unmanaged, double budgetWarnFactor, double coverageWarn, List<CheckCommand> checks, String baseRef)` với `static RepoConfig load(Path repoRoot)` đọc `aisdlc/config.yaml`; khóa nào thiếu thì dùng mặc định: `mvn`, `AisdlcCasesTest`, `src/test/java/**/AisdlcCasesTest.java`, `[]`, `["*.md","docs/**"]`, `1.5`, `0.80`, `[]`, `origin/main`.
  - `ActiveFeature`: `static Optional<FeatureKey> read(Path repoRoot)`, `static void write(Path repoRoot, FeatureKey)`. File `.aisdlc/active`.
  - `Hashing`: `static String sha256(Path repoRoot, List<Path> files)`. Hash gồm từng (đường dẫn tương đối + `\0` + bytes + `\0`) theo thứ tự truyền vào. File không tồn tại góp chuỗi `<missing>`. Trả hex.
  - `Text`: `static String nfc(String)`.
  - `Yaml`: `static <T> T read(Path, Class<T>)`, `static void write(Path, Object)` (Jackson YAML, có JavaTimeModule).

- [ ] **Step 1: `git init`, tạo parent pom và 3 module**, `.gitignore` gồm `target/`, `.superpowers/`, `plugin/lib/`, `.aisdlc/`. Parent khai báo các version ở Tech Stack. `mvn -q -DskipTests package` phải chạy được.
- [ ] **Step 2: Viết test thất bại** `ModelTest`:
  - `featureKeyRejectsLowercase`: `new FeatureKey("pay-1")` ném IAE; `new FeatureKey("PAY-123")` ok.
  - `featurePathsLayout`: `paths.rel(paths.cases())` == `"aisdlc/features/PAY-123/cases.yaml"`.
  - `repoConfigDefaultsWhenMissing`: trong thư mục tạm không có config → `budgetWarnFactor()==1.5`, `coverageWarn()==0.80`, `unmanaged()` == `["*.md","docs/**"]`.
  - `repoConfigOverrides`: config chứa `budgetWarnFactor: 2.0` → 2.0, các khóa khác vẫn mặc định.
  - `hashChangesWhenContentChanges` và `hashOfMissingFileIsStable`.
  - `featureMetaRoundTrip`.
- [ ] **Step 3: Chạy** `mvn -q -pl aisdlc-core test` → FAIL (compile error).
- [ ] **Step 4: Cài đặt** các class trên.
- [ ] **Step 5: Chạy lại** → PASS.
- [ ] **Step 6: Commit** `feat(core): repo skeleton and feature model`.

### Task 2: Snapshot git và diff

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/git/{GitRepo,FileChange,ChangeType}.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/git/GitRepoTest.java` (tạo repo git thật trong `@TempDir`)

**Interfaces:**
- Produces:
  - `enum ChangeType { ADDED, MODIFIED, DELETED, RENAMED }`
  - `record FileChange(String path, String oldPath, ChangeType type, int added, int deleted, boolean binary)`. `oldPath` là null nếu không phải RENAMED. File binary có `added=deleted=0`. Thêm `int lines()` = added+deleted.
  - `final class GitRepo`: `GitRepo(Path root)`.
    - `String snapshotTree()`: tree hash của working tree **gồm cả file chưa track** (trừ file bị ignore). Không đụng tới index của dev: dùng `GIT_INDEX_FILE` trỏ tới file tạm, chạy `git read-tree HEAD` (hoặc empty tree nếu chưa có commit), rồi `git add -A`, rồi `git write-tree`.
    - `List<FileChange> diff(String fromTree, String toTree)`: dùng `git diff --numstat -M` và `--name-status -M` giữa hai tree.
    - `Map<String, Set<Integer>> changedLines(String fromTree, String toTree)`: các dòng mới hoặc sửa ở phía `to`, parse từ `git diff -U0`.
    - `String mergeBaseTree(String ref)`: tree của `git merge-base HEAD <ref>`.
    - `String userName()`.
    - Lỗi git → `GitException` (RuntimeException) chứa stderr.

- [ ] **Step 1: Viết test thất bại:**
  - `snapshotIncludesUntrackedFiles`: tạo `a.txt` chưa add; diff(empty-commit tree, snapshot) có `a.txt` ADDED, added=1.
  - `snapshotDoesNotTouchUserIndex`: sau `snapshotTree()`, `git status --porcelain` vẫn là `?? a.txt`.
  - `detectsDeleteAndRename`: commit `x.txt`, `y.txt` (nội dung 10 dòng); xóa `x.txt`, `git mv`-tương đương (đổi tên file trên đĩa) `y.txt`→`z.txt` → có DELETED `x.txt` và RENAMED `z.txt` với oldPath `y.txt`.
  - `binaryFileHasZeroLinesAndFlag`: file 1KB byte ngẫu nhiên → `binary==true`, `lines()==0`.
  - `changedLinesReportsNewSideLineNumbers`: sửa dòng 3 của file 5 dòng → `{f.txt: [3]}`.
  - `ignoredFilesExcluded`: `.gitignore` chứa `target/` → `target/x` không xuất hiện.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-core test -Dtest=GitRepoTest` → FAIL.
- [ ] **Step 3: Cài đặt `GitRepo`** bằng `ProcessBuilder`. Không dùng JGit.
- [ ] **Step 4: Chạy lại** → PASS.
- [ ] **Step 5: Commit** `feat(core): git snapshots and diffs`.

### Task 3: Đọc spec và các validator (BRD, design, cases)

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/spec/*.java`
- Create (mẫu, dùng làm fixture test và template ở Task 7): `aisdlc-cli/src/main/resources/templates/{brd.md,design.md,design.yaml,cases.full.yaml,cases.lite.yaml}`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/spec/{BrdValidatorTest,DesignValidatorTest,CasesSpecTest,CasesValidatorTest}.java`

**Interfaces:**
- Produces:
  - `record Finding(String code, String message)`. `message` viết bằng tiếng Việt.
  - `record Budget(int files, int lines)`; `record StepSpec(String id, String title, List<String> scope, Budget budget)`; `record DesignSpec(List<StepSpec> steps)` với `static DesignSpec read(Path)`.
  - Định dạng `design.yaml`:
    ```yaml
    steps:
      - id: S1
        title: Validate request
        scope: ["src/main/java/**/limit/web/**", "src/main/resources/application*.yml"]
        budget: { files: 3, lines: 120 }
    ```
  - `enum CaseKind { HAPPY, ERROR, BOUNDARY }` (YAML viết thường: `happy|error|boundary`).
  - `record StubSpec(String id, Map<String,Object> request, Map<String,Object> response)`: request và response theo định dạng JSON mapping của WireMock.
  - `record CaseSpec(String id, String step, CaseKind kind, String title, Given given, When when, Then then)`.
    - `record Given(List<String> sql, List<String> stubs)`
    - `record When(String method, String path, Map<String,String> headers, Object body)`
    - `record Then(int status, Object body, List<DbExpect> db, List<MessageExpect> messages)`
    - `record DbExpect(String query, List<Map<String,Object>> rows)`
    - `record MessageExpect(String topic, Object value)`
  - `record CasesSpec(List<String> resetSql, List<StubSpec> stubs, List<CaseSpec> cases, List<String> scope, Budget budget)` với `static CasesSpec read(Path)`. `scope` và `budget` chỉ dùng cho LITE.
  - Định dạng `cases.yaml` (template `cases.full.yaml`):
    ```yaml
    reset_sql: ["DELETE FROM transfer_limit_usage"]
    stubs:
      - id: core-balance-ok
        request:  { method: GET, urlPath: /accounts/123/balance }
        response: { status: 200, jsonBody: { balance: 5000000 } }
    cases:
      - id: C1
        step: S1
        kind: error
        title: Thiếu accountId thì trả 400
        given: { sql: [], stubs: [] }
        when:  { method: POST, path: /api/v1/limits/check, body: { amount: 100 } }
        then:  { status: 400, body: { code: "VALIDATION_ERROR" } }
    ```
  - `EffectiveSteps.of(Flow, DesignSpec /*nullable*/, CasesSpec) → List<StepSpec>`. FULL lấy các bước của design. LITE trả đúng một bước `S1` với title "Thay đổi", scope và budget lấy từ cases.
  - `BrdValidator.validate(String content) → List<Finding>`. Bắt buộc đủ 5 heading cấp 2: `Mục tiêu`, `Trong phạm vi`, `Ngoài phạm vi`, `Quy tắc nghiệp vụ`, `Tiêu chí chấp nhận`. So khớp sau khi đưa cả nội dung lẫn heading về NFC và trim. Mã lỗi: `BRD_MISSING_SECTION`, `BRD_EMPTY_SECTION` (mục chỉ còn khoảng trắng hoặc comment `<!-- -->` của template).
  - `DesignValidator.validate(String designMd, DesignSpec) → List<Finding>`. Mã lỗi:
    - `DESIGN_NO_SEQUENCE`: không có khối ```` ```mermaid ```` mà dòng đầu không rỗng là `sequenceDiagram`.
    - `DESIGN_STEP_NOT_IN_DIAGRAM`: id bước trong yaml không xuất hiện dạng `\bS\d+\b` trong khối sequence.
    - `DESIGN_UNKNOWN_STEP_IN_DIAGRAM`: ngược lại.
    - `DESIGN_STEP_NO_SCOPE`, `DESIGN_STEP_NO_BUDGET`: budget thiếu, hoặc files ≤ 0, hoặc lines ≤ 0.
    - `DESIGN_DUP_STEP`.
    - `DESIGN_OPEN_QUESTION`: giống `BRD_OPEN_QUESTION` bên dưới.
  - **Câu hỏi mở** (spec §11.2): template `brd.md` và `design.md` có mục `## Câu hỏi mở`. Mỗi dòng `- [ ] …` là một câu chưa trả lời và sinh một finding `BRD_OPEN_QUESTION` / `DESIGN_OPEN_QUESTION` có nội dung câu hỏi. Dòng `- [x] … → <trả lời>` thì không sinh finding. Mục này không bắt buộc phải có.
    - Ghi chú: giai đoạn 1 chỉ kiểm cú pháp ở mức nhẹ như trên. Kiểm migration hợp lệ nằm ở Task 7 (`submit design` chạy migration-check).
  - `CasesValidator.validate(CasesSpec, List<StepSpec>) → List<Finding>`. Mã lỗi: `CASES_DUP_ID`, `CASES_UNKNOWN_STEP`, `CASES_STEP_UNCOVERED` (bước không có case nào), `CASES_NO_ERROR_CASE` (cả bộ không có case `error`), `CASES_UNKNOWN_STUB`, `CASES_LITE_NO_SCOPE`. Validator nhận flow qua tham số thứ ba `Flow`, nên signature cuối là `validate(CasesSpec, List<StepSpec>, Flow)`.

- [ ] **Step 1: Viết template** `brd.md` (5 heading, mỗi mục một comment hướng dẫn ngắn), `design.md` (mục Sequence chứa khối mermaid mẫu có `Note over` S1, S2; mục API; mục DB trỏ tới `db/`), `design.yaml`, `cases.full.yaml`, `cases.lite.yaml`. Template phải ngắn, mỗi file dưới 40 dòng (phản mục tiêu: không sinh tài liệu dài).
- [ ] **Step 2: Viết test thất bại:**
  - `BrdValidatorTest.templateUntouchedReportsAllSectionsEmpty`: template gốc → 5 lỗi `BRD_EMPTY_SECTION`.
  - `BrdValidatorTest.acceptsNfdHeadingsAndTrailingSpaces`: nội dung đủ mục nhưng heading ở dạng NFD (`Normalizer.normalize(.., NFD)`) và có `"## Mục tiêu   "` → không có lỗi.
  - `BrdValidatorTest.missingSection`: bỏ mục `Ngoài phạm vi` → đúng một lỗi `BRD_MISSING_SECTION`, message chứa "Ngoài phạm vi".
  - `BrdValidatorTest.openQuestionBlocks`: mục Câu hỏi mở có `- [ ] Hạn mức tính theo ngày lịch hay 24h?` → một `BRD_OPEN_QUESTION` chứa câu đó; đổi thành `- [x] … → ngày lịch` → không lỗi.
  - `DesignValidatorTest`: một test cho mỗi mã lỗi. Template hợp lệ thì không có lỗi.
  - `CasesSpecTest.parsesTemplateFull`: đọc template full → 1 stub, case C1 có `kind==ERROR`, `then.status()==400`.
  - `CasesValidatorTest`: một test cho mỗi mã lỗi, thêm `liteGetsSingleStepS1FromCasesScope` (gọi `EffectiveSteps.of`).
- [ ] **Step 3: Chạy** `mvn -q -pl aisdlc-core test` → FAIL.
- [ ] **Step 4: Cài đặt.** Đọc file với UTF-8; tách mục BRD theo dòng bắt đầu bằng `## `.
- [ ] **Step 5: Chạy lại** → PASS.
- [ ] **Step 6: Commit** `feat(core): spec formats and validators`.

### Task 4: Cổng duyệt

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/gate/{Gate,GateState,Approval,Submission,GateStore,LocalGateStore,GateService,GateException}.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/gate/GateServiceTest.java`

**Interfaces:**
- Consumes: `FeaturePaths`, `Flow`, `Hashing`, `Finding` (Task 1, 3).
- Produces:
  - `enum Gate { BRD, DESIGN, CASES, REVIEW }` với:
    - `static List<Gate> sequence(Flow)`: FULL → `[BRD, DESIGN, CASES, REVIEW]`, LITE → `[CASES, REVIEW]`.
    - `List<Path> artifacts(FeaturePaths p, Flow f, RepoConfig c)`:
      - BRD → `brd.md`
      - DESIGN → `design.md`, `design.yaml`, mọi file trong `db/` (sắp theo tên), `schema-expected.json`
      - CASES → `cases.yaml` và file lớp test suite (tìm theo glob `c.casesTestPath()`); riêng LITE thêm `schema-expected.json`
      - REVIEW → `eval/report.json`, `review-agent.json`
  - `enum GateState { LOCKED_BY_PREVIOUS, NOT_SUBMITTED, SUBMITTED_WITH_FINDINGS, AWAITING_APPROVAL, APPROVED, STALE }`
  - `record Submission(Gate gate, String hash, Instant at, List<Finding> findings)`; `record Approval(Gate gate, String hash, String approver, Instant at)`.
  - `interface GateStore`: `Optional<Submission> submission(Gate)`, `void saveSubmission(Submission)`, `Optional<Approval> approval(Gate)`, `void saveApproval(Approval)`, `void clear(Gate)` (xóa cả submission lẫn approval).
  - `LocalGateStore(FeaturePaths)`: lưu vào `gates.json` (Jackson, ghi atomic qua file tạm rồi move).
  - `GateService(FeaturePaths, Flow, RepoConfig, GateStore, Clock)`:
    - `GateState state(Gate)`
    - `boolean isOpen(Gate)`: APPROVED, đồng thời mọi cổng trước nó trong sequence cũng open.
    - `boolean codeReady()`: FULL thì DESIGN và CASES open; LITE thì CASES open.
    - `Submission submit(Gate, List<Finding>)`: ném `GateException` nếu cổng trước chưa open.
    - `Approval approve(Gate, String approver)`: ném lỗi nếu không có submission, hoặc submission có findings, hoặc hash hiện tại khác hash submission.
    - `void reject(Gate, String approver, String comment)`: nối vào `feedback.md` khối `## <GATE> — trả lại bởi <approver> lúc <ISO time>` cộng comment, rồi `clear(gate)`.
    - `void reopen(Gate, String approver)`: `clear(gate)` và ghi một dòng vào feedback.
    - `Optional<Gate> firstNotOpen()`.
  - Quy tắc state, xét theo thứ tự: có cổng trước chưa open → `LOCKED_BY_PREVIOUS`; có approval mà hash hiện tại khác → `STALE`; có approval → `APPROVED`; có submission có findings → `SUBMITTED_WITH_FINDINGS`; có submission mà hash hiện tại ≠ hash submission → `NOT_SUBMITTED`; có submission → `AWAITING_APPROVAL`; còn lại `NOT_SUBMITTED`.

- [ ] **Step 1: Viết test thất bại** (dùng `Clock.fixed`):
  - `fullFlowOrderEnforced`: `submit(DESIGN, [])` khi BRD chưa duyệt → `GateException`.
  - `approveRequiresCleanSubmission`: submit BRD có 1 finding → `approve` ném lỗi; submit lại sạch → approve ok, state APPROVED.
  - `approveRejectsIfChangedSinceSubmit`: submit, sửa brd.md, approve → ném lỗi.
  - `editAfterApprovalMakesStaleAndClosesDownstream`: duyệt BRD, DESIGN, CASES; sửa brd.md → `state(BRD)==STALE`, `state(DESIGN)==LOCKED_BY_PREVIOUS`, `codeReady()==false`. (Review Focus 5)
  - `liteFlowStartsAtCases`: LITE → `submit(CASES, [])` không cần BRD; `codeReady()` true sau khi duyệt CASES.
  - `rejectWritesFeedbackAndClears`: feedback.md chứa `## CASES — trả lại bởi dev1` và comment; state về NOT_SUBMITTED.
  - `gatesJsonSurvivesReload`: `LocalGateStore` mới đọc lại đúng approval.
- [ ] **Step 2: Chạy** → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy lại** → PASS.
- [ ] **Step 5: Commit** `feat(core): gate service with stale detection`.

### Task 5: Bước code, phạm vi, ngân sách

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/step/{StepStatus,StepRecord,StepCheck,StepService,ScopeMatcher,ScopeExtras,StepException}.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/run/{CaseOutcome,CaseResult,CaseRunResult,SchemaSnapshot}.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/step/{ScopeMatcherTest,StepServiceTest}.java`

**Interfaces:**
- Consumes: `GitRepo`, `FileChange` (Task 2), `StepSpec`, `Budget` (Task 3), `GateService.codeReady()` (Task 4), `RepoConfig`.
- Produces:
  - `enum CaseOutcome { PASSED, FAILED, ERROR }`; `record CaseResult(String id, String step, CaseOutcome outcome, String message)`; `record CaseRunResult(int exitCode, List<CaseResult> cases)` với `long passed()` và `List<String> notPassedIds()`.
  - `record SchemaSnapshot(Map<String, List<String>> tables)`: tên bảng viết thường → danh sách `"column:TYPE:NULL|NOT NULL"` đã sắp xếp. Có `static read(Path)`, `write(Path)`, `List<String> diff(SchemaSnapshot expected)` trả các dòng `+table.col …` hoặc `-table.col …`.
  - `ScopeMatcher.matches(List<String> globs, String relPath) → boolean`: dùng `FileSystems.getDefault().getPathMatcher("glob:"+g)`. `**/` khớp cả số thư mục bằng 0, nên mỗi glob được thử thêm biến thể đã bỏ `**/`.
  - `ScopeExtras(FeaturePaths)`: `List<String> paths()`, `void add(String relPath, String approver, Instant at)`. Lưu trong `scope-extra.json`.
  - `enum StepStatus { NOT_STARTED, IN_PROGRESS, AWAITING_DEV, ACCEPTED }`
  - `record StepCheck(long casesPassed, List<String> casesNotPassed, int files, int lines, Budget budget, boolean overBudget, List<String> outOfScope)`
  - `record StepRecord(String id, StepStatus status, String startTree, String endTree, List<FileChange> changes, StepCheck check, String acceptedBy)`
  - `StepService(FeaturePaths, GitRepo, List<StepSpec>, RepoConfig, ScopeExtras, GateService, Clock)`:
    - `StepRecord start(String id)`. Ném `StepException` khi: `!codeReady()`; id không tồn tại; có bước khác đang IN_PROGRESS hoặc AWAITING_DEV; bước liền trước chưa ACCEPTED. Hàm ghi `startTree = git.snapshotTree()`.
    - `StepRecord finish(String id, CaseRunResult run)`. Bước phải đang IN_PROGRESS. Hàm ghi `endTree`, `changes = diff(startTree,endTree)` (bỏ qua file dưới `aisdlc/features/<KEY>/` và file khớp `unmanaged`), tính check, rồi chuyển sang AWAITING_DEV.
    - `StepRecord accept(String id, String approver)`. Bước phải đang AWAITING_DEV.
    - `Optional<StepRecord> current()`: bản ghi đang IN_PROGRESS hoặc AWAITING_DEV.
    - `List<StepRecord> all()`.
    - `List<String> allowedGlobs(String stepId)` = scope của bước ∪ `alwaysAllowed` ∪ `scopeExtras.paths()`.
    - Lưu trong `steps.json`.
  - Quy tắc của check:
    - `overBudget` = `files > budget.files*factor || lines > budget.lines*factor`.
    - `outOfScope` = các path trong changes (với RENAMED xét cả path mới) không khớp `allowedGlobs`.
    - Danh sách case chạy là các case thuộc bước ≤ bước hiện tại, theo thứ tự khai báo. Việc chạy do CLI gọi `MavenRunner` (Task 7).

- [ ] **Step 1: Viết test thất bại:**
  - `ScopeMatcherTest.doubleStarMatchesZeroDirs`: `src/main/java/**/limit/**` khớp `src/main/java/limit/A.java` và `src/main/java/vn/x/limit/web/B.java`; không khớp `src/main/java/vn/x/other/C.java`.
  - `StepServiceTest.cannotStartBeforeCodeReady`.
  - `StepServiceTest.cannotStartS2BeforeS1Accepted`: sau `finish(S1)`, `start(S2)` ném lỗi; sau `accept(S1)` thì ok.
  - `StepServiceTest.finishCountsUntrackedDeletedRenamed`: trong S1 tạo file mới chưa track (5 dòng), xóa một file đã commit (3 dòng), đổi tên một file → `files==3`, `lines==8 + lines của rename`. (Review Focus 3)
  - `StepServiceTest.overBudgetUsesFactor`: budget lines 10, factor 1.5: 15 dòng → false, 16 → true.
  - `StepServiceTest.outOfScopeListsPathAndExtrasWhitelist`: sửa `pom.xml` ngoài scope → nằm trong outOfScope; sau `ScopeExtras.add("pom.xml",…)` thì không còn.
  - `StepServiceTest.featureDirChangesIgnored`.
- [ ] **Step 2: Chạy** → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy lại** → PASS.
- [ ] **Step 5: Commit** `feat(core): step workflow with scope and budget`.

### Task 6: Chính sách hook (PreToolUse, Stop, SessionStart)

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/policy/{ToolCall,Decision,HookPolicy,BashHeuristics,PolicyState}.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/policy/{HookPolicyTest,BashHeuristicsTest}.java`

**Interfaces:**
- Consumes: `GateService`, `StepService`, `FeaturePaths`, `RepoConfig`, `ScopeMatcher`, `Gate.artifacts`.
- Produces:
  - `record ToolCall(String toolName, String filePath /*nullable*/, String command /*nullable*/)`
  - `record Decision(boolean allow, String reason)` với `static allow()` và `static deny(String)`.
  - `record PolicyState(Path repoRoot, RepoConfig config, Optional<FeatureKey> active, GateService gates /*null nếu không active*/, StepService steps /*null nếu không active*/, FeaturePaths paths)`
  - `HookPolicy.preToolUse(ToolCall, PolicyState) → Decision`. Áp dụng theo thứ tự, luật đầu tiên khớp sẽ quyết định:
    1. Tool không phải `Edit|Write|MultiEdit|NotebookEdit|Bash` → allow.
    2. Bash → `BashHeuristics.check(command, protectedPaths, repoRoot)`; có vi phạm → deny; không vi phạm → allow (giai đoạn 1 không suy luận được Bash ghi vào code sản phẩm; phần này do eval phát hiện qua SCOPE).
    3. Path nằm ngoài repoRoot → allow.
    4. Path thuộc `.aisdlc/**` hoặc là `gates.json`, `steps.json`, `scope-extra.json`, `feedback.md` của feature → deny "File trạng thái do CLI quản lý…".
    5. Path là artifact của một cổng có approval (APPROVED hoặc STALE) → deny "<file> đã được duyệt ở cổng <GATE>; nếu cần sửa, dev chạy `aisdlc reopen <gate>`".
    6. Path dưới `aisdlc/features/<active>/` → allow.
    7. Path khớp `unmanaged` → allow.
    8. Không có feature active → deny "Chưa có tính năng đang làm. Chạy /aisdlc:init <JIRA-KEY>".
    9. `!codeReady()` → deny, nêu cổng đầu tiên chưa mở.
    10. Không có bước IN_PROGRESS → deny. Nếu đang AWAITING_DEV thì nêu "đang chờ dev `aisdlc step accept Sx`"; ngược lại "chạy `aisdlc step start Sx`".
    11. Path không khớp `allowedGlobs(current)` → deny "Ngoài phạm vi Sx (<globs>). Dev có thể chạy `aisdlc scope allow <path>`".
    12. allow.
  - `protectedPaths` = các path ở luật 4 cộng artifact của các cổng đã có approval (luật 5), ở cả dạng tương đối lẫn tuyệt đối.
  - `BashHeuristics.check(String command, List<String> protectedPaths, Path repoRoot) → Optional<String>`. Trả lý do nếu:
    - (a) lệnh khớp `\baisdlc\s+(approve|reject|reopen|scope\s+allow|step\s+accept)\b`, hoặc
    - (b) lệnh nhắc tới một protected path **và** khớp regex ghi: `>|>>|\btee\b|\bsed\s+-i|\brm\b|\bmv\b|\bcp\b|\btruncate\b|\bgit\s+(checkout|restore|stash|reset)\b|\bperl\b|\bpython3?\b|\bdd\b`.
  - `HookPolicy.stop(boolean stopHookActive, PolicyState) → Decision`: `stopHookActive` true, hoặc không có bước IN_PROGRESS → allow. Ngược lại deny "Bước Sx chưa kết thúc. Chạy `aisdlc step finish Sx` và báo kết quả cho dev trước khi dừng."
  - `HookPolicy.sessionContext(PolicyState) → String`: quy tắc AISDLC (≤ 15 dòng) cộng trạng thái hiện tại (feature, cổng, bước). Không có feature active thì chỉ có quy tắc.

- [ ] **Step 1: Viết test thất bại** (`HookPolicyTest` dựng repo tạm có feature thật, dùng GateService/StepService thật):
  - Một test cho mỗi luật 4–11, kiểm cả allow/deny lẫn chuỗi con chính trong reason.
  - `staleBrdBlocksCodeWrites`: sau khi sửa brd.md đã duyệt, Write vào `src/main/java/…` → deny có nhắc `BRD`. (Review Focus 5)
  - `BashHeuristicsTest` (Review Focus 2), mỗi lệnh sau → vi phạm:
    `cat > aisdlc/features/PAY-1/cases.yaml <<EOF`, `sed -i 's/400/200/' aisdlc/features/PAY-1/cases.yaml`, `git checkout -- aisdlc/features/PAY-1/cases.yaml`, `rm /abs/repo/aisdlc/features/PAY-1/gates.json`, `aisdlc approve cases`, `aisdlc step accept S1`.
    Còn các lệnh sau → không vi phạm: `cat aisdlc/features/PAY-1/cases.yaml`, `grep -n C1 aisdlc/features/PAY-1/cases.yaml`, `aisdlc step finish S1`, `mvn -q test`.
  - `stopBlocksWhileStepInProgress` và `stopAllowsWhenStopHookActive`.
- [ ] **Step 2: Chạy** → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy lại** → PASS.
- [ ] **Step 5: Commit** `feat(core): hook policy`.

### Task 7: CLI `aisdlc` (init, setup, status, submit, duyệt, step, hook)

**Files:**
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/{Main,DevConsole,SystemDevConsole,RepoLocator,Context}.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/{InitCommand,UseCommand,SetupCommand,StatusCommand,SubmitCommand,ApproveCommand,RejectCommand,ReopenCommand,StepCommand,ScopeCommand,DiffCommand}.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/hook/HookCommand.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/run/MavenRunner.java`
- Create: `aisdlc-cli/src/main/resources/templates/{config.yaml,AisdlcCasesTest.java.tpl,gitlab-ci.aisdlc.yml,CLAUDE.aisdlc.md}`
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/{CliFlowTest,HookCommandTest,DevOnlyCommandTest}.java`

**Interfaces:**
- Consumes: mọi thứ từ Task 1–6.
- Produces (CLI, các task sau và plugin dựa vào):
  - `interface DevConsole { Optional<String> readLine(String prompt); }`. `SystemDevConsole` trả `Optional.empty()` khi `System.console()==null`. `Main.run(String[] args, InputStream in, PrintStream out, PrintStream err, DevConsole console, Path cwd) → int` là điểm vào để test.
  - Exit code: `0` ok, `1` có findings hoặc kiểm tra thất bại, `2` sai trạng thái hoặc sai cú pháp, `3` không phải dev (không có console hoặc gõ sai key).
  - `RepoLocator.find(Path cwd) → Optional<Path>`: thư mục cha gần nhất chứa `aisdlc/config.yaml`.
  - `MavenRunner(Path repoRoot, RepoConfig)`: `CaseRunResult runCases(FeatureKey, List<String> steps, String mode)` và `int verify(FeatureKey, Path outDir)`. Lệnh: `<mavenCmd> -q -Dtest=<casesTestClass> -Dsurefire.failIfNoSpecifiedTests=false -Daisdlc.feature=<KEY> -Daisdlc.steps=<csv> -Daisdlc.mode=<cases|migration-check> -Daisdlc.out=<abs> -Daisdlc.repoRoot=<abs> test`. Kết quả đọc từ `<out>/results.json` (Task 8). Thiếu file thì mọi case là ERROR, message "harness không ghi kết quả". `verify` chạy `<mavenCmd> -q verify` kèm cùng các `-Daisdlc.*` (không có `-Dtest`).
  - Các lệnh:

| Lệnh | Hành vi |
|---|---|
| `aisdlc setup --package <pkg>` | Tạo `aisdlc/config.yaml`, `src/test/java/<pkg path>/AisdlcCasesTest.java`, `aisdlc/gitlab-ci.aisdlc.yml`; nối `CLAUDE.aisdlc.md` vào `CLAUDE.md` (bỏ qua nếu đã có marker `<!-- aisdlc -->`); thêm `.aisdlc/` vào `.gitignore`. In hướng dẫn thêm dependency `com.aisdlc:aisdlc-junit` và JaCoCo. Chạy lại lần nữa không đổi gì. |
| `aisdlc init <KEY> [--lite] [--title T]` | Tạo thư mục feature từ template (FULL: brd, design.md, design.yaml, db/, cases.full→cases.yaml; LITE: cases.lite→cases.yaml); ghi `feature.yaml` với `baseTree = git.snapshotTree()`; ghi active. Nếu có env `JIRA_URL` và `JIRA_TOKEN`: `GET <JIRA_URL>/rest/api/2/issue/<KEY>?fields=summary`, 404 → exit 2 "Không tìm thấy ticket", ok → title = summary. Không có env → in cảnh báo bỏ qua kiểm tra Jira. Feature đã tồn tại → exit 2. |
| `aisdlc use <KEY>` | Đổi active. |
| `aisdlc status [--json]` | Bảng cổng (state) và bước (status + check tóm tắt). JSON dùng cho web ở giai đoạn 2. |
| `aisdlc submit brd` | `BrdValidator` → `GateService.submit`. In findings. Exit 1 nếu có. |
| `aisdlc submit design` | `DesignValidator`; nếu sạch thì `runCases(key, [], "migration-check")`. Thất bại (exitCode≠0 hoặc thiếu schema.json) → finding `DESIGN_MIGRATION_INVALID` kèm 20 dòng cuối output Maven. Thành công → copy `schema.json` thành `schema-expected.json`, rồi submit. |
| `aisdlc submit cases` | `CasesValidator`; LITE thì chạy migration-check trước để tạo `schema-expected.json`; sau đó `runCases(key, [], "cases")`. Bất kỳ case nào ERROR → finding `CASES_HARNESS_ERROR`. Không có case FAILED nào → finding `CASES_NONE_FAILING` ("mọi case đã pass trước khi code: case không kiểm được thay đổi"). Các case PASSED được liệt kê là thông tin, không phải lỗi. |
| `aisdlc submit review` | Yêu cầu `eval/report.json` có `passed=true` và `review-agent.json` hợp lệ (Task 10). Nếu không → finding. |
| `aisdlc approve <gate>` / `reject <gate> -m <text>` / `reopen <gate>` | **Chỉ dev**: in tóm tắt (findings, hash, các file), yêu cầu gõ đúng feature key qua `DevConsole`. Không có console hoặc gõ sai → exit 3. Approver = `git config user.name`. |
| `aisdlc step start <Sx>` | `StepService.start`; in scope, budget, case của bước. |
| `aisdlc step finish <Sx>` | `runCases(key, steps ≤ Sx, "cases")` → `StepService.finish` → in tóm tắt: `Case: 7/9 pass (C4, C8 fail) · 3 file / 140 dòng (ngân sách 3/120 → vượt) · Ngoài phạm vi: pom.xml`. Kết thúc bằng dòng "Dừng lại. Dev xem rồi chạy `aisdlc step accept Sx` ở terminal của mình." |
| `aisdlc step accept <Sx>` | **Chỉ dev**. |
| `aisdlc scope allow <path>` | **Chỉ dev**. Ghi vào `ScopeExtras`. |
| `aisdlc diff --by-step [--patch]` | Với mỗi StepRecord: header `## Sx — <title>` và numstat; với `--patch` thì thêm `git diff startTree endTree`. Ghi ra stdout. |
| `aisdlc hook session-start|pre-tool-use|stop` | Đọc JSON từ stdin, đọc `cwd` trong JSON, gọi `HookPolicy`. Định dạng output ở dưới. |

  - Hook I/O (executor đối chiếu với tài liệu hooks của phiên bản Claude Code đang dùng trước khi cài; có thể hỏi agent `claude-code-guide`):
    - Input có `hook_event_name`, `cwd`, `tool_name`, `tool_input.file_path` / `tool_input.notebook_path` / `tool_input.command`, `stop_hook_active`.
    - PreToolUse deny: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"<reason>"}}`, exit 0. Allow: không in gì, exit 0.
    - Stop block: `{"decision":"block","reason":"<reason>"}`.
    - SessionStart: `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"<text>"}}`.
    - Không tìm thấy repo AISDLC → không in gì, exit 0, với mọi sự kiện.
    - Repo AISDLC mà JSON hỏng hoặc có exception: PreToolUse với tool ghi (hoặc tool_name không đọc được) → deny "aisdlc lỗi: <msg>" (đóng); các sự kiện khác → không in gì, exit 0.
  - `AisdlcCasesTest.java.tpl`:
    ```java
    package ${package};
    @org.springframework.boot.test.context.SpringBootTest(webEnvironment = org.springframework.boot.test.context.SpringBootTest.WebEnvironment.RANDOM_PORT)
    class AisdlcCasesTest extends com.aisdlc.junit.AisdlcCaseSuite {}
    ```
    Repo dịch vụ tự cấu hình DB Testcontainers cho profile test của mình. README hướng dẫn cách làm.
  - `CLAUDE.aisdlc.md`: ≤ 20 dòng gồm quy tắc của spec §5 và tên các slash command. Bắt đầu bằng `<!-- aisdlc -->`.
  - `gitlab-ci.aisdlc.yml`: job `aisdlc-eval` (stage test, image Maven có JDK 17, service docker:dind) chạy `aisdlc eval --ci --base origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME`, artifacts `aisdlc/features/*/eval/`.

- [ ] **Step 1: Viết test thất bại:**
  - `CliFlowTest.fullFlowHappyPathWithoutMaven`: thay `MavenRunner` bằng fake qua `Context` (ghi results/schema giả). Chạy lần lượt setup → init PAY-1 → điền brd hợp lệ → submit brd (0) → approve brd với `FakeDevConsole("PAY-1")` (0) → … → step start S1 → finish S1 → status `--json` có `"S1":"AWAITING_DEV"`.
  - `CliFlowTest.submitCasesRequiresAtLeastOneFailing`: fake trả toàn PASSED → exit 1, output chứa `CASES_NONE_FAILING`.
  - `CliFlowTest.setupIsIdempotent`: chạy hai lần, `CLAUDE.md` chỉ có một marker.
  - `CliFlowTest.initWithoutJiraEnvWarns`.
  - `DevOnlyCommandTest.approveWithoutConsoleExits3`: `DevConsole` rỗng → 3, gates.json không đổi. `approveWithWrongKeyExits3`. Tương tự cho `step accept` và `scope allow`.
  - `HookCommandTest.noAisdlcRepoIsSilent`: cwd là thư mục tạm không có config, stdin là Write → stdout rỗng, exit 0. (Review Focus 4)
  - `HookCommandTest.malformedJsonInAisdlcRepoDeniesWrites`: stdin `{"tool_name":"Write",` → stdout chứa `"permissionDecision":"deny"` và `aisdlc lỗi`. (Review Focus 4)
  - `HookCommandTest.denyJsonShape`: parse output bằng Jackson, kiểm 3 trường.
  - `HookCommandTest.sessionStartIncludesStatus`.
- [ ] **Step 2: Chạy** `mvn -q -pl aisdlc-cli -am test` → FAIL.
- [ ] **Step 3: Cài đặt**: picocli và `Context` (factory cho GitRepo, MavenRunner, Clock, DevConsole để test thay được). Shade thành `aisdlc-cli/target/aisdlc-cli.jar` với `Main-Class: com.aisdlc.cli.Main`.
- [ ] **Step 4: Chạy lại** → PASS. `java -jar aisdlc-cli/target/aisdlc-cli.jar --help` in danh sách lệnh.
- [ ] **Step 5: Đo thời gian khởi động**: `time (echo '{"tool_name":"Read","cwd":"/tmp"}' | java -jar aisdlc-cli.jar hook pre-tool-use)` phải dưới 1 giây trên máy dev. Nếu vượt, bật `-XX:TieredStopAtLevel=1 -Xshare:auto` trong wrapper (Task 11) và ghi số đo vào commit message.
- [ ] **Step 6: Commit** `feat(cli): aisdlc command line and hooks`.

### Task 8: Harness JUnit (HTTP, DB, WireMock, schema, kết quả)

**Files:**
- Create: `aisdlc-junit/src/main/java/com/aisdlc/junit/{AisdlcCaseSuite,HarnessOptions,CaseExecutor,HttpCaller,DbChecker,StubRegistry,MessageProbe,SchemaDumper,ResultWriter}.java`
- Test: `aisdlc-junit/src/test/java/com/aisdlc/junit/sample/{SampleApp,TransferController,schema in resources/db/migration/V1__init.sql}` và `aisdlc-junit/src/test/java/com/aisdlc/junit/{SampleCasesTest,CaseExecutorTest,SchemaDumperTest}.java`, cùng `aisdlc-junit/src/test/resources/aisdlc/features/DEMO-1/cases.yaml`

**Interfaces:**
- Consumes: `CasesSpec`, `CaseSpec`, `StubSpec`, `CaseResult`, `CaseOutcome`, `SchemaSnapshot` từ `aisdlc-core` (dependency compile).
- Produces:
  - Hợp đồng system property (CLI ở Task 7 dựa vào): `aisdlc.feature` (bắt buộc; thiếu thì suite trả về 0 test, nhờ vậy `mvn test` thường không bị ảnh hưởng), `aisdlc.steps` (csv; rỗng = tất cả), `aisdlc.mode` (`cases` mặc định, hoặc `migration-check`), `aisdlc.out` (mặc định `target/aisdlc`), `aisdlc.repoRoot` (mặc định `user.dir`).
  - `<out>/results.json`: `{"feature":"DEMO-1","mode":"cases","cases":[{"id":"C1","step":"S1","outcome":"PASSED","message":""}]}`. `<out>/schema.json` = `SchemaSnapshot`.
  - `abstract class AisdlcCaseSuite`:
    - `@LocalServerPort int port`, `@Autowired DataSource dataSource`.
    - `@DynamicPropertySource static void aisdlcProperties(DynamicPropertyRegistry r)` đăng ký `aisdlc.wiremock.url` (WireMock static, cổng ngẫu nhiên). Khi mode = migration-check thì đăng ký thêm `spring.flyway.locations` = `classpath:db/migration,filesystem:<repoRoot>/aisdlc/features/<KEY>/db`.
    - `@TestFactory Stream<DynamicTest> aisdlcCases()`: mode cases → mỗi case được chọn là một DynamicTest tên `"<id> [<step>] <title>"`, cộng một DynamicTest cuối `schema-dump`; mode migration-check → chỉ `schema-dump`.
    - `@AfterAll static` → `ResultWriter`.
    - Một case FAILED hoặc ERROR làm DynamicTest đó fail (để Maven và CI thấy). Kết quả vẫn được ghi đầy đủ.
  - `CaseExecutor(HttpCaller, DbChecker, StubRegistry, MessageProbe, List<String> resetSql)`: `CaseResult execute(CaseSpec)`. Thứ tự: chạy resetSql → `given.sql` → reset WireMock và đăng ký `given.stubs` → gọi HTTP → so status → so body (JSONAssert, `strict=false`, chỉ khi `then.body` khác null) → so db → so messages. Lần so sánh đầu tiên lệch → FAILED, message dạng `status: mong đợi 400, nhận 200` hoặc `body: <diff JSONAssert>` hoặc `db[0]: …`. Exception khi chuẩn bị (SQL lỗi, stub lỗi) → ERROR.
  - `HttpCaller(int port)`: `Response call(When)` dùng `java.net.http.HttpClient`, body serialize JSON, mặc định `Content-Type: application/json`.
  - `DbChecker(DataSource)`: `Optional<String> check(DbExpect)` so từng dòng: tên cột không phân biệt hoa thường, giá trị so qua `String.valueOf`, thứ tự dòng phải khớp.
  - `StubRegistry(WireMockServer)`: `register(List<StubSpec> all, List<String> ids)` dùng `StubMapping.buildFrom(json)`.
  - `interface MessageProbe { void reset(); Optional<String> check(List<MessageExpect>); }`. Cài đặt mặc định `NoMessageProbe`: nếu có expect thì trả ERROR "chưa cấu hình Kafka".
  - `SchemaDumper.dump(DataSource) → SchemaSnapshot`: dùng `DatabaseMetaData.getTables(null, currentSchema, "%", {"TABLE"})` và `getColumns`; bỏ bảng `flyway_schema_history`.

- [ ] **Step 1: Tạo app mẫu** trong src/test: `POST /api/transfers` lưu vào bảng `transfer`, gọi `GET ${aisdlc.wiremock.url}/accounts/{id}/balance`, trả 422 nếu số dư không đủ. DB dùng Postgres 16 qua Testcontainers (`@ServiceConnection`).
- [ ] **Step 2: Viết test thất bại:**
  - `SampleCasesTest extends AisdlcCaseSuite` chạy với `-Daisdlc.feature=DEMO-1`. Bộ case DEMO-1 có 3 case pass (happy với kiểm db, error 422 dùng stub, boundary) và 1 case cố ý sai `C9` (mong đợi 201, thực tế 200). Test riêng `ResultsFileTest` đọc results.json sau suite (đặt ở module test phụ hoặc gọi suite qua `EngineTestKit`): C1–C3 PASSED, C9 FAILED với message chứa `mong đợi 201`. Dùng `org.junit.platform.testkit.engine.EngineTestKit` để chạy `SampleCasesTest` và kiểm cả số test fail (=1).
  - `CaseExecutorTest.unknownSqlTableIsError`: given.sql lỗi → ERROR.
  - `CaseExecutorTest.bodyIsLenientSubset`: then.body là tập con của response → PASSED.
  - `SchemaDumperTest.excludesFlywayHistory` và `migrationCheckIncludesDrafts`: chạy với mode migration-check, có thêm draft `V2__add_note.sql` trong thư mục feature test → schema.json có `transfer.note`.
  - `noFeaturePropertyMeansNoTests`.
- [ ] **Step 3: Chạy** `mvn -q -pl aisdlc-junit -am test` → FAIL.
- [ ] **Step 4: Cài đặt.**
- [ ] **Step 5: Chạy lại** → PASS (cần Docker).
- [ ] **Step 6: Commit** `feat(junit): case harness for spring boot`.

### Task 9: Kiểm message Kafka trong harness

**Files:**
- Create: `aisdlc-junit/src/main/java/com/aisdlc/junit/KafkaMessageProbe.java`
- Modify: `aisdlc-junit/pom.xml`: `spring-kafka` scope `provided` và optional.
- Modify: `AisdlcCaseSuite`: chọn `KafkaMessageProbe` khi bean `KafkaTemplate`/`ConsumerFactory` có trong context, ngược lại dùng `NoMessageProbe`.
- Test: `aisdlc-junit/src/test/java/com/aisdlc/junit/kafka/KafkaCasesTest.java` (Testcontainers Kafka `apache/kafka:3.8.0`)

**Interfaces:**
- Consumes: `MessageProbe`, `MessageExpect` (Task 3, 8).
- Produces: `KafkaMessageProbe(ConsumerFactory<String,String>, Duration timeout /*mặc định 5s*/)`.
  - `reset()` đăng ký consumer group mới (`aisdlc-<uuid>`, `auto.offset.reset=latest`) cho mọi topic xuất hiện trong bộ case, rồi chờ có partition assignment.
  - `check()` poll tới khi mỗi expect có ít nhất một message mà value (JSON) chứa `value` theo kiểu JSONAssert lenient, hoặc hết thời gian. Hết thời gian → `"messages[i]: không nhận được message khớp trên topic <t> trong 5s"`.

- [ ] **Step 1: Viết test thất bại:** app mẫu publish `transfer.created` khi tạo giao dịch. Case có `messages: [{topic: transfer.created, value: {status: "CREATED"}}]` → PASSED. Case mong đợi `status: "DONE"` → FAILED với message chứa `transfer.created`.
- [ ] **Step 2: Chạy** → FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy lại** → PASS. Chạy lại cả test Task 8 để bảo đảm app không có Kafka vẫn pass.
- [ ] **Step 5: Commit** `feat(junit): kafka message expectations`.

### Task 10: Eval tầng 1 và báo cáo

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/eval/{HardCheck,SoftCheck,EvalReport,EvalService,ChangedLines,JacocoCoverage,AgentReview,ReportMarkdown,SurefireReports}.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/EvalCommand.java`
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/eval/{EvalServiceTest,JacocoCoverageTest,AgentReviewTest,SurefireReportsTest}.java`, fixture XML trong `src/test/resources/eval/`

**Interfaces:**
- Consumes: `MavenRunner.verify`, `GitRepo.diff/changedLines/mergeBaseTree`, `GateService`, `StepService.all()`, `SchemaSnapshot`, `ScopeMatcher`, `ScopeExtras`, `RepoConfig.checks`.
- Produces:
  - `record HardCheck(String id, boolean passed, String detail)`; `record SoftCheck(String id, String level /*OK|WARN|NA*/, String detail)`; `record EvalReport(String feature, Instant at, String mode, List<HardCheck> hard, List<SoftCheck> soft, boolean passed)`. `passed` = mọi hard check đều passed.
  - `EvalService(...)`: `EvalReport run(boolean ci, Optional<String> baseRef)`. Hàm ghi `eval/report.json`, `eval/report.md`, `eval/diff-by-step.md`.
  - Hard check ids, đúng spec §6:
    - `CASES_ALL_PASS`: mọi case trong results.json PASSED.
    - `CASES_LOCKED`: hash artifact CASES hiện tại == hash approval. Ở CI kết quả này chuyển thành soft check `CASES_LOCKED` mức WARN, không phải hard.
    - `BUILD_AND_TESTS`: Maven `verify` exit 0, **hoặc** chỉ có các test thuộc `casesTestClass` fail. Đọc từ `**/target/surefire-reports/TEST-*.xml`, detail liệt kê test cũ bị fail.
    - `SCHEMA_MATCH`: `schema.json` so với `schema-expected.json`; lệch thì detail là `diff()`.
    - `SCOPE`: file thay đổi từ base (local: `feature.baseTree`; CI: `mergeBaseTree(baseRef)`), trừ thư mục feature và `unmanaged`, phải khớp hợp của mọi scope bước ∪ alwaysAllowed ∪ extras.
    - `CHECK:<name>`: mỗi `RepoConfig.checks` chạy `sh -c <cmd>` tại repoRoot; exit 0 là passed. Giai đoạn 1 lấy "không có lỗi mới" theo nghĩa lệnh đó pass. Repo nào có lỗi tồn đọng thì cấu hình lệnh có baseline của riêng công cụ đó.
  - Soft check ids:
    - `BUDGET:<Sx>`: từ StepRecord.check; WARN nếu overBudget.
    - `COVERAGE_CHANGED_LINES`: JaCoCo ở `**/target/site/jacoco/jacoco.xml`. NA nếu không có file. WARN nếu < `coverageWarn`. Detail dạng `"73% (146/200 dòng mới/sửa)"`.
    - `AGENT_REVIEW`: từ `review-agent.json`. NA nếu chưa có. WARN nếu có tiêu chí `NGHIEM_TRONG` hoặc issue `severity: high`. Detail dạng `"4/5 ĐẠT, 1 CẦN_SỬA (error-handling); 0 high, 2 medium"`.
  - `ChangedLines`: chỉ xét file `src/main/java/**.java`. `JacocoCoverage.parse(Path xml) → Map<String /*pkg/File.java*/, Map<Integer, Boolean /*covered*/>>`. Map sang path repo bằng hậu tố `src/main/java/<pkg>/<File>.java`. Dòng không có trong jacoco (comment, dòng trống) không tính.
  - `AgentReview.read(Path) → AgentReview` với `record AgentReview(String summary, List<Criterion> criteria, List<Issue> issues)`, `record Criterion(String id, String label, String reason)` và `record Issue(String step, String file, int line, String severity, String category, String message)` (spec §11.3: nhãn thay cho thang điểm).
    - Hợp lệ khi `criteria` có **đúng** 5 id `sequence, error-handling, over-engineering, naming, security`, mỗi id một lần; `label ∈ {DAT, CAN_SUA, NGHIEM_TRONG}`; `reason` không rỗng; `severity ∈ {high,medium,low}`; `category` thuộc 5 id trên hoặc `other`.
    - Không hợp lệ → `IllegalArgumentException` với message tiếng Việt.
    - `int passedCount()` = số tiêu chí `DAT`.
  - `report.md` ≤ 40 dòng: dòng đầu `Eval PAY-123: ĐẠT` hoặc `KHÔNG ĐẠT`, bảng hard check, bảng soft check, rồi danh sách case không pass. Nội dung này dùng làm mô tả MR.
  - CLI: `aisdlc eval [--ci] [--base <ref>]`, exit 0 nếu passed, 1 nếu không. `--ci` không cần feature active: nó tìm feature theo mọi thư mục trong `aisdlc/features/` có thay đổi trong diff từ base, rồi eval từng cái.

- [ ] **Step 1: Viết test thất bại** (`EvalServiceTest` dùng fake MavenRunner và fixture files):
  - `allGreenPasses`.
  - `failingCaseFailsCasesAllPass`.
  - `editedCasesFailsLockLocallyButWarnsInCi`.
  - `oldTestFailureFailsBuild` và `onlySuiteFailureDoesNotFailBuild` (fixture surefire XML).
  - `extraColumnFailsSchemaMatch`: detail chứa `+transfer.note`.
  - `bashWrittenOutOfScopeFileFailsScope`: file nằm ngoài mọi scope, tạo trực tiếp trên đĩa không qua hook → SCOPE fail, detail có path.
  - `checkCommandNonZeroFails`: `checks: [{name: lint, cmd: "exit 3"}]` → `CHECK:lint` fail.
  - `JacocoCoverageTest.mapsChangedLinesToCoverage`: fixture jacoco.xml cộng changed lines → 2/3 = 67% → WARN.
  - `AgentReviewTest.rejectsUnknownSeverity`, `rejectsMissingCriterion` (chỉ có 4 tiêu chí), `rejectsUnknownLabel`.
  - `agentReviewSeriousCriterionWarns`: một tiêu chí `NGHIEM_TRONG` → `AGENT_REVIEW` mức WARN.
  - `reportMarkdownUnder40Lines`.
- [ ] **Step 2: Chạy** → FAIL.
- [ ] **Step 3: Cài đặt** `EvalService` và `EvalCommand`. Hoàn thiện `aisdlc submit review` (Task 7) dùng `EvalReport.passed` và `AgentReview.read`.
- [ ] **Step 4: Chạy lại** `mvn -q test` toàn repo → PASS.
- [ ] **Step 5: Commit** `feat(eval): tier-1 evaluation report`.

### Task 11: Plugin Claude Code

**Files:**
- Create: `plugin/.claude-plugin/plugin.json`, `plugin/hooks/hooks.json`, `plugin/bin/aisdlc`, `plugin/install.sh`, `.claude-plugin/marketplace.json`
- Create: `plugin/commands/{init,brd,design,cases,step,eval,review,status}.md`
- Create: `plugin/agents/aisdlc-reviewer.md`
- Test: `plugin/test/plugin-files-test.sh`

**Interfaces:**
- Consumes: CLI (Task 7, 10).
- Produces:
  - `plugin.json`: `{"name":"aisdlc","version":"0.1.0","description":"Quy trình AISDLC: cổng duyệt, code theo bước, eval tầng 1"}`.
  - `hooks.json`:
    ```json
    {"hooks":{
      "SessionStart":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/bin/aisdlc hook session-start"}]}],
      "PreToolUse":[{"matcher":"Edit|Write|MultiEdit|NotebookEdit|Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/bin/aisdlc hook pre-tool-use","timeout":15}]}],
      "Stop":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/bin/aisdlc hook stop"}]}]
    }}
    ```
  - `bin/aisdlc`: `exec java -XX:TieredStopAtLevel=1 -Xshare:auto -jar "$(dirname "$(readlink -f "$0")")/../lib/aisdlc-cli.jar" "$@"`.
  - `install.sh`: chạy `mvn -q -DskipTests package` ở root, copy jar vào `plugin/lib/`, symlink `~/.local/bin/aisdlc` → `plugin/bin/aisdlc` để dev chạy được lệnh duyệt trong terminal riêng, rồi in lệnh `/plugin marketplace add <repo>` và `/plugin install aisdlc@aisdlc`.
  - Mỗi command `.md` có frontmatter `description` và `argument-hint`. Thân lệnh ngắn (≤ 25 dòng), chỉ dẫn agent:
    - `init` (bước triage, spec §11.1): đọc ticket (dev dán nếu không có Jira). Đề xuất **luồng đầy đủ hay nhẹ** kèm lý do một dòng (luồng nhẹ chỉ khi là sửa bug hoặc thay đổi nhỏ, không đổi DB hay API) và liệt kê tối đa 5 điểm chưa rõ. **Hỏi dev chọn luồng**, rồi mới chạy `aisdlc init <KEY> [--lite]`. Các điểm chưa rõ được ghi vào mục `Câu hỏi mở` của brd.md (luồng đầy đủ) hoặc nêu cho dev (luồng nhẹ).
    - `brd` / `design` / `cases`: đọc `feedback.md` nếu có; soạn bằng cách điền **template, giữ ngắn**; chạy `aisdlc submit <gate>`, sửa tới khi hết findings; rồi **dừng** và nói "Dev duyệt bằng `aisdlc approve <gate>` ở terminal riêng" (hoặc `! aisdlc approve <gate>` nếu console hoạt động trong bash mode của Claude Code; executor kiểm và ghi kết quả vào README). Riêng `cases`: case phải cụ thể (giá trị thật), mỗi bước có case lỗi hoặc biên, và không viết case để dễ pass.
    - `step`: `aisdlc step start $1`, chỉ sửa file trong scope, viết code tối thiểu cho các case của bước, không thêm abstraction ngoài thiết kế; xong thì `aisdlc step finish $1`, dán nguyên tóm tắt cho dev và dừng.
    - `eval`: `aisdlc eval`, tóm tắt hard check fail.
    - `review`: `aisdlc eval`; `aisdlc diff --by-step --patch > aisdlc/features/<KEY>/eval/diff-by-step.md`; gọi subagent `aisdlc-reviewer` với **chỉ** đường dẫn brd, design.md, design.yaml, cases.yaml, diff-by-step.md (không tóm tắt quá trình code); ghi kết quả JSON vào `review-agent.json`; `aisdlc submit review`; dừng chờ dev duyệt.
    - `status`: `aisdlc status`.
  - `agents/aisdlc-reviewer.md`: frontmatter `name: aisdlc-reviewer`, `description`, `tools: Read, Grep, Glob`. Thân gồm 5 tiêu chí cố định, khớp id ở Task 10:
    - `sequence`: đúng sequence từng bước.
    - `error-handling`: xử lý lỗi theo BRD.
    - `over-engineering`: code hoặc abstraction thừa so với thiết kế.
    - `naming`: đặt tên.
    - `security`: input validation, SQL injection, log dữ liệu nhạy cảm.

    Mỗi tiêu chí có định nghĩa ngắn cho nhãn `DAT` / `CAN_SUA` / `NGHIEM_TRONG` để judge chấm ổn định. Output **chỉ** là JSON đúng schema `AgentReview`. Mỗi issue phải có `step`, `file`, `line`. Không đề xuất refactor ngoài phạm vi.
  - Executor đối chiếu định dạng plugin, commands, agents, hooks, marketplace với tài liệu Claude Code hiện hành (agent `claude-code-guide`) trước khi viết, và sửa chỗ khác nếu tài liệu khác plan.

- [ ] **Step 1: Viết test thất bại** `plugin-files-test.sh`: kiểm JSON hợp lệ (`python3 -m json.tool` hoặc `jq` nếu có, nếu không thì `java -jar aisdlc-cli.jar` có lệnh ẩn `validate-json`; chọn công cụ có sẵn trên máy dev); mỗi command có frontmatter `description`; `bin/aisdlc --help` chạy được sau `install.sh`; `echo '{"tool_name":"Read","cwd":"/tmp"}' | plugin/bin/aisdlc hook pre-tool-use` in rỗng.
- [ ] **Step 2: Chạy** `bash plugin/test/plugin-files-test.sh` → FAIL.
- [ ] **Step 3: Viết các file plugin.**
- [ ] **Step 4: Chạy lại** → PASS.
- [ ] **Step 5: Thử tay** trong Claude Code: cài plugin từ marketplace cục bộ, mở một repo không dùng AISDLC → không có thông báo hay chặn gì; mở `e2e/fixture-service` → SessionStart hiện quy tắc. Ghi kết quả vào commit message.
- [ ] **Step 6: Commit** `feat(plugin): claude code plugin for aisdlc`.

### Task 12: E2E trên fixture service + README

**Files:**
- Create: `e2e/fixture-service/` (Spring Boot 3.3.5, Maven, Postgres qua Testcontainers, Flyway `V1__init.sql` có bảng `account`; endpoint cần làm: `POST /api/v1/limits/check`, trả `{allowed, remaining}` theo hạn mức ngày trong bảng mới `daily_limit`)
- Create: `e2e/solutions/PAY-1/{brd.md,design.md,design.yaml,db/V2__daily_limit.sql,cases.yaml,S1/,S2/,review-agent.json}` và `e2e/solutions/FIX-2/` (luồng nhẹ: sửa một bug làm tròn)
- Create: `e2e/run-e2e.sh`, `docs/README.vi.md`

**Interfaces:**
- Consumes: toàn bộ CLI và plugin.
- Produces: script chạy được trên Linux có Docker, exit 0 khi toàn bộ luồng đúng.

- [ ] **Step 1: Viết `run-e2e.sh`** (`set -euo pipefail`). Script copy fixture sang thư mục tạm, `git init` rồi commit, chạy `aisdlc setup --package com.example.fixture`, rồi chạy luồng FULL PAY-1:
  1. Copy solution brd → `submit brd`. Duyệt bằng `script -qec "aisdlc approve brd" /dev/null <<< "PAY-1"` (pty thật, không có cửa sau).
  2. Kiểm hook: pipe JSON Write `src/main/java/.../LimitController.java` vào `aisdlc hook pre-tool-use` → output chứa `deny` (chưa qua cổng).
  3. design → submit (migration-check chạy thật) → approve. cases → submit (phải có case fail) → approve.
  4. Hook Bash `sed -i s/a/b/ aisdlc/features/PAY-1/cases.yaml` → deny.
  5. `step start S1` → copy `S1/` → `step finish S1` → kiểm output có `pass` → `step accept` (qua pty).
  6. `step start S2` → copy `S2/` cộng một file ngoài scope `README2.txt` → `step finish S2` có "Ngoài phạm vi" → `scope allow README2.txt` (pty).
  7. `eval` → exit 0; `report.md` dòng đầu `Eval PAY-1: ĐẠT`.
  8. Copy `review-agent.json` → `submit review` → approve review.
  9. Chạy luồng LITE FIX-2 tương tự (không có brd/design). Cuối cùng chạy `aisdlc eval --ci --base <commit ban đầu>` → exit 0.
  10. Âm tính: sửa `cases.yaml` trên đĩa sau khi duyệt → `eval` exit 1 và report có `CASES_LOCKED`.
- [ ] **Step 2: Chạy** `bash e2e/run-e2e.sh` → ban đầu FAIL ở chỗ còn thiếu. Sửa code hoặc solution tới khi PASS. Bug nào lộ ra ở CLI hoặc core phải có thêm unit test ở task tương ứng trước khi sửa.
- [ ] **Step 3: Viết `docs/README.vi.md`** (≤ 150 dòng): cài đặt (`install.sh`, marketplace), `aisdlc setup` cho repo dịch vụ (dependency `aisdlc-junit`, cấu hình Testcontainers, JaCoCo), luồng làm một tính năng với slash command, lệnh của dev, cách đọc report, cài job CI (`include` file `aisdlc/gitlab-ci.aisdlc.yml`), **bật protected branch cho nhánh chính và bắt buộc pipeline (gồm job `aisdlc-eval`) pass mới được merge** (spec §11.4: merge do GitLab cưỡng chế, hook chỉ là lớp bảo vệ trên máy dev), giới hạn giai đoạn 1 (Bash heuristic, mermaid chỉ kiểm nhẹ, job theo lịch chưa hỗ trợ).
- [ ] **Step 4: Chạy toàn bộ** `mvn -q verify && bash plugin/test/plugin-files-test.sh && bash e2e/run-e2e.sh` → tất cả PASS.
- [ ] **Step 5: Commit** `test(e2e): full and lite flows on fixture service; docs`.

### Task 13: Thí điểm trên repo thật của team (cần dev)

**Files:**
- Create: `docs/pilot-notes.md`

- [ ] **Step 1: Hỏi dev đường dẫn repo Spring Boot thí điểm.** Xác minh các giả định của spec §7c.4: Maven hay Gradle, Flyway hay Liquibase, phiên bản Java/Spring Boot, loại DB (nếu là Oracle hoặc DB khác thì kiểm `SchemaDumper` và Testcontainers image), Docker trên runner, lint và quét bảo mật đang dùng. Ghi vào `pilot-notes.md`. Giả định nào sai → dừng và báo dev trước khi sửa.
- [ ] **Step 2: Chạy `aisdlc setup`** trên một branch của repo đó, cấu hình `checks` theo công cụ sẵn có, chạy `mvn verify` để bảo đảm build cũ không đổi.
- [ ] **Step 3: Dev làm 1 tính năng nhỏ** theo luồng đầy đủ và 1 bug theo luồng nhẹ. Ghi số lần bị trả lại mỗi cổng, thời gian mỗi bước, các chỗ hook chặn sai hoặc bỏ lọt, và những gì dev thấy thừa hoặc thiếu vào `pilot-notes.md`.
- [ ] **Step 4: Commit** `docs: pilot notes`.
