---
description: Approve a gate inside Claude Code (dev asked), via the permission prompt / Duyệt một cổng ngay trong Claude Code (dev đã yêu cầu), qua hộp xác nhận quyền
argument-hint: "<brd|design|cases|review>"
---
The dev asked to approve gate `$ARGUMENTS`. Never approve unless the dev asked for it in this conversation.

1. Run `aisdlc status`. Its first line is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English). If it does not show approval inside Claude Code as on (`Approval inside Claude Code: on`, or `Duyệt trong Claude Code: bật` in a vi repo), tell the dev to approve in a separate terminal (`aisdlc approve $ARGUMENTS`) or on the web, then stop.
2. Run `aisdlc review $ARGUMENTS` to print the review card (red flags, assumptions, summary) and show it to the dev.
3. Run **a single command** `aisdlc approve $ARGUMENTS`: call `aisdlc` directly, no path, no variable, no `bash -c`, no chaining with `&&`/`;`/`|`, not in parallel with another tool (the permit is cleared when another tool runs in between). Claude Code shows the dev a Yes/No box.
4. Dev picks Yes: the gate is approved; report the result. Dev picks No: nothing is approved; say so and stop (it will ask again next time).
5. The command is denied (a permission mode that does not ask a human such as bypass/dontAsk/auto, or an expired permit): tell the dev to approve in a separate terminal or on the web. **Do not retry around it** and do not rerun it in another form.
