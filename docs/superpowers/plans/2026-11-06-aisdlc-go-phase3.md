# AISDLC Go — Giai đoạn 3: Harness hộp đen và eval

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `aisdlc eval`, `submit design`, `submit cases` và `step finish` chạy thật trên dịch vụ Go qua một harness hộp đen (build, unit test, Postgres/Kafka bằng testcontainers, goose, stub HTTP, chạy case, coverage), thay `aisdlc-junit` và `MavenRunner` của Java.

**Architecture:**
- Phần phụ thuộc ngôn ngữ khi chạy tiến trình nằm trong interface mới `lang.Runtime`, do `internal/lang/golang` cài: build `-cover`, `go test -json`, khởi động tiến trình, coverage.
- `internal/harness` điều phối một lần chạy: hạ tầng, migration, stub, dịch vụ, case, schema, coverage, rồi ghi `results.json` và `schema.json`. Nó gồm các khối nhỏ có interface để test bằng fake.
- `internal/eval.Service` là bản port của `EvalService.java`, dùng `harness.VerifyResult` thay cho Maven, Surefire và JaCoCo.
- CLI thay `pendingRunner` bằng runner thật và viết lệnh `eval` đầy đủ (local và `--ci`).

**Tech Stack:** Go 1.26 (go directive `1.26.0`), cobra, pgx/v5 v5.12.0, testcontainers-go v0.44.0 (module `postgres` và `kafka`), franz-go v1.22.2 (`kgo`), goose/v3 v3.29.0 (thư viện), chi/v5 v5.5.0 (chỉ cho dịch vụ mẫu e2e).

**Spec:** `~/Documents/git/aisdlc/docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`: §3 (hợp đồng dịch vụ), §5 (adapter), §6 (harness), §9 (kiểm thử), §10 bước 3.

**Nguồn Java đối chiếu** (đọc khi task trỏ tới; repo `~/Documents/git/aisdlc`):
- `aisdlc-junit/src/main/java/com/aisdlc/junit/*.java` và test của nó.
- `aisdlc-core/src/main/java/com/aisdlc/core/eval/*.java` và `aisdlc-core/src/main/java/com/aisdlc/core/run/MavenRunner.java`.
- `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/EvalCommand.java`.

Test Java tương ứng nằm trong `docs/superpowers/deferred-tests.md` (mục "Giai đoạn 3").

## Global Constraints

- Thông báo cho dev bằng tiếng Việt và giữ nguyên câu chữ Java, trừ chỗ nhắc Maven, JUnit, JaCoCo hay Surefire. Những chỗ đó đổi sang Go: "Maven" → "harness", `maven.log` → `harness.log`. Mã lỗi và id check (`CASES_ALL_PASS`, `BUILD`, `SCHEMA_MATCH`...) giữ nguyên.
- Ghi file bằng `fsx.WriteAtomic`. Thời gian là UTC, và mọi chỗ cần giờ hiện tại nhận `now func() time.Time` để test tiêm được.
- `results.json` giữ định dạng `ResultWriter.java`: `{"feature","mode","cases":[{id,step,outcome,message}]}`, thụt 2 dấu cách. `schema.json` là `run.SchemaSnapshot`.
- Ba chế độ của harness giữ tên `cases`, `cases-with-drafts` và `migration-check`, với ngữ nghĩa như spec §6 bước 4.
- Dịch vụ đích không import gì của AISDLC; hợp đồng chỉ gồm env và `aisdlc/config.yaml` (`model.ServiceConfig` đã có).
- Logic riêng của Go chỉ nằm trong `internal/lang/golang`. `harness` và `eval` chỉ gọi `lang.Adapter` và `lang.Runtime`.
- Container được gắn nhãn `aisdlc.run=<runID>` và dừng khi xong, kể cả khi lỗi hay bị hủy ctx. Dịch vụ chạy trong process group riêng (`Setpgid`) và bị kill cả nhóm khi harness thoát.
- Test cần Docker chạy mặc định trong `go test ./...`. Chỉ bỏ qua khi có `AISDLC_SKIP_DOCKER=1`, qua helper `internal/harness/harnesstest.RequireDocker(t)`. Không có Docker mà cũng không đặt biến này thì test **fail** với lý do rõ ràng; không được tự skip.
- Không chạm `~/.config/aisdlc`. Test git hermetic như giai đoạn trước (`GIT_CONFIG_GLOBAL=/dev/null`...).
- Commit có trailer:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Dịch vụ thoát sớm hoặc không bao giờ healthy.** Mọi case được chọn thành `ERROR` với `harness.NotRun`, `harness.log` có 50 dòng log cuối của dịch vụ, và container đã dừng. Test: Task 7 `TestRunServiceNeverHealthy`.
2. **Hủy ctx (Ctrl-C) hoặc quá `timeoutMinutes` giữa chừng.** Không còn tiến trình con hay container nào. Kết quả: mọi case `ERROR` với thông điệp timeout và `ExitCode == harness.ExitTimeout`. Test: Task 7 `TestRunTimeoutKillsServiceAndContainers`.
3. **Dịch vụ phớt lờ SIGTERM.** Harness SIGKILL sau `shutdownTimeoutSeconds`. Coverage khi chạy case là N/A kèm lý do, nhưng kết quả case vẫn giữ. Test: Task 1 `TestStopKillsAfterTimeout`, Task 7 `TestVerifyServiceIgnoresSigterm`.
4. **So DB khác kiểu.** `100` khớp `100.00` (numeric). `timestamptz` khi session ở múi giờ khác UTC vẫn so theo RFC 3339 UTC. `NULL` khớp `null`. Tên cột không phân biệt hoa thường. Test: Task 5 `TestDbCheckNormalization`.
5. **Docker không chạy (đường dùng thật, không phải test).** `RunCases` và `Verify` trả kết quả có thông điệp tiếng Việt `không khởi động được Postgres: ...`, mọi case `ERROR`, không panic. Test: Task 7 `TestRunInfraFailureMarksCasesError`, dùng fake `Infra` trả lỗi.

---

## File structure

