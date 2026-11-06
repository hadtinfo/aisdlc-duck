# Hướng dẫn sử dụng AISDLC

Cập nhật: 2026-11-06

## 1. AISDLC là gì

AISDLC để agent Claude Code viết tài liệu, case và code, còn dev duyệt từng cổng. Hook của plugin chặn agent đi tắt, CI chấm lại mỗi MR. Dùng cho dịch vụ Go (chi/pgx hoặc tương tự) có migration goose và PostgreSQL.

Một tính năng đi qua 5 cổng, mỗi cổng phải được dev duyệt thì cổng sau mới mở:

1. **BRD**: yêu cầu nghiệp vụ, các rule `BR-n`.
2. **Thiết kế**: `design.md` (sequence có S1…Sn), `design.yaml` (scope và ngân sách từng bước), migration goose nháp.
3. **Case**: `cases.yaml` (HTTP, DB, message, stub). Lúc nộp, bộ case phải có case fail vì chưa có code.
4. **Code từng bước S1…Sn**: agent chỉ ghi được file trong scope của bước; dev accept từng bước.
5. **Review**: eval phải ĐẠT, có review độc lập của subagent; dev duyệt rồi mở MR.

Artifact đã duyệt bị khóa bằng hash. Muốn sửa thì dev mở lại cổng (`aisdlc reopen <gate>`).

![Luồng cổng: agent làm, dev duyệt](images/luong-cong.png)

Mỗi mũi tên sang phải là một lần agent nộp; chỉ sau khi dev duyệt, agent mới được sang cổng kế tiếp.

Các thành phần dev đụng tới:

| Thành phần | Vai trò |
| --- | --- |
| Plugin Claude Code (`plugin/`) | Slash command `/aisdlc:*`, hook, subagent `aisdlc-reviewer` |
| CLI `aisdlc` | Một binary Go: lệnh của agent và của dev; cũng là chương trình chạy hook |
| Harness (trong `aisdlc eval`) | Build dịch vụ, dựng Postgres/Kafka bằng Docker (testcontainers), chạy `cases.yaml` như hộp đen |
| Web `aisdlc-web` (tùy chọn) | Bảng theo dõi, duyệt cổng và điều khiển agent trên web |

Agent gọi plugin → `plugin/bin/aisdlc` chọn binary đúng nền tảng → CLI đọc/ghi file trạng thái trong repo. Eval build dịch vụ, chạy test cũ rồi chạy bộ case qua harness. Web là tùy chọn: CLI gửi lên `aisdlc-web` qua HTTPS kèm token.

## 2. Cài đặt

Máy dev cần Linux hoặc macOS, Go, Docker (harness dùng cho Postgres/Kafka), git và Claude Code. Không cần JDK hay công cụ build nào khác.

### 2.1. Cài plugin trên máy dev

```bash
git clone <repo aisdlc> ~/aisdlc && ~/aisdlc/plugin/install.sh
# build binary cho máy này vào plugin/lib/aisdlc-<os>-<arch>, symlink ~/.local/bin/aisdlc (thêm ~/.local/bin vào PATH)
```

Máy không có Go thì lấy repo đã chạy `make plugin-dist` (build sẵn linux/darwin, amd64/arm64 vào `plugin/lib/`); `install.sh` chỉ cần tạo symlink.

Trong Claude Code:

```
/plugin marketplace add ~/aisdlc
/plugin install aisdlc@aisdlc
```

- `plugin/lib/aisdlc-*` không nằm trong git. Chạy `install.sh` (hoặc `make plugin-dist`) trước `/plugin install`; marketplace phải trỏ vào thư mục đã build, không dùng URL git.
- Cập nhật binary xong thì cài lại plugin.
- Thiếu binary cho nền tảng này: trong repo AISDLC, hook chặn mọi công cụ ghi (Edit, Write, Bash ghi…) tới khi sửa xong; `Read`, `Grep`, `Glob` vẫn dùng được. Ở repo khác plugin im lặng, không làm gì.

### 2.2. Gắn vào repo dịch vụ (một MR riêng)

Chạy ở gốc repo dịch vụ Go (có `go.mod`, thường có `cmd/<tên>/main.go` và thư mục migration goose):

```bash
# chạy-được
aisdlc setup --preset generic
git status --short
```

(Thêm `--preset banking` nếu muốn mẫu chính sách rủi ro ngân hàng. Repo đã có file nào thì `setup` giữ nguyên file đó.)

Lệnh tạo `aisdlc/config.yaml`, `aisdlc/risk-policy.yaml`, `aisdlc/gitlab-ci.aisdlc.yml`, nối mục AISDLC vào `CLAUDE.md` và thêm `.aisdlc/` vào `.gitignore`. Nó dò `service.main` (đúng một `cmd/*/main.go`) và thư mục migration (`db/migrations`, `migrations`, `db/migration`, `sql/migrations`); dò không ra thì in lời nhắc để bạn tự sửa `aisdlc/config.yaml`.

Chế độ duyệt: mặc định `web` nếu máy chạy `setup` đã `aisdlc web login` (mục 6), ngược lại `local`. Chọn hẳn bằng `--approvals web` hoặc `--approvals local`.

