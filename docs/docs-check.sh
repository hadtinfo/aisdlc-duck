#!/usr/bin/env bash
# Kiểm tài liệu: chạy mọi khối ```bash có dòng `# runnable` (hoặc `# chạy-được`) trong tài liệu chỉ định trên repo tạm
# dựng từ e2e/fixture-service, với binary vừa build và HOME tạm. Các khối chạy lần lượt trong cùng một repo. Khối nào
# lỗi thì fail. Mỗi tài liệu chạy hai lượt: repo locale vi (config không có khóa locale, như repo cũ) và repo locale en
# (`locale: en`), vì lệnh trong tài liệu phải chạy được với cả hai; sau các khối, `aisdlc status` phải in đúng
# `Locale: <locale>`. Cuối cùng soát tài liệu không còn nhắc công cụ Java.
#
#   docs/docs-check.sh                          # user-guide.md và huong-dan-su-dung.md, mỗi tài liệu cả vi và en
#   docs/docs-check.sh docs/huong-dan-su-dung.md # một tài liệu, cả vi và en
#   docs/docs-check.sh docs/user-guide.md en     # một tài liệu, một locale
set -euo pipefail
ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
if [ $# -ge 1 ]; then DOCS=("$(readlink -f "$1")"); else DOCS=("$ROOT/docs/user-guide.md" "$ROOT/docs/huong-dan-su-dung.md"); fi
case "${2:-}" in
  "") LOCALES=(vi en) ;;
  vi | en) LOCALES=("$2") ;;
  *)
    echo "docs-check: locale phải là vi hoặc en: $2" >&2
    exit 2
    ;;
esac
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/bin"
(cd "$ROOT" && go build -o "$TMP/bin/aisdlc" ./cmd/aisdlc && go build -o "$TMP/bin/aisdlc-harness" ./cmd/aisdlc-harness)
export PATH="$TMP/bin:$PATH"
# Locale chỉ lấy từ config của repo tạm, không từ môi trường của người chạy.
unset AISDLC_SESSION JIRA_URL JIRA_TOKEN AISDLC_WEB_URL AISDLC_WEB_TOKEN AISDLC_LOCALE

total=0
# check_doc DOC LOCALE: chạy các khối của DOC trên một repo tạm mới có locale LOCALE.
check_doc() {
  local doc="$1" loc="$2"
  local w="$TMP/$loc-$(basename "$doc" .md)"
  mkdir -p "$w/home" "$w/blocks"

  # Trích các khối bash có đánh dấu.
  awk -v dir="$w/blocks" '
    /^```bash[ \t]*$/ && !inb { inb = 1; keep = 0; buf = ""; next }
    /^```[ \t]*$/ && inb { inb = 0; if (keep) { n++; f = sprintf("%s/%02d.sh", dir, n); printf "%s", buf > f; close(f) } next }
    inb { buf = buf $0 "\n"; if ($0 ~ /# (runnable|chạy-được)/) keep = 1 }
  ' "$doc"

  shopt -s nullglob
  local blocks=("$w"/blocks/*.sh)
  if [ "${#blocks[@]}" -eq 0 ]; then
    echo "docs-check: không có khối nào đánh dấu '# runnable' hoặc '# chạy-được' trong $doc" >&2
    return 1
  fi

  # Repo tạm: bản sao fixture, đã commit; locale en thì thêm khóa locale vào config có sẵn của fixture.
  local repo="$w/repo"
  cp -r "$ROOT/e2e/fixture-service" "$repo"
  if [ "$loc" = en ]; then printf 'locale: en\n' >>"$repo/aisdlc/config.yaml"; fi
  (
    export HOME="$w/home" GIT_CONFIG_GLOBAL="$w/gitconfig"
    git config --global user.email docs@example.com
    git config --global user.name docs
    cd "$repo"
    git init -q -b main .
    git add -A
    git commit -qm init
  )

  echo "== $(basename "$doc") [locale $loc]"
  local b
  for b in "${blocks[@]}"; do
    echo "== chạy khối $(basename "$b")"
    sed 's/^/   | /' "$b"
    if ! (export HOME="$w/home" GIT_CONFIG_GLOBAL="$w/gitconfig" && cd "$repo" && bash -euo pipefail "$b") >"$w/out.log" 2>&1; then
      echo "docs-check: $(basename "$doc") [locale $loc]: khối $(basename "$b") LỖI:" >&2
      cat "$w/out.log" >&2
      return 1
    fi
  done

  local first
  (cd "$repo" && HOME="$w/home" GIT_CONFIG_GLOBAL="$w/gitconfig" aisdlc status) >"$w/status.log"
  first="$(head -1 "$w/status.log")"
  if [ "$first" != "Locale: $loc" ]; then
    echo "docs-check: $(basename "$doc") [locale $loc]: aisdlc status in '$first', cần 'Locale: $loc'" >&2
    return 1
  fi
  total=$((total + ${#blocks[@]}))
}

for d in "${DOCS[@]}"; do
  for l in "${LOCALES[@]}"; do
    check_doc "$d" "$l"
  done
done

# Không còn nhắc công cụ Java (trừ docs/legacy-java/).
if grep -niE 'mvn|maven|junit|jacoco|flyway|src/main/java|pom\.xml' "$ROOT"/docs/*.md "$ROOT"/README*.md; then
  echo "docs-check: tài liệu còn nhắc công cụ Java" >&2
  exit 1
fi
echo "docs-check PASS (${#DOCS[@]} tài liệu × ${#LOCALES[@]} locale, $total khối)"
