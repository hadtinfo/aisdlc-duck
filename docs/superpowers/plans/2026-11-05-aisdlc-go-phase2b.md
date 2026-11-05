# AISDLC Go — Giai đoạn 2b (CLI, hook, plugin): Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Có binary `aisdlc` bằng Go chạy được toàn bộ luồng duyệt local (BRD → thiết kế → case → bước Sx → nộp review) và mọi hook của Claude Code. Plugin gọi binary Go thay cho JVM. Bốn lỗ vượt chặn bằng Bash mà bản Java để hở được vá.

**Architecture:**
- CLI nằm trong `internal/cli` (cobra), mỗi lệnh Java một file. `cmd/aisdlc/main.go` chỉ gọi `cli.Main`.
- `cli.Context` giữ mọi thứ lệnh cần từ môi trường (stdin/out/err, console, cwd, env, đồng hồ, git, runner), để test thay được.
- Hai điểm nối còn để trống:
  - `CaseRunner`: chạy case. Bản thật ở giai đoạn 3 (harness); bản mặc định trả lỗi.
  - Phần web có mạng: login, refresh, nộp/duyệt qua web, outbox, flush, policy pull. Ở giai đoạn 4.

  Ở 2b, chế độ duyệt web **chỉ đọc cache đã ký** (hook chạy được). Lệnh nào cần mạng thì báo "cần giai đoạn 4".
- Hook `post-tool-use` và `user-prompt-submit` chỉ phục vụ web: ở 2b im lặng, exit 0.

**Tech Stack:** Go 1.26; thêm **một** dependency `github.com/spf13/cobra` (spec §4). Còn lại là thư viện chuẩn và những gì `go.mod` đã có.

**Spec:** `~/Documents/git/aisdlc/docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md`, các mục §4, §7 và §10 bước 2.

**Nguồn Java** (chỉ đọc):
- CLI: `~/Documents/git/aisdlc/aisdlc-cli/src/{main,test}/java/com/aisdlc/cli/`
- review card và báo cáo eval: `~/Documents/git/aisdlc/aisdlc-core/src/{main,test}/java/com/aisdlc/core/{review,eval}/`
- plugin: `~/Documents/git/aisdlc/plugin/`

**Nền đã có** (giai đoạn 1 và 2a, nhánh `go-phase2a`): `model`, `spec`, `gitx`, `lang`, `yamlx`, `fsx`, `run`, `snapshot`, `gate`, `step`, `review` (DocFacts, ScopeFlags, SqlFacts), `policy`, `risk`. Đọc code để biết chữ ký chính xác. Các điểm đáng chú ý:
- `step.NewService(...).WithGates(...)`: thiếu `WithGates` thì `Start` từ chối.
- `risk.AutoGate` trả `(reason string, manual bool)`.
- `risk.CheckRequirements` nhận con trỏ; `nil` nghĩa là luồng nhẹ.
- `policy.PolicyState.Active` rỗng nghĩa là không có tính năng active.

## Global Constraints

- Module `aisdlc`. Dependency mới duy nhất là `github.com/spf13/cobra`.
- **Tên lệnh, tên cờ, output và mã thoát giữ nguyên như bản Java** (`Main.java`: OK 0, FINDINGS 1, USAGE 2, NOT_DEV 3; hook PreToolUse chặn bằng exit 2 và fail closed).
  - Thông báo tiếng Việt giữ nguyên văn.
  - Chỉ đổi chỗ nhắc Maven, Java hoặc Flyway: "Maven" thành "harness"; đường dẫn migration `src/main/resources/db/migration/` thành `cfg.Migrations.Dir`; `mvn verify` thành `aisdlc eval`.
  - Lỗi không lường trước in `Lỗi: <msg>` và exit 2, như execution handler của `Main.java`.
