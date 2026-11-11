# Song ngữ tiếng Anh / tiếng Việt cho AISDLC

Ngày: 2026-11-11. Trạng thái: chờ duyệt.

## Mục đích

AISDLC đã public trên GitHub với tài liệu tiếng Anh, nhưng mọi thứ người dùng thấy khi chạy (CLI, hook, template,
review card, web) đều là tiếng Việt. Team không đọc tiếng Việt không dùng được. Mục tiêu: một team chọn tiếng Anh
hoặc tiếng Việt và thấy trọn vẹn ngôn ngữ đó, từ `aisdlc setup` tới lúc duyệt Review trên web.

## Quyết định của chủ dự án

- **Mỗi team/repo dùng một ngôn ngữ từ đầu đến cuối.** Ngôn ngữ là thuộc tính của repo.
- **Giao diện web do từng người dùng tự chọn ngôn ngữ** (một server có nhiều repo).
- **Phạm vi là tất cả:** web, template, review card, *và* thông báo CLI/hook trong terminal.

## Mục tiêu

1. `aisdlc/config.yaml` có khóa `locale: vi | en`. Thiếu khóa thì là `vi`, nên repo đang dùng không đổi gì.
2. `aisdlc setup --locale en` ghi khóa đó; template `config.yaml`, `CLAUDE.aisdlc.md`, `gitlab-ci.aisdlc.yml` ra
   theo ngôn ngữ đã chọn.
3. `aisdlc init` chép bộ template BRD / design / cases theo locale của repo.
4. Validator BRD/design nhận tiêu đề mục của cả hai ngôn ngữ; lỗi báo theo locale của repo.
5. Mọi thông báo CLI, hook (gồm lý do chặn mà agent đọc), harness, eval (`eval/report.md`), review card (cờ, chữ trên
   thẻ), feedback.md ra theo locale của repo.
6. Agent viết BRD, thiết kế, case và trả lời dev bằng ngôn ngữ của repo.
7. Web có bộ chọn EN/VI; lựa chọn lưu theo người dùng trên server. Chữ do server sinh ra cho giao diện (inbox, lỗi
   API) theo ngôn ngữ người xem.

## Không làm

- Ngôn ngữ thứ ba. Cơ chế cho phép thêm sau, nhưng không làm bây giờ.
- Dịch nội dung tài liệu dev/agent đã viết (BRD, design, case của tính năng cũ).
- Đổi locale giữa chừng một tính năng: được phép, nhưng không chuyển đổi file đã có (validator nhận cả hai bộ tiêu đề
  nên vẫn chạy).

## Điều không được xảy ra (anti-goal)

- Repo đang dùng (không có `locale`) đổi hành vi, đổi hash artifact đã duyệt, hoặc test/E2E hiện có phải sửa
  kỳ vọng.
- Một chuỗi tiếng Việt lọt vào luồng tiếng Anh mà không test nào bắt được.
- Server web bị lẫn ngôn ngữ giữa các request đồng thời (dùng biến toàn cục cho locale trong server).

## Thiết kế

### Xác định locale

| Ngữ cảnh | Locale |
|---|---|
| Lệnh CLI / hook / harness chạy trong repo có `aisdlc/config.yaml` | `locale` trong file đó (thiếu → `vi`) |
| Lệnh CLI chạy ngoài repo (`web login`, `setup` trước khi có config) | biến `AISDLC_LOCALE` (`en`/`vi`), thiếu → `vi` |
| `aisdlc setup` | `--locale`, rồi `AISDLC_LOCALE`, rồi `vi` (*quyết định của người viết*) |
| `aisdlc-harness` (tiến trình con) | `aisdlc` truyền xuống qua biến `AISDLC_LOCALE` |
| Request tới `aisdlc-web` | locale của người dùng đã đăng nhập; chưa đăng nhập → header `Accept-Language`, rồi `vi` |

Giá trị khác `en`/`vi` trong config → lỗi cấu hình rõ ràng (exit 2).

### Cơ chế dịch phía Go: `internal/i18n`

Kiểu gettext: **chuỗi tiếng Việt hiện có chính là khóa**, bản tiếng Anh nằm trong catalog. Lý do: code đổi ít nhất,
người đọc code vẫn thấy câu đầy đủ, thiếu bản dịch thì rơi về tiếng Việt chứ không ra khóa vô nghĩa.

```go
i18n.T("Giữ nguyên CLAUDE.md (đã có mục AISDLC).")
i18n.Tf("Đã tạo %s", rel)              // thay cho "Đã tạo " + rel
loc.T(...), loc.Tf(...)                 // dạng có locale tường minh, dùng trong web server
```

- `i18n.SetDefault(loc)` gọi một lần ở `main` của `aisdlc` / `aisdlc-harness` sau khi đọc config. Hàm gói `T`/`Tf`
  dùng locale mặc định này. **Web server không gọi `T`/`Tf` gói**, chỉ dùng `Locale` lấy từ request.
