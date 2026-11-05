# AISDLC Go — Giai đoạn 1 (Nền): Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dựng repo Go mới với các package nền: `model`, `spec`, `gitx`, `lang` và adapter Go (phần tĩnh). Các phần này có hành vi tương đương bản Java và có test xanh.

**Architecture:** Một Go module trong `<repo root>`.
- Mỗi package Java (`model`, `spec`, `git`) thành một package trong `internal/`.
- Hành vi lấy từ code Java và **test Java tương ứng** làm chuẩn. Mỗi test Java có một test Go cùng tên, viết dạng `TestXxx`.
- `lang` định nghĩa interface adapter. Ở giai đoạn này chỉ có các phương thức không chạy tiến trình. Build, test và start dịch vụ được thêm vào interface ở giai đoạn 3.

**Tech Stack:** Go ≥ 1.23 (máy dev đang có 1.27.3), `gopkg.in/yaml.v3`, `golang.org/x/text/unicode/norm`, `golang.org/x/mod/modfile`, git CLI. Test bằng thư viện chuẩn `testing`.

**Spec:** `docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`, các mục §3.1, §4, §5 và §10 bước 1.

**Nguồn Java** (đọc để lấy hành vi; không sửa): `~/Documents/git/aisdlc`
- `aisdlc-core/src/main/java/com/aisdlc/core/{model,spec,git}/` và test ở `aisdlc-core/src/test/java/com/aisdlc/core/{model,spec,git}/`.
- `aisdlc-core/src/main/java/com/aisdlc/core/review/SqlFacts.java:440-468` (thứ tự migration).
- `review/DependencyFlags.java` (so dependency).
- `aisdlc-junit/src/main/java/com/aisdlc/junit/DraftMigrations.java` (version của migration nháp).

## Global Constraints

- Module path: `aisdlc`. Go directive: `go 1.23`.
- Dependency được phép ở giai đoạn này: chỉ `gopkg.in/yaml.v3`, `golang.org/x/text`, `golang.org/x/mod`. Thêm thư viện khác phải ghi lý do trong commit.
- **Thông báo cho dev bằng tiếng Việt**, giữ **nguyên văn** câu chữ của bản Java. **Mã lỗi giữ nguyên** (`BRD_MISSING_SECTION`, `CASES_DUP_ID`, …).
- **Đọc YAML khoan dung với khóa lạ, như Jackson** (`FAIL_ON_UNKNOWN_PROPERTIES` đang tắt), nên **không** dùng `KnownFields(true)`. Lỗi cú pháp thì trả error có tên file và số dòng (spec §4).
- Ngoại lệ unchecked của Java thành `error` trả về. Không `panic` trừ lỗi lập trình.
- Đường dẫn tương đối luôn dùng `/`.
- Mọi file `.go` qua được `gofmt` và `go vet ./...`.
- Commit nhỏ theo task, message tiếng Việt kiểu `feat(model): ...`, kết thúc bằng:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **`cases.yaml` hoặc `design.yaml` sai cú pháp** (agent viết hỏng): trả error tiếng Việt có tên file và số dòng, không panic, không trả struct rỗng. Test ở task 5.
2. **Heading tiếng Việt dạng NFD** (editor trên macOS lưu "Mục tiêu" thành NFD): vẫn khớp mục bắt buộc sau khi chuẩn hóa NFC. Test ở task 6.
3. **File markdown xuống dòng CRLF** (dev dùng Windows): tách mục, sơ đồ mermaid và câu hỏi mở vẫn đúng. Test ở task 6.
4. **`config.yaml` thiếu, rỗng, có giá trị `null` hoặc còn khóa cũ của bản Java** (`mavenCmd`): dùng mặc định, bỏ qua khóa cũ, không lỗi. Test ở task 4.
5. **`go.mod` hỏng hoặc base tree không có `go.mod`**: kết quả là N/A kèm lý do, không lỗi cả review. Test ở task 7.

---