- Đồng hồ mặc định của `Context` là `func() time.Time { return time.Now().UTC() }`. **Mọi thời gian ghi xuống đĩa là UTC.**
- Mọi file trạng thái ghi qua `fsx.WriteAtomic`. Không còn chỗ nào ghi trạng thái bằng `os.WriteFile`.
- Adapter ngôn ngữ lấy qua `langs.For(cfg.Language)`. Không hard-code Java hay Go trong CLI.
- Test dùng repo git tạm và hermetic: `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `HOME` tạm, `user.name`/`user.email` cục bộ. Test CLI chạy `cli.Run(ctx, args...)` trong tiến trình, không exec binary (trừ task 8).
- **Port test:** mỗi test Java của lớp test được giao thành một `TestXxx` Go cùng tên.
  - Test cần web sống (dùng `FakeWeb` qua HTTP) hoặc cần eval thật thì **không viết**. Liệt kê tên các test đó trong report của task, ghi là "để giai đoạn 4" hoặc "để giai đoạn 3".
  - Test nhắc Maven, Java hoặc Flyway thì đổi sang Go và goose.
- Mọi file qua `gofmt`; `make check` xanh trước mỗi commit. Commit message tiếng Việt, kết thúc bằng:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Agent vượt chặn bằng Bash** (bốn lỗ của bản Java: sửa `aisdlc/config.yaml`, ghi đè file trạng thái của tính năng khác, path có `..`, `cd` rồi dùng path tương đối). Mọi biến thể phải bị chặn. Lệnh chỉ đọc vẫn phải chạy được. Test ở task 3.
2. **Hai tính năng chạy song song, hai session**: snapshot git không bao giờ lỗi vì file tạm của tính năng kia, và cây snapshot không chứa file tạm. Test ở task 1.
3. **Hook gặp input hỏng** (JSON sai, `cwd` ngoài repo, `gates.json` hỏng, không có tính năng active, binary thiếu): PreToolUse ghi thì chặn (fail closed); các event khác im lặng; ngoài repo AISDLC thì không làm gì. Test ở task 7 và task 8.
4. **Dev duyệt trong Claude Code** (`/aisdlc:approve`): phiếu cho phép chỉ dùng được một lần, đúng lệnh, đúng hash, trong 60 giây. Session khác không dùng được phiếu. Test ở task 7.
5. **Hook phải nhanh**: dưới 50 ms (p95) mỗi lần gọi khi không có web. Test ở task 8.

---

### Task 1: Nền cho 2b — snapshot an toàn khi chạy song song, ghi atomic ở `model`

**Files:**
- Modify: `internal/gitx` (hàm snapshot dùng chung cho `SnapshotTree`/`SnapshotTreeExcluding`)
- Modify: `internal/step/service.go` (bỏ vòng retry snapshot)
- Modify: `internal/model/active.go` và chỗ ghi session binding
- Modify: `internal/step/service.go` và `extras.go` (lỗi file hỏng bọc nguyên nhân bằng `%w`)
- Test: `internal/gitx/snapshot_race_test.go`, `internal/model/active_test.go`

**Interfaces:**
- Produces: hành vi mới, chữ ký không đổi.
  - Snapshot loại các file tạm của `fsx.WriteAtomic` (mẫu tên `.<base>.tmp-*`) bằng pathspec `:(exclude,glob)**/.*.tmp-*`.
  - Nếu `git add` vẫn lỗi với output chứa `unable to stat` hoặc `No such file or directory`, thử lại tối đa 5 lần. Lỗi khác trả ngay.
  - `model.WriteActive` và ghi session binding đi qua `fsx.WriteAtomic`.

- [ ] **Step 1: Viết test fail.**
  - `TestSnapshotExcludesAtomicTempFiles`: tạo file `aisdlc/features/A/.gates.json.tmp-123` trong worktree. Cây snapshot (`ListTree`) không chứa file đó.
  - `TestSnapshotRaceWithAtomicWriters`: một goroutine gọi `fsx.WriteAtomic` liên tục vào `aisdlc/features/B/steps.json`. Trong lúc đó gọi `SnapshotTree` 100 lần, không lần nào lỗi.
  - `TestWriteActiveAtomic`: 20 goroutine cùng ghi active; mỗi lần đọc đều ra một giá trị nguyên vẹn.
- [ ] **Step 2:** Chạy `go test ./internal/gitx/ ./internal/model/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Sửa `gitx` và `model` theo Interfaces. Bỏ retry trong `step.Service`. `TestStepsJSONAtomicUnderConcurrentStart` phải xanh mà không còn retry ở `step`.
- [ ] **Step 4:** Chạy `make check` và `go test -race -count=3 ./internal/gitx/ ./internal/step/ ./internal/model/`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `fix(gitx): snapshot bỏ file tạm atomic, ghi active/session atomic`.

