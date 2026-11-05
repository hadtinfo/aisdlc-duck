# AISDLC — Port toàn bộ sang Go

Nguồn đối chiếu: repo Java `~/Documents/git/aisdlc` (commit `f040fdf`), các spec trong `docs/superpowers/specs/` (2026-11-02 đến 2026-11-05).
Repo đích: `<repo root>` (hiện trống).

Quy ước: **[Người dùng]** là quyết định của người dùng; **[Claude]** là lựa chọn của Claude, người dùng có thể bác.

## 1. Vì sao port

- **[Người dùng]** Các dịch vụ mà team sẽ làm bằng AISDLC **chủ yếu viết bằng Go** và triển khai trên **EKS**. Về sau AISDLC phải **hỗ trợ được nhiều ngôn ngữ**.
- Bản Java gắn chặt với dịch vụ Java/Spring, nhất là `aisdlc-junit`. Harness này chạy *bên trong* JVM test của dịch vụ: lấy `DataSource`, `@LocalServerPort` và `ApplicationContext` từ Spring. Dịch vụ Go không có JVM để harness chạy vào.
- **[Người dùng]** Viết lại toàn bộ bằng Go: core, CLI, harness, web, plugin, e2e. Chọn Go thay vì Rust vì team là 4 dev Java, học Go nhanh hơn nhiều.
- Lợi ích phụ: hook chạy dưới 50 ms thay vì khoảng 0,3 s và 89 MB mỗi lần khởi động JVM. Launcher không cần các nhánh bash để né JVM nữa.

## 2. Mục tiêu, không phải mục tiêu, phản mục tiêu

**Mục tiêu**
1. **[Người dùng]** Bản Go có **đủ chức năng của bản Java hiện tại**:
   - giai đoạn 1 (CLI, cổng, step, hook, eval tầng 1),
   - web 2a và 2b,
   - risk flow và risk policy,
   - duyệt trong Claude Code, nhiều tính năng song song theo session.
2. Dịch vụ đích là **Go**. Harness **độc lập ngôn ngữ**: chỉ nói chuyện với dịch vụ qua HTTP, Postgres và Kafka.
3. Mọi điểm phụ thuộc ngôn ngữ của dịch vụ đích nằm sau một **adapter ngôn ngữ** (§5). Bản đầu chỉ có adapter Go. Adapter Java chỉ có interface và ghi chú, chưa code.
4. **Dịch vụ không phải import thư viện nào của AISDLC.** Hợp đồng chỉ gồm file cấu hình và biến môi trường.
5. Tiêu chí nghiệm thu: kịch bản e2e (bản Go của `e2e/run-e2e.sh`) pass trọn vẹn trên một dịch vụ mẫu viết bằng Go.

**Không phải mục tiêu**
- **[Người dùng]** Giai đoạn 3 (eval tầng 2, bộ bài chuẩn, đề xuất tự cải thiện, webhook). Làm sau, trên bản Go.
- Jira, GitLab, Confluence: đã bỏ từ 2026-11-04.
- **[Người dùng]** Tương thích ngược với dữ liệu của bản Java, vì chưa dự án nào dùng. Web dựng DB mới, không chuyển dữ liệu.
- Build Docker image hay deploy dịch vụ lên EKS. Việc này là của CI/CD hiện có.
- Code adapter Java (chỉ thiết kế interface).

**Phản mục tiêu** (chạy được nhưng vẫn coi là hỏng)
- Port "dịch từng dòng" mà làm mất các quy tắc an toàn đã có:
  - fail closed khi hook lỗi,
  - khóa `cases.yaml` sau khi duyệt,
  - phạm vi file của bước,
  - chữ ký Ed25519,
  - không chạm `~/.config/aisdlc`,
  - CSRF.

  Mọi quy tắc trong spec cũ vẫn áp dụng, trừ chỗ spec này ghi rõ là đổi.
- Dịch vụ Go phải viết code riêng cho AISDLC (test class, hook, thư viện) thì mới eval được.
- Logic Go-specific rải khắp core thay vì nằm trong adapter. Như vậy adapter Java sau này sẽ phải sửa lõi.
- Đổi API JSON của web làm UI React phải viết lại.

## 3. Dịch vụ Go mục tiêu và hợp đồng với AISDLC

