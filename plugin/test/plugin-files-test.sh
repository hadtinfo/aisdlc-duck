#!/usr/bin/env bash
# Kiểm tra file plugin AISDLC. Chạy từ gốc repo: bash plugin/test/plugin-files-test.sh
set -u
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
P="$ROOT/plugin"
fail=0
ok() { echo "ok   - $1"; }
ko() { echo "FAIL - $1"; fail=1; }
check() { if eval "$2" >/dev/null 2>&1; then ok "$1"; else ko "$1"; fi; }

for f in "$P/.claude-plugin/plugin.json" "$P/hooks/hooks.json" "$ROOT/.claude-plugin/marketplace.json"; do
  check "JSON hợp lệ: ${f#$ROOT/}" "python3 -m json.tool '$f'"
done
check "plugin.json name=aisdlc" "python3 -c \"import json,sys;d=json.load(open('$P/.claude-plugin/plugin.json'));assert d['name']=='aisdlc' and d['version']=='0.1.0'\""
check "hooks.json có 5 sự kiện" "python3 -c \"import json;h=json.load(open('$P/hooks/hooks.json'))['hooks'];assert set(h)=={'SessionStart','PreToolUse','UserPromptSubmit','PostToolUse','Stop'}\""
check "hooks.json: UserPromptSubmit gọi hook user-prompt-submit" "python3 -c \"import json;p=json.load(open('$P/hooks/hooks.json'))['hooks']['UserPromptSubmit'];assert len(p)==1 and 'matcher' not in p[0] and p[0]['hooks'][0]['command'].endswith('hook user-prompt-submit')\""
check "hooks.json: PostToolUse gọi hook post-tool-use cho mọi tool (không matcher), timeout 5" "python3 -c \"import json;p=json.load(open('$P/hooks/hooks.json'))['hooks']['PostToolUse'];h=p[0];assert len(p)==1 and 'matcher' not in h and h['hooks'][0]['command'].endswith('hook post-tool-use') and h['hooks'][0]['timeout']==5\""
check "hooks.json: PreToolUse ghi/Bash giữ nguyên" "python3 -c \"import json;h=json.load(open('$P/hooks/hooks.json'))['hooks']['PreToolUse'][0];assert h['hooks'][0]['command'].endswith('hook pre-tool-use') and h['matcher']=='Edit|Write|MultiEdit|NotebookEdit|Bash'\""
check "hooks.json: PreToolUse Read|Grep|Glob gọi hook pre-tool-use (luật chặn thư mục cấu hình web)" "python3 -c \"import json;h=json.load(open('$P/hooks/hooks.json'))['hooks']['PreToolUse'][1];assert h['hooks'][0]['command'].endswith('hook pre-tool-use') and h['matcher']=='Read|Grep|Glob' and h['hooks'][0]['timeout']==15\""
check "marketplace liệt kê plugin aisdlc" "python3 -c \"import json;d=json.load(open('$ROOT/.claude-plugin/marketplace.json'));assert d['plugins'][0]['name']=='aisdlc' and d['plugins'][0]['source']=='./plugin'\""

for c in init brd design cases step eval review status approve reject accept; do
  f="$P/commands/$c.md"
  check "command $c: frontmatter description + argument-hint" "head -1 '$f' | grep -q '^---\$' && grep -q '^description: ' '$f' && grep -q '^argument-hint: ' '$f'"
  check "command $c: thân <= 25 dòng" "[ \$(awk 'BEGIN{n=0} /^---\$/{n++; next} n>=2' '$f' | grep -c .) -le 25 ]"
done
check "agent aisdlc-reviewer" "grep -q '^name: aisdlc-reviewer' '$P/agents/aisdlc-reviewer.md' && grep -q '^tools: Read, Grep, Glob' '$P/agents/aisdlc-reviewer.md'"
for id in sequence error-handling over-engineering naming security; do
  check "agent có tiêu chí $id" "grep -q '$id' '$P/agents/aisdlc-reviewer.md'"
