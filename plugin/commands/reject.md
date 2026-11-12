---
description: Send a gate back with a reason inside Claude Code (dev asked), via the permission prompt / Trả lại một cổng kèm lý do ngay trong Claude Code (dev đã yêu cầu), qua hộp xác nhận quyền
argument-hint: "<brd|design|cases|review> <reason>"
---
The dev asked to send a gate back with: `$ARGUMENTS` (first word is the gate, the rest is the reason). Never reject unless the dev asked for it in this conversation.

1. Run `aisdlc status`. Its first line is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English). If it does not show approval inside Claude Code as on (`Approval inside Claude Code: on`, or `Duyệt trong Claude Code: bật` in a vi repo), tell the dev to run `aisdlc reject <gate> -m "<reason>"` in a separate terminal or use the web, then stop.
2. No reason given: ask the dev. Keep it short, one line, without the characters `; & | $ \` < > ( ) * ? { } [ ] ~ #` or quotes (the hook treats them as an indirect command and blocks it), and without the word `eval` (also blocked as an indirect command).
3. Run `aisdlc review <gate>` so the dev can see the review card again.
4. Run **a single command** `aisdlc reject <gate> -m "<reason>"`: call `aisdlc` directly, no path, no variable, no `bash -c`, no chained commands, not in parallel with another tool. Claude Code shows the dev a Yes/No box.
5. Yes: the gate is sent back (the reason goes to `feedback.md`); report the result. No: nothing is sent back; stop.
6. Denied (a permission mode that does not ask a human, or an expired permit): tell the dev to do it in a separate terminal or on the web. **Do not retry around it**.
