# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/). Before 1.0.0, minor versions may contain breaking changes.

## [Unreleased]

### Added

- Project site on GitHub Pages (`site/`, English and Vietnamese) with a guided tour and screenshots, assembled by
  `make site` and published by the `Pages` workflow.
- `aisdlc setup` creates a git repository (`git init -b main`) when the directory is not inside one yet, so
  `aisdlc init` works right away in a new project; the remote can be added later.

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
