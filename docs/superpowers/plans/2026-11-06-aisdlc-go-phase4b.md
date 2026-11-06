# AISDLC Go — Giai đoạn 4b: Web client trong CLI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CLI Go nói chuyện với web server theo đúng giao thức của bản Java. Cụ thể:
- outbox sự kiện và snapshot, flush nền, backoff;
- `web login|status|push|flush|approve-token|migrate`;
- chế độ web cho nộp và duyệt cổng;
- điều khiển từ web (pause, instructions, actions);
- `policy pull`;
- nguồn approval web cho `eval --ci`.

Hết giai đoạn này, 139 test Java đang hoãn có bản Go.

**Architecture:**
- Logic không phụ thuộc lệnh nằm trong package mới `internal/webclient`: HTTP client, khóa đã ghim, outbox, khóa flush, backoff, publisher, dựng snapshot, feedback log, sổ điều khiển.
- `internal/cli` gọi `webclient` từ các lệnh và hook. Phần đọc cache đã có sẵn (`webcache.go`), nay được nối thêm làm mới qua HTTP.
- Test dùng server giả `internal/webclient/fakeweb` (port `FakeWeb.java`). Task cuối chạy CLI trên server Go thật của giai đoạn 4a.

**Tech Stack:** Go 1.26, `net/http`, `crypto/ed25519`. Không thêm thư viện.

