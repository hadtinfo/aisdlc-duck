---
description: Draft brd.md from the ticket and submit the BRD gate / Soạn brd.md từ ticket và nộp cổng BRD
argument-hint: "[extra notes]"
---
Write and submit the `brd` gate of the current feature. Dev notes (if any): $ARGUMENTS

1. Run `aisdlc status` for the feature path and gate status. Its first line is `Locale: vi|en`: write `brd.md` and reply to the dev in that language (`vi` = Vietnamese, `en` = English).
2. Read the feature's `feedback.md` if present and address every comment from the dev.
3. Use only the sections in the **template** (`brd.md` as `aisdlc init` copied it, headings in the repo's language); add no section, do not copy the ticket. At most 5 lines per section (except the `Assumptions` section); drop sections that do not apply.
   Sections are named here by their en heading; a vi repo uses the vi heading (`Assumptions` = `Giả định`, `Open questions` = `Câu hỏi mở`).
4. Missing information: ask the dev or write it under `Open questions`. Never invent anything.
   Business rules are numbered `BR-1`, `BR-2`... (one ID per line).
   `Assumptions`: list every choice (data type, unit, time zone, default value, external-system error handling, locking/transactions) the BRD/ticket does not state, one line each `- <decision>. (<source>)`; source is `own choice`, `inferred from BRD`, `answered open question` or `out of scope` (vi: `tự chọn`, `suy ra từ BRD`, `câu hỏi mở đã trả lời`, `ngoài phạm vi`).
   (BRD: `Open questions` keeps the unclear points from `/aisdlc:init`.)
5. Run `aisdlc submit brd`. Fix the findings and resubmit until there are none.
6. **Stop.** Tell the dev: "Before approving, `aisdlc review brd` prints the review card (red flags, assumptions, summary). Approve with `aisdlc approve brd` in a separate terminal (the command needs a real console)."
   If `aisdlc status` shows approval inside Claude Code as on (`Approval inside Claude Code: on` / `Duyệt trong Claude Code: bật`), add: "or type `/aisdlc:approve brd` to approve inside Claude Code (shows a confirmation box), or approve on the web"; otherwise keep the terminal/web reminder.
   Never approve unless the dev asked for it in this conversation; do not use `!`.