done
check "bin/aisdlc thực thi được" "[ -x '$P/bin/aisdlc' ]"
check "install.sh thực thi được" "[ -x '$P/install.sh' ]"
check "install.sh không còn nhắc Maven/JDK" "! grep -qi 'mvn\|jar\|jdk' '$P/install.sh' '$P/bin/aisdlc'"

# Cần binary Go cho máy này (plugin/lib/aisdlc-<os>-<arch>): build nếu thiếu (lib/ nằm trong .gitignore)
OS="$(uname -s | tr 'A-Z' 'a-z')"
case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) ARCH="$(uname -m)" ;; esac
NAME="aisdlc-$OS-$ARCH"
if [ ! -x "$P/lib/$NAME" ] && command -v go >/dev/null 2>&1; then
  mkdir -p "$P/lib"; (cd "$ROOT" && CGO_ENABLED=0 go build -o "$P/lib/$NAME" ./cmd/aisdlc)
fi
check "bin/aisdlc --help chạy được" "'$P/bin/aisdlc' --help | grep -q 'Usage:'"
check "bin/aisdlc --version" "'$P/bin/aisdlc' --version | grep -q '^aisdlc 0.1.0-SNAPSHOT\$'"
out="$(echo '{"tool_name":"Read","cwd":"/tmp"}' | "$P/bin/aisdlc" hook pre-tool-use 2>&1)"
if [ -z "$out" ]; then ok "hook pre-tool-use với Read ở /tmp in rỗng"; else ko "hook pre-tool-use in: $out"; fi

check "hooks.json: cả 5 sự kiện có timeout" "python3 -c \"import json;h=json.load(open('$P/hooks/hooks.json'))['hooks'];assert all('timeout' in v[0]['hooks'][0] for v in h.values())\""
for c in brd design; do check "command $c: giới hạn ngắn" "grep -q '5 dòng' '$P/commands/$c.md' && grep -q 'Câu hỏi mở' '$P/commands/$c.md'"; done
check "command design: miễn giới hạn 5 dòng cho bảng API và Giả định" "grep -q 'trừ bảng \`API\` và mục \`Giả định\`' '$P/commands/design.md'"
check "command brd: miễn giới hạn 5 dòng cho Giả định, không nhắc API" "grep -q 'trừ mục \`Giả định\`' '$P/commands/brd.md' && ! grep -q 'bảng \`API\`' '$P/commands/brd.md'"
check "command cases: tối đa 15 case" "grep -q '15 case' '$P/commands/cases.md'"
check "command cases: reset_sql/given.sql chỉ dùng bảng đã có ở bước của case" "grep -q 'reset_sql' '$P/commands/cases.md' && grep -q 'bước sau' '$P/commands/cases.md'"
check "command step: không refactor, không comment dài" "grep -q 'refactor' '$P/commands/step.md' && grep -q 'comment dài' '$P/commands/step.md'"
check "command review: bỏ rào \`\`\` và dùng diff-by-step của eval" "grep -q 'rào' '$P/commands/review.md' && ! grep -q 'aisdlc diff' '$P/commands/review.md'"

check "commands và agents không còn nhắc Java/Maven" "! grep -rniE 'mvn|maven|junit|jacoco|flyway|src/main/java|pom\\.xml|AisdlcCasesTest|javadoc' '$P/commands' '$P/agents'"
for c in cases step; do
  check "command $c: quy tắc so DB (số, bool, null, RFC 3339 UTC, json/jsonb, text)" "grep -q 'RFC 3339 UTC' '$P/commands/$c.md' && grep -q 'json/jsonb' '$P/commands/$c.md' && grep -q 'true' '$P/commands/$c.md' && grep -q 'null' '$P/commands/$c.md' && grep -q 'dạng text' '$P/commands/$c.md'"
  check "command $c: quy ước tên goose" "grep -q 'NNNNN_ten.sql' '$P/commands/$c.md' && grep -q -- '-- +goose Up' '$P/commands/$c.md'"
