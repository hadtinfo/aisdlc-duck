# Xếp loại rủi ro bằng LLM

Ngày: 2026-11-11. Trạng thái: chờ duyệt. Làm **trước** bản song ngữ
([2026-11-11-i18n-en-vi-design.md](2026-11-11-i18n-en-vi-design.md)).

## Mục đích

Hiện nay mức rủi ro của một tính năng được tính bằng luật so khớp: từ khóa (regex), path, bảng, endpoint, nhãn. So
khớp chữ không hiểu nghĩa. Nó bắt nhầm ("không thu phí" khớp luật phí) và bỏ sót (BRD tiếng Anh "ledger posting"
không khớp `hạch toán`, hoặc mô tả sổ cái bằng từ khác). Mục tiêu: một LLM đọc tài liệu của tính năng và xếp loại theo
nghĩa của từng luật trong policy.

## Quyết định của chủ dự án

- LLM chạy **trong phiên Claude Code**, là một **subagent độc lập** (như `aisdlc-reviewer`). Không cần API key. CI
  không chạy lại LLM.
- LLM được **nâng hoặc hạ** mức.
- **Bỏ hẳn** các luật `keyword`, `path`, `endpoint`, `table`, vì không bảo đảm hiểu đúng nghĩa.
- LLM xếp loại **một lần, lúc `submit design`**, khi đã có đủ BRD, thiết kế và migration nháp.
- **Cổng BRD luôn duyệt tay** (trước đây có thể tự duyệt theo mức tạm tính từ từ khóa).
- Làm tính năng này trước bản song ngữ.

## Mục tiêu

1. Luật trong policy là **mô tả bằng lời** kèm mức; LLM quyết định luật nào khớp, có trích bằng chứng.
2. Subagent mới `aisdlc-risk-classifier` (chỉ Read/Grep/Glob) trả JSON; `submit design` dùng JSON đó làm mức rủi ro.
3. Kết quả gắn với đúng bộ tài liệu đã đọc; tài liệu đổi sau đó thì kết quả bị coi là cũ và phải xếp lại.
4. Dev vẫn đổi mức tay được (`aisdlc risk set`, theo `classification.devLower`).
5. Review card và web hiện mức, luật khớp, câu trích, lý do.
6. Repo cũ chuyển sang policy mới bằng một lệnh.

## Không làm

- Gọi Claude API trực tiếp từ CLI hay CI.
- Xếp loại tạm ở `submit brd`.
- Giữ song song luật so khớp cũ "cho chắc".
- Xếp loại lại sau khi cổng Cases đã duyệt (giữ nguyên quy tắc hiện tại: duyệt Cases khóa `risk.yaml`).

## Điều không được xảy ra

- Tính năng bị tự duyệt khi chưa có kết quả xếp loại hợp lệ cho đúng tài liệu hiện tại.
- Kết quả của subagent bị dùng mà không kiểm schema, mức không có trong thang, hoặc luật không có trong policy.
- Repo có policy kiểu cũ chạy tiếp mà âm thầm bỏ qua các luật cũ (phải báo lỗi rõ, có lệnh chuyển).

## Thiết kế

### Policy

`schema: 2`. Luật chỉ còn:

```yaml
rules:
  - { id: R1, level: high, describe: "Mọi thay đổi chạm sổ cái: hạch toán, bút toán, định khoản, số dư" }
  - { id: G1, level: high, describe: "Có dòng tiền hoặc giá trị đi qua" }
```

- `describe` bắt buộc, không rỗng; viết bằng ngôn ngữ nào cũng được. `enabled` giữ như hiện tại.
- Bỏ `kind`, `match`, `note` (nội dung `note` gộp vào `describe`).
- Bỏ `classification.agent` (không còn mức của agent chính, xem dưới). Giữ `classification.devLower`.
- Khối `flow` bỏ cột `brd`. Policy còn khai `brd` thì cảnh báo và bỏ qua.
- `levels`, `default`, `requirements` giữ nguyên ý nghĩa. `default` là mức sàn: LLM xếp thấp hơn `default` thì lấy
  `default`.