```
internal/lang/runtime.go               interface Runtime + BuildResult, UnitResult, Process, Coverage
internal/lang/langs/langs.go           thêm RuntimeFor(name)
internal/lang/golang/build.go          Build (go build -cover -coverpkg)
internal/lang/golang/unittest.go       UnitTests (go test -json), parseTestJSON
internal/lang/golang/process.go        Start/Process (Setpgid, SIGTERM→SIGKILL)
internal/lang/golang/coverage.go       parse coverprofile, covdata textfmt, merge, import path → repo path
internal/eval/coverage.go              ChangedCoverage (thay JacocoCoverage + ChangedLines)
internal/harness/stub/stub.go          stub server (tập con WireMock)
internal/harness/options.go            mode, Options, Select
internal/harness/drafts.go             PrepareDrafts (DraftMigrations)
internal/harness/results.go            WriteResults, ReadResults, NotRun/NoResults/NoRow
internal/harness/jsoncmp.go            CompareJSON (JSONAssert lenient)
internal/harness/http.go               HTTPCaller
internal/harness/executor.go           Executor (CaseExecutor) + interface DB, Stubs, MessageProbe
internal/harness/checks.go             Checks (bất biến + sql-zero)
internal/harness/postgres.go           Infra Postgres (testcontainers), Migrate (goose)
internal/harness/pgdb.go               PgDB: Exec, Check (chuẩn hóa), Violations
internal/harness/schema.go             DumpSchema
internal/harness/kafka.go              Infra Kafka, KafkaProbe (franz-go), NoMessageProbe
internal/harness/harness.go            Harness.Run, Harness.Verify
internal/harness/harnesstest/docker.go RequireDocker, CopySample
internal/harness/testdata/sample/      dịch vụ mẫu Go (module riêng) + aisdlc/features/DEMO-*
internal/eval/service.go               Service.Run (EvalService)
internal/eval/checks.go                các hard/soft check tách khỏi service.go
internal/cli/runner.go                 harnessRunner (thay pendingRunner), Verifier
internal/cli/cmd_eval.go               eval local + --ci
e2e/fixture-service/                   dịch vụ mẫu Go (fee), module riêng
```

---

### Task 1: `lang.Runtime` và adapter Go (build, unit test, tiến trình)

**Files:**
- Create: `internal/lang/runtime.go`, `internal/lang/golang/build.go`, `internal/lang/golang/unittest.go`, `internal/lang/golang/process.go`
- Modify: `internal/lang/langs/langs.go`, `go.mod`
- Test: `internal/lang/golang/runtime_test.go`

**Interfaces:**
- Consumes: `model.ServiceConfig`, `model.UnitTestConfig` (`internal/model/config.go`).
- Produces:
```go
package lang
type BuildResult struct {
    OK     bool
    Binary string // đường dẫn tuyệt đối; "" khi !OK
    Output string // output của trình biên dịch (stdout+stderr)
}
type UnitResult struct {
    Exit    int
    Failed  []string // "<import path>.<TestName>"; package fail không có test nào fail: "<import path>"; đã sắp xếp
    Profile string   // file coverprofile; "" nếu không có
    Output  string   // output text (đã bỏ JSON), để ghi log
}
type Process interface {
    Done() <-chan struct{}                         // đóng khi tiến trình thoát
    ExitErr() error                                // sau Done
    Stop(timeout time.Duration) (graceful bool)    // SIGTERM nhóm, chờ timeout, quá thì SIGKILL nhóm
}
type Runtime interface {
    Build(ctx context.Context, repoRoot string, svc model.ServiceConfig, outDir string) (BuildResult, error)
    UnitTests(ctx context.Context, repoRoot string, ut model.UnitTestConfig, outDir string) (UnitResult, error)
    Start(ctx context.Context, binary string, env map[string]string, coverDir, logPath string) (Process, error)
    Coverage(ctx context.Context, repoRoot, outDir string, unitProfile, coverDir string) (Coverage, error) // Task 2
}
// langs: func RuntimeFor(name string) (lang.Runtime, error) — cùng thông báo lỗi với For khi ngôn ngữ lạ.
```
`error` trả về chỉ dành cho lỗi hạ tầng, ví dụ không có `go` trên PATH. Build hỏng thì `OK=false`, error là nil. Test fail thì `Exit != 0`, error là nil.

- [ ] **Step 1: Viết test.** Mỗi test tạo một module tạm, chỉ dùng thư viện chuẩn, trong `t.TempDir()`, có `go.mod` `module example.com/m`.
  - `TestBuildProducesCoverBinary`: `cmd/server/main.go` hợp lệ, nên `OK`, `Binary` tồn tại và nằm trong `outDir`.
  - `TestBuildFailureIsNotError`: code sai cú pháp, nên `OK==false`, error là nil, `Output` chứa tên file.
  - `TestUnitTestsCollectsFailures`: một test pass, `TestBad` fail, nên `Exit!=0` và `Failed==["example.com/m/p.TestBad"]`.
  - `TestUnitTestsPackageBuildFailure`: file `_test.go` sai cú pháp, nên `Failed` chứa import path của package. Đây là bản Go của `nonZeroExitWithoutTestFailuresFailsBuild` ở tầng adapter.
  - `TestUnitTestsSubtestFailureCountsParent`: subtest fail, nên có `TestX/sub` và `TestX`, đúng như stream JSON.
  - `TestStartAndGracefulStop`: binary bắt SIGTERM rồi thoát 0, nên `Stop(5s)==true`.
  - `TestStopKillsAfterTimeout`: binary bỏ qua SIGTERM và có tiến trình con `sleep`. `Stop(500ms)==false`, và sau đó tiến trình con cũng đã chết (kiểm bằng `syscall.Kill(pid,0)`).
  - `TestStartWritesLog`: stdout và stderr của dịch vụ được ghi vào `logPath`.
- [ ] **Step 2:** Chạy `go test ./internal/lang/golang/ -run 'Build|Unit|Start|Stop'`. Mong đợi FAIL vì chưa có các hàm.
- [ ] **Step 3: Cài đặt.**
  - **Build:** chạy `go build -cover -covermode=atomic -coverpkg=<module>/... -o <outDir>/svc <svc.Main> <svc.BuildFlags...>`, trong đó `<module>` đọc từ `go.mod` ở `repoRoot` bằng `golang.org/x/mod/modfile`. Không có `go.mod` thì `OK=false`, Output là `"không có go.mod ở <repoRoot>"`.
  - **UnitTests:** chạy `go test -json -cover -covermode=atomic -coverpkg=<module>/... -coverprofile=<outDir>/unit.cov <Flags...> <Packages...>`. Đọc stream JSON: `Action=="fail"` có `Test` thì ghi `Package+"."+Test`; không có `Test` và package đó không có test nào fail thì ghi `Package`.
  - **Start:** `exec.CommandContext`, `SysProcAttr{Setpgid:true}`, env gồm `os.Environ()`, `env` và `GOCOVERDIR=coverDir`; `Cancel` kill cả nhóm.
  - Thêm `golang.org/x/mod` vào require trực tiếp nếu cần (đã có).
