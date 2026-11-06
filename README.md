# AISDLC

[![CI](https://github.com/hadtinfo/aisdlc-duck/actions/workflows/ci.yml/badge.svg)](https://github.com/hadtinfo/aisdlc-duck/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A quality-gated feature development workflow for teams building Go services (PostgreSQL, goose migrations) with
Claude Code agents. The agent writes the documents, test cases and code. A developer approves each gate. Plugin hooks
stop the agent from taking shortcuts, and CI re-checks every merge request.

```
BRD ──▶ Design ──▶ Cases ──▶ Code step by step S1…Sn ──▶ Review ──▶ MR
 │         │         │                │                     │
 approve   approve   approve          accept each step      approve (eval must PASS)
```

- **Approval gates:** the agent may only write product code once the earlier gates are approved. Approved artifacts
  are locked by hash; changing one requires the developer to reopen its gate.
- **Step-by-step coding:** each step has a file scope (globs) and a budget (number of files, number of lines) declared
  in the design. Writes outside the scope are blocked unless the developer allows each file individually.
- **Cases before code:** the case suite (HTTP, DB, messages, stubs) is written and locked before any code. At
  submission the suite must contain failing cases. The harness runs cases as a black box against the real service,
  with Postgres (and Kafka if needed) started in Docker.
- **Eval:** build + `go test` + case suite + coverage of new lines + review by a subagent, written to
  `eval/report.md`; a CI job re-runs it on the MR.
- **Review card:** each gate has a summary card with red flags (overlapping scopes, globs touching old migrations,
  changes to existing tables, missing cases…), a step map, an ER diff, an API table and the agent's assumptions.
- **Risk:** the repo's risk policy assigns each feature a level; the higher the level, the stricter the case suite must
  be. Low levels can let the system auto-approve cleanly submitted gates (Review is always approved by hand).
- **Web (optional):** a feature board, live view of the running agent, approving gates on the web, pausing the agent,
  sending instructions, accepting steps / allowing files from the browser.

> **Language note:** the CLI messages, slash-command prompts and web UI are currently in Vietnamese. The documentation
> is in English. Contributions that add English localisation are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md).
> Vietnamese documentation: [README.vi.md](README.vi.md), [docs/huong-dan-su-dung.md](docs/huong-dan-su-dung.md),
> [docs/van-hanh-web.md](docs/van-hanh-web.md).

## Components

| Directory | Contents |
|---|---|
| `cmd/aisdlc` | The `aisdlc` CLI (cobra): commands for the agent and the developer, and also the hook runner. It does not pull in testcontainers, so hooks start fast |
| `cmd/aisdlc-harness` | The harness that `aisdlc` runs as a child process for cases and eval: starts Postgres/Kafka (testcontainers), HTTP stubs, runs the service and `cases.yaml` as a black box |
| `cmd/aisdlc-web` | Web server: API, embedded React UI, embedded goose migrations, user administration (`aisdlc-web user ...`) |
| `internal/` | Shared logic: gates and artifact hashes, steps and scopes, BRD/design/case validators, review card, hook policy, risk, eval, harness, Ed25519 signed envelopes, web |
| `plugin/` | Claude Code plugin: `/aisdlc:*` slash commands, hooks, the `aisdlc-reviewer` subagent, the `bin/aisdlc` launcher, `install.sh` |
| `templates/` | Templates for `config.yaml`, `gitlab-ci.aisdlc.yml`, BRD, design and cases that `aisdlc setup` and `aisdlc init` copy out |
| `web/ui/` | React UI for the web server |
| `e2e/` | End-to-end tests on the sample service `e2e/fixture-service` with solutions `e2e/solutions/PAY-1`, `FIX-2`, `FEE-3` |
| `docs/` | Developer guide, web operations, specs and plans; `docs/legacy-java/` holds documents of the old Java version |

## Requirements

Developer machine: Linux or macOS, Go, Docker (the harness starts Postgres/Kafka), git, Claude Code. Building the web
UI also needs Node. E2E additionally needs `jq`, `curl`, `script`, Node + npm and network access for the first
Playwright install.

## Quick start

1. **Install the plugin on a developer machine**

   ```bash
   git clone https://github.com/hadtinfo/aisdlc-duck.git ~/aisdlc && ~/aisdlc/plugin/install.sh   # builds aisdlc + aisdlc-harness, links ~/.local/bin/aisdlc
   ```

   In Claude Code:

   ```
   /plugin marketplace add ~/aisdlc
   /plugin install aisdlc@aisdlc
   ```

2. **Attach it to a service repo** (in its own MR): run `aisdlc setup` at the repo root, then check the `service` and
   `migrations` sections of `aisdlc/config.yaml` (main package, environment variable names, health check, goose
   directory). Details: section 2.2 of the [user guide](docs/user-guide.md).

3. **Build a feature:** `/aisdlc:init PAY-123` → `/aisdlc:brd` → `/aisdlc:design` → `/aisdlc:cases` →
   `/aisdlc:step S1`… → `/aisdlc:review`. After each stage the developer approves **in a separate terminal**, for
   example `aisdlc approve design` or `aisdlc step accept S1`. These commands require a real console and ask you to
   retype the feature key. The lite flow (`--lite`) for small bug fixes skips BRD and design. A full walkthrough of one
   feature: section 3.4 of the [user guide](docs/user-guide.md).

4. **(Optional) Enable the web server:**

   ```
   docker build -f Dockerfile.web -t aisdlc-web .
   ```

   Run the image with Postgres (variables `AISDLC_DB_URL`, `AISDLC_DB_PASSWORD`, volume `/data`), create a user with
   `aisdlc-web user add`, sign in to the web, create a token on the `/me` page and run `aisdlc web login`. To approve
   on the web, set `approvals: web` in `aisdlc/config.yaml`. See section 6 of the [user guide](docs/user-guide.md) and
   [web operations](docs/web-operations.md).

## Developing this project

| Command | What it does |
| --- | --- |
| `make build` | Builds `bin/aisdlc` (CLI, hooks) and `bin/aisdlc-harness`, embedding the same build version (commit, plus `+dirty` when there are uncommitted changes) |
| `make check` | gofmt, vet, test (needs Docker) |
| `make test-short` | Tests without the parts that need Docker (`AISDLC_SKIP_DOCKER=1`) |
| `make plugin-dist` | Builds `aisdlc` and `aisdlc-harness` for linux/darwin × amd64/arm64 into `plugin/lib/` |
| `make build-web` | Builds the UI (`make ui`) then `bin/aisdlc-web` |
| `make ci` | Runs the CI check and ui stages locally |
| `make e2e` | Full E2E: first repo (PAY-1 FULL, FIX-2 LITE, negative cases), web, risk-level flow, multiple sessions. Logs in `e2e/out/` |
| `make e2e-web` | Web and flow parts only |
| `WEB=0 bash e2e/run-e2e.sh` | E2E without the web |
| `SCREENSHOTS=1 make e2e` | E2E with screenshots of the web pages in `e2e/out/screens` |
| `bash plugin/test/plugin-files-test.sh` | Checks `hooks.json`, the launcher and the plugin files |
| `bash docs/docs-check.sh` | Runs the command blocks marked `# runnable` in the user guide on a temporary repo |

`plugin/lib/` is not tracked in git. After changing the CLI or the harness, re-run `plugin/install.sh` (or
`make plugin-dist`) and reinstall the plugin. `aisdlc` rejects results from an `aisdlc-harness` of a different build.

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## Safety model

We assume the agent can make **mistakes**, not that it is **deliberately attacking**.

- **PreToolUse hook:**
  - blocks writes to state files (`.aisdlc/`, `gates.json`, `steps.json`…) and approved artifacts;
  - blocks writes outside the step scope;
  - blocks any tool touching `~/.config/aisdlc`, where tokens and pinned keys are stored;
  - blocks commands that print environment variables;
  - blocks the agent from running approval commands (unless the repo enables `sessionApprove`; then the developer must
    click Yes in Claude Code).
- **Hooks never call the network.** They only read a cache signed with Ed25519 by the server; the public key is
  pinned on the developer machine.
- **In web approval mode,** approval records live on the server. CI queries the web and FAILS when a gate is not
  approved, or when the code changed after the REVIEW approval.
- **Tokens are typed** (`login`, `ingest`, `approve`, `ci`). Tokens used by the CLI and hooks cannot approve anything.
- Blocking writes via Bash is only a heuristic; GitLab (protected branches + the eval job) is the real enforcement
  layer. Known bypasses: section 8.1 of the [user guide](docs/user-guide.md).

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Documentation

- [Project site](https://hadtinfo.github.io/aisdlc-duck/): a guided tour with screenshots.
- [User guide](docs/user-guide.md): installation, attaching a repo, building a feature end to end, writing cases,
  eval and CI, the web, risk policy, troubleshooting.
- [Web operations](docs/web-operations.md): deploying `aisdlc-web`, environment variables, signing keys, database,
  user administration.
- Port spec: [docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md](docs/superpowers/specs/2026-11-05-aisdlc-go-port-design.md);
  phase plans in [docs/superpowers/plans/](docs/superpowers/plans/) (internal design notes, in Vietnamese).
- Documents of the old version: [docs/legacy-java/](docs/legacy-java/).

## License

[MIT](LICENSE)
