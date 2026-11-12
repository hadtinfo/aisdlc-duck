---
description: Eval, independent subagent review and submit the review gate / Eval, review độc lập bằng subagent và nộp cổng review
argument-hint: ""
---
Do these in order; `<KEY>` is the Jira key of the current feature (see `aisdlc status`). The first line of `aisdlc status` is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English).

1. Run `aisdlc eval`. If it fails, tell the dev and stop.
2. Use `aisdlc/features/<KEY>/eval/diff-by-step.md` that `aisdlc eval` just generated (patches included); do not regenerate it.
3. Call the subagent `aisdlc-reviewer` (agent `aisdlc:aisdlc-reviewer`) with **only** the paths `brd.md` (if present), `design.md`, `design.yaml`, `cases.yaml` in `aisdlc/features/<KEY>/` and `eval/diff-by-step.md`, plus the `Locale:` line. Do not summarize how the code was written or why.
4. Write the JSON it returns, verbatim (strip the ``` fence if the subagent wrapped the JSON in a code block, keep only plain JSON), to `aisdlc/features/<KEY>/review-agent.json` (same folder as brd.md, not eval/).
5. Run `aisdlc submit review`. If it reports invalid JSON, call the subagent again and rewrite the file.
6. **Stop.** Tell the dev: "Before approving, `aisdlc review review` prints the review card (red flags, assumptions, summary). Approve with `aisdlc approve review` in a separate terminal."
   If `aisdlc status` shows approval inside Claude Code as on (`Approval inside Claude Code: on` / `Duyệt trong Claude Code: bật`), add: "or type `/aisdlc:approve review` to approve inside Claude Code (shows a confirmation box), or approve on the web". Never approve unless the dev asked for it in this conversation.