### Task 1: Khởi tạo repo, Makefile, template và YAML helper

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore`, `README.md` (3–5 dòng: repo là bản Go của AISDLC, trỏ tới spec)
- Create: `internal/yamlx/yamlx.go`, `internal/yamlx/yamlx_test.go`
- Create: `templates/templates.go` (`//go:embed *.md *.yaml`), chép nguyên `brd.md`, `design.md`, `design.yaml`, `cases.full.yaml`, `cases.lite.yaml` từ `aisdlc-cli/src/main/resources/templates/`
- Create: `docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md` (chép từ repo Java), `docs/superpowers/plans/2026-11-05-aisdlc-go-phase1.md` (file này), `docs/legacy-java/` (chép `docs/superpowers/specs` và `docs/superpowers/plans` cũ của repo Java)

**Interfaces:**
- Produces:
  - `yamlx.Parse(data []byte, out any) error`
  - `yamlx.Read(path string, out any) error`
  - `yamlx.Dump(v any) ([]byte, error)`
  - `yamlx.Write(path string, v any) error` (tạo thư mục cha nếu cần, không ghi `---` đầu file)
  - `templates.FS embed.FS`
  - `make test` = `go test ./...`; `make check` = `gofmt -l` (rỗng) + `go vet ./...` + `go test ./...`

- [ ] **Step 1:** `git init` trong `<repo root>`, `go mod init aisdlc`, sửa directive thành `go 1.23`. Chép docs và template như liệt kê ở trên.
- [ ] **Step 2:** Chạy `go build ./...`. Kết quả mong đợi: không lỗi (chưa có package nào).
- [ ] **Step 3: Viết test fail** `internal/yamlx/yamlx_test.go`:
  - `TestParseIgnoresUnknownKeys`: `a: 1\nzzz: 2` vào `struct{A int \`yaml:"a"\`}` thì không lỗi và `A == 1`.
  - `TestReadSyntaxErrorHasFileAndLine`: file có dòng 3 là `  - [unclosed` thì error chứa tên file, chuỗi `dòng 3` và tiền tố `Không đọc được`.
  - `TestParseSyntaxErrorPrefix`: error bắt đầu bằng `YAML không hợp lệ: `.
  - `TestWriteCreatesParentAndNoDocMarker`: ghi vào `t.TempDir()/x/y.yaml`; file tồn tại và không bắt đầu bằng `---`.
- [ ] **Step 4:** Chạy `go test ./internal/yamlx/`. Kết quả mong đợi: FAIL (chưa có hàm).
- [ ] **Step 5: Code** `internal/yamlx/yamlx.go`. Bọc `yaml.v3`. Lấy số dòng từ `*yaml.TypeError` hoặc chuỗi lỗi `yaml: line N:` rồi viết lại thành `dòng N`. Message:
  - Read: `Không đọc được <path>: <chi tiết>`.
  - Parse: `YAML không hợp lệ: <chi tiết>`.
  - Write: `Không ghi được <path>: <chi tiết>`.

  Encoder dùng `SetIndent(2)`.
- [ ] **Step 6:** Viết `Makefile` có các target `build`, `test`, `vet`, `fmt-check` và `check`. Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 7:** Commit: `chore: khởi tạo repo Go, yamlx, template`.

### Task 2: `internal/model`, phần cơ bản

**Files:**
- Create:
  - `internal/model/featurekey.go`
  - `internal/model/flow.go`
  - `internal/model/featuremeta.go`
  - `internal/model/paths.go`
  - `internal/model/text.go`
  - `internal/model/hashing.go`
  - `internal/model/active.go` (ActiveFeature + SessionBindings)
- Test: `internal/model/model_test.go`, `hashing_test.go`, `sessions_test.go`