- [ ] **Step 4:** Chạy `go test ./internal/lang/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(lang): Runtime cho adapter Go — build -cover, go test -json, tiến trình dịch vụ`.

---

### Task 2: Coverage (thay JaCoCo + ChangedLines)

**Files:**
- Create: `internal/lang/golang/coverage.go`, `internal/eval/coverage.go`
- Test: `internal/lang/golang/coverage_test.go`, `internal/eval/coverage_test.go`

**Interfaces:**
- Consumes: Task 1 `lang.Runtime`; `gitx.Repo.ChangedLines(ctx, from, to) (map[string]map[int]bool, error)` (đã có); `lang.Adapter.IsSource`.
- Produces:
```go
package lang
type Coverage struct {
    Lines map[string]map[int]bool // repo path (slash) → dòng đo được → đã chạy
    NA    string                  // khác "" thì không có coverage (lý do tiếng Việt)
}
package eval
type ChangedCoverage struct {
    Covered, Measurable int
    Unmeasured          []string // file nguồn có dòng đổi mà không có trong Coverage.Lines; tối đa 5 tên + "…"
    NA                  string   // "" hoặc lý do
}
func ComputeChangedCoverage(changed map[string]map[int]bool, cov lang.Coverage, isSource func(string) bool, strict bool) ChangedCoverage
func (c ChangedCoverage) Percent() float64 // Measurable==0 → 100
```
- [ ] **Step 1: Viết test.**
  - golang: `TestParseProfileBlocksToLines`. Profile mẫu `mode: atomic\nexample.com/m/p/a.go:3.10,5.2 2 1\nexample.com/m/p/a.go:7.1,7.20 1 0`, module `example.com/m`, cho ra `{"p/a.go": {3:true,4:true,5:true,7:false}}`.
  - golang: `TestMergeProfilesOrsCounts`. Cùng dòng, một profile count 0 và profile kia count 1, kết quả là true.
  - golang: `TestCoverageFromCoverDirAndUnit`. Build binary mẫu của Task 1, chạy nó với `GOCOVERDIR`, Stop êm, rồi `Coverage` gộp với `unit.cov`. Dòng chỉ chạy lúc dịch vụ chạy phải là true.
  - golang: `TestCoverageEmptyCoverDirIsNA`. `coverDir` rỗng và không có unit profile thì `NA != ""`.
  - eval: port 5 test của `JacocoCoverageTest` theo hành vi:
    - `TestChangedCoverageParsesLinesPerFile`,
    - `TestChangedCoverageMapsChangedLines`,
    - `TestChangedCoverageOnlySourceFilesCount` (`_test.go`, `vendor/`, `testdata/` không tính, qua `IsSource`),
    - `TestChangedCoverageStrictUnmeasuredFilesNamedUpToFive` (strict: 6 file không đo được thì `Unmeasured` có 5 tên và `"…"`, dòng của chúng tính là chưa chạy),
    - `TestChangedCoverageNoMeasurableIsNA`.
  - eval: `TestChangedCoverageSoftIgnoresUnmeasured`. Không strict thì file không đo được bị bỏ qua.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.**
  - `Coverage`: chạy `go tool covdata textfmt -i <coverDir> -o <outDir>/cases.cov` nếu `coverDir` có file. Parse `cases.cov` và `unitProfile`, gộp bằng phép OR. Đổi import path thành repo path bằng cách bỏ tiền tố `<module>/`; file ngoài module thì bỏ.
  - Thông điệp NA:
    - không có dữ liệu: `"không có dữ liệu coverage (unit test và case đều không ghi)"`;
    - covdata lỗi: `"covdata lỗi: <msg>"`.
- [ ] **Step 4:** Chạy `go test ./internal/lang/... ./internal/eval/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat: coverage Go (coverprofile + GOCOVERDIR) và coverage dòng thay đổi`.

---

### Task 3: Stub server (tập con WireMock)

**Files:**
- Create: `internal/harness/stub/stub.go`
- Test: `internal/harness/stub/stub_test.go`

**Interfaces:**
- Consumes: `spec.StubSpec{ID, Request, Response map[string]any}`. Tập khóa hợp lệ do `internal/spec/stubs.go` định nghĩa; validator đã chặn khóa lạ.
- Produces:
```go
package stub
type Server struct{ /* httptest-like, 127.0.0.1:0 */ }
func Start() (*Server, error)
func (s *Server) URL() string
func (s *Server) Close()
// Register xóa mọi stub rồi đăng ký các stub có id trong ids (lấy từ all). id không có trong all → lỗi "stub không tồn tại: <id>".
func (s *Server) Register(all []spec.StubSpec, ids []string) error
```
Không khớp stub nào thì trả 404, body `{"error":"không có stub khớp","method":..,"url":..}`. Nhiều stub khớp thì stub đăng ký sau thắng, như WireMock.

- [ ] **Step 1: Viết test** `TestStubMatching`, dạng bảng, mỗi dòng một khóa:
  - `method` (`ANY` khớp mọi method);
  - `url` (path + query khớp chính xác);
  - `urlPath`;
  - `urlPathPattern` (regex khớp toàn chuỗi);
  - `queryParameters.x.equalTo` và `.matches`;
  - `headers.X.equalTo` (tên header không phân biệt hoa thường);
  - `bodyPatterns[].equalToJson` (so JSON bằng nhau, không tính thứ tự khóa) và `.contains`;
  - `response.status`, `headers`, `body` (chuỗi), `jsonBody` (Content-Type `application/json` nếu chưa đặt);
  - `fixedDelayMilliseconds` (trễ ≥ giá trị).

  Thêm `TestRegisterResetsPrevious` và `TestRegisterUnknownID`. Port hai test của `StubRegistryTest.java`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt bằng `net/http` thuần, có mutex bao danh sách stub.
