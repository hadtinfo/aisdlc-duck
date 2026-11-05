# Duyệt trong Claude Code + tính năng theo session — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mỗi session Claude Code gắn một tính năng riêng, và (khi repo bật `sessionApprove`) dev duyệt các lệnh chỉ dev
ngay trong Claude Code qua hộp xác nhận quyền.

**Architecture:** Core thêm ba đơn vị thuần: `SessionBindings` (session → key trong `.aisdlc/sessions/`),
`DevGrants` (phiếu cho phép một lần trong `.aisdlc/grants/`), `DevCommand` (nhận diện lệnh chỉ dev trong chuỗi
Bash). CLI phân giải tính năng qua một hàm chung cho lệnh và hook (`FeatureResolver`); `confirmDev` chấp nhận phiếu
thay console. Hook `SessionStart` đưa `AISDLC_SESSION` vào `CLAUDE_ENV_FILE`; `PreToolUse` trả `ask` + ghi phiếu.

**Tech Stack:** Java 21, picocli, Jackson, JUnit 5 + AssertJ (`TestRepo`, `HookCommandTest` pattern), Spring Boot +
Flyway (aisdlc-web), plugin markdown của Claude Code.

**Spec:** `docs/superpowers/specs/2026-11-05-aisdlc-self-approve-and-parallel-design.md`

## Global Constraints

- Chỉ Claude Code; không làm gì cho Codex.
- `sessionApprove` mặc định `false`; đọc từ `aisdlc/config.yaml` tại `baseRef` (không đọc được → `false`).
- Phiếu: hạn **60 giây**, dùng một lần, gắn session + feature key + động từ + đối số (+ hash cổng với approve/reject/reopen).
- `permission_mode` được hỏi người: `default`, `acceptEdits`, `plan`. Mọi giá trị khác (kể cả thiếu) → deny.
- Kênh mới tên đúng là `claude-code`.
- Lệnh chỉ dev: `approve`, `reject`, `reopen`, `step accept`, `scope allow`, `risk set`.
- Repo không bật `sessionApprove` và chỉ một tính năng mở: hành vi không đổi (mọi test hiện có phải xanh).
- Thông báo cho người dùng bằng tiếng Việt, cùng giọng với thông báo hiện có.
- `e2e/run-e2e.sh` đang có thay đổi chưa commit của dev: chỉ thêm, không ghi đè phần đó.

## Review Focus

1. **Gọi tool song song:** agent gửi lệnh duyệt cùng lúc với tool khác; `PreToolUse` của tool kia xóa phiếu → CLI exit 3
   với lời nhắc "chạy lại lệnh duyệt một mình", không bao giờ duyệt mà thiếu phiếu. Test ở Task 5.
2. **Repo không có `origin`/chưa fetch `baseRef`:** `sessionApprove` coi như tắt và `aisdlc status` nói rõ lý do
   (`không đọc được aisdlc/config.yaml ở origin/main`). Test ở Task 4.
3. **`session_id` có ký tự lạ** (`/`, `..`, rất dài): tên file an toàn, không thoát khỏi `.aisdlc/`. Test ở Task 1.
4. **Đổi nhánh sau khi gắn:** session trỏ tới tính năng không có trên nhánh → lỗi như `Context.feature()` hiện có,
   hook coi như session chưa gắn. Test ở Task 2.
5. **Dev có rule allow `Bash(aisdlc:*)`:** hook vẫn trả `ask`; kiểm tay trong Task 7 (không tự động hóa được).

---

