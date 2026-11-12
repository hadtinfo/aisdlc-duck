# Song ngữ EN/VI — Đợt 3 (harness, eval, review card, risk) Implementation Plan

**Goal:** Mọi chữ người dùng thấy do `internal/harness/**`, `cmd/aisdlc-harness`, `internal/eval`, `internal/review/**`,
`internal/risk`, `internal/citest`, `internal/lang/**`, `internal/run`, `internal/webclient/**` sinh ra theo locale của
repo; review card có trường `locale`; `eval/report.md` theo locale; CLI gửi `Accept-Language` cho web.

**Architecture:** Dùng cơ chế gettext của đợt 1 (`i18n.T`/`Tf`, khóa là chuỗi tiếng Việt). Bản dịch đặt trong
`internal/i18n/en_engine.go` (`var enEngine`, `register` trong `init`). Gói nào web server gọi (`internal/risk`,
`internal/review`) thì hàm trả chữ cho người dùng có thêm dạng locale tường minh (hậu tố `In`, đối số đầu
`i18n.Locale`); hàm cũ giữ chữ ký, gọi dạng `In` với `i18n.Default()`.

**Spec:** `docs/superpowers/specs/2026-11-11-i18n-en-vi-design.md` (đợt 3; mục "Review card, eval, feedback",
"Các điểm đã chốt").

**Nhánh:** `feat/i18n-engine`, tách từ `feat/i18n-foundation`; chạy song song với đợt 2 và 4.

## Global Constraints

- Locale `vi` ra đúng byte như cũ: test hiện có giữ nguyên kỳ vọng tiếng Việt.
- Không sửa `internal/i18n/en.go`, không sửa dòng allowlist của đợt khác, không sửa `CHANGELOG.md`.
- Mã cờ (`code`), mã lỗi, tên cổng, khóa JSON/YAML không dịch.
- Regex so khớp nội dung tài liệu nhận cả hai ngôn ngữ; không dịch regex.
- `card.locale` (`json:"locale,omitempty"`) chỉ ghi khi locale khác `vi` → JSON thẻ `vi` không đổi byte.
- Mọi request của `internal/webclient` gửi `Accept-Language: vi|en` (locale mặc định của tiến trình).

## Review Focus

1. JSON thẻ `vi` và `eval/report.md` của repo cũ giữ nguyên byte (test golden hiện có).
2. Dạng `In` của `risk`/`review`: web gọi được với locale của request, không đụng locale toàn cục.
3. Thẻ tiếng Anh dựng từ fixture không còn ký tự tiếng Việt.
4. So thẻ nộp lại: cờ so theo `code`, thông điệp đổi do đổi locale không là cờ mới (`review.NewFlags`).

## Tasks

### Task 1: harness, `cmd/aisdlc-harness`, `lang`, `run`, `citest`

- Bọc chuỗi trong `internal/harness/**`, `internal/lang/**`, `internal/run/schema.go`; thêm bản dịch vào `en_engine.go`.
- Test: `harnesscmd` chạy `Main` với `AISDLC_LOCALE=en` (tham số sai, `--mode` sai) → stderr và `Result.Error` tiếng Anh,
  không còn ký tự tiếng Việt.
- Xóa dòng allowlist `(đợt 3)` của các file này.

### Task 2: risk (kèm dạng `In` cho web)

- `ParsePolicyIn`, `PolicyFromJSONIn`, `LoadPresetIn`, `LatestPresetIn`, `MigrateToV2In`, `SupersedeMessageIn`,
  `(RiskPolicy).UsableIn`; `PolicyError` mang locale để `Error()` theo đúng locale.
- Test `en`: chính sách sai → lỗi tiếng Anh; dạng cũ vẫn ra tiếng Việt.

### Task 3: review, cardbuild

- Bọc cờ, chữ trên thẻ (`cardtext.go`), bảng API; `ReviewCard.Locale` (`omitempty`, `marshalSkipNil` tôn trọng
  `omitempty` cho chuỗi rỗng); `cardbuild` đặt `Locale` khi locale khác `vi`.
- `review.NewFlags(prev, cur)`: cờ của `cur` có `code` chưa có ở `prev`.
- Test: thẻ tiếng Anh từ fixture của `cardbuild` không có ký tự tiếng Việt; JSON thẻ `vi` không có khóa `locale`.

### Task 4: eval

- Bọc chuỗi `checks.go`, `service.go`, `markdown.go`, `report.go`, ...; `eval/report.md` theo locale.
- Test: dựng report với locale `en` không có ký tự tiếng Việt.

### Task 5: webclient

- Bọc chuỗi; `Client.Do` gửi `Accept-Language` theo `i18n.Default()`.
- Test với `fakeweb`: header `Accept-Language` là `vi` mặc định và `en` khi đặt locale `en`.

### Task 6: kiểm tra cuối

- `gofmt -l .`, `go vet ./...`, `AISDLC_SKIP_DOCKER=1 go test -race ./...`; không còn dòng `(đợt 3)` trong allowlist.