Sau đó kiểm dịch vụ đáp ứng hợp đồng với harness:

1. **Biến môi trường**: `config.yaml` khai tên biến dịch vụ đọc (`service.env.port`, `databaseUrl`, `kafkaBrokers` nếu có). Harness điền giá trị. Dịch vụ phải nghe đúng cổng được cấp.
2. **Health check**: `service.healthPath` trả 2xx khi dịch vụ sẵn sàng, trong `startTimeoutSeconds`.
3. **Tắt êm**: dịch vụ thoát sau khi nhận `SIGTERM` trong `shutdownTimeoutSeconds`. Nếu không, harness `SIGKILL` và coverage dòng mới là N/A kèm lý do trong report. Mẫu: `e2e/fixture-service/cmd/server/main.go`.
4. **Migration goose**: tên `NNNNN_ten.sql`, có `-- +goose Up`, đặt trong `migrations.dir`.
5. `build`, `go test ./...` phải chạy sạch ở gốc repo (eval chạy `unitTest.packages`, mặc định `./...`).

Merge MR setup trước, rồi mới bật CI ở MR sau (mục 5).

## 3. Làm một tính năng

Agent chạy slash command trong Claude Code. Dev duyệt bằng lệnh ở một terminal riêng, trên web (mục 6), hoặc ngay trong Claude Code nếu repo bật `sessionApprove` (mục 3.1). Không dùng `!` trong Claude Code cho lệnh duyệt: chế độ đó không có console nên lệnh bị từ chối.

| Việc | Agent (Claude Code) | Dev (terminal riêng, hoặc mục 3.1) |
| --- | --- | --- |
| Khởi tạo | `/aisdlc:init PAY-123`: đề xuất luồng đầy đủ hay nhẹ, chạy `aisdlc init` | Chọn luồng |
| BRD | `/aisdlc:brd`: soạn `brd.md`, `aisdlc submit brd` | `aisdlc approve brd` hoặc `aisdlc reject brd -m "..."` |
| Thiết kế | `/aisdlc:design`: `design.md`, `design.yaml`, `db/*.sql` nháp; submit chạy thử migration goose | `aisdlc approve design` |
| Case | `/aisdlc:cases`: `cases.yaml`; submit chạy bộ case, phải có case fail | `aisdlc approve cases` (khóa bộ case) |
| Code | `/aisdlc:step S1`: `step start`, code trong scope, `step finish` | `aisdlc step accept S1` |
| Eval | `/aisdlc:eval`: build, test cũ và bộ case, ghi `eval/report.md` | Đọc report |
| Review | `/aisdlc:review`: eval, subagent ghi `review-agent.json`, `aisdlc submit review` | `aisdlc approve review`, rồi mở MR |

Lệnh duyệt (`approve`, `reject -m`, `reopen`, `step accept`, `scope allow`, `risk set`) mặc định đòi console thật và bắt gõ lại feature key. Không có cờ hay biến môi trường nào để bỏ qua; cách khác duy nhất là bật `sessionApprove` (mục 3.1).

`aisdlc approve <gate>` in review card trước khi hỏi: cờ đỏ (HIGH, WARN), giả định agent tự chốt, bản đồ bước, bảng DB, API. Xem trước bằng `aisdlc review <brd|design|cases|review>`. Cờ chỉ là gợi ý, không chặn cổng.

Ticket Jira chỉ để lấy tiêu đề: chưa đặt `JIRA_URL` và `JIRA_TOKEN` thì `init` cảnh báo rồi bỏ qua kiểm tra ticket (dùng `--title`).

### 3.1. Duyệt ngay trong Claude Code (`sessionApprove`)

Mặc định tắt. Bật bằng `sessionApprove: true` trong `aisdlc/config.yaml`, ở MR riêng. Giá trị được đọc từ `baseRef` (thường `origin/main`), không đọc working tree, nên chỉ có hiệu lực sau khi MR đã merge và agent không tự bật được. Repo không có `origin` hoặc chưa fetch thì coi như tắt. Kiểm tra bằng `aisdlc status`: dòng `Duyệt trong Claude Code: bật|tắt (<lý do>)`.

Khi bật, dev yêu cầu agent trong Claude Code:

| Slash command | Lệnh agent chạy |
| --- | --- |
| `/aisdlc:approve <cổng>` | `aisdlc review <cổng>` (in review card), rồi `aisdlc approve <cổng>` |
| `/aisdlc:reject <cổng> <lý do>` | `aisdlc reject <cổng> -m "<lý do>"` |
| `/aisdlc:accept <Sx>` | `aisdlc step accept <Sx>` |
| `/aisdlc:accept --file <path>` | `aisdlc scope allow <path>` |

Claude Code hiện hộp Yes/No; chỉ khi dev bấm Yes lệnh mới chạy (cấp một phiếu dùng một lần, hạn 60 giây, khớp session, tính năng, lệnh và hash cổng). Bấm No thì không duyệt; lần sau hỏi lại. Bản duyệt ghi kênh `claude-code`: `status` hiện "duyệt trong Claude Code (<dev>)", web hiện "(trong Claude Code)".

