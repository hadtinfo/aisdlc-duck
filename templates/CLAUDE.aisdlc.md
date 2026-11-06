<!-- aisdlc -->
## AISDLC

Repo này làm tính năng theo quy trình BRD → Thiết kế → Case → Code từng bước → Review.
Tài liệu của tính năng nằm ở `aisdlc/features/<JIRA-KEY>/`; mỗi cổng do dev duyệt.

- Không sửa code sản phẩm khi cổng thiết kế và cổng case chưa được duyệt (hook chặn).
- Không sửa `cases.yaml` sau khi bộ case đã khóa; muốn đổi thì dev chạy `aisdlc reopen cases`.
- Không sửa artifact đã duyệt và file trạng thái (`gates.json`, `steps.json`, `scope-extra.json`, `feedback.md`, `.aisdlc/`), kể cả qua Bash.
- Làm từng bước: `aisdlc step start Sx`, chỉ sửa file trong phạm vi bước, xong chạy `aisdlc step finish Sx`, báo kết quả rồi dừng chờ dev.
- Cần sửa file ngoài phạm vi: nhờ dev chạy `aisdlc scope allow <path>`.
- Bị trả lại ở cổng nào: đọc `feedback.md` và làm theo ở lượt kế tiếp.
- Rủi ro: ở bước thiết kế chạy `aisdlc policy show` rồi điền `impact` trong `design.yaml` (lite: `cases.yaml`). `risk.yaml` và `aisdlc/risk-policy.yaml` chỉ đọc; viết đủ case theo `requirements` trong `risk.yaml`.
- Chỉ dev chạy (trong terminal của mình): `approve`, `reject`, `reopen`, `scope allow`, `step accept`, `risk set`, `risk reclassify`.

- Dịch vụ Go: mã nằm ở `cmd/` (main) và `internal/`; migration theo goose, tên `NNNNN_ten.sql`, có `-- +goose Up`, thư mục chính `db/migrations/`. Bản nháp của tính năng đặt ở `aisdlc/features/<KEY>/db/`.
- Kiểm tra mỗi bước bằng `go test ./...` (và `go vet ./...` nếu repo bật); `aisdlc step finish Sx` cũng chạy build và test.
- Quy tắc so DB trong `then.db` (cases.yaml): số so theo giá trị; bool là `true`/`false`; NULL khớp `null`; timestamp và date dạng RFC 3339 UTC; json/jsonb so theo cấu trúc; còn lại so dạng text.
- Chạy `aisdlc eval` cần Docker (harness dùng testcontainers cho Postgres/Kafka).

Slash command: `/aisdlc:init`, `/aisdlc:brd`, `/aisdlc:design`, `/aisdlc:cases`, `/aisdlc:step`, `/aisdlc:eval`, `/aisdlc:review`, `/aisdlc:status`.
