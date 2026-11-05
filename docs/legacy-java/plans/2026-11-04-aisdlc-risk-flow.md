# Lint sau mỗi bước, trần số vòng sửa, quy trình theo mức rủi ro — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Thêm ba thứ:
- lint tất định trong `step finish`;
- bộ đếm vòng sửa, vượt trần thì tạm dừng agent;
- `flow` theo mức rủi ro: cổng hoặc bước `auto` tự duyệt khi sạch; Review luôn duyệt tay; nâng mức thì bản duyệt auto bị thay thế.

**Architecture:**
- Quyết định thuần (`FlowDecision`, `RoundCounter`, `LintRunner`) nằm trong `aisdlc-core`.
- CLI gọi các quyết định này trong `step finish`, `submit`, `eval` và các lệnh đổi mức.
- Chế độ local: CLI ghi bản duyệt auto.
- Chế độ web: server tự duyệt khi nhận submission, và đánh dấu bản auto bị thay thế khi nhận sự kiện rủi ro.
- CI chỉ chấp nhận bản duyệt auto khi `risk.yaml` đã khóa xác nhận flow.

**Tech Stack:** Java 17, Maven, picocli, Jackson, JUnit 5 + AssertJ, Spring Boot 3.3.5 + JdbcTemplate + Flyway + Postgres 16, React 18 + TS + Vitest, Playwright 1.49.1.

**Spec:** `docs/superpowers/specs/2026-11-04-aisdlc-risk-flow-design.md`. Spec nền: `docs/superpowers/specs/2026-11-04-aisdlc-risk-policy-design.md`.

**Nhánh:** `risk-flow`, tạo từ `risk-policy`, hoặc từ `main` nếu `risk-policy` đã được merge.

## Global Constraints

**Bất biến về quyền**
- Cổng **Review** luôn duyệt tay. Không cấu hình nào bỏ được việc này. Chính sách có key `flow.<level>.review` thì bị báo lỗi validate.
- Agent không tự duyệt được, không tự gỡ tạm dừng được, và không sửa được `rounds.json`, file tạm dừng, chính sách hay `risk.yaml` (hook chặn).
- Token CLI không duyệt được gì trên web. Ở chế độ web, chỉ server ghi bản duyệt auto.

**Ràng buộc kỹ thuật**
- Hook không gọi mạng.
- Lõi không gắn cứng công cụ lint nào: lệnh lint lấy từ `aisdlc/config.yaml`.

**Tương thích ngược**
- Không có khối `lint` thì không lint.
- Không có `flow` thì mọi cổng là `manual`.
- Không có `maxRounds` thì trần là 3.
- Tính năng không có `risk.yaml` thì mọi cổng `manual` và trần là 3.

**Tên cố định**
- Cấu hình: `lint.check`, `lint.fix`, `lint.timeoutSeconds`.
- Chính sách: `flow.<level>.{brd,design,cases,step}` nhận `auto|manual`; `maxRounds.<level>`.
- File:
  - `aisdlc/features/<KEY>/rounds.json`;
  - `.aisdlc/pause/<KEY>.json` với nội dung `{reason:"ROUNDS", unit, count, at}`.
- Sự kiện: `ROUNDS_EXCEEDED`, `ROUNDS_RESUMED`.
- Hard check: `LINT`.
- Lệnh: `aisdlc resume`.
- Approver và kênh: approver `auto`; kênh web `auto`.
- Trạng thái bản duyệt bị thay thế: `superseded`.

**Thông điệp**
- Tạm dừng do vượt trần: `"Đã thử <N> lần chưa qua ở <unit>, cần dev xem. Dev chạy: aisdlc resume"`.
- Nâng mức làm bản auto bị thay thế: `"Mức tăng <from> → <to>: cổng <list> cần dev duyệt lại."`.
- Hiển thị bản duyệt auto: `"tự duyệt (mức <level>)"`.

