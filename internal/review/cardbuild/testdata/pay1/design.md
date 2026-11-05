# Thiết kế

## Sequence
```mermaid
sequenceDiagram
    participant C as App
    participant A as fixture-service
    participant D as DB
    C->>A: POST /api/v1/limits/check {accountId, amount}
    Note over A: S1 kiểm tra request (400 VALIDATION_ERROR)
    A->>D: S2 đọc limit_amount, used_amount từ daily_limit
    Note over A: S2 so amount với hạn mức còn lại
    A-->>C: 200 {allowed, remaining} / 404 LIMIT_NOT_FOUND
```

## API
- `POST /api/v1/limits/check`, body `{"accountId": "A1", "amount": 200000}`.
- 200 `{"allowed": true, "remaining": 100000}`; 400 / 404 `{"code": "..."}`.

## DB
- `db/00002_daily_limit.sql`: bảng `daily_limit` (khóa `account_id` → `account`), không sửa bảng cũ.

## Câu hỏi mở
- [x] Có cần khóa dòng khi đọc? → Không, API chỉ đọc.