- Policy `schema: 1` → lỗi cấu hình (exit 2) với hướng dẫn chạy `aisdlc policy migrate`.
- `aisdlc policy migrate`: chuyển `schema: 1` sang `schema: 2`. Mỗi luật cũ thành `describe` = `note` + gợi ý từ `match`
  (ví dụ `"Mọi thay đổi chạm sổ cái (từ khóa cũ: hạch toán|bút toán|định khoản)"`). Bỏ `classification.agent`. In
  diff để dev xem rồi mới ghi. Dev commit qua MR như mọi thay đổi policy.
- Preset `generic` và `banking` có phiên bản mới (3) dạng `schema: 2`. Phiên bản 1, 2 giữ byte nhưng chỉ còn dùng để so
  sánh (`diff-preset`); `setup` chép phiên bản 3.

### Khối `impact` trong design.yaml

Bỏ. Agent chính không còn tự đánh giá mức và nhãn, vì subagent làm việc này độc lập hơn. *Quyết định của người viết.*
`design.yaml` còn `impact` thì validator cảnh báo "không còn dùng", không lỗi.

### Subagent `aisdlc-risk-classifier`

`plugin/agents/aisdlc-risk-classifier.md`, công cụ `Read, Grep, Glob`.

**Đầu vào:** chỉ đường dẫn file, do `aisdlc risk inputs` in ra:

- Luồng đầy đủ: `brd.md`, `design.md`, `design.yaml`, migration nháp trong `db/` của tính năng, và
  `aisdlc/risk-policy.yaml`.
- Luồng nhẹ: tiêu đề và mô tả ticket (`feature.json`), `cases.yaml`, policy.

Lệnh còn in `inputsHash`: băm của nội dung các file trên. Agent chính truyền nguyên giá trị này cho subagent.

**Nhiệm vụ:** với mỗi luật đang bật, quyết định có khớp không theo nghĩa của `describe`. Mức đề xuất là mức cao nhất
trong các luật khớp, hoặc `default` nếu không luật nào khớp. Subagent được đề xuất mức khác với cách tính đó (nâng hoặc
hạ), nhưng phải ghi lý do trong `override`.

**Đầu ra:** chỉ một JSON, agent chính ghi nguyên văn vào `aisdlc/features/<KEY>/risk-agent.json` (cùng cách làm với
`review-agent.json`):

```json
{"inputsHash": "<y nguyên giá trị nhận được>",
 "level": "high",
 "matched": [{"rule": "R1", "evidence": "câu trích nguyên văn", "file": "brd.md"}],
 "notMatched": [{"rule": "G2", "reason": "không đổi cấu trúc bảng nào"}],
 "override": null,
 "summary": "1-2 câu"}
```

- `matched` và `notMatched` cộng lại phủ đủ mọi luật đang bật, mỗi luật đúng một lần.
- `override` là `{"from": "<mức tính từ luật>", "reason": "…"}` khi `level` khác cách tính trên, còn lại là `null`.
- `evidence` phải là chuỗi có thật trong `file`. CLI kiểm tra: không tìm thấy thì lỗi.

### `submit design` (luồng nhẹ: `submit cases`)

1. Đọc `risk-agent.json`. Thiếu, JSON sai, `inputsHash` khác băm hiện tại, mức không có trong thang, luật không có
   trong policy, không phủ đủ luật, hoặc `evidence` không tìm thấy → **chặn**, kèm thông báo hướng dẫn chạy lại
   subagent.
2. Mức = mức của LLM, nâng lên `default` nếu thấp hơn. Ghi `risk.yaml`: `level`, `policy`, `matched` (rule, level,
   evidence), `agent` → thay bằng `llm: { level, override, summary, inputsHash }`, `override` của dev giữ nguyên.
