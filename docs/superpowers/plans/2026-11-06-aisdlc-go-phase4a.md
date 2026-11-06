# AISDLC Go — Giai đoạn 4a: Web server

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `aisdlc-web` (Spring) sang `cmd/aisdlc-web` + `internal/web` (chi + pgx + goose), giữ nguyên API JSON, SSE, cookie và CSRF để UI React chạy không sửa. UI được nhúng bằng `go:embed`.

**Architecture:**
- Mỗi package Java thành một package con trong `internal/web`: `auth`, `ingest`, `board`, `gates`, `review`, `control`, `risk`, `spa`.
- Các package con dùng chung `internal/web/store` (pool pgx + migration goose nhúng) và `internal/web/httpx` (JSON, lỗi, giới hạn body, principal trong ctx).
- `internal/web/server.go` ráp router chi và middleware xác thực.
- Test tích hợp chạy trên Postgres thật, qua `internal/web/webtest`: container dùng chung, mỗi test một database mới, và một `httptest.Server`.

**Tech Stack:** Go 1.26, chi/v5 v5.5.0, pgx/v5 v5.12.0, goose/v3 v3.29.0 (embed), testcontainers-go v0.44.0 (chỉ trong test, qua `harness.StartPostgres`), React/Vite và vitest giữ nguyên.

**Spec:** `~/Documents/git/aisdlc/docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md` §8 (web), §2 (phản mục tiêu: CSRF, Ed25519, không đổi API JSON), §9 (test bằng Postgres thật).

**Nguồn Java:** `~/Documents/git/aisdlc/aisdlc-web`. Mã ở `src/main/java/com/aisdlc/web/**`, migration ở `src/main/resources/db/migration/V1..V6`, UI ở `ui/`, test ở `src/test/java/**` (53 unit + 102 IT). Mỗi task ghi lớp Java và test Java cần port.

## Global Constraints

- **API giữ nguyên cho UI.** Đường dẫn, method, tên trường JSON, mã HTTP, body lỗi, sự kiện SSE, cookie `AISDLC_SESSION` và header `X-CSRF-Token` đều như Java.
  - Lỗi xác thực trả đúng `{"error":"unauthorized"}` (401) và `{"error":"forbidden"}` (403). Lỗi nghiệp vụ dùng định dạng của `ApiError.java`.
  - Thời điểm ghi ra JSON theo ISO-8601 UTC như Jackson của Spring Boot, ví dụ `"2026-11-05T10:00:00Z"`, có phần lẻ giây khi khác 0. Dùng `time.RFC3339Nano` trên giá trị đã `.UTC()`.