- [ ] **Step 4:** Chạy `go test ./internal/harness/stub/`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(harness): stub server tập con WireMock`.

---

### Task 4: Lõi chạy case (không cần Docker)

**Files:**
- Create: `internal/harness/options.go`, `drafts.go`, `results.go`, `jsoncmp.go`, `http.go`, `executor.go`, `checks.go`
- Test: `internal/harness/options_test.go`, `drafts_test.go`, `results_test.go`, `jsoncmp_test.go`, `executor_test.go`, `checks_test.go`

**Interfaces:**
- Consumes: `spec.CaseSpec`, `spec.CasesSpec`, `spec.StubSpec`, `spec.Invariant.AppliesTo`, `spec.DesignSpec.StepOrder`, `risk.ReadRiskFile`, `risk.Requirement.StrParam`, `model.FeaturePaths`, `lang.Adapter.MigrationVersion`, `run.CaseResult`.
- Produces:
```go
package harness
const (
    ModeCases = "cases"; ModeCasesWithDrafts = "cases-with-drafts"; ModeMigrationCheck = "migration-check"
    NotRun    = "case không được chạy (ứng dụng không khởi động được hoặc suite bị dừng)"
    NoResults = "harness không ghi kết quả"
    NoRow     = "không có kết quả từ harness"
)
func ParseMode(s string) (string, error) // "" → cases; lạ → "mode không hợp lệ: <s> (cases, migration-check hoặc cases-with-drafts)"
func Select(cases []spec.CaseSpec, steps []string) []spec.CaseSpec
// PrepareDrafts: chép nháp trong draftsDir vào outDir, bỏ bản trùng version với file trong mainDir; ok=false khi
// không còn nháp nào.
func PrepareDrafts(draftsDir, mainDir, outDir string, a lang.Adapter) (ok bool, err error)
func WriteResults(outDir, feature, mode string, cases []run.CaseResult) error
// ReadResults: đọc outDir/results.json; case được chọn thiếu dòng → ERROR NoRow; thiếu file/hỏng → mọi case ERROR NoResults.
func ReadResults(outDir string, selected []spec.CaseSpec) []run.CaseResult
func CompareJSON(expected any, actual string) string // "" = khớp
type Response struct{ Status int; Body string }
type Caller interface { Call(ctx context.Context, w spec.When) (Response, error); Timeout() time.Duration }
type HTTPCaller struct{ BaseURL string; RequestTimeout time.Duration } // mặc định 60s; Content-Type application/json nếu case không đặt
type DB interface {
    Exec(ctx context.Context, sql string) error
    Check(ctx context.Context, e spec.DbExpect) (string, error)               // "" = khớp
    Violations(ctx context.Context, sql string, max int) ([]map[string]any, error) // read-only tx
}
type Stubs interface{ Register(all []spec.StubSpec, ids []string) error }
type MessageProbe interface {
    Reset(ctx context.Context) error
    Check(ctx context.Context, want []spec.MessageExpect) (string, error)
}
type NoMessageProbe struct{ Reason string } // NotConfigured = "chưa cấu hình Kafka"
type Executor struct {
    HTTP Caller; DB DB; Stubs Stubs; Messages MessageProbe
    ResetSQL []string; AllStubs []spec.StubSpec; Checks []spec.Invariant; StepOrder []string
}
func (e *Executor) Execute(ctx context.Context, c spec.CaseSpec) run.CaseResult // không panic
// Checks: bất biến design.yaml (FULL) hoặc cases.yaml (lite, bỏ step) + sql-zero của risk.yaml (step = bước cuối).
func Checks(p model.FeaturePaths) (checks []spec.Invariant, stepOrder []string, err error)
```
- [ ] **Step 1: Viết test.** Port theo hành vi, từ các file sau:
  - `HarnessOptionsTest` (4) vào `options_test.go`.
  - `DraftMigrationsTest` (4) vào `drafts_test.go`, với tên file goose như `00002_add_note.sql`.
  - `ResultsFileTest` (3) vào `results_test.go`, cộng các test runner của `MavenRunnerTest`:
    - `TestReadResultsMissingRowIsError`,
    - `TestReadResultsEmptyMakesEveryCaseError`,
    - `TestReadResultsCorruptFileIsNoResults`.
  - `CaseExecutorTest` (10) và `CaseExecutorRiskTest` (16) vào `executor_test.go` và `checks_test.go`. Dùng `httptest.Server` làm dịch vụ và fake `DB`, `Stubs`, `MessageProbe` ghi lại lời gọi. Thông điệp phải trùng Java. Quan trọng nhất:
    - `"reset_sql: <msg>"` và `"given.sql: <msg>"`: `cmd_submit.go` dò hai tiền tố này;
    - `"status: mong đợi X, nhận Y"`;
    - `"statusCounts: mong đợi {..}, nhận {..}"`, map in theo thứ tự khóa tăng dần, dạng `{200=1, 409=1}` như `TreeMap.toString`;
    - `"parallel: n/N request chưa xong sau Ns"`;
    - `"case thiếu when.method, then.status"`;
    - `"Bất biến '<name>' vi phạm: [...]"` và `"Bất biến '<name>' lỗi: <msg>"`;
    - `"messages: ..."`;
    - case có `then.messages` khi probe là `NoMessageProbe` thì ERROR với `Reason`.
  - `jsoncmp_test.go` có `TestCompareJSONLenient`:
    - object kỳ vọng là tập con thì khớp;
    - mảng phải cùng độ dài, không xét thứ tự;
    - số `1` khớp `1.0`;
    - giá trị đơn so sau khi parse;
    - actual không phải JSON thì `"phản hồi không phải JSON hợp lệ: <≤200 ký tự>…"`.
- [ ] **Step 2:** Chạy `go test ./internal/harness/`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.** Đọc `CaseExecutor.java` để lấy thứ tự pha: `reset_sql` → `given.sql` → `given.stubs` → `reset messages` → `gọi HTTP` → `kiểm tra` → `bất biến`.
  - `parallel`: `n` goroutine chờ chung một kênh rồi cùng gửi. Hạn của cả loạt là `Timeout()+5s`.
  - `CompareJSON` tự viết. Thông điệp lệch khi khớp lỏng là `"<đường dẫn JSON>: mong đợi <x>, nhận <y>"`, ví dụ `$.items[0].code`. Không bắt buộc giống chữ của JSONAssert, vì đây là định dạng do builder quyết (spec §11).
- [ ] **Step 4:** Chạy `go test ./internal/harness/`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(harness): chạy case, so JSON, nháp migration, results.json`.

---

### Task 5: Postgres — container, goose, DB checker, schema dump

**Files:**
- Create: `internal/harness/postgres.go`, `pgdb.go`, `schema.go`, `internal/harness/harnesstest/docker.go`
- Modify: `go.mod`, `go.sum` (pgx, testcontainers-go, `testcontainers-go/modules/postgres`, goose)
- Test: `internal/harness/postgres_test.go`, `pgdb_test.go`, `schema_test.go`