done
check "command step: viết Go" "grep -q 'Go' '$P/commands/step.md' && grep -q 'gofmt' '$P/commands/step.md'"
check "command eval: build, unit test, Docker và bộ case" "grep -q 'go build' '$P/commands/eval.md' && grep -q 'go test' '$P/commands/eval.md' && grep -q 'Docker' '$P/commands/eval.md' && grep -q 'Kafka' '$P/commands/eval.md' && grep -q 'bộ case' '$P/commands/eval.md'"
check "command cases: nêu tập con khóa stub" "grep -q 'bodyPatterns' '$P/commands/cases.md' && grep -q 'jsonBody' '$P/commands/cases.md'"
check "agent reviewer: tiêu chí Go" "grep -q 'context' '$P/agents/aisdlc-reviewer.md' && grep -q 'defer' '$P/agents/aisdlc-reviewer.md' && grep -q 'database/sql\|pgx' '$P/agents/aisdlc-reviewer.md'"

# Launcher khi thiếu binary (lib/ rỗng)
T="$(mktemp -d)"; mkdir -p "$T/bin" "$T/lib" "$T/plain" "$T/repo/aisdlc/sub"; cp "$P/bin/aisdlc" "$T/bin/aisdlc"; touch "$T/repo/aisdlc/config.yaml"
W="$T/bin/aisdlc"
run() { # dir stdin args... -> đặt out, err, rc
  local d="$1" in="$2"; shift 2
  err="$(cd "$d" && echo "$in" | "$@" 2>&1 >"$T/out")"; rc=$?; out="$(cat "$T/out")"
}
expect() { # tên rc_mong_đợi [grep]
  if [ "$rc" -eq "$2" ] && [ -z "$out" ] && { [ -z "${3:-}" ] && [ -z "$err" ] || { [ -n "${3:-}" ] && echo "$err" | grep -q "$3"; }; }; then ok "$1"; else ko "$1: rc=$rc out=$out err=$err"; fi
}
run "$T/plain" '{}' "$W" hook pre-tool-use; expect "thiếu binary, không phải repo AISDLC: pre-tool-use rc 0 im lặng" 0
run "$T/plain" '{}' "$W" hook stop; expect "thiếu binary, không phải repo AISDLC: stop rc 0 im lặng" 0
run "$T/plain" '{}' "$W" hook session-start; expect "thiếu binary, không phải repo AISDLC: session-start rc 0 im lặng" 0
run "$T/repo" '{}' "$W" hook pre-tool-use; expect "thiếu binary, repo AISDLC: pre-tool-use rc 2 (chặn)" 2 "không tìm thấy binary aisdlc cho $OS/$ARCH"
run "$T/repo/aisdlc/sub" '{}' "$W" hook stop; expect "thiếu binary, repo AISDLC (thư mục con): stop rc 1 (không chặn)" 1 'plugin/install.sh'
run "$T/repo" '{}' "$W" hook session-start; expect "thiếu binary, repo AISDLC: session-start rc 1" 1 'plugin/install.sh'
run "$T/plain" "{\"cwd\":\"$T/repo\"}" "$W" hook pre-tool-use; expect "thiếu binary: cwd lấy từ stdin JSON" 2 'plugin/install.sh'
run "$T/plain" '{}' "$W" status; expect "thiếu binary, lệnh thường: rc 2 kèm lỗi" 2 'plugin/install.sh'
for tn in Read Grep Glob LS NotebookRead; do
  run "$T/repo" "{\"tool_name\":\"$tn\",\"cwd\":\"$T/repo\"}" "$W" hook pre-tool-use; expect "thiếu binary, repo AISDLC: pre-tool-use $tn rc 0 (cho đọc)" 0