- Token chỉ lưu SHA-256. CSRF so bằng `subtle.ConstantTimeCompare`. Có audit log. Vai trò giữ như Java (`lead`...).
- Khóa ký Ed25519 là file PEM (PKCS#8 `PRIVATE KEY` + X.509 `PUBLIC KEY`). Chưa có thì tự sinh với quyền 0600. Phong bì ký dùng `internal/snapshot.Sign` / `SignedEnvelope` đã có, để CLI xác minh được.
- Cấu hình qua env:
  - `AISDLC_DB_URL`: URL pgx; mặc định `postgres://localhost:5432/aisdlc`.
  - `AISDLC_DB_USER`, `AISDLC_DB_PASSWORD`: tùy chọn, ghi đè phần trong URL.
  - `AISDLC_SIGNING_KEY_FILE`: mặc định `/data/signing-key`.
  - `AISDLC_SIGNING_PREVIOUS_KEY_FILE`.
  - `AISDLC_LISTEN`: mặc định `:8080`.
- Body ingest tối đa 8 MB, quá thì 413 sau khi đã đọc bỏ phần thân (không reset kết nối). Form tối đa 16 MB.
- Gộp V1..V6 thành **một** migration goose `internal/web/store/migrations/00001_init.sql`. Schema cuối phải tương đương Java V6.
- Thông báo tiếng Việt giữ nguyên văn bản Java.
- Ghi file bằng `fsx.WriteAtomic`. Thời gian UTC, tiêm qua `now func() time.Time`.
- Test cần Docker theo luật giai đoạn 3: `harnesstest.RequireDocker`, chỉ skip khi `AISDLC_SKIP_DOCKER=1`.
- `internal/web/**` không import `internal/cli`. Logic nghiệp vụ dùng chung lấy từ `internal/gate`, `internal/risk`, `internal/snapshot`, `internal/model`.
- Commit có trailer:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Lb7JEauZCA581fDsGLCLSE
  ```

## Review Focus

1. **Đường dẫn lạ lách vùng xác thực.** Các dạng `/api/cli;x/...`, `/api/%63li/...`, `/api/./gates/...`, `/api/x/../control/...`, `//api/...` và `\api\...` phải bị xác thực theo đúng vùng sau khi chuẩn hóa, hoặc bị từ chối. Test: Task 2 `TestAuthAreaNormalization`, dạng bảng, gồm mọi ca của `AuthFilter.normalize` cùng ca chi tự `CleanPath`.
2. **Body ingest quá 8 MB, hoặc request không có token mà body lớn.** Trả 413 hoặc 401 sạch, client nhận được phản hồi chứ không bị reset kết nối. Test: Task 4 `TestIngestTooLargeReturns413`, `TestUnauthorizedLargeBodyGets401`.
3. **SSE khi client ngắt, nhiều client, hoặc server tắt.** Không rò goroutine, và `Shutdown` không treo. Test: Task 5 `TestSseClientDisconnectReleases`, `TestSseShutdownClosesStreams`.
4. **Hai lead duyệt cùng lúc một cổng, hoặc duyệt khi bản nộp vừa đổi.** Chỉ một bản thắng. Bản kia nhận lỗi của Java (409 hoặc thông báo cổng đã đổi), không ghi trùng. Test: Task 6 `TestConcurrentApproveOneWins`.
5. **File khóa ký hỏng, sai quyền, hoặc thư mục không ghi được.** Server từ chối khởi động với thông báo rõ, không tự sinh đè khóa cũ. Test: Task 3 `TestSigningKeyCorruptFailsStartup`.

---

## File structure

```
cmd/aisdlc-web/main.go                 serve (mặc định) | user add|list|disable|token ...
internal/web/server.go                 Config (env), New(cfg) http.Handler, Run(ctx)
internal/web/httpx/                    WriteJSON, ApiError, LimitBody, Principal ctx, Now
internal/web/store/                    Open(ctx,url) *pgxpool.Pool, Migrate, migrations/00001_init.sql (embed)
internal/web/webtest/                  Env{Pool, Server, Client}, New(t), Login(t, user) ...
internal/web/auth/                     users, tokens, sessions, audit, middleware, handlers, admin CLI
internal/web/signing/                  SigningKeys (PEM, endorsement)
internal/web/ingest/                   SnapshotStore, EventStore, validator, handlers
internal/web/board/                    BoardRules, FeatureReader, board/feature handlers, SseHub, stream
internal/web/gates/                    GateRepository, GateRequests, GateOverlay, ApprovalService, AutoApprover,
                                       ModeService, handlers (cli, actions, import, ci, repos)
internal/web/review/                   LineDiff, ResubmissionService
internal/web/control/                  ControlRepository, ControlService, handlers
internal/web/risk/                     PolicyRepository, PolicyService, handlers
internal/web/spa/                      embed web/ui/dist, SPA fallback
web/ui/                                chép từ aisdlc-web/ui (bỏ node_modules, dist)
Dockerfile.web                         multi-stage: node build UI → go build → distroless
```

Package con nhận dependency qua struct, ví dụ `auth.Service{Pool, Now}`, và đăng ký route qua `func Routes(r chi.Router, deps)`. Không dùng biến toàn cục.

---

### Task 1: Khung server, store, migration, webtest

**Files:** `cmd/aisdlc-web/main.go`, `internal/web/server.go`, `internal/web/httpx/*.go`, `internal/web/store/{store.go,migrations/00001_init.sql}`, `internal/web/webtest/webtest.go`, `go.mod` (chi). Test: `internal/web/store/store_test.go`, `internal/web/httpx/httpx_test.go`, `internal/web/server_test.go`.

**Interfaces — Produces:**
```go
package store
func Open(ctx context.Context, url, user, password string) (*pgxpool.Pool, error)
func Migrate(ctx context.Context, pool *pgxpool.Pool) error // goose, embed FS, Up
package httpx
type ApiError struct{ Status int; Code, Message string } // JSON như ApiError.java
func WriteJSON(w http.ResponseWriter, status int, v any)
func WriteError(w http.ResponseWriter, err error) // *ApiError → status+body; khác → 500 {"error":"internal"} + log
func DecodeJSON(r *http.Request, dst any) error // body sai → *ApiError 400 như Java BadRequest
func LimitBody(max int64) func(http.Handler) http.Handler // quá max → đọc bỏ phần còn lại rồi 413
func WithPrincipal(ctx context.Context, p Principal) context.Context; func PrincipalFrom(ctx) (Principal, bool)
type Principal struct{ UserID int64; Login, Role, Via string /* "session" | "login"|"ingest"|"ci"|"approve" */ }
func (p Principal) IsSession() bool
func Instant(t time.Time) string // UTC RFC3339Nano
package web
type Config struct{ Listen, DBURL, DBUser, DBPassword, KeyFile, PrevKeyFile string }
func ConfigFromEnv() Config
type Deps struct{ Pool *pgxpool.Pool; Now func() time.Time; /* thêm dần ở các task sau */ }
func NewHandler(d Deps) http.Handler // chi router; các task sau thêm Routes vào đây
func Run(ctx context.Context, cfg Config) error // Open, Migrate, keys, ListenAndServe, Shutdown khi ctx hủy (10s)
package webtest
type Env struct{ Pool *pgxpool.Pool; Server *httptest.Server; Now *time.Time }
func New(t *testing.T, build func(web.Deps) http.Handler) *Env // DB mới (CREATE DATABASE t_<rand>) trên container chung, Migrate
```
- [ ] **Step 1: Viết test.**
  - `TestMigrateCreatesFinalSchema`: sau `Migrate`, danh sách bảng và cột trong `information_schema` trùng danh sách viết tay lấy từ V1..V6 Java. Liệt kê đủ bảng: users, api_tokens, sessions, audit... (lấy tên thật từ SQL Java).
  - `TestMigrateIdempotent`.
  - `TestLimitBodyDrainsAnd413` với `httptest` và body 9 MB.
  - `TestApiErrorShape`: so JSON với `ApiError.java`.
  - `TestInstantFormat`: `2026-11-05T10:00:00Z`, và `...00.5Z` khi có phần lẻ giây.
  - `TestConfigFromEnvDefaults`.
- [ ] **Step 2:** Chạy `go test ./internal/web/...`. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.**
  - `cmd/aisdlc-web`: không đối số thì `Run`; `user ...` để Task 2.
  - `NewHandler` có middleware recover (500 + log), và với `/api/*` mà không route nào khớp thì trả 404 JSON.
- [ ] **Step 4:** Chạy `go test ./internal/web/...` và `make check`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): khung server Go, migration gộp, webtest`.

### Task 2: Xác thực, token, phiên, audit, quản trị user

**Port:** `auth/*.java` (AuthFilter, AuthController, TokenController, TokenService, SessionService, UserRepository, AuditLog, Principal, UserAdminCli). **Test Java:** `AuthIT` (7), `TokenIT` (4), `UserAdminCliTest` (5).

**Interfaces — Produces:**
```go
package auth
type Service struct{ Pool *pgxpool.Pool; Now func() time.Time }
func (s *Service) Middleware(next http.Handler) http.Handler // luật vùng như AuthFilter
func Routes(r chi.Router, s *Service)                         // /api/login, /api/me, /api/logout, /api/me/tokens...
func (s *Service) Audit(ctx context.Context, login, action, detail string) error
func Normalize(raw string) string // AuthFilter.normalize
func AreaOf(path string) Area     // LOGIN|INGEST|CLI|CI|GATES|CONTROL|OTHER
func RunAdmin(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer) int // UserAdminCli
```
- [ ] **Step 1: Viết test.**
  - Port 16 test Java theo hành vi.
  - `TestAuthAreaNormalization`, dạng bảng. Path thô qua chi phải đánh giá ra cùng vùng như Java: vùng theo `r.URL.Path` (chi đã giải mã) và vùng theo `Normalize(r.RequestURI)` phải trùng, nếu không thì 403.
  - `TestAuthRulesMatrix`, bảng vùng × loại principal × method → 200/401/403, đúng ma trận trong javadoc `AuthFilter`.
  - `TestCsrfConstantTime`: CSRF sai thì 403.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3: Cài đặt.**
  - Middleware bọc mọi route. `/api/login` và đường tĩnh "sạch" không thuộc `/api` thì bỏ qua, theo đúng `shouldNotFilter`.
  - Thêm lệnh `aisdlc-web user add|list|disable|token ...` vào `cmd/aisdlc-web`, với cú pháp và output như `UserAdminCli.java`.
- [ ] **Step 4:** Chạy `go test ./internal/web/... ./cmd/aisdlc-web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): xác thực, token, phiên, CSRF, audit, quản trị user`.

### Task 3: Khóa ký, chế độ repo, pubkey

**Port:** `gates/SigningKeys.java`, `gates/ModeService.java`, `gates/RepoModeController.java`, và phần `/api/cli/pubkey`, `/api/cli/mode` của `CliGateController.java`. **Test Java:** `SigningKeysTest` (4), cùng các test mode/pubkey trong `CliGateIT`.

**Interfaces — Produces:**
```go
package signing
type Keys struct{ Current ed25519.PrivateKey; Previous ed25519.PrivateKey /* nil nếu không */ }
func Load(file, prevFile string) (*Keys, error) // chưa có file → sinh, ghi 0600; hỏng → lỗi (không ghi đè)
func (k *Keys) Sign(payload []byte) snapshot.SignedEnvelope
func (k *Keys) PubkeyResponse() any // đúng JSON /api/cli/pubkey (kèm endorsement khi có khóa cũ)
package gates
type ModeService struct{ Pool *pgxpool.Pool; Keys *signing.Keys; Now func() time.Time }
func (m *ModeService) Mode(ctx context.Context, repoID string) (string, bool, error) // ghim chưa
```
- [ ] **Step 1: Viết test.**
  - Port các test Java.
  - `TestSigningKeyCorruptFailsStartup`.
  - `TestSigningKeyFilePerm0600`.
  - `TestEnvelopeVerifiesWithGoCLI`: envelope ký ở server phải qua được `snapshot.SignedEnvelope.Verify` bằng khóa đọc từ `/api/cli/pubkey` và `snapshot.PublicKeyFromBase64`.
  - `TestPemInteropWithJava`: đọc được file PEM mẫu do Java sinh. Lấy mẫu từ `src/test/resources`, hoặc sinh bằng openssl trong test nếu không có.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. `Run` (Task 1) gọi `signing.Load` trước khi listen.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): khóa ký Ed25519, chế độ repo, pubkey`.