**[Claude]** Stack khuyến nghị cho dịch vụ Go (cũng là stack của dịch vụ mẫu e2e):
- `net/http` + router `chi`,
- `pgx/v5`,
- `goose` cho migration (file SQL thuần),
- `franz-go` cho Kafka.

Harness **không bắt buộc** dịch vụ dùng đúng stack này. Nó chỉ cần dịch vụ tuân theo hợp đồng bên dưới.

**Hợp đồng chạy** (kiểu 12-factor, hợp với EKS):
- Dịch vụ là một binary, build từ một package `main`, ví dụ `./cmd/server`.
- Cấu hình hoàn toàn qua biến môi trường. Tên biến do repo khai báo trong `aisdlc/config.yaml` (§3.1).
- Có endpoint health check trả 2xx khi đã sẵn sàng.
- **Tắt êm khi nhận SIGTERM** (`main` return hoặc `os.Exit` bình thường). Thiếu điều này thì không ghi được coverage (`GOCOVERDIR`). Harness gửi SIGTERM, chờ `shutdownTimeoutSeconds`, quá giờ thì SIGKILL và đánh soft check coverage là N/A, kèm lý do.
- Dịch vụ **không bắt buộc** tự chạy migration: harness chạy migration trước khi khởi động dịch vụ (§6). Nếu dịch vụ có tự chạy goose lúc khởi động thì vẫn không sao, vì goose bỏ qua migration đã áp dụng.

### 3.1 Phần `service` trong `aisdlc/config.yaml` **[Claude]**

Thay cho `mavenCmd`, `casesTestClass` và `casesTestPath`:

```yaml
language: go                     # chọn adapter; bản đầu chỉ có "go"
timeoutMinutes: 30               # thay mavenTimeoutMinutes: build + test + case
service:
  main: ./cmd/server             # package main để build
  buildFlags: []                 # thêm cho go build, vd ["-tags=prod"]
  healthPath: /healthz
  startTimeoutSeconds: 60
  shutdownTimeoutSeconds: 15
  env:                           # tên biến env dịch vụ đọc; harness điền giá trị
    port: PORT                   # bắt buộc
    databaseUrl: DATABASE_URL    # bỏ trống nếu dịch vụ không dùng DB
    kafkaBrokers: KAFKA_BROKERS  # bỏ trống nếu không dùng Kafka
    externalBaseUrls: [CORE_BANKING_URL]  # các biến trỏ tới hệ thống ngoài → URL stub server
  extraEnv: { LOG_LEVEL: warn }  # giá trị cố định thêm vào
  postgres: { image: "postgres:16-alpine" }
  kafka: { image: "apache/kafka:3.8.0", topics: [] }  # khởi động khi env.kafkaBrokers có giá trị
migrations:
  dir: db/migrations             # migration chính
  tool: goose                    # bản đầu chỉ có goose
unitTest:
  packages: ["./..."]
  flags: []
```

`aisdlc setup` sinh phần này: dò `go.mod`, tìm `cmd/*/main.go` và thư mục migration, rồi hỏi lại dev những gì không dò được. `setup` không còn hỏi package Java và không sinh `AisdlcCasesTest.java`.

Các khóa khác của config giữ nguyên ý nghĩa: `alwaysAllowed`, `unmanaged`, `budgetWarnFactor`, `coverageWarn`, `checks`, `baseRef`, `sessionApprove`, `ciRequireFeature` và `review.*`. Hai giá trị mặc định đổi:
- `review.migrationDirs` lấy mặc định từ `migrations.dir`.
- `checks` gợi ý `go vet ./...`, `staticcheck ./...` và `govulncheck ./...` (chỉ là ghi chú trong template, không bật sẵn).

## 4. Cấu trúc repo Go **[Claude]**

Một Go module duy nhất, `module` đặt theo đường dẫn GitLab nội bộ (builder hỏi lại hoặc tạm dùng `aisdlc`). Go 1.23 trở lên.

