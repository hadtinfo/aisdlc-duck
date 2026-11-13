# Quay về mốc (`aisdlc rewind`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lệnh chỉ-dev `aisdlc rewind [mốc]` đưa một tính năng về đầu một mốc (brd/design/cases/Sx/review), xoá sạch mọi thứ sau mốc, giữ ref dự phòng; ở chế độ web, server khép lần làm cũ vào archive xem được trên web.

**Architecture:** Gói mới `internal/rewind` chứa phần thuần (danh sách mốc, tính kế hoạch xoá) và phần thực thi (backup, khôi phục code, xoá artifact/trạng thái). CLI `cmd_rewind.go` hiển thị mốc, xem trước, xác nhận qua `ConfirmDev`, ở chế độ web gọi server trước khi xoá. Server thêm `features.attempt`, bảng `feature_rewinds`, cột `rewind_id` đánh dấu bản nộp/feedback đã khép; endpoint `POST /api/gates/rewind`; ingest bỏ qua snapshot của lần làm cũ; API + UI archive.

**Tech Stack:** Go (cobra, pgx, chi, goose migrations), git CLI qua `internal/gitx`, React 18 + TypeScript + vitest ở `web/ui`.

**Spec:** `docs/superpowers/specs/2026-11-13-feature-rewind-design.md`

## Global Constraints

- Mốc luồng đầy đủ: `brd`, `design`, `cases`, `S1`…`Sn`, `review`; luồng nhẹ: `cases`, `S1`…`Sn`, `review`. Không phân biệt hoa thường. Chỉ liệt kê mốc đã đạt tới.
- Xoá sạch artifact sau mốc theo bảng "Mốc" của spec; luôn giữ `feature.yaml`, `feedback.md`.
- Code: cả working tree về snapshot mốc, **trừ** `aisdlc/features/` (mọi tính năng), `.aisdlc/`, và path khớp `cfg.Unmanaged`. Không đổi `HEAD`, không viết lại lịch sử.
- Mốc cổng (brd/design/cases) dùng snapshot đầu S1; chưa có S1 thì không đổi code. Mốc `review` không đổi code.
- Ref checkpoint: `refs/aisdlc/checkpoints/<KEY>/<Sx>`; ref dự phòng: `refs/aisdlc/backup/<KEY>/<yyyymmdd-hhmm>` (giờ địa phương; trùng thì thêm `-2`, `-3`). Không push, không tự dọn.
- Xác nhận: `ctx.ConfirmDev(f.Key, invocation(ctx, "rewind"), "")`; không có cờ bỏ qua. Thoát 3 khi không console/không phiếu, như `reopen`.
- Dòng `feedback.md`: `## REWIND — quay về <mốc> bởi <người> lúc <thời điểm> (dự phòng: <ref>)` (thời điểm theo định dạng `s.instant()` hiện có của `gate.Service`).
- Câu kết: `Đã quay <KEY> về đầu <mốc>. Tiếp: <lệnh>` với lệnh: brd → `/aisdlc:brd`, design → `/aisdlc:design`, cases → `/aisdlc:cases`, Sx → `aisdlc step start Sx`, review → `/aisdlc:review`.
- Mọi chuỗi người dùng thấy đi qua `i18n.T/Tf` (bản gốc tiếng Việt) và có bản dịch trong `internal/i18n/en_cli.go`; chuỗi web UI có key trong cả `web/ui/src/i18n/vi.ts` và `en.ts`. Kiểm tra hiện có (`english_test.go`, docs-check) phải qua.
- Web: web **không** có nút quay về. Archive giữ vĩnh viễn; không đụng logic dọn `events` 180 ngày.

## Review Focus

1. **Working tree bẩn lúc rewind** (dev đang sửa dở file chưa commit): phải nằm trong ref dự phòng trước khi bị ghi đè → test ở Task 4 (`TestApply_DirtyTreeBackedUp`).
2. **Snapshot mốc đã bị `git gc` xoá** (tính năng cũ, không có checkpoint ref): báo rõ, không xoá gì → Task 3 (`TestPlan_MissingTree`).
3. **Đứt giữa chừng sau khi server đã ghi nhận** (mất điện sau bước 4): chạy lại cùng mốc phải hoàn tất, server không mở lần làm thứ hai → Task 4 (`TestApply_Idempotent`) và Task 7 (`TestRewind_SameRequestIDNoDouble`).
4. **Agent tự gọi `aisdlc rewind`** (trực tiếp, `bash -c`, qua biến): bị chặn → Task 5 (`TestHook_RewindIsDevOnly`).
5. **Máy/clone khác đẩy snapshot lần làm cũ sau khi đã quay về**: server bỏ qua, board không nhảy về trạng thái cũ → Task 8 (`TestIngest_OldAttemptIgnored`).

