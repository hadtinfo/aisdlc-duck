# AISDLC — Review nhanh trước khi agent thực thi: review card, cờ đỏ, hình sinh tự động

Ngày: 2026-11-03 · Trạng thái: đã duyệt (R1 đã xong; R2 đang làm, kế hoạch `docs/superpowers/plans/2026-11-03-aisdlc-review-r2.md`)

Spec gốc: `2026-11-02-aisdlc-design.md` (§1, §5, §7 màn B). Nền tảng: giai đoạn 1 (cổng, `design.yaml`, `cases.yaml`, `ScopeMatcher`, `SchemaSnapshot`) và 2a (trang B chỉ xem, snapshot đã mang nội dung artifact). Mock tham khảo: trang review cổng thiết kế của PAY-1 (artifact riêng tư, dữ liệu từ `e2e/solutions/PAY-1`).

## 1. Vì sao cần

Mỗi tính năng có một dev duy nhất duyệt mọi cổng (§2 spec gốc), nên người là điểm nghẽn. Hiện tại, khi chạy `aisdlc approve design`, dev phải tự mở `design.md`, `design.yaml`, `db/*.sql`, đọc chữ rồi tự ghép trong đầu xem agent định làm gì. Sơ đồ sequence do agent vẽ, và `DesignValidator` chỉ kiểm có đủ nhãn `S1…Sn`, nên một hình đẹp dễ làm dev yên tâm sai.

Ví dụ thật trong `e2e/solutions/PAY-1`: scope S2 `src/main/java/**/limit/**` bao luôn `limit/web/**` của S1, và `db/migration/**` khớp cả `V1__init.sql` đã có từ trước. Đọc `design.yaml` dạng chữ hay nhìn sequence đều không thấy hai điểm này.

**Nguyên tắc:** người không duyệt agent định làm *như thế nào*. Người duyệt **cái khung** (scope, budget, DB, API, case) và **các giả định** agent đã tự đặt ra. Mọi thứ máy kiểm được thì máy kiểm và tóm lại trước khi đưa cho người.

## 2. Mục tiêu

1. Duyệt một cổng trong **2–5 phút** theo thứ tự cố định: cờ đỏ → giả định → (cổng cases) bảng case → mở file gốc chỉ khi có điểm đáng ngờ.
2. Mọi tóm tắt, cờ và hình đều **sinh tất định** từ artifact và cây code thật. Không qua LLM, không do agent vẽ.
3. CLI và web dùng **chung một mô hình dữ liệu** (`ReviewCard`). CLI in ra chữ, web vẽ hình.

**Không phải mục tiêu:**
- Thay thế việc đọc case. Bảng case vẫn phải đọc kỹ, vì đó là nơi người review có giá trị nhất.
- Chặn cổng bằng cờ đỏ. Cờ chỉ cảnh báo. Findings chặn cổng vẫn do các validator hiện có quyết định.
- Thêm cổng mới cho mọi bước code (xem §7, chỉ áp cho bước rủi ro cao và để sau).

**Phản mục tiêu:**
- Lại là một lớp nội dung dài. Thẻ tóm tắt phải đọc xong trong khoảng 30 giây.
- Hình nói khác với thứ sẽ bị kiểm. Nếu không tính được thì hiện "không tính được" chứ không đoán.
- Duyệt chậm hơn hiện tại. In thẻ không được làm `approve` chậm quá 1 giây khi không chạy Maven.

## 3. Mục "Giả định" trong template **[Lựa chọn của Claude]**

