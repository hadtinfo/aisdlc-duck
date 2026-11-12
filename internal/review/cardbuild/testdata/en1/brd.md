# BRD

## Goal
The app blocks transfers that exceed the account's daily limit early, before sending the transfer order.

## In scope
- API `POST /api/v1/limits/check` takes `accountId`, `amount`; returns `allowed`, `remaining`.
- Table `daily_limit`: daily limit and used amount per account.

## Out of scope
- Adding to the used amount after a transfer (the transfer flow does it).
- Resetting the used amount at the start of the day.

## Business rules
- Missing `accountId` or `amount` ≤ 0: return 400, `code` = `VALIDATION_ERROR`.
- Remaining = limit − used. `amount` ≤ remaining: `allowed` = true, `remaining` = remaining − `amount`. [boundary]
- `amount` > remaining: `allowed` = false, `remaining` = remaining; the used amount is unchanged.
- Account without a limit: return 404, `code` = `LIMIT_NOT_FOUND`.

## Acceptance criteria
- Limit 1,000,000, used 700,000: check 200,000 → allowed, 100,000 left.

## Assumptions
- Amounts are whole units of the account currency (inferred from BRD)

## Open questions
- [x] Does the check API consume the limit? → No, it only checks.
