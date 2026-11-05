# AISDLC Web 2a (web chỉ xem) — Kế hoạch triển khai

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CLI và hook gửi snapshot/event của mọi tính năng lên một web nội bộ chỉ xem, với ba màn: bảng quy trình kèm "Việc chờ tôi", trang review và theo dõi agent, cập nhật trực tiếp.

**Architecture:**
- `aisdlc-core` có thêm gói `web`. Gói này dựng snapshot JSON (hợp đồng v1 ở dưới) từ các file trạng thái có sẵn.
- `aisdlc-cli` ghi bản tin vào outbox `.aisdlc/web/outbox/` rồi gửi không chặn qua `java.net.http`.
- Module mới `aisdlc-web` gồm Spring Boot MVC, Postgres và Flyway. Module này lưu snapshot mới nhất và event, tính cột/nhãn/"việc chờ tôi" ở phía server, phát SSE và phục vụ SPA React (Vite) đóng sẵn trong jar.

**Tech Stack:**
- Java 17, Spring Boot 3.3.5 (web, jdbc, validation), Postgres 16, Flyway, Testcontainers 1.21.4.
- React 18.3.1, TypeScript 5.6, Vite 5.4, mermaid 11.4.1, Vitest 2.1.
- frontend-maven-plugin 1.15.1 với Node v20.18.0.
- Docker Compose.

**Spec:** `docs/superpowers/specs/2026-11-03-aisdlc-web-2a-design.md` (gốc: `docs/superpowers/specs/2026-11-02-aisdlc-design.md`).

## Global Constraints

- Mọi lỗi gửi lên web **mở**: không đổi exit code của lệnh, không đổi quyết định của hook, không in gì ra stdout của hook (stdout của hook là JSON cho Claude Code).
- Timeout gửi: 1 giây kết nối, 2 giây tổng mỗi request. Mỗi lần gọi gửi bù tối đa 20 bản tin. Outbox tối đa 2000 file, vượt thì bỏ event cũ nhất. Snapshot chỉ giữ bản mới nhất mỗi tính năng.
- Giới hạn kích thước: mỗi artifact 256 KB, mỗi diff bước 512 KB, cả snapshot 8 MB. Vượt 8 MB thì bỏ diff từ bước cũ nhất và đặt `truncated: true`.
- Bật web khi có `AISDLC_WEB_URL` + `AISDLC_WEB_TOKEN` (ưu tiên) hoặc file `~/.config/aisdlc/web.json` dạng `{"url":..,"token":..}`. Không có cấu hình thì không tạo `.aisdlc/web/` và không gửi gì.
- Hook PostToolUse < 300 ms khi web không truy cập được.
- Định danh repo: remote `origin` chuẩn hóa về `host/group/name` (chữ thường host, bỏ `.git`, bỏ user/port/scheme). Không có remote thì `local/<tên thư mục gốc>`.
- Thứ tự snapshot (§5b.5): nhận nếu `capturedAt` ≥ bản đang lưu. Bản cũ hơn cùng `clientId` thì bỏ (trả 200, `{"accepted":false}`). Bản cũ hơn khác `clientId` thì nhận và đặt cờ `clockSkew`.
- Web: token cá nhân, DB chỉ lưu SHA-256 của token. Ai đăng nhập cũng xem được mọi thứ. Payload ingest tối đa 8 MB. Event giữ 180 ngày.
- Phiên Claude Code coi như đã dừng khi có Stop, hoặc khi quá 30 phút không có event.
- Giao diện tiếng Việt, có sáng/tối (`prefers-color-scheme`), bề rộng tối thiểu 1280px. Web không có nút ghi trạng thái.
- Cột (§5b.2):
  - BRD/DESIGN/CASES: cổng đầu tiên trong luồng chưa APPROVED.
  - CASES đã APPROVED, REVIEW chưa: còn bước chưa ACCEPTED thì `CODE`, ngược lại `REVIEW`.
  - REVIEW APPROVED thì `DONE`.
  - Luồng LITE không có BRD/DESIGN.
- Copy lệnh terminal hiển thị trên web phải đúng tên lệnh CLI hiện có: `aisdlc approve <brd|design|cases|review>`, `aisdlc step accept <Sx>`, `aisdlc eval`, `aisdlc submit <gate>`, `aisdlc reopen <gate>`.

## Hợp đồng snapshot v1 (dùng chung CLI ↔ web)

```json
{
  "schemaVersion": 1,
  "repo": { "id": "gitlab.corp/pay/payment-svc", "branch": "feature/PAY-1" },
  "clientId": "uuid", "capturedAt": "ISO-8601", "sender": "git user.name",
  "feature": { "key": "PAY-1", "title": "...", "flow": "FULL|LITE", "owner": "email|null", "createdAt": "ISO" },
  "gates": [ { "gate": "BRD|DESIGN|CASES|REVIEW", "state": "<GateState>", "currentHash": "hex",
               "submission": { "hash": "", "at": "", "findings": [ { "code": "", "message": "" } ] } | null,
               "approval": { "hash": "", "approver": "", "at": "" } | null,
               "lastFeedback": { "kind": "REJECTED|REOPENED", "by": "", "at": "", "comment": "" } | null } ],
  "steps": [ { "id": "S1", "title": "", "scope": ["glob"], "budget": { "files": 1, "lines": 40 },
               "caseIds": ["C1"], "status": "<StepStatus>",
               "record": <StepRecord as in steps.json> | null,
               "diff": { "text": "unified diff", "truncated": false } | null } ],
  "cases": [ { "id": "C1", "step": "S1", "kind": "happy|error|boundary", "title": "",
               "request": "METHOD path (rút gọn)", "expect": "status + body rút gọn", "raw": "YAML đầy đủ của case" } ],
  "openQuestions": [ { "gate": "BRD|DESIGN", "text": "" } ],
  "artifacts": { "<đường dẫn tương đối trong thư mục tính năng>": { "content": "", "truncated": false } },
  "eval": <eval/report.json nguyên văn> | null,
  "review": <review-agent.json nguyên văn> | null,
  "truncated": false
}
```