- `brd.md` và `design.md` có thêm mục `## Giả định`, đặt trước `Câu hỏi mở`.
- Mỗi dòng là một quyết định agent đã tự chốt mà BRD hoặc ticket không nói rõ, kèm nguồn: `- Số tiền là BIGINT, đơn vị VND, không có phần lẻ. (tự chọn)`. Nguồn là một trong `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời`, `ngoài phạm vi`.
- Phân biệt với `Câu hỏi mở`: câu hỏi mở là điều agent **biết** mình chưa biết, còn giả định là điều agent **đã tự quyết**. Intent drift nằm ở loại thứ hai.
- Mục không bắt buộc phải có dòng. Mục trống hợp lệ, nhưng thẻ hiện "Agent không ghi giả định nào" để dev biết mà để ý.
- Prompt `/aisdlc:brd` và `/aisdlc:design` yêu cầu liệt kê mọi lựa chọn kiểu dữ liệu, đơn vị, múi giờ, giá trị mặc định, cách xử lý lỗi hệ thống ngoài, khóa/giao dịch mà BRD không ghi.
- Tính năng cũ không có mục này vẫn đọc được.

## 4. Review card **[Lựa chọn của Claude]**

`ReviewCard` là record trong `aisdlc-core` (gói mới `com.aisdlc.core.review`), tính từ artifact của cổng và **danh sách file của cây git** (`git ls-files` hoặc cây `HEAD`, cùng nguồn với `GitRepo`). Không duyệt thư mục làm việc, nên `target/` và `node_modules/` tự bị loại và việc in thẻ vẫn nhanh. File mới chưa commit mà khớp glob được hiện là "file mới" khi có trong `git ls-files --others --exclude-standard`.

| Cổng | Nội dung thẻ |
|---|---|
| BRD | số quy tắc, số tiêu chí chấp nhận, giả định, câu hỏi mở đã trả lời, phần ngoài phạm vi |
| DESIGN | cờ đỏ; giả định; mỗi bước: id, tiêu đề, scope, budget, **file đang có khớp glob**; API (endpoint, mã trả về); DB (bảng/cột mới, thao tác trên bảng có sẵn) |
| CASES | cờ đỏ; bảng mỗi case một dòng: `id | bước | loại | given rút gọn → status + body/db rút gọn`; độ phủ theo bước và theo loại; (khi có ID quy tắc, xem §6) ma trận quy tắc × case |
| REVIEW | giữ báo cáo eval hiện có, thêm dòng đầu tóm tắt cờ đỏ của các bước |

- **CLI:** `aisdlc approve <gate>` in thẻ **trước** bước xác nhận gõ lại feature key. Thêm `aisdlc review <gate>` chỉ in thẻ, agent được chạy (chỉ đọc). Thẻ dạng chữ, rộng tối đa 100 cột, không màu khi không phải TTY.
- **Snapshot (2a):** thêm trường tùy chọn `reviewCards` theo cổng. Web không tự tính lại, để CLI và web luôn khớp nhau. **Giữ `schemaVersion: 1`** vì đây là thay đổi chỉ thêm. Web hiện từ chối mọi version khác 1 (`SnapshotValidator`), nên tăng version sẽ khiến snapshot bị đẩy vào `rejected/` và bảng ngừng cập nhật mà không báo. Web bản cũ bỏ qua trường lạ, còn web bản mới hiện "chưa có review card" khi thiếu trường này.

### Hợp đồng JSON `reviewCards` (R1, cho web R2)

- **Khi nào có thẻ:** `reviewCards` là map `"BRD" | "DESIGN" | "CASES" | "REVIEW"` → thẻ, theo thứ tự cổng của luồng. Một cổng chỉ có thẻ khi trạng thái của nó **khác `LOCKED_BY_PREVIOUS`**. Ngay sau `init` chỉ có thẻ của cổng đầu, và thẻ REVIEW chỉ xuất hiện khi CASES đã duyệt. Cổng dựng thẻ lỗi thì bị bỏ khỏi map. Không còn cổng nào thì không có trường `reviewCards`. Snapshot quá lớn thì bỏ cả trường này, sau khi đã bỏ hết diff.
- **Trường null bị bỏ** (`NON_NULL`). Mọi thẻ có `gate` và `flags`.
  - `assumptions` và `assumptionsSection` **chỉ có ở BRD và DESIGN**. Mục "Giả định" chỉ vẽ khi có trường này. Có `assumptions: []` và `assumptionsSection: false` nghĩa là tài liệu cũ chưa có mục Giả định.
  - `brd` chỉ có ở BRD: `rules`, `rulesWithId`, `acceptanceCriteria`, `outOfScope[]`, `answeredQuestions[]`.
  - `design` chỉ có ở DESIGN: `steps[]`, `apiLines[]`, `statusCodes[]`, `apiRows[]` (`status`, `when`, `body`), `tables[]`, `touchesExisting[]` (`kind`, `table`, `text`).
  - `cases` chỉ có ở CASES: `rows[]` (`id`, `step`, `kind`, `title`, `given`, `expect`, `rules[]`), `coverage`, và `ruleMatrix` nếu có.
  - Thẻ REVIEW chỉ có `flags`. Thẻ lỗi chỉ có cờ NA `CARD_FAILED`.
