# Operating the AISDLC web server

> Tiếng Việt: [van-hanh-web.md](van-hanh-web.md).

For whoever deploys `aisdlc-web`. How devs use the web: section 6 of [user-guide.md](user-guide.md).

## 1. Components

- Binary `aisdlc-web` (`cmd/aisdlc-web`): HTTP server, embedded React UI, embedded goose migrations. With no arguments it runs the server; `aisdlc-web user ...` manages users (opens no port, needs only the DB).
- `Dockerfile.web` at the repo root: builds the UI (node), builds a static binary (`CGO_ENABLED=0`), runs on distroless (`nonroot`, uid 65532), port 8080, volume `/data`.
- PostgreSQL (tested with version 16).

```bash
docker build -f Dockerfile.web -t aisdlc-web:<phiên bản> .
```

To run without Docker: `make build-web` (builds the UI, then `bin/aisdlc-web`).

## 2. Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `AISDLC_LISTEN` | `:8080` | Listen address. Localhost only: `127.0.0.1:8080` |
| `AISDLC_DB_URL` | `postgres://localhost:5432/aisdlc` | Postgres URL in pgx format (see section 4 on `pool_max_conns`) |
| `AISDLC_DB_USER` | empty | Overrides the user in the URL. If the URL has no user and this is empty, `aisdlc` is used |
| `AISDLC_DB_PASSWORD` | empty | Overrides the password in the URL. Set it from a secret, not in the URL |
| `AISDLC_SIGNING_KEY_FILE` | `/data/signing-key` | Ed25519 signing key file (PEM). Generated automatically if missing (mode 0600) |
| `AISDLC_SIGNING_PREVIOUS_KEY_FILE` | empty | Previous signing key during key rotation: the server signs an endorsement of the new key with the old one so CLIs that pinned the old key switch over automatically |
| `TZ` | `UTC` | Time zone for the daily purge (section 5) |
| `AISDLC_LOCALE` | `vi` | Operator language (`vi` or `en`) of the server log, startup errors and the `aisdlc-web user ...` commands. It does not affect what users see: a browser session uses the user's saved choice, else `Accept-Language`; token requests (CLI/CI) use `Accept-Language`; default `vi` |

`AISDLC_WEB_URL` and `AISDLC_WEB_TOKEN` belong to the CLI/CI, not the server.

## 3. Signing key shared across replicas

The server signs the state cache and control commands with an Ed25519 key; the dev's CLI pins the public key at `aisdlc web login`. So **every replica must use the same key**:

