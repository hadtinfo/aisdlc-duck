<!-- aisdlc -->
## AISDLC

This repo builds features through the flow BRD → Design → Cases → Code step by step → Review.
Feature documents live in `aisdlc/features/<JIRA-KEY>/`; the dev approves every gate.

- Do not edit product code until the design gate and the cases gate are approved (a hook blocks it).
- Do not edit `cases.yaml` once the case suite is locked; to change it the dev runs `aisdlc reopen cases`.
- Do not edit approved artifacts or state files (`gates.json`, `steps.json`, `scope-extra.json`, `feedback.md`, `.aisdlc/`), including through Bash.
- Work one step at a time: `aisdlc step start Sx`, edit only files inside the step scope, then run `aisdlc step finish Sx`, report the result and stop to wait for the dev.
- To edit a file outside the scope: ask the dev to run `aisdlc scope allow <path>`.
- When a gate is sent back: read `feedback.md` and act on it in the next turn.
- Risk: do not fill `impact` in `design.yaml`/`cases.yaml` any more; the subagent `aisdlc-risk-classifier` sets the level (see `aisdlc risk inputs`). `risk.yaml` and `aisdlc/risk-policy.yaml` are read-only; write enough cases to meet the `requirements` in `risk.yaml`.
- Only the dev runs (in their own terminal): `approve`, `reject`, `reopen`, `scope allow`, `step accept`, `risk set`, `risk reclassify`.

- Go service: code lives in `cmd/` (main) and `internal/`; migrations use goose, named `NNNNN_name.sql`, with `-- +goose Up`, main folder `db/migrations/`. Feature drafts go in `aisdlc/features/<KEY>/db/`.
- Self-check each step with `go test ./...` (and `go vet ./...` if the repo enables it). `aisdlc step finish Sx` builds the service and runs the step's cases through the harness (needs Docker); unit tests run in `aisdlc eval`.
- Rules for comparing the DB in `then.db` (cases.yaml): numbers compare by value; bool is `true`/`false`; NULL matches `null`; timestamp and date use RFC 3339 UTC; json/jsonb compare by structure; everything else compares as text.
- Running `aisdlc eval` needs Docker (the harness uses testcontainers for Postgres/Kafka).
- Agents write documents and reply to the dev in English.

Slash commands: `/aisdlc:init`, `/aisdlc:brd`, `/aisdlc:design`, `/aisdlc:cases`, `/aisdlc:step`, `/aisdlc:eval`, `/aisdlc:review`, `/aisdlc:status`.