Event (lô): `{"repoId","clientId","events":[{"type","at","feature":null|"PAY-1","step":null|"S1","path":null|"rel/path","outOfScope":false,"sender","detail":null|"..."}]}`.
`type` ∈ `COMMAND` (detail = tên lệnh, ví dụ `step finish S1`), `FILE_EDIT`, `SESSION_START`, `SESSION_STOP`.

## Review Focus

1. **Web chết giữa một lệnh dev:** lệnh vẫn chạy xong với exit code cũ, và bản tin nằm trong outbox. Có test ở Task 2, kiểm cả việc hook PreToolUse không in thêm gì.
2. **Hai clone của cùng một tính năng gửi lệch giờ:** bản mới hơn không bị bản gửi bù muộn từ clone kia đè lên. Có test ở Task 4: cùng `clientId` cũ thì bị bỏ, khác `clientId` cũ thì được nhận kèm `clockSkew`.
3. **Artifact hoặc diff khổng lồ** (ví dụ dev lỡ commit file dump 50 MB trong một bước): CLI vẫn gửi được dưới 8 MB, và web hiện "đã cắt". Có test ở Task 1.
4. **Tính năng cũ không có `owner`, hoặc email không khớp user nào:** thẻ vẫn hiện. "Việc chờ tôi" rơi về login của người gửi đầu tiên. Có test ở Task 5.
5. **Trình duyệt mất SSE, ví dụ server khởi động lại:** trình duyệt tự nối lại và tải lại toàn bộ, không hiện dữ liệu cũ mãi. Có test ở Task 6 cho logic nối lại (thuần TS) và kiểm tay ở Task 9.

---

### Task 1: Core — dựng snapshot, owner, định danh repo

**Files:**
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/web/FeatureSnapshot.java` (records của hợp đồng v1, Jackson)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/web/SnapshotBuilder.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/web/RepoIdentity.java`
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/web/FeedbackLog.java` (parse `feedback.md`)
- Create: `aisdlc-core/src/main/java/com/aisdlc/core/spec/OpenQuestions.java` (facade công khai cho `MarkdownSections.openQuestions`)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/model/FeatureMeta.java` (thêm `String owner` ở cuối, nullable; YAML cũ thiếu trường vẫn đọc được)
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/InitCommand.java` (ghi `owner` = `git config user.email`, rỗng/lỗi thì null)
- Modify: `aisdlc-core/src/main/java/com/aisdlc/core/git/GitRepo.java` (thêm `userEmail()`, `remoteUrl(String name): Optional<String>`, `currentBranch(): Optional<String>`)
- Test: `aisdlc-core/src/test/java/com/aisdlc/core/web/SnapshotBuilderTest.java`, `RepoIdentityTest.java`, `FeedbackLogTest.java`

**Interfaces:**
- Consumes: `GateService.state(Gate)`, `GateStore.submission/approval`, `Gate.sequence(Flow)`, `StepService.all()`, `List<StepSpec>`, `CasesSpec`, `GitRepo.patch(from,to)`, `GitRepo.snapshotTree()`, `EvalReport` (file `eval/report.json`), `FeaturePaths`.
- Produces:
  - `RepoIdentity.normalize(String remoteUrl): String`
  - `RepoIdentity.of(GitRepo git, Path root): String`
  - `SnapshotBuilder.build(SnapshotInput in): FeatureSnapshot`. `SnapshotInput` là record gồm `FeaturePaths paths, FeatureMeta meta, Flow flow, GateService gates, GateStore store, List<StepSpec> specs, List<StepRecord> records, Optional<CasesSpec> cases, GitRepo git, String repoId, String branch, String clientId, Instant capturedAt, String sender`.
  - `FeatureSnapshot.toJson(): byte[]` và hằng số `FeatureSnapshot.MAX_BYTES = 8 * 1024 * 1024`.
  - `OpenQuestions.of(String markdown): List<String>`.
  - `FeedbackLog.last(Path feedbackMd, Gate gate): Optional<Feedback>`, với `record Feedback(String kind, String by, Instant at, String comment)`.

- [ ] **Step 1: Viết test thất bại**
  - `RepoIdentityTest.normalizesSshAndHttps`:
    - `git@GitLab.Corp:pay/payment-svc.git`, `https://user:pw@gitlab.corp:8443/pay/payment-svc.git` và `ssh://git@gitlab.corp:2222/pay/payment-svc` đều cho `gitlab.corp/pay/payment-svc`.
    - `RepoIdentity.of` trên repo không có remote cho `local/<dirname>`.
  - `FeedbackLogTest`:
    - Nội dung `## DESIGN — trả lại bởi An lúc 2026-11-03T01:00:00Z\n\nThiếu bước lỗi\n\n## DESIGN — mở lại bởi An lúc 2026-11-03T02:00:00Z\n\n` → `last(DESIGN)` là `REOPENED`, by `An`, comment rỗng.
    - `last(BRD)` rỗng.
  - `SnapshotBuilderTest`: dùng repo git tạm và fixture như các test core hiện có. Khẳng định các ý sau:
    - `gates` theo `Gate.sequence(flow)`, kèm `state` và `currentHash`.
    - `steps[i].diff.text` chứa `+++ b/<file>` của bước đó. Bước `IN_PROGRESS` thì diff được tính tới cây hiện tại.
    - `openQuestions` lấy từ `brd.md` có `- [ ] Hạn mức theo ngày hay tháng?`.
    - Artifact 300 KB thì `content.length()` ≤ 256*1024 và `truncated` là true.
    - Hai bước có diff 5 MB mỗi bước thì `toJson().length` ≤ `MAX_BYTES`, `steps[0].diff.truncated` là true, `steps[1].diff.text` còn nguyên và `truncated` cấp trên là true.
    - Không có `eval/report.json` thì `eval` là null.
  - `FeatureMetaTest.readsLegacyYamlWithoutOwner`: đọc YAML cũ thiếu `owner` thì được `owner()` là null.
