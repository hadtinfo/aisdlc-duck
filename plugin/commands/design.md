---
description: Soạn design.md, design.yaml và migration nháp, nộp cổng thiết kế
argument-hint: "[ghi chú thêm]"
---
Soạn và nộp cổng `design` của tính năng đang làm. Ghi chú của dev (nếu có): $ARGUMENTS

1. Chạy `aisdlc status` để biết đường dẫn tính năng và trạng thái cổng.
2. Đọc `feedback.md` của tính năng nếu có và xử lý hết góp ý của dev.
3. Chỉ dùng các mục có trong **template**, không thêm mục, không chép lại ticket. Mỗi mục tối đa 5 dòng (trừ bảng `API` và mục `Giả định`); bỏ mục không áp dụng.
4. Thiếu thông tin thì hỏi dev hoặc ghi vào mục `Câu hỏi mở`. Tuyệt đối không bịa.
   Sequence chỉ vẽ các bước quan trọng (tối đa khoảng 7 bước); `design.yaml` liệt kê các bước Sx và scope từng bước, bám BRD đã duyệt.
   Mục `API`: mỗi endpoint một dòng `METHOD /path · body {…}` và bảng `| Mã | Khi nào | Body |` cho từng mã trả về.
   Mục `Giả định`: liệt kê mọi lựa chọn (kiểu dữ liệu, đơn vị, múi giờ, giá trị mặc định, xử lý lỗi hệ thống ngoài, khóa/giao dịch) mà BRD/ticket không ghi, mỗi dòng `- <quyết định>. (<nguồn>)`, nguồn là `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời` hoặc `ngoài phạm vi`.
   Nếu đổi DB, viết migration goose nháp trong `db/` của tính năng: tên `NNNNN_ten.sql`, có `-- +goose Up`.
   **Rủi ro:** chạy `aisdlc policy show` để xem thang mức, rule và nhãn. Điền khối `impact` trong `design.yaml`: `level` (một mức trong thang), `labels` (đúng chữ các nhãn mà rule `label` dùng, nếu áp dụng) và `reason` (một câu: sai thì hậu quả gì). Không hạ mức cho dễ; chỉ dev đổi mức (`aisdlc risk set`).
   Khi tính năng ghi dữ liệu tiền hoặc trạng thái, khai `invariants` trong `design.yaml`: mỗi mục `{ name, sql, step }`, `sql` là một câu `SELECT`/`WITH` trả về các dòng vi phạm (đúng thì 0 dòng). Bất biến dùng bảng do một bước tạo ra thì đặt `step:` bằng bước đó (chỉ kiểm sau case của bước đó và các bước sau); bỏ `step` thì kiểm sau mọi case. Không sửa `risk.yaml` hay `aisdlc/risk-policy.yaml` (hook chặn).
5. Chạy `aisdlc submit design`. Sửa theo findings rồi nộp lại cho tới khi hết findings.
6. **Dừng.** Nói với dev: "Trước khi duyệt, `aisdlc review design` in review card (cờ đỏ, giả định, tóm tắt). Dev duyệt bằng `aisdlc approve design` ở terminal riêng (lệnh cần console thật)."
   Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm: "hoặc gõ `/aisdlc:approve design` để duyệt ngay trong Claude Code (hiện hộp xác nhận), hoặc duyệt trên web"; ngược lại giữ lời nhắc ở terminal/web.
   Không tự duyệt khi dev chưa yêu cầu trong cuộc trò chuyện này, không dùng `!`.