- **`steps[]`:** gồm `id`, `title`, `scope[]`, `budget`, `matchedCount` (số thật) và `matchedFiles[]` (tối đa 20, xếp theo đường dẫn). Thêm `plannedFiles[]`, là đích của migration nháp khớp scope. `emptyGlobs[]` liệt kê glob không khớp file nào, tức "sẽ tạo mới".
- **`tables[].status`:**
  - `NEW`: bảng do SQL nháp tạo; `columns` lấy từ nháp.
  - `EXISTING`: bảng có sẵn được khóa ngoại của bảng trong thẻ trỏ tới.
  - `ALTERED`: bảng có sẵn bị `ALTER`, `TRUNCATE` hoặc `CREATE` lại; `columns` là cột hiện có; `addedColumns[]` (chỉ có ở `ALTERED`) là cột thêm bằng `ADD COLUMN`, theo thứ tự câu, bỏ trùng.
  - `DROPPED`: bảng có sẵn bị `DROP`.
  - Bảng tạo rồi xóa ngay trong nháp thì không có mặt.
  - Mỗi cột: `name`, `type`, `constraints[]` (theo thứ tự PK, NOT NULL, UNIQUE, FK, DEFAULT v) và `references` (dạng `account(id)`).
- **`coverage`:** bước → loại case → số case. Mọi bước khai báo đều có mặt, kể cả bước 0 case. Mọi loại (`happy`, `error`, `boundary`) đều có mặt, kể cả khi bằng 0. Case không có bước nằm ở khóa `"?"`.
- **`ruleMatrix`:** `BR-n` → loại → id case. Trường này chỉ có khi BRD có ID quy tắc. Mọi loại đều có mặt, rỗng là `[]`. ID quy tắc ở cả BRD lẫn `rules:` của case đều được chuẩn hóa thành `BR-n` (viết hoa, bỏ số 0 đầu), nên `br-1` và `BR-01` đều thành `BR-1`.
- **Khóa của cờ là cặp (`code`, `level`), không phải `code`.** Một mã có thể xuất hiện ở hai mức. Ví dụ: `DB_TOUCHES_EXISTING` HIGH (DROP/TRUNCATE hoặc UPDATE/DELETE không có WHERE cấp ngoài) và WARN (đổi cấu trúc) cùng lúc. `DEPENDENCY_ADDED` WARN hoặc NA cũng vậy.
- **`refs` theo mã:**
  - `SCOPE_OVERLAP`: hai id bước và chỗ chồng lấn, là một glob hoặc một file, xếp chung theo thứ tự tự nhiên (S2 trước S10).
  - `SCOPE_TOO_WIDE`, `BUDGET_HIGH`, `SCOPE_NO_SCOPE`: `[idBước]`. `SCOPE_BAD_GLOB`: `[idBước, glob]`.
  - `SCOPE_MATCHES_OLD_MIGRATION`: `[idBước, đường dẫn…]`.
  - `DB_TOUCHES_EXISTING`, `DB_CREATE_ONLY`: tên bảng.
  - `DB_UNRECOGNIZED_SQL`: câu SQL rút gọn. `DB_COLUMN_UNPARSED`: `"bảng: dòng cột"`.
  - `BRD_RULE_NO_ID`: nội dung quy tắc. `CASES_RULE_UNCOVERED`, `CASES_RULE_UNKNOWN`: `BR-n`.
  - `CASE_VALUE_NOT_IN_BRD`: id case, xếp theo thứ tự tự nhiên (C2 trước C10).
  - `DEPENDENCY_ADDED` WARN: `groupId:artifactId`.
  - Các mã còn lại, gồm `ASSUMPTIONS_EMPTY` và mọi cờ NA `*_FAILED`/`DOC_UNREADABLE`, có `[]`.
