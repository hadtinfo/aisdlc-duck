# BRD

## Mục tiêu
App chặn sớm giao dịch chuyển tiền vượt hạn mức ngày của tài khoản, trước khi gửi lệnh chuyển.

## Trong phạm vi
- API `POST /api/v1/limits/check` nhận `accountId`, `amount`; trả `allowed`, `remaining`.
- Bảng `daily_limit`: hạn mức ngày và số đã dùng của từng tài khoản.

## Ngoài phạm vi
- Cộng số đã dùng sau giao dịch (luồng chuyển tiền làm).
- Reset số đã dùng đầu ngày.

## Quy tắc nghiệp vụ
- Thiếu `accountId` hoặc `amount` ≤ 0: trả 400, `code` = `VALIDATION_ERROR`.
- Còn lại = hạn mức − đã dùng. `amount` ≤ còn lại: `allowed` = true, `remaining` = còn lại − `amount`.
- `amount` > còn lại: `allowed` = false, `remaining` = còn lại; không đổi số đã dùng.
- Tài khoản chưa có hạn mức: trả 404, `code` = `LIMIT_NOT_FOUND`.

## Tiêu chí chấp nhận
- Hạn mức 1.000.000, đã dùng 700.000: kiểm 200.000 → cho phép, còn 100.000.
- Kiểm 300.000 → cho phép, còn 0; kiểm 300.001 → từ chối, còn 300.000.

## Câu hỏi mở
- [x] API kiểm tra có trừ hạn mức không? → Không, chỉ kiểm tra.