**Spec:** `~/Documents/git/aisdlc/docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md` §7 (web client giữ nguyên giao thức: Outbox, BackgroundFlush, FlushLock, Backoff, PinnedKey, PolicySync, ControlLedger; phong bì `payload`/`sig` base64; PEM PKCS#8 + X.509) và §2 (fail closed, không chạm `~/.config/aisdlc` trong test).

**Nguồn Java:**
- `~/Documents/git/aisdlc/aisdlc-cli/src/main/java/com/aisdlc/cli/web/*.java`: WebClient, WebConfig, PinnedKey, Outbox, FlushLock, Backoff, BackgroundFlush, WebPublisher, WebGates, WebControl, ControlLedger, PolicySync, HttpCiApprovalSource, Event;
- `aisdlc-core/src/main/java/com/aisdlc/core/web/{SnapshotBuilder,FeedbackLog}.java`;
- các lệnh trong `aisdlc-cli/.../commands/` (WebCommand, HookCommand, EvalCommand, PolicyCommand);
- test trong `aisdlc-cli/src/test/java/**` và `aisdlc-core/src/test/java/**`, cùng `FakeWeb.java`.

Danh sách test cần port là mục "Giai đoạn 4" của `docs/superpowers/deferred-tests.md` trong repo Go.

## Global Constraints

- Giao thức giữ nguyên như Java: đường dẫn, JSON, header `Authorization: Bearer`, phong bì ký, định dạng `web.json` và `web-pubkey`, thư mục `.aisdlc/web/**` cùng tên file outbox, cache, sổ.
- **Hook không bao giờ chặn vì web.** Web chết thì hook vẫn nhanh. Lỗi publisher chỉ ra stderr, không bao giờ ra stdout. Mã thoát của lệnh không đổi vì web.
- **Fail closed** khi xác thực và chữ ký: phong bì sai chữ ký thì giữ cache cũ; khóa đổi mà không được khóa cũ ký xác nhận thì từ chối. `eval --ci` ở chế độ web mà web không tới được thì hard fail.
- Token không bao giờ xuất hiện trong output, log hay thông báo lỗi. File `web.json`, `web-pubkey` và token approve có quyền 0600.
- Test không chạm `~/.config/aisdlc` thật: dùng `HOME` tạm, như các test CLI hiện có.
- `aisdlc web flush` (dùng trong hook) im lặng và luôn thoát 0.
- Thông báo tiếng Việt giữ nguyên văn Java. Ghi file bằng `fsx.WriteAtomic`. Thời gian UTC, tiêm qua `now`.
- Độ trễ hook p95 dưới 50 ms vẫn đúng khi web bật nhưng chết, và khi đang backoff.
- Commit có trailer:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Web treo, không trả lời.** Hook và lệnh thường vẫn kết thúc đúng hạn: flush inline có deadline tổng, flush nền tách hẳn khỏi tiến trình. Test: Task 2 `TestInlineFlushStopsAtOverallDeadlineOnSlowServer`, Task 3 `TestPostToolUseFastWhenWebHangs`.
2. **Nhiều tiến trình CLI và hook flush cùng lúc.** Không gửi trùng, không mất bản tin, không hỏng file outbox. Test: Task 2 `TestConcurrentPublishAndFlushNoLossNoDup`, với nhiều goroutine và cả tiến trình con.
3. **Server đổi khóa ký.** Khóa mới có khóa cũ ký xác nhận thì ghim lại và thử tiếp; không có xác nhận thì từ chối và giữ cache cũ. Test: Task 4, các port của `refreshRepinsKeyEndorsedByPinnedKeyAndRetries` và `refreshRefusesKeyNotEndorsedByPinnedKey`.
4. **Token lọt ra ngoài.** Mọi đường lỗi (401, 403, 500, mạng) không in token. Test: Task 6 `TestCiWebModeUnreachableNeverPrintsToken`, Task 4 `TestNoTokenInAnyErrorOutput`. Test thứ hai quét stdout và stderr của mọi lệnh web khi server giả trả lỗi.
5. **Outbox đầy hoặc file hỏng.** Giữ tối đa 2000 sự kiện và bỏ cũ nhất. File hỏng chuyển sang rejected, không làm vỡ hook. Test: Task 2 `TestCorruptOutboxFileIsQuarantined`.

---

## File structure

```
internal/webclient/client.go      Client{BaseURL, Token, HTTP}: Get/Post JSON, lỗi tiếng Việt (401/403/kind), không lộ token
internal/webclient/config.go      đọc/ghi web.json (0600), token env cảnh báo, approve token
internal/webclient/pinned.go      PinnedKey: ghim lần đầu 0600, đổi khóa cần endorsement
internal/webclient/outbox.go      Outbox (snapshot thay thế, sự kiện ≤2000, rejected, liệt kê theo thứ tự)
internal/webclient/flushlock.go   FlushLock (file lock liên tiến trình, có ngân sách thời gian)
internal/webclient/backoff.go     Backoff (file trạng thái, hết hạn)
internal/webclient/publisher.go   Publisher (batch v2, sessionId, gửi ≤20, deadline tổng, flush nền)
internal/webclient/snapshot.go    BuildSnapshot (SnapshotBuilder), FeedbackLog
internal/webclient/ledger.go      ControlLedger (claim một lần, khóa có ngân sách, kết quả)
internal/webclient/fakeweb/       server giả cho test (port FakeWeb.java)
internal/cli/cmd_web.go           web status|push|flush|login|approve-token|migrate
internal/cli/webgates.go          chế độ web cho submit/approve/reject/reopen/status (WebGates)
internal/cli/webcontrol.go        làm mới điều khiển, áp lệnh chờ (WebControl)
internal/cli/policysync.go        policy pull (PolicySync)
internal/cli/ciapprovals.go       eval.CIApprovalSource qua HTTP (HttpCiApprovalSource)
```

---

### Task 1: HTTP client, cấu hình web, khóa ghim, định danh repo, fakeweb

**Port:** `WebClient`, `WebConfig`, `PinnedKey`, `FakeWeb`; `RepoIdentityTest` (1); phần ghim khóa của `WebGatesTest` (`pinFirstTimeWith600`, `otherKeyNeedsEndorsementByPinnedKey`).

**Interfaces — Produces:**
```go
package webclient
type Client struct{ BaseURL, Token string; HTTP *http.Client; Now func() time.Time }
type HTTPError struct{ Status int; Reason string } // Error() tiếng Việt như Java, không chứa token
func (c *Client) GetJSON(ctx context.Context, path string, out any) error
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error
type Config struct{ URL, Token, ApproveToken string }
func LoadConfig(home string, env func(string) string) (Config, bool, []string /*cảnh báo*/)
func SaveConfig(home string, c Config) error // 0600
func Pin(home string, key ed25519.PublicKey) error
func AcceptKey(home string, offered ed25519.PublicKey, endorsementB64 string) (pinned bool, err error)
package fakeweb
type Server struct{ /* httptest; ghi lại request; cấu hình phản hồi; ký bằng khóa test */ }
func New(t *testing.T) *Server
```
- [ ] **Step 1: Viết test.** Port các test nêu trên. Thêm:
  - `TestHTTPErrorNeverContainsToken`;
  - `TestLoadConfigWebJsonBeatsEnv`, port `webJsonBeatsEnvToken` cùng hai test cảnh báo env token của `WebLoginTest`.
- [ ] **Step 2:** Chạy `go test ./internal/webclient/...`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.** Dùng chung `internal/cli/webcache.go` (`LoadWebConfig`, `LoadPinnedKey`): chuyển phần đọc sang `webclient`, `webcache` gọi lại. Timeout HTTP mặc định như Java.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(webclient): HTTP client, cấu hình web, ghim khóa, server giả`.

### Task 2: Outbox, FlushLock, Backoff, Publisher, snapshot

**Port:**
- `Outbox`, `FlushLock`, `Backoff`, `BackgroundFlush`, `WebPublisher`, `Event`;
- core `SnapshotBuilder`, `FeedbackLog`.

**Test Java:** `OutboxTest` (4), `WebPublisherTest` (16), `SnapshotBuilderTest` (12), `FeedbackLogTest` (3).

**Interfaces — Produces:**
```go
type Outbox struct{ Dir string }
func (o Outbox) AddEvent(e Event) error; func (o Outbox) PutSnapshot(key string, b []byte) error
type Publisher struct{ Root string; Client func() (*Client, bool); Outbox Outbox; Now func() time.Time; Spawn func() error /* tự gọi `aisdlc web flush` nền */ }
func (p *Publisher) Emit(e Event)                                   // vào outbox, rồi spawn flush nền nếu không backoff và không đang flush
func (p *Publisher) FlushInline(ctx context.Context, deadline time.Duration) // gửi ≤20/lô
func (p *Publisher) Drain(ctx context.Context) error                // web push/flush
func BuildSnapshot(f SnapshotInput) ([]byte, error)                 // giới hạn kích thước như Java
```
- [ ] **Step 1: Viết test.** Port 35 test. Thêm:
  - `TestConcurrentPublishAndFlushNoLossNoDup`;
  - `TestCorruptOutboxFileIsQuarantined`;
  - `TestSpawnedFlushDetached`: tiến trình con không giữ stdout và stderr của hook (`Setsid`, fd về `/dev/null`).

  Kiểm thêm snapshot do `BuildSnapshot` tạo qua được validator của server Go: gọi `ingest.Validate` nếu export được, hoặc POST tới `webtest`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(webclient): outbox, flush nền, backoff, publisher, snapshot`.

### Task 3: Nối publisher vào lệnh và hook

**Port:**
- `HookCommandTest` (5, phần post-tool-use);
- `webPublisher.afterCommand` của các lệnh;
- `aisdlc web flush`;
- event `session-start`/`stop` còn hoãn: `sessionStartStillOutputsContextAndEmitsEvent`, `stopBlockedEmitsNoSessionStopButAllowedDoes`, `postToolUseOutsideAisdlcRepoSilent`;
- `user-prompt-submit` (phần event);
- `RiskCommandTest.lowerWithReason_writesOverrideAndEvent` (phần event).

**Interfaces — Consumes:** Task 2 `Publisher`. **Produces:** `Context.Publisher(root) *webclient.Publisher`, được dùng sau mỗi lệnh có thay đổi trạng thái.
- [ ] **Step 1: Viết test.** Port các test nêu trên. Thêm `TestPostToolUseFastWhenWebHangs`: fakeweb treo, hook trả về dưới 200 ms. Chạy lại `TestHookLatencyP95` khi web bật nhưng chết.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Lệnh `web flush` luôn thoát 0 và im lặng.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(cli): hook và lệnh gửi sự kiện web qua outbox`.

### Task 4: Chế độ web cho cổng, `web login|status|approve-token|migrate|push`

**Port:**
- `WebGates`: refresh, phong bì, feedback, nộp, duyệt, từ chối, migrate, chế độ, endorsement;
- `WebCommand`.

**Test Java:**
- `WebGatesTest`: 27, trừ phần ghim đã làm ở Task 1, cộng phần còn lại của 4 test webcache đã port một phần;
- `WebLoginTest`: 8;
- `CliFlowTest`: 1;
- `FlowLocalTest`: 8 test chế độ web;
- `PolicyCommandTest` (2), dùng `PolicySync` cho `policy pull`.

**Interfaces — Produces:**
```go
func refreshGates(ctx *Context, f *Feature) (bool, error) // phong bì → cache, feedback.md
func (w *WebGates) Submit(...); Approve(...); Reject(...); Migrate(...)
```
`gate.GateStore` của chế độ web dùng cache đã xác minh, như hiện có ở `WebCache.Store`.
- [ ] **Step 1: Viết test.** Port 46 test. Thêm `TestNoTokenInAnyErrorOutput`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Đổi các chỗ `WebCmdPhase4Msg` thành lệnh thật.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(cli): chế độ web cho cổng, web login/status/push/migrate, policy pull`.

### Task 5: Điều khiển từ web

**Port:** `WebControl`, `ControlLedger`, và áp lệnh chờ (pending actions) trong hook và status.

**Test Java:** `ControlLedgerTest` (4), `WebControlTest` (5), `HookControlTest` (11), `CliControlTest` (4), `StatusControlTest` (1).

- [ ] **Step 1: Viết test.** Port 25 test. Test khóa liên tiến trình dùng tiến trình con thật (`budgetedLockGivesUpWhenAnotherProcessHoldsIt`).
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Hướng dẫn chỉ hiện một lần mỗi phiên: sổ ghi theo session, sửa carry-over "instructions re-shown every session" của giai đoạn 2b.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(cli): điều khiển từ web, sổ điều khiển, lệnh chờ`.

### Task 6: Approval web cho `eval --ci`

**Port:** `HttpCiApprovalSource`, cài `eval.CIApprovalSource`.

**Test Java:** `HttpCiApprovalSourceTest` (5), `EvalCommandTest` (4 test web), `EvalServiceTest` (15 test web), `CiAutoApprovalTest` (3 test web).

Thêm carry-over của giai đoạn 3: trong `eval.Service.Run`, kiểm `ctx.Err()` ngay trước khi ghi báo cáo. Một lệnh `checks` bị hủy được ghi là bị hủy, không phải "quá thời gian". Test cho việc này: `TestEvalCanceledDuringChecksWritesNoReport`.

- [ ] **Step 1: Viết test.** Port 27 test. Thêm `TestCiWebModeUnreachableNeverPrintsToken`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. `cmd_eval` lấy `AISDLC_WEB_URL` và `AISDLC_CI_TOKEN`; có đủ thì tạo nguồn HTTP, thiếu thì để nil, `eval.Service` đã fail closed sẵn.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(cli): eval --ci lấy approval từ web`.

### Task 7: Chạy CLI trên server Go thật

Task này phụ thuộc giai đoạn 4a đã merge.

**Files:** `internal/cli/webe2e_test.go`.
- [ ] **Step 1: Viết test** `TestCliAgainstGoServer`, cần Docker. Các bước:
  1. Dựng `internal/web` qua `webtest`; tạo user lead và token ingest/approve bằng `auth.RunAdmin`.
  2. Chạy `web login`, đưa token qua stdin tương tác giả lập như test Java.
  3. Chạy `init`, `submit brd`, rồi duyệt qua `POST /api/gates/approve` bằng phiên lead (CSRF).
  4. CLI refresh và thấy cổng đã duyệt.
  5. Chạy `web push`; `GET /api/board` có thẻ tính năng.
  6. Web pause tính năng; hook `pre-tool-use` bị chặn với thông báo pause.
  7. Chạy `eval --ci` với `AISDLC_CI_TOKEN`: lấy được approval.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL nếu có chỗ lệch giao thức. Sửa chỗ lệch ở phía nào thì ghi rõ trong report.
- [ ] **Step 3:** Cập nhật `docs/superpowers/deferred-tests.md`: mục "Giai đoạn 4" ghi đã port, kèm tên test Go.
- [ ] **Step 4:** Chạy `go test ./...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `test: CLI chạy trên web server Go thật; tài liệu test đã port`.
