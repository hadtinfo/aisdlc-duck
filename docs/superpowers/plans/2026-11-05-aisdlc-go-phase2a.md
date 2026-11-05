# AISDLC Go — Giai đoạn 2a (Lõi cổng, bước, chính sách, risk): Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port lõi nghiệp vụ không có giao diện của AISDLC sang Go: cổng duyệt, các bước Sx, chính sách hook, risk policy, cùng các kiểu dữ liệu dùng chung mà chúng cần. Hành vi phải giống bản Java và có test xanh.

**Architecture:**
- Mỗi package Java thành một package trong `internal/`, dùng lại `model`, `spec`, `gitx`, `lang` và `yamlx` của giai đoạn 1.
- Hành vi lấy từ code Java và **test Java tương ứng** làm chuẩn: mỗi test Java có một test Go cùng tên `TestXxx`.
- Giai đoạn 2b (CLI và hook) dùng các package này.
- Phần chạy case (harness) ở giai đoạn 3; ở đây chỉ có kiểu dữ liệu kết quả chạy.
- Phần gọi HTTP lên web ở giai đoạn 4; ở đây chỉ đọc và ghi file cache đã ký.

**Tech Stack:** Go (directive hiện là 1.26 trong `go.mod`), thư viện chuẩn: `crypto/ed25519`, `crypto/x509`, `encoding/json`, `embed`. Không thêm dependency mới.

**Spec:** `docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`, các mục §4, §5 và §10 bước 2.

**Nguồn Java** (chỉ đọc): `~/Documents/git/aisdlc/aisdlc-core/src/{main,test}/java/com/aisdlc/core/`
- `{gate,step,policy,risk}/`
- `run/{CaseOutcome,CaseResult,CaseRunResult,SchemaSnapshot}.java`
- `web/{FeatureControl,FeatureGateState,SignedEnvelope,ControlCache,PolicyCache}.java`
- `review/{ReviewFlag,DocFacts,ScopeFlags,SqlFacts}.java`
- preset ở `aisdlc-core/src/main/resources/risk-presets/`

## Global Constraints

- Module `aisdlc`. Không thêm dependency ngoài thư viện chuẩn và những gì `go.mod` đã có.
- **Thông báo cho dev bằng tiếng Việt, giữ nguyên văn bản Java. Mã lỗi giữ nguyên.**
- Exception của Java thành error có kiểu riêng, ví dụ `*gate.Error{Msg string}`; `Error()` trả nguyên message Java. Không `panic` trừ lỗi lập trình.
- **File JSON và YAML trên đĩa** (`gates.json`, `steps.json`, `scope-extra.json`, `risk.yaml`, cache đã ký, grant) **giữ tên trường như Jackson ghi** (camelCase theo tên field của record Java). Thời gian ghi dạng RFC 3339. Ghi file trạng thái theo kiểu **atomic** (file tạm cùng thư mục rồi `os.Rename`) qua `fsx.WriteAtomic` (task 1).
- Đồng hồ được tiêm qua tham số `now func() time.Time`, thay cho `Clock` của Java.
- **Quy ước tên:**
  - class/record Java thành type Go cùng tên;
  - field thành exported field;
  - method thành method;
  - static method thành hàm package cùng tên (viết hoa chữ đầu);
  - enum thành `type X string` với hằng `XValue`, mang đúng chuỗi Jackson ghi.
  - Chỉ đặt tên khác khi Interfaces dưới đây ghi rõ.