**Quy tắc chung**
- `aisdlc resume` cần console thật và gõ lại feature key.
- Commit kết thúc bằng `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Test không ghi vào `~/.config/aisdlc` thật.
- Chuẩn bị môi trường: `export JAVA_HOME=~/.local/opt/jdk17 PATH=~/.local/opt/jdk17/bin:~/.local/opt/maven/bin:$PATH`.

## Review Focus

1. **Lệnh lint treo hoặc in ra rất nhiều dòng.** `step finish` phải dừng đúng `timeoutSeconds`, giết cả cây tiến trình, và chỉ giữ 40 dòng cuối, không giữ cả output trong bộ nhớ. Test ở Task 2 (`lint_timeout_killsProcessTree`, `lint_hugeOutput_keepsTail`).
2. **`lint.fix` tạo file mới ngoài phạm vi, hoặc sửa file đã bẩn từ trước.** CLI chỉ hoàn tác những thay đổi do `fix` gây ra; thay đổi có sẵn của dev phải giữ nguyên. Test ở Task 2 (`fixOutsideScope_revertsOnlyFixChanges`).
3. **Mức tăng sau khi cổng Case đã tự duyệt.** Ví dụ `risk set high` khi Case đã duyệt auto: bị chặn như hiện tại, vì `risk set` không cho chạy sau khi cổng Case đã duyệt. `reclassify` mở lại Case. Mọi bản auto mà nay thành `manual` phải bị thay thế, kể cả BRD và Design. Test ở Task 4 (`reclassifyRaise_supersedesAllAutoGates`).
4. **Hai lệnh bị chặn chạy gần như cùng lúc** (hook gọi CLI song song). `rounds.json` phải ghi dưới khóa file, không mất lượt đếm. Test ở Task 3 (`concurrentIncrements_noLostUpdate`).
5. **Server nhận submission có `risk.level` không có trong thang của chính sách đang hiệu lực** (chính sách đổi giữa chừng). Server không tự duyệt và ghi lý do. Test ở Task 5 (`autoApprove_unknownLevel_staysPending`).

---

### Task 1: Core — `flow` và `maxRounds` trong chính sách; `FlowDecision`

**Files:**
- Modify in `aisdlc-core/src/main/java/com/aisdlc/core/risk/`: `RiskPolicy.java` (schema và validate), `RiskFile.java` (chụp lại `flow` và `maxRounds` của mức), `PolicyDiff.java` (nới lỏng: `manual→auto`, tăng `maxRounds`)
- Modify resources: `aisdlc-core/src/main/resources/risk-presets/generic/2.yaml`, `banking/2.yaml` (thêm phiên bản mẫu mới có `flow` và `maxRounds` như spec §5 và §4; **không sửa file `1.yaml`**)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/risk/FlowDecision.java`
- Test: `RiskPolicyTest`, `PolicyDiffTest`, `PresetsTest`, `FlowDecisionTest`, `RiskFileTest`

**Interfaces — Produces:**
- `RiskPolicy`:
  - thêm `Map<String, Map<String,String>> flow` và `Map<String,Integer> maxRounds`;
  - `String flowFor(String level, String unit)`: `unit` ∈ `brd|design|cases|step`; mặc định `"manual"`;
  - `int maxRoundsFor(String level)`: mặc định 3.
- `RiskFile`: thêm `Map<String,String> flow` (của mức đã chọn) và `int maxRounds`. Đọc file cũ thiếu hai trường thì dùng `{}` và 3.
- `FlowDecision.autoGate(RiskPolicy p, String level, Gate gate, boolean hasHighFlag) -> Optional<String>`:
  - trả `Optional.empty()` khi được tự duyệt, ngược lại trả lý do;
  - `Gate.REVIEW` luôn trả lý do `"Review luôn duyệt tay"`.
- `FlowDecision.brdLevel(RiskPolicy p, String previewLevel) -> String` = max(preview, default).
- `FlowDecision.autoFromRiskFile(RiskFile r, Gate gate) -> boolean`: CI dùng hàm này.

