# Design

## Sequence
```mermaid
sequenceDiagram
    participant C as App
    participant A as fixture-service
    C->>A: POST /api/v1/limits/check {accountId, amount}
    A-->>C: 200 {allowed, remaining} / 404 LIMIT_NOT_FOUND
```

## API
- `POST /api/v1/limits/check`, body `{"accountId": "A1", "amount": 200000}`.
- 200 `{"allowed": true, "remaining": 100000}`; 400 / 404 `{"code": "..."}`.

## DB
- `db/00002_daily_limit.sql`: table `daily_limit`, no change to existing tables.

## Assumptions
- One limit row per account (own choice)
