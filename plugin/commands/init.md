---
description: Triage ticket và tạo tính năng AISDLC mới (luồng đầy đủ hoặc nhẹ)
argument-hint: "<JIRA-KEY>"
---
Khởi tạo tính năng `$ARGUMENTS` (Jira key).

1. Đọc nội dung ticket. Nếu không có Jira, yêu cầu dev dán nội dung ticket.
2. Đề xuất **luồng đầy đủ** hay **luồng nhẹ** kèm lý do một dòng. Luồng nhẹ chỉ dùng khi là sửa bug hoặc thay đổi nhỏ, không đổi DB hay API.
3. Liệt kê tối đa 5 điểm chưa rõ của ticket.
4. **Hỏi dev chọn luồng** và dừng chờ trả lời. Không tự chọn.
5. Sau khi dev chọn, chạy `aisdlc init <KEY>` (luồng đầy đủ) hoặc `aisdlc init <KEY> --lite` (luồng nhẹ).
6. Các điểm chưa rõ:
   - Luồng đầy đủ: ghi vào mục `Câu hỏi mở` của `brd.md`.
   - Luồng nhẹ: nêu lại cho dev, không ghi file.
7. Chạy `aisdlc status` và báo cổng kế tiếp cần làm.