```
cmd/aisdlc/            CLI + hook (thay aisdlc-cli)
cmd/aisdlc-web/        web server + lệnh quản trị user (thay aisdlc-web, UserAdminCli)
internal/model/        RepoConfig, FeatureKey, FeaturePaths, Flow, SessionBindings, ActiveFeature, Hashing, Yaml
internal/spec/         BRD/design/cases: kiểu dữ liệu + validator + MarkdownSections, OpenQuestions
internal/gate/         GateService, GateStore (local, web), Approval, Submission
internal/step/         StepService, ScopeMatcher, PendingActions, StepStore
internal/policy/       HookPolicy, BashHeuristics, DevGrants, ToolCall
internal/risk/         Classifier, RiskPolicy, RequirementCatalog/Checker, Presets, GlobOverlap
internal/review/       ReviewCardBuilder, ScopeFlags, SqlFacts, DocFacts, DependencyFlags (qua adapter)
internal/eval/         EvalService, HardCheck/SoftCheck, ChangedLines, report.json/report.md, CI approvals
internal/gitx/         gọi git CLI (giữ cách của GitRepo.java)
internal/lang/         interface Adapter (§5) + registry
internal/lang/golang/  adapter Go
internal/harness/      runner hộp đen (thay aisdlc-junit, §6)
internal/harness/stub/ stub HTTP server (thay WireMock)
internal/snapshot/     FeatureSnapshot, SignedEnvelope, ControlCache, PolicyCache, RepoIdentity
internal/cli/          lệnh cobra, web client (Outbox, BackgroundFlush, PinnedKey, PolicySync...)
internal/web/          server: auth, board, ingest, gates, control, risk, review, sse
internal/web/migrations/  goose, embed
web/ui/                UI React chép nguyên từ aisdlc-web/ui, build bằng Vite, nhúng bằng go:embed
templates/             template cho init/setup (embed)
plugin/                plugin Claude Code
e2e/                   kịch bản e2e + dịch vụ mẫu Go + lời giải
docs/                  tài liệu, spec/plan cũ chép sang docs/legacy-java/ để tham khảo
Makefile               build, test, ui, plugin-dist, e2e
```

Thư viện chọn:
- `cobra` cho CLI.
- `gopkg.in/yaml.v3`, **khoan dung với khóa lạ** như Jackson hiện tại (`FAIL_ON_UNKNOWN_PROPERTIES` đang tắt). Lỗi cú pháp và kiểu sai báo bằng tiếng Việt kèm tên file và số dòng.
- `chi`, `pgx/v5`, `goose` (thư viện, migration embed).
- `testcontainers-go` (module postgres và kafka), `franz-go`.
- Thư viện chuẩn cho `crypto/ed25519`, `crypto/sha256`, `encoding/json` và `log/slog`.

Không dùng ORM.

Thông báo cho dev vẫn bằng **tiếng Việt**, giữ nguyên câu chữ của bản Java khi không có lý do để đổi. Mã lỗi (`CASES_LOCKED` v.v.) giữ nguyên.

## 5. Adapter ngôn ngữ **[Claude]**

Mọi chỗ bản Java đang giả định Java/Maven chuyển vào adapter:

| Chỗ ở bản Java | Adapter Go |
|---|---|
| `MavenRunner`: `mvn verify` | `go build -cover`, `go test -json -cover -coverprofile` |
| `SurefireReports`: test cũ fail | đọc stream `go test -json` (Action `fail`, theo Package + Test) |
| `JacocoCoverage` + `ChangedLines` (`src/main/java/**.java`) | `go tool covdata textfmt` (coverage khi chạy case) hợp với coverprofile của unit test; file nguồn: `*.go`, không phải `*_test.go`, không trong `vendor/` hay `testdata/` |
| `DependencyFlags`: so `pom.xml` | so `go.mod` (require, replace) giữa base tree và working tree |
| `SqlFacts`: thứ tự migration theo version Flyway | thứ tự goose: tiền tố số (`00001_`, hoặc timestamp `20261105120000_`) |
| `ScopeFlags`: file mẫu `aisdlcx/AisdlcX.java` | adapter cho tên file mẫu (`aisdlcx/aisdlcx.go`) |
| `SubmitCommand`: thư mục migration Flyway | `migrations.dir` |
| `DraftMigrations` (bỏ nháp trùng version với migration chính) | dùng lại logic, theo quy tắc version của goose |

Interface (phác thảo, builder chốt chi tiết):

