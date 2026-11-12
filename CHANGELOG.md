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
  `AISDLC_LOCALE` environment variable (a wrong value is ignored) and `aisdlc setup --locale vi|en`. A repo without
  the key stays Vietnamese, byte for byte.
- English templates (`templates/en/`) for `brd.md`, `design.md`, `design.yaml`, `cases.yaml`, `config.yaml`,
  `CLAUDE.md` and the CI job; `aisdlc init` uses the repo locale. The Vietnamese templates are unchanged except for
  the new `locale: vi` line in `config.yaml`.
- The BRD validator accepts English section headings (`Goal`, `In scope`, `Out of scope`, `Business rules`,
  `Acceptance criteria`, `Assumptions`, `Open questions`) as well as the Vietnamese ones.
- `aisdlc status` prints a `Locale:` line. CLI messages, the web UI and the plugin are still Vietnamese; they are
  translated in later waves.
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