### Task 4: Ingest

**Port:** `ingest/*.java` (IngestController, SnapshotStore, EventStore, SnapshotValidator, FeatureChanged, BadRequestException). **Test Java:** `IngestIT` (12).

**Interfaces — Produces:**
```go
package ingest
type Service struct{ Pool *pgxpool.Pool; Now func() time.Time; Changed func(repoID, feature string) }
func Routes(r chi.Router, s *Service) // POST /api/ingest/snapshot, /api/ingest/events (LimitBody 8 MB)
func (s *Service) LatestSnapshot(ctx context.Context, repoID, feature string) (json.RawMessage, time.Time, bool, error)
```
- [ ] **Step 1: Viết test.** Port 12 test, thêm `TestIngestTooLargeReturns413` và `TestUnauthorizedLargeBodyGets401`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Sau khi lưu thì gọi callback `Changed`, Task 5 nối callback này vào SSE.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): ingest snapshot và sự kiện`.

### Task 5: Board, feature, SSE

**Port:** `board/*.java` (BoardRules, BoardController, FeatureController, FeatureReader, SseHub, StreamController). **Test Java:** `BoardRulesTest` (16), `BoardIT` (9).

**Interfaces — Produces:**
```go
package board
type Hub struct{ /* clients */ }
func NewHub(reader *Reader) *Hub
func (h *Hub) Publish(repoID, feature string)       // nối từ ingest.Service.Changed và từ gates/control
func (h *Hub) Close()                               // đóng mọi stream, dùng khi Shutdown
func Routes(r chi.Router, reader *Reader, hub *Hub) // /api/board, /api/features/**, /api/stream
```
- [ ] **Step 1: Viết test.**
  - Port 25 test.
  - `TestBoardJSONMatchesUIFixture`: card của PAY-1 dựng từ snapshot mẫu khớp `web/ui/src/fixtures/pay1-cards.json` theo cấu trúc trường. Sinh snapshot vào fixture nếu cần.
  - `TestSseClientDisconnectReleases`: số client về 0 sau khi ngắt.
  - `TestSseShutdownClosesStreams`.
  - `TestSseEventFormat`: tên event và dòng `data:` như `SseHub.java`, cả nhịp keep-alive nếu Java có.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Mỗi lần ghi SSE thì `Flush`. Hai lần ghi liên tiếp mà lỗi thì gỡ client.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): board, feature, SSE`.