- [ ] **Step 2: Chạy `mvn -q -pl aisdlc-core test`**. Kỳ vọng: FAIL do thiếu lớp.
- [ ] **Step 3: Cài đặt.** `artifacts` gồm `brd.md`, `design.md`, `design.yaml`, `cases.yaml`, `feedback.md` và mọi file trong `db/`, nếu file tồn tại. `cases[].request` có dạng `"<METHOD> <path>"`, `cases[].expect` có dạng `"<status> <body rút gọn 120 ký tự>"`, `raw` là YAML của case serialize lại. Cắt nội dung theo số byte UTF-8 và không cắt giữa ký tự.
- [ ] **Step 4: Chạy lại `mvn -q -pl aisdlc-core,aisdlc-cli -am test`**. Kỳ vọng: PASS, kể cả các test cũ của init.
- [ ] **Step 5: Commit** `feat(core): feature snapshot v1, repo identity, owner`.

### Task 2: CLI — outbox, gửi không chặn, `aisdlc web status|push`, gửi sau mỗi lệnh đổi trạng thái

**Files:**
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/web/WebConfig.java` (đọc env, sau đó tới `~/.config/aisdlc/web.json`; home lấy từ env `HOME` qua `Context.env`)
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/web/Outbox.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/web/WebClient.java` (`java.net.http.HttpClient`)
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/web/WebPublisher.java`
- Create: `aisdlc-cli/src/main/java/com/aisdlc/cli/commands/WebCommand.java` (`web status`, `web push`)
- Modify: `Main.java` (đăng ký `WebCommand`)
- Modify: `Context.java` (thêm `webPublisher(): WebPublisher` và `withWebClient(...)` cho test)
- Modify: các lệnh `InitCommand`, `SubmitCommand`, `ApproveCommand`, `RejectCommand`, `ReopenCommand`, `StepCommand` (start/finish/accept), `ScopeCommand` (allow), `EvalCommand`. Mỗi lệnh gọi `ctx.webPublisher().afterCommand(feature, "<tên lệnh>")` ở cuối, kể cả khi exit 1 do có phát hiện. Lệnh bị Abort trước khi đổi trạng thái thì không gọi.
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/WebPublisherTest.java`, `OutboxTest.java`

**Interfaces:**
- Consumes: Task 1 `SnapshotBuilder`, `RepoIdentity`, `FeatureSnapshot`. Hợp đồng snapshot và event ở đầu tài liệu.
- Produces:
  - `WebPublisher.afterCommand(Context.Feature f, String command): void`. Hàm này ghi event `COMMAND` và snapshot vào outbox rồi gọi `flush()`.
  - `WebPublisher.event(Path root, Event e): void`, ghi vào outbox rồi gọi `flush()`.
  - `WebPublisher.flush(): FlushResult(int sent, int pending)`.
  - `WebPublisher.enabled(): boolean`.
  - `Outbox(Path root)` có `putSnapshot(String featureKey, byte[])` (thay file `snapshot-<KEY>.json`), `putEvent(byte[])` (file `event-<epochMillis>-<uuid8>.json`), `list(int max): List<Path>` (cũ nhất trước, snapshot trước event cùng thời điểm), `remove(Path)`, `size()`.
  - Mọi file ghi theo kiểu tạm rồi `ATOMIC_MOVE`.
  - `clientId` đọc hoặc tạo ở `.aisdlc/web/client-id`. Lần gửi thành công cuối ghi ở `.aisdlc/web/last-ok`.
  - Endpoint: `POST {url}/api/ingest/snapshot` (body là snapshot) và `POST {url}/api/ingest/events` (body là lô event), header `Authorization: Bearer <token>`. Server trả 2xx thì xóa file. Trả 4xx (trừ 401/408/429) thì chuyển file sang `.aisdlc/web/rejected/` để không gửi lại mãi. Các lỗi khác thì giữ file.

- [ ] **Step 1: Viết test thất bại.** Dùng `com.sun.net.httpserver.HttpServer` làm web giả, chạy trên cổng ngẫu nhiên.
  - `OutboxTest.capsAt2000DroppingOldestEvents`: thêm 2001 event và 1 snapshot thì `size()` là 2000 và snapshot vẫn còn.
  - `OutboxTest.snapshotReplacesPrevious`: hai lần `putSnapshot("PAY-1",...)` thì chỉ còn một file, chứa bản sau.
  - `WebPublisherTest.disabledWritesNothing`: không có env và không có web.json thì không tạo `.aisdlc/web`.
  - `WebPublisherTest.serverDownKeepsQueueAndExitCodeUnchanged`: trỏ URL tới một cổng đã đóng rồi chạy `Main.run(ctx, "submit", "brd")`.
    - Exit code phải bằng exit code khi tắt web.
    - Outbox có 1 snapshot và 1 event.
    - Lệnh kết thúc dưới 3 giây.
  - `WebPublisherTest.flushSendsBacklogMax20`: có 25 event chờ và server trả 200, một lần `flush()` thì gửi 20 và còn 5.
  - `WebPublisherTest.sendsBearerAndContract`:
    - Server nhận header `Authorization: Bearer t0k`.
    - JSON có `schemaVersion: 1`, `feature.key` là `PAY-1`, `clientId` giữ nguyên giữa các lần.
  - `WebPublisherTest.rejected400MovesToRejected`.
  - `CliFlowTest`, sửa test có sẵn: `aisdlc web status` in ra các dòng `Web: tắt`, hoặc `Web: <url>`, `Đang chờ gửi: N`, `Gửi thành công lần cuối: <ISO|chưa>`.
