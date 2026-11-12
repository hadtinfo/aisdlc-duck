---
description: Draft design.md, design.yaml and draft migrations, submit the design gate / Soạn design.md, design.yaml và migration nháp, nộp cổng thiết kế
argument-hint: "[extra notes]"
---
Write and submit the `design` gate of the current feature. Dev notes (if any): $ARGUMENTS

1. Run `aisdlc status` for the feature path and gate status. Its first line is `Locale: vi|en`: write the documents and reply to the dev in that language (`vi` = Vietnamese, `en` = English).
2. Read the feature's `feedback.md` if present and address every comment from the dev.
3. Use only the sections in the **template** (`design.md` as `aisdlc init` copied it, headings in the repo's language); add no section, do not copy the ticket. At most 5 lines per section (except the `API` table and the `Assumptions` section); drop sections that do not apply.
   Sections are named here by their en heading; a vi repo uses the vi heading (`Assumptions` = `Giả định`, `Open questions` = `Câu hỏi mở`).
4. Missing information: ask the dev or write it under `Open questions`. Never invent anything.
   Sequence shows only the key steps (about 7 at most); `design.yaml` lists the steps Sx and each step's scope, following the approved BRD.
   `API` section: one line per endpoint `METHOD /path · body {…}` and a table `| Code | When | Body |` (vi: `| Mã | Khi nào | Body |`) for each returned status.
   `Assumptions`: list every choice (data type, unit, time zone, default value, external-system error handling, locking/transactions) the BRD/ticket does not state, one line each `- <decision>. (<source>)`; source is `own choice`, `inferred from BRD`, `answered open question` or `out of scope` (vi: `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời`, `ngoài phạm vi`).
   If the DB changes, write draft goose migrations in the feature's `db/`: named `NNNNN_name.sql`, with `-- +goose Up`.
   **Risk:** `design.yaml` has no risk block; an independent subagent classifies the level in step 5. Do not edit `risk.yaml`, `risk-agent.json` by hand, or `aisdlc/risk-policy.yaml`.
   When the feature writes money or state data, declare `invariants` in `design.yaml`: each item `{ name, sql, step }`, `sql` is one `SELECT`/`WITH` returning the violating rows (0 rows when correct). An invariant on a table created by a step sets `step:` to that step (checked only after the cases of that step and later steps); without `step` it is checked after every case. Do not edit `risk.yaml` or `aisdlc/risk-policy.yaml` (the hook blocks it).
5. **Risk classification** (after `design.md`, `design.yaml` and migrations are done): run `aisdlc risk inputs` (prints the input paths, a `policy:` line and `inputsHash:`). Call the subagent `aisdlc-risk-classifier` (agent `aisdlc:aisdlc-risk-classifier`) with **only** the printed input paths, the policy path from the `policy:` line, `inputsHash` and the `Locale:` line from `aisdlc status`; say nothing else. Write the JSON it returns, verbatim (strip any ``` fence), to `aisdlc/features/<KEY>/risk-agent.json`. Never edit this file by hand.
6. Run `aisdlc submit design`. On `RISK_AGENT_MISSING` or `RISK_AGENT_INVALID`, fix the documents (if needed), rerun `aisdlc risk inputs`, call the subagent again and rewrite the file; do not edit the JSON. Fix other findings and resubmit until there are none. Editing the documents after classification means classifying again (the hash changes).
7. **Stop.** Tell the dev: "Before approving, `aisdlc review design` prints the review card (red flags, assumptions, summary). Approve with `aisdlc approve design` in a separate terminal (the command needs a real console)."
   If `aisdlc status` shows approval inside Claude Code as on (`Approval inside Claude Code: on` / `Duyệt trong Claude Code: bật`), add: "or type `/aisdlc:approve design` to approve inside Claude Code (shows a confirmation box), or approve on the web"; otherwise keep the terminal/web reminder.
   Never approve unless the dev asked for it in this conversation; do not use `!`.