---

### Task 1: gitx — commit tree, ref, khôi phục file

**Files:**
- Create: `internal/gitx/refs.go`
- Test: `internal/gitx/refs_test.go`

**Interfaces:**
- Produces:
  - `func (r *Repo) CommitTree(ctx context.Context, tree, msg string) (string, error)` — `git commit-tree <tree> -m msg`, không cha.
  - `func (r *Repo) UpdateRef(ctx context.Context, ref, sha string) error`
  - `func (r *Repo) ResolveRef(ctx context.Context, ref string) (string, bool, error)` — sha của ref; false nếu không có.
  - `func (r *Repo) ObjectExists(ctx context.Context, sha string) bool` — `git cat-file -e`.
  - `func (r *Repo) RestorePaths(ctx context.Context, tree string, changes []FileChange) error` — với mỗi change: path có trong `tree` thì ghi nội dung từ tree (giữ mode thực thi), không có thì xoá file khỏi đĩa; rename xử lý cả `OldPath`. Không đụng index của repo, không đổi `HEAD`.

- [ ] **Step 1: Viết test thất bại** trong `refs_test.go` (dùng helper repo tạm như các test hiện có của gói):
  - `TestCommitTreeAndRef`: snapshot → `CommitTree` → `UpdateRef("refs/aisdlc/checkpoints/K/S1")` → `ResolveRef` trả đúng sha; `git gc --prune=now` xong `ObjectExists(tree)` vẫn true.
  - `TestRestorePaths`: snapshot T0 với `a.go`="1"; sửa `a.go`="2", thêm `b.go`, xoá `c.go`; `Diff(T0, now)` → `RestorePaths(T0, changes)` → `a.go`="1", `b.go` không còn, `c.go` có lại; `git rev-parse HEAD` không đổi.
- [ ] **Step 2:** `go test ./internal/gitx -run 'TestCommitTreeAndRef|TestRestorePaths'` → FAIL (undefined).
- [ ] **Step 3:** Cài đặt các hàm trong `refs.go` qua `r.run`. `RestorePaths` đọc nội dung bằng `git cat-file blob <tree>:<path>` / mode bằng `git ls-tree`.
- [ ] **Step 4:** Chạy lại → PASS.
- [ ] **Step 5:** `git commit -m "feat(gitx): commit-tree, refs và khôi phục file từ tree"`

### Task 2: step — checkpoint ref, ScopeEntry.Step, cắt bước

**Files:**
- Modify: `internal/step/service.go` (`Start`), `internal/step/extras.go`, `internal/step/pending.go:80`, `internal/cli/cmd_scope.go:55`
- Test: `internal/step/service_test.go`, `internal/step/extras_test.go` (tạo nếu chưa có)

**Interfaces:**
- Consumes: Task 1 `CommitTree`, `UpdateRef`.
- Produces:
  - `func CheckpointRef(key model.FeatureKey, stepID string) string` → `"refs/aisdlc/checkpoints/<KEY>/<Sx>"`.
  - `Service.Start` ghi checkpoint ref khi tạo bước mới (không ghi lại khi mở lại bước `AWAITING_DEV`). Lỗi ghi ref → trả lỗi, không lưu bản ghi.
  - `ScopeEntry` thêm `Step string \`json:"step,omitempty"\``.
  - `func (s *ScopeExtras) Add(relPath, approver, stepID string, at time.Time) error` (thêm tham số `stepID`; caller truyền bước đang mở, rỗng nếu không có).
  - `func (s *ScopeExtras) RemoveSteps(ids []string) error` — xoá mục có `Step` thuộc `ids`; giữ mục `Step` rỗng.
  - `func (s *Service) Truncate(fromID string) error` — bỏ bản ghi của `fromID` và mọi bước sau theo thứ tự spec; `fromID` không có bản ghi thì no-op.

