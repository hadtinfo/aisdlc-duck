# Contributing to AISDLC

Thanks for your interest. This guide covers how to report problems, propose changes and get a pull request merged.

## Ways to contribute

- **Questions and ideas:** use [GitHub Discussions](https://github.com/hadtinfo/aisdlc-duck/discussions).
- **Bugs:** open an issue with the bug template. Include the version, the commands you ran and the output.
- **Security problems:** do not open a public issue. Follow [SECURITY.md](SECURITY.md).
- **Code and docs:** look for issues labelled `good first issue` or `help wanted`. For anything larger than a small
  fix, open an issue first so we can agree on the approach before you write code.

## Development setup

You need Linux or macOS, Go (version in `go.mod`), Docker, git and Node 22 (for the web UI).

```bash
git clone https://github.com/hadtinfo/aisdlc-duck.git
cd aisdlc-duck
make build          # bin/aisdlc and bin/aisdlc-harness
make test-short     # quick tests, no Docker
make check          # gofmt, vet and the full test suite (needs Docker)
```

Other useful targets:

| Command | When |
| --- | --- |
| `make ui && make ui-test` | You changed `web/ui/` |
| `bash plugin/test/plugin-files-test.sh` | You changed `plugin/` |
| `bash docs/docs-check.sh` | You changed `docs/user-guide.md` (runs every block marked `# runnable`) |
| `make e2e` | You changed gates, steps, hooks, the harness or the web flow (slow; needs `jq`, `curl`, `script`, Node) |

To try your build inside Claude Code, run `plugin/install.sh` and reinstall the plugin. `aisdlc` refuses results from
an `aisdlc-harness` of a different build, so rebuild both binaries together.

## Pull requests

1. Fork the repo and create a branch from `main`.
2. Keep the change focused. One logical change per PR is easier to review and revert.
3. Add or update tests. Bug fixes should come with a test that fails without the fix.
4. Run `make check` (and the other checks above that apply) before pushing.
5. Update `README.md` / `docs/` when behaviour changes, and add an entry under "Unreleased" in `CHANGELOG.md` for
   user-visible changes.
6. Open the PR and fill in the template. CI must be green before review.

PRs are squash-merged, so the PR title becomes the commit message. Use the
[Conventional Commits](https://www.conventionalcommits.org/) style: `feat: ...`, `fix(harness): ...`, `docs: ...`.

### Changes to the safety model

Hooks, gate hashing, step scopes, token types and the Ed25519 signed cache are what keep the agent on the rails (see
"Safety model" in the README). PRs touching them get extra scrutiny. In the description, explain what the change
allows or blocks that was not allowed or blocked before, and add tests for both the allowed and the blocked path.

## Developer Certificate of Origin

Every commit must be signed off to certify that you wrote it or have the right to submit it under the project's
license ([DCO 1.1](https://developercertificate.org/)):

```bash
git commit -s -m "fix: ..."
```

This adds a `Signed-off-by: Your Name <you@example.com>` line. To fix earlier commits on your branch:
`git rebase --signoff main` then force-push the branch.

## Code style

- Go: `gofmt` and `go vet` must be clean. Keep packages under `internal/`. Prefer small functions with a doc comment
  that explains *why*, not *what*.
- TypeScript/React (`web/ui/`): follow the existing structure and keep the build warning-free.
- Shell scripts: `set -euo pipefail`, quote variables, and keep them runnable on both Linux and macOS.

## Language

Documentation, issues and PRs are in English. AISDLC itself runs in English or Vietnamese (see section 2.3 of the
[user guide](docs/user-guide.md)):

- Go: user-facing strings are written in Vietnamese inside `i18n.T` / `i18n.Tf` (web server: the request's
  `Locale`), and the English text goes in the catalog `internal/i18n/en*.go`. The catalog tests fail on a missing or
  unused entry and on Vietnamese literals outside `T`/`Tf`. A line that must keep Vietnamese as data (e.g. a
  regex over Vietnamese document content) gets a `// i18n:data <reason>` comment.
- Web UI: add each key to both `web/ui/src/i18n/vi.ts` and `en.ts` (`tsc` fails on a missing key).
- Templates: change `templates/vi/` and `templates/en/` together.
- Slash-command prompts and subagents (`plugin/`) are written in English and tell the agent to follow the repo's
  `Locale:` line from `aisdlc status`.

## Code of Conduct

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