- [ ] **Step 1: Viết test**
  - `parse_flowAndMaxRounds`.
  - `parse_flowReviewKey_error`: lỗi chứa `"review"`.
  - `parse_flowBadValue_error`: `auto2`.
  - `parse_maxRoundsNonPositive_error`.
  - `flowDefaultsManual`.
  - `autoGate_reviewAlwaysManual`.
  - `autoGate_highFlagBlocks`.
  - `brdLevel_raisedToDefault`.
  - `diff_manualToAuto_isLoosening`.
  - `diff_maxRoundsIncrease_isLoosening`.
  - `riskFile_oldWithoutFlow_readsDefaults`.
  - `presets_v2_haveFlow_v1_unchanged`.
- [ ] **Step 2: RED.** `mvn -q -pl aisdlc-core test -Dtest='RiskPolicyTest,PolicyDiffTest,PresetsTest,FlowDecisionTest,RiskFileTest'`
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: GREEN.** Chạy `mvn -q -pl aisdlc-core test`.
- [ ] **Step 5: Commit.** `feat(core): flow và maxRounds theo mức rủi ro, FlowDecision`

### Task 2: Lint trong `step finish` và eval

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/step/LintRunner.java`, `.../step/LintResult.java`
- Modify:
  - `aisdlc-core/src/main/java/com/aisdlc/core/model/RepoConfig.java`: thêm `LintConfig lint`, nullable;
  - `.../step/StepRecord.java`: thêm `LintResult lint`, nullable;
  - `.../step/StepService.java`: thêm `finish` overload có lint;
  - `.../eval/EvalService.java`: hard check `LINT`;
  - `aisdlc-cli/.../commands/StepCommand.java`: `finish` chạy lint trước case, `accept` từ chối khi lint chưa qua;
  - template `aisdlc-cli/src/main/resources/templates/config.yaml`: thêm khối `lint` dạng comment.
- Test: `LintRunnerTest` (core, dùng repo git tạm và script shell giả lập lint), `StepCommandLintTest` (cli), `EvalServiceTest`

**Interfaces — Produces:**
- `record LintConfig(String check, String fix, int timeoutSeconds)`: mặc định `timeoutSeconds` là 60.
- `record LintResult(boolean passed, Instant at, String tail, List<String> reverted)`: `tail` tối đa 40 dòng cuối.
- `LintRunner.run(Path repoRoot, LintConfig cfg, Predicate<String> inScope, GitRepo git) -> LintResult`, chạy theo thứ tự:
  1. chụp `git status --porcelain=v1 -z` cùng sha256 của các file đang thay đổi hoặc chưa track;
  2. chạy `fix` nếu có;
  3. so sánh lại, lấy các path mới đổi hoặc mới xuất hiện so với ảnh chụp mà `!inScope`: file đã track thì `git checkout -- <p>`, file chưa track thì xóa, rồi đưa vào `reverted`;
  4. chạy `check`;
  5. `passed` khi exit 0 và không quá giờ.
- **Chạy tiến trình:**
  - qua `ProcessBuilder("bash","-lc", cmd)`, gộp stderr vào stdout;
  - output đọc bằng ring buffer 40 dòng;
  - hết giờ thì `destroyForcibly` cả `ProcessHandle.descendants()`.
- **`step finish` có lint:**
  - lint đỏ thì bước giữ trạng thái `IN_PROGRESS` và exit ≠ 0;
  - CLI in `tail` cùng danh sách `reverted`;
  - case không chạy khi lint đỏ.
- **`step accept`:** khi `lint != null && !lint.passed()` thì exit 2 với `"Lint của lần finish gần nhất chưa qua."`.
- **Eval:** có `lint` thì chạy `check` một lần trên cây hiện tại → `HardCheck("LINT", passed, tail)`.

- [ ] **Step 1: Viết test**
  - `lint_pass`.
  - `lint_fail_tailOnly40Lines`.
  - `lint_hugeOutput_keepsTail`: 100k dòng.
  - `lint_timeout_killsProcessTree`: script sinh tiến trình con `sleep 30`, timeout 1s; sau đó không còn tiến trình con.
  - `fixOutsideScope_revertsOnlyFixChanges`:
    - trước khi chạy, có sẵn một file ngoài phạm vi đã bẩn;
    - `fix` sửa file đó và tạo một file mới ngoài phạm vi;
    - kỳ vọng: file mới bị xóa; file bẩn sẵn chỉ được trả về đúng nội dung trước `fix` (dùng hash đã chụp, ghi lại nội dung cũ); file trong phạm vi giữ thay đổi của `fix`.
  - `noLintConfig_finishUnchanged`.
  - `accept_refusedWhenLintFailed`.
  - `eval_lintHardCheck`.
- [ ] **Step 2: RED. Step 3: Cài đặt.** Khi hoàn tác một file đã bẩn sẵn, không dùng `git checkout`, vì nó làm mất thay đổi của dev. Thay vào đó lưu nội dung file vào bộ nhớ trước khi chạy `fix` và ghi lại đúng nội dung đó.
- [ ] **Step 4: GREEN.** `mvn -q -pl aisdlc-cli -am verify -Dskip.ui`
- [ ] **Step 5: Commit.** `feat: lint tất định trong step finish và eval`

### Task 3: Trần số vòng sửa, tạm dừng local, `aisdlc resume`

**Files:**
- Create in `aisdlc-core/src/main/java/com/aisdlc/core/`: `step/RoundCounter.java`, `web/LocalPause.java`
- Modify:
  - `aisdlc-core/.../policy/HookPolicy.java`: deny `PAUSED` khi có `LocalPause`; thêm `rounds\.json` vào `FEATURE_STATE`; `.aisdlc/pause/**` đã nằm dưới `.aisdlc/` nên đã bị chặn;
  - `.../policy/PolicyState.java`: nạp `LocalPause`;
  - CLI: `StepCommand`, `SubmitCommand`, `EvalCommand` (tăng hoặc reset bộ đếm), lớp lệnh mới `ResumeCommand`, `web/Event.java` (javadoc);
  - plugin: `plugin/bin/aisdlc` (fast path của hook) nếu fast path cần biết `LocalPause`.
- Test: `RoundCounterTest`, `LocalPauseTest`, `HookPolicyTest`, `RoundsCliTest`, `plugin/test/plugin-files-test.sh`

**Interfaces — Produces:**
- `RoundCounter(Path roundsJson)`:
  - `int fail(String unit)`: tăng rồi trả số mới;
  - `void reset(String unit)`;
  - `int count(String unit)`.
  - `unit` ∈ `S1..Sn | BRD | DESIGN | CASES | REVIEW`.
  - Ghi dưới khóa file (`FileChannel.lock` trên file `.lock` cạnh `rounds.json`), ghi atomic qua file tạm rồi move.
- `LocalPause`:
  - `static Path file(Path root, FeatureKey k)`;
  - `static Optional<LocalPause> read(...)`;
  - `static void write(..., String unit, int count, Instant at)`;
  - `static void clear(...)`;
  - `record LocalPause(String reason, String unit, int count, Instant at)`.
- **Trần:** `RiskFile.maxRounds()` nếu có `risk.yaml`, ngược lại 3. Khi `fail(...) >= trần` thì ghi `LocalPause` và gửi `Event("ROUNDS_EXCEEDED", detail={unit,count})` nếu có web.
- **Điểm đếm** (spec §4, §5.4):
  - `step finish` thất bại → `fail("Sx")`; thành công → `reset("Sx")`;
  - `submit <gate>` bị chặn → `fail(gate)`; thành công → `reset(gate)`;
  - `eval` FAIL → `fail("REVIEW")`; ĐẠT → `reset("REVIEW")`.
- **`aisdlc resume`:**
  - cần console thật và gõ lại key;
  - xóa `LocalPause`, `reset(unit)`, gửi `ROUNDS_RESUMED`;
  - không có tạm dừng local thì exit 0 với `"Không có tạm dừng local."`;
  - không gỡ tạm dừng do web.
- **Hook:** khi có `LocalPause`, mọi tool ghi bị deny với mã `PAUSED` và thông điệp trong Global Constraints. Tạm dừng do web vẫn dùng thông điệp cũ. Có cả hai thì nêu cả hai.

- [ ] **Step 1: Viết test**
  - `concurrentIncrements_noLostUpdate`: 8 luồng × 25 lần thì `count == 200`.
  - `exceed_writesPause_hookDeniesWrite`.
  - `success_resetsCounter`.
  - `resume_needsConsole`.
  - `resume_clearsPauseAndCounter`.
  - `hookDeniesAgentWriteRoundsJson`.
  - `maxRoundsFromRiskFile`.
  - `noRiskFile_defaultThree`.
  - `evalFail_countsReview`.
  - Chạy `plugin-files-test.sh` sau khi build lại jar.
- [ ] **Step 2: RED. Step 3: Cài đặt. Step 4: GREEN.** `mvn -q -pl aisdlc-cli -am verify -Dskip.ui` và `bash plugin/test/plugin-files-test.sh`
- [ ] **Step 5: Commit.** `feat: trần số vòng sửa, tạm dừng local và aisdlc resume`

### Task 4: Tự duyệt ở chế độ local, tự accept bước, thay thế khi nâng mức, CI

**Files:**
- Modify:
  - `aisdlc-core/.../gate/Approval.java`: thêm `String rule` (nullable, ví dụ `"flow:low"`) và `boolean superseded`, giữ constructor cũ;
  - `.../gate/GateService.java`: thêm `autoApprove(Gate, String rule)` và `supersedeAuto(Set<Gate>)`; một bản duyệt có `superseded` thì không được tính là đã duyệt;
  - `.../eval/EvalService.java` hoặc `CiApprovals`: kiểm tra bản duyệt `auto` bằng `FlowDecision.autoFromRiskFile`;
  - CLI: `SubmitCommand` (sau khi submit thành công, ở chế độ local và cổng được tự duyệt theo `FlowDecision`, gọi `autoApprove`), `StepCommand` (`finish` thành công, `flow.step == auto`, bước không có `ScopeExtras` thì tự accept với approver `auto`), `RiskCommand` và nhánh tính lại mức trong `SubmitCommand` (gọi `supersedeAuto` cho các cổng đã duyệt auto mà nay là `manual`, rồi in thông điệp), `StatusCommand` và `ReviewCardText` (hiện `"tự duyệt (mức <level>)"`).
- Test: `GateServiceTest`, `FlowLocalTest` (cli), `CiAutoApprovalTest` (core)

**Quy tắc:**
- **BRD** dùng `FlowDecision.brdLevel(policy, riskPreview.level)`. Các cổng khác dùng `risk.yaml`; không có `risk.yaml` thì `manual`.
- **Cờ HIGH:** lấy từ review card của cổng đó. Có cờ HIGH thì không tự duyệt và in lý do.
- **Chế độ web:** CLI không gọi `autoApprove`; server làm việc này ở Task 5.
- **Thay thế khi nâng mức:**
  - chạy ở cả hai chế độ: local ghi trực tiếp; ở web, CLI chỉ in thông báo vì server tự làm;
  - các cổng bị thay thế quay về trạng thái `SUBMITTED`, chờ dev duyệt tay, không cần nộp lại;
  - `codeReady()` và hook coi các cổng này là chưa duyệt.
- **CI** (chế độ local, đọc `gates.json` trong repo):
  - bản duyệt `approver == "auto"` mà không có `risk.yaml`, hoặc `autoFromRiskFile` trả false, thì FAIL `"Bản tự duyệt <gate> không khớp flow trong risk.yaml"`;
  - với BRD, kiểm theo mức cuối trong `risk.yaml` (spec §5.4).

- [ ] **Step 1: Viết test**
  - `lowFeature_brdDesignCasesAutoApproved_reviewManual`: đi hết luồng FULL trên repo tạm với mẫu có `flow.low`, chỉ cổng Review chờ duyệt.
  - `highFlag_noAuto`.
  - `stepAutoAccept_whenClean`.
  - `stepWithAllowScope_needsDev`.
  - `reclassifyRaise_supersedesAllAutoGates`.
  - `submitDesignRaise_supersedesBrd`.
  - `lowerNeverTurnsManualIntoAuto`.
  - `ci_autoApprovalMatchingFlow_pass`.
  - `ci_autoApprovalNotMatching_fail`.
  - `webMode_cliDoesNotAutoApprove`.
- [ ] **Step 2: RED. Step 3: Cài đặt. Step 4: GREEN.** `mvn -q -pl aisdlc-cli -am verify -Dskip.ui`
- [ ] **Step 5: Commit.** `feat: tự duyệt theo flow ở chế độ local, tự accept bước, thay thế khi nâng mức`

### Task 5: Web — server tự duyệt, thay thế bản auto, tạm dừng do vượt trần

**Files:**
- Create: `aisdlc-web/src/main/resources/db/migration/V5__risk_flow.sql`
- Modify:
  - `aisdlc-web/.../gates/ApprovalService.java` và `CliGateController.java` (`POST /api/cli/submissions` nhận thêm trường `risk: {level}`);
  - `GateRepository.java`;
  - `ingest/EventStore.java`: sự kiện rủi ro thì gọi thay thế; `ROUNDS_EXCEEDED` và `ROUNDS_RESUMED` thì cập nhật trạng thái dừng;
  - `control/ControlService.java` và `aisdlc-core/.../web/FeatureControl.java`: thêm `roundsResumeAt`, nullable;
  - CLI `web/WebControl.java`: khi thấy `roundsResumeAt` sau `LocalPause.at`, xóa `LocalPause` và reset bộ đếm;
  - CLI `SubmitCommand`: gửi `risk.level` cùng submission.
- Test: `AutoApproveIT`, `SupersedeIT`, `RoundsPauseIT`, `WebControlTest` (cli)

**Schema:**
```sql
ALTER TABLE approvals DROP CONSTRAINT approvals_channel_check;
ALTER TABLE approvals ADD CONSTRAINT approvals_channel_check CHECK (channel IN ('web','terminal','import','auto'));
ALTER TABLE approvals ADD COLUMN rule text;
ALTER TABLE features ADD COLUMN rounds_pause jsonb;
```
Tên constraint thật lấy từ V2: kiểm bằng `\d approvals` hoặc đặt tên rõ trong migration.

**Quy tắc:**
- **Tự duyệt khi lưu submission.**
  1. Lấy chính sách đang hiệu lực của repo. Không có thì không tự duyệt.
  2. Xác định mức áp cho cổng: với BRD là `brdLevel(policy, card.riskPreview.level)`; với cổng khác là `risk.level` trong request.
  3. Mức không có trong `policy.levels` thì không tự duyệt, ghi audit `"auto: mức <x> không có trong chính sách v<N>"`.
  4. Gọi `FlowDecision.autoGate(policy, level, gate, hasHighFlag(card))`. Được thì ghi approval với `channel='auto'`, `approver='auto'`, `rule='flow:<level>'`, cùng `hash` của submission.
  5. Cổng REVIEW không bao giờ tự duyệt.
- **Thay thế khi nhận sự kiện rủi ro:**
  - với mỗi cổng đang có approval `channel='auto'` và chưa thu hồi, mà `flowFor(level mới, unit) == manual`: đặt `revoked_at = now()` và `revoked_by = 'auto-supersede'`;
  - ghi audit `"Mức tăng <from> → <to>"`;
  - cổng quay về trạng thái chờ duyệt, nhờ cơ chế approval sống đã dựa vào `revoked_at IS NULL`.
- **Dừng do vượt trần:**
  - `ROUNDS_EXCEEDED` thì `features.rounds_pause = {unit,count,at}`; `ROUNDS_RESUMED` thì đặt lại NULL;
  - nút "Tiếp tục" trên web (session + CSRF) đặt `roundsResumeAt = now()` trong control đã ký và xóa `rounds_pause`.
- **Payload CI** `/api/ci/approvals` trả thêm `channel` và `rule`, để eval CI hiện "tự duyệt".

- [ ] **Step 1: Viết test**
  - `autoApprove_lowCleanSubmission`.
  - `autoApprove_highFlag_staysPending`.
  - `autoApprove_unknownLevel_staysPending`.
  - `review_neverAuto`.
  - `supersede_onRiskClassifiedRaise`.
  - `supersede_ignoresManualApprovals`.
  - `roundsExceeded_showsPause_resumeSignsControl`.
  - `cli_roundsResumeAtClearsLocalPause`.
- [ ] **Step 2: RED. Step 3: Cài đặt. Step 4: GREEN.** `mvn -q -pl aisdlc-web -am verify -Dskip.ui`
- [ ] **Step 5: Commit.** `feat(web): server tự duyệt theo flow, thay thế khi nâng mức, tạm dừng do vượt trần`

### Task 6: Web UI và tài liệu

**Files:**
- Modify in `aisdlc-web/ui/src/`:
  - `components/RiskRequirements.tsx`: thêm hàng `flow` (4 cổng × mức, chọn `auto`/`manual`) và hàng `maxRounds` (ô số);
  - `risk.ts`: các hàm sửa nháp, kèm test;
  - các component cổng và bước: hiện huy hiệu `"tự duyệt (mức <level>)"`, và `"đã thay thế"` cho bản duyệt bị thu hồi với `revoked_by = 'auto-supersede'`;
  - `ControlPanel.tsx`: banner dừng do vượt trần, với `unit`, `count` và nút "Tiếp tục".
- Modify docs: `docs/README.vi.md` thêm mục "Lint, trần vòng sửa, quy trình theo mức".

**Lưu ý UI:**
- Hộp xác nhận xuất bản đã liệt kê các thay đổi nới lỏng; `manual→auto` và tăng `maxRounds` nhờ Task 1 sẽ tự hiện ở đó.
- Không có ô `review` trong hàng `flow`. Thay vào đó hiện chữ cố định "Review: luôn duyệt tay".

- [ ] **Step 1: Viết test Vitest**
  - `setFlow_updatesDraft`.
  - `setMaxRounds_validatesPositive`.
  - `draftChanges_flowManualToAutoListed`.
- [ ] **Step 2: RED. Step 3: Cài đặt. Step 4: GREEN.** `npx vitest run && npx tsc --noEmit`, sau đó `mvn -q -pl aisdlc-web -am verify`
- [ ] **Step 5: Commit.** `feat(ui): flow và trần vòng sửa trên trang chính sách, huy hiệu tự duyệt, banner dừng`

### Task 7: E2E

**Files:** `e2e/run-e2e.sh` (bước mới, thay đổi khoanh vùng, không động vào hàm dọn dẹp), dữ liệu fixture, `e2e/web-*.mjs`.

**Kịch bản:**
1. **Mức thấp chạy một mạch.** Một tính năng mới dùng solution có sẵn (FIX-2, hoặc bản sao PAY-1 đổi BRD để không khớp rule nào), với chính sách fixture có `flow.low` toàn `auto`. Agent giả lập nộp BRD → Thiết kế → Case → S1..Sn → eval mà không có lệnh `approve` hay `step accept` nào. Cuối cùng chỉ cổng Review chờ duyệt. `WEB=0` thì CLI tự duyệt; `WEB=1` thì server tự duyệt, và web hiện "tự duyệt".
2. **Nâng mức.** PAY-1 với BRD được tự duyệt ở mức tạm thấp. Thiết kế chạm bảng của rule mức cao, nên `submit design` in thông báo cần duyệt lại và cổng BRD về chờ duyệt.
3. **Lint đỏ.** Config fixture có `lint.check` là một script kiểm tra đơn giản, ví dụ không cho phép tab trong file `.java`. Một bước có tab thì `step finish` thất bại. Sửa xong thì qua.
4. **Vượt trần.** Ba lần `step finish` đỏ liên tiếp, với `maxRounds.low = 2`, thì lần ghi tiếp theo bị hook chặn với `PAUSED`. Chạy `aisdlc resume` (console giả lập như các bước approve sẵn có của E2E) thì ghi được lại.

- [ ] **Step 1: Viết kịch bản. Step 2:** Chạy `WEB=0 e2e/run-e2e.sh`, `e2e/run-e2e.sh` và `SCREENSHOTS=1 e2e/run-e2e.sh` → PASS. Sau đó chạy `mvn -q verify` và `bash plugin/test/plugin-files-test.sh` → PASS.
- [ ] **Step 3: Commit.** `test(e2e): luồng mức thấp tự duyệt, nâng mức, lint đỏ, vượt trần`
