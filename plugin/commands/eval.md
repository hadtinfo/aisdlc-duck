---
description: Run tier-1 eval (build, unit tests, Docker and the full case suite) / Chạy eval tầng 1 (build, unit test, Docker và bộ case đầy đủ)
argument-hint: ""
---
Run `aisdlc status` first. Its first line is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English).

1. Run `aisdlc eval`. It runs `go build`, `go test` (unit tests), starts Postgres (and Kafka if configured) in Docker, then runs the feature's whole case suite. Docker must work; without it the harness reports a startup error (e.g. "cannot start Postgres: ..." / "không khởi động được Postgres: ...") and every case is ERROR. Copy that error verbatim for the dev; do not try to fix the environment yourself.
2. Summarize the result briefly: PASS or FAIL.
3. Results are in `aisdlc/features/<KEY>/eval/report.json`, `report.md`, `diff-by-step.md`; the harness log is `.aisdlc/run/<KEY>/harness.log`. Read them when you need details.
4. If it fails, list each failed hard check (check name and main cause). Do not change code outside the open step.