Lưu ý:

- Lệnh duyệt phải là lệnh đơn gọi thẳng `aisdlc <lệnh>`. Dạng gián tiếp (`bash -c`, biến, đường dẫn tới launcher, ghép `&&`/`;`/`|`, ký tự `$ * ~ ( )`…) bị từ chối. Lý do trả lại không chứa nháy hay chữ `eval`.
- Lệnh duyệt phải chạy một mình: tool khác chạy giữa chừng sẽ xóa phiếu, lệnh báo "chạy lại lệnh duyệt một mình".
- Chế độ quyền không hỏi người (`bypassPermissions`, `dontAsk`, `auto`) thì lệnh duyệt bị chặn: duyệt ở terminal riêng hoặc web.
- **Không** thêm quy tắc allow cho lệnh duyệt vào `permissions.allow` của Claude Code (ví dụ `Bash(aisdlc:*)`, `Bash(aisdlc approve:*)`). Chưa kiểm chứng hộp Yes/No vẫn hiện khi có quy tắc allow khớp.
- Terminal riêng và web vẫn dùng được như cũ.

### 3.2. Nhiều tính năng song song

Mỗi session Claude Code nhớ tính năng riêng của mình: `/aisdlc:init` hoặc `aisdlc use <KEY>` trong session nào thì gắn tính năng cho session đó, hook kiểm tra scope theo đúng tính năng đó. Hai session cùng thư mục không ghi đè nhau. Mọi lệnh in `[KEY]` ở dòng đầu.

- Terminal ngoài Claude Code: có từ 2 tính năng đang mở thì CLI hỏi chọn, hoặc báo lỗi kèm danh sách. Ghi rõ bằng `--feature <KEY>` (hoặc `-f`), ví dụ `aisdlc approve brd --feature PAY-1`.
- "Đang mở" là tính năng có trong checkout, chưa có ở `baseRef` và cổng REVIEW chưa duyệt. Tính năng đã merge mà vẫn làm tiếp thì chạy `aisdlc use <KEY>` hoặc dùng `--feature`.
- Session mới (hoặc sau `/clear`) chưa gắn tính năng mà có nhiều tính năng mở: hook chặn Edit/Write và nhắc agent chạy `aisdlc use <KEY>`.
- Làm nhiều tính năng trên cùng nhánh thì diff, bước và MR lẫn nhau; CLI cảnh báo một lần. Nên dùng mỗi tính năng một nhánh hoặc `git worktree`.

### 3.3. Luồng đầy đủ (FULL) và luồng nhẹ (LITE)

**FULL** (mặc định): BRD → Thiết kế → Case → Code từng bước → Review, như mục 3. Dùng cho thay đổi có DB, API hoặc dòng tiền.

**LITE** (`--lite`): cho sửa lỗi hoặc thay đổi nhỏ, không đổi DB hay API. Bỏ BRD và thiết kế; còn ba cổng CASES → code → REVIEW. `cases.yaml` tự khai `scope`, `budget`, `impact`, `invariants` và có đúng một bước `S1`.

```bash
# chạy-được
aisdlc init DOC-1 --lite --title "Sửa lỗi thiếu accountId"
aisdlc status
```

Ví dụ `status` sau lệnh trên: cổng `CASES` là `NOT_SUBMITTED`, `REVIEW` là `LOCKED_BY_PREVIOUS`, bước `S1` là `NOT_STARTED`. Làm tiếp một tính năng LITE từ đầu đến cuối:

1. `/aisdlc:cases`: agent sửa `aisdlc/features/DOC-1/cases.yaml` (scope Go như `internal/fee/**`, ngân sách, `impact`, case thật), chạy `aisdlc submit cases`. Submit chạy bộ case trên dịch vụ hiện có nên phải có ít nhất một case fail.
2. Dev `aisdlc approve cases`. Từ giờ `cases.yaml` bị khóa.
3. `/aisdlc:step S1`: agent code trong scope, `aisdlc step finish S1` chạy các case của bước. Dev `aisdlc step accept S1`.
4. `/aisdlc:review`: eval, review độc lập, `aisdlc submit review`. Dev `aisdlc approve review`, rồi mở MR.

Hook chặn sửa code khi cổng chưa mở. Ví dụ mô phỏng agent ghi `internal/fee/fee.go` ngay sau `init`:

```bash
# chạy-được
echo '{"tool_name":"Write","tool_input":{"file_path":"'"$PWD"'/internal/fee/fee.go","content":"x"},"session_id":"s1","cwd":"'"$PWD"'"}' \
  | aisdlc hook pre-tool-use | grep '"permissionDecision":"deny"' >/dev/null
```

### 3.4. Lệnh hay dùng khác

