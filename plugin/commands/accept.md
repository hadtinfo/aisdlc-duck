---
description: Chấp nhận bước Sx hoặc cho phép file ngoài phạm vi ngay trong Claude Code (dev đã yêu cầu)
argument-hint: "<Sx> | --file <path>"
---
Dev yêu cầu: `$ARGUMENTS`. Không tự chạy khi dev chưa yêu cầu trong cuộc trò chuyện này.

1. Chạy `aisdlc status`. Không thấy dòng "Duyệt trong Claude Code: bật" thì nói dev chạy lệnh ở terminal riêng (`aisdlc step accept <Sx>` hoặc `aisdlc scope allow <path>`) hoặc dùng web, rồi dừng.
2. Đối số là `<Sx>` (ví dụ `S1`): dán lại tóm tắt của `aisdlc step finish` cho dev nếu chưa có, rồi chạy **riêng một lệnh** `aisdlc step accept <Sx>`.
3. Đối số là `--file <path>`: chạy **riêng một lệnh** `aisdlc scope allow <path>` (đường dẫn thẳng, không glob, không biến).
4. Mọi lệnh gọi thẳng `aisdlc`, không đường dẫn, không `bash -c`, không ghép lệnh, không song song với tool khác. Claude Code sẽ hiện hộp Yes/No cho dev; Yes thì lệnh chạy, No thì không.
5. Bị deny (chế độ quyền không hỏi người, phiếu hết hạn): nói dev làm ở terminal riêng hoặc web. **Không thử vòng**.