### Task 2: Review card và đọc báo cáo eval

**Files:**
- Create:
  - `internal/review/card.go` (ReviewCard và các part, Assumption)
  - `cardbuilder.go` (ReviewCardBuilder, ReviewInput)
  - `caseflags.go`
  - `depflags.go`
  - `cardtext.go` (ReviewCardText của CLI)
- Create: `internal/eval/report.go` (EvalReport, HardCheck, SoftCheck), `agentreview.go` (AgentReview), `markdown.go` (ReportMarkdown), `refresh.go` (`RefreshAgentReview`, `AgentReviewCheck`, `ConfigChanged`)
- Test: `internal/review/{cardbuilder,caseflags,depflags,cardtext}_test.go`, `internal/eval/{agentreview,report}_test.go`

**Interfaces:**
- Consumes: `review.ParseBRD`/`ParseDesign`/`ParseSQL`/`ScopeFlags`/`SqlFlags`/`ExistingTables`; `risk.CheckRequirements`/`ReadRiskFile`; `gate.Gate`; `gitx.Repo`; `lang.Adapter` (`DependencyChanges`, `SampleFileName`, `MigrationOrder`).
- Produces:
  - `review.ReviewInput{Paths model.FeaturePaths; Meta model.FeatureMeta; Flow model.Flow; Config model.RepoConfig; Git *gitx.Repo; Adapter lang.Adapter}`.
  - `review.BuildCard(in ReviewInput, g gate.Gate) (ReviewCard, error)`.
  - `(ReviewCard) WithRiskPreview(RiskPreview) ReviewCard`.
  - Tag JSON của `ReviewCard` giống Jackson, vì `aisdlc review --json` in ra.
  - `review.RenderCard(key string, card ReviewCard, width int, color bool, approval string) string`; chuỗi `approval` rỗng tương ứng `null` của Java.
  - `review.DependencyFlags(...)`: thay chỗ Java đọc `pom.xml` bằng `adapter.DependencyChanges`. Có `NA` thì ra cờ mức `NA` với câu của Java.
  - `eval.ReadEvalReport(path) (EvalReport, error)`, `eval.ReadAgentReview(path) (AgentReview, error)`, `eval.RefreshAgentReview(p model.FeaturePaths) error`, `eval.AgentReviewCheck(path string) SoftCheck`.
  - `eval.RenderReport(r EvalReport, notPassed []run.CaseResult) string`, `eval.WithSoftRow(md string, s SoftCheck) string`, `eval.DiffByStep(records []step.StepRecord, titles map[string]string, git *gitx.Repo, patch bool) (string, error)`.
  - Hằng `eval.ConfigChanged` giữ nguyên văn `EvalService.CONFIG_CHANGED`.

  Phần chạy eval (`EvalService.run`, JaCoCo, Surefire) để giai đoạn 3.

- [ ] **Step 1: Viết test fail.** Port:
  - `ReviewCardBuilderTest` (27 test), `CaseFlagsTest` (4), `DependencyFlagsTest` (5) và `ReviewCardTextTest` (4);
  - `AgentReviewTest` (7) và các test `refreshAgentReview`/`agentReview` trong `EvalServiceTest`.

  `DependencyFlagsTest` viết lại trên `go.mod` qua `golang.Adapter`: thêm `require` thì có cờ; go.mod hỏng thì cờ `NA`.