- `aisdlc status` hoặc `/aisdlc:status`: cổng nào đang chờ.
- `aisdlc use <KEY>`: đổi tính năng đang làm (của session Claude Code hiện tại).
- `--feature <KEY>` (`-f`): chỉ định tính năng cho một lệnh, không đổi tính năng đang làm.
- `aisdlc review cases`: xem trước review card của một cổng.
- `aisdlc policy show`: thang mức rủi ro, rule và yêu cầu theo mức.
- `aisdlc diff --by-step --patch`: diff theo từng bước.
- `aisdlc reopen <gate>`: mở lại cổng để sửa artifact đã duyệt. Sửa thẳng làm cổng thành STALE và các cổng sau coi như đóng.

```bash
# chạy-được
aisdlc review cases
aisdlc use DOC-1
aisdlc policy show
```

### 3.5. Viết `cases.yaml`

Một case gồm `given` (SQL dọn dữ liệu, stub), `when` (một request HTTP), `then` (status, body, DB, message). Mẫu đầy đủ: `templates/cases.full.yaml`; chưa có ví dụ tính năng hoàn chỉnh nào trong repo (thư mục `e2e/solutions/` chưa được port).

```yaml
reset_sql: ["DELETE FROM account"]
cases:
  - id: C1
    step: S1
    kind: happy
    title: Tạo tài khoản mới
    given: { sql: [], stubs: [] }
    when:  { method: POST, path: /api/v1/accounts, body: { id: "A1", ownerName: "An" } }
    then:
      status: 201
      db:
        - query: "SELECT id, owner_name FROM account"
          rows: [ { id: "A1", owner_name: "An" } ]
```

Quy tắc:

- `submit cases` chạy trên schema đã thiết kế (migration chính + `db/*.sql` nháp trong `aisdlc/features/<KEY>/db/`), nên case được dùng bảng mới. Khi `step` và `eval` chạy thì **chỉ** migration chính được dùng, không có nháp: bước nào chưa chép migration vào `migrations.dir` thì case của bước đó fail, giống CI và production.
- `reset_sql` chạy trước mọi case, nên chỉ dùng bảng có từ bước đầu; bảng tạo ở bước sau thì dọn trong `given.sql` (nếu không: `CASES_SETUP_NEEDS_LATER_TABLE`).
- Không hỗ trợ anchor/alias YAML (`&a`, `*a`).
- Stub dùng cú pháp kiểu WireMock, chỉ một tập con: `request` (`method`, `url`, `urlPath`, `urlPathPattern`, `queryParameters`, `headers` với `equalTo`/`matches`, `bodyPatterns` với `equalToJson`/`contains`), `response` (`status`, `headers`, `body`, `jsonBody`, `fixedDelayMilliseconds`). Khóa ngoài tập này bị `submit cases` từ chối.
- `then.body` so JSON theo cấu trúc, `then.statusCounts` cho case gửi nhiều request.
- Mức rủi ro cao có thể đòi case gửi trùng (`when.repeat`), song song (`when.parallel`) và bất biến DB; thiếu thì `submit cases` báo `RISK_REQ`. Xem chính sách bằng `aisdlc policy show`.

**So khớp `then.db`** (harness đọc DB bằng pgx và chuẩn hóa theo kiểu cột):

| Kiểu cột | Viết trong `rows` | Ví dụ |
| --- | --- | --- |
| Số (int, numeric, float) | So theo **giá trị** | `100` khớp `100.00`; `0.5` khớp `0.50` |
| Bool | `true` / `false` | `active: true` |
| `NULL` | `null` | `closed_at: null` |
| timestamp, timestamptz, date | Chuỗi RFC 3339 ở UTC; date là `YYYY-MM-DD` | `created_at: "2026-11-06T03:00:00Z"`, `due: "2026-11-06"` |
| json, jsonb | So theo **cấu trúc** (thứ tự khóa không quan trọng) | `meta: { a: 1 }` |
| uuid, text, các kiểu còn lại | So dạng text của Postgres | `id: "0b8e…"` |

Cách so dòng:

- Các dòng so **theo thứ tự** và số dòng phải khớp đúng; lệch thì báo "số dòng: mong đợi N, nhận M". Vì vậy `query` nhiều dòng phải có `ORDER BY`.
- Chỉ so các cột có trong `rows`; cột khác của câu `SELECT` bị bỏ qua.
- Tên cột (hoặc alias) không phân biệt hoa thường.

## 4. Code theo bước

Mỗi bước Sx có scope (glob file) và ngân sách (số file, số dòng) khai trong `design.yaml`, hoặc trong `cases.yaml` ở luồng nhẹ. Hook chặn agent ghi file ngoài scope của bước đang mở.

1. Agent chạy `/aisdlc:step S1`, tức `aisdlc step start S1`, đọc scope và case của bước.
2. Agent chỉ sửa file trong scope (code Go, và migration goose nếu bước có). Cần file ngoài scope thì agent dừng và nhờ dev.
3. Agent chạy `aisdlc step finish S1`: chạy migration đã có trong `migrations.dir` và các case của bước, rồi bước chuyển sang chờ dev (`AWAITING_DEV`).
4. Dev xem diff (`aisdlc diff --by-step --patch`) rồi chạy `aisdlc step accept S1` ở terminal riêng, hoặc `/aisdlc:accept S1` khi bật `sessionApprove`. Agent không tự accept.