```go
type Adapter interface {
    Name() string
    IsSource(repoPath string) bool           // file nguồn "main", tính coverage và phạm vi
    Build(ctx context.Context, r Repo, outDir string) (Binary, error)
    UnitTests(ctx context.Context, r Repo, outDir string) (UnitResult, error) // exit, test fail, coverage
    Start(ctx context.Context, b Binary, env map[string]string, coverDir string) (Process, error)
    Coverage(outDir string) (map[string]map[int]bool, error) // repo path → dòng → đã chạy
    DependencyChanges(base, head TreeReader) ([]DependencyChange, error)
    MigrationOrder(names []string) []string
    SampleSourcePath() string
}
```

Adapter Java sau này: `mvn package` ra jar, chạy `java -javaagent:jacoco.jar -jar`, coverage từ `jacoco.xml`, unit test từ Surefire, Flyway. Spec này chỉ đảm bảo interface đủ chỗ cho các việc đó, không code adapter Java.

## 6. Harness hộp đen (thay `aisdlc-junit`)

Chạy trong `aisdlc eval` (và ở các lệnh khác đang gọi harness, như kiểm case của một bước). Luồng:

1. **Build**: `go build -cover -coverpkg=<module>/... -o <out>/svc <service.main>`. Lỗi build thì là hard check fail.
2. **Unit test**: `go test -json -cover -coverprofile=<out>/unit.cov <packages>`. Giữ logic hiện tại: test cũ bị fail thì là hard check fail; chỉ suite case fail thì không phải test cũ fail. Nay suite case nằm ngoài `go test`, nên hai việc này tách hẳn.
3. **Hạ tầng** (testcontainers-go):
   - Postgres: bật khi `env.databaseUrl` có giá trị.
   - **[Claude]** Kafka: bật khi `env.kafkaBrokers` có giá trị, kể cả khi không case nào kiểm message, vì dịch vụ có thể cần broker để khởi động. Bật sẵn auto-create topic. Ngoài ra `service.kafka.topics` (tùy chọn) liệt kê topic cần tạo trước khi khởi động dịch vụ.

   Mỗi lần eval dùng container mới, dừng khi xong, kể cả khi lỗi hoặc Ctrl-C.
4. **Migration**: **giữ ba chế độ của `HarnessOptions`** (tên giữ nguyên):
   - `cases` (dùng cho `step` và `eval`): chỉ chạy migration chính, **không** chạy nháp. Bước nào chưa chép migration vào `migrations.dir` thì case fail, giống CI và production.
   - `cases-with-drafts` (nộp cổng case): chạy migration chính, rồi nháp trong `aisdlc/features/<KEY>/db/`. Nháp đã có bản trùng version trong migration chính thì bỏ (logic `DraftMigrations`). Vẫn giữ quy tắc `CASES_NONE_FAILING`.
   - `migration-check` (nộp cổng thiết kế): **[Claude]** chỉ chạy goose (chính + nháp) trên Postgres rồi dump schema ra `schema-expected.json`. **Không build, không khởi động dịch vụ**: bản Java khởi động ứng dụng chỉ vì Flyway chạy trong Spring, lý do đó không còn.
5. **Stub server** (`internal/harness/stub`) chạy trong tiến trình harness, cổng ngẫu nhiên. URL của nó được điền vào mọi biến `env.externalBaseUrls`.
6. **Khởi động dịch vụ** với env đã điền và `GOCOVERDIR`, rồi chờ `healthPath` trả 2xx trong `startTimeoutSeconds`. Quá giờ hoặc dịch vụ thoát sớm: mọi case là `NOT_RUN` với thông điệp hiện có, kèm 50 dòng log cuối của dịch vụ trong report.
7. **Chạy case** theo đúng ngữ nghĩa của `CaseExecutor` hiện tại:
   - `reset_sql`, rồi `given.sql`, rồi đăng ký `given.stubs`,
   - `when` (kể cả `repeat` và `parallel`),
   - `then.status`, `statusCounts` và `body` (so JSON như `jsonassert` ở chế độ hiện dùng),
   - `then.db` qua pgx. **[Claude]** Không cần giữ chuỗi kiểu JDBC (`String.valueOf`), vì chưa có dự án nào dùng. Chuẩn hóa theo kiểu dữ liệu:
     - số (int, numeric, float) so theo **giá trị**, nên `100` khớp `100.00`;
     - bool là `true`/`false`;
     - `NULL` khớp `null` trong YAML;
     - timestamp, timestamptz, date so theo chuỗi RFC 3339 ở UTC (date là `YYYY-MM-DD`);
     - uuid, text và các kiểu còn lại so theo dạng text của Postgres.

     Skill `cases` và `step` ghi rõ quy tắc này,
   - `then.messages` qua probe Kafka (franz-go, subscribe các topic trước khi gọi request),
   - `checks`: bất biến trong `design.yaml`/`cases.yaml` và sql-zero của `risk.yaml`, theo `appliesTo`/`stepOrder`.

   Không có Kafka mà case cần message thì giữ hành vi của `NoMessageProbe`.