**Interfaces:**
- Consumes: Task 4 `DB`; `run.SchemaSnapshot`, `run.NewSchemaSnapshot`.
- Produces:
```go
package harness
type Postgres struct{ URL string /* postgres://... sslmode=disable */ }
func StartPostgres(ctx context.Context, image, runID string) (*Postgres, func(context.Context) error, error) // stop func
func Migrate(ctx context.Context, dbURL string, dirs ...string) error // gộp các dir vào một dir tạm, goose Up, AllowMissing
type PgDB struct{ /* *pgxpool.Pool */ }
func OpenPgDB(ctx context.Context, url string) (*PgDB, error)
func (d *PgDB) Close()
// PgDB cài harness.DB
func DumpSchema(ctx context.Context, url string) (run.SchemaSnapshot, error)
package harnesstest
func RequireDocker(t *testing.T) // AISDLC_SKIP_DOCKER=1 → t.Skip; Docker không tới được → t.Fatal("cần Docker cho test harness: <err>; đặt AISDLC_SKIP_DOCKER=1 để bỏ qua")
```
- [ ] **Step 1: Viết test.** Mọi test gọi `RequireDocker` và dùng chung một container mỗi package qua `TestMain`.
  - `TestMigrateMainThenDrafts`: hai dir, migration goose `-- +goose Up`, nên cả hai bảng đều có.
  - `TestMigrateFailureReturnsError`: SQL lỗi thì error chứa tên file.
  - `TestDbCheckNormalization`, dạng bảng:
    - `numeric(10,2)` có 100.00 khớp YAML `100` và `"100.00"`;
    - `bigint` 5 khớp `5`;
    - `boolean` khớp `true`;
    - `NULL` khớp `null`;
    - `timestamptz` khớp `"2026-01-02T03:04:05Z"` khi session đặt `SET TIME ZONE 'Asia/Ho_Chi_Minh'`;
    - `date` khớp `"2026-01-02"`;
    - `uuid` và `text` so dạng text;
    - cột `FROM_ACCOUNT` khớp `from_account`.
  - `TestDbCheckMessages`: ba thông điệp `DbChecker.java`:
    - `"số dòng: mong đợi N, nhận M"`,
    - `"dòng i: không có cột X"`,
    - `"dòng i cột X: mong đợi W, nhận G"`.
  - `TestViolationsReadOnly`: SQL `DELETE ...` trong Violations trả lỗi, và dữ liệu không đổi.
  - `TestViolationsLimit`: trả tối đa `max` dòng.
  - `TestDumpSchema`: port 3 test `SchemaDumperTest`. Bỏ `goose_db_version`. Dòng có dạng `"<column>:<TYPE>:NULL|NOT NULL"`, TYPE là `upper(data_type)` của `information_schema.columns`, riêng `USER-DEFINED` và `ARRAY` thì là `upper(udt_name)`. Chỉ lấy schema `public`.
- [ ] **Step 2:** Chạy `go test ./internal/harness/ -run 'Migrate|DbCheck|Violations|DumpSchema'`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.**
  - Container `postgres` của testcontainers-go có `Labels{"aisdlc.run": runID}` và wait strategy log "ready to accept connections" ×2.
  - goose: `goose.NewProvider(goose.DialectPostgres, db, os.DirFS(tmp), goose.WithAllowOutofOrder(true))`, với `*sql.DB` lấy từ `pgx/v5/stdlib`.
  - Chuẩn hóa so sánh. Đọc giá trị bằng pgx (kiểu Go gốc), rồi:
    - số (`int*`, `float*`, `pgtype.Numeric`) so bằng `big.Rat`, với cả kỳ vọng là số lẫn chuỗi số;
    - `time.Time`: cột `date` cho `YYYY-MM-DD`, còn lại `UTC().Format(time.RFC3339Nano)`. Kỳ vọng là chuỗi thì parse RFC 3339 rồi so thời điểm;
    - `[16]byte` (uuid) thành dạng chuẩn;
    - còn lại `fmt.Sprint`.

    Trong thông điệp in giá trị đã chuẩn hóa.
- [ ] **Step 4:** Chạy `go test ./internal/harness/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(harness): Postgres testcontainers, goose, so DB theo kiểu, dump schema`.

---

### Task 6: Kafka — container và probe

**Files:**
- Create: `internal/harness/kafka.go`
- Modify: `go.mod`, `go.sum` (franz-go, `testcontainers-go/modules/kafka`)
- Test: `internal/harness/kafka_test.go`

**Interfaces:**
- Consumes: Task 4 `MessageProbe`, `CompareJSON`.
- Produces:
```go
func StartKafka(ctx context.Context, image string, topics []string, runID string) (brokers string, stop func(context.Context) error, err error)
type KafkaProbe struct{ /* kgo.Client */ }
func NewKafkaProbe(brokers string, topics []string, timeout time.Duration) *KafkaProbe // timeout mặc định 5s
func (p *KafkaProbe) Close()
// KafkaProbe cài MessageProbe: Reset tạo consumer group mới "aisdlc-<rand>", offset latest, chờ có partition;
// Check poll (200ms) tới khi mỗi expect có một message khớp CompareJSON (value) trên đúng topic, hết timeout thì
// "messages[i]: không nhận được message khớp trên topic <t> ..." (đuôi theo KafkaMessageProbe.java).
```
- [ ] **Step 1: Viết test** (cần Docker). Port 3 test của `kafka/KafkaCasesTest.java` ở tầng probe:
  - `TestKafkaProbeMatchesProducedMessage`;
  - `TestKafkaProbeIgnoresMessagesBeforeReset`: message gửi trước Reset không được tính;
  - `TestKafkaProbeTimeoutMessage`.

  Thêm `TestStartKafkaCreatesTopics`: các topic khai trong `topics` có sẵn trước khi dịch vụ chạy, và auto-create đang bật.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Image lấy từ `service.kafka.image` (KRaft `apache/kafka:3.8.0`); tạo topic bằng `kadm`.
