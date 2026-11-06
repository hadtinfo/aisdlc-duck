#!/usr/bin/env bash
# Kiểm tài liệu: chạy mọi khối ```bash có dòng `# runnable` trong docs/user-guide.md trên repo tạm dựng từ
# e2e/fixture-service, với binary vừa build và HOME tạm. Các khối chạy lần lượt trong cùng một repo. Khối nào lỗi
# thì fail. Cuối cùng soát tài liệu không còn nhắc công cụ Java.
set -euo pipefail
ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
DOC="$ROOT/docs/user-guide.md"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/bin" "$TMP/home" "$TMP/blocks"
(cd "$ROOT" && go build -o "$TMP/bin/aisdlc" ./cmd/aisdlc && go build -o "$TMP/bin/aisdlc-harness" ./cmd/aisdlc-harness)

# Trích các khối bash có đánh dấu.
awk -v dir="$TMP/blocks" '
  /^```bash[ \t]*$/ && !inb { inb = 1; keep = 0; buf = ""; next }
  /^```[ \t]*$/ && inb { inb = 0; if (keep) { n++; f = sprintf("%s/%02d.sh", dir, n); printf "%s", buf > f; close(f) } next }
  inb { buf = buf $0 "\n"; if ($0 ~ /# runnable/) keep = 1 }
' "$DOC"

shopt -s nullglob
blocks=("$TMP"/blocks/*.sh)
if [ "${#blocks[@]}" -eq 0 ]; then
  echo "docs-check: không có khối nào đánh dấu '# runnable' trong $DOC" >&2
  exit 1
fi

# Repo tạm: bản sao fixture, đã commit.
REPO="$TMP/repo"
cp -r "$ROOT/e2e/fixture-service" "$REPO"
(
  cd "$REPO"
  git init -q -b main .
  git add -A
  git -c user.email=docs@example.com -c user.name=docs commit -qm init
)

export HOME="$TMP/home"
export PATH="$TMP/bin:$PATH"
export GIT_CONFIG_GLOBAL="$TMP/gitconfig"
git config --global user.email docs@example.com
git config --global user.name docs
unset AISDLC_SESSION JIRA_URL JIRA_TOKEN AISDLC_WEB_URL AISDLC_WEB_TOKEN

fail=0
for b in "${blocks[@]}"; do
  echo "== chạy khối $(basename "$b")"
  sed 's/^/   | /' "$b"
  if ! (cd "$REPO" && bash -euo pipefail "$b") >"$TMP/out.log" 2>&1; then
    echo "docs-check: khối $(basename "$b") LỖI:" >&2
    cat "$TMP/out.log" >&2
    fail=1
    break
  fi
done
[ "$fail" -eq 0 ] || exit 1

# Không còn nhắc công cụ Java (trừ docs/legacy-java/).
if grep -niE 'mvn|maven|junit|jacoco|flyway|src/main/java|pom\.xml' "$ROOT"/docs/*.md "$ROOT/README.md"; then
  echo "docs-check: tài liệu còn nhắc công cụ Java" >&2
  exit 1
fi
echo "docs-check PASS (${#blocks[@]} khối)"