- [ ] **Step 2:** Chạy `go test ./internal/review/ ./internal/eval/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(review): review card, cờ case và dependency, đọc báo cáo eval`.

### Task 3: Vá bốn lỗ vượt chặn bằng Bash

**Files:**
- Modify: `internal/policy/bash.go`, `internal/policy/hook.go`
- Test: `internal/policy/bash_test.go`, `internal/policy/hook_test.go`

**Interfaces:**
- Produces: `BashCheck(command string, protectedPaths []string, repoRoot, cwd string) (string, bool)`. Thêm tham số `cwd`, là thư mục làm việc của hook (`cwd` trong input JSON). Sửa mọi chỗ gọi.
- **Luật mới [quyết định trong plan]:**
  1. **Danh sách bảo vệ cho Bash** luôn có:
     - `aisdlc/config.yaml` và `aisdlc/risk-policy.yaml`;
     - `.aisdlc/`;
     - mọi file khớp `featureState` của **mọi** tính năng, không chỉ tính năng active;
     - các artifact đã duyệt của tính năng active (như hiện nay).
  2. **Chuẩn hóa token đường dẫn trước khi so:**
     - Mỗi token có `/` hoặc trùng tên một file được bảo vệ thì resolve theo thư mục hiện tại của đoạn lệnh, rồi `filepath.Clean`, rồi đổi sang đường dẫn tương đối với `repoRoot`.
     - Sau đó so khớp đúng đường dẫn, hoặc so tiền tố với thư mục được bảo vệ.
     - Giữ cả phép so chuỗi con cũ của Java. Chặn nếu một trong hai cách khớp.
  3. **Theo dõi `cd`/`pushd`:** tách lệnh theo `&&`, `||`, `;`, `|` và xuống dòng. `cd X` hoặc `pushd X` đổi thư mục hiện tại cho các đoạn sau. Lệnh `cd` không có đối số, hoặc đối số có `$`, thì coi thư mục là không xác định; khi đó đoạn sau có động từ ghi và một token trùng tên file được bảo vệ (theo basename) thì chặn.
  4. **Tool Edit, Write, MultiEdit, NotebookEdit vào `aisdlc/config.yaml` hoặc `aisdlc/risk-policy.yaml` luôn bị chặn**, bất kể cổng và bước. Mã lỗi là `CONFIG_LOCKED`; câu: `aisdlc/config.yaml chỉ dev sửa, ngoài Claude Code.` (thay tên file tương ứng). Kiểm sau luật "đường dẫn ngoài repo" và trước luật "file trạng thái".
  5. Câu chặn Bash dùng lại câu của Java. Đường dẫn trong câu là đường dẫn đã chuẩn hóa.

  Động từ ghi giữ nguyên tập của `BashHeuristics` (redirect `>`/`>>`, `tee`, `sed -i`, `mv`, `cp`, `rm`, ...). Không thêm động từ.

- [ ] **Step 1: Viết test fail.**
  - Sửa `TestBashBypassAttempts`: cả 7 lệnh trong bảng đều phải bị chặn, kể cả `cd aisdlc/features && sed -i s/a/b/ PAY-1/cases.yaml`.
  - Thêm `TestBashHardeningBlocked`, dạng table. Mỗi dòng phải bị chặn:
    - `sed -i 's/unmanaged: .*/unmanaged: ["**"]/' aisdlc/config.yaml`
    - `echo x >> aisdlc/config.yaml`
    - `cd aisdlc && echo x > config.yaml`
    - `echo {} > aisdlc/features/PAY-456/gates.json` (PAY-456 không active)
    - `sed -i s/a/b/ aisdlc/features/../features/PAY-123/gates.json`
    - `cd aisdlc/features/PAY-1 && cp /tmp/c cases.yaml`
    - `pushd aisdlc/features; sed -i s/a/b/ PAY-1/cases.yaml`
    - `cd "$X" && echo {} > gates.json`
    - `rm -rf .aisdlc/grants`
    - `cat x > aisdlc/risk-policy.yaml`
  - Thêm `TestBashHardeningAllowed`. Mỗi dòng phải **được cho qua**:
    - `cat aisdlc/config.yaml`
    - `grep -r foo aisdlc/features/PAY-1/cases.yaml`
    - `cd internal && go test ./...`
    - `git diff aisdlc/config.yaml`
    - `cd aisdlc/features/PAY-1 && cat cases.yaml`
    - `go build ./... > /tmp/build.log`
    - `ls aisdlc/features/*/gates.json`
  - Thêm `TestEditConfigYamlDenied`: Edit, Write, MultiEdit và NotebookEdit vào `aisdlc/config.yaml` và vào `aisdlc/features/../config.yaml` đều bị chặn với mã `CONFIG_LOCKED`, kể cả khi mọi cổng đã duyệt.