- [ ] **Step 4:** Chạy `go test ./internal/harness/ -run Kafka`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(harness): Kafka testcontainers và probe franz-go`.

---

### Task 7: Điều phối harness và dịch vụ mẫu

**Files:**
- Create: `internal/harness/harness.go`
- Create: `internal/harness/testdata/sample/` gồm:
  - `go.mod`, `go.sum`: `module example.com/sample`, require pgx và franz-go **cùng version** với `go.mod` chính;
  - `cmd/server/main.go`;
  - `internal/transfer/transfer.go`, `transfer_test.go`;
  - `db/migrations/00001_init.sql`;
  - `aisdlc/config.yaml`;
  - `aisdlc/features/DEMO-1/cases.yaml`, `DEMO-2/...`, `DEMO-3/...`, `db/00002_add_note.sql`. Port từ `aisdlc-junit/src/test/resources`, đổi tên migration sang goose.
- Modify: `internal/harness/harnesstest/docker.go` (thêm `CopySample`)
- Test: `internal/harness/harness_test.go`

**Interfaces:**
- Consumes: mọi thứ của Task 1–6.
- Produces:
```go
const ExitTimeout = 124
type Infra interface { // để test tiêm lỗi; mặc định dùng testcontainers
    Postgres(ctx context.Context, image, runID string) (url string, stop func(context.Context) error, err error)
    Kafka(ctx context.Context, image string, topics []string, runID string) (brokers string, stop func(context.Context) error, err error)
}
type Harness struct {
    Root    string
    Config  model.RepoConfig
    Adapter lang.Adapter
    Runtime lang.Runtime
    Infra   Infra            // nil → testcontainers
    Now     func() time.Time
}
type Options struct { Feature model.FeatureKey; Steps []string; Mode string; Out string }
type RunResult struct { ExitCode int; Cases []run.CaseResult } // 0 mọi case PASSED; 1 có case không PASSED; 2 lỗi harness; ExitTimeout
func (h *Harness) Run(ctx context.Context, o Options) (RunResult, error)
type VerifyResult struct {
    Build     lang.BuildResult
    Unit      lang.UnitResult
    Run       RunResult       // mode cases, mọi bước
    HarnessErr string         // "" hoặc lỗi hạ tầng/khởi động (tiếng Việt)
    Coverage  lang.Coverage   // NA có lý do khi dịch vụ không tắt êm
    TimedOut  bool
}
func (h *Harness) Verify(ctx context.Context, key model.FeatureKey, out string) (VerifyResult, error)
func TimeoutMessage(minutes float64) string // "Harness chạy quá thời gian (N phút), đã dừng" (N in như MavenRunner.minutes)
```
`error` chỉ dành cho lỗi không ghi được `out`. Lỗi hạ tầng vẫn trả kết quả bình thường, trong đó mọi case là `ERROR`.

- [ ] **Step 1: Viết dịch vụ mẫu.** Nó có hành vi như `sample/TransferController.java` và `kafka/KafkaTransferController.java`:
  - `POST /api/transfers` gọi `GET {CORE_BANKING_URL}/accounts/{from}/balance`. Không đủ số dư thì trả 422 `{"code":"INSUFFICIENT_BALANCE"}`; đủ thì INSERT rồi trả `{"status":"OK"}`.
  - Có `KAFKA_BROKERS` thì gửi `transfer.created` với key `from` và value như Java.
  - `GET /healthz` trả 200. Nhận SIGTERM thì `Shutdown` rồi return.
  - Biến `SAMPLE_IGNORE_SIGTERM=1` khiến dịch vụ bỏ qua SIGTERM. `SAMPLE_EXIT_AT_START=1` khiến nó in `"boom"` rồi thoát 3.

  `config.yaml` khai `service.env.port: PORT`, `databaseUrl: DATABASE_URL` và `externalBaseUrls: [CORE_BANKING_URL]`. Chạy `go mod tidy` trong thư mục mẫu và commit `go.sum`.
- [ ] **Step 2: Viết test.** Mọi test cần Docker. Mỗi test chép mẫu vào `t.TempDir()` bằng `CopySample` rồi `git init`.
  - `TestRunDemo1Cases`: port `SampleCasesTest` và `SuiteRun`. C1, C2, C3 `PASSED`; C9 `FAILED` `"status: mong đợi 201, nhận 200"`; `ExitCode==1`; có `results.json` và `schema.json`.
  - `TestRunStepsFilter`: `Steps:["S2"]` chỉ chạy case S2.
  - `TestRunCasesWithDrafts`: port 3 test `DraftsModeTest`. Nháp `00002_add_note` được chạy. Nếu `db/migrations` đã có bản trùng version thì bỏ nháp. Mode `cases` không chạy nháp.
  - `TestRunMigrationCheck`: chỉ có `schema.json`, `cases` rỗng, **không** build; kiểm bằng fake `Runtime` đếm số lần gọi `Build`.
  - `TestRunKafkaCase`: config có `kafkaBrokers`, case có `then.messages` → `PASSED`.
  - `TestRunServiceNeverHealthy`: với `SAMPLE_EXIT_AT_START=1` qua `extraEnv`, mọi case `ERROR NotRun`, `harness.log` có `"boom"`, và không còn container nhãn `aisdlc.run=<id>`. Kiểm bằng `docker ps --filter label=...`.
  - `TestRunTimeoutKillsServiceAndContainers`: `TimeoutMinutes=0.05`, case gọi stub có `fixedDelayMilliseconds: 10000`. Mọi case `ERROR TimeoutMessage`, `ExitCode==ExitTimeout`, không còn container hay tiến trình.
  - `TestRunInfraFailureMarksCasesError`: fake `Infra` trả lỗi, nên mọi case `ERROR` `"không khởi động được Postgres: <err>"` và `ExitCode==2`. Test này không cần Docker.
  - `TestVerifyAllGreen`: Build OK, Unit `Exit==0`, `Coverage.Lines` có `internal/transfer/transfer.go` với dòng INSERT là true.
  - `TestVerifyServiceIgnoresSigterm`: `SAMPLE_IGNORE_SIGTERM=1`, `ShutdownTimeoutSeconds=1`. Case vẫn có kết quả; `Coverage.NA` chứa `"không tắt êm"` nếu không có unit profile, còn nếu có thì chỉ dùng unit profile. Thông điệp bị kill được ghi vào `harness.log`.
- [ ] **Step 3: Cài đặt `Run` theo spec §6.**
  1. Xóa rồi tạo lại `out`, mở `out/harness.log`.
  2. Đọc `cases.yaml` và chọn case; lỗi thì một dòng `HARNESS` `"không đọc được cases.yaml: ..."` như Java.
  3. Build, trừ mode `migration-check`.
  4. Bật Postgres khi `env.databaseUrl != ""` hoặc mode là `migration-check`. Bật Kafka khi `env.kafkaBrokers != ""`.
  5. `Migrate`: dir chính `migrations.dir`; với `cases-with-drafts` và `migration-check` thì thêm `PrepareDrafts(... out/drafts)`.
  6. Với `migration-check`, dump schema, ghi file rồi dừng.
  7. Bật stub. Lấy cổng trống (`net.Listen("tcp","127.0.0.1:0")` rồi đóng). Env gồm `extraEnv`, `port`, `databaseUrl`, `kafkaBrokers`, và mọi `externalBaseUrls` trỏ tới URL stub.
  8. `Start` với `GOCOVERDIR=out/cover`. Poll `healthPath` mỗi 200ms trong `startTimeoutSeconds`. Tiến trình thoát hay quá giờ thì mọi case `NotRun`, và 50 dòng cuối log dịch vụ được ghi vào `harness.log`.
  9. Chạy `Executor` cho từng case. Nếu `Checks` lỗi thì mọi case `ERROR` `"không đọc được bất biến (design.yaml/cases.yaml/risk.yaml): ..."`.
  10. `DumpSchema`, `Stop`, rồi `WriteResults` và schema.

  `defer` dừng container, stub và tiến trình bằng một ctx riêng (`context.WithoutCancel`, hạn 30s) để vẫn dọn được khi ctx gốc bị hủy. Timeout: `ctx` có hạn `TimeoutMinutes`; hết hạn thì ghi mọi case `ERROR TimeoutMessage` và `ExitCode=ExitTimeout`.

  `Verify` chạy `Build`, rồi `UnitTests`, rồi `Run` (mode `cases`, không lọc bước, coverage bật), rồi `Coverage`. Build hỏng thì không chạy unit test và case, và mọi case là `ERROR` `"build lỗi"`.
- [ ] **Step 4:** Chạy `go test ./internal/harness/...`. Mong đợi PASS. Chạy thêm `AISDLC_SKIP_DOCKER=1 go test ./internal/harness/...`: test cần Docker phải SKIP, test fake vẫn PASS.
- [ ] **Step 5:** Commit `feat(harness): điều phối chạy case trên dịch vụ Go, dịch vụ mẫu`.

---

### Task 8: `eval.Service` (port EvalService)

**Files:**
- Create: `internal/eval/service.go`, `internal/eval/checks.go`
- Test: `internal/eval/service_test.go`, `internal/eval/ciauto_test.go`

**Interfaces:**
- Consumes:
  - Task 7 `harness.VerifyResult`, `harness.ReadResults`;
  - Task 2 `ComputeChangedCoverage`;
  - các hàm đã có trong `internal/eval` (`NewEvalReport`, `RenderReport`, `AgentReviewCheck`, `RefreshAgentReview`, `EvalReport.Write`);
  - `gate.Service`, `step.Service`, `risk.ReadRiskFile`, `risk.RequiredByDesign`, `gitx.Repo` (`MergeBaseTree`, `SnapshotTreeExcluding`, `ChangedLines`, diff/name-status đã có);
  - `step.ScopeMatches`, `step.ScopeExtras`.
- Produces:
```go
type Verifier interface {
    Verify(ctx context.Context, key model.FeatureKey, out string) (harness.VerifyResult, error)
}
// CIApprovalSource: approval lấy từ web khi chạy CI (giai đoạn 4 cài). nil → không có web.
type CIApprovalSource interface {
    Approvals(ctx context.Context, key model.FeatureKey, tree string) (CIApprovals, error)
}
type CIApprovals struct { Mode string; Approvals map[gate.Gate]gate.Approval } // như CiApprovals.java
type Service struct {
    Paths model.FeaturePaths; Meta model.FeatureMeta; Config model.RepoConfig; Git *gitx.Repo
    Verifier Verifier; Gates *gate.Service; Steps *step.Service; Specs []spec.StepSpec
    Adapter lang.Adapter; Now func() time.Time
}
const ConfigChanged = " bị sửa trong thay đổi này — sửa cấu hình phải ở MR riêng"
func (s *Service) Run(ctx context.Context, ci bool, baseRef string, web CIApprovalSource) (EvalReport, error)
func AutoApprovals(seq []gate.Gate, approval func(gate.Gate) (gate.Approval, bool), riskPath string) []HardCheck
```
- [ ] **Step 1: Viết test.** Port 35 test của `EvalServiceTest` và 5 test của `CiAutoApprovalTest`, giữ tên dạng `TestEvalAllGreenPasses`... Dùng fake `Verifier`: nó ghi `results.json` và `schema.json` vào `out`, rồi trả `VerifyResult` dựng sẵn, theo cách `EvalServiceTest.java` dùng Maven giả. Cách đổi test Java sang Go:
  - `oldTestFailureFailsBuild` → `Unit.Failed` không rỗng, nên BUILD fail và liệt kê test.
  - `onlySuiteFailureDoesNotFailBuild` → case fail, Unit sạch, nên BUILD pass.
  - `nonZeroExitWithoutTestFailuresFailsBuild` → `Unit.Exit!=0` và `Failed` rỗng.
  - `suiteFailureWhileEveryCasePassedFailsBuild` → `HarnessErr!=""` dù mọi case pass.
  - `exitZeroStillReadsFreshSurefireReports` → `Unit.Exit==0` nhưng `Failed` không rỗng, nên BUILD fail.
  - Thêm `TestEvalBuildFailure`: `Build.OK=false`.
  - `coverageOfChangedLinesFromJacoco` và các test `riskCoverage_*` dùng `VerifyResult.Coverage`:
    - "không có báo cáo JaCoCo" thành `Coverage.NA != ""`, giữ thông điệp riêng của Java nhưng đổi chữ JaCoCo thành "coverage";
    - "module không có báo cáo" thành file không có trong `Coverage.Lines`.
  - Chỉ đổi chữ thông điệp BUILD có nhắc Maven: `"go build lỗi"`, `"unit test fail: <≤10 tên>"`, `"go test thoát mã N"`, `"harness lỗi: <HarnessErr>"`, `"quá thời gian"` khi `TimedOut`.
  - `CASES_ALL_PASS` (số pass/tổng, ≤10 id), `CASES_LOCKED`, `SCHEMA_MATCH`, `SCOPE`, `CHECK:*`, `RISK_FILE`, coverage, risk section, budgets và `CONFIG_CHANGED` giữ chữ Java.
- [ ] **Step 2:** Chạy `go test ./internal/eval/`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.** Theo đúng thứ tự trong `EvalService.run` (đoạn trích ở đầu file Java): `from` và `to` lấy trước khi Verify, rồi gọi `Verifier.Verify(ctx, key, out)` với `out = <root>/.aisdlc/run/<KEY>`.
  - `check` (lệnh `checks` của config) chạy qua `sh -c`, log vào `out/check-<name>.log`.
  - Với `web == nil` thì luôn coi `webMode=false`.
  - Ghi `eval/report.json` và `report.md` như `write()` Java.
- [ ] **Step 4:** Chạy `go test ./internal/eval/`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(eval): EvalService trên harness Go`.