- **Không gắn với ngôn ngữ dịch vụ:** chỗ nào bản Java giả định Java/Flyway thì đi qua `lang.Adapter` (giai đoạn 1: `IsSource`, `SampleFileName`, `MigrationVersion`, `MigrationOrder`). Lấy adapter bằng `langs.For(cfg.Language)` (package `aisdlc/internal/lang/langs`).
- Test dùng repo git tạm: đặt `user.name`/`user.email` cục bộ, đặt `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `HOME` tạm. Máy dev không có git config global.
- Mọi file qua `gofmt`; `make check` xanh trước mỗi commit.
- Commit message tiếng Việt kiểu `feat(gate): ...`, kết thúc bằng:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Hai hook ghi `gates.json` hoặc `steps.json` cùng lúc** (nhiều tính năng song song, mỗi session một hook): file không bao giờ bị cắt giữa chừng; người đọc luôn thấy bản cũ hoặc bản mới nguyên vẹn. Test ở task 1 (`fsx`) và task 3.
2. **Cache đã ký bị sửa tay hoặc ký bằng khóa khác** (agent cố tự duyệt bằng cách sửa file cache): `WebGateStore`/`ControlCache`/`PolicyCache` coi như không có dữ liệu, không lỗi, không tin nội dung. Test ở task 2.
3. **Sửa `cases.yaml` hoặc `risk.yaml` sau khi cổng CASES đã duyệt**: hash đổi nên cổng không còn khớp (cơ sở của `CASES_LOCKED`), kể cả khi chỉ đổi khoảng trắng. Test ở task 3.
4. **Scope glob có ký tự đặc biệt hoặc path unicode** (`docs/**`, `src/**/[id].go`, tên file tiếng Việt): khớp đúng như bản Java. Test ở task 4.
5. **Lệnh Bash tìm cách vượt chặn** (đổi tên qua `mv`, `cp`, `tee`, redirect `>`, `sed -i` vào file đã khóa, path tương đối có `..`): bị chặn như bản Java. Test ở task 5.

---

### Task 1: Kiểu dùng chung — `fsx`, `run`, `snapshot`

**Files:**
- Create: `internal/fsx/atomic.go`
- Create: `internal/run/run.go` (CaseOutcome, CaseResult, CaseRunResult), `internal/run/schema.go` (SchemaSnapshot)
- Create: `internal/snapshot/envelope.go` (SignedEnvelope), `control.go` (FeatureControl), `gatestate.go` (FeatureGateState), `controlcache.go`, `policycache.go`
- Modify: `internal/spec/stubs.go` (sửa câu báo lỗi, xem bước 4)
- Test: `internal/fsx/atomic_test.go`, `internal/run/schema_test.go`, `internal/snapshot/*_test.go`

**Interfaces:**
- Produces:
  - `fsx.WriteAtomic(path string, data []byte, perm os.FileMode) error`: tạo thư mục cha, ghi file tạm cùng thư mục, `fsync`, rồi `rename`.
  - `run.CaseOutcome` (`PASSED`, `FAILED`, `ERROR`); `run.CaseResult{ID, Step string; Outcome CaseOutcome; Message string}`; `run.CaseRunResult{ExitCode int; Cases []CaseResult}` với `Passed() int64` và `NotPassedIDs() []string`.
  - `run.SchemaSnapshot{Tables map[string][]string}` với `ReadSchemaSnapshot(path) (SchemaSnapshot, error)`, `(SchemaSnapshot) Write(path) error`, `(SchemaSnapshot) Diff(expected SchemaSnapshot) []string`.
  - `snapshot.SignedEnvelope{Payload, Sig, KeyID string}`; tag JSON: `payload`, `sig`, `keyId`.
    - `Sign(payload []byte, key ed25519.PrivateKey, keyID string) SignedEnvelope`
    - `(SignedEnvelope) Verify(key ed25519.PublicKey) ([]byte, bool)`
    - `KeyID(ed25519.PublicKey) string`
    - `PublicKeyFromBase64(x509b64 string) (ed25519.PublicKey, error)`
    - `EncodePublicKey(ed25519.PublicKey) string`

    Định dạng giống hệt `SignedEnvelope.java`: X.509 DER, base64 chuẩn, cách tính keyId như Java.
  - `snapshot.FeatureControl` cùng `Pause`, `Instruction`, `Action`; `(FeatureControl) ToJSON() ([]byte, error)`, `FeatureControlFromJSON([]byte) (FeatureControl, error)`.
  - `snapshot.FeatureGateState` cùng `Sub`, `Appr`, `Fb`; `ToJSON` và `FeatureGateStateFromJSON`. `Sub.Findings` là `[]spec.Finding`.
  - `snapshot.ControlCacheFile(repoRoot string, key model.FeatureKey) string`; `snapshot.ReadControlCache(file string, pinned ed25519.PublicKey, repoID string, key model.FeatureKey) (FeatureControl, bool)`.
  - `snapshot.CachedPolicy{RepoID string; Version int; YAML string}`; `PolicyCacheFile(repoRoot) string`; `ReadPolicyCache(file, pinned, repoID) (CachedPolicy, bool)`; `VerifyPolicy(envelope []byte, pinned, repoID) (CachedPolicy, bool)`.

  Đường dẫn file cache giữ đúng như Java.

- [ ] **Step 1: Viết test fail.**
  - Port `SchemaSnapshotTest` (2 test), `SignedEnvelopeTest` (6), `FeatureControlTest` và `ControlCacheTest` (6).
  - Thêm:
    - `TestWriteAtomicNoPartialFile`: 20 goroutine cùng `WriteAtomic` vào một path, mỗi goroutine ghi một nội dung 64 KB khác nhau. Mỗi lần đọc giữa chừng phải ra đúng một trong các nội dung đầy đủ. Sau cùng thư mục không còn file tạm.
    - `TestEnvelopeJavaInterop`: dùng **một phong bì do bản Java ký** (sinh một lần từ repo Java bằng `jshell` với classpath `aisdlc-core/target/*.jar`, gồm payload `{"a":1}`, khóa Ed25519 sinh tại chỗ, `keyId` và khóa công khai base64; dán vào test làm hằng). Go phải `Verify` được phong bì đó, và `KeyID` của khóa đó phải bằng `keyId` Java. Test này bảo đảm web Go và CLI Go tương thích định dạng mà bản Java đã chọn.
- [ ] **Step 2:** Chạy `go test ./internal/fsx/ ./internal/run/ ./internal/snapshot/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces. Cache bị sửa, sai chữ ký, sai repo hoặc sai feature thì trả `false`, không trả error (đúng như Java trả `Optional.empty()`).
- [ ] **Step 4:** Sửa câu báo lỗi của `CASES_STUB_UNSUPPORTED` trong `internal/spec/stubs.go`. Đây là điểm còn để lại từ giai đoạn 1: khi khóa được hỗ trợ nhưng sai cấu trúc (ví dụ `queryParameters` không phải map, matcher rỗng hoặc có hai khóa), câu báo lỗi là `Stub <id>: "<đường dẫn>" sai cấu trúc (<mô tả cấu trúc đúng>).` Khóa lạ thật sự thì giữ câu cũ. Thêm `TestStubWrongShapeMessage`, và test cho matcher có hai khóa được hỗ trợ (`{equalTo, matches}`).
- [ ] **Step 5:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 6:** Commit: `feat(snapshot): kiểu dùng chung, phong bì ký, ghi file atomic`.

### Task 2: `internal/gate`

**Files:**
- Create: `internal/gate/gate.go` (Gate, GateState, ApprovalMode, artifacts), `approval.go` (Approval, Submission), `store.go` (interface GateStore), `localstore.go`, `webstore.go`, `service.go`, `errors.go`
- Test: `internal/gate/gate_test.go`, `service_test.go`, `webstore_test.go`

**Interfaces:**
- Consumes: `model.FeaturePaths`, `model.Flow`, `model.RepoConfig`, `model.Contents`/`SHA256Contents`, `spec.Finding`, `snapshot.FeatureGateState`/`ReadControlCache`-style verify, `fsx.WriteAtomic`.
- Produces:
  - `type Gate string`: `BRD`, `DESIGN`, `CASES`, `REVIEW`.
    - `Sequence(flow model.Flow) []Gate`
    - `(Gate) Artifacts(p model.FeaturePaths, f model.Flow) []string`
    - `(Gate) ArtifactContents(p model.FeaturePaths, f model.Flow) ([]model.Content, error)`
  - `type GateState string` với đúng các hằng của `GateState.java`; `type ApprovalMode string`.
  - `Approval` (field như Java, hằng `ApprovalAuto = "auto"`, `ApprovalClaudeCode = "claude-code"`) với `IsAuto()`, `RuleLevel()`, `AsSuperseded()`, `ApprovalFromWeb(...)`; `Submission{Gate; Hash string; At time.Time; Findings []spec.Finding}`.
  - `type GateStore interface { Refresh() error; Submission(Gate) (Submission, bool, error); SaveSubmission(Submission) error; Approval(Gate) (Approval, bool, error); SaveApproval(Approval) error; Clear(Gate) error }`.
  - `NewLocalGateStore(p model.FeaturePaths) *LocalGateStore`: file `gates.json`, ghi bằng `fsx.WriteAtomic`.
  - `NewWebGateStore(cacheFile string, pinned ed25519.PublicKey, repoID string, key model.FeatureKey) *WebGateStore`: chỉ đọc cache đã ký; các hàm `Save*`/`Clear` trả lỗi đúng như Java. `ReadWebGateState(cacheFile, pinned, repoID, key) (snapshot.FeatureGateState, bool)`.
  - `NewService(p model.FeaturePaths, flow model.Flow, cfg model.RepoConfig, store GateStore, now func() time.Time) *Service`, có các method đúng như `GateService.java`: `State`, `CurrentHash`, `Flow`, `HasApproval`, `Approval`, `ApprovalMatches`, `IsOpen`, `CodeReady`, `Submit`, `Approve(gate, approver, channel string)` (channel rỗng là bản 2 tham số của Java), `AutoApprove`, `SupersedeAuto`, `Reject`, `Reopen`, `FirstNotOpen`. Method nào Java ném `GateException` thì Go trả `error`.
  - `type Error struct{ Msg string }` (`GateException`).

**Thay đổi so với Java [quyết định trong plan]:** artifact của cổng CASES **không còn file suite test** (`casesTestPath`/`AisdlcCasesTest.java`), vì harness Go nằm ngoài repo dịch vụ. CASES gồm:
- `cases.yaml`,
- `schema-expected.json` nếu là LITE,
- `risk.yaml` nếu file có.

Bỏ hẳn `suiteFiles`.

- [ ] **Step 1: Viết test fail.**
  - Port `GateTest`, `GateServiceTest` và `WebGateStoreTest` (tổng 36 test). Test nào dùng suite file thì chuyển thành test khẳng định CASES **không** phụ thuộc file nào ngoài danh sách trên.
  - Thêm:
    - `TestCasesLockedOnWhitespaceEdit`: duyệt CASES, thêm một dấu cách vào `cases.yaml`, thì `ApprovalMatches(CASES)` là false.
    - `TestRiskYamlEditBreaksCasesApproval`: tương tự, nhưng sửa `risk.yaml`.
    - `TestWebStoreTamperedCacheIsEmpty`: cache ký đúng nhưng sửa 1 byte payload, hoặc ký bằng khóa khác, thì `Approval(...)` trả `ok == false` và không có error.
- [ ] **Step 2:** Chạy `go test ./internal/gate/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(gate): cổng duyệt, store local và cache web đã ký`.

### Task 3: `internal/step`

**Files:**
- Create: `internal/step/scope.go` (ScopeMatcher), `record.go` (StepRecord, StepStatus, StepCheck), `store.go`, `service.go`, `extras.go` (ScopeExtras), `pending.go` (PendingActions), `errors.go`
- Test: `internal/step/scope_test.go`, `service_test.go`, `pending_test.go`

**Interfaces:**
- Consumes: `gitx.Repo` (`SnapshotTree`, `Diff`), `gitx.FileChange`, `spec.StepSpec`, `model.*`, `run.CaseRunResult`, `snapshot.FeatureControl`, `gate` (đúng các chỗ `StepService.java` dùng), `fsx.WriteAtomic`.
- Produces:
  - `ScopeMatches(globs []string, relPath string) bool`; `CompileScope(globs []string) func(string) bool`. Ngữ nghĩa glob đúng như `ScopeMatcher.java`, gồm `**`, `*`, `?` và ngoặc.
  - `type StepStatus string`: `NOT_STARTED`, `IN_PROGRESS`, `AWAITING_DEV`, `ACCEPTED`. `StepRecord` và `StepCheck` có field như Java.
  - `NewService(p model.FeaturePaths, git *gitx.Repo, specs []spec.StepSpec, cfg model.RepoConfig, now func() time.Time) *Service`, có các method đúng như `StepService.java`: `Start`, `Finish(id string, run run.CaseRunResult)`, `Accept`, `Current`, `All`, `AllowedGlobs`, `UsesScopeExtras`. Thêm hàm package `Ignored(p, cfg, gitx.FileChange) bool` và `IgnoredPath(p, cfg, path string) bool`.
  - `NewScopeExtras(p model.FeaturePaths) *ScopeExtras` với `Paths()` và `Add(relPath, approver string, at time.Time) error`.
  - `ApplyPending(c snapshot.FeatureControl, steps *Service, extras *ScopeExtras, done map[int64]bool, ...)` theo đúng tham số của `PendingActions.apply`, trả `[]PendingResult`. Có hằng `PendingApplied = "APPLIED"`, `PendingStale = "STALE"` và hàm `ValidScopePath(p string) bool`.
  - `type Error struct{ Msg string }` (`StepException`).

- [ ] **Step 1: Viết test fail.**
  - Port `ScopeMatcherTest`, `StepServiceTest` và `PendingActionsTest` (27 test), dùng repo git tạm theo Global Constraints.
  - Thêm:
    - `TestScopeUnicodeAndBrackets`: `src/**/[id].go` khớp `src/a/b/[id].go` (ngoặc vuông là ký tự thường hay lớp ký tự: theo đúng Java); `docs/**` khớp `docs/hướng-dẫn.md`.
    - `TestStepsJSONAtomicUnderConcurrentStart`: 2 goroutine mỗi bên gọi `Start`/`Finish` 50 lần trên 2 tính năng khác nhau cùng repo. `steps.json` của mỗi tính năng luôn parse được.
- [ ] **Step 2:** Chạy `go test ./internal/step/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(step): bước Sx, scope, phạm vi bổ sung, hành động chờ`.

### Task 4: `internal/review` (phần risk cần)

**Files:**
- Create: `internal/review/flag.go` (ReviewFlag, Level), `docfacts.go`, `scopeflags.go`, `sqlfacts.go`
- Test: `internal/review/docfacts_test.go`, `scopeflags_test.go`, `sqlfacts_test.go`

**Interfaces:**
- Consumes: `spec.MarkdownSections`, `spec.StepSpec`, `step.CompileScope`, `model.ReviewSettings`, `lang.Adapter`.
- Produces:
  - `ReviewFlag{Code string; Level Level; Message, Hint string; Refs []string}`; `type Level string`: `HIGH`, `WARN`, `INFO`, `NA`.
  - `DocFacts`: các type `Rule`, `BrdDoc`, `ApiRow`, `DesignDoc` (field như Java) và hàm `ParseBRD(md string) BrdDoc`, `ParseDesign(md string) DesignDoc` (đổi tên từ `DocFacts.brd`/`design`).
  - `ScopeMatchedFiles(st spec.StepSpec, files []string) []string`; `ScopeMatcher(globs []string) func(string) bool`; `ValidGlob(g string) bool`; `ScopeFlags(steps []spec.StepSpec, files []string, s model.ReviewSettings, sampleFile string) []ReviewFlag`; `SampleFor(glob, sampleFile string) string`.

    **Đổi so với Java:** `**` ở cuối glob thành `aisdlcx/<sampleFile>`, thay cho `aisdlcx/AisdlcX.java`. Người gọi truyền `adapter.SampleFileName()`.
  - `SqlColumn`, `SqlStatement` (field như Java); `ParseSQL(sql string) []SqlStatement`. Hàm gom bảng theo thư mục migration (đoạn Java quanh dòng 400–437) nhận thêm `order func([]string) []string`; người gọi truyền `adapter.MigrationOrder`. Bỏ `compareMigrations`/`VERSION` kiểu Flyway khỏi package này.

  Regex Java sang RE2: không dùng lookaround (Java không dùng ở đây). Cờ `(?is)` tương ứng cờ `F` của Java; đọc giá trị `F` trong `SqlFacts.java` để chuyển đúng.

- [ ] **Step 1: Viết test fail.** Port `DocFactsTest` (13 test), `ScopeFlagsTest` (6) và `SqlFactsTest` (12). Trong test, tên migration đổi từ kiểu Flyway (`V1__init.sql`) sang kiểu goose (`00001_init.sql`), và kỳ vọng thứ tự dùng `golang.Adapter{}.MigrationOrder`. Test có `AisdlcX.java` thì đổi thành `aisdlcx.go`.
- [ ] **Step 2:** Chạy `go test ./internal/review/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(review): DocFacts, ScopeFlags, SqlFacts theo adapter`.

### Task 5: `internal/policy`

**Files:**
- Create: `internal/policy/decision.go` (Decision, ToolCall, PolicyState), `bash.go` (BashHeuristics), `devcommand.go`, `grants.go` (DevGrants), `hook.go` (HookPolicy)
- Test: `internal/policy/bash_test.go`, `devcommand_test.go`, `grants_test.go`, `hook_test.go`

**Interfaces:**
- Consumes: `gate`, `step`, `model`, `snapshot.FeatureControl` (đúng các chỗ `HookPolicy.java` dùng), `fsx.WriteAtomic`.
- Produces:
  - `Decision{Allow bool; Reason, Code string}` với `Ok()`, `Deny(reason)`, `DenyCode(code, reason)`.
  - `ToolCall{ToolName, FilePath, Command string}`.
  - `PolicyState`: field như `PolicyState.java`.
  - `BashCheck(command string, protectedPaths []string, repoRoot string) (string, bool)`.
  - `DevCommand{Verb string; Args []string}` với `DevCommandMentions`, `ParseDevCommand`, `DevCommandFromArgs`.
  - `DevGrants`: `GrantTTL = 60 * time.Second`; `GrantFile(root, sessionID) string`; `IssueGrant(root string, g Grant) error`; `ConsumeGrant(root, sessionID, feature, command, hash string, now time.Time) (bool, error)`; `ClearGrant(root, sessionID) error`.
  - `WebConfigDecision(paths []string, command, configDir, cwd string) Decision`; `PreToolUse(call ToolCall, s PolicyState) Decision`; `Stop(stopHookActive bool, s PolicyState) Decision`; `InstructionContext(items []snapshot.Instruction, zone *time.Location) string`; `SessionContext(s PolicyState) string`.

  Logic, thứ tự kiểm tra, mã lỗi và câu chữ giữ đúng `HookPolicy.java` và `BashHeuristics.java`.

**Thay đổi so với Java:** chỗ nào `HookPolicy` bảo vệ file suite test (`casesTestPath`) thì bỏ, vì không còn file đó. Các bảo vệ khác giữ nguyên: `cases.yaml` đã khóa, `aisdlc/config.yaml`, `.aisdlc/**`, `~/.config/aisdlc`, file ngoài scope của bước.

- [ ] **Step 1: Viết test fail.**
  - Port `BashHeuristicsTest`, `DevCommandTest`, `DevGrantsTest` và `HookPolicyTest` (59 test).
  - Thêm `TestBashBypassAttempts`, dạng table. Mỗi lệnh dưới đây ghi vào `aisdlc/features/PAY-1/cases.yaml` đã khóa và phải bị chặn:
    - `mv x aisdlc/features/PAY-1/cases.yaml`
    - `cp /tmp/c aisdlc/features/PAY-1/cases.yaml`
    - `echo a > aisdlc/features/PAY-1/cases.yaml`
    - `tee aisdlc/features/PAY-1/cases.yaml < x`
    - `sed -i s/a/b/ aisdlc/features/PAY-1/cases.yaml`
    - `cd aisdlc/features && sed -i s/a/b/ PAY-1/cases.yaml`
    - `sed -i s/a/b/ ./aisdlc/../aisdlc/features/PAY-1/cases.yaml`

    Với lệnh nào mà bản Java **không** chặn, ghi rõ trong report của task và giữ đúng hành vi Java (không tự thêm luật mới); test khẳng định hành vi Java.
- [ ] **Step 2:** Chạy `go test ./internal/policy/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(policy): chính sách hook, Bash heuristics, phiếu cho phép`.

### Task 6: `internal/risk`, phần 1 (policy, preset, phân loại)

**Files:**
- Create: `internal/risk/policy.go` (RiskPolicy, RiskRule, MatchKind, ClassificationRules, RiskPolicyException → `PolicyError`), `presets.go`, `presets/` (chép nguyên `banking/{1,2}.yaml`, `generic/{1,2}.yaml`, embed), `globoverlap.go`, `inputs.go` (RiskInputs, RiskInputsCollector), `classifier.go` (Classifier, RiskResult), `flow.go` (FlowDecision)
- Test: `internal/risk/policy_test.go`, `presets_test.go`, `globoverlap_test.go`, `inputs_test.go`, `classifier_test.go`, `flow_test.go`

**Interfaces:**
- Consumes: `review.ParseDesign`, `review.ParseSQL`, `review.ScopeMatcher`, `review.SampleFor`, `spec.*`, `model.FeaturePaths`, `gate.Gate`, `lang.Adapter`.
- Produces:
  - `ParsePolicy(yaml string) (RiskPolicy, error)` (lỗi kiểu `*PolicyError`); `PolicyFromJSON(json string) (RiskPolicy, error)`; `FlowUnits = []string{"brd", "design", "cases", "step"}`.
  - `PresetNames() []string`, `PresetLatestVersion(name) string`, `PresetYAML(name, version string) (string, error)`, `LoadPreset(name, version) (RiskPolicy, error)`, `LatestPreset(name) (RiskPolicy, error)`.
  - `Overlaps(ruleGlob, scopeGlob, sampleFile string) bool` (Java `GlobOverlap.overlaps`, thêm `sampleFile`).
  - `CollectFull(p model.FeaturePaths, title string, a lang.Adapter) (RiskInputs, error)`, `CollectLite(...)`, `CollectBrdOnly(title, brd string) RiskInputs`. Bảng và cột lấy từ migration theo `a.MigrationOrder`.
  - `Classify(p RiskPolicy, in RiskInputs) RiskResult`.
  - Từ `FlowDecision`: `FlowUnit(g gate.Gate) string`, `AutoGate(p RiskPolicy, level string, g gate.Gate, hasHighFlag bool) (string, bool)`, `BrdLevel`, `Stricter`, `LevelForGate`, `AutoStep(r RiskFile) bool`. `RiskFile` được định nghĩa ở task 7; task này tạo trước type rỗng trong `riskfile.go` để `AutoStep` biên dịch được, task 7 bổ sung đủ field.

- [ ] **Step 1: Viết test fail.**
  - Port `RiskPolicyTest`, `PresetsTest`, `GlobOverlapTest`, `RiskInputsCollectorTest`, `ClassifierTest` và `FlowDecisionTest`.
  - Port `NoDomainStringsTest` thành `TestNoBusinessVocabulary`: quét mọi file `.go` trong `internal/` và `cmd/` (nếu có) với regex `(?i)hạch toán|bút toán|định khoản|\bledger_|\bfee_|accounting`; danh sách vi phạm phải rỗng, và phải quét được ít nhất 1 file.
  - Migration trong test dùng tên kiểu goose.
- [ ] **Step 2:** Chạy `go test ./internal/risk/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces. Preset chép **nguyên byte** từ repo Java.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(risk): risk policy, preset, phân loại, quyết định luồng`.

### Task 7: `internal/risk`, phần 2 (risk.yaml, yêu cầu, diff policy)

**Files:**
- Create: `internal/risk/riskfile.go` (bổ sung RiskFile), `requirements.go` (Requirement, RequirementCatalog, RequirementStatus, RequirementChecker), `policydiff.go` (PolicyChange, PolicyDiff)
- Test: `internal/risk/riskfile_test.go`, `requirements_test.go`, `policydiff_test.go`

**Interfaces:**
- Consumes: task 6, `review.BrdDoc`/`DesignDoc`, `spec.CasesSpec`/`DesignSpec`/`Impact`/`Finding`, `yamlx`.
- Produces:
  - `RiskFile` (field và tên YAML như `RiskFile.java`); `FlowOf(p RiskPolicy, level string) map[string]string`; `NewRiskFile(p RiskPolicy, r RiskResult, agent *spec.Impact) RiskFile`; `RequiredByDesign(designYaml string) (bool, error)`; `ReadRiskFile(path string) (RiskFile, bool, error)`; `(RiskFile) Write(path string) error`, ghi bằng `fsx.WriteAtomic` sau khi `yamlx.Dump`.
  - Hằng `ReqRulePosNeg = "rule-pos-neg"`, `ReqBoundary`, `ReqIdempotent`, `ReqConcurrent`, `ReqInvariant`, `ReqCoverage`, `ReqSQLZero` (giá trị như Java); `KnownRequirement(id) bool`, `RequiredParams(id) []string`, `RequirementDisplayName(r Requirement) string`.
  - `CheckRequirements(risk RiskFile, cases spec.CasesSpec, design *spec.DesignSpec, brd review.BrdDoc, designDoc review.DesignDoc) []RequirementStatus`; `RequirementFindings(risk RiskFile, st []RequirementStatus) []spec.Finding`.
  - `DiffPolicies(from, to RiskPolicy) []PolicyChange`.

- [ ] **Step 1: Viết test fail.**
  - Port `RiskFileTest`, `RequirementCheckerTest` và `PolicyDiffTest`.
  - Thêm `TestRiskFileRoundTripStable`: `Write` rồi `ReadRiskFile` rồi `Write` lần nữa cho ra cùng byte. Nếu không, mỗi lần ghi lại sẽ làm đổi hash cổng CASES.
- [ ] **Step 2:** Chạy `go test ./internal/risk/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS cho toàn repo.
- [ ] **Step 5:** Commit: `feat(risk): risk.yaml, kiểm yêu cầu, diff policy`.

---

## Ghi chú cho giai đoạn 2b (CLI và hook, plan riêng)

- `Context` của CLI có ba điểm nối, mỗi điểm có một bản tạm:
  - `CaseRunner`: bản tạm báo "chạy case cần giai đoạn 3".
  - `Publisher`: bản không làm gì cho tới giai đoạn 4.
  - Review card: bản tạm ở giai đoạn 3.
- Launcher `plugin/bin/aisdlc` chọn binary theo `uname`; bỏ các nhánh bash né JVM.
- Port test CLI dùng `TestRepo`/`FakeWeb` của Java; phần `FakeWeb` để tới giai đoạn 4.
- Hook không còn bảo vệ `AisdlcCasesTest.java` (đã bỏ ở task 2 và task 5).
