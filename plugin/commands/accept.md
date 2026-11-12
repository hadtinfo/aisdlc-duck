---
description: Accept step Sx or allow an out-of-scope file inside Claude Code (dev asked) / Chấp nhận bước Sx hoặc cho phép file ngoài phạm vi ngay trong Claude Code (dev đã yêu cầu)
argument-hint: "<Sx> | --file <path>"
---
The dev asked: `$ARGUMENTS`. Never run this unless the dev asked for it in this conversation.

1. Run `aisdlc status`. Its first line is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English). If it does not show approval inside Claude Code as on (`Approval inside Claude Code: on`, or `Duyệt trong Claude Code: bật` in a vi repo), tell the dev to run the command in a separate terminal (`aisdlc step accept <Sx>` or `aisdlc scope allow <path>`) or use the web, then stop.
2. Argument `<Sx>` (e.g. `S1`): paste the summary of `aisdlc step finish` for the dev if not done yet, then run **a single command** `aisdlc step accept <Sx>`.
3. Argument `--file <path>`: run **a single command** `aisdlc scope allow <path>` (a plain path, no glob, no variable).
4. Call `aisdlc` directly: no path, no `bash -c`, no chained commands, not in parallel with another tool. Claude Code shows the dev a Yes/No box; Yes runs the command, No does not.
5. Denied (a permission mode that does not ask a human, or an expired permit): tell the dev to do it in a separate terminal or on the web. **Do not retry around it**.
