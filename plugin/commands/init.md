---
description: Triage a ticket and create a new AISDLC feature (full or lite flow) / Triage ticket và tạo tính năng AISDLC mới (luồng đầy đủ hoặc nhẹ)
argument-hint: "<JIRA-KEY>"
---
Create feature `$ARGUMENTS` (Jira key).

0. Run `aisdlc status`. Its first line is `Locale: vi|en`: write documents and reply to the dev in that language (`vi` = Vietnamese, `en` = English). Before init it reports no active feature and exits non-zero; that is expected, only the `Locale:` line matters here.
1. Read the ticket. Without Jira, ask the dev to paste the ticket text.
2. Propose the **full flow** or the **lite flow** with a one-line reason. Use the lite flow only for a bug fix or a small change that touches neither the DB nor an API.
3. List at most 5 unclear points in the ticket.
4. **Ask the dev to choose the flow** and wait for the answer. Never choose yourself.
5. Once the dev chooses, run `aisdlc init <KEY>` (full flow) or `aisdlc init <KEY> --lite` (lite flow).
6. Unclear points:
   - Full flow: write them under the open questions section of `brd.md` (`Open questions`, or `Câu hỏi mở` in a vi repo; keep the heading the template has).
   - Lite flow: restate them to the dev; do not write a file.
7. Run `aisdlc status` and report the next gate to work on.