- [ ] **Step 2:** Chạy `go test ./internal/policy/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces. Toàn bộ test Java đã port của task 5 giai đoạn 2a vẫn phải xanh. Test nào trước đây khẳng định "Java không chặn" thì sửa thành khẳng định "chặn".
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `fix(policy): vá bốn lỗ vượt chặn bằng Bash, khóa config.yaml`.

### Task 4: Khung CLI — Context, tìm tính năng, `setup`, `init`, `use`, `status`

**Files:**
- Create:
  - `internal/cli/main.go` (`Main`, `Run`, cây cobra, mã thoát)
  - `context.go` (Context, Abort, Feature)
  - `console.go` (DevConsole)
  - `locator.go`
  - `resolver.go` (FeatureResolver)
  - `featureopt.go`
  - `runner.go` (CaseRunner)
  - `webcache.go` (phần đọc cache của WebGates, WebConfig.load, PinnedKey.load; không gọi mạng)
- Create: `internal/cli/cmd_setup.go`, `cmd_init.go`, `cmd_use.go`, `cmd_status.go`
- Test: `internal/cli/testrepo_test.go` (TestRepo, FakeRunner, console giả), `context_test.go`, `setup_test.go`, `flow_init_status_test.go`, `parallel_test.go`

**Interfaces:**
- Produces:
  - `cli.Run(ctx *Context, args ...string) int` và `cli.Main()`; hằng `ExitOK = 0`, `ExitFindings = 1`, `ExitUsage = 2`, `ExitNotDev = 3`.
  - `cli.NewContext(in io.Reader, out, errw io.Writer, console DevConsole, cwd string) *Context`, cùng các hàm `With*`:
    - `WithNow(func() time.Time)`, `WithEnv(map[string]string)`, `WithGit(func(root string) *gitx.Repo)`, `WithRunner(func(root string, cfg model.RepoConfig) CaseRunner)`;
    - `WithColor(bool)`, `WithSessionID(string)`, `WithFeatureOverride(model.FeatureKey)`, `Offline()` (hook dùng: không mạng).
  - `type Abort struct{ Code int; Msg string }`, dùng như `Context.Abort` của Java.
  - `type DevConsole interface{ ReadLine(prompt string) (string, bool) }`.
  - `type CaseRunner interface { RunCases(key model.FeatureKey, steps []string, mode string) (run.CaseRunResult, error); OutDir(key model.FeatureKey) string; OutputTail(key model.FeatureKey, n int) string }`. Bản mặc định trả lỗi `Chạy case cần harness Go (giai đoạn 3).`.
  - `(*Context) Feature() (*Feature, error)` và `FeatureAt(root string, key model.FeatureKey) (*Feature, error)`.
    - `Feature` có các field: `Root`, `Config`, `Key`, `Paths`, `Meta`, `Mode`, `Store`, `Gates`, `Adapter`, `Control` (`*snapshot.FeatureControl`, đọc từ cache đã ký; nil nếu không có).
    - Có các method: `Flow()`, `Git()`, `Design()`, `Cases()`, `Specs()`, `ScopeExtras()`, `Steps()`. `Steps()` đã gọi `WithGates(f.Gates)`.
  - `Mode` theo `WebGates.cachedMode`:
    - đọc mode cache đã ký nếu có, không thì theo `config.approvals`;
    - chế độ WEB dùng `gate.NewWebGateStore(...)`;
    - lệnh có mạng ở chế độ WEB trả `Abort{2, "Chế độ duyệt trên web chưa có trong bản Go (giai đoạn 4)."}`.
  - `(*Context) ConfirmDev(key, action policy.DevCommand, gateHash string) (string, error)` và `Approver(root string) (string, error)`, như Java.
  - `cli.FeatureResolver` có `OpenFeatures`, `Bind`, `WarnSameBranch`, `Resolve` (Resolved, Ambiguous, None), như Java.

- [ ] **Step 1: Viết test fail.**
  - Port `ContextColorTest` (2), `SetupCommandTest` (9) và `ParallelFeatureTest` (15).
  - Port phần `init`/`use`/`status` của `CliFlowTest`. Test nào của `CliFlowTest` thuộc lệnh ở task 5 và 6 thì để cho task đó.
  - `SetupCommandTest` dùng template `config.yaml` của Go (có sẵn trong `templates/`). `--package` giữ như Java nếu còn ý nghĩa; nếu không, ghi lý do trong report.
  - Thêm `TestUnknownErrorPrintsLoiAndExit2` và `TestWebModeNetworkCommandAborts`.
- [ ] **Step 2:** Chạy `go test ./internal/cli/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** `go get github.com/spf13/cobra@latest`, rồi code theo Interfaces. Kiểm `go.mod` không đổi directive `go`.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(cli): khung CLI, tìm tính năng, setup/init/use/status`.

### Task 5: Lệnh luồng cổng — `submit`, `approve`, `reject`, `reopen`, `review`

**Files:**
- Create: `internal/cli/cmd_submit.go`, `cmd_approve.go`, `cmd_reject.go`, `cmd_reopen.go`, `cmd_review.go`, `autoflow.go` (AutoFlow), `featurerisk.go` (FeatureRisk), `policysource.go` (PolicySource: phần đọc file và cache đã ký)
- Test: `internal/cli/flow_local_test.go`, `submit_risk_test.go`, `review_cmd_test.go`, `policysource_test.go`

**Interfaces:**
- Consumes: task 2 (`review.BuildCard`, `review.RenderCard`, `eval.*`), task 4 (`Context`, `Feature`, `CaseRunner`), `gate`, `risk`, `step`, `policy.ConsumeGrant`.
- Produces: lệnh `submit brd|design|cases|review`, `approve <gate>`, `reject <gate> -m`, `reopen <gate>` và `review <gate> [--json]`, hành vi như Java. Thêm hàm `cli.CardFor(ctx, f, g) (review.ReviewCard, error)` (ReviewCommand.card), task 7 dùng lại.
  - `submit design` gọi `CaseRunner.RunCases(key, nil, "migration-check")`, rồi chép `<OutDir>/schema.json` sang `schema-expected.json`. Câu báo dùng "harness" thay cho "Maven".
  - `submit cases` gọi `RunCases(..., "cases-with-drafts")` và `RunCases(..., "cases")` như Java.
  - `submit review` chỉ đọc `eval/report.json` và `review-agent.json` qua `eval.*`.
  - Ở chế độ WEB, phần gửi lên web trả Abort "giai đoạn 4" như task 4.

- [ ] **Step 1: Viết test fail.**
  - Port `FlowLocalTest` (23), `SubmitRiskTest` (12), `ReviewCommandTest` (6), `PolicySourceTest` (6) và phần `submit`/`approve`/`reject`/`reopen` còn lại của `CliFlowTest`.
  - Dùng `FakeRunner` của task 4: ghi `schema.json` và trả kết quả case cấu hình được, như `FakeMaven`.
- [ ] **Step 2:** Chạy `go test ./internal/cli/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(cli): nộp và duyệt cổng, review card, tự duyệt theo risk`.

### Task 6: Lệnh còn lại — `step`, `scope`, `diff`, `risk`, `policy`; `eval` và `web` tạm

**Files:**
- Create: `internal/cli/cmd_step.go`, `cmd_scope.go`, `cmd_diff.go`, `cmd_risk.go`, `cmd_policy.go`, `cmd_eval.go` (tạm), `cmd_web.go` (tạm)
- Test: `internal/cli/step_cmd_test.go`, `risk_cmd_test.go`, `policy_cmd_test.go`, `devonly_test.go`

**Interfaces:**
- Produces:
  - `step start|finish|accept`: `finish` gọi `CaseRunner.RunCases(key, upTo, "cases")` rồi `steps.Finish`.
  - `scope allow <path>`, `diff --by-step [--patch]`, `risk set <level> [--reason]`, `risk reclassify`, `policy show`, `policy diff-preset`.
  - `policy pull`, mọi lệnh con của `web`, và `eval`: có trong cây lệnh, cờ như Java. Khi chạy:
    - `eval`: `Abort{2, "aisdlc eval cần harness Go (giai đoạn 3)."}`;
    - `web` và `policy pull`: `Abort{2, "Lệnh web chưa có trong bản Go (giai đoạn 4)."}`.
  - Lệnh chỉ dev (`approve`, `reject`, `reopen`, `step accept`, `scope allow`, `risk set`/`reclassify`) kiểm console và phiếu cho phép đúng như Java (`NOT_DEV` 3).
- [ ] **Step 1: Viết test fail.**
  - Port `RiskCommandTest` (9), `PolicyCommandTest` (6, trừ phần pull qua web) và `DevOnlyCommandTest` (11).
  - Port phần `step`/`scope`/`diff` của `CliFlowTest`.
  - Thêm `TestEvalAndWebStubsExit2`.
- [ ] **Step 2:** Chạy `go test ./internal/cli/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(cli): bước Sx, scope, diff, risk, policy; eval và web chờ giai đoạn sau`.

### Task 7: Lệnh `hook`

**Files:**
- Create: `internal/cli/hook.go` (đọc input, tìm repo theo `cwd`, các event), `hook_ask.go` (câu hỏi xác nhận lệnh chỉ dev, cấp phiếu)
- Test: `internal/cli/hook_test.go`, `hook_session_test.go`, `session_approve_test.go`

**Interfaces:**
- Consumes: `policy.PreToolUse`/`Stop`/`SessionContext`/`InstructionContext`/`BashCheck`/`IssueGrant`/`ClearGrant`/`WebConfigDecision`, `cli.CardFor`, `Context.Offline()`.
- Produces: `aisdlc hook <event>` với các event:
  - `pre-tool-use`: cho mọi tool, gồm cả Read, Grep và Glob. Nhận cả tên cũ `pre-tool-use-read` như một alias.
  - `stop`, `session-start`.
  - `post-tool-use`, `user-prompt-submit`: im lặng, exit 0 ở 2b.

  Output JSON (`hookSpecificOutput`, `permissionDecision`, `additionalContext`) và mã thoát giữ đúng `HookCommand.java`.
  - Phiếu cho phép của session bị xóa ở **mọi** PreToolUse không phải lệnh được cấp phiếu. Đây là hợp của luật Java và luật `clear_grant` trong `plugin/bin/aisdlc`.
  - Hook luôn chạy `Offline()`: không gọi mạng, chỉ đọc cache.

- [ ] **Step 1: Viết test fail.**
  - Port `HookCommandTest` (25), `HookSessionTest` (8) và `SessionApproveTest` (14). Test về web flush, publisher hoặc control ledger thì để giai đoạn 4 và ghi trong report.
  - Thêm `TestHookBashHardeningEndToEnd`: chạy `hook pre-tool-use` với input Bash `cd aisdlc && echo x > config.yaml` và `cwd` là gốc repo. Kết quả: exit 2 và JSON deny.
  - Thêm `TestGrantNotUsableFromOtherSession`.
  - Thêm `TestReadToolClearsGrant`.
- [ ] **Step 2:** Chạy `go test ./internal/cli/`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(cli): hook PreToolUse, Stop, SessionStart, duyệt trong Claude Code`.