- **`DB_CREATE_ONLY` (INFO, "an toàn"):** chỉ có khi **mọi** câu SQL nháp là `CREATE TABLE` bảng mới hoặc `CREATE INDEX` trên bảng mới tạo. Một câu INSERT, UPDATE, DELETE, ALTER, DROP, hay một câu lạ là đủ để cờ này không xuất hiện.
- **Thứ tự** (JSON ổn định giữa các lần dựng):
  - `flags` theo `code`, rồi `level` (HIGH, WARN, INFO, NA), rồi `message`.
  - `steps`, khóa `coverage` và `ruleMatrix`, cùng id case trong ma trận: theo thứ tự tự nhiên (S2 trước S10).
  - `tables` theo tên.
  - `touchesExisting` theo thứ tự câu SQL. File nháp đọc theo version Flyway (V2 trước V10).
  - `rows` theo thứ tự trong `cases.yaml`.
  - Các map khác theo khóa.

## 5. Cờ đỏ **[Lựa chọn của Claude]**

Cờ đỏ là kiểu riêng `ReviewFlag(code, level, message, hint)` nằm trong `ReviewCard`, **không phải `Finding`** và **không lưu vào submission**. Lý do: `GateService` hiện coi mọi `Finding` là chặn (cổng thành `SUBMITTED_WITH_FINDINGS`, `approve` từ chối). Vì vậy cờ đỏ không bao giờ làm đổi trạng thái cổng. `level` là `WARN`, `INFO` (cờ "an toàn") hoặc `NA`. Mỗi cờ có mã, mô tả một dòng và gợi ý sửa một dòng.

| Mã | Cổng | Khi nào |
|---|---|---|
| `SCOPE_OVERLAP` | DESIGN, CASES (lite) | glob của hai bước cùng khớp một file hoặc thư mục (kiểm bằng `ScopeMatcher` trên cây hiện tại cộng đường dẫn mẫu sinh từ glob) |
| `SCOPE_TOO_WIDE` | DESIGN, CASES (lite) | glob khớp quá ngưỡng file đang có (mặc định 30) hoặc có dạng `src/**`, `**` |
| `SCOPE_MATCHES_OLD_MIGRATION` | DESIGN | glob khớp migration đã có trong `src/main/resources/db/migration` |
| `DB_TOUCHES_EXISTING` | DESIGN | migration nháp có `ALTER`, `DROP`, `TRUNCATE`, `UPDATE`/`DELETE` không có `WHERE` trên bảng đã có trong schema hiện tại |
| `BUDGET_HIGH` | DESIGN | budget một bước vượt ngưỡng của team (mặc định 10 file hoặc 400 dòng) |
| `CASE_VALUE_NOT_IN_BRD` | CASES | Một con số trong `when.body`, `given.sql`, `then.body` hoặc `then.db` không xuất hiện trong BRD đã duyệt, là dấu hiệu agent tự bịa. Chỉ xét số gắn với trường có tên thuộc nhóm tiền/hạn mức (`amount`, `limit`, `balance`, `fee`, `rate`, `max`, `min`, có thể cấu hình trong `review.valueFields`). Bỏ qua số có ≤ 2 chữ số, id, timestamp và status HTTP để tránh báo nhầm. |
| `DEPENDENCY_ADDED` | REVIEW | `pom.xml` có dependency mới so với lúc `init` |