- The server generates a key on first startup if the file does not exist. So you must **mount a secret containing the shared key BEFORE the first startup** of any replica (create a PKCS#8 PEM pair beforehand, or take the first replica's key and put it in the secret), mounted (Kubernetes Secret, Docker secret) at `AISDLC_SIGNING_KEY_FILE`, read-only. Do not let each replica generate its own key: the CLI would see different keys depending on the replica and reject the cache.
- The default `/data` volume only suits a single replica. With several replicas, mount the secret instead of the volume.
- If the file permissions are broader than 0600 the server only logs a warning; if the file is corrupt or unreadable the server refuses to start and does not overwrite it.
- Back up the key. If it is lost, every dev must run `aisdlc web login` again.
- Key rotation: mount the new key at `AISDLC_SIGNING_KEY_FILE` and the old key at `AISDLC_SIGNING_PREVIOUS_KEY_FILE`; keep both until every CLI has synced.

## 4. Database

`AISDLC_DB_URL` is a pgx URL; pool parameters go directly in the URL:

```
postgres://aisdlc-db:5432/aisdlc?sslmode=require&pool_max_conns=20&pool_min_conns=2
```

- `pool_max_conns`: maximum connections **per replica** (pgx default: number of CPUs, minimum 4). The total `replicas × pool_max_conns` must be below Postgres `max_connections` (leave room for admin and migrations).
- Keep the password in `AISDLC_DB_PASSWORD` (from a secret), not in the URL.
- **Migrations run automatically** at startup (goose, embedded in the binary) under a Postgres session advisory lock: replicas starting at the same time migrate one after another instead of racing. No separate migrate step is needed. If a migration fails the server does not start and the log names the failing file.
- The `aisdlc-web user ...` command also migrates before running.
- Migration `00002_user_locale.sql` adds the nullable column `users.locale` (`vi` or `en`; `NULL` = not chosen yet), which stores each user's UI language from the EN/VI switch (`PUT /api/me/locale`). Existing users keep `NULL` and see the UI in the language last picked in that browser, or else the browser's language, until they choose.

## 5. Purging old data

Every day at **03:00 local time** of the process, the server deletes old events. Local time follows `TZ` (e.g. `TZ=Asia/Ho_Chi_Minh`). The binary embeds the time zone database (`time/tzdata`), so `TZ` works even on the distroless image without `/usr/share/zoneinfo`. Without `TZ`, purge runs at 03:00 UTC. Each replica runs its own purge; the delete is idempotent, so duplicate runs are harmless.

## 6. TLS, proxy and cookies

The server speaks plain HTTP. The session cookie has `HttpOnly`, `SameSite=Strict`, and `Secure` only **when TLS terminates at the process itself** (the incoming request is TLS). The server does not read `X-Forwarded-Proto`. Consequence behind a TLS-terminating reverse proxy/ingress: the proxy → server connection is plain HTTP, so the cookie does **not** get `Secure`. Mitigations:

- Allow external access only over HTTPS at the proxy (redirect HTTP to HTTPS, HSTS).
- If the proxy supports it, add the `Secure` attribute to `Set-Cookie` at the proxy.
- Do not expose port 8080 beyond the proxy (`AISDLC_LISTEN=127.0.0.1:8080` or a NetworkPolicy).

## 7. Example deployment

```bash
docker run -d --name aisdlc-web -p 127.0.0.1:8080:8080 \
  -e TZ=Asia/Ho_Chi_Minh \
  -e AISDLC_DB_URL='postgres://aisdlc-db:5432/aisdlc?pool_max_conns=20' \
  -e AISDLC_DB_PASSWORD="$(cat /run/secrets/db-password)" \
  -e AISDLC_SIGNING_KEY_FILE=/secrets/signing-key \
  -v /srv/aisdlc/signing-key:/secrets/signing-key:ro \
  aisdlc-web:<phiên bản>
```

(`docker run` is only for trying a single replica; on Kubernetes use a Deployment and Secrets for `signing-key` and the DB password. Manifests are not part of this repo.)

Check: opening `/` shows the sign-in page; the log has no migration errors; `docker exec aisdlc-web /aisdlc-web user list` prints the user list.

## 8. User administration

Use the binary inside the container (distroless has no shell, so call the binary directly):

```bash
docker exec aisdlc-web /aisdlc-web user add dev "Tên Dev" --email dev@example.com   # prints a one-time login token
docker exec aisdlc-web /aisdlc-web user role dev lead                                # dev | lead
docker exec aisdlc-web /aisdlc-web user login-token dev                              # issue another login token
docker exec aisdlc-web /aisdlc-web user ci-token gitlab                              # CI token, printed once
docker exec aisdlc-web /aisdlc-web user list
```

Tokens are shown exactly once and stored as SHA-256 in the DB; the audit log does not contain tokens. If a token leaks, revoke it on the `/me` page.

## 9. Connecting from dev machines and CI

- Dev: `aisdlc web login` (web URL + ingest token), `aisdlc web status`, `aisdlc web push`, `aisdlc web approve-token`, `aisdlc web migrate`, `aisdlc policy pull`. See section 6 of the user guide.
- CI: variables `AISDLC_WEB_URL` and `AISDLC_CI_TOKEN` (Masked, Protected).

## 10. Common problems

| Symptom | Cause | Fix |
| --- | --- | --- |
| Server exits immediately, "Không nạp được khóa ký …" (could not load signing key) | Corrupt key file, or wrong secret path | Check the file; do not delete a key in use |
| "không kết nối được Postgres" (cannot connect to Postgres) | Wrong URL/password, Postgres not ready | Check `AISDLC_DB_URL`, `AISDLC_DB_PASSWORD`, network |
| "migration … lỗi" (migration failed) | Broken migration or DB at a mismatched version | Read the log, fix the DB; do not manually rerun parts already applied |
| `too many clients` from Postgres | `replicas × pool_max_conns` exceeds `max_connections` | Lower `pool_max_conns` or raise `max_connections` |
| CLI reports the signing key changed | Another replica uses a different key | Mount the same secret on every replica (section 3) |
| Cookie not kept after sign-in | Accessed over HTTP instead of HTTPS at the proxy | Use HTTPS at the proxy (section 6) |
| Purge runs at the wrong time | `TZ` not set | Set `TZ=Asia/Ho_Chi_Minh` |
