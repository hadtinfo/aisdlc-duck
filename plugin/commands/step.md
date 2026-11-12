---
description: Code one step Sx from the design, within the step's scope only / Code một bước Sx theo thiết kế, chỉ trong phạm vi bước
argument-hint: "<Sx, e.g. S1>"
---
Work on step `$ARGUMENTS`.

0. Run `aisdlc status`. Its first line is `Locale: vi|en`: reply to the dev in that language (`vi` = Vietnamese, `en` = English).
1. Run `aisdlc step start $ARGUMENTS`. Read the step's scope and cases.
2. Edit only files in the step's scope. Needing a file outside the scope: stop and ask the dev to run `aisdlc scope allow <path>` in a separate terminal; if `aisdlc status` shows approval inside Claude Code as on (`Approval inside Claude Code: on` / `Duyệt trong Claude Code: bật`), also mention `/aisdlc:accept --file <path>` (or the web).
3. Write the **minimum** code that makes the step's cases pass. No abstraction beyond the design, no long comments, no file outside the scope, do not refactor existing code.
   Write Go following the design: run `gofmt`, return errors as `error` (no `panic`), pass `context.Context`, use parameterized SQL (`$1`, `$2`), never string concatenation.
   Migrations use goose: file `NNNNN_name.sql` (5 increasing digits, e.g. `00002_add_limit_table.sql`), starting with `-- +goose Up` (required), `-- +goose Down` recommended. Drafts go in `aisdlc/features/<KEY>/db/`; the repo's migration directory (`migrations.dir` in `aisdlc/config.yaml`) takes only approved files per the step's scope.
   Cases compare the DB in `then.db` by these rules: numbers by value; bool is `true`/`false`; NULL matches `null`; timestamp and date as RFC 3339 UTC; json/jsonb by structure; everything else as text. Code that writes the DB must produce exactly those values.
4. When done, run `aisdlc step finish $ARGUMENTS`.
5. Paste the command's summary **verbatim** for the dev, then **stop**.
6. If a case fails and the dev wants to keep fixing: run `aisdlc step start $ARGUMENTS` again to continue that same step (a step waiting for the dev may be reopened), fix, then `aisdlc step finish $ARGUMENTS`.
7. The dev accepts the step with `aisdlc step accept $ARGUMENTS` in a separate terminal. If `aisdlc status` shows approval inside Claude Code as on, also mention `/aisdlc:accept $ARGUMENTS` (or the web). Never run it unless the dev asked for it in this conversation.
