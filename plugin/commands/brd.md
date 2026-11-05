---
description: Soạn brd.md từ ticket và nộp cổng BRD
argument-hint: "[ghi chú thêm]"
---
Soạn và nộp cổng `brd` của tính năng đang làm. Ghi chú của dev (nếu có): $ARGUMENTS

1. Chạy `aisdlc status` để biết đường dẫn tính năng và trạng thái cổng.
2. Đọc `feedback.md` của tính năng nếu có và xử lý hết góp ý của dev.
3. Chỉ dùng các mục có trong **template**, không thêm mục, không chép lại ticket. Mỗi mục tối đa 5 dòng (trừ mục `Giả định`); bỏ mục không áp dụng.
4. Thiếu thông tin thì hỏi dev hoặc ghi vào mục `Câu hỏi mở`. Tuyệt đối không bịa.
   Quy tắc nghiệp vụ đánh `BR-1`, `BR-2`... (mỗi dòng một ID).
   Mục `Giả định`: liệt kê mọi lựa chọn (kiểu dữ liệu, đơn vị, múi giờ, giá trị mặc định, xử lý lỗi hệ thống ngoài, khóa/giao dịch) mà BRD/ticket không ghi, mỗi dòng `- <quyết định>. (<nguồn>)`, nguồn là `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời` hoặc `ngoài phạm vi`.
   (BRD: mục `Câu hỏi mở` giữ các điểm chưa rõ từ `/aisdlc:init`.)
5. Chạy `aisdlc submit brd`. Sửa theo findings rồi nộp lại cho tới khi hết findings.
6. **Dừng.** Nói với dev: "Trước khi duyệt, `aisdlc review brd` in review card (cờ đỏ, giả định, tóm tắt). Dev duyệt bằng `aisdlc approve brd` ở terminal riêng (lệnh cần console thật)."
   Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm: "hoặc gõ `/aisdlc:approve brd` để duyệt ngay trong Claude Code (hiện hộp xác nhận), hoặc duyệt trên web"; ngược lại giữ lời nhắc ở terminal/web.
   Không tự duyệt khi dev chưa yêu cầu trong cuộc trò chuyện này, không dùng `!`.