---

### Task 9: CLI — runner thật và lệnh `eval`

**Files:**
- Modify: `internal/cli/runner.go`, `internal/cli/cmd_eval.go`, `internal/cli/context.go` (thêm `verifier` và `WithVerifier`), `internal/cli/main.go` (cài `signal.NotifyContext` cho SIGINT/SIGTERM)
- Test: `internal/cli/eval_cmd_test.go`, `internal/cli/runner_test.go`

**Interfaces:**
- Consumes: Task 7 `harness.Harness`, Task 8 `eval.Service`, `langs.For`, `langs.RuntimeFor`, `run.CaseRunResult`.
- Produces:
```go
// runner thật: RunCases → harness.Run; OutDir không đổi; OutputTail đọc harness.log.
func harnessRunner(root string, cfg model.RepoConfig) CaseRunner
func (c *Context) WithVerifier(f func(root string, cfg model.RepoConfig) eval.Verifier) *Context
func (c *Context) Verifier(root string, cfg model.RepoConfig) eval.Verifier
```
`RunCases` trả `run.CaseRunResult{ExitCode: RunResult.ExitCode, Cases: RunResult.Cases}`. Mode lạ thì `abort(2, <lỗi của ParseMode>)`. Xóa `EvalPhase3Msg` và `pendingRunner`.