- Catalog: `internal/i18n/en.go`, `map[string]string` từ chuỗi định dạng tiếng Việt sang tiếng Anh. Chuỗi định dạng
  phải cùng số và thứ tự động từ `%`.
- Các chuỗi đang nối bằng `+` được đổi sang `Tf` với `%s`/`%d`.

**Test bảo vệ (bắt buộc):**

1. `TestCatalogComplete`: quét AST mọi file `.go` không phải test; mọi lời gọi `T`/`Tf` với literal phải có mục
   trong catalog `en`, và động từ `%` khớp nhau.
2. `TestNoBareVietnamese`: mọi string literal không phải test có ký tự tiếng Việt phải nằm trong lời gọi `T`/`Tf`,
   hoặc trong danh sách cho phép có lý do (bảng tiêu đề mục, chính catalog, template nhúng).
3. `TestCatalogNoOrphans`: catalog không có mục thừa.

Ba test này là thứ bảo đảm anti-goal "chuỗi tiếng Việt lọt vào luồng tiếng Anh".

### Tiêu đề mục markdown

`internal/spec` có bảng mục chuẩn, mỗi mục một ID và tiêu đề theo từng locale:

| ID | vi | en |
|---|---|---|
| goal | Mục tiêu | Goal |
| inScope | Trong phạm vi | In scope |
| outOfScope | Ngoài phạm vi | Out of scope |
| rules | Quy tắc nghiệp vụ | Business rules |
| acceptance | Tiêu chí chấp nhận | Acceptance criteria |
| assumptions | Giả định | Assumptions |
| openQuestions | Câu hỏi mở | Open questions |
| (design) Sequence, API, DB | giữ nguyên | giữ nguyên |

`MarkdownSections` trả nội dung theo **ID**, nhận tiêu đề của cả hai ngôn ngữ. Validator, `docfacts`, review card đều
tra theo ID. Thông báo "thiếu mục X" dùng tiêu đề của locale repo. Thẻ `[biên]` trong quy tắc nghiệp vụ có bản
tiếng Anh `[boundary]`; nhận cả hai.

### Template

`templates/vi/…` (chuyển file hiện có vào, nội dung giữ nguyên byte) và `templates/en/…` (bản dịch). Áp dụng cho
`brd.md`, `design.md`, `design.yaml`, `cases.full.yaml`, `cases.lite.yaml`, `config.yaml`, `CLAUDE.aisdlc.md`,
`gitlab-ci.aisdlc.yml`. Khóa YAML giữ nguyên (đã là tiếng Anh), chỉ comment và giá trị mẫu được dịch. `config.yaml`
bản nào cũng có dòng `locale:` tương ứng.

### Review card, eval, feedback

Do CLI sinh ra theo locale **của repo** rồi đẩy lên web. Người xem web thấy giao diện theo ngôn ngữ của mình, còn nội
dung thẻ (cờ, giả định, bảng API, `eval/report.md`) theo ngôn ngữ của repo. *Quyết định của người viết:* vì mỗi team
một ngôn ngữ, giữ một catalog duy nhất phía Go thay vì để web dịch lại mã cờ (đỡ hai catalog phải đồng bộ). Thẻ có
thêm trường `locale` để web biết.

### Plugin Claude Code

Lệnh `/aisdlc:*`, agent `aisdlc-reviewer` và hook là file tĩnh, không đổi theo repo được. *Quyết định của người viết:*
viết lại prompt bằng tiếng Anh, mỗi prompt có chỉ dẫn: "đọc `locale` từ `aisdlc status` (dòng `Locale: …`), viết tài
liệu và trả lời dev bằng ngôn ngữ đó". `aisdlc status` in thêm dòng locale. Mô tả lệnh (`description`) viết song ngữ
ngắn: `Draft brd.md… / Soạn brd.md…`. Mục AISDLC trong `CLAUDE.md` của repo (template theo locale) nhắc lại ngôn ngữ.

### Web

- **UI:** thư viện tự viết nhỏ, không thêm dependency. `web/ui/src/i18n/{vi,en}.ts`; `en` có kiểu
  `Record<keyof typeof vi, string>` nên thiếu khóa là `tsc` báo lỗi. Hàm `t(key, params)` và hook `useT()`.
  Định dạng ngày giờ/số theo locale (`Intl`).
- **Lưu lựa chọn:** migration goose mới thêm cột `users.locale` (`NULL` = chưa chọn). `GET /api/me` trả `locale`,
  `PUT /api/me/locale` đặt lại. Chưa đăng nhập hoặc chưa chọn: `localStorage`, rồi `navigator.language`
  (`en*` → en, còn lại vi).
- **Bộ chọn** EN/VI trong `UserMenu` và trên trang Login.
- **Chữ do server sinh** (ví dụ inbox ở `internal/web/board/rules.go`, lỗi API hiển thị cho người dùng): dịch theo
  `Locale` của request, qua `internal/i18n` dạng tường minh.
