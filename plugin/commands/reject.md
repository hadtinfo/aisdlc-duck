---
description: Trả lại một cổng kèm lý do ngay trong Claude Code (dev đã yêu cầu), qua hộp xác nhận quyền
argument-hint: "<brd|design|cases|review> <lý do>"
---
Dev yêu cầu trả lại cổng với đối số: `$ARGUMENTS` (từ đầu tiên là tên cổng, phần còn lại là lý do). Không tự trả lại khi dev chưa yêu cầu trong cuộc trò chuyện này.

1. Chạy `aisdlc status`. Không thấy dòng "Duyệt trong Claude Code: bật" thì nói dev chạy `aisdlc reject <cổng> -m "<lý do>"` ở terminal riêng hoặc dùng web, rồi dừng.
2. Thiếu lý do thì hỏi dev. Lý do ngắn, một dòng, không chứa các ký tự `; & | $ \` < > ( ) * ? { } [ ] ~ #` hay dấu nháy (hook coi là lệnh gián tiếp và chặn), và không chứa từ `eval` (cũng bị coi là lệnh gián tiếp và chặn).
3. Chạy `aisdlc review <cổng>` để dev xem lại review card.
4. Chạy **riêng một lệnh** `aisdlc reject <cổng> -m "<lý do>"`: gọi thẳng `aisdlc`, không đường dẫn, không biến, không `bash -c`, không ghép lệnh, không song song với tool khác. Claude Code sẽ hiện hộp Yes/No cho dev.
5. Yes thì cổng được trả lại (góp ý vào `feedback.md`): báo kết quả. No thì không trả lại, dừng.
6. Bị deny (chế độ quyền không hỏi người, phiếu hết hạn): nói dev làm ở terminal riêng hoặc web. **Không thử vòng**.