- [ ] **Step 1: Test thất bại:**
  - `TestStart_WritesCheckpointRef`: `Start("S1")` → `ResolveRef(CheckpointRef("K","S1"))` trỏ commit có tree = `rec.StartTree`.
  - `TestScopeExtras_RemoveSteps`: mục `{a,S2}`, `{b,S1}`, `{c,""}` → `RemoveSteps(["S2","S3"])` → còn `b`, `c`.
  - `TestTruncate`: S1, S2 ACCEPTED, S3 IN_PROGRESS → `Truncate("S2")` → chỉ còn S1.
- [ ] **Step 2:** `go test ./internal/step` → FAIL.
- [ ] **Step 3:** Cài đặt; cập nhật hai caller của `Add` (bước đang mở lấy từ `Service.Current()`; ở `pending.go` dùng `a.Step`).
- [ ] **Step 4:** `go test ./internal/step ./internal/cli` → PASS.
- [ ] **Step 5:** Commit `feat(step): checkpoint ref đầu bước, scope-extra ghi bước, Truncate`.

### Task 3: rewind — mốc và kế hoạch (thuần)

**Files:**
- Create: `internal/rewind/plan.go`
- Test: `internal/rewind/plan_test.go`

**Interfaces:**
- Consumes: `gate.Sequence`, `gate.Gate.Artifacts`, `step.StepRecord`, `spec.StepSpec`, `step.CheckpointRef`.
- Produces:
  ```go
  type Checkpoint struct{ ID string; Gate gate.Gate; Step string } // ID: "brd"|"design"|"cases"|"S2"|"review"
  type Input struct {
      Key model.FeatureKey; Flow model.Flow; Paths model.FeaturePaths
      Steps []spec.StepSpec; Records []step.StepRecord
      Reached func(gate.Gate) bool        // cổng đã có bản nộp/duyệt hoặc là cổng đang mở
      RiskFromDesign bool                 // luồng đầy đủ: risk.yaml sinh ở submit design
  }
  type Plan struct {
      Target Checkpoint
      ClearGates []gate.Gate      // từ mốc trở đi (mốc Sx/review: chỉ REVIEW)
      DropSteps []string          // Sk..Sn có bản ghi
      DeletePaths []string        // artifact tuyệt đối sẽ xoá (chỉ những file đang tồn tại)
      ScopeSteps []string         // bước cần RemoveSteps
      CodeTree string             // "" = không đổi code
      OwnChanges, ForeignChanges []gitx.FileChange // điền ở Task 4 (cần git)
  }
  func Checkpoints(in Input) []Checkpoint
  func Parse(in Input, arg string) (Checkpoint, error)   // không hợp lệ/chưa đạt tới → lỗi liệt kê mốc hợp lệ
  func Build(in Input, cp Checkpoint) (Plan, error)
  ```
  `CodeTree`: mốc `Sk` → `StartTree` của Sk; mốc cổng → `StartTree` của S1 nếu có bản ghi, ngược lại ""; `review` → "".
  `DeletePaths` theo bảng spec; `risk.yaml` + file kết quả risk xoá ở `brd`/`design`, ở `cases` chỉ khi `!RiskFromDesign`.

- [ ] **Step 1: Test thất bại** (bảng test cho cả hai luồng):
  - `TestCheckpoints_FullFlow`: BRD/DESIGN/CASES đã duyệt, S1–S3 có bản ghi → `brd,design,cases,S1,S2,S3,review`; chưa có S3 → không có `S3`.
  - `TestCheckpoints_LiteFlow`: bắt đầu bằng `cases`.
  - `TestBuild_Table`: mỗi mốc → `ClearGates`, `DropSteps`, `DeletePaths` (so tên file tương đối), `CodeTree` đúng bảng spec; `cases` luồng đầy đủ giữ `risk.yaml`.
  - `TestParse_CaseInsensitiveAndUnknown`: `"S2"`/`"s2"`/`"Design"` hợp lệ; `"S9"` lỗi có danh sách mốc.