Cho phép một file ngoài scope: `aisdlc scope allow <path>` (từng file, ở terminal riêng, hoặc `/aisdlc:accept --file <path>` khi bật `sessionApprove`). Case fail mà muốn sửa tiếp thì agent chạy lại `aisdlc step start S1` (bước đang chờ dev được phép mở lại), sửa, rồi `finish`.

Vượt 1,5 lần ngân sách chỉ bị cảnh báo (`BUDGET:Sx` trong eval), không chặn. Ngược lại, file đổi nằm ngoài mọi scope (kể cả `scope allow`) làm eval FAIL (`SCOPE`).

Mỗi MR chỉ một tính năng: MR chứa hai tính năng thì `SCOPE` của tính năng này báo nhầm file của tính năng kia.

## 5. Eval, report và CI

`aisdlc eval` (hoặc `/aisdlc:eval`) chạy trọn bộ trên máy dev:

1. Build dịch vụ có đo coverage (`go build -cover`).
2. Chạy test cũ (`go test` trên `unitTest.packages`).
3. Dựng Postgres (và Kafka nếu `env.kafkaBrokers` có giá trị) bằng Docker, chạy migration goose, khởi động dịch vụ, chờ `healthPath`.
4. Chạy từng case của `cases.yaml` như hộp đen, kiểm schema, tắt dịch vụ bằng `SIGTERM` và gộp coverage.

**Eval cần Docker** (testcontainers). Container được dọn khi eval xong, kể cả khi lỗi hay Ctrl-C. Không khởi động được container (Docker không chạy, không có quyền socket) thì harness báo lỗi khởi động như "không khởi động được Postgres: …", mọi case ở trạng thái ERROR và `eval` (hoặc `step finish`) thoát khác 0. Kết quả nằm ở `aisdlc/features/<KEY>/eval/report.json` và `report.md`, log harness ở `.aisdlc/run/<KEY>/harness.log`.

Eval ghi `eval/report.md`, dòng đầu là `Eval <KEY>: ĐẠT` hoặc `KHÔNG ĐẠT`; có thể dán nguyên file làm mô tả MR. Chỉ cần một kiểm tra cứng fail là eval fail; chỉ số mềm chỉ cảnh báo.

| Mã | Loại | Kiểm gì |
| --- | --- | --- |
| `CASES_ALL_PASS` | Cứng | Mọi case trong `cases.yaml` pass |
| `CASES_LOCKED` | Cứng | Bộ case khớp hash lúc duyệt |
| `BUILD_AND_TESTS` | Cứng | Build pass, test cũ không fail |
| `SCHEMA_MATCH` | Cứng | Schema sau migration khớp thiết kế |
| `SCOPE` | Cứng | Mọi file đổi nằm trong scope các bước |
| `CHECK:<tên>` | Cứng | Lệnh khai trong `checks` của `config.yaml` (ví dụ `go vet ./...`) |
| `RISK_COVERAGE` | Cứng | Coverage dòng mới ≥ `min` khi mức rủi ro yêu cầu |
| `BUDGET:Sx` | Mềm | Vượt 1,5 lần ngân sách bước |
| `COVERAGE_CHANGED_LINES` | Mềm | Coverage dòng mới dưới 80% |
| `AGENT_REVIEW` | Mềm | Subagent báo tiêu chí `NGHIEM_TRONG` hoặc issue `high` |
| `GATE:<cổng>` | Mềm, chỉ CI | Cổng chưa duyệt hoặc artifact đổi sau khi duyệt |

Chi tiết nằm ở `eval/report.json`, diff theo bước ở `eval/diff-by-step.md`, log build và dịch vụ ở `.aisdlc/run/<KEY>/`. Coverage "N/A" kèm lý do khi dịch vụ không tắt êm (không thoát sau `SIGTERM` trong `shutdownTimeoutSeconds`).

### Bật CI GitLab (MR sau MR setup)

1. Thêm vào `.gitlab-ci.yml`: `include: { local: aisdlc/gitlab-ci.aisdlc.yml }`.
2. Đặt biến CI/CD `AISDLC_BIN` = URL tải binary `aisdlc` linux/amd64 (từ `make plugin-dist`, đưa lên registry nội bộ). Runner phải chạy được `docker:dind` (privileged); template đã khai service `docker:dind` và `DOCKER_HOST`. Không ghi đè `GIT_DEPTH: "0"`.
3. Bật protected branch cho nhánh chính với "Pipelines must succeed". Hook chỉ bảo vệ trên máy dev; chặn merge do GitLab làm.

Job chạy `aisdlc eval --ci --base origin/<nhánh đích>` cho mọi tính năng có thay đổi trong MR. Runner không có Docker thì harness báo "không khởi động được Postgres: …", mọi case ERROR và job thoát khác 0.

- Sửa `aisdlc/config.yaml` phải nằm ở MR riêng; eval luôn đánh fail thay đổi kèm sửa cấu hình.
- MR đổi code không thuộc tính năng nào (ví dụ nâng dependency) bị fail: tạo một tính năng `--lite`, hoặc đặt `ciRequireFeature: false` ở MR riêng.