3. `submit cases` của luồng đầy đủ: nếu policy hoặc tài liệu đầu vào đã đổi so với `inputsHash` trong `risk.yaml` thì
   chặn và yêu cầu xếp lại (trước đây tự tính lại bằng luật).

### Cổng BRD

`AutoGate` coi BRD luôn là duyệt tay ("Cổng BRD luôn duyệt tay"). Bỏ `BrdLevel` / mức tạm và dòng
`Rủi ro tạm (từ khóa)` của `submit brd`.

### Review card và web

- Card cổng Design (luồng nhẹ: Cases) có khối "Xếp loại LLM": mức, từng luật khớp với câu trích, luật không khớp kèm
  lý do (thu gọn), `override` nếu có, `summary`.
- Cờ mới: `RISK_LLM_OVERRIDE` (WARN) khi subagent chọn mức khác cách tính từ luật. Đây là cờ mức WARN nên không chặn
  tự duyệt; dev đọc lý do trên card.
- Trang chính sách trên web: bảng luật hiện `describe` thay cho `kind`/`match`. Bỏ ô "thử phân loại" (`RiskTry`, API
  `internal/web/policy/service.go` gọi `risk.Classify` trên văn bản mẫu), vì server không có LLM.
- Phiên bản policy trên web và so sánh phiên bản hiểu `schema: 2`.

### Plugin

- `/aisdlc:design`: bỏ hướng dẫn điền `impact`. Thêm bước, trước `submit design`: chạy `aisdlc risk inputs`, gọi
  subagent `aisdlc-risk-classifier` với đúng các đường dẫn và `inputsHash` đó, ghi JSON nguyên văn vào
  `risk-agent.json`. `submit design` báo lỗi → sửa tài liệu thì chạy lại subagent, không sửa tay JSON.
- `/aisdlc:cases` luồng nhẹ: tương tự, trước `submit cases`.
- Hook: thêm `risk-agent.json` vào danh sách agent chính được ghi (như `review-agent.json`). Giữ chặn ghi `risk.yaml`
  và `aisdlc/risk-policy.yaml`.

### CI

`eval --ci` kiểm `risk.yaml` có `llm.inputsHash` khớp tài liệu đã duyệt, và policy hash khớp. Không gọi LLM. Mô hình an
toàn giữ nguyên: phòng agent làm sai, không phòng agent cố tình gian lận. Vì kết quả do phía agent ghi, lớp bảo vệ thật
vẫn là dev đọc card, cổng duyệt tay và MR.

### Dọn code

Bỏ khỏi `internal/risk`: `firstMatch`, `keyword`, `tableMatches`, `endpointMatches`, đường dẫn `path` trong
classifier, `CollectBrdOnly`, các `Collect*` thu path/bảng/endpoint không còn dùng, và `TestNoBusinessVocabulary` nếu
không còn ý nghĩa. `globoverlap.go` giữ nếu review card (cờ trùng scope) còn dùng.

### Các điểm đã chốt sau vòng đọc của reviewer (quyết định của người viết)

1. **Chế độ web:** policy có hiệu lực có thể nằm trong cache đã ký `.aisdlc/web/policy.json` chứ không ở
   `aisdlc/risk-policy.yaml`. `aisdlc risk inputs` ghi policy có hiệu lực ra
   `.aisdlc/risk/policy-effective.yaml` để subagent đọc. `inputsHash` dùng `p.Hash()` (băm chuẩn hóa của policy) cộng
   băm nội dung các tài liệu, không dùng byte file policy.
   - Server **không** tự chuyển các phiên bản đã lưu.
   - Server đọc được `schema: 1` ở chế độ chỉ đọc (lịch sử, so sánh). Trang chính sách có nút "Chuyển sang schema 2"
     tạo bản nháp theo đúng quy tắc của `policy migrate`; người có quyền sửa policy xem và phát hành như mọi phiên bản.
   - Khi CLI nhận policy `schema: 1` từ server thì exit 2, kèm thông báo "quản trị cần chuyển policy trên web".