- [ ] **Step 2:** `go test ./internal/rewind` → FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(rewind): danh sách mốc và kế hoạch xoá`.

### Task 4: rewind — thực thi local

**Files:**
- Create: `internal/rewind/apply.go`
- Test: `internal/rewind/apply_test.go`

**Interfaces:**
- Consumes: Task 1, Task 2, Task 3.
- Produces:
  ```go
  type Deps struct {
      Git *gitx.Repo; Paths model.FeaturePaths; Cfg model.RepoConfig
      Steps *step.Service; Extras *step.ScopeExtras
      ClearGate func(gate.Gate) error  // local: LocalGateStore.Clear; web: no-op (server đã làm) + Refresh
      AppendFeedback func(text string) error
      Now func() time.Time; By string
  }
  func ResolveCode(ctx context.Context, d Deps, p *Plan) error  // điền OwnChanges/ForeignChanges; lỗi nếu CodeTree != "" và !ObjectExists (thử checkpoint ref trước)
  func Backup(ctx context.Context, d Deps) (ref string, err error) // snapshot toàn working tree (gồm thư mục tính năng) → commit → refs/aisdlc/backup/<KEY>/<yyyymmdd-hhmm>
  func Apply(ctx context.Context, d Deps, p Plan, backupRef string) error // bước 5–7 spec, idempotent
  ```
  `ResolveCode`: `Diff(CodeTree, SnapshotTree)`, lọc bỏ `aisdlc/features/`, `.aisdlc/`, `cfg.Unmanaged`; `OwnChanges` = path nằm trong `Changes` của các bước bị bỏ hoặc diff của bước IN_PROGRESS; còn lại `ForeignChanges`.
  `Apply` thứ tự: `RestorePaths(CodeTree, Own+Foreign)` → xoá `DeletePaths` (bỏ qua file không còn) → `ClearGate` từng cổng → `Steps.Truncate` → `Extras.RemoveSteps` → `AppendFeedback` (không ghi lại nếu dòng cuối đã là REWIND cùng mốc và cùng ref).

- [ ] **Step 1: Test thất bại** (repo tạm, tính năng luồng đầy đủ dựng bằng fixture, S1–S3 ACCEPTED):
  - `TestApply_RewindToS2`: code về `StartTree` S2; `steps.json` chỉ còn S1; REVIEW cleared; `eval/` mất; feedback có dòng REWIND.
  - `TestApply_RewindToBRD`: chỉ còn `feature.yaml`, `feedback.md` trong thư mục tính năng; code về đầu S1.
  - `TestApply_OtherFeatureUntouched`: `aisdlc/features/ROOM-2/steps.json` sửa sau mốc vẫn giữ nguyên.
  - `TestApply_DirtyTreeBackedUp` (Review Focus 1): file chưa commit `x.go` sửa sau mốc → sau `Backup`+`Apply`, `git show <backupRef>:x.go` ra nội dung đã sửa.
  - `TestApply_Idempotent` (Review Focus 3): gọi `Apply` hai lần → kết quả như một lần, feedback một dòng REWIND.
  - `TestResolveCode_ForeignSplit`: file do S2 sửa → Own; file sửa tay ngoài bước → Foreign.
  - `TestPlan_MissingTree` (Review Focus 2): xoá checkpoint ref + `gc --prune=now` → `ResolveCode` trả lỗi nhắc mốc còn snapshot; không file nào đổi.
- [ ] **Step 2:** `go test ./internal/rewind` → FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(rewind): backup, khôi phục code và xoá trạng thái`.

### Task 5: CLI `aisdlc rewind` (local) + hook chỉ-dev

**Files:**
- Create: `internal/cli/cmd_rewind.go`
- Modify: nơi đăng ký lệnh con (cùng chỗ `newReopenCmd`), `internal/policy/devcommand.go` (`devOnly`, `verbRe`), `internal/cli/hook_ask.go` (`indirectDevMsg` thêm `rewind`), `internal/i18n/en_cli.go`
- Test: `internal/cli/rewind_cmd_test.go`, `internal/policy/devcommand_test.go`, `internal/cli/hook_test.go`

**Interfaces:**
- Consumes: Task 3, 4; `FeatureOption`, `ctx.ConfirmDev`, `ctx.Approver`, `ctx.AskDev`.
- Produces: `func newRewindCmd(ctx *Context) *cobra.Command` (`Use: "rewind [mốc]"`, `Args: cobra.MaximumNArgs(1)`).

