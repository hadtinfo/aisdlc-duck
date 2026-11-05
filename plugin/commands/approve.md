---
description: Duyệt một cổng ngay trong Claude Code (dev đã yêu cầu), qua hộp xác nhận quyền
argument-hint: "<brd|design|cases|review>"
---
Dev yêu cầu duyệt cổng `$ARGUMENTS`. Không tự duyệt khi dev chưa yêu cầu trong cuộc trò chuyện này.

1. Chạy `aisdlc status`. Không thấy dòng "Duyệt trong Claude Code: bật" thì nói dev duyệt ở terminal riêng (`aisdlc approve $ARGUMENTS`) hoặc trên web, rồi dừng.
2. Chạy `aisdlc review $ARGUMENTS` để in review card (cờ đỏ, giả định, tóm tắt) và đưa cho dev xem.
3. Chạy **riêng một lệnh** `aisdlc approve $ARGUMENTS`: gọi thẳng `aisdlc`, không đường dẫn, không biến, không `bash -c`, không ghép `&&`/`;`/`|`, không song song với tool khác (phiếu cho phép bị xóa khi có tool khác chạy giữa chừng). Claude Code sẽ hiện hộp Yes/No cho dev.
4. Dev chọn Yes thì lệnh duyệt xong: báo kết quả. Dev chọn No thì không duyệt, báo lại và dừng (lần sau sẽ hỏi lại).
5. Lệnh bị deny (chế độ quyền không hỏi người như bypass/dontAsk/auto, hoặc hết hạn phiếu): nói dev duyệt ở terminal riêng hoặc web. **Không thử vòng**, không chạy lại bằng dạng khác.
