# AISDLC

> English: [README.md](README.md), [docs/user-guide.md](docs/user-guide.md), [docs/web-operations.md](docs/web-operations.md).

Quy trình phát triển tính năng có kiểm soát chất lượng cho team làm dịch vụ Go (PostgreSQL, migration goose) cùng
agent Claude Code. Agent viết tài liệu, case và code. Dev duyệt từng cổng. Hook của plugin chặn agent đi tắt, và CI
chấm lại mỗi MR.

```
BRD ──▶ Thiết kế ──▶ Case ──▶ Code từng bước S1…Sn ──▶ Review ──▶ MR
 │         │           │            │                     │
 dev duyệt  dev duyệt   dev duyệt    dev accept từng bước   dev duyệt (eval phải ĐẠT)
```

- **Cổng duyệt:** agent chỉ được viết code sản phẩm khi các cổng trước đã được duyệt. Artifact đã duyệt bị khóa bằng
  hash; muốn sửa thì dev phải mở lại cổng.
- **Code theo bước:** mỗi bước có phạm vi file (glob) và ngân sách (số file, số dòng) khai trong thiết kế. Ghi ra
  ngoài phạm vi bị chặn, trừ khi dev cho phép từng file.
- **Case trước code:** bộ case (HTTP, DB, message, stub) được viết và khóa trước khi code. Lúc nộp, bộ case phải có
  case fail. Harness chạy case như hộp đen trên dịch vụ thật, với Postgres (và Kafka nếu cần) dựng bằng Docker.
- **Eval:** build + `go test` + bộ case + coverage dòng mới + review của subagent, ghi ra `eval/report.md`; job CI
  chạy lại trên MR.
- **Review card:** mỗi cổng có thẻ tóm tắt kèm cờ đỏ (scope chồng lấn, glob chạm migration cũ, đổi bảng có sẵn, case
  thiếu…), bản đồ bước, ER diff, bảng API và giả định của agent.
- **Rủi ro:** chính sách rủi ro của repo mô tả rule bằng lời; một subagent độc lập đọc tài liệu của tính năng ở cổng
  Thiết kế và xếp mức, kèm câu trích cho mỗi rule khớp. Mức càng cao thì bộ case càng phải chặt. Mức thấp có thể để hệ
  thống tự duyệt các cổng Thiết kế, Case và bước khi nộp sạch (BRD và Review luôn duyệt tay).
- **Web (tùy chọn):** bảng theo dõi tính năng, xem agent chạy trực tiếp, duyệt cổng trên web, tạm dừng agent, gửi chỉ
  dẫn, accept bước / cho phép file từ trình duyệt.
- **Ngôn ngữ:** tiếng Việt và tiếng Anh. Mỗi repo chọn một ngôn ngữ bằng `locale: vi|en` trong `aisdlc/config.yaml`
  (`aisdlc setup --locale en`; thiếu khóa là `vi`), và CLI, hook, template, review card, báo cáo eval cùng tài liệu
  agent viết đều theo đó. Mỗi người dùng web tự chọn ngôn ngữ giao diện (bộ chọn EN/VI). Chi tiết: mục 2.3 của
  [hướng dẫn sử dụng](docs/huong-dan-su-dung.md).

## Thành phần

| Thư mục | Nội dung |
|---|---|
| `cmd/aisdlc` | CLI `aisdlc` (cobra): lệnh của agent và của dev, đồng thời là chương trình chạy hook. Không kéo theo testcontainers nên hook khởi động nhanh |
| `cmd/aisdlc-harness` | Harness mà `aisdlc` gọi như tiến trình con khi chạy case và eval: dựng Postgres/Kafka (testcontainers), stub HTTP, chạy dịch vụ và `cases.yaml` như hộp đen |
| `cmd/aisdlc-web` | Server web: API, UI React nhúng, migration goose nhúng, quản trị user (`aisdlc-web user ...`) |
| `internal/` | Logic dùng chung: cổng và hash artifact, bước và phạm vi, validator BRD/thiết kế/case, review card, chính sách hook, rủi ro, eval, harness, phong bì ký Ed25519, web |
| `plugin/` | Plugin Claude Code: slash command `/aisdlc:*`, hook, subagent `aisdlc-reviewer`, launcher `bin/aisdlc`, `install.sh` |
| `templates/` | Mẫu tiếng Việt (`templates/vi/`) và tiếng Anh (`templates/en/`) cho `config.yaml`, `gitlab-ci.aisdlc.yml`, BRD, thiết kế, case mà `aisdlc setup` và `aisdlc init` chép ra |
| `web/ui/` | UI React của web |
| `e2e/` | Kiểm thử đầu-cuối trên dịch vụ mẫu `e2e/fixture-service` với lời giải `e2e/solutions/PAY-1`, `FIX-2`, `FEE-3` |
| `docs/` | Hướng dẫn cho dev, vận hành web, spec và plan; `docs/legacy-java/` là tài liệu của bản cũ |

## Yêu cầu

Máy dev: Linux hoặc macOS, Go, Docker (harness dựng Postgres/Kafka), git, Claude Code. Build UI web cần thêm Node.
E2E cần thêm `jq`, `curl`, `script`, Node + npm và mạng cho lần cài Playwright đầu tiên.

## Bắt đầu nhanh

1. **Cài plugin trên máy dev**

   ```bash
   git clone <repo aisdlc> ~/aisdlc && ~/aisdlc/plugin/install.sh   # build aisdlc + aisdlc-harness, link ~/.local/bin/aisdlc
   ```

   Trong Claude Code:

   ```
   /plugin marketplace add ~/aisdlc
   /plugin install aisdlc@aisdlc
   ```