- Ngưỡng nằm trong `aisdlc/config.yaml` mục `review:`, có giá trị mặc định. Đổi ngưỡng theo quy tắc MR cấu hình riêng như giai đoạn 1.
- Cờ "an toàn" cũng được hiện (ví dụ "DB chỉ `CREATE TABLE`, khóa ngoại trỏ vào bảng có sẵn") để dev khỏi phải tự kiểm.
- Phân tích SQL ở mức nhẹ: tách câu lệnh, nhận dạng từ khóa và tên bảng, đối chiếu với `SchemaSnapshot`. Không parse SQL đầy đủ. Không chắc thì hiện `NA` kèm lý do.

## 6. Hình sinh tự động trên trang B **[Lựa chọn của Claude]**

Web vẽ từ `reviewCards` trong snapshot, bằng SVG của frontend, có tô màu theo cờ. Hình chỉ để định hướng, quyết định duyệt vẫn dựa trên chữ và số.

1. **Bản đồ bước → vùng code** (cổng DESIGN): bước ở bên trái, thư mục hoặc file khớp glob ở bên phải. Đỏ: nhiều bước cùng chạm; vàng: glob khớp file đã có cần chú ý (migration cũ); xanh: file mới khớp thiết kế.
2. **ER diff** (cổng DESIGN): bảng có sẵn màu trung tính, bảng/cột mới màu xanh, cột hoặc bảng cũ bị sửa màu cam, `DROP` màu đỏ. Dữ liệu từ `SchemaSnapshot` hiện tại và schema sau khi chạy migration nháp (đã có khi `submit design`).
3. **Ma trận quy tắc × case** (cổng CASES): hàng là quy tắc, cột là happy / error / boundary. Ô trống là chỗ thiếu case. Cần quy tắc có ID:
   - Template BRD đánh số quy tắc `- BR-1: …`. Quy tắc thiếu ID sinh **cờ** `BRD_RULE_NO_ID` trong review card. Đây không phải finding của `BrdValidator`, nên không chặn cổng.
   - Case có thêm trường tùy chọn `rules: [BR-1]`. Quy tắc không có case nào thì sinh **cờ** `CASES_RULE_UNCOVERED`. Cờ này cũng không chặn cổng.
   - Tính năng không đánh ID thì không vẽ ma trận, chỉ hiện bảng case.
4. **Sequence của agent** giữ nguyên nhưng chuyển xuống mục thu gọn, ghi rõ "do agent vẽ, chỉ để tham khảo", đặt cạnh bản đồ bước để đối chiếu.

Bố trí trang B cho một cổng: dải tóm tắt → cột phải (cờ đỏ, giả định có ô tick "đã xem", lệnh terminal) → cột trái (hình, bảng API/case, sequence thu gọn). Ô tick giả định chỉ là tiện ích của trình duyệt trong 2a; từ 2b có thể ghi kèm bản ghi duyệt (§8).

## 7. Chỉ review phần thay đổi khi nộp lại **[Lựa chọn của Claude]**

- **Đường tạm cho R1, trước khi có 2b:** mỗi lần `submit`, CLI lưu bản sao artifact của cổng vào `.aisdlc/run/<KEY>/submissions/<gate>/<n>/` (đã gitignore, được hook bảo vệ).
- Từ 2b, web lưu toàn văn submission (§2 spec 2b) và đó là **nguồn duy nhất**. Khi repo ở chế độ `approvals: web`, CLI thôi ghi bản sao cục bộ, để không tồn tại hai nguồn lệch nhau.
- Thẻ của lần nộp thứ n > 1 có thêm mục "Thay đổi so với lần nộp trước": diff rút gọn theo file, và từng góp ý trong `feedback.md` của lần trả lại gần nhất kèm trạng thái "có thay đổi liên quan / chưa thấy thay đổi" (khớp theo file và mục được nhắc tới, mức nhẹ).

## 8. Liên quan tới 2b