- [ ] **Step 2: Chạy `mvn -q -pl aisdlc-cli -am test -Dtest='WebPublisherTest,OutboxTest'`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.** `web push` dựng snapshot cho mọi thư mục trong `aisdlc/features/*/feature.yaml` rồi gọi `flush()` cho tới khi hết hàng hoặc gặp lỗi, và in `Đã gửi N, còn chờ M`. Mọi exception trong publisher (kể cả `Error` trừ `OutOfMemoryError`) đều bị bắt và ghi một dòng `aisdlc web: <lỗi>` ra **stderr**, không bao giờ ra stdout.
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-cli -am test`**. Kỳ vọng: PASS toàn bộ, kể cả test cũ.
- [ ] **Step 5: Commit** `feat(cli): web outbox and non-blocking publisher`.

### Task 3: Hook — event sửa file (PostToolUse), SessionStart/Stop

**Files:**
- Modify: `aisdlc-cli/src/main/java/com/aisdlc/cli/hook/HookCommand.java`. Thêm lệnh con `post-tool-use`. Trong `session-start` và `stop`, sau khi xử lý phần cũ, gửi event `SESSION_START` / `SESSION_STOP`.
- Modify: `plugin/hooks/hooks.json`. Thêm `"PostToolUse":[{"matcher":"Edit|Write|MultiEdit|NotebookEdit|Bash","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/bin/aisdlc hook post-tool-use","timeout":5}]}]`.
- Modify: `plugin/test/plugin-files-test.sh` (kiểm có mục PostToolUse)
- Test: `aisdlc-cli/src/test/java/com/aisdlc/cli/HookCommandTest.java` (thêm test)

**Interfaces:**
- Consumes: `WebPublisher.event`, `PolicyState` (hàm `state(root)` có sẵn trong HookCommand), `StepService.current()`, `StepService.allowedGlobs(stepId)`, `ScopeMatcher`.
- Produces: event `FILE_EDIT` gồm `feature`, `step` (bước `IN_PROGRESS` hoặc null), `path` (tương đối repo, file ngoài repo thì bỏ qua), `outOfScope` (khi có bước đang chạy và path không khớp `allowedGlobs`).
  - Với Bash: chỉ gửi khi có bước `IN_PROGRESS`, với `path` null và `detail` = 120 ký tự đầu của lệnh.

- [ ] **Step 1: Viết test thất bại.**
  - `HookCommandTest.postToolUseEmitsFileEditWithScope`: có bước S1 đang chạy với phạm vi `src/main/java/**/limit/web/**`.
    - Write `README2.txt` thì outbox có event `FILE_EDIT` với `outOfScope: true`, `step: "S1"`.
    - Write file trong phạm vi thì `outOfScope: false`.
    - stdout rỗng.
  - `HookCommandTest.postToolUseBashOnlyWhenStepRunning`.
  - `HookCommandTest.postToolUseFastWhenWebDown`: URL trỏ tới cổng đóng. Chạy 5 lần, trung vị thời gian dưới 300 ms (đo `System.nanoTime` quanh `Main.run`).
  - `HookCommandTest.postToolUseOutsideAisdlcRepoSilent`.
  - `HookCommandTest.sessionStartStillOutputsContextAndEmitsEvent`: JSON `additionalContext` giữ nguyên, outbox có `SESSION_START`.
- [ ] **Step 2: Chạy `mvn -q -pl aisdlc-cli -am test -Dtest=HookCommandTest`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.** `post-tool-use` luôn exit 0 và không in gì ra stdout. Khi web đang tắt thì không đọc trạng thái gì: kiểm `enabled()` trước, để hook gần như miễn phí.
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-cli -am test && bash plugin/test/plugin-files-test.sh`**. Kỳ vọng: PASS, rc=0.
- [ ] **Step 5: Commit** `feat(hook): post-tool-use file edit events and session events`.

### Task 4: Web backend — module, schema, user/token, ingest

**Files:**
- Modify: `pom.xml` (thêm module `aisdlc-web`)
- Create: `aisdlc-web/pom.xml`. Gồm spring-boot-starter-web, -jdbc, -validation, flyway-core, flyway-database-postgresql, postgresql, jackson; test dùng spring-boot-starter-test và testcontainers postgresql. `spring-boot-maven-plugin` repackage ra `aisdlc-web.jar`. **Không** phụ thuộc `aisdlc-core`: web đọc snapshot như JSON (`JsonNode`) để hai bên chỉ ràng buộc qua hợp đồng.
- Create: `aisdlc-web/src/main/java/com/aisdlc/web/WebApplication.java`. Nếu `args[0] == "user"` thì chạy `UserAdminCli` rồi thoát, không mở cổng web.
- Create: `aisdlc-web/src/main/java/com/aisdlc/web/auth/{TokenService,AuthFilter,UserAdminCli,UserRepository}.java`
- Create: `aisdlc-web/src/main/java/com/aisdlc/web/ingest/{IngestController,SnapshotStore,EventStore}.java`
- Create: `aisdlc-web/src/main/resources/db/migration/V1__init.sql`
- Create: `aisdlc-web/src/main/resources/application.yml`. Gồm `server.port: 8080`, datasource lấy từ env `AISDLC_DB_URL/USER/PASSWORD`, `spring.servlet.multipart` không dùng, và `server.tomcat.max-swallow-size`/`max-http-form-post-size` ≥ 8 MB.
- Test: `aisdlc-web/src/test/java/com/aisdlc/web/IngestIT.java`, `UserAdminCliTest.java`

**Interfaces:**
- Consumes: hợp đồng snapshot và event ở đầu tài liệu.
- Produces:
  - Bảng:
    - `users(id bigserial, login text unique, display_name text, email text, token_sha256 text unique, created_at timestamptz)`
    - `repos(id text primary key, last_seen_at timestamptz)`
    - `features(repo_id text, feature_key text, snapshot jsonb, captured_at timestamptz, client_id text, clock_skew boolean, first_sender_login text, last_sender_login text, synced_at timestamptz, primary key(repo_id, feature_key))`
    - `events(id bigserial, repo_id text, feature_key text null, type text, at timestamptz, step text, path text, out_of_scope boolean, sender_login text, detail text, received_at timestamptz)`, có index `(repo_id, feature_key, at)`.
  - `SnapshotStore.upsert(String login, JsonNode snapshot): UpsertResult(boolean accepted, boolean clockSkew)`.
  - `EventStore.append(String login, JsonNode batch): int`.
  - Một Spring `ApplicationEventPublisher` event `FeatureChanged(String repoId, String featureKey)`, phát sau mỗi upsert hoặc append thành công. Task 5 dùng event này cho SSE.
  - Auth:
    - `AuthFilter` nhận `Authorization: Bearer` hoặc cookie `AISDLC_SESSION` (giá trị chính là token, HttpOnly, SameSite=Strict).
    - `POST /api/login {token}` đặt cookie. `POST /api/logout` xóa cookie.
    - Mọi `/api/**` trừ `/api/login` đều cần auth, thiếu thì trả 401. Tài nguyên tĩnh không cần.
  - CLI admin: `java -jar aisdlc-web.jar user add <login> "<Tên>" --email <e>` in `Token: <32 byte base64url>` đúng một lần. `user list` in login, tên, email.

- [ ] **Step 1: Viết test thất bại.** Dùng `@SpringBootTest(webEnvironment=RANDOM_PORT)` với Testcontainers `postgres:16-alpine`.
  - `IngestIT.rejectsMissingOrBadToken401`
  - `IngestIT.acceptsSnapshotAndStoresLatest`
  - `IngestIT.olderSameClientIgnored`: snapshot t2 rồi t1 cùng clientId. Response `accepted:false`, DB giữ t2.
  - `IngestIT.olderOtherClientAcceptedWithSkew`: `clock_skew` là true.
  - `IngestIT.rejectsWrongSchemaVersion400`: thiếu `feature.key` cũng trả 400.
  - `IngestIT.rejectsOver8MB413`
  - `IngestIT.eventsBatchAppended`: 3 event thì có 3 dòng với `sender_login` là login của token.
  - `IngestIT.loginSetsHttpOnlyCookie`
  - `UserAdminCliTest.addPrintsTokenOnceStoresHash`: DB không chứa chuỗi token gốc.
- [ ] **Step 2: Chạy `mvn -q -pl aisdlc-web test`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.** Xóa event cũ hơn 180 ngày bằng `@Scheduled` mỗi ngày.
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-web verify`**. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(web): module, schema, token auth, ingest`.

### Task 5: Web backend — API đọc, quy tắc bảng, "việc chờ tôi", SSE

**Files:**
- Create: `aisdlc-web/src/main/java/com/aisdlc/web/board/BoardRules.java` (hàm thuần trên `JsonNode` snapshot)
- Create: `aisdlc-web/src/main/java/com/aisdlc/web/board/{BoardController,FeatureController,StreamController,SseHub}.java`
- Test: `aisdlc-web/src/test/java/com/aisdlc/web/board/BoardRulesTest.java`, `aisdlc-web/src/test/java/com/aisdlc/web/BoardIT.java`

**Interfaces:**
- Consumes: Task 4 gồm các bảng, `FeatureChanged`, `AuthFilter` (cung cấp login hiện tại qua request attribute `aisdlc.login`).
- Produces (JSON dùng chung cho frontend ở Task 6–8):
  - `BoardRules.column(JsonNode snap): String`, trả về `BRD|DESIGN|CASES|CODE|REVIEW|DONE` theo Global Constraints.
  - `BoardRules.labels(JsonNode snap): List<String>`, gồm các mã sau:
    - `AWAITING_APPROVAL:<GATE>` khi cổng đang chờ duyệt;
    - `REJECTED:<GATE>` khi cổng `NOT_SUBMITTED` và `lastFeedback.kind=REJECTED`;
    - `STALE:<GATE>`;
    - `FINDINGS:<GATE>` khi cổng `SUBMITTED_WITH_FINDINGS`;
    - `CODING:<Sx>` khi bước `IN_PROGRESS`;
    - `AWAITING_ACCEPT:<Sx>` khi bước `AWAITING_DEV`;
    - `EVAL_FAIL` khi `eval.passed=false`.
  - `BoardRules.inbox(JsonNode snap): List<InboxItem(String kind, String text, String command)>`. Mỗi loại kèm lệnh terminal như sau:

    | Loại | Lệnh |
    |---|---|
    | cổng chờ duyệt | `aisdlc approve <gate lowercase>` |
    | bước `AWAITING_DEV` | `aisdlc step accept <Sx>` |
    | câu hỏi mở | `aisdlc submit <brd\|design>` sau khi trả lời |
    | bước có `record.check.casesNotPassed` khác rỗng hoặc `outOfScope` khác rỗng | `aisdlc step finish <Sx>` |
    | eval không đạt | `aisdlc eval` |
    | STALE | `aisdlc submit <gate>` hoặc `aisdlc reopen <gate>` |
    | FINDINGS | `aisdlc submit <gate>` |

  - `BoardRules.evalSummary(JsonNode snap): Optional<{passed, casesPassed, casesTotal}>`. Hàm lấy `eval.passed`. Số case lấy từ `detail` của hard check `CASES_ALL_PASS` theo regex `(\d+)/(\d+)`. Không khớp thì hai số đó là null.
  - `BoardRules.owner(JsonNode snap, Map<String,String> emailToLogin, String firstSenderLogin): String`. Thứ tự ưu tiên: login khớp email, rồi `firstSenderLogin`, rồi email thô.
  - `GET /api/board?repo=&owner=&hideDoneOlderThanDays=7` trả `{ "me": "<login>", "cards": [ { "repoId","branch","key","title","flow","owner","column","labels":[],"eval":{"passed":bool,"casesPassed":n,"casesTotal":n}|null,"syncedAt","clockSkew":bool,"session":"ACTIVE|STOPPED|NONE" } ], "inbox": [ { "repoId","key","kind","text","command" } ] }`.
    - `inbox` chỉ gồm việc của các tính năng có `owner == me`.
    - Thẻ DONE có `synced_at` cũ hơn N ngày thì bị ẩn.
  - `GET /api/features/{repoId}/{key}` trả `{ "snapshot": <nguyên văn>, "column", "labels", "owner", "syncedAt", "clockSkew", "lastSender" }`. Vì `repoId` có dấu `/`, dùng mẫu `/api/features/**` và tách `key` ở đoạn cuối.
  - `GET /api/features/{repoId}/{key}/events?limit=200` trả event mới nhất trước.
  - SSE:
    - `GET /api/stream` phát sự kiện `card` với `{repoId,key}` mỗi khi có `FeatureChanged`.
    - `GET /api/features/{repoId}/{key}/stream` phát `snapshot` (body rỗng, client tự tải lại) hoặc `event` (event vừa thêm).
    - Cả hai gửi heartbeat comment mỗi 20 giây, dùng `SseEmitter` timeout 0.
  - `session`: `ACTIVE` khi event `SESSION_START` hoặc `FILE_EDIT` cuối cùng mới hơn 30 phút và không có `SESSION_STOP` nào sau nó. `STOPPED` khi đã có event phiên. `NONE` khi chưa có.

- [ ] **Step 1: Viết test thất bại.**
  - `BoardRulesTest`, test bảng với các snapshot JSON tối thiểu:
    - FULL, BRD đang `AWAITING_APPROVAL` → cột `BRD`, có nhãn `AWAITING_APPROVAL:BRD`, inbox có `aisdlc approve brd`.
    - FULL, DESIGN `STALE` → cột `DESIGN`, nhãn `STALE:DESIGN`.
    - LITE, CASES `NOT_SUBMITTED` → cột `CASES`.
    - CASES `APPROVED`, S1 `ACCEPTED`, S2 `NOT_STARTED` → `CODE`.
    - CASES `APPROVED`, mọi bước `ACCEPTED`, REVIEW `NOT_SUBMITTED` → `REVIEW`.
    - REVIEW `APPROVED` → `DONE`.
    - S2 `AWAITING_DEV` với `outOfScope:["README2.txt"]` → inbox có `aisdlc step accept S2` và mục về file ngoài phạm vi.
    - Cổng `NOT_SUBMITTED` kèm `lastFeedback.kind=REJECTED` → nhãn `REJECTED:DESIGN`.
    - `owner` null với first sender `an` → `an`. Email `an@corp` ánh xạ sang `an.nguyen` → `an.nguyen`.
  - `BoardIT.boardShowsCardAndInboxForOwner`: ingest snapshot với owner là email của user A.
    - A thấy mục inbox.
    - B thấy thẻ nhưng không thấy mục inbox.
  - `BoardIT.featureStreamPushesOnIngest`: mở SSE bằng `HttpClient` rồi ingest event, nhận được `event:` trong vòng 2 giây.
  - `BoardIT.featureDetailWithSlashRepoId`.
- [ ] **Step 2: Chạy `mvn -q -pl aisdlc-web test`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-web verify`**. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(web): board rules, read API, SSE`.

### Task 6: Frontend — khung, build vào jar, đăng nhập, màn A (bảng + Việc chờ tôi)

**Files:**
- Create: `aisdlc-web/ui/{package.json,package-lock.json,tsconfig.json,vite.config.ts,index.html}`. Dependency gồm `react@18.3.1`, `react-dom@18.3.1`, `react-router-dom@6.28.0`, `mermaid@11.4.1`. devDependency gồm `typescript@5.6.3`, `vite@5.4.11`, `@vitejs/plugin-react@4.3.4`, `vitest@2.1.8`.
  - Không dùng thư viện UI. CSS thuần có token màu đặt trên `:root` và định nghĩa lại trong `@media (prefers-color-scheme: dark)`.
- Create: `aisdlc-web/ui/src/{main.tsx,App.tsx,api.ts,sse.ts,format.ts,styles.css}`, `aisdlc-web/ui/src/pages/{Login.tsx,Board.tsx}`, `aisdlc-web/ui/src/components/{Card.tsx,Inbox.tsx,CopyCommand.tsx}`
- Create: `aisdlc-web/ui/src/{sse.test.ts,format.test.ts}`
- Modify: `aisdlc-web/pom.xml`. Thêm `frontend-maven-plugin` 1.15.1, cài Node v20.18.0 vào `aisdlc-web/target/node`, chạy `npm ci`, `npm test -- --run` và `npm run build` ở phase `generate-resources`. Thêm `maven-resources-plugin` để chép `ui/dist` vào `target/classes/static`. Cờ `-Dskip.ui` bỏ qua toàn bộ phần UI (dùng cho Task 4–5 khi chạy nhanh).
- Modify: Spring thêm `SpaController`, trả `index.html` cho mọi đường dẫn không bắt đầu bằng `/api` và không phải file tĩnh.
- Modify: `.gitignore` (thêm `aisdlc-web/ui/node_modules/`, `aisdlc-web/ui/dist/`)

**Interfaces:**
- Consumes: Task 5 API và SSE, Task 4 `/api/login` và `/api/logout`.
- Produces:
  - `api.ts`: `getBoard(params)`, `getFeature(repoId,key)`, `getEvents(repoId,key)`, `login(token)`. Khi API trả 401, chuyển về `/login`.
  - `sse.ts`: `subscribe(url, onMessage, onReset): () => void`. Khi lỗi, đóng kết nối rồi nối lại sau backoff 1 → 2 → 4 → … tối đa 30 giây. Mỗi lần nối lại thành công thì gọi `onReset` để trang tải lại toàn bộ.
  - `format.ts`: `ago(iso, now): string` trả "vừa xong", "N phút trước", "N giờ trước" hoặc "N ngày trước". `budgetLevel(actual, budget): "ok"|"over"|"far"` trả `ok` khi ≤1×, `over` khi ≤1,5×, `far` khi >1,5×. `labelText(code): string` dịch mã nhãn sang tiếng Việt: "Chờ duyệt BRD", "Bị trả lại DESIGN", "Đã đổi sau duyệt (STALE) CASES", "Có phát hiện BRD", "Đang code S2", "Chờ accept S2", "Eval không đạt".
  - Route: `/` (Board), `/login`, `/f/:repoId*/:key` (Task 7), `/f/:repoId*/:key/live` (Task 8).

- [ ] **Step 1: Viết test thất bại.**
  - `format.test.ts`:
    - `ago` cho 30 giây, 5 phút, 3 giờ, 2 ngày.
    - `budgetLevel(40,40)` = ok, `(60,40)` = over, `(61,40)` = far, budget 0 → ok.
    - `labelText("AWAITING_ACCEPT:S2")` = "Chờ accept S2".
  - `sse.test.ts`: dùng `EventSource` giả.
    - Lỗi 3 lần liên tiếp thì backoff là 1000, 2000, 4000 ms.
    - Mở lại thành công thì `onReset` được gọi một lần.
    - Hàm hủy đăng ký dừng việc nối lại.
- [ ] **Step 2: Chạy `cd aisdlc-web/ui && npm ci && npx vitest --run`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
  - Login: ô nhập token, nút "Đăng nhập", lỗi 401 hiện "Token không đúng".
  - Board:
    - Sáu cột "BRD · Thiết kế · Case · Code · Review · Xong".
    - Bộ lọc repo và chủ tính năng (select), checkbox "Ẩn đã xong quá 7 ngày" (mặc định bật).
    - Khung "Việc chờ tôi" ở trên cùng. Khi rỗng thì hiện "Không có việc nào chờ bạn."
    - Thẻ có nhãn, eval tóm tắt "ĐẠT 6/6" hoặc "KHÔNG ĐẠT 5/6", "đồng bộ X trước", biểu tượng cảnh báo khi `clockSkew`, chấm xanh khi `session=ACTIVE`.
    - Bấm thẻ thì mở trang review. Thẻ ở cột Code có thêm link "Theo dõi".
  - `CopyCommand`: hiện lệnh dạng `<code>` kèm nút "Copy" (`navigator.clipboard`, có fallback chọn text).
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-web verify`**. Build UI, chạy Vitest và test Java. Kỳ vọng: PASS và `target/classes/static/index.html` tồn tại.
- [ ] **Step 5: Commit** `feat(web-ui): scaffold, login, board with inbox`.