## 6. Web (tùy chọn)

Web cho bảng theo dõi, xem agent chạy trực tiếp, duyệt cổng, tạm dừng agent và gửi chỉ dẫn. Ở chế độ mặc định `local`, web chỉ xem và điều khiển; duyệt cổng vẫn bằng terminal (hoặc trong Claude Code, mục 3.1). Bản duyệt ghi kênh: `web`, `terminal` hoặc `claude-code` (hiện "(trong Claude Code)").

### 6.1. Chạy web

Server là binary Go `aisdlc-web` kèm UI React nhúng, đóng gói bằng `Dockerfile.web` (distroless). Chi tiết triển khai, biến môi trường và vận hành: [van-hanh-web.md](van-hanh-web.md). Chạy thử trên máy dev:

```
docker build -f Dockerfile.web -t aisdlc-web .
docker run -d --name aisdlc-db -e POSTGRES_USER=aisdlc -e POSTGRES_PASSWORD=doi-mat-khau -e POSTGRES_DB=aisdlc postgres:16-alpine
docker run -d --name aisdlc-web -p 127.0.0.1:8080:8080 --link aisdlc-db \
  -e AISDLC_DB_URL=postgres://aisdlc-db:5432/aisdlc -e AISDLC_DB_PASSWORD=doi-mat-khau \
  -v aisdlc-data:/data aisdlc-web
```

Sao lưu file khóa ký `/data/signing-key` (volume `aisdlc-data`): mất file thì mọi CLI phải `aisdlc web login` lại.

### 6.2. Tài khoản và token

1. Admin tạo user (email trùng `git config user.email`), lệnh in token login một lần:

   ```
   docker exec aisdlc-web /aisdlc-web user add dev "Tên Dev" --email dev@example.com
   ```
   (Còn `user role`, `user login-token`, `user ci-token <tên>`, `user list`.)
2. Dev đăng nhập web bằng token login, mở trang `/me` và tạo token **ingest**.
3. Chạy `aisdlc web login` trong terminal thật: nhập URL và token ingest. CLI ghim khóa ký của web và lưu `~/.config/aisdlc/web.json`.
4. Muốn duyệt từ terminal ở chế độ web: tạo token **approve** ở `/me`, chạy `aisdlc web approve-token`.

| Token | Dùng cho | Không làm được |
| --- | --- | --- |
| `login` | Đăng nhập web | CLI dùng bị từ chối (403) |
| `ingest` | CLI và hook gửi snapshot, tải trạng thái | Đăng nhập web, duyệt |
| `approve` | Duyệt, trả lại, mở lại từ terminal | Gửi snapshot |
| `ci` | `aisdlc eval --ci` hỏi bản duyệt | Duyệt |

Không đặt token vào biến môi trường `AISDLC_WEB_TOKEN`: agent đọc được biến môi trường. Lộ token thì thu hồi ở `/me` (có hiệu lực ngay).

### 6.3. Duyệt trên web (chế độ `approvals: web`)

Bật bằng `approvals: web` trong `aisdlc/config.yaml` (MR cấu hình riêng). Khi đó bản ghi duyệt nằm trên server, không ở `gates.json`.

- `aisdlc submit <gate>` nộp artifact lên web; web không trả lời thì lệnh exit 2 và không ghi gì.
- Trên trang tính năng `/f/<repo>/<key>?g=<GATE>`, chủ tính năng hoặc `lead` bấm **Duyệt**, **Trả lại**, **Mở lại**. Hộp xác nhận liệt kê cờ HIGH/WARN và giả định chưa xem.
- Repo đang dùng `local`: chạy `aisdlc web migrate`, rồi bấm **Xác nhận** trên trang B theo thứ tự cổng. Cổng REVIEW phải nộp và duyệt lại trên web.
- Chính sách rủi ro ở chế độ web: `aisdlc policy pull` lấy chính sách từ web (web nhập `aisdlc/risk-policy.yaml` làm bản v1); `submit design`/`submit cases` dừng và nhắc lệnh này khi cache chính sách thiếu hoặc sai chữ ký.
- CI: thêm biến `AISDLC_WEB_URL` và `AISDLC_CI_TOKEN` (Masked, Protected); token CI do admin cấp bằng `user ci-token <tên>`. CI FAIL khi cổng chưa duyệt, nội dung lệch hash, code đổi sau khi duyệt REVIEW, hoặc web không trả lời.

### 6.4. Điều khiển agent từ web

Dùng được ở cả hai chế độ duyệt, miễn máy dev đã `aisdlc web login`.

- **Tạm dừng** (màn live `/f/<repo>/<key>/live`): hook chặn mọi lệnh ghi của agent. Agent có thể ghi thêm tối đa một lần trước khi dừng. Sau khi bấm **Tiếp tục**, dev gõ một prompt trong Claude Code (ví dụ `tiếp đi`) để agent chạy lại.
- **Chỉ dẫn**: 1–2000 ký tự, không sửa hay xóa được. Agent nhận ở lần gọi tool kế tiếp hoặc prompt kế tiếp của dev.
- **Accept bước và Cho phép file** (trang B): web tạo lệnh có chữ ký, lệnh `aisdlc` hoặc hook kế tiếp trên máy dev áp dụng. Lệnh hết hiệu lực nếu bước đã đổi hoặc sau 7 ngày.