done
for tn in Edit Write MultiEdit NotebookEdit Bash; do
  run "$T/repo" "{\"tool_name\":\"$tn\",\"cwd\":\"$T/repo\"}" "$W" hook pre-tool-use; expect "thiếu binary, repo AISDLC: pre-tool-use $tn rc 2 (chặn)" 2 'plugin/install.sh'
done
run "$T/repo" '{"tool_name":"Read","tool_input":{"file_path":"/x"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: tool_name Read kèm tool_input vẫn rc 0" 0
run "$T/repo" '{"tool_name":"Write","tool_input":{"content":"x \"tool_name\":\"Read\""},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: tool_name giả trong nội dung ghi không lừa được (Write vẫn rc 2)" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{"tool_name":"Read"},"tool_name":"Write","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: thứ tự khóa đảo (tool_input trước) Write vẫn rc 2" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{"file_path":"/x"},"tool_name":"Read","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: thứ tự khóa đảo, Read rc 0" 0
run "$T/repo" '{"tool_name":"Write","tool_input":{"tool_name":"Read"}}' "$W" hook pre-tool-use; expect "thiếu binary: tool_name lồng trong tool_input bị bỏ qua" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{"tool_name":"Read"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: không có tool_name cấp ngoài (chỉ lồng) -> rc 2" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{},"tool_name":"Write","x":{"tool_name":"Read"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: tool_name Read trong đối tượng khác -> Write rc 2" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{"content":"} {\"tool_name\": \"Read\" {"},"tool_name":"Write","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: dấu ngoặc trong chuỗi không lừa được" 2 'plugin/install.sh'
run "$T/repo" '{"tool_input":{"content":"}{ x"},"tool_name":"Read","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; expect "thiếu binary: Read với dấu ngoặc trong chuỗi vẫn rc 0" 0
big="$(head -c 40000 /dev/zero | tr '\0' a)"
t0=$(date +%s%N); run "$T/repo" '{"tool_name":"Read","tool_input":{"file_path":"'"$big"'"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "$rc" -eq 2 ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary: payload > 16 KB chặn nhanh (${ms} ms)"; else ko "payload lớn: rc=$rc ${ms} ms"; fi
patho="$(for i in $(seq 1 1500); do printf '{"a":"b"},'; done)"
t0=$(date +%s%N); run "$T/repo" '{"tool_input":{"e":['"${patho%,}"']},"tool_name":"Read","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "${#patho}" -lt 16000 ] && [ "$rc" -eq 0 ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary: payload 15 KB nhiều đối tượng nhỏ xử lý nhanh (${ms} ms)"; else ko "payload nhiều đối tượng: rc=$rc ${ms} ms len=${#patho}"; fi
patho="$(for i in $(seq 1 1100); do printf '"k%d":"v",' $i; done)"
t0=$(date +%s%N); run "$T/repo" '{"tool_input":{'"${patho%,}"'},"tool_name":"Write","cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "${#patho}" -lt 16000 ] && [ "$rc" -eq 2 ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary: payload 15 KB nhiều chuỗi nhỏ xử lý nhanh (${ms} ms)"; else ko "payload nhiều chuỗi: rc=$rc ${ms} ms len=${#patho}"; fi
# Đo theo byte, không theo ký tự, kể cả dưới locale UTF-8
U8=C.UTF-8; locale -a 2>/dev/null | grep -qix 'en_US.utf-\?8' && U8=en_US.UTF-8
mb="$(for i in $(seq 1 6000); do printf 'ệ'; done)"
t0=$(date +%s%N); LC_ALL=$U8 run "$T/repo" '{"tool_name":"Read","tool_input":{"file_path":"'"$mb"'"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "$(LC_ALL=$U8; echo ${#mb})" -lt 16384 ] && [ "$(printf %s "$mb" | wc -c)" -gt 16384 ] && [ "$rc" -eq 2 ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary, $U8: payload > 16384 byte (< 16384 ký tự) chặn nhanh (${ms} ms)"; else ko "payload đa byte lớn: rc=$rc ${ms} ms"; fi
mb="$(for i in $(seq 1 4000); do printf 'ệ'; done)"
t0=$(date +%s%N); LC_ALL=$U8 run "$T/repo" '{"tool_name":"Read","tool_input":{"file_path":"'"$mb"'"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "$(printf %s "$mb" | wc -c)" -lt 16384 ] && [ "$rc" -eq 0 ] && [ -z "$err" ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary, $U8: Read đa byte < 16384 byte rc 0 nhanh (${ms} ms)"; else ko "Read đa byte nhỏ: rc=$rc ${ms} ms err=$err"; fi
huge="$(head -c 2000000 /dev/zero | tr '\0' a)"
t0=$(date +%s%N); run "$T/plain" '{"cwd":"'"$T/plain"'","tool_name":"Write","tool_input":{"content":"'"$huge"'"}}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "$rc" -eq 0 ] && [ -z "$err" ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary: payload 2 MB ngoài repo (cwd ở đầu) rc 0 nhanh (${ms} ms)"; else ko "payload 2 MB ngoài repo: rc=$rc ${ms} ms"; fi
t0=$(date +%s%N); run "$T/plain" '{"tool_name":"Write","tool_input":{"content":"'"$huge"'"},"cwd":"'"$T/repo"'"}' "$W" hook pre-tool-use; ms=$(( ($(date +%s%N)-t0)/1000000 ))
if [ "$rc" -eq 2 ] && [ "$ms" -lt 1000 ]; then ok "thiếu binary: payload 2 MB, cwd sau 4 KB đầu -> pre-tool-use chặn nhanh (${ms} ms)"; else ko "payload 2 MB cwd cuối: rc=$rc ${ms} ms"; fi
run "$T/repo" '{"tool_name":"Read","tool_input":{"cwd":"'"$T/plain"'"},"cwd":"'"$T/repo"'"}' "$W" hook stop; expect "thiếu binary: cwd lồng trong tool_input bị bỏ qua" 1 'plugin/install.sh'
run "$T/repo" '{"tool_name":"Read"}' "$W" hook stop; expect "thiếu binary: stop dù tool_name Read vẫn rc 1" 1 'plugin/install.sh'
run "$T/plain" '{"tool_name":"Edit"}' "$W" hook pre-tool-use; expect "thiếu binary, ngoài repo AISDLC: Edit rc 0 im lặng" 0
rm -rf "$T"

# Binary thật: luật thư mục cấu hình web cho tool đọc (Read|Grep|Glob gọi hook pre-tool-use)
H2="$(mktemp -d)"; mkdir -p "$H2/.config/aisdlc"
out="$(printf '{"cwd":"/tmp","tool_name":"Read","tool_input":{"file_path":"%s/.config/aisdlc/web.json"}}' "$H2" | HOME="$H2" "$P/bin/aisdlc" hook pre-tool-use 2>&1)"
if echo "$out" | grep -q '"permissionDecision":"deny"' && echo "$out" | grep -q 'File cấu hình web'; then ok "Read web.json của dev bị chặn"; else ko "Read web.json: $out"; fi
out="$(printf '{"cwd":"/tmp","tool_name":"Read","tool_input":{"file_path":"/proc/self/environ"}}' | HOME="$H2" "$P/bin/aisdlc" hook pre-tool-use 2>&1)"
if echo "$out" | grep -q '"permissionDecision":"deny"' && echo "$out" | grep -q 'biến môi trường'; then ok "Read /proc/self/environ bị chặn"; else ko "Read environ: $out"; fi
out="$(printf '{"cwd":"/tmp","tool_name":"Read","tool_input":{"file_path":"/tmp/x"}}' | HOME="$H2" "$P/bin/aisdlc" hook pre-tool-use 2>&1)"
if [ -z "$out" ]; then ok "Read thường in rỗng"; else ko "Read thường in: $out"; fi
rm -rf "$H2"

[ $fail -eq 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