### Task 7: Frontend — màn B (trang review)

**Files:**
- Create: `aisdlc-web/ui/src/pages/Review.tsx`, `aisdlc-web/ui/src/components/{GateList.tsx,Markdown.tsx,Mermaid.tsx,CasesTable.tsx,EvalTable.tsx,DiffView.tsx,AgentReview.tsx,GatePanel.tsx}`
- Create: `aisdlc-web/ui/src/markdown.ts` (bộ render markdown tối giản tự viết, escape HTML trước rồi xử lý heading, list, checkbox, code fence, bảng pipe, `**`, `` ` `` và link) và `aisdlc-web/ui/src/diff.ts`
- Test: `aisdlc-web/ui/src/{markdown.test.ts,diff.test.ts}`

**Interfaces:**
- Consumes: `getFeature`, `getEvents`, `subscribe` (Task 6) và hợp đồng snapshot v1.
- Produces:
  - `markdown.ts`: `renderMarkdown(src): string`. Kết quả là HTML an toàn, `<script>` trong nguồn phải ra dạng text. Khối ```` ```mermaid ```` thành `<div data-mermaid>` chứa mã đã escape.
  - `diff.ts`: `parseDiff(text): DiffFile[]`, với `DiffFile {path, added, deleted, hunks: {header, lines: {kind: "+"|"-"|" ", text}[]}[]}`.
  - `diff.ts`: `groupByStep(steps): {stepId, title, files: DiffFile[], outOfScope: Set<string>, truncated}[]`.

- [ ] **Step 1: Viết test thất bại.**
  - `markdown.test.ts`:
    - `# A` cho ra `<h1>A</h1>`.
    - `- [ ] Câu hỏi` cho ra một checkbox disabled, chưa tick.
    - Bảng pipe ra `<table>`.
    - `<script>alert(1)</script>` ra `&lt;script&gt;`.
    - Khối mermaid ra `data-mermaid`.
  - `diff.test.ts`:
    - Parse một diff 2 file thì được đúng số dòng `+` và `-`.
    - File mới (`--- /dev/null`) được nhận đúng.
    - `groupByStep` đánh dấu `README2.txt` ngoài phạm vi khi nó nằm trong `record.check.outOfScope`.
- [ ] **Step 2: Chạy `npx vitest --run`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt bố cục ba cột.**
  - Cột trái `GateList`: các cổng theo luồng và các bước Sx, mỗi mục có chấm trạng thái.
  - Cột giữa, theo mục đang chọn:
    - BRD: `Markdown` của `brd.md`.
    - DESIGN: `Markdown` của `design.md`, trong đó Mermaid được render bằng `mermaid.render` với theme theo sáng/tối. Kèm bảng bước (phạm vi, ngân sách), các file `db/*` dạng code và `design.yaml`.
    - CASES: `CasesTable`, bấm vào một dòng thì mở `raw`.
    - REVIEW: `EvalTable` (cứng/mềm, giống bảng ở CLI), `DiffView` nhóm theo bước (file ngoài phạm vi có viền đỏ, có nhãn "đã cắt" khi `truncated`), `AgentReview` (nhãn ĐẠT, CẦN_SỬA, NGHIÊM_TRỌNG kèm lý do).
  - Cột phải `GatePanel`:
    - trạng thái;
    - hash đã nộp và hash đã duyệt (8 ký tự đầu);
    - người duyệt và thời điểm;
    - phát hiện;
    - "Đã sửa sau duyệt" khi hash hiện tại khác hash đã duyệt;
    - `lastFeedback`;
    - `CopyCommand` của lệnh tương ứng;
    - 20 event gần nhất của tính năng.
  - Nhận SSE `snapshot` thì tải lại. Nhận `event` thì chèn lên đầu danh sách.
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-web verify`**. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(web-ui): feature review page`.

### Task 8: Frontend — màn C (theo dõi agent)

**Files:**
- Create: `aisdlc-web/ui/src/pages/Live.tsx`, `aisdlc-web/ui/src/components/{StepProgress.tsx,EditTimeline.tsx}`, `aisdlc-web/ui/src/live.ts`
- Test: `aisdlc-web/ui/src/live.test.ts`

**Interfaces:**
- Consumes: `getFeature`, `getEvents`, `subscribe`, `budgetLevel`.
- Produces: `live.ts` có:
  - `sessionState(events, now): "ACTIVE"|"STOPPED"|"NONE"`, cùng quy tắc với server;
  - `editsByStep(events): Map<string, {path, at, outOfScope}[]>`, mỗi path chỉ giữ lần sửa mới nhất.

- [ ] **Step 1: Viết test thất bại.**
  - `live.test.ts`:
    - Có `SESSION_START` cách đây 10 phút và chưa có Stop → `ACTIVE`.
    - `FILE_EDIT` cuối cùng cách đây 31 phút → `STOPPED`.
    - Không có event → `NONE`.
    - Sửa cùng một path hai lần thì `editsByStep` giữ lần mới nhất.
- [ ] **Step 2: Chạy `npx vitest --run`**. Kỳ vọng: FAIL.
- [ ] **Step 3: Cài đặt.**
  - Đầu trang: Jira key, tiêu đề, trạng thái phiên ("Agent đang chạy" / "Đã dừng" / "Chưa có phiên").
  - Mỗi bước có trạng thái và hai thanh tiến độ, file và dòng, so với ngân sách. Màu theo `budgetLevel`: xanh, vàng hoặc đỏ.
  - Kết quả case của lần finish gần nhất, dạng "6/6 pass" kèm danh sách case không pass.
  - Dòng thời gian trực tiếp các event `FILE_EDIT`: giờ, bước, path. Path ngoài phạm vi tô đỏ kèm chữ "ngoài phạm vi".
  - Event mới qua SSE được chèn ngay, không cần tải lại.
  - Số dòng và số file chỉ cập nhật khi có snapshot mới. Ghi chú nhỏ: "Số dòng cập nhật sau mỗi lệnh step".
- [ ] **Step 4: Chạy `mvn -q -pl aisdlc-web verify`**. Kỳ vọng: PASS.
- [ ] **Step 5: Commit** `feat(web-ui): live agent monitor`.

### Task 9: Đóng gói, E2E và tài liệu

**Files:**
- Create: `aisdlc-web/Dockerfile`. Image chạy dựa trên `eclipse-temurin:17-jre`, chép `aisdlc-web.jar`, `ENTRYPOINT ["java","-jar","/app/aisdlc-web.jar"]`, `EXPOSE 8080`.
- Create: `aisdlc-web/docker-compose.yml`. Gồm service `db` (postgres:16-alpine, volume `aisdlc-db`, healthcheck) và service `web` (build `.`, cổng `${AISDLC_WEB_PORT:-8080}:8080`, env DB, `depends_on` theo healthcheck).
- Modify: `e2e/run-e2e.sh`. Biến `WEB=1` mặc định bật, còn `WEB=0` thì bỏ phần web.
- Create: `e2e/web-check.sh` (các hàm `curl` cùng `jq` hoặc `python3 -c` để kiểm JSON)
- Create: `e2e/screenshots.mjs`. Dùng Playwright 1.49.1, cài vào thư mục tạm bằng `npm i playwright@1.49.1`, chụp `/`, `/f/<repo>/PAY-1` và `/f/<repo>/PAY-1/live` ra `$WORK/screens/*.png`.
- Modify: `docs/README.vi.md`. Thêm mục "Web theo dõi (2a)": chạy compose, `user add`, cấu hình `~/.config/aisdlc/web.json`, `aisdlc web status|push`, và ghi rõ là web chỉ xem, duyệt vẫn bằng terminal.

**Interfaces:**
- Consumes: mọi task trước.
- Produces: E2E in `E2E PASS` với phần web bật.

- [ ] **Step 1: Mở rộng E2E.**
  - Đầu script:
    - Chạy `docker compose -p aisdlc-e2e -f aisdlc-web/docker-compose.yml up -d --build` với `AISDLC_WEB_PORT` là một cổng trống.
    - Chờ `/api/login` trả 400 hoặc 401, tối đa 90 giây.
    - Chạy `docker compose exec web java -jar /app/aisdlc-web.jar user add dev "Dev E2E" --email dev@example.com` để lấy token.
    - Export `AISDLC_WEB_URL` và `AISDLC_WEB_TOKEN`.
  - Cleanup: `docker compose down -v`.
  - Thêm các khẳng định:
    1. Sau `PAY-1 init` và `submit brd`: `/api/board` có thẻ PAY-1 ở cột `BRD`, có nhãn `AWAITING_APPROVAL:BRD`, inbox có `aisdlc approve brd`.
    2. Sau khi hook post-tool-use ghi `README2.txt` trong S2: `/api/features/.../PAY-1/events` có `FILE_EDIT` với `outOfScope: true`.
    3. Trước bước "PAY-1 eval":
       - Chạy `docker compose stop web`, rồi chạy `aisdlc eval` và `submit review` như cũ, cả hai phải pass.
       - `aisdlc web status` báo `Đang chờ gửi` lớn hơn 0.
       - Chạy `docker compose start web`, chờ sẵn sàng, rồi `aisdlc web push`. `Đang chờ gửi` về 0.
       - Thẻ có `eval.passed` là true.
    4. Sau `approve review`: cột `DONE`.
    5. FIX-2 (LITE): thẻ bắt đầu ở cột `CASES`.
  - Nếu có `SCREENSHOTS=1` thì chạy `node e2e/screenshots.mjs` và in đường dẫn ảnh.
- [ ] **Step 2: Chạy `KEEP=1 SCREENSHOTS=1 bash e2e/run-e2e.sh`**. Kỳ vọng: `E2E PASS` và ba ảnh PNG. Mở ảnh xem để kiểm bố cục không trống hay vỡ.
- [ ] **Step 3: Chạy `WEB=0 bash e2e/run-e2e.sh`**. Kỳ vọng: `E2E PASS`, tức đường giai đoạn 1 không đổi.
- [ ] **Step 4: Chạy `mvn -q verify && bash plugin/test/plugin-files-test.sh`**. Kỳ vọng: PASS, rc=0.
- [ ] **Step 5: Commit** `feat(web): docker compose, e2e with web, docs`.