Kiểm tra kết nối bằng `aisdlc web status`; gửi bù hàng đợi bằng `aisdlc web push`. Web chết thì cổng đã duyệt vẫn còn trong cache đã ký, agent vẫn code được; chỉ `submit`, duyệt và CI dừng. Bản tin chưa gửi nằm ở hàng đợi `.aisdlc/web/`; sau một lần không kết nối được, CLI và hook thôi gửi 60 giây.

## 7. Mô hình an toàn và giới hạn đã biết

AISDLC giả định agent có thể nhầm lẫn, không giả định agent cố ý tấn công. Hook chặn lỗi thật thà trên máy dev; GitLab (protected branch + job eval) mới là lớp cưỡng chế.

Hook `PreToolUse` chặn:

- ghi file trạng thái (`.aisdlc/`, `gates.json`, `steps.json`, `risk.yaml`…) và artifact đã duyệt;
- ghi ngoài phạm vi bước đang mở, hoặc sửa code khi cổng chưa mở;
- mọi tool chạm `~/.config/aisdlc`, nơi lưu token và khóa ghim;
- lệnh Bash in biến môi trường (`env`, `printenv` không đối số, `export -p`…);
- mọi lệnh ghi khi dev đã bấm Tạm dừng trên web;
- gán hoặc export `AISDLC_SESSION` (để session này không đổi tính năng của session khác);
- lệnh duyệt khi chưa bật `sessionApprove`. Khi bật, lệnh duyệt gọi thẳng thì phải qua hộp Yes/No của Claude Code; dạng gián tiếp và chế độ quyền không hỏi người thì bị chặn.

Hook không bao giờ gọi mạng: nó chỉ đọc cache có chữ ký Ed25519 của server, xác minh bằng khóa công khai ghim trên máy dev. Cache bị sửa hay chép từ nơi khác thì coi như chưa duyệt; `aisdlc status` tải lại bản đúng. Mỗi lần gọi hook dưới 50 ms (p95) khi không có web.

### 7.1. Giới hạn đã biết

**Chặn ghi qua Bash chỉ dựa trên heuristic.** Hook chặn lệnh Bash chuyển hướng `>` vào file được bảo vệ, hoặc có lệnh ghi (`sed -i`, `rm`, `cp`, `git checkout`…) **và** nhắc đúng tên file được bảo vệ. Mục tiêu là chặn lỗi thật thà, không chống được người cố tình lách. Các đường vòng đã biết (không chặn được):

- Chuỗi lệnh lồng: `sh -c '…'`, `bash -c`, `eval "…"` nhận cả câu lệnh dưới dạng chuỗi.
- Tự viết một script rồi chạy sau: nội dung script không bị soi.
- `git -C <thư mục> …` ghi vào thư mục khác.
- `ln` (tạo liên kết rồi ghi qua liên kết) và `install`.
- `cp` sang một **thư mục** đích (tên file đích không nằm trong lệnh).
- `rm -rf` thư mục cha của file được bảo vệ.
- Chạy `python` hoặc `make` rồi để chúng ghi.
- `cd` thất bại giữa chuỗi lệnh `cd … && …` (lệnh sau chạy ở thư mục khác dự định), và biến `CDPATH`.

Lệnh nhắm cả thư mục hoặc dùng `cd … &&` có thể lọt. Dù lọt qua hook, sửa file đã khóa **vẫn lộ ra** ở `git diff` và ở hash trong `CASES_LOCKED`/`GATE:*`, nên eval trong CI đánh fail: GitLab mới là lớp cưỡng chế.

Các giới hạn khác:

- Kiểm Mermaid chỉ ở mức nhẹ: có khối `sequenceDiagram` và các bước S1…Sn khớp `design.yaml`. Không parse cú pháp đầy đủ.
- Chưa có job theo lịch (eval định kỳ, báo cáo tổng hợp). Chưa gọi GitLab/Confluence.
- **Mỗi MR một tính năng.**
- Chỉ có adapter Go (chi/pgx/goose hoặc tương tự) và PostgreSQL qua Docker. Adapter cho ngôn ngữ khác (ví dụ Java) là việc tương lai.
- Mới chạy trên Linux và macOS.

## 8. Xử lý sự cố và tra cứu nhanh

### Sự cố thường gặp