### Task 8: Binary, plugin và tốc độ hook

**Files:**
- Create: `cmd/aisdlc/main.go`
- Create: `plugin/bin/aisdlc` (wrapper bash ngắn), `plugin/hooks/hooks.json`, `plugin/install.sh`. Chép nguyên `plugin/commands/*.md`, `plugin/agents/aisdlc-reviewer.md` và `plugin/test/plugin-files-test.sh` từ repo Java; giai đoạn 5 mới sửa nội dung nhắc Maven.
- Modify: `Makefile` (`build` ra `bin/aisdlc`, thêm `plugin-dist` build 4 nền tảng vào `plugin/lib/aisdlc-<os>-<arch>`), `.gitignore` (`/plugin/lib/`)
- Test: `cmd/aisdlc/main_test.go` (exec binary đã build), `plugin/test/plugin-files-test.sh` (sửa cho layout mới)

**Interfaces:**
- `plugin/bin/aisdlc`:
  - Chọn `plugin/lib/aisdlc-$(uname -s | tr A-Z a-z)-<arch>`, với `x86_64` thành `amd64` và `aarch64`/`arm64` thành `arm64`, rồi `exec` binary đó.
  - Thiếu binary: lệnh thường in `AISDLC: không tìm thấy binary aisdlc cho <os>/<arch>. Chạy plugin/install.sh.` và exit 2.
  - Thiếu binary trong hook: giữ hàm `fail` của bản cũ. Trong repo AISDLC thì `pre-tool-use` exit 2, event khác exit 1; ngoài repo thì exit 0.
  - Bỏ mọi nhánh tối ưu bash khác.
