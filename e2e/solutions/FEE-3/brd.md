# BRD

## Mục tiêu
Phí chuyển tiền được làm tròn tới đồng gần nhất thay vì luôn cắt phần lẻ.

## Trong phạm vi
- API `GET /api/v1/fees?amount=` trả `fee` đã làm tròn.

## Ngoài phạm vi
- Đổi tỷ lệ phí 0,15%.
- Đổi cấu trúc dữ liệu.

## Quy tắc nghiệp vụ
- Phí = `amount` × 0,15%, làm tròn tới đồng gần nhất (0,5 làm tròn lên).
- `amount` âm: trả 400, `code` = `VALIDATION_ERROR`.

## Tiêu chí chấp nhận
- Số tiền 1.000 → phí 2; số tiền 900 → phí 1.
- Số tiền -1 → 400.

## Câu hỏi mở
- [x] Có làm tròn kiểu ngân hàng không? → Không, 0,5 làm tròn lên.
