---
description: Code một bước Sx theo thiết kế, chỉ trong phạm vi bước
argument-hint: "<Sx, ví dụ S1>"
---
Làm bước `$ARGUMENTS`.

1. Chạy `aisdlc step start $ARGUMENTS`. Đọc scope và các case của bước.
2. Chỉ sửa file trong scope của bước. Cần file ngoài scope thì dừng và nhờ dev chạy `aisdlc scope allow <path>` ở terminal riêng; nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm `/aisdlc:accept --file <path>` (hoặc web).
3. Viết code **tối thiểu** để các case của bước pass. Không thêm abstraction ngoài thiết kế, không viết comment/javadoc dài, không tạo file ngoài scope, không refactor code cũ.
4. Xong thì chạy `aisdlc step finish $ARGUMENTS`.
5. Dán **nguyên văn** tóm tắt của lệnh cho dev rồi **dừng**.
6. Nếu case fail và dev muốn sửa tiếp: chạy lại `aisdlc step start $ARGUMENTS` để làm tiếp đúng bước đó (bước đang chờ dev được phép mở lại), sửa, rồi `aisdlc step finish $ARGUMENTS`.
7. Dev chấp nhận bước bằng `aisdlc step accept $ARGUMENTS` ở terminal riêng. Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm `/aisdlc:accept $ARGUMENTS` (hoặc web). Không tự chạy khi dev chưa yêu cầu trong cuộc trò chuyện này.