### Task 6: Cổng — kho, overlay, duyệt, nộp từ CLI

**Port:**
- `gates/GateRepository.java`, `GateRequests.java`, `GateOverlay.java`, `ApprovalService.java`, `GateActionController.java`, `ApiError.java`;
- phần `/api/cli/submissions` và `/api/cli/gates` (phong bì ký) của `CliGateController.java`;
- `review/ResubmissionService.java` và `review/LineDiff.java`, vì ApprovalService dùng chúng.

**Test Java:** `GateOverlayTest` (7), `ApprovalIT` (19), `SupersedeIT` (3), `CliGateIT` (5, trừ phần Task 3), `LineDiffTest` (8), `ResubmissionServiceTest` (10), `ResubmissionIT` (2).

**Interfaces — Produces:**
```go
package gates
type Service struct{ Pool *pgxpool.Pool; Keys *signing.Keys; Mode *ModeService; Hub interface{ Publish(string, string) }; Audit func(ctx context.Context, login, action, detail string) error; Now func() time.Time }
func Routes(r chi.Router, s *Service) // /api/cli/submissions, /api/cli/gates, /api/gates/{approve,reject,reopen,confirm-import}
func (s *Service) GateState(ctx context.Context, repoID, feature string) (snapshot.FeatureGateState, error) // dùng cho phong bì và CI
package review
func Diff(a, b string) []LineDiffOp // LineDiff
```
- [ ] **Step 1: Viết test.**
  - Port 54 test.
  - `TestConcurrentApproveOneWins`: 2 goroutine duyệt cùng phiên bản thì đúng một 200; bản kia nhận lỗi Java; bảng approvals chỉ có 1 dòng. Dùng transaction `SELECT ... FOR UPDATE` hoặc ràng buộc unique như Java.
  - `TestGatesEnvelopeVerifiesInCLI`: `snapshot.FeatureGateStateFromJSON` đọc được payload.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Mọi thay đổi gọi `Hub.Publish` và audit như Java.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): cổng duyệt, overlay, nộp lại, phong bì ký`.

### Task 7: Tự duyệt, import, CI

**Port:** `gates/AutoApprover.java`, `ImportController.java`, `CiController.java`. **Test Java:** `AutoApproveIT` (5), `ImportIT` (5), `CiIT` (2).

**Interfaces — Produces:**
```go
func ImportRoutes(r chi.Router, s *Service) // POST /api/cli/import
func CIRoutes(r chi.Router, s *Service)     // GET /api/ci/approvals — JSON mà eval.CIApprovals (Go) đọc
func (s *Service) AutoApprove(ctx context.Context, repoID, feature string, g gate.Gate) error // dùng risk.AutoGate
```
- [ ] **Step 1: Viết test.**
  - Port 12 test.
  - `TestCiApprovalsJSONShape`: khớp `CiApprovals.java`, nghĩa là các trường mà CLI Java `HttpCiApprovalSource` đọc. Giai đoạn 4b dùng lại hợp đồng này.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): tự duyệt, import, approval cho CI`.