Luồng: dựng `Input` → không có tham số thì in danh sách đánh số kèm tóm tắt xoá (như ví dụ spec) và `AskDev("Chọn mốc (số): ")` → `Build` → `ResolveCode` → in xem trước (các dòng theo ví dụ spec; nhóm `ForeignChanges` in tiêu đề cảnh báo riêng `Cảnh báo: N file đổi ngoài các bước của <KEY> (merge/pull, sửa tay, tính năng khác) cũng sẽ về như mốc:` kèm danh sách; thêm dòng nếu `HEAD` khác commit lúc checkpoint) → `ConfirmDev` → `Backup` → (web: Task 9) → `Apply` → câu kết (Global Constraints) → `ctx.AfterCommand(f, "rewind "+id)`.

- [ ] **Step 1: Test thất bại** (theo khuôn `flow_local_test.go`, console giả):
  - `TestRewindCmd_LocalToS2`: gõ `rewind S2`, nhập key → exit 0, stdout chứa `Đã quay`, trạng thái như Task 4.
  - `TestRewindCmd_PickerNumber`: không tham số, nhập `5` rồi key → quay về mốc thứ 5.
  - `TestRewindCmd_WrongKeyAborts`: key sai → exit 3, không file nào đổi, không có ref backup.
  - `TestRewindCmd_NoConsole`: không console → exit 3 với `noConsoleMsg`.
  - `TestParseDevCommand_Rewind` (policy): `aisdlc rewind S2`, `aisdlc rewind -f K brd` là lệnh chỉ-dev.
  - `TestHook_RewindIsDevOnly` (Review Focus 4): PreToolUse Bash `aisdlc rewind S2` không sessionApprove → deny; `bash -c "aisdlc rewind S2"` → deny với `indirectDevMsg`.
- [ ] **Step 2:** `go test ./internal/cli ./internal/policy -run 'Rewind'` → FAIL.
- [ ] **Step 3:** Cài đặt; thêm bản dịch en cho mọi chuỗi mới.
- [ ] **Step 4:** `go test ./...` → PASS (gồm `english_test.go`).
- [ ] **Step 5:** Commit `feat(cli): aisdlc rewind cho chế độ local`.

### Task 6: Server — migration và trạng thái theo lần làm

**Files:**
- Create: `internal/web/store/migrations/00003_feature_rewind.sql`
- Modify: `internal/web/gates/repo.go` (`latestSubmission`, `submissionCount`, `lastSubmissions`, `feedbackBetween`, `state`), `internal/snapshot/gatestate.go`
- Test: `internal/web/gates/rewind_it_test.go` (phần trạng thái)

**Interfaces:**
- Produces:
  - SQL: `ALTER TABLE features ADD COLUMN attempt int NOT NULL DEFAULT 1`; `submissions`, `feedback` thêm `rewind_id bigint NULL REFERENCES feature_rewinds(id)`; `pending_actions` CHECK status thêm `'CANCELLED'`; bảng
    `feature_rewinds(id bigserial PK, repo_id text, feature_key text, from_attempt int, to_attempt int, checkpoint text, by_login text, at timestamptz DEFAULT now(), snapshot jsonb, request_id text NOT NULL, UNIQUE(repo_id, feature_key, request_id))`.
  - Mọi truy vấn trạng thái hiện tại của submissions/feedback thêm `AND rewind_id IS NULL`.
  - `snapshot.FeatureGateState` thêm `Attempt int \`json:"attempt"\``; `state()` đọc `features.attempt` (không có hàng → 1).
- [ ] **Step 1: Test thất bại** `TestState_IgnoresClosedRows`: chèn submission có `rewind_id` → `state()` không thấy nó; `Attempt` = giá trị trong `features`.
- [ ] **Step 2:** chạy IT như các `*_it_test.go` hiện có → FAIL.
- [ ] **Step 3:** Viết migration (goose Up/Down), sửa truy vấn.
- [ ] **Step 4:** `go test ./internal/web/...` → PASS.
- [ ] **Step 5:** Commit `feat(web): lưu lần làm và đánh dấu bản ghi đã khép`.

### Task 7: Server — endpoint `POST /api/gates/rewind`

**Files:**
- Create: `internal/web/gates/rewind.go`
- Modify: `internal/web/gates/handlers.go` (`gateRoutes`), `internal/web/gates/actions.go` nếu cần chung `authorize`
- Test: `internal/web/gates/rewind_it_test.go`