2. **Tính năng đang làm hoặc đã merge:**
   - `init` ghi `riskSchema: 2` vào `feature.yaml`. Chỉ tính năng có dấu này mới đi luồng mới.
   - Tính năng cũ (không có dấu) giữ `risk.yaml` cũ, không xếp lại. `eval --ci` chấp nhận `risk.yaml` không có khối
     `llm` cho các tính năng này.
   - `risk.RequiredByDesign` được thay bằng phép kiểm dấu này.
3. **`aisdlc risk reclassify`:** giữ lệnh, đổi nghĩa thành "áp kết quả `risk-agent.json` mới" (sau khi chạy lại
   subagent). Như hiện nay, lệnh được mở lại cổng Cases khi dev xác nhận, và vẫn phát các sự kiện web
   `supersede`/`RISK_CLASSIFIED`. Mục "Không làm" về xếp lại sau Cases hiểu là: không *tự động* xếp lại.
4. **Luồng nhẹ:** đầu vào chỉ có tiêu đề (`feature.yaml`) và `cases.yaml`, không lưu mô tả ticket. Khối `impact` trong
   `cases.yaml` bỏ, cùng cách xử lý như `design.yaml` (cảnh báo, không lỗi).
5. **Đọc `schema: 1`:** parser có chế độ chỉ đọc cho `schema: 1`. Lỗi exit 2 chỉ xảy ra ở chỗ policy thật sự được dùng
   để xếp loại hoặc phát hành.
   - So sánh `schema: 1` với `schema: 2` sinh một mục `SCHEMA_MIGRATED`, rồi mỗi luật một mục thay đổi.
   - Khi phát hành, đổi `describe` của một luật cũng bị coi là có thể nới lỏng, nên phải ghi lý do (chọn phía chặt).
6. **CI so `llm.inputsHash` với file hiện tại.** Các file này đã bị khóa theo hash cổng Design (BRD, design, migration
   nháp; luồng nhẹ: cases). Sửa chúng phải mở lại cổng, và lúc đó phải xếp lại. Vì vậy kiểm trên file hiện tại không gây
   lỗi giả.

## Kiểm thử

- Unit: kiểm `risk-agent.json` (từng lỗi ở mục `submit design` bước 1 có một test), mức nâng lên `default`, BRD luôn duyệt
  tay, `policy migrate` trên cả hai preset cũ và trên policy của `e2e/fixture-service`, cảnh báo `impact`/`flow.brd`.
- E2E: không gọi LLM thật. `e2e/solutions/*` có sẵn `risk-agent.json` cố định; script E2E thay `inputsHash` bằng giá
  trị `aisdlc risk inputs` in ra rồi ghi file, giả lập việc agent làm. PAY-1 vẫn ra `high` (luật E1 chuyển thành mô tả
  "hạn mức").
- Web: bảng luật hiển thị `describe`; không còn ô thử phân loại.

## Tài liệu

User guide (cả hai bản) mục rủi ro viết lại: luật mô tả bằng lời, subagent xếp loại, BRD duyệt tay, `policy migrate`.
CHANGELOG ghi **Breaking**: policy `schema: 2`, bỏ `impact`, BRD luôn duyệt tay.

## Chia đợt giao

1. **Lõi:** policy `schema: 2` và `policy migrate`, preset v3, `risk inputs`, kiểm `risk-agent.json`, `submit
   design`/`submit cases`, BRD duyệt tay, `risk.yaml` mới, dọn classifier.
2. **Plugin, card, web, E2E, tài liệu.**

## Để người làm tự quyết

Cách băm `inputsHash` (thứ tự file, chuẩn hóa xuống dòng), câu chữ của prompt subagent, bố cục khối "Xếp loại LLM",
cách dò `evidence` (khớp nguyên văn sau chuẩn hóa NFC và khoảng trắng).