**Interfaces:**
- Consumes: `yamlx.Read/Write` (task 1).
- Produces:
  - `type FeatureKey string`;
    - `ParseFeatureKey(s string) (FeatureKey, error)`: regex `^[A-Z][A-Z0-9]+-\d+$`; message lỗi `Feature key không hợp lệ: <s> (ví dụ: PAY-123)`;
    - `UnmarshalYAML` và `UnmarshalJSON` kiểm tra như `ParseFeatureKey`.
  - `type Flow string`, hằng `FlowFull = "FULL"` và `FlowLite = "LITE"`. Ghi và đọc YAML bằng chữ hoa như Jackson ghi enum.
  - `type FeatureMeta struct { Key FeatureKey; Title string; Flow Flow; BaseTree string; CreatedAt time.Time; Owner string }`. Tag YAML: `key`, `title`, `flow`, `baseTree`, `createdAt`, `owner`.
    - `ReadFeatureMeta(path string) (FeatureMeta, error)`
    - `(FeatureMeta) Write(path string) error`
  - `type FeaturePaths struct{ RepoRoot string; Key FeatureKey }` với các method trả path tuyệt đối: `Dir`, `Meta`, `Brd`, `DesignMd`, `DesignYaml`, `DbDrafts`, `Cases`, `SchemaExpected`, `Gates`, `Steps`, `ScopeExtra`, `Feedback`, `EvalDir`, `ReviewAgent`, `Risk`. Tên file đúng như `FeaturePaths.java`. Thêm `Rel(p string) string`.
  - `NFC(s string) string`.
  - `SHA256Files(repoRoot string, files []string) (string, error)`.
  - `type Content struct{ Path string; Data []byte }` (`Data == nil` nghĩa là file không có); `SHA256Contents(c []Content) string`; `Contents(repoRoot string, files []string) ([]Content, error)`. Thuật toán đúng như `Hashing.java`: path, `0x00`, bytes hoặc `<missing>`, `0x00`; kết quả là hex chữ thường.
  - `ReadActive(repoRoot) (FeatureKey, bool, error)`; `WriteActive(repoRoot, FeatureKey) error`. File `.aisdlc/active`, nội dung là key + `\n`.
  - `SessionFileName(id string) string`; `ReadSession(repoRoot, id) (FeatureKey, bool, error)`; `WriteSession(repoRoot, id, FeatureKey) error`; `AllSessions(repoRoot) (map[string]FeatureKey, error)`. Thư mục `.aisdlc/sessions/`; làm sạch tên và cắt 100 ký tự như `SessionBindings.java`.

- [ ] **Step 1: Viết test fail.** Port toàn bộ `ModelTest.java` (14 test), `HashingTest.java` (2) và `SessionBindingsTest.java` (4), cùng tên theo kiểu `TestXxx`. Thêm:
  - `TestHashKnownVector`: `SHA256Contents([]Content{{"a.txt", []byte("x")}, {"b.txt", nil}})` bằng đúng giá trị Java tính ra. Lấy giá trị bằng cách chạy `jshell` với classpath core hoặc viết một test Java tạm trong repo Java, **không commit** test đó. Giá trị này bảo đảm web và CLI tính cùng hash.
  - `TestFeatureMetaRoundTrip`: `createdAt` ghi ra dạng RFC 3339 và đọc lại bằng nhau.
- [ ] **Step 2:** Chạy `go test ./internal/model/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code các file ở trên theo đúng Interfaces.
- [ ] **Step 4:** Chạy `go test ./internal/model/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(model): FeatureKey, FeaturePaths, hashing, session`.

### Task 3: `internal/gitx`

**Files:**
- Create: `internal/gitx/repo.go`, `internal/gitx/diff.go` (parse numstat, name-status và unified diff)
- Test: `internal/gitx/repo_test.go`

**Interfaces:**
- Produces:
  - `const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"`
  - `type ChangeType string`: `Added`, `Modified`, `Deleted`, `Renamed`
  - `type FileChange struct{ Path, OldPath string; Type ChangeType; Added, Deleted int; Binary bool }` và `(FileChange) Lines() int`
  - `type Error struct{ Msg string; Stderr string }` (thỏa `error`). Message giữ như `GitException`, kèm stderr.
  - `New(root string) *Repo`. Các method nhận `ctx context.Context`, trừ khi Java không có tham số nào:
    - `SnapshotTree(ctx) (string, error)`; `SnapshotTreeExcluding(ctx, dir string) (string, error)`
    - `Diff(ctx, from, to string) ([]FileChange, error)`
    - `ChangedLines(ctx, from, to string) (map[string]map[int]bool, error)`: set dòng dạng `map[int]bool`
    - `Patch(ctx, from, to string, paths ...string) (string, error)`
    - `FileAt(ctx, treeish, path string) (string, bool, error)`
    - `ListFiles(ctx) ([]string, error)`
    - `ListTree(ctx, treeish string, under ...string) ([]string, error)`
    - `MergeBaseTree(ctx, ref string) (string, error)`
    - `UserName(ctx) (string, error)`, `UserEmail(ctx) (string, error)`
    - `RemoteURL(ctx, name string) (string, bool, error)`
    - `CurrentBranch(ctx) (string, bool, error)`