8. **Schema dump** qua `information_schema` của Postgres (bỏ bảng `goose_db_version`), kiểu cột theo tên chuẩn SQL như `SchemaDumper`.
9. **Tắt dịch vụ** (SIGTERM, §3), lấy coverage bằng `go tool covdata textfmt`, rồi gộp với `unit.cov`.
10. Ghi `results.json` và `schema.json`. **Định dạng giữ như bản Java**, chỉ bỏ trường gắn với JUnit nếu có.

**Stub server [Claude]:** giữ cú pháp `request`/`response` kiểu WireMock trong `cases.yaml`. Hỗ trợ một tập con có định nghĩa rõ:
- `request`: `method`, `url`, `urlPath`, `urlPathPattern`, `queryParameters` và `headers` (với `equalTo`, `matches`), `bodyPatterns` (với `equalToJson`, `contains`).
- `response`: `status`, `headers`, `body`, `jsonBody`, `fixedDelayMilliseconds`.

`CasesValidator` từ chối khóa ngoài tập này bằng thông báo rõ, thay vì lặng lẽ bỏ qua. Hiện e2e chỉ dùng `method`, `urlPath`, `status` và `jsonBody`.

**Dọn dẹp:** container được gắn nhãn `aisdlc.run=<id>`, và testcontainers-go Ryuk dọn khi tiến trình chết. Tiến trình dịch vụ chạy trong process group riêng và bị kill khi harness thoát.

## 7. CLI, hook và plugin

- Lệnh và cờ **giữ nguyên tên**: `init`, `use`, `status`, `submit`, `approve`, `reject`, `reopen`, `step`, `scope`, `diff`, `eval`, `review`, `risk`, `policy`, `web`, `setup`, `hook <event>`. Output giữ nguyên trừ chỗ nhắc Maven/Java. Mã thoát giữ nguyên, nhất là hook: 2 để chặn PreToolUse, fail closed.
- `plugin/bin/aisdlc` thành **wrapper ngắn**: chọn `plugin/lib/aisdlc-<os>-<arch>` theo `uname`; thiếu binary thì fail closed như hiện nay khi đang ở repo AISDLC. **Bỏ toàn bộ nhánh tối ưu bằng bash** (`pre-tool-use-read`, `post-tool-use`, `user-prompt-submit`, `clear_grant`). Logic tương đương chuyển vào Go, vì khởi động Go đủ nhanh. Mục tiêu: mỗi lần gọi hook dưới 50 ms (p95) khi không có web.
- `plugin/install.sh` build hoặc chép binary cho linux/amd64, linux/arm64, darwin/amd64 và darwin/arm64 (`make plugin-dist`). Không còn cần JDK.
- `plugin/commands/*.md` và `agents/aisdlc-reviewer.md`: sửa chỗ nhắc `mvn verify`, Java, JUnit; skill `step` hướng dẫn agent viết Go và migration goose.
- Template:
  - bỏ `AisdlcCasesTest.java.tpl`,
  - `config.yaml` theo §3.1,
  - `gitlab-ci.aisdlc.yml` dùng image Go, có Docker-in-Docker hoặc socket cho testcontainers, chạy `aisdlc eval --ci`,
  - `CLAUDE.aisdlc.md` viết lại cho Go.