- Nút **Duyệt** trên trang B (2b §3) hiện ngay dưới review card. Bấm khi còn cờ đỏ chưa xem thì hỏi xác nhận thêm một lần, liệt kê mã cờ. Bản ghi duyệt lưu kèm danh sách mã cờ lúc duyệt và các giả định dev đã tick, để audit biết dev đã thấy gì khi duyệt.
- Duyệt bằng terminal (2b §10) in cùng thẻ đó trước khi hỏi xác nhận.

## 9. Để sau

- **Plan trước khi code cho bước rủi ro cao:** `design.yaml` thêm `risk: low | high` cho từng bước. Bước `high` khi `step start` phải ghi 3–5 dòng kế hoạch (file sẽ sửa, chữ ký mới, cách xử lý giao dịch và đồng thời) để dev liếc qua trước khi agent code. Bước `low` giữ nguyên như hiện tại.
- **Facts mạnh hơn cho case:** bất biến DB chạy sau mọi case (`invariants` trong `design.yaml`), case `property` có bộ sinh dữ liệu và seed, case `concurrent` cho lỗi tranh chấp hạn mức. Ghi lại ở §12 spec gốc.

## 10. Thứ tự làm

1. **R1, không cần web:** mục Giả định trong template và prompt; `ReviewCard` + cờ đỏ trong `aisdlc-core`; `aisdlc review <gate>`; `approve` in thẻ. Snapshot mang `reviewCards`.
2. **R2, web trang B (nối tiếp 2a, chỉ xem):** vẽ bản đồ bước, ER diff, bố trí mới; ID quy tắc và ma trận quy tắc × case.
3. **R3, cùng 2b:** nút Duyệt dùng thẻ, bản ghi duyệt lưu cờ và giả định đã tick; diff giữa các lần nộp.
   - **Đã làm (2b-1, nhánh `aisdlc-web-2b1`):** nút Duyệt trên trang B mở hộp xác nhận liệt kê cờ HIGH/WARN, số giả định chưa đánh dấu và các file đã nộp; bản ghi duyệt lưu `flags` (mã + mức) và `assumptionsSeen` (giả định đã tick); duyệt từ terminal lưu cờ của thẻ CLI vừa in, `assumptionsSeen: []`. E2E kiểm cờ lưu đúng cho cả hai kênh.
   - **Đã làm (2b-2, nhánh `aisdlc-web-2b2`, plan `docs/superpowers/plans/2026-11-04-aisdlc-web-2b2.md`):** diff giữa các lần nộp — trang B mục "Thay đổi so với lần nộp trước (lần n)" (`GET /api/features/<repo>/<key>/resubmission?gate=`), mỗi góp ý trả lại kèm trạng thái `RELATED_CHANGED` / `NO_CHANGE_SEEN` / `UNMATCHED`; chỉ ở chế độ web. E2E trả lại DESIGN rồi nộp lại, kiểm lần 2 và góp ý `RELATED_CHANGED`.

## 11. Kiểm thử

- Đơn vị: mỗi mã cờ có test dương và âm. Dữ liệu PAY-1 phải sinh đúng `SCOPE_OVERLAP` (S1/S2 trên `limit/web/**`) và `SCOPE_MATCHES_OLD_MIGRATION` (`V1__init.sql`); FIX-2 không có cờ đỏ.
- Phân tích SQL: `CREATE TABLE` mới không cờ; `ALTER TABLE account` có `DB_TOUCHES_EXISTING`; câu không nhận dạng được trả `NA`.
- `ReviewCard` tất định: cùng đầu vào cho cùng JSON (so snapshot test).
- CLI: `approve` in thẻ trước bước xác nhận; `review` chạy được không cần console; thời gian in thẻ dưới 1 giây trên PAY-1.
- Web: Vitest cho hàm dựng hình từ `reviewCards`; E2E chụp trang B cổng DESIGN của PAY-1 khi `SCREENSHOTS=1`.

## 12. Để builder quyết

Định dạng chữ chi tiết của thẻ, tên trường JSON, thuật toán sinh đường dẫn mẫu từ glob, ngưỡng mặc định, cách bố trí SVG.