- `hooks.json`: như bản Java, trừ matcher `Read|Grep|Glob` gọi `hook pre-tool-use`.
- `plugin/install.sh`: có Go thì build cho máy hiện tại; không có thì báo cần chạy `make plugin-dist`. Không còn cần JDK.

- [ ] **Step 1: Viết test fail.**
  - `TestBinaryHookOutsideRepoSilent`, `TestBinaryHookDeniesLockedCases` (exit 2) và `TestBinaryVersion`.
  - `TestHookLatencyP95`: build binary, tạo repo tạm có một tính năng active đang ở bước S1, gọi `hook pre-tool-use` 60 lần (Edit một file trong scope). p95 phải dưới 50 ms. Bỏ qua khi `testing.Short()`.
  - `TestLauncherMissingBinaryFailClosed`: chạy `plugin/bin/aisdlc hook pre-tool-use` với `lib/` rỗng, `cwd` trong repo AISDLC. Kết quả: exit 2.
- [ ] **Step 2:** Chạy `go test ./cmd/aisdlc/` và `bash plugin/test/plugin-files-test.sh`. Kết quả mong đợi: FAIL.
- [ ] **Step 3:** Code theo Interfaces.
- [ ] **Step 4:** Chạy `make check`, `make plugin-dist` và `bash plugin/test/plugin-files-test.sh`. Kết quả mong đợi: PASS.
- [ ] **Step 5:** Commit: `feat(plugin): binary Go, launcher theo nền tảng, hook nhanh`.

---

## Ghi chú cho giai đoạn 3 và 4

- **Giai đoạn 3:**
  - `CaseRunner` thật (harness).
  - Lệnh `eval` và `EvalService.run`.
  - Các test Java ghi "để giai đoạn 3" trong report của task 4 đến 7.
- **Giai đoạn 4:**
  - Web client của CLI: WebGates có mạng, WebPublisher, Outbox, BackgroundFlush, FlushLock, Backoff, PinnedKey (pin/rotate), PolicySync, ControlLedger, áp lệnh chờ từ web, `web login|push|flush|approve-token|migrate`, `policy pull`.
  - Hook `post-tool-use` và `user-prompt-submit`.
  - Các test "để giai đoạn 4".
