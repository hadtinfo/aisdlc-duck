---
description: Code một bước Sx theo thiết kế, chỉ trong phạm vi bước
argument-hint: "<Sx, ví dụ S1>"
---
Làm bước `$ARGUMENTS`.

1. Chạy `aisdlc step start $ARGUMENTS`. Đọc scope và các case của bước.
2. Chỉ sửa file trong scope của bước. Cần file ngoài scope thì dừng và nhờ dev chạy `aisdlc scope allow <path>` ở terminal riêng; nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm `/aisdlc:accept --file <path>` (hoặc web).
3. Viết code **tối thiểu** để các case của bước pass. Không thêm abstraction ngoài thiết kế, không viết comment dài, không tạo file ngoài scope, không refactor code cũ.
   Viết Go theo thiết kế: chạy `gofmt`, trả lỗi bằng `error` (không `panic`), truyền `context.Context`, dùng câu lệnh SQL có tham số (`$1`, `$2`), không nối chuỗi.
   Migration dùng goose: file `NNNNN_ten.sql` (5 chữ số tăng dần, ví dụ `00002_them_bang_han_muc.sql`), mở đầu bằng `-- +goose Up`, `-- +goose Up` là bắt buộc, `-- +goose Down` nên có. Bản nháp ở `aisdlc/features/<KEY>/db/`; thư mục thư mục migration của repo (`migrations.dir` trong `aisdlc/config.yaml`) chỉ nhận bản đã duyệt theo scope của bước.
   Case so DB trong `then.db` theo quy tắc: số so theo giá trị; bool là `true`/`false`; NULL khớp `null`; timestamp, date dạng RFC 3339 UTC; json/jsonb so theo cấu trúc; còn lại so dạng text. Code ghi DB phải cho ra đúng các giá trị đó.
4. Xong thì chạy `aisdlc step finish $ARGUMENTS`.
5. Dán **nguyên văn** tóm tắt của lệnh cho dev rồi **dừng**.
6. Nếu case fail và dev muốn sửa tiếp: chạy lại `aisdlc step start $ARGUMENTS` để làm tiếp đúng bước đó (bước đang chờ dev được phép mở lại), sửa, rồi `aisdlc step finish $ARGUMENTS`.
7. Dev chấp nhận bước bằng `aisdlc step accept $ARGUMENTS` ở terminal riêng. Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm `/aisdlc:accept $ARGUMENTS` (hoặc web). Không tự chạy khi dev chưa yêu cầu trong cuộc trò chuyện này.
