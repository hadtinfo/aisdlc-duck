# Security Policy

## Supported versions

Until 1.0.0, only the latest release receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue, discussion or PR for a security problem.

Report it privately through GitHub:
[Report a vulnerability](https://github.com/hadtinfo/aisdlc-duck/security/advisories/new).

Include what you can of:

- the affected component (CLI/hooks, harness, plugin, web server, web UI) and version or commit;
- steps to reproduce or a proof of concept;
- the impact you expect (for example: an agent bypassing a gate or step scope, a token gaining rights it should not
  have, forging the signed cache, auth bypass on the web server).

We aim to acknowledge reports within 5 working days and to agree on a disclosure date with you once a fix is ready.
We credit reporters in the advisory unless you ask us not to.

## Scope

The threat model is described under "Safety model" in the [README](README.md): the agent is assumed to make
mistakes, not to attack deliberately, and the Bash write checks are a heuristic. Known bypasses listed in section 8.1
of the [user guide](docs/user-guide.md) are not vulnerabilities on their own. A new way around the hooks that defeats
gates or scopes without the developer noticing is in scope, and so is anything affecting the web server, tokens or
signing keys.