- Web client trong CLI (Outbox, BackgroundFlush, FlushLock, Backoff, PinnedKey, PolicySync, ControlLedger) giữ nguyên giao thức. Chữ ký Ed25519 giữ định dạng phong bì (`payload` base64, `sig` base64) và khóa PEM (PKCS#8 + X.509).

## 8. Web

- `chi` + `pgx/v5` + goose (migration embed). **Gộp V1 đến V6 thành một migration khởi tạo**, vì không chuyển dữ liệu.
- **API JSON, SSE và cookie/CSRF giữ nguyên**, để `web/ui` chạy không sửa. Nếu buộc phải sửa UI thì chỉ được sửa chỗ gọi API, và ghi lý do.
- Auth giữ nguyên: token lưu SHA-256, session, CSRF so sánh constant-time, audit log, vai trò (`lead` v.v.). `UserAdminCli` thành `aisdlc-web user add|list|disable|token ...`.
- Khóa ký: file PEM như hiện tại, tự sinh khi chưa có.
- UI build bằng `npm ci && npm run build` trong `web/ui`, nhúng bằng `go:embed`. SPA fallback như `SpaController`.
- Cấu hình qua env: `AISDLC_DB_URL` (dạng URL pgx), `AISDLC_SIGNING_KEY_FILE`, `AISDLC_LISTEN`, và các khóa đang có trong `application.yml`.
- **[Claude]** Có thêm `Dockerfile` (multi-stage, distroless) cho web, vì team chạy trên EKS. Manifest hoặc Helm không thuộc phạm vi.

## 9. Kiểm thử

- **Port bộ test theo hành vi, không theo dòng.** Bản Java có khoảng 960 test: core 468, cli 286, junit 52, web 155. Mỗi lớp test Java có một file `_test.go` tương ứng, dạng table-driven. Test của `aisdlc-junit` thành test của `internal/harness`, chạy trên dịch vụ mẫu Go.
- Test web và harness dùng testcontainers-go (Postgres thật), không dùng H2 hay mock DB.
- UI giữ vitest hiện có.
- **Nghiệm thu:** `e2e/run-e2e.sh` (viết lại) cùng các script Playwright (`web-*.mjs`, `screenshots.mjs`) pass trên:
  - **dịch vụ mẫu Go** `e2e/fixture-service`: chi + pgx + goose, có endpoint fee tương đương bản Java;
  - lời giải PAY-1, FIX-2, FEE-3 và `flow/risk-policy.yaml` **viết lại bằng Go**. Kịch bản giữ đủ các bước dương tính và âm tính hiện có, ví dụ hook chặn sửa `cases.yaml` đã khóa, `CASES_LOCKED`, risk set, duyệt qua web và qua Claude Code.

## 10. Thứ tự làm

Mỗi giai đoạn có plan riêng. Hết mỗi giai đoạn, phần đã port phải build và test xanh.

1. **Nền:** `go mod`, Makefile, `model`, `spec` (đọc YAML chặt, validator), `gitx`, `lang` + adapter Go (phần không chạy tiến trình).
2. **Cổng và bước:** `gate`, `step`, `policy`, `risk`, CLI và hook (chưa có eval và web). Plugin chạy được với binary Go.
3. **Harness và eval:** `harness`, `stub`, adapter Go đầy đủ, `eval`, `review`, dịch vụ mẫu Go.
4. **Web:** server, migration, auth, ingest, gates, control, risk, SSE; nhúng UI; web client của CLI.
5. **Hoàn thiện:** template, plugin commands, e2e đầy đủ, tài liệu tiếng Việt (README, `huong-dan-su-dung.md`), CI.

Có thể làm song song: giai đoạn 4 (web server) bắt đầu ngay sau giai đoạn 1, chạy cùng giai đoạn 2 và 3.

## 11. Để builder quyết

- Tên module Go và layout chi tiết bên trong mỗi package.
- Chữ ký cụ thể của interface `Adapter`, `Process`, `UnitResult`.
- Thư viện so JSON tương đương `jsonassert` (tự viết hay dùng thư viện), miễn giữ đúng chế độ so sánh mà `CaseExecutor` đang dùng.
- Cách tổ chức Makefile và CI của chính repo AISDLC.
- Cắt các lớp Java thành file Go thế nào, miễn không gộp logic adapter vào core.
- Chi tiết dọn dẹp tiến trình và container khi bị ngắt.
