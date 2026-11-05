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

Slash command: `/aisdlc:init`, `/aisdlc:brd`, `/aisdlc:design`, `/aisdlc:cases`, `/aisdlc:step`, `/aisdlc:eval`, `/aisdlc:review`, `/aisdlc:status`.