### Task 8: Điều khiển (pause, instructions, actions)

**Port:** `control/*.java`. **Test Java:** `ControlIT` (12).

**Interfaces — Produces:**
```go
package control
type Service struct{ Pool *pgxpool.Pool; Keys *signing.Keys; Hub interface{ Publish(string, string) }; Now func() time.Time }
func Routes(r chi.Router, s *Service) // /api/control/pause|instructions|actions, GET /api/cli/control (phong bì ký → snapshot.FeatureControl)
```
- [ ] **Step 1: Viết test.** Port 12 test. Thêm `TestControlEnvelopeVerifiesInCLI`: `snapshot.FeatureControlFromJSON` đọc được payload và chữ ký hợp lệ.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): điều khiển tính năng từ web`.

### Task 9: Risk policy

**Port:** `risk/*.java` (PolicyController, PolicyRepository, PolicyService). **Test Java:** `PolicyServiceTest` (3), `PolicyControllerIT` (14).

**Interfaces — Consumes:** `internal/risk` (parse, validate và classify policy, đã có từ giai đoạn 2a). **Produces:** `func Routes(r chi.Router, s *Service)` cho `/api/policy`, `/api/policy/versions`, `/api/policy/{publish,validate,classify}`, `/api/cli/policy`, `/api/cli/policy/import`. Phong bì policy phải qua được `snapshot.VerifyPolicy`.

- [ ] **Step 1: Viết test.**
  - Port 17 test.
  - `TestPolicyValidateUsesGoRisk`: lỗi validate trùng thông báo `risk` của Go.
  - `TestPolicyEnvelopeVerifiesInCLI`.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt.
- [ ] **Step 4:** Chạy `go test ./internal/web/...`. Mong đợi PASS.
- [ ] **Step 5:** Commit `feat(web): risk policy`.

### Task 10: UI nhúng, SPA fallback, Dockerfile, chạy thật

**Files:**
- `web/ui/**`: chép từ `aisdlc-web/ui`, không lấy `node_modules` và `dist`.
- `internal/web/spa/spa.go`: `//go:embed all:dist`, với `dist` được Makefile chép vào `internal/web/spa/dist`.
- `internal/web/spa/dist/index.html`: placeholder có commit, để `go build` chạy được khi chưa build UI. Placeholder ghi rõ "UI chưa build: chạy make ui".
- `Makefile`: target `ui` (`cd web/ui && npm ci && npm run build && rm -rf ../../internal/web/spa/dist && cp -r dist ../../internal/web/spa/dist`), target `ui-test` (vitest), target `build-web`.
- `Dockerfile.web`.
- `.gitignore` cho bản build thật trong `internal/web/spa/dist`, trừ placeholder.

**Port:** `spa/SpaController.java`. **Test Java:** `SpaIT` (3).

- [ ] **Step 1: Viết test.**
  - Port 3 test: `/` và `/board/x` trả index.html; `/api/khong-co` trả 404 JSON, không trả SPA; asset có Content-Type đúng.
  - `TestServerEndToEnd`: `web.Run` trên cổng ngẫu nhiên với Postgres thật. Tạo user bằng `RunAdmin`, đăng nhập, `GET /api/me` trả 200. `Shutdown` khi hủy ctx.
- [ ] **Step 2:** Chạy test. Mong đợi FAIL.
- [ ] **Step 3:** Cài đặt. Chạy `make ui`; `npm run test -- --run` phải pass. UI chỉ được sửa chỗ gọi API, và có ghi lý do (spec §8).
- [ ] **Step 4:** Chạy `make ui && go test ./internal/web/... && make check`. Mong đợi PASS. Build `docker build -f Dockerfile.web .` phải thành công; nếu không có mạng tải image base thì ghi rõ trong report.
- [ ] **Step 5:** Commit `feat(web): nhúng UI React, SPA fallback, Dockerfile`.

---

## Ngoài phạm vi (giai đoạn 4b, 5)

- Web client trong CLI (Outbox, BackgroundFlush, FlushLock, Backoff, PinnedKey, PolicySync, ControlLedger, WebPublisher, WebGates, WebControl, HttpCiApprovalSource) và 139 test hoãn của CLI/core: giai đoạn 4b.
- Script Playwright `web-*.mjs` chạy trên server Go: giai đoạn 5.