**Interfaces:**
- Consumes: Task 6; `authorize`, `revoke`, `addInstruction`, audit hiện có.
- Produces: `func (s *Service) Rewind(ctx context.Context, p httpx.Principal, ip, channel, repo, feature, checkpoint, requestID string, gates []gate.Gate) (snapshot.FeatureGateState, error)`; body JSON `{repo, feature, checkpoint, gates[], requestId, channel}`; trả phong bì trạng thái có chữ ký như `/api/gates/reopen`. Quyền: như reopen trên cổng đầu tiên trong `gates`.

Trong một giao dịch (spec mục Web, có điều chỉnh đã nêu trong spec "Để builder quyết"): đã có `feature_rewinds` cùng `request_id` → trả trạng thái hiện tại, không làm gì; ngược lại chép `features.snapshot` → `feature_rewinds`; `revoke` các cổng trong `gates` (by = login); `UPDATE submissions/feedback SET rewind_id` cho các cổng trong `gates`, `rewind_id IS NULL`; tăng `features.attempt`; `pending_actions` PENDING → `CANCELLED`; feedback kind `REWOUND`; chỉ dẫn agent `Tính năng <KEY> đã được quay về đầu <mốc> bởi <login>. Mọi thứ sau mốc đã bị xoá; bắt đầu lại bằng <lệnh>.`; audit `feature.rewind`.

- [ ] **Step 1: Test thất bại:**
  - `TestRewind_ToDesign`: BRD, DESIGN, CASES đã duyệt → rewind `design` với gates `[DESIGN,CASES,REVIEW]` → BRD vẫn APPROVED cùng hash, DESIGN/CASES không còn submission hiện tại, `Attempt`=2, có hàng `feature_rewinds` với `snapshot`.
  - `TestRewind_SameRequestIDNoDouble` (Review Focus 3): gọi hai lần cùng `requestId` → `attempt`=2, một hàng `feature_rewinds`.
  - `TestRewind_CancelsPendingActions`, `TestRewind_ForbiddenForNonOwner` (như quyền reopen).
- [ ] **Step 2:** FAIL. **Step 3:** cài đặt. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(web): endpoint quay về mốc`.

### Task 8: Ingest bỏ qua snapshot lần làm cũ

**Files:**
- Modify: `internal/webclient/snapshot.go` (`FeatureSnapshot` thêm `Attempt int \`json:"attempt,omitempty"\``; điền từ cache cổng có chữ ký), `internal/web/ingest/snapshot.go`, `internal/web/ingest/validate.go`
- Test: `internal/web/ingest/ingest_test.go` hoặc IT tương ứng

