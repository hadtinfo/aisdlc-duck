# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Before 1.0.0, minor versions may contain breaking changes.

## [Unreleased]

### Breaking

- Risk policy `schema: 2`: rules are described in words (`{ id, level, describe }`) and judged by an LLM subagent;
  the matching rule kinds `keyword`, `path`, `table`, `endpoint` and `label` and `classification.agent` are gone. A
  `schema: 1` policy stops classification with exit 2: run `aisdlc policy migrate` (prints the diff; `--write` writes
  it) and merge the result, or in web mode publish the draft from **Chuyển sang schema 2** on the policy page. Presets
  `generic` and `banking` v3 are schema 2.
- The `impact` block in `design.yaml` (lite: `cases.yaml`) is no longer used; the validator only warns.
- The BRD gate is always approved by the dev: no provisional risk level at `submit brd` (`Rủi ro tạm (từ khóa)` is
  gone) and `flow.<level>.brd` is ignored with a warning.
- `submit design` (lite: `submit cases`) requires a valid `aisdlc/features/<KEY>/risk-agent.json` whose `inputsHash`
  matches `aisdlc risk inputs` (`RISK_AGENT_MISSING`, `RISK_AGENT_INVALID`). `aisdlc risk reclassify` now applies a
  new `risk-agent.json` instead of recomputing from rules.

### Added

- Locale foundation (English/Vietnamese): the `locale` key (`vi` or `en`) in `aisdlc/config.yaml`, the
  `AISDLC_LOCALE` environment variable (a wrong value is ignored) and `aisdlc setup --locale vi|en`. Inside a repo
  with `aisdlc/config.yaml` the language comes only from its `locale` key (missing or empty means `vi`; the
  environment variable is ignored); `AISDLC_LOCALE` applies only outside such a repo. A repo without the key stays
  Vietnamese, byte for byte. A wrong `locale` value is an error for commands that read the config but no longer
  blocks `--version` or the fail-open hooks.
- English templates (`templates/en/`) for `brd.md`, `design.md`, `design.yaml`, `cases.yaml`, `config.yaml`,
  `CLAUDE.md` and the CI job; `aisdlc init` uses the repo locale. The Vietnamese templates are unchanged except for
  the new `locale: vi` line in `config.yaml`.
- The BRD validator accepts English section headings (`Goal`, `In scope`, `Out of scope`, `Business rules`,
  `Acceptance criteria`, `Assumptions`, `Open questions`) as well as the Vietnamese ones.
- `aisdlc status` always prints a `Locale: vi|en` first line, which the plugin prompts read.
- CLI and hooks in the repo's language: command output, cobra help and usage errors, and hook messages, including
  the block reasons the agent reads (the hook takes the locale of the repo in the `cwd` of its JSON input).
  `aisdlc-harness` receives the locale through `AISDLC_LOCALE`.
- Harness, eval, review card and risk in the repo's language: case messages, `eval/report.md` and eval messages,
  risk policy messages, and the review card (flags and card text). The card has a `locale` field, written only for
  `en`, so Vietnamese card JSON is unchanged; when a gate is resubmitted, flags are compared by code, level and refs, so
  a message that changed only because the locale changed is not a new flag.
- The CLI sends `Accept-Language: vi|en` (from the repo locale) on every web request, and the server answers API
  errors in that language.
- Web UI in English and Vietnamese, chosen per user: an EN/VI switch in the user menu and on the sign-in page. The
  choice is stored in the new column `users.locale` (migration `00002_user_locale.sql`, `NULL` = not chosen), set
  with `PUT /api/me/locale` and returned by `GET /api/me`; before signing in or choosing, the UI uses the browser's
  saved choice, then `navigator.language` (`en*` → English). Server-generated text (inbox, API errors, auto-approval
  labels) follows the viewer. Text already stored in the DB (import reasons, pending-action reasons) stays in
  Vietnamese and is translated when displayed. The server log, startup errors and `aisdlc-web user ...` follow the
  server's `AISDLC_LOCALE`.
- Plugin prompts in English: the `/aisdlc:*` commands and the `aisdlc-reviewer` and `aisdlc-risk-classifier`
  subagents tell the agent to read the `Locale:` line of `aisdlc status` and to write documents and reply to the dev
  in that language; command descriptions are bilingual (`English / Tiếng Việt`).
- Documentation: a "Language / Ngôn ngữ" section in `docs/user-guide.md` and `docs/huong-dan-su-dung.md`; the README
  lists the supported languages; `docs/docs-check.sh` runs each guide on a `vi` repo and an `en` repo.
- Every Vietnamese string literal in Go code must go through `i18n.T`/`Tf` (the bare-string allowlist is gone);
  intentional data lines carry a `// i18n:data <reason>` marker. The web's "not in web mode" 409 error carries
  `"code": "NOT_WEB_MODE"`, which the CLI matches (it still recognises older servers by the message). Reject/reopen
  instructions in `feedback.md` follow the repo locale; the reviewer's own text stays verbatim. E2E adds a lite-flow
  pass on an English repo and an EN/VI switch check in the web UI.
- Project site on GitHub Pages (`site/`, English and Vietnamese) with a guided tour and screenshots, assembled by
  `make site` and published by the `Pages` workflow.
- Subagent `aisdlc-risk-classifier` (read-only): decides which policy rules match a feature's documents, with verbatim
  quotes, and returns the JSON the agent saves as `risk-agent.json`. `/aisdlc:design` and lite `/aisdlc:cases` run it
  before submitting.
- `aisdlc risk inputs`: lists the classifier's input files and the effective policy
  (`.aisdlc/risk/policy-effective.yaml`) and prints `inputsHash`.
- `aisdlc policy migrate [--write]`: converts a `schema: 1` policy to `schema: 2`.
- Web: the policy page shows rule descriptions and offers a schema 2 migration draft for a schema 1 policy (the "try
  classification" tab is removed); the Design (lite: Cases) review card has an "Xếp loại LLM" block, with the WARN flag
  `RISK_LLM_OVERRIDE` when the subagent departs from the rule-based level.
- CI check `RISK_LLM` (`eval --ci`): `llm.inputsHash` in `risk.yaml` must match the current documents and policy; no
  LLM is called. In web mode the CI token may read the live policy (`GET /api/cli/policy`).

## [0.1.0] - 2026-11-06

First public release.

### Added

- Go implementation of the CLI (`aisdlc`), harness (`aisdlc-harness`) and web server (`aisdlc-web`), replacing the
  Java version (see `docs/legacy-java/`).
- Open-source project files: license, contribution guide, security policy, GitHub Actions CI, E2E and release
  workflows.
- English documentation: `README.md`, `docs/user-guide.md`, `docs/web-operations.md`. The Vietnamese versions are
  kept alongside (`README.vi.md`, `docs/huong-dan-su-dung.md`, `docs/van-hanh-web.md`).

### Security

- `github.com/moby/go-archive` upgraded to v0.3.0 (CVE-2026-17106).
- The web image runs as an explicit non-root user.

[Unreleased]: https://github.com/hadtinfo/aisdlc-duck/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hadtinfo/aisdlc-duck/releases/tag/v0.1.0