2. **Gắn vào repo dịch vụ** (một MR riêng): chạy `aisdlc setup` ở gốc repo (thêm `--locale en` cho repo tiếng Anh), rồi kiểm mục `service` và `migrations`
   trong `aisdlc/config.yaml` (package main, tên biến môi trường, health check, thư mục goose). Chi tiết: mục 2.2 của
   [hướng dẫn sử dụng](docs/huong-dan-su-dung.md).

3. **Làm một tính năng:** `/aisdlc:init PAY-123` → `/aisdlc:brd` → `/aisdlc:design` → `/aisdlc:cases` →
   `/aisdlc:step S1`… → `/aisdlc:review`. Sau mỗi bước, dev duyệt **ở một terminal riêng**, ví dụ
   `aisdlc approve design` hoặc `aisdlc step accept S1`. Các lệnh này đòi console thật và bắt gõ lại feature key.
   Luồng nhẹ (`--lite`) cho sửa lỗi nhỏ thì bỏ qua BRD và thiết kế. Ví dụ đi trọn một tính năng: mục 3.4 của
   [hướng dẫn sử dụng](docs/huong-dan-su-dung.md).

4. **(Tùy chọn) Bật web:**

   ```
   docker build -f Dockerfile.web -t aisdlc-web .
   ```

   Chạy image cùng Postgres (biến `AISDLC_DB_URL`, `AISDLC_DB_PASSWORD`, volume `/data`), tạo user bằng
   `aisdlc-web user add`, đăng nhập web, tạo token ở trang `/me` và chạy `aisdlc web login`. Muốn duyệt trên web thì
   đặt `approvals: web` trong `aisdlc/config.yaml`. Xem mục 6 của [hướng dẫn sử dụng](docs/huong-dan-su-dung.md) và
   [vận hành web](docs/van-hanh-web.md).

## Phát triển dự án này

| Lệnh | Việc |
| --- | --- |
| `make build` | Build `bin/aisdlc` (CLI, hook) và `bin/aisdlc-harness`, nhúng cùng một phiên bản build (commit, thêm `+dirty` khi có thay đổi chưa commit) |
| `make check` | gofmt, vet, test (cần Docker) |
| `make test-short` | Test bỏ qua phần cần Docker (`AISDLC_SKIP_DOCKER=1`) |
| `make plugin-dist` | Build `aisdlc` và `aisdlc-harness` cho linux/darwin × amd64/arm64 vào `plugin/lib/` |
| `make build-web` | Dựng UI (`make ui`) rồi build `bin/aisdlc-web` |
| `make ci` | Chạy cục bộ các stage check và ui của CI |
| `make e2e` | E2E đầy đủ: repo đầu (PAY-1 FULL, FIX-2 LITE, âm tính), web, flow theo mức rủi ro, nhiều session. Log ở `e2e/out/` |
| `make e2e-web` | Chỉ phần web và flow |
| `WEB=0 bash e2e/run-e2e.sh` | E2E không có web |
| `SCREENSHOTS=1 make e2e` | E2E kèm ảnh chụp các trang web vào `e2e/out/screens` |
| `bash plugin/test/plugin-files-test.sh` | Kiểm `hooks.json`, launcher và các file của plugin |
| `bash docs/docs-check.sh` | Chạy các khối lệnh đánh dấu `# chạy-được` trong hướng dẫn trên repo tạm |

`plugin/lib/` không nằm trong git. Mỗi lần sửa CLI hoặc harness, chạy lại `plugin/install.sh` (hoặc
`make plugin-dist`), rồi cài lại plugin. `aisdlc` từ chối kết quả của `aisdlc-harness` khác bản build.

## Mô hình an toàn

Giả định agent có thể **nhầm lẫn**, không giả định agent **cố ý tấn công**.

- **Hook PreToolUse:**
  - chặn ghi file trạng thái (`.aisdlc/`, `gates.json`, `steps.json`…) và artifact đã duyệt;
  - chặn ghi ngoài phạm vi bước;
  - chặn mọi tool chạm `~/.config/aisdlc`, nơi lưu token và khóa ghim;
  - chặn lệnh in biến môi trường;
  - chặn agent chạy lệnh duyệt (trừ khi repo bật `sessionApprove`; khi đó dev phải bấm Yes trong Claude Code).
- **Hook không bao giờ gọi mạng.** Hook chỉ đọc cache có chữ ký Ed25519 của server; khóa công khai được ghim trên máy
  dev.
- **Ở chế độ duyệt web,** bản ghi duyệt nằm trên server. CI hỏi web và FAIL khi cổng chưa duyệt, hoặc khi code đổi
  sau lúc duyệt REVIEW.
- **Token có loại** (`login`, `ingest`, `approve`, `ci`). Token mà CLI và hook dùng không duyệt được gì.
- Chặn ghi qua Bash chỉ là heuristic; GitLab (protected branch + job eval) mới là lớp cưỡng chế. Các đường vòng đã
  biết: mục 8.1 của [hướng dẫn sử dụng](docs/huong-dan-su-dung.md).

## Tài liệu

- [Trang dự án](https://hadtinfo.github.io/aisdlc-duck/vi/): giới thiệu kèm ảnh chụp màn hình.
- [Hướng dẫn sử dụng](docs/huong-dan-su-dung.md): cài đặt, gắn repo, làm một tính năng từ đầu đến cuối, viết case,
  eval và CI, web, chính sách rủi ro, xử lý sự cố.
- [Vận hành web](docs/van-hanh-web.md): triển khai `aisdlc-web`, biến môi trường, khóa ký, DB, quản trị user.
- Spec port: [docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md](docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md);
  kế hoạch các giai đoạn trong [docs/superpowers/plans/](docs/superpowers/plans/).
- Tài liệu bản cũ: [docs/legacy-java/](docs/legacy-java/).