- **Lệnh CLI hiển thị trên web** (`CopyCommand`) không đổi: tên lệnh và cờ vốn là tiếng Anh.

### Tương thích

- Không có `locale` → `vi` → byte-for-byte như hiện tại: template giống hệt, thông báo giống hệt, hash không đổi.
  Test và E2E hiện có giữ nguyên kỳ vọng tiếng Việt.
- Mã lỗi, mã cờ (`code`), tên cổng, khóa JSON/YAML không dịch.

### Chính sách rủi ro

Làm sau [xếp loại rủi ro bằng LLM](2026-11-11-llm-risk-classification-design.md). Bản đó bỏ luật từ khóa, luật là
mô tả bằng lời mà LLM hiểu ở ngôn ngữ nào cũng được, nên không cần preset song ngữ. Bản song ngữ chỉ dịch thông báo
của `internal/risk` và prompt của subagent `aisdlc-risk-classifier` như mọi phần khác.

### Các điểm đã chốt sau vòng đọc của reviewer (quyết định của người viết)

- **Nguồn giả định** (`tự chọn | suy ra từ BRD | câu hỏi mở đã trả lời | ngoài phạm vi`): bản tiếng Anh
  `own choice | inferred from BRD | answered open question | out of scope`. Validator nhận cả hai. Thẻ lưu nguyên văn
  dev/agent đã viết, nên JSON của thẻ `vi` không đổi. So khớp nội bộ dùng ID chuẩn hóa.
- **Chữ server gửi cho CLI/agent** (lỗi API khi gọi bằng token): CLI gửi `Accept-Language` theo locale của repo, server
  trả lỗi theo header đó. Người dùng web vẫn theo lựa chọn của mình.
- **Chữ server đã lưu vào DB** (ví dụ `ImportReason`, `pending_actions.reason`, audit): vẫn lưu tiếng Việt như cũ. Khi
  hiển thị thì dịch qua catalog (khóa của catalog chính là chuỗi tiếng Việt, nên dịch được cả dòng đã lưu từ trước).
  Phần động trong chuỗi đã lưu, nếu có, được tách ra lưu riêng hoặc giữ nguyên văn.
- **`aisdlc status` luôn in dòng `Locale: vi|en`**, kể cả repo cũ. Đây là ngoại lệ có chủ ý của anti-goal "không đổi
  kỳ vọng test": sửa vài kỳ vọng của `status`. Prompt của plugin dựa vào dòng này.
- **`card.locale`** chỉ để hiển thị, `omitempty`, không ghi khi là `vi`, nên JSON cũ giữ nguyên. Khi so thẻ lúc nộp lại,
  cờ được so theo `code`; chỉ thông điệp đổi (do đổi locale) thì không tính là cờ mới.

## Kiểm thử

- Ba test catalog ở trên, cùng `vitest` và `tsc` cho bộ từ điển UI.
- Unit: validator BRD/design với tiêu đề tiếng Anh; `setup --locale en` tạo đúng template; `init` theo locale; review
  card tiếng Anh không còn ký tự tiếng Việt trên một fixture.
- E2E: thêm một lượt luồng nhẹ (FIX-2) trên repo `locale: en`, kiểm tra output CLI và `eval/report.md` không có ký tự
  tiếng Việt, và web trả UI tiếng Anh khi người dùng chọn `en`.
- `docs/docs-check.sh` chạy được với cả hai locale.

## Tài liệu

`docs/user-guide.md` và `docs/huong-dan-su-dung.md` có mục chọn ngôn ngữ; README nêu ngôn ngữ hỗ trợ; bỏ ghi chú
"CLI messages… are currently in Vietnamese". CHANGELOG mục Added.

## Chia đợt giao (mỗi đợt một PR, đợt sau dựa trên đợt trước)

1. **Nền:** `internal/i18n` và ba test catalog (danh sách cho phép lúc đầu gồm mọi file chưa chuyển, rồi thu hẹp dần),
   khóa `locale`, `setup --locale`, template `vi`/`en`, bảng tiêu đề mục và validator, `status` in locale.
2. **CLI và hook:** `internal/cli`, `policy`, `gate`, `step`, `spec`, `model`, `snapshot`, `gitx`, `yamlx`, `fsx`,
   `cmd/aisdlc`.
3. **Harness, eval, review card, risk:** `harness`, `eval`, `review`, `risk`, `citest`, `lang`, `run`, `webclient`.
4. **Web:** cột `users.locale`, API, i18n UI và bộ chọn, chữ do server sinh.
5. **Plugin và tài liệu:** prompt tiếng Anh có chỉ dẫn locale, E2E tiếng Anh, docs, bỏ danh sách cho phép
   (`TestNoBareVietnamese` áp dụng toàn repo).

## Để người làm tự quyết

Tên hàm/kiểu cụ thể, cấu trúc file catalog (một file hay theo package), văn phong bản dịch tiếng Anh (ngắn, rõ, cùng
thuật ngữ với `docs/user-guide.md`: gate, step, scope, budget, case suite, eval).
