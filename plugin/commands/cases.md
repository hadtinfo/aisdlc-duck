---
description: Write concrete cases.yaml and submit the case gate / Soạn cases.yaml cụ thể và nộp cổng case
argument-hint: "[extra notes]"
---
Write and submit the `cases` gate of the current feature. Dev notes (if any): $ARGUMENTS

1. Run `aisdlc status` for the feature path and gate status. Its first line is `Locale: vi|en`: write case names, comments and replies to the dev in that language (`vi` = Vietnamese, `en` = English); YAML keys stay as they are.
2. Read the feature's `feedback.md` if present and address every comment from the dev.
3. The number of cases is about the number of business rules/acceptance criteria in the BRD, plus one error or boundary case per step Sx. At most 15 cases unless the dev asks for more. No speculative flows.
4. A case's `reset_sql` and `given.sql` may only use tables that exist at that case's step. A table created by a later step's migration must not appear in an earlier step's case, nor in `reset_sql`. Clean that table in `given.sql` of a case belonging to the step that creates it.
5. DB comparison rules in `then.db` (`query` + `rows`): numbers compare by value (`100` matches `100.00`); bool is `true`/`false`; NULL is `null`; timestamp and date are RFC 3339 UTC (`2026-01-31T10:00:00Z`, date `2026-01-31`); json/jsonb compare by structure (key order ignored); everything else compares as text. Row counts must match exactly.
   Migrations use goose: `NNNNN_name.sql` (5 digits, e.g. `00002_add_limit_table.sql`) with `-- +goose Up` (required; `-- +goose Down` recommended); drafts go in `aisdlc/features/<KEY>/db/`.
   Stubs support a WireMock subset. `request`: `method`, `url`, `urlPath`, `urlPathPattern`, `queryParameters`, `headers`, `bodyPatterns` (`equalToJson`, `contains`); matchers are `equalTo` or `matches`. `response`: `status`, `headers`, `body`, `jsonBody`, `fixedDelayMilliseconds`. Other keys report `CASES_STUB_UNSUPPORTED`.
6. A case checking a BRD rule sets `rules: [BR-n]`.
   Cases must be **concrete**: real values (never 'a valid value'). Do not write cases that are easy to pass; a case must fail before the code exists (submit checks this).
7. **Risk:** if the feature has `risk.yaml`, read it (read only, never edit) and write every case its level's `requirements` ask for:
   `idempotent` → a duplicate-request case `when.repeat: 2` (or more) for each write endpoint; `concurrent` → a `when.parallel` case ≥ `threads` with `then.statusCounts`; `boundary` → a `kind: boundary` case for each BRD rule tagged `[boundary]` (`[biên]` in vi); `rule-pos-neg` → each rule has a passing case and a failing/boundary case. When the feature writes money or state data (or the level requires `invariant`), the lite flow declares `invariants` in `cases.yaml`: each item `{ name, sql }`, `sql` is a `SELECT`/`WITH` returning violating rows, checked after every case. The full flow declares `invariants` in `design.yaml`, not in `cases.yaml`.
   Lite flow (not classified at the design gate): once `cases.yaml` is done, run `aisdlc risk inputs`, call the subagent `aisdlc-risk-classifier` (agent `aisdlc:aisdlc-risk-classifier`) with **only** the printed input paths, the policy path from the `policy:` line, `inputsHash` and the `Locale:` line, then write its JSON verbatim (strip the ``` fence) to `aisdlc/features/<KEY>/risk-agent.json`. On `RISK_AGENT_MISSING`/`RISK_AGENT_INVALID`, fix the documents, rerun `aisdlc risk inputs` and the subagent; never edit the JSON by hand.
8. Run `aisdlc submit cases`. Fix the findings and resubmit until there are none.
9. **Stop.** Tell the dev: "Before approving, `aisdlc review cases` prints the review card (red flags, assumptions, summary). Approve with `aisdlc approve cases` in a separate terminal (the command needs a real console)."
   If `aisdlc status` shows approval inside Claude Code as on (`Approval inside Claude Code: on` / `Duyệt trong Claude Code: bật`), add: "or type `/aisdlc:approve cases` to approve inside Claude Code (shows a confirmation box), or approve on the web"; otherwise keep the terminal/web reminder.
   Never approve unless the dev asked for it in this conversation; do not use `!`.