- [ ] **Step 1: Viết test.** Port 10 test của `EvalCommandTest`: `TestEvalWritesReportAndExitsByResult`, `TestEvalCi...`. Fake `Verifier` tiêm qua `WithVerifier`. Repo git hermetic dựng bằng helper sẵn có trong `testrepo_test.go`. Mã thoát: 0 khi mọi hard check pass, 1 khi không. Thông báo giữ chữ của `EvalCommand.java`. Riêng:
  - CI không có tính năng nào đổi thì in thông báo Java và thoát 0;
  - code đổi mà không có tính năng thì thoát 1, kèm gợi ý `aisdlc init <KEY>`.

  Thêm `TestRunnerTimeoutMarksCasesError`. Dùng `harness.Harness` có fake `Runtime` (Start trả process không bao giờ healthy) và fake `Infra`; `TimeoutMinutes` rất nhỏ. Kết quả: mọi case ERROR `TimeoutMessage`. Đây là bản port của `timeoutKillsMavenAndMarksCasesError`.

  Thêm `TestRunnerClearsStaleResults`: `results.json` cũ trong `OutDir`, harness lỗi trước khi ghi, nên kết quả là `NoResults` chứ không dùng file cũ.
- [ ] **Step 2:** Chạy `go test ./internal/cli/ -run 'Eval|Runner'`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.** `cmd_eval` theo `EvalCommand.java`:
  - Không có `--ci` thì cần tính năng active.
  - Có `--ci`:
    - so `MergeBaseTree(base)` với working tree;
    - không tin config của chính thay đổi: `config.yaml` bị sửa thì fail với `ConfigChanged`, còn `unmanaged` và `ciRequireFeature` lấy từ config ở base;
    - eval từng tính năng có thay đổi.
  - `web` luôn là nil ở giai đoạn này.
  - Ghi lỗi cho người dùng thấy, không panic.
  - Thay mọi chỗ `pendingRunner` bằng `harnessRunner`. Test cũ dùng `WithRunner` vẫn chạy.
- [ ] **Step 4:** Chạy `go test ./internal/cli/...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(cli): eval và chạy case qua harness Go`.

---

### Task 10: Dịch vụ mẫu e2e, Makefile, tài liệu

**Files:**
- Create: `e2e/fixture-service/` (module `example.com/fixture`):
  - `go.mod`, `go.sum`, `cmd/server/main.go`;
  - `internal/fee/fee.go` và `fee_test.go`: phí = `amount*15/10000`;
  - `internal/fee/handler.go`: chi, `GET /api/v1/fees?amount=`, `amount<0` thì 400 `{"code":"VALIDATION_ERROR"}`, ngược lại 200 `{"amount":..,"fee":..}`;
  - `db/migrations/00001_init.sql`: bảng `account` như `V1__init.sql`;
  - `aisdlc/config.yaml` theo spec §3.1;
  - `aisdlc/risk-policy.yaml`: chép từ Java;
  - `.gitignore`.
- Create: `internal/harness/fixture_test.go`
- Modify: `Makefile`: thêm `test-short: AISDLC_SKIP_DOCKER=1 go test ./...`. `check` vẫn gọi `test` đầy đủ.
- Modify: `docs/superpowers/deferred-tests.md`: mục "Giai đoạn 3" ghi đã port, mỗi test Java ghép với tên test Go.

**Interfaces:**
- Consumes: Task 7 `harness.Harness`.

- [ ] **Step 1: Viết test** `TestFixtureServiceFeeCases` (cần Docker). Nó chép `e2e/fixture-service` vào thư mục tạm, `git init`, và tạo tính năng `FEE-T` có `cases.yaml` gồm hai case:
  - `GET /api/v1/fees?amount=1000000` → 200 `{"fee":1500}`;
  - `amount=-1` → 400.

  Gọi `Harness.Verify`. Kỳ vọng: cả hai case `PASSED`, Build OK, unit test xanh, `Coverage.Lines["internal/fee/fee.go"]` có dòng tính phí là true.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL vì chưa có fixture.
- [ ] **Step 3:** Viết fixture-service. Chạy `go mod tidy` trong đó và commit `go.sum`.
- [ ] **Step 4:** Chạy `go test ./internal/harness/ -run Fixture`, rồi `make check`. Mong đợi PASS và `CHECK_OK`, hoặc exit 0.
- [ ] **Step 5:** Commit `feat(e2e): dịch vụ mẫu Go (fee) cho harness; tài liệu test đã port`.

---

## Ngoài phạm vi (giai đoạn sau)

- `CIApprovalSource` qua web, `webPublisher.afterCommand` sau eval, event của eval: giai đoạn 4.
- Viết lại `e2e/run-e2e.sh`, lời giải PAY-1, FIX-2, FEE-3 bằng Go, template `gitlab-ci.aisdlc.yml`, skill `cases` và `step` ghi quy tắc so DB mới: giai đoạn 5.