### Task 1: Core — `SessionBindings` và `DevCommand`

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/model/SessionBindings.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/policy/DevCommand.java`
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/policy/BashHeuristics.java:10-11` (DEV_ONLY dùng `DevCommand`, thêm `risk set`)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/model/SessionBindingsTest.java`, `aisdlc-core/src/test/java/com/aisdlc/core/policy/DevCommandTest.java`

**Interfaces — Produces:**
- `SessionBindings.read(Path repoRoot, String sessionId) -> Optional<FeatureKey>`
- `SessionBindings.write(Path repoRoot, String sessionId, FeatureKey key)`
- `SessionBindings.all(Path repoRoot) -> Map<String, FeatureKey>`
- `SessionBindings.fileName(String sessionId) -> String` — `[A-Za-z0-9_-]` giữ nguyên, ký tự khác → `_`, cắt 100.
- `record DevCommand(String verb, List<String> args)`; `verb` ∈ `approve|reject|reopen|step accept|scope allow|risk set`.
- `DevCommand.mentions(String bash) -> boolean` — regex DEV_ONLY hiện có + `risk\s+set`.
- `DevCommand.parse(String bash) -> Optional<DevCommand>` — chỉ nhận **một lệnh đơn**: tùy chọn đường dẫn tới
  launcher (`.../aisdlc`), không có `;` `&&` `||` `|` `$(` `` ` `` `eval`, không gán biến đầu lệnh; đối số tách theo
  khoảng trắng, bỏ nháy bao ngoài. Ngược lại → empty.
- `DevCommand.key() -> String` — `verb + " " + String.join(" ", args)` (dùng để so khớp phiếu).

- [ ] **Step 1: Test** `SessionBindingsTest`: `writeThenRead` (ghi `abc`→PAY-1, đọc lại PAY-1); `twoSessionsIndependent`;
  `unsafeIdStaysInside` (`../../x/y`, chuỗi 300 ký tự → file nằm trong `.aisdlc/sessions/`); `missingIsEmpty`.
- [ ] **Step 2: Test** `DevCommandTest`: `parse("aisdlc approve brd")` → `approve [brd]`;
  `parse("/p/bin/aisdlc step accept S1")` → `step accept [S1]`; `parse("aisdlc reject brd -m 'thiếu số'")` →
  args `[brd, -m, thiếu số]`; empty cho `A=aisdlc; $A approve brd`, `bash -c "aisdlc approve brd"`,
  `cd x && aisdlc approve brd`, `aisdlc approve brd | cat`; `mentions(...)` true cho cả các dạng trên và
  `aisdlc risk set high`.
- [ ] **Step 3:** Chạy `mvn -q -pl aisdlc-core test -Dtest='SessionBindingsTest,DevCommandTest'` → FAIL (chưa có lớp).
- [ ] **Step 4:** Cài đặt; `BashHeuristics.check` dùng `DevCommand.mentions`.
- [ ] **Step 5:** Chạy `mvn -q -pl aisdlc-core test` → PASS (kể cả `BashHeuristicsTest`).
- [ ] **Step 6:** Commit `feat(core): gắn session với tính năng, nhận diện lệnh chỉ dev`.

### Task 2: CLI — phân giải tính năng theo session, `--feature`, tiền tố `[KEY]`

**Files:**
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/FeatureResolver.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/FeatureOption.java` (picocli mixin)
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/Context.java:236-250` (`feature()` gọi resolver; `sessionId` lấy từ env `AISDLC_SESSION` khi không phải hook)
- Modify: `InitCommand.java`, `UseCommand.java` (ghi binding + `.aisdlc/active`; cảnh báo cùng nhánh), mọi lệnh gọi `ctx.feature()` (thêm mixin)
- Modify: `StatusCommand.java` (không session + nhiều tính năng mở: liệt kê, kèm session đang gắn)
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/ParallelFeatureTest.java`

**Interfaces:**
- Consumes: `SessionBindings.*` (Task 1).
- Produces:
  - `FeatureResolver.openFeatures(Path root) -> List<FeatureKey>` — thư mục `aisdlc/features/<KEY>/` có meta trong
    checkout và cổng REVIEW chưa `APPROVED` (đọc qua `LocalGateStore`, hoặc cache web khi `approvals: web`).
  - `sealed interface Resolution permits Resolved, Ambiguous, None`; `Resolved(FeatureKey key)`,
    `Ambiguous(List<FeatureKey> open)`, `None()`.
  - `FeatureResolver.resolve(Path root, Optional<FeatureKey> explicit, Optional<String> sessionId) -> Resolution`
    theo đúng thứ tự spec 4.1 (explicit → binding còn trên nhánh → 0/1/≥2 tính năng mở → `.aisdlc/active`).
  - `Context.withFeatureOverride(FeatureKey)`; `Context.feature()` giữ chữ ký, xử lý `Ambiguous`: có console thì hỏi
    chọn số, không console → `Abort(2, "Có nhiều tính năng đang mở: PAY-1, PAY-2. Ghi rõ --feature <KEY>.")`.
  - `FeatureOption`: `@Option(names = {"--feature", "-f"}, paramLabel = "KEY")`.
  - `Context.featureBanner(FeatureKey) -> void` in `[KEY]` làm dòng đầu stdout; gọi một lần trong `feature()`.

- [ ] **Step 1: Test** `ParallelFeatureTest`:
  - `twoSessionsSameDirKeepTheirFeature`: env `AISDLC_SESSION=a` init PAY-1, `=b` init PAY-2; `status --json` với
    `a` báo PAY-1, với `b` báo PAY-2.
  - `noSessionTwoOpenNoConsoleExits2`: không env, `NO_CONSOLE`, `submit brd` → exit 2, `err` chứa `PAY-1`, `PAY-2`, `--feature`.
  - `noSessionTwoOpenConsolePicks`: `console("2")` → chạy trên tính năng thứ hai.
  - `explicitFeatureWins`: `--feature PAY-1` trong session `b` → PAY-1.
  - `unboundSessionOneOpenUsesIt`; `bindingMissingOnBranchErrors` (xóa meta của PAY-1 → exit 2 với thông báo
    "không có trên nhánh này").
  - `outputStartsWithKey`: `status` → dòng đầu `[PAY-1]`.
  - `sameBranchSecondInitWarnsOnce`: init PAY-2 khi PAY-1 đang mở → `out` chứa `git worktree`; init lại không cảnh báo lần hai.
- [ ] **Step 2:** `mvn -q -pl aisdlc-cli test -Dtest=ParallelFeatureTest` → FAIL.
- [ ] **Step 3:** Cài đặt. Cờ "đã cảnh báo" lưu `.aisdlc/sessions/.warned-<branch>` *(lựa chọn của builder nếu muốn chỗ khác)*.
- [ ] **Step 4:** `mvn -q -pl aisdlc-cli test` → PASS toàn bộ (test cũ một tính năng không đổi; nếu test cũ so khớp dòng đầu stdout thì sửa test theo tiền tố mới).
- [ ] **Step 5:** Commit `feat(cli): tính năng theo session, --feature, hỏi khi nhiều tính năng mở`.

### Task 3: Hook — `SessionStart` đặt `AISDLC_SESSION`, hook phân giải theo session

**Files:**
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/hook/HookCommand.java:173-195` (session-start), `:451-468` (`activeKey`/`state` dùng `FeatureResolver`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/policy/HookPolicy.java` (deny `AMBIGUOUS_FEATURE`, deny gán `AISDLC_SESSION`)
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/HookSessionTest.java`

**Interfaces:**
- Consumes: `FeatureResolver.resolve` (Task 2), `Context.sessionId()` (đã có, lấy từ input hook).
- Produces: `PolicyState` thêm trường `List<FeatureKey> ambiguous` (rỗng khi phân giải được);
  `HookPolicy.preToolUse` trả `Decision.deny("AMBIGUOUS_FEATURE", "Có nhiều tính năng đang mở (…). Chạy `aisdlc use <KEY>` (hỏi dev nếu chưa biết).")` cho lệnh ghi khi `ambiguous` không rỗng.

- [ ] **Step 1: Test** `HookSessionTest`:
  - `sessionStartAppendsEnvFile`: env `CLAUDE_ENV_FILE=<tmp>/env`, input `session_id: s1` → file chứa đúng một dòng `export AISDLC_SESSION=s1` (chạy hai lần không nhân đôi).
  - `noEnvFileIsSilent`.
  - `scopeCheckedAgainstSessionFeature`: session `a`→PAY-1 (bước S1 mở), `b`→PAY-2; Write file trong scope PAY-1 với `session_id: a` → không output; với `b` → deny OUT_OF_SCOPE.
  - `unboundSessionTwoOpenDeniesWrites`: `session_id: z` → deny chứa `aisdlc use`.
  - `assigningSessionVarDenied`: Bash `AISDLC_SESSION=b aisdlc use PAY-9` và `export AISDLC_SESSION=b` → deny.
- [ ] **Step 2:** `mvn -q -pl aisdlc-cli test -Dtest=HookSessionTest` → FAIL.
- [ ] **Step 3:** Cài đặt. `post-tool-use` và `user-prompt-submit` cũng dùng resolver (event mang đúng key).
- [ ] **Step 4:** `mvn -q -pl aisdlc-core,aisdlc-cli test` → PASS.
- [ ] **Step 5:** Commit `feat(hook): hook theo tính năng của session`.

### Task 4: `sessionApprove` từ `baseRef`

**Files:**
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/model/RepoConfig.java` (thêm `boolean sessionApprove`, khóa `sessionApprove`, mặc định false)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/model/SessionApproveSetting.java`
- Modify: `StatusCommand.java` (in dòng `Duyệt trong Claude Code: bật|tắt (<lý do>)`, và trường `sessionApprove` trong `--json`)
- Modify: `aisdlc-cli/src/main/resources/templates/config.yaml` (ghi chú `# sessionApprove: true` kèm giải thích MR riêng)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/model/SessionApproveSettingTest.java`

**Interfaces — Produces:**
- `record SessionApproveSetting(boolean enabled, String reason)`
- `SessionApproveSetting.read(GitRepo git, RepoConfig workingConfig) -> SessionApproveSetting` — dùng
  `git.fileAt(workingConfig.baseRef(), "aisdlc/config.yaml")` rồi `RepoConfig.parse(...).sessionApprove()`;
  không đọc được → `(false, "không đọc được aisdlc/config.yaml ở <baseRef>")`.

- [ ] **Step 1: Test**: `workingTreeOnlyIsOff` (sửa file chưa commit → false); `committedOnBaseIsOn` (repo git tạm, commit config có `sessionApprove: true`, `baseRef: HEAD`... dùng nhánh `main` làm baseRef → true); `missingBaseRefIsOffWithReason`.
- [ ] **Step 2:** FAIL → cài đặt → `mvn -q -pl aisdlc-core,aisdlc-cli test` PASS.
- [ ] **Step 3:** Commit `feat(core): sessionApprove đọc từ baseRef`.

### Task 5: Phiếu cho phép + hook `ask` + `confirmDev` nhận phiếu + kênh `claude-code` (local)

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/policy/DevGrants.java`
- Modify: `HookCommand.java` `preToolUse()` (nhánh lệnh chỉ dev; xóa phiếu cũ của session ở mọi PreToolUse khác; thêm `ask(String reason)` cạnh `deny`)
- Modify: `Context.java:280-290` — `confirmDev(FeatureKey key)` → `confirmDev(FeatureKey key, DevCommand action, String gateHash)`; 6 nơi gọi (`ApproveCommand:60`, `RejectCommand:31`, `ReopenCommand:27`, `StepCommand:146`, `ScopeCommand:42`, `RiskCommand:125`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/gate/Approval.java` (thêm thành phần `@JsonInclude(NON_NULL) String channel`; giữ các constructor cũ với `channel = null`), `GateService.approve(Gate, String approver, String channel)`
- Modify: `StatusCommand`, `ReviewCardText` (hiện "duyệt trong Claude Code" khi `channel = claude-code`)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/policy/DevGrantsTest.java`, `aisdlc-cli/src/test/java/com/aisdlc/cli/SessionApproveTest.java`

**Interfaces:**
- Consumes: `DevCommand.parse/mentions/key` (Task 1), `SessionApproveSetting.read` (Task 4), `FeatureResolver` (Task 2).
- Produces:
  - `record Grant(String sessionId, String feature, String command, String hash, Instant expiresAt)`
  - `DevGrants.issue(Path root, Grant g)`; `DevGrants.consume(Path root, String sessionId, String feature, String command, String hash, Instant now) -> boolean` (khớp và còn hạn → xóa, true); `DevGrants.clear(Path root, String sessionId)`. File `.aisdlc/grants/<SessionBindings.fileName(id)>.json`.
  - Approval channel: `Approval.CLAUDE_CODE = "claude-code"`.

Luồng hook (`sessionApprove` bật): `DevCommand.mentions` true mà `parse` empty → deny "gọi thẳng `aisdlc <lệnh>` để hiện hộp xác nhận". `parse` có giá trị: `permission_mode` ∉ {default, acceptEdits, plan} → deny "duyệt ở terminal riêng hoặc web"; ngược lại `issue` phiếu (hạn 60s, hash = `currentHash` của cổng với approve/reject/reopen, null với lệnh khác) rồi trả
`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"[PAY-1] Duyệt cổng BRD @ <hash8> — HIGH 1, WARN 2. Bấm Yes nếu chính bạn muốn duyệt."}}`.
`sessionApprove` tắt: hành vi cũ (deny qua `BashHeuristics`).

CLI: `confirmDev` có console → như cũ (gõ lại key, kênh terminal). Không console → `DevGrants.consume(root, ctx.sessionId(), key, action.key(), gateHash, now)`; true → tiếp tục với kênh `claude-code`; false → `Abort(3, "…Không có xác nhận trong Claude Code (hoặc đã hết hạn). Chạy lại lệnh duyệt một mình, không kèm lệnh khác.")`.

- [ ] **Step 1: Test** `DevGrantsTest`: `consumeOnce`; `expiredAfter60s`; `mismatchSessionFeatureCommandHash` (mỗi cái → false); `clearRemoves`.
- [ ] **Step 2: Test** `SessionApproveTest` (repo bật `sessionApprove` ở baseRef):
  - `hookAsksAndIssuesGrant` (`permission_mode: default`) → output có `"ask"`, có file phiếu.
  - `bypassDenies` (`bypassPermissions`), `missingModeDenies`.
  - `indirectFormDenied` (`bash -c "aisdlc approve brd"`).
  - `approveWithGrantNoConsole` → exit 0, `gates.json` có `"channel" : "claude-code"`, phiếu bị xóa; chạy lại → exit 3.
  - `otherToolClearsGrant` → sau một PreToolUse Read/Write khác, approve → exit 3, `err` chứa "một mình".
  - `hashChangedAfterAsk` → sửa `brd.md` sau khi ask → exit 3.
  - `disabledKeepsOldBehaviour` → hook deny, CLI exit 3 như `DevOnlyCommandTest`.
  - `stepAcceptAndScopeAllowWithGrant` → thành công, kênh ghi nhận nơi lưu được.
- [ ] **Step 3:** FAIL → cài đặt → `mvn -q -pl aisdlc-core,aisdlc-cli test` PASS (gồm `DevOnlyCommandTest`).
- [ ] **Step 4:** Commit `feat: duyệt trong Claude Code qua hộp xác nhận quyền`.

### Task 6: Web — kênh `claude-code`

**Files:**
- Create: `aisdlc-web/src/main/resources/db/migration/V6__claude_code_channel.sql` (bỏ CHECK cũ của `approvals.channel`, thêm CHECK `IN ('web','terminal','import','claude-code')`; tên constraint lấy từ `V2__approvals.sql`)
- Modify: `aisdlc-web/src/main/java/com/aisdlc/web/gates/GateActionController.java:71-73` — `channel(Principal p, JsonNode body)`: session → `web`; token: `"claude-code".equals(body.channel)` → `claude-code`, còn lại `terminal`
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/web/WebGates.java:340-346` — `act(..., String channel)` gửi `channel`
- Modify: `GateOverlay.java` + UI `GatePanel.tsx` (nhãn "trong Claude Code")
- Test: test controller hiện có của aisdlc-web (thêm case), `WebGatesTest` (body chứa `"channel":"claude-code"`), `ui` vitest nếu có test nhãn

- [ ] **Step 1: Test** server: token + `channel: claude-code` → lưu `claude-code`; token + `channel: web` → `terminal`; session → `web`. CLI: `WebGatesTest` approve bằng phiếu gửi `claude-code`.
- [ ] **Step 2:** FAIL → cài đặt → `mvn -q -pl aisdlc-web,aisdlc-cli test` và `npm --prefix aisdlc-web/ui test` PASS.
- [ ] **Step 3:** Commit `feat(web): kênh duyệt claude-code`.

### Task 7: Plugin, tài liệu, e2e

**Files:**
- Create: `plugin/commands/approve.md` (`/aisdlc:approve <gate>`), `plugin/commands/reject.md` (`/aisdlc:reject <gate> <lý do>`), `plugin/commands/accept.md` (`/aisdlc:accept <Sx>` và `/aisdlc:accept --file <path>` → `scope allow`)
- Modify: `plugin/commands/{brd,design,cases,review,step}.md` bước "Dừng": nếu `aisdlc status` báo "Duyệt trong Claude Code: bật" thì nhắc `/aisdlc:approve …` hoặc web; ngược lại giữ lời cũ. Giữ "Không tự duyệt khi dev chưa yêu cầu".
- Modify: `docs/huong-dan-su-dung.md` (mục 3, 4, 6, 8: duyệt trong Claude Code, nhiều tính năng song song, `--feature`), `docs/README.vi.md` tương ứng
- Modify: `e2e/run-e2e.sh` (chỉ thêm kịch bản: hai `AISDLC_SESSION` song song; duyệt bằng phiếu giả lập qua hook stdin)

Nội dung mỗi command mới: chạy `aisdlc review <gate>` để in review card, rồi chạy **riêng một lệnh** `aisdlc approve <gate>` (không song song với tool khác); nếu bị deny vì chế độ quyền thì nói dev duyệt ở terminal/web; không thử vòng.

- [ ] **Step 1:** Viết command và sửa lời nhắc.
- [ ] **Step 2:** `bash e2e/run-e2e.sh` → PASS.
- [ ] **Step 3: Kiểm tay** trong Claude Code thật: (a) `/aisdlc:approve brd` hiện hộp Yes/No, Yes → duyệt, No → không duyệt và lần sau hỏi lại; (b) có rule allow `Bash(aisdlc:*)` vẫn hiện hộp; (c) `--dangerously-skip-permissions` → bị chặn; (d) hai session song song đúng tính năng. Ghi kết quả vào mô tả commit.
- [ ] **Step 4:** Commit `feat(plugin): /aisdlc:approve, reject, accept; tài liệu và e2e`.