| Triệu chứng | Nguyên nhân | Cách xử lý |
| --- | --- | --- |
| Lệnh duyệt bị từ chối khi chạy bằng `!` trong Claude Code | Không có console thật | Chạy ở một terminal riêng, trong thư mục repo, hoặc bật `sessionApprove` (mục 3.1) |
| `/aisdlc:approve` báo "gọi thẳng `aisdlc <lệnh>`" | Lệnh gọi gián tiếp (`bash -c`, biến, đường dẫn, ghép lệnh) | Gọi đúng một lệnh `aisdlc approve <cổng>` |
| Lệnh duyệt báo "chạy lại lệnh duyệt một mình" | Tool khác chạy giữa chừng xóa phiếu, hoặc phiếu quá 60 giây | Chạy lại lệnh duyệt, không kèm tool khác |
| Lệnh duyệt bị chặn vì chế độ quyền | Claude Code đang ở `bypassPermissions`, `dontAsk` hoặc `auto` | Đổi về chế độ thường, hoặc duyệt ở terminal/web |
| `status` báo `Duyệt trong Claude Code: tắt (không đọc được … ở origin/main)` | Repo chưa có `origin` hoặc chưa fetch, hoặc MR bật chưa merge | `git fetch`, kiểm tra `baseRef` trong config |
| "Có nhiều tính năng đang mở…" | Từ 2 tính năng mở, lệnh không biết chọn cái nào | Thêm `--feature <KEY>`, hoặc `aisdlc use <KEY>` trong session |
| Hook chặn Edit/Write, nhắc `aisdlc use` | Session chưa gắn tính năng, có nhiều tính năng mở | Agent chạy `aisdlc use <KEY>`; vẫn chặn thì mở lại session Claude Code |
| Hook chặn mọi lệnh ghi, báo thiếu binary | Chưa có `plugin/lib/aisdlc-<os>-<arch>` cho nền tảng này | Chạy `plugin/install.sh` hoặc `make plugin-dist`, cài lại plugin |
| Cổng chuyển STALE | Artifact đã duyệt bị sửa trực tiếp | `aisdlc reopen <gate>`, sửa, nộp và duyệt lại |
| Eval báo "không khởi động được Postgres: …", mọi case ERROR | Docker không chạy, hoặc CI không có `docker:dind` / `DOCKER_HOST` | Bật Docker; ở CI giữ service `docker:dind` của template |
| Coverage "N/A" trong report | Dịch vụ không thoát sau `SIGTERM` kịp `shutdownTimeoutSeconds` | Sửa dịch vụ tắt êm (`http.Server.Shutdown`), hoặc tăng timeout |
| Dịch vụ không lên: mọi case `NOT_RUN` | Sai `healthPath`, sai tên biến cổng/DB, hoặc quá `startTimeoutSeconds` | Đọc 50 dòng log cuối trong report, sửa `service` trong `config.yaml` |
| `CASES_SETUP_NEEDS_LATER_TABLE` | `reset_sql` dùng bảng tạo ở bước sau | Dọn bảng đó trong `given.sql` của case bước đó |
| `RISK_REQ` khi `submit cases` | Mức rủi ro đòi case mà bộ case chưa có | Thêm case theo thông báo (`repeat`, `parallel`, bất biến) |
| `submit design`/`submit cases` dừng, nhắc `aisdlc policy pull` | Cache chính sách thiếu hoặc sai chữ ký | Chạy `aisdlc policy pull` |
| Web trả 401 | Token hết hiệu lực | Chạy lại `aisdlc web login` |
| Web trả 403 | Sai loại token | CLI dùng token ingest, duyệt dùng token approve |
| Agent đứng sau khi bấm Tiếp tục | Agent chờ prompt mới | Gõ một prompt (ví dụ `tiếp đi`) |
| CI fail với MR chỉ nâng dependency | MR không thuộc tính năng nào | Tạo tính năng `--lite` cho thay đổi đó |

### Lệnh của dev (terminal thật, gõ lại feature key; hoặc trong Claude Code khi bật `sessionApprove`)

| Lệnh | Việc |
| --- | --- |
| `aisdlc approve <gate>` | Duyệt cổng `brd`, `design`, `cases`, `review` |
| `aisdlc reject <gate> -m "..."` | Trả lại kèm lý do |
| `aisdlc reopen <gate>` | Mở lại cổng đã duyệt |
| `aisdlc step accept Sx` | Accept một bước |
| `aisdlc scope allow <path>` | Cho phép một file ngoài scope |
| `aisdlc risk set <mức> --reason "..."` | Đổi mức rủi ro (hạ mức bắt buộc lý do) |
| `aisdlc risk reclassify` | Phân loại lại theo chính sách hiện tại |
| `aisdlc web login` / `aisdlc web approve-token` | Lưu token ingest / approve |
| `aisdlc web migrate` | Đưa bản duyệt cục bộ lên web (chờ chủ tính năng xác nhận) |

### Lệnh chỉ đọc (agent cũng chạy được)

| Lệnh | Việc |
| --- | --- |
| `aisdlc status` | Trạng thái cổng, bước, tạm dừng từ web |
| `aisdlc review <gate>` | In review card (`--json` để lấy JSON) |
| `aisdlc diff --by-step --patch` | Diff theo từng bước |
| `aisdlc policy show` / `aisdlc policy pull` | Xem chính sách / lấy chính sách từ web |
| `aisdlc web status` / `aisdlc web push` | Kiểm hàng đợi gửi web / gửi bù |
| `aisdlc eval [--ci --base <ref>]` | Chạy eval (cần Docker) |

Vận hành server web: [van-hanh-web.md](van-hanh-web.md).