- [ ] **Step 1: Viết test fail.** Port toàn bộ 17 test của `GitRepoTest.java`, gồm: snapshot không đụng index của dev, rename và delete, file binary, path có dấu cách, unicode và ký tự đặc biệt, dòng nội dung bắt đầu bằng `+++`, repo chưa có commit, git lỗi thì error có stderr. Helper test tạo repo trong `t.TempDir()` và đặt `user.name`/`user.email` cục bộ.
- [ ] **Step 2:** Chạy `go test ./internal/gitx/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3: Code** bằng `exec.CommandContext("git", ...)`, giữ **đúng các lệnh và cờ git mà `GitRepo.java` dùng**:
  - Snapshot qua `GIT_INDEX_FILE` tạm.
  - Lệnh nào có tùy chọn `-z` thì dùng `-z`.
  - Đặt `core.quotepath=false`.

  Đọc `GitRepo.java` trước khi code. Không đổi chiến lược.
- [ ] **Step 4:** Chạy `go test ./internal/gitx/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(gitx): thao tác git qua CLI`.

### Task 4: `RepoConfig` và `SessionApproveSetting`

**Files:**
- Create: `internal/model/config.go`, `internal/model/sessionapprove.go`
- Test: `internal/model/config_test.go`, `internal/model/sessionapprove_test.go`

**Interfaces:**
- Consumes: `yamlx` (task 1), `gitx.Repo.FileAt` (task 3).
- Produces:

```go
type CheckCommand struct{ Name, Cmd string }
type ReviewSettings struct {
    ScopeMaxFiles, BudgetMaxFiles, BudgetMaxLines int
    MigrationDirs, ValueFields []string
}
type ServiceEnv struct {
    Port, DatabaseURL, KafkaBrokers string
    ExternalBaseURLs []string
}
type ContainerImage struct{ Image string; Topics []string }
type ServiceConfig struct {
    Main string; BuildFlags []string; HealthPath string
    StartTimeoutSeconds, ShutdownTimeoutSeconds int
    Env ServiceEnv; ExtraEnv map[string]string
    Postgres, Kafka ContainerImage
}
type MigrationsConfig struct{ Dir, Tool string }
type UnitTestConfig struct{ Packages, Flags []string }
type RepoConfig struct {
    Language string; TimeoutMinutes float64
    Service ServiceConfig; Migrations MigrationsConfig; UnitTest UnitTestConfig
    AlwaysAllowed, Unmanaged []string
    BudgetWarnFactor, CoverageWarn float64
    Checks []CheckCommand
    BaseRef string; CIRequireFeature bool
    Review ReviewSettings
    Approvals string // "web" | "local"
    SessionApprove bool
}
func LoadRepoConfig(repoRoot string) (RepoConfig, error) // aisdlc/config.yaml; không có file → mặc định
func ParseRepoConfig(yaml string) (RepoConfig, error)     // chuỗi rỗng/blank → mặc định
type SessionApproveSetting struct{ Enabled bool; Reason string }
func ReadSessionApprove(ctx context.Context, git *gitx.Repo, working RepoConfig) SessionApproveSetting
```

Giá trị mặc định:
- Các khóa cũ, đúng như `RepoConfig.java`: `alwaysAllowed: []`, `unmanaged: ["*.md","docs/**"]`, `budgetWarnFactor: 1.5`, `coverageWarn: 0.80`, `baseRef: origin/main`, `ciRequireFeature: true`, `approvals: local` (chỉ đúng chữ `web`, không phân biệt hoa thường, sau khi trim, mới là `web`), `sessionApprove: false`, `review` theo `ReviewSettings.DEFAULTS`.
- Các khóa mới (spec §3.1): `language: go`, `timeoutMinutes: 30`, `service.main: ./cmd/server`, `service.healthPath: /healthz`, `startTimeoutSeconds: 60`, `shutdownTimeoutSeconds: 15`, `service.env.port: PORT`, `service.postgres.image: postgres:16-alpine`, `service.kafka.image: apache/kafka:3.8.0`, `migrations.dir: db/migrations`, `migrations.tool: goose`, `unitTest.packages: ["./..."]`.
- `review.migrationDirs` không khai thì lấy `[migrations.dir]`.
- `databaseUrl` và `kafkaBrokers` mặc định rỗng: dịch vụ không dùng.

Đọc theo kiểu node như Java: khóa có giá trị `null` coi như không khai. Bỏ qua các khóa cũ `mavenCmd`, `mavenTimeoutMinutes`, `casesTestClass` và `casesTestPath`.

- [ ] **Step 1: Viết test fail.**
  - Port `SessionApproveSettingTest.java` (3 test), giữ nguyên reason `theo <ref>`, `chưa bật ở <ref>`, `không đọc được aisdlc/config.yaml ở <ref>`.
  - Port các test config trong `ModelTest.java`. Test nào kiểm khóa Maven (`mavenCmd`, `casesTestClass`) thì đổi sang khóa mới tương ứng (`timeoutMinutes`, `service.*`), không bỏ.
  - Thêm:
    - `TestConfigDefaultsWhenMissing`: không có file thì mọi giá trị mặc định ở trên đúng.
    - `TestConfigNullValuesUseDefaults`: `baseRef: null` thì là `origin/main`.
    - `TestConfigIgnoresLegacyJavaKeys`: có `mavenCmd: mvn` và `casesTestClass: X` thì không lỗi.
    - `TestMigrationDirsDefaultFollowsMigrationsDir`: `migrations: {dir: sql}` thì `Review.MigrationDirs == ["sql"]`.
    - `TestApprovalsNormalized`: `" WEB "` thành `web`, `foo` thành `local`.
- [ ] **Step 2:** Chạy `go test ./internal/model/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code. Parse vào `yaml.Node` hoặc `map[string]any` rồi lấy từng khóa với mặc định, như `RepoConfig.from`, để phân biệt "không khai" với "giá trị 0".
- [ ] **Step 4:** Chạy `go test ./internal/model/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Sửa `templates/` bằng cách thêm `config.yaml` mới theo spec §3.1. Các phần chung giữ chú thích tiếng Việt của bản Java; bỏ các khóa Maven. Thêm `TestTemplateConfigParses`: parse template không lỗi, và `Language == "go"`.
- [ ] **Step 6:** Commit: `feat(model): RepoConfig cho Go, SessionApproveSetting`.

### Task 5: `internal/spec`, kiểu dữ liệu và đọc YAML

**Files:**
- Create: `internal/spec/cases.go`, `internal/spec/design.go`, `internal/spec/ruleid.go`
- Test: `internal/spec/cases_test.go`, `internal/spec/ruleid_test.go`, `internal/spec/extensions_test.go`

**Interfaces:**
- Consumes: `yamlx`, `model.Flow`.
- Produces (tag YAML theo đúng tên trường của Java; `reset_sql` giữ dạng snake case):
  - `Budget{Files, Lines int}` và `Valid() bool`, đúng khi cả hai lớn hơn 0.
  - `Impact{Level string; Labels []string; Reason string}`.
  - `StepSpec{ID, Title string; Scope []string; Budget *Budget}`.
  - `Invariant{Name, SQL, Step string}` và `AppliesTo(caseStep string, stepOrder []string) bool`, đúng như `Invariant.java`.
  - `DesignSpec{Steps []StepSpec; Invariants []Invariant; Impact *Impact}`, `StepOrder() []string`, `ReadDesign(path) (DesignSpec, error)`.
  - `type CaseKind string`: `happy`, `error`, `boundary`. Khi đọc thì trim và không phân biệt hoa thường; giá trị khác thì lỗi.
  - `Given{SQL, Stubs []string}`.
  - `When{Method, Path string; Headers map[string]string; Body any; Repeat, Parallel int}`. Sau khi đọc, `Repeat` và `Parallel` nhỏ hơn hoặc bằng 0 thì thành 1.
  - `DbExpect{Query string; Rows []map[string]any}`.
  - `MessageExpect{Topic string; Value any}`.
  - `Then{Status int; Body any; DB []DbExpect; Messages []MessageExpect; StatusCounts map[int]int}`.
  - `CaseSpec{ID, Step string; Kind CaseKind; Title string; Given Given; When *When; Then *Then; Rules []string}`. `Rules` được chuẩn hóa bằng `NormalizeRuleID`, bỏ chuỗi rỗng, bỏ trùng và giữ thứ tự.
  - `StubSpec{ID string; Request, Response map[string]any}`.
  - `CasesSpec{ResetSQL []string; Stubs []StubSpec; Cases []CaseSpec; Scope []string; Budget *Budget; Impact *Impact; Invariants []Invariant}` và `ReadCases(path) (CasesSpec, error)`.
  - `NormalizeRuleID(raw string) string`, đúng như `RuleId.java`.
  - `EffectiveSteps(flow model.Flow, design *DesignSpec, cases CasesSpec) []StepSpec`: LITE thì một bước `S1` có title `Thay đổi`.
  - `Finding{Code, Message string}`.

  Trường slice khi không khai thì là `nil`. Code gọi dùng `len()`, không phân biệt `nil` với rỗng.

- [ ] **Step 1: Viết test fail.**
  - Port `CasesSpecTest.java` (2 test), `RuleIdTest.java` (2) và `SpecExtensionsTest.java` (9).
  - Đọc template `templates/cases.full.yaml` và `templates/design.yaml` qua `templates.FS`, thay cho `Templates.java`.
  - Thêm:
    - `TestReadCasesSyntaxError`: `cases.yaml` có `cases: [ {id: C1` thì error chứa tên file và `dòng`.
    - `TestUnknownCaseKindFails`: `kind: smoke` thì lỗi.
    - `TestStatusCountsIntKeys`: `statusCounts: {200: 1, 409: 4}` thì đọc ra `map[int]int{200:1, 409:4}`.
- [ ] **Step 2:** Chạy `go test ./internal/spec/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code. Mặc định của `CaseKind` và `When` xử lý trong `UnmarshalYAML` của từng kiểu.
- [ ] **Step 4:** Chạy `go test ./internal/spec/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(spec): kiểu dữ liệu design.yaml và cases.yaml`.

### Task 6: `internal/spec`, markdown và validator

**Files:**
- Create: `internal/spec/markdown.go`, `brd.go`, `designvalidator.go`, `casesvalidator.go`, `stubs.go`
- Test: `internal/spec/brd_test.go`, `designvalidator_test.go`, `casesvalidator_test.go`, `markdown_test.go`

**Interfaces:**
- Consumes: kiểu dữ liệu ở task 5, `model.NFC`, `model.Flow`.
- Produces:
  - `MarkdownSections(md string) (names []string, body map[string]string)`. Thứ tự xuất hiện được giữ trong `names`.
  - `OpenQuestions(md string) []string`.
  - `ValidateBRD(content string) []Finding`.
  - `ValidateDesign(designMd string, spec DesignSpec, levels []string) []Finding`. `levels == nil` thì bỏ kiểm impact.
  - `ValidateCases(cases CasesSpec, steps []StepSpec, flow model.Flow) []Finding`.

  Logic, thứ tự finding, mã lỗi và message **giữ đúng** như `MarkdownSections.java`, `BrdValidator.java`, `DesignValidator.java` và `CasesValidator.java`. Regex dịch sang RE2:
  - mermaid: `(?s)` + "```mermaid[^\n]*\r?\n(.*?)```"
  - bước: `\bS\d+\b`
  - câu hỏi mở: `^\s*[-*]\s*\[ \]\s*(.*\S)?\s*$`

  Tách dòng chấp nhận `\r\n`, `\n` và `\r`, tương đương `\R`.
- **Mới so với Java** (spec §6, stub server): `ValidateCases` kiểm mỗi `StubSpec` chỉ dùng khóa trong tập con được hỗ trợ:
  - `request`: `method`, `url`, `urlPath`, `urlPathPattern`, `queryParameters`, `headers`, `bodyPatterns`. Trong `queryParameters`/`headers`, mỗi giá trị là map có đúng một khóa `equalTo` hoặc `matches`. Mỗi phần tử `bodyPatterns` có đúng một khóa `equalToJson` hoặc `contains`.
  - `response`: `status`, `headers`, `body`, `jsonBody`, `fixedDelayMilliseconds`.

  Mỗi khóa lạ sinh một finding `CASES_STUB_UNSUPPORTED` với message `Stub <id>: khóa "<đường dẫn khóa>" không được hỗ trợ (chỉ hỗ trợ <danh sách>).` Finding này nằm sau các finding cũ của stub đó.

- [ ] **Step 1: Viết test fail.** Port `BrdValidatorTest.java` (5 test), `DesignValidatorTest.java` (8) và `CasesValidatorTest.java` (9). Thêm:
  - `TestBrdNfdHeadingsMatch`: BRD có heading viết dạng NFD (`norm.NFD.String("## Mục tiêu")` …) thì không có `BRD_MISSING_SECTION`.
  - `TestCRLFMarkdown`: BRD và design.md đổi `\n` thành `\r\n` thì cho cùng finding như bản LF. Dùng template hợp lệ: kết quả là không có finding nào.
  - `TestStubUnsupportedKey`: stub có `request.cookies` và `response.fault` thì có 2 finding `CASES_STUB_UNSUPPORTED`.
  - `TestStubSupportedSubsetPasses`: stub dùng đủ mọi khóa được hỗ trợ thì không có finding stub.
- [ ] **Step 2:** Chạy `go test ./internal/spec/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `go test ./internal/spec/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(spec): validator BRD, design, cases và tập con stub`.

### Task 7: `internal/lang` và adapter Go (phần tĩnh)

**Files:**
- Create: `internal/lang/lang.go`, `internal/lang/files.go`, `internal/lang/golang/golang.go`, `internal/lang/golang/gomod.go`, `internal/lang/golang/migrations.go`
- Test: `internal/lang/golang/golang_test.go`

**Interfaces:**
- Consumes: `gitx.Repo.ListTree/FileAt` (task 3).
- Produces:

```go
package lang
// FileSource: tập file để so (base tree hoặc working tree).
type FileSource interface {
    List(ctx context.Context) ([]string, error)
    Read(ctx context.Context, path string) ([]byte, error)
}
func TreeSource(git *gitx.Repo, treeish string) FileSource
func DirSource(root string, files []string) FileSource
type DependencyChanges struct {
    Added []string // đã sắp xếp; "module/path" hoặc "replace a => b"
    NA    string   // khác "" thì không so được, đây là lý do (tiếng Việt)
}
type Adapter interface {
    Name() string
    IsSource(repoPath string) bool
    SampleFileName() string                      // tên file mẫu cho ScopeFlags
    MigrationVersion(fileName string) (int64, bool)
    MigrationOrder(paths []string) []string       // bản sao đã sắp xếp
    DependencyChanges(ctx context.Context, base, now FileSource) DependencyChanges
}
func For(language string) (Adapter, error) // "go" → golang.Adapter; khác → lỗi "Ngôn ngữ chưa hỗ trợ: <x> (hiện có: go)"
```

Adapter Go:
- `Name()` là `go`.
- `IsSource`: đuôi `.go`, không phải `_test.go`, không có đoạn `vendor/` hay `testdata/` ở bất kỳ cấp nào.
- `SampleFileName()` là `aisdlcx.go`.
- `MigrationVersion`: tên file (phần sau `/` cuối) khớp `^(\d+)_.+\.(sql|go)$`. Version dài quá int64 thì là `math.MaxInt64`, như Java.
- `MigrationOrder`: file có version xếp trước, theo version tăng dần; bằng version hoặc không có version thì theo tên. Đây là logic `SqlFacts.compareMigrations` với quy tắc version của goose.
- `DependencyChanges`:
  - Đọc mọi `go.mod` (gốc hoặc `*/go.mod`) bằng `modfile.ParseLax`.
  - Kết quả là các module `require` cộng các chỉ thị `replace` (dạng `replace <old> => <new>`) có ở `now` mà không có ở `base`.
  - `now` không có `go.mod` nào thì `NA = "Repo không có go.mod, bỏ qua kiểm tra dependency."`.
  - Lỗi đọc hoặc parse thì `NA = "Không đọc được go.mod hoặc base tree: <dòng đầu của lỗi>"`.
  - Không bao giờ trả error.

- [ ] **Step 1: Viết test fail.**
  - `TestIsSource`, table: `cmd/server/main.go` đúng; `internal/fee/fee_test.go` sai; `vendor/x/a.go` sai; `pkg/testdata/a.go` sai; `README.md` sai.
  - `TestMigrationOrder`: `["db/migrations/00010_b.sql", "db/migrations/00002_a.sql", "db/migrations/notes.sql", "db/migrations/20261105120000_c.sql", "db/migrations/00002_0.sql"]` thì ra `["db/migrations/00002_0.sql", "db/migrations/00002_a.sql", "db/migrations/00010_b.sql", "db/migrations/20261105120000_c.sql", "db/migrations/notes.sql"]`.
  - `TestMigrationVersionHuge`: `99999999999999999999_x.sql` thì ra `(math.MaxInt64, true)`.
  - `TestDependencyAdded`: base `go.mod` require `github.com/go-chi/chi/v5`; now thêm `github.com/jackc/pgx/v5` và `replace example.com/a => ../a`, thì `Added == ["github.com/jackc/pgx/v5", "replace example.com/a => ../a"]`.
  - `TestDependencyNoGoMod`: `NA` đúng message ở trên.
  - `TestDependencyBrokenGoMod`: `now` có `go.mod` là `module` thì `NA` bắt đầu bằng `Không đọc được go.mod hoặc base tree: `.
  - `TestTreeSourceReadsBaseTree`: repo git tạm có commit `go.mod`; `TreeSource(repo, "HEAD")` liệt kê và đọc được file.
  - `TestForUnknownLanguage`: `For("java")` lỗi với message ở trên.
- [ ] **Step 2:** Chạy `go test ./internal/lang/...`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS cho toàn repo, `gofmt -l` rỗng.
- [ ] **Step 5:** Commit: `feat(lang): interface adapter và adapter Go (phần tĩnh)`.

---

## Ghi chú cho các giai đoạn sau (không làm ở đây)

- Giai đoạn 3 mở rộng `lang.Adapter` thêm `Build`, `UnitTests`, `Start` và `Coverage` (spec §5), và dùng `MigrationVersion` cho logic `DraftMigrations`.
- `review.DependencyFlags` (giai đoạn 3) bọc `DependencyChanges` thành `ReviewFlag` mã `DEPENDENCY_ADDED`: `NA` thành mức NA, `Added` khác rỗng thành mức WARN, giữ message của Java (đổi `pom.xml` thành `go.mod`).