**Interfaces:**
- Produces: ingest trả `accepted=false` và mã lỗi mới `STALE_ATTEMPT` (thêm vào `internal/snapshot/errcode.go`) khi `attempt < features.attempt`, hoặc khi thiếu `attempt` và `features.attempt > 1`. CLI nhận mã này thì `f.Store.Refresh()` và in `Tính năng <KEY> đã được quay về trên web; đã làm mới trạng thái.`
- [ ] **Step 1: Test thất bại** `TestIngest_OldAttemptIgnored` (Review Focus 5): `features.attempt`=2, đẩy snapshot `attempt`=1 → không ghi đè; thiếu `attempt` → không ghi đè; `attempt`=2 → ghi.
- [ ] **Step 2:** FAIL. **Step 3:** cài đặt. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(web): ingest bỏ qua snapshot của lần làm cũ`.

### Task 9: CLI rewind ở chế độ web

**Files:**
- Modify: `internal/cli/webgates.go` (thêm `Rewind`), `internal/cli/cmd_rewind.go`
- Test: `internal/cli/flow_web_test.go` (thêm ca)

**Interfaces:**
- Consumes: Task 5, Task 7.
- Produces: `func (w *WebCache) Rewind(f *Feature, cp rewind.Checkpoint, gates []gate.Gate, requestID, channel string) error` — POST `/api/gates/rewind` bằng token approve, xử lý 409/lỗi như `Act`, rồi `accept` phong bì + `f.Store.Refresh()`.
  `requestID` = tên ref dự phòng (đã duy nhất theo thời điểm), để chạy lại sau khi đứt dùng cùng giá trị: `cmd_rewind` tìm ref dự phòng mới nhất của KEY có dòng feedback chưa ghi cho mốc này thì dùng lại.
  Ở chế độ web, `Deps.ClearGate` là no-op (trạng thái lấy từ cache đã refresh).
- [ ] **Step 1: Test thất bại:**
  - `TestRewindWeb_ServerFirst`: server giả trả lỗi 500 → exit 2, working tree và `steps.json` không đổi, ref dự phòng có (in ra).
  - `TestRewindWeb_Success`: server thật (webtest) → cache cổng có `attempt`=2, trạng thái local như Task 4.
- [ ] **Step 2:** FAIL. **Step 3:** cài đặt. **Step 4:** `go test ./...` PASS.
- [ ] **Step 5:** Commit `feat(cli): rewind ở chế độ web`.

### Task 10: API và UI archive

**Files:**
- Modify: `internal/web/board/handlers.go` (route `GET /api/features/{repo}/{key}/attempts`), `internal/web/board/reader.go`
- Create: `web/ui/src/components/AttemptArchive.tsx`, `web/ui/src/components/AttemptArchive.test.tsx`
- Modify: `web/ui/src/pages/Review.tsx`, `web/ui/src/components/Card.tsx`, `web/ui/src/api.ts`, `web/ui/src/types.ts`, `web/ui/src/i18n/vi.ts`, `web/ui/src/i18n/en.ts`, `web/ui/src/review.css`
- Test: `internal/web/board/board_it_test.go`

**Interfaces:**
- Produces: JSON `[{attempt, rewind:{checkpoint, by, at}, gates:[{gate, submissions:[{hash, at, by, artifacts:[{path, content}]}], feedback:[{kind, by, at, comment}], approvals:[{approver, at, revokedAt}]}], steps:[{id, status, files, lines, casesPassed, casesTotal}], reviewCard?}]`, mới nhất trước, chỉ các lần làm đã khép. `steps`/`reviewCard` lấy từ `feature_rewinds.snapshot`.
  UI: trong trang review, dưới danh sách cổng, mục `rvw.attemptsTitle` "Các lần làm trước" (chỉ khi mảng không rỗng), mỗi lần làm là `<details>` có tiêu đề "Lần N · quay về đầu <mốc> bởi <by> · <thời điểm>", nội dung chỉ đọc theo spec. Board card và header review hiện tag `card.attempt` "Lần làm N" khi N > 1. Không có nút hành động. Theo `DESIGN.md` (panel, tag, mono cho hash/path; không viền trái màu).
- [ ] **Step 1: Test thất bại:**
  - Go IT `TestAttempts_ListsClosedAttempt`: sau Task 7 → endpoint trả một phần tử, có nội dung artifact BRD cũ.
  - vitest `AttemptArchive.test.tsx`: render với fixture một lần làm → thấy tiêu đề "Lần 1", tên mốc, nội dung artifact; mảng rỗng → không render gì.
- [ ] **Step 2:** `go test ./internal/web/board/...` và `cd web/ui && npx vitest run` → FAIL.
- [ ] **Step 3:** Cài đặt; key i18n có ở cả vi/en.
- [ ] **Step 4:** PASS; `cd web/ui && npx tsc --noEmit && npm run build` sạch.
- [ ] **Step 5:** Commit `feat(web): archive các lần làm trước`.

### Task 11: Docs và E2E

**Files:**
- Modify: `docs/huong-dan-su-dung.md`, `docs/user-guide.md` (mục mới "Làm lại tính năng" + bảng lệnh + bảng xử lý sự cố), `e2e/` (kịch bản mới theo khuôn kịch bản hiện có)
- Test: `docs/docs-check.sh`, e2e

- [ ] **Step 1:** Thêm kịch bản e2e: tính năng tới S2 ACCEPTED → `aisdlc rewind S2` (nhập key) → `aisdlc status` thấy S2 chưa bắt đầu → web hiện "Lần làm 2" và archive.
- [ ] **Step 2:** Chạy e2e → FAIL trước khi có docs/khớp chuỗi (nếu e2e ghim chuỗi), chỉnh tới PASS.
- [ ] **Step 3:** Viết docs hai ngôn ngữ: mốc, xem trước, cảnh báo file ngoài bước, ref dự phòng và cách cứu (`git restore --source <ref> -- <path>`, xoá ref `git update-ref -d`), archive trên web, `reopen` vẫn dùng khi chỉ sửa một artifact.
- [ ] **Step 4:** `bash docs/docs-check.sh` và `go test ./...` PASS.
- [ ] **Step 5:** Commit `docs: hướng dẫn aisdlc rewind`.
