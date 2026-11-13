#!/usr/bin/env bash
# E2E AISDLC (bản Go): luồng FULL (PAY-1) và LITE (FIX-2) trên dịch vụ mẫu Go thật (e2e/fixture-service: chi, pgx,
# goose; harness chạy Postgres bằng testcontainers), git thật, pty thật cho lệnh chỉ dev. Exit 0 khi mọi bước đúng.
# Cần: Go, Docker, git, jq, curl, script (util-linux); WEB=1 cần thêm Node + npm (UI và Playwright, mạng lần đầu).
# Chạy lại nhiều lần được, kể cả hai lần liền: thư mục làm việc tạm, HOME tạm, cổng ngẫu nhiên, container gắn nhãn
# aisdlc-e2e (dọn khi thoát; container sót của lần chạy đã chết được dọn lúc khởi động). Log ở e2e/out/e2e.log
# (giữ lại sau khi chạy); KEEP=1 giữ cả thư mục làm việc.
# WEB=1 (mặc định): chạy server web Go (bin/aisdlc-web) với Postgres `docker run`, kiểm board/event; chạy luồng duyệt
# trên web (approvals: web) trên repo thứ hai local/web-service: token /me, web login, policy pull, migrate + Xác nhận,
# duyệt trên trang B bằng Playwright, trả lại + nộp lại DESIGN, chỉ dẫn, tạm dừng / tiếp tục, độ trễ hook, Accept bước
# và Cho phép file trên trang B, lệnh hết hiệu lực, web chết, cache bị sửa, review, eval --ci với token ci.
# HOME của E2E là thư mục tạm (không đụng ~/.config/aisdlc thật); token không bao giờ vào log.
# WEB=0 bỏ phần web. SCREENSHOTS=1: chụp các trang web ra e2e/out/screens (cần WEB=1).
# E2E_PARTS (mặc định local,web,flow,session,en): chọn nhóm bước. local = repo đầu (PAY-1 FULL, FIX-2 LITE, âm tính,
# DOCKER_HOST sai, fixture bỏ qua SIGTERM); web = repo local/web-service; flow = quy trình theo mức (repo flow-service);
# session = nhiều session song song; en = FIX-2 LITE trên repo locale: en và UI web tiếng Anh. `make e2e-web` chạy E2E_PARTS=web,flow.
# E2E_LATENCY_SLACK_MS (mặc định 0): nới ngưỡng p95 50 ms của hook cho máy CI chậm. E2E_PW_DIR: nơi cài Playwright
# (mặc định ~/.cache/aisdlc-e2e/pw, ngoài repo).
# Chính sách rủi ro: fixture có aisdlc/risk-policy.yaml (schema 2, luật mô tả bằng lời; E1 "Chặn giao dịch theo hạn
# mức" → high; high cần case gửi trùng và bất biến). Không gọi LLM: trước `submit design` (lite: `submit cases`) script
# giả lập subagent aisdlc-risk-classifier bằng risk_agent (inputsHash của `aisdlc risk inputs` thay vào
# e2e/solutions/<KEY>/risk-agent.json). Cổng BRD luôn duyệt tay.
# Quy trình theo mức (flow): repo flow-service có chính sách riêng (e2e/solutions/flow/risk-policy.yaml, cùng luật,
# flow.low tự duyệt Thiết kế/Case/bước). FEE-3 mức thấp: dev duyệt BRD rồi chạy một mạch tới Review; PAY-1 mức high
# nên mọi cổng chờ dev. WEB=0 thì CLI tự duyệt; WEB=1 thì repo ở approvals: web và server tự duyệt.
set -euo pipefail

E2E="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
ROOT="$(dirname "$E2E")"
SOL="$E2E/solutions"
OUTDIR="$E2E/out"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/aisdlc-e2e.XXXXXX")"
REPO="$WORK/fixture-service"
LOG="$OUTDIR/e2e.log"
STEP="khởi động"
WEB="${WEB:-1}"
E2E_PARTS="${E2E_PARTS:-local,web,flow,session,en}"
WEB_PID=""
PG_CTR=""
LABEL="aisdlc-e2e"
# Server web nền của lần chạy ("pid-server pid-script"), để lần chạy sau dọn khi lần này bị SIGKILL (trap không chạy).
WEB_PIDFILE="${TMPDIR:-/tmp}/aisdlc-e2e-web.$(id -u).pids"
LAT_SUMMARY=""

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"
: >"$LOG"
# aisdlc-web sót của lần chạy bị SIGKILL: script chủ đã chết thì giết server.
if [ -f "$WEB_PIDFILE" ]; then
  while read -r wpid owner; do
    if [ -n "$wpid" ] && ! kill -0 "${owner:-0}" 2>/dev/null && grep -qa 'aisdlc-web' "/proc/$wpid/cmdline" 2>/dev/null; then
      kill -9 "$wpid" 2>/dev/null && echo "dọn aisdlc-web sót pid $wpid (script $owner đã chết)" >>"$LOG"
    fi
  done <"$WEB_PIDFILE"
  rm -f "$WEB_PIDFILE"
fi

part() { case ",$E2E_PARTS," in *",$1,"*) return 0 ;; esac; return 1; }

# Token đã tạo trong E2E: không bao giờ được vào $LOG; redact thay chúng trong $OUT bằng *** trước khi ghi log.
# ${SECRETS[@]+...}: mảng rỗng an toàn với set -u ở bash < 4.4.
SECRETS=()
# Không token nào trong $SECRETS được xuất hiện trong $LOG (không in token ra, chỉ báo lỗi).
log_has_secret() {
  local t f
  for f in "$LOG" "$OUTDIR/web.log"; do
    [ -f "$f" ] || continue
    for t in ${SECRETS[@]+"${SECRETS[@]}"}; do [ -n "$t" ] && grep -qF -- "$t" "$f" && return 0; done
  done
  return 1
}

# web_stop: tắt server web (SIGTERM, chờ tối đa 15 giây rồi SIGKILL), xóa file pid.
web_stop() {
  [ -n "$WEB_PID" ] || return 0
  local i
  kill "$WEB_PID" 2>/dev/null || true
  for i in $(seq 1 30); do kill -0 "$WEB_PID" 2>/dev/null || break; sleep 0.5; done
  kill -9 "$WEB_PID" 2>/dev/null || true
  wait "$WEB_PID" 2>/dev/null || true
  WEB_PID=""
  rm -f "$WEB_PIDFILE"
}

# svc_pids: dịch vụ mẫu do harness build (binary <repo>/.aisdlc/run/.../svc) còn chạy, thuộc lần chạy này ($WORK).
svc_pids() {
  local p
  for p in $(pgrep -x svc 2>/dev/null || true); do
    case "$(readlink "/proc/$p/exe" 2>/dev/null)" in "$WORK"/*) echo "$p" ;; esac
  done
  return 0
}

cleanup() {
  local rc=$? leaked=0 p
  trap - EXIT INT TERM
  if log_has_secret; then
    leaked=1
    echo "E2E FAIL: token đã lọt vào $LOG" >&2
    [ $rc -ne 0 ] || rc=1
  fi
  if [ $rc -ne 0 ]; then
    echo "E2E FAIL tại: $STEP" >&2
    # log có token thì không in đuôi log ra terminal
    if [ $leaked = 0 ] && [ -f "$LOG" ]; then echo "--- 60 dòng cuối log ($LOG) ---" >&2; tail -n 60 "$LOG" >&2; fi
  fi
  # tiến trình `aisdlc web flush` nền của lần chạy này (thư mục làm việc dưới $WORK)
  for p in $(flush_pids); do kill "$p" 2>/dev/null || true; done
  # KEEP_WEB=1 (chỉ khi PASS): giữ web + Postgres chạy để xem giao diện.
  if [ -n "${KEEP_WEB:-}" ] && [ $rc -eq 0 ] && [ -n "$WEB_PID" ]; then
    echo "Giữ web (KEEP_WEB=1): $WEB_URL — dọn bằng: kill $WEB_PID; docker rm -f $PG_CTR" >&2
  else
    web_stop
    # mọi container của lần chạy này (Postgres của web, container eval không Docker nếu bị ngắt giữa chừng)
    for p in $(docker ps -aq --filter "label=$LABEL.pid=$$" 2>/dev/null); do docker rm -f -v "$p" >/dev/null 2>&1 || true; done
  fi
  for p in $(svc_pids); do kill -9 "$p" 2>/dev/null || true; done
  if [ -n "${KEEP:-}" ]; then echo "Giữ thư mục (KEEP=1): $WORK" >&2; else chmod -R u+w "$WORK" 2>/dev/null; rm -rf "$WORK"; fi
  exit $rc
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

step() { STEP="$*"; echo "== $*" | tee -a "$LOG"; }
die() { echo "LỖI: $*" | tee -a "$LOG" >&2; exit 1; }

# run <exit mong đợi> <lệnh...>: chạy, lưu output vào $OUT, so exit code.
run() {
  local want=$1; shift
  local rc=0
  OUT="$("$@" 2>&1)" || rc=$?
  redact
  local cmd="$*" t
  for t in ${SECRETS[@]+"${SECRETS[@]}"}; do [ -n "$t" ] && cmd="${cmd//"$t"/***}"; done
  printf '$ %s\n%s\n[exit %s]\n' "$cmd" "$OUT" "$rc" >>"$LOG"
  [ "$rc" -eq "$want" ] || die "'$*' exit $rc, mong đợi $want"
}
redact() { local t; for t in ${SECRETS[@]+"${SECRETS[@]}"}; do [ -n "$t" ] && OUT="${OUT//"$t"/***}"; done; return 0; }
has() { grep -qF -- "$1" <<<"$OUT" || die "output thiếu '$1'"; }
hasnt() { ! grep -qF -- "$1" <<<"$OUT" || die "output không được có '$1'"; }

# Lệnh chỉ dev: chạy trong pty thật (script), dev gõ lại feature key.
dev() { run 0 script -qec "aisdlc $*" /dev/null <<<"$KEY"; }

# Lệnh chỉ dev nhận token qua pty (web login, web approve-token): <input> chứa token, đưa vào stdin của pty. Pty có thể
# dội lại chữ gõ trước khi CLI tắt echo, nên output luôn qua redact trước khi ghi $LOG; dòng lệnh không chứa token.
dev_secret() { # <input> <đối số aisdlc...>
  local input=$1 rc=0
  shift
  OUT="$(printf '%s\n' "$input" | script -qec "aisdlc $*" /dev/null 2>&1)" || rc=$?
  redact
  printf '$ aisdlc %s (input ẩn)\n%s\n[exit %s]\n' "$*" "$OUT" "$rc" >>"$LOG"
  [ "$rc" -eq 0 ] || die "'aisdlc $*' exit $rc, mong đợi 0"
}

# Hook PreToolUse qua wrapper plugin/bin/aisdlc, đúng JSON Claude Code gửi.
hook() { # <tool_name> <tool_input JSON>
  local json
  json="$(printf '{"session_id":"e2e","hook_event_name":"PreToolUse","cwd":"%s","tool_name":"%s","tool_input":%s}' \
    "$REPO" "$1" "$2")"
  run 0 bash -c 'printf "%s" "$1" | aisdlc hook pre-tool-use' _ "$json"
}

# Hook PostToolUse (gửi FILE_EDIT lên web), cùng định dạng JSON Claude Code gửi.
hook_post() { # <tool_name> <tool_input JSON>
  local json
  json="$(printf '{"session_id":"e2e","hook_event_name":"PostToolUse","cwd":"%s","tool_name":"%s","tool_input":%s}' \
    "$REPO" "$1" "$2")"
  run 0 bash -c 'printf "%s" "$1" | aisdlc hook post-tool-use' _ "$json"
}

# Hook UserPromptSubmit (dev gõ prompt ở terminal), cùng định dạng JSON Claude Code gửi.
hook_prompt() { # <prompt>
  local json
  json="$(printf '{"session_id":"e2e","transcript_path":"/dev/null","cwd":"%s","permission_mode":"default","hook_event_name":"UserPromptSubmit","prompt":"%s"}' \
    "$REPO" "$1")"
  run 0 bash -c 'printf "%s" "$1" | aisdlc hook user-prompt-submit' _ "$json"
}

# Hook Stop, cùng định dạng JSON Claude Code gửi.
hook_stop() {
  local json
  json="$(printf '{"session_id":"e2e","hook_event_name":"Stop","cwd":"%s","stop_hook_active":false}' "$REPO")"
  run 0 bash -c 'printf "%s" "$1" | aisdlc hook stop' _ "$json"
}

# hook_ms <pre-tool-use|post-tool-use> <tool_name> <tool_input JSON>: chạy hook thật một lần (wrapper + binary), in
# thời gian (ms). cwd của JSON là $HOOK_CWD nếu đặt (đo nền ngoài repo AISDLC), không thì $REPO.
hook_ms() {
  local ev json t0 t1
  case "$1" in pre-tool-use) ev=PreToolUse ;; *) ev=PostToolUse ;; esac
  json="$(printf '{"session_id":"e2e","hook_event_name":"%s","cwd":"%s","tool_name":"%s","tool_input":%s}' \
    "$ev" "${HOOK_CWD:-$REPO}" "$2" "$3")"
  t0=$(date +%s%N)
  printf '%s' "$json" | aisdlc hook "$1" >/dev/null 2>&1
  t1=$(date +%s%N)
  echo $(( (t1 - t0) / 1000000 ))
}

# pipe_ms <lệnh...>: cùng khuôn đo như hook_ms (printf JSON | lệnh) cho một lệnh bất kỳ: nền đo ngoài sản phẩm.
pipe_ms() {
  local t0 t1
  t0=$(date +%s%N)
  printf '%s' '{"session_id":"e2e"}' | "$@" >/dev/null 2>&1
  t1=$(date +%s%N)
  echo $(( (t1 - t0) / 1000000 ))
}

# stats20 <lệnh hook_ms...>: chạy 20 lần, in "p95 trung-vị (các lần đo)" (p95 = lần thứ 19 sau khi sắp).
stats20() {
  local all=() i sorted
  for i in $(seq 1 20); do all+=("$("$@")"); done
  sorted="$(printf '%s\n' "${all[@]}" | sort -n)"
  printf '%s %s (%s)\n' "$(sed -n 19p <<<"$sorted")" "$(sed -n 10p <<<"$sorted")" "${all[*]}"
}

# flush_pids: PID các tiến trình `aisdlc web flush` nền do lần chạy này mở (thư mục làm việc nằm dưới $WORK).
flush_pids() {
  local p
  for p in $(pgrep -f 'aisdlc[^ ]* web flush' 2>/dev/null || true); do
    case "$(readlink "/proc/$p/cwd" 2>/dev/null)" in "$WORK"/*) echo "$p" ;; esac
  done
}

# wait_no_flush <giây>: chờ tới khi không còn tiến trình gửi nền nào của lần chạy này (quá hạn → die).
wait_no_flush() {
  local i
  for i in $(seq 1 $(( $1 * 2 ))); do
    [ -z "$(flush_pids)" ] && return 0
    sleep 0.5
  done
  die "tiến trình web flush nền vẫn chạy sau $1 giây"
}

# Playwright (e2e/package.json + lock, chromium) cài ngoài repo ở $PW_DIR (mặc định ~/.cache/aisdlc-e2e/pw, giữ qua các
# lần chạy): chép package.json, lock và các .mjs vào đó, `npm ci` khi lock đổi, rồi `playwright install chromium`; node
# chạy .mjs từ $PW_DIR để import thấy node_modules. Trình duyệt và cache npm dùng HOME thật. Thiếu mạng lần đầu thì fail.
pw_install() {
  [ -n "${PW_READY:-}" ] && return 0
  export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$REAL_HOME/.cache/ms-playwright}"
  export npm_config_cache="${npm_config_cache:-$REAL_HOME/.npm}"
  mkdir -p "$PW_DIR"
  cp "$E2E"/*.mjs "$PW_DIR/"
  if ! cmp -s "$E2E/package-lock.json" "$PW_DIR/package-lock.json" || [ ! -d "$PW_DIR/node_modules/playwright" ]; then
    cp "$E2E/package.json" "$E2E/package-lock.json" "$PW_DIR/"
    (cd "$PW_DIR" && npm ci --no-audit --no-fund >>"$LOG" 2>&1) \
      || die "không cài được playwright (e2e/package.json vào $PW_DIR; cần Node, mạng lần đầu)"
  fi
  (cd "$PW_DIR" && timeout 600 npx playwright install chromium >>"$LOG" 2>&1) \
    || die "không cài được chromium cho playwright (cần mạng lần đầu)"
  PW_READY=1
}

# Chụp 5 trang web ngay khi PAY-1 xong (bảng có PAY-1 ở XONG, eval đạt); SCREENSHOTS=1 mà thất bại thì fail rõ ràng.
screenshots() {
  step "Chụp ảnh web (Playwright)"
  pw_install
  WEB_URL="$WEB_URL" WEB_TOKEN="$WEB_TOKEN" SHOTS="$SHOTS" REPO_ID="local/fixture-service" KEY=PAY-1 \
    node "$PW_DIR/screenshots.mjs" >>"$LOG" 2>&1 || die "chụp ảnh thất bại (cần Node, playwright và chromium)"
  for f in "$SHOTS"/*.png; do echo "Ảnh: $f"; done
}

# web_ui <ACTION> [GATE] [SHOT] [KEY]: thao tác trang B bằng Playwright (e2e/web-approve.mjs) trên repo chế độ web.
web_ui() {
  pw_install
  WEB_URL="$WEB_URL" WEB_TOKEN="$WEB_TOKEN" REPO_ID="$WREPO_ID" KEY="${4:-$KEY}" ACTION="$1" \
    GATE="${2:-}" SHOT="${3:-}" PAGE="${PAGE:-}" node "$PW_DIR/web-approve.mjs" >>"$LOG" 2>&1 \
    || die "Playwright: $1 ${2:-} thất bại (xem log)"
}

# web_risk: trang /risk/<repo>#rules có rule E1 (mô tả bằng lời) của fixture, không có tab thử phân loại; panel Rủi ro
# của $KEY (cổng CASES) mức high; review card DESIGN có khối Xếp loại LLM với E1 và câu trích từ brd.md
# (e2e/web-risk.mjs, repo chế độ web). SCREENSHOTS=1 thì chụp risk-rules/risk-levels/risk-feature.png.
web_risk() {
  pw_install
  WEB_URL="$WEB_URL" WEB_TOKEN="$WEB_TOKEN" REPO_ID="$WREPO_ID" KEY="$KEY" RULE=E1 \
    DESCRIBE="Chặn giao dịch theo hạn mức" LEVEL=high GATE=CASES LLM_GATE=DESIGN \
    EVIDENCE="chặn sớm giao dịch chuyển tiền vượt hạn mức ngày" \
    SHOTS="${SCREENSHOTS:+$SHOTS}" node "$PW_DIR/web-risk.mjs" >>"$LOG" 2>&1 \
    || die "Playwright: web-risk thất bại (xem log)"
}

# risk_agent [KEY lời giải]: giả lập agent chạy subagent aisdlc-risk-classifier trước `submit design` (lite: `submit
# cases`) hay `risk reclassify`: lấy inputsHash do `aisdlc risk inputs` in ra (tính năng $KEY), thay vào
# e2e/solutions/<KEY>/risk-agent.json rồi ghi $P/risk-agent.json như agent ghi nguyên văn JSON của subagent.
risk_agent() {
  local sol=${1:-$KEY} hash
  run 0 aisdlc risk inputs --feature "$KEY"
  has "policy: .aisdlc/risk/policy-effective.yaml"
  hash="$(sed -n 's/^inputsHash: *//p' <<<"$OUT")"
  [ -n "$hash" ] || die "aisdlc risk inputs không in inputsHash"
  jq --arg h "$hash" '.inputsHash = $h' "$SOL/$sol/risk-agent.json" >"$P/risk-agent.json"
}

# risk_level <thư mục tính năng>: mức trong risk.yaml (dòng `level:` đầu file).
risk_level() { sed -n 's/^level: *"\{0,1\}\([^"]*\)"\{0,1\} *$/\1/p' "$1/risk.yaml" | head -n 1; }

# web_ctl <ACTION> [VAR=giá trị...]: điều khiển agent trên màn C / trang B bằng Playwright (e2e/web-control.mjs),
# repo chế độ web. Biến theo ACTION (GATE, STEP, FILE, TEXT, PAGE, SHOT) xem đầu file .mjs.
web_ctl() {
  local action=$1
  shift
  pw_install
  env WEB_URL="$WEB_URL" WEB_TOKEN="$WEB_TOKEN" REPO_ID="$WREPO_ID" KEY="$KEY" ACTION="$action" \
    "$@" node "$PW_DIR/web-control.mjs" >>"$LOG" 2>&1 || die "Playwright: web-control $action $* thất bại (xem log)"
}

# Trạng thái bước <id> trong steps.json của tính năng $KEY ở $REPO.
step_status() { jq -r --arg id "$1" '[.. | objects | select(.id==$id and has("status"))][0].status' \
  "$REPO/aisdlc/features/$KEY/steps.json"; }

copy() { cp -r "$1/." "$REPO/"; } # chép file "agent viết" vào repo

# new_repo <thư mục>: bản sao dịch vụ mẫu, git init, commit "fixture".
new_repo() {
  cp -r "$E2E/fixture-service" "$1"
  rm -rf "$1/bin"
  cd "$1"
  git init -q -b main
  git config user.name "Dev E2E"
  git config user.email "dev@example.com"
  git add -A && git commit -qm "fixture"
}

# docker_host: máy chạy Docker nhìn từ đây (dind trong CI: DOCKER_HOST=tcp://docker:2375 → docker).
docker_host() {
  if [ -n "${TESTCONTAINERS_HOST_OVERRIDE:-}" ]; then echo "$TESTCONTAINERS_HOST_OVERRIDE"; return; fi
  case "${DOCKER_HOST:-}" in
    tcp://*) local h="${DOCKER_HOST#tcp://}"; echo "${h%%:*}" ;;
    *) echo 127.0.0.1 ;;
  esac
}

# free_port: một cổng TCP ngẫu nhiên chưa ai nghe trên 127.0.0.1.
free_port() {
  local p i
  for i in $(seq 1 50); do
    p=$(( 20000 + RANDOM % 40000 ))
    (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null || { echo "$p"; return 0; }
  done
  die "không tìm được cổng trống"
}

# web_start: chạy server web Go nền, log vào e2e/out/web.log, chờ sẵn sàng.
web_start() {
  AISDLC_LISTEN="127.0.0.1:$AISDLC_WEB_PORT" AISDLC_DB_URL="$WEB_DB_URL" AISDLC_DB_USER=aisdlc AISDLC_DB_PASSWORD=aisdlc \
    AISDLC_SIGNING_KEY_FILE="$WORK/web-data/signing-key" "$ROOT/bin/aisdlc-web" >>"$OUTDIR/web.log" 2>&1 &
  WEB_PID=$!
  echo "$WEB_PID $$" >"$WEB_PIDFILE"
  web_wait_ready
}

# web_admin <đối số user ...>: lệnh quản trị của aisdlc-web trên cùng DB (in ra stdout; có token, không ghi log).
web_admin() {
  AISDLC_DB_URL="$WEB_DB_URL" AISDLC_DB_USER=aisdlc AISDLC_DB_PASSWORD=aisdlc "$ROOT/bin/aisdlc-web" user "$@"
}

# shellcheck source=web-check.sh
. "$E2E/web-check.sh"

# ---------------------------------------------------------------------------------------------
step "Công cụ cần có, dọn container sót của lần chạy trước"
for t in go git jq curl script docker; do command -v "$t" >/dev/null || die "thiếu lệnh $t"; done
if [ "$WEB" = 1 ]; then for t in node npm; do command -v "$t" >/dev/null || die "thiếu lệnh $t (WEB=1)"; done; fi
docker info >/dev/null 2>&1 || die "Docker không chạy (cần cho harness và Postgres của web)"
# Container của lần chạy đã chết (bị kill -9, máy tắt): nhãn aisdlc-e2e.pid trỏ tới tiến trình không còn.
for c in $(docker ps -aq --filter "label=$LABEL" 2>/dev/null); do
  pid="$(docker inspect -f "{{index .Config.Labels \"$LABEL.pid\"}}" "$c" 2>/dev/null || true)"
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    docker rm -f -v "$c" >/dev/null 2>&1 && echo "dọn container sót $c (pid $pid)" >>"$LOG"
  fi
done

step "Build aisdlc và aisdlc-harness (plugin dist), aisdlc-web (UI nhúng), symlink plugin/bin/aisdlc vào PATH tạm"
(cd "$ROOT" && run 0 make -s plugin-dist)
if [ "$WEB" = 1 ]; then
  # UI build (npm) nhúng vào binary; E2E_SKIP_UI=1 dùng lại dist đã có (chạy lại nhanh khi không sửa UI).
  if [ -n "${E2E_SKIP_UI:-}" ] && [ -f "$ROOT/internal/web/spa/dist/index.html" ]; then
    (cd "$ROOT" && run 0 make -s build-web-bin)
  else
    (cd "$ROOT" && run 0 make -s build-web)
  fi
  [ -f "$ROOT/internal/web/spa/dist/index.html" ] || die "UI chưa được build vào internal/web/spa/dist"
fi
mkdir -p "$WORK/bin"
ln -s "$ROOT/plugin/bin/aisdlc" "$WORK/bin/aisdlc" # như install.sh, nhưng không đụng ~/.local/bin
export PATH="$WORK/bin:$PATH"
# Như terminal của dev hay job CI: không trong Claude Code, không session (phần session tự đặt AISDLC_SESSION).
unset JIRA_URL JIRA_TOKEN AISDLC_SESSION CLAUDECODE CLAUDE_CODE_ENTRYPOINT
unset AISDLC_WEB_URL AISDLC_WEB_TOKEN AISDLC_CI_TOKEN
# HOME riêng: `aisdlc web login` ghi ~/.config/aisdlc; không bao giờ đụng HOME thật. Cache Go (module, build) và cấu
# hình docker giữ của HOME thật để harness không tải lại module, không build lại từ đầu.
REAL_HOME="$HOME"
GOMODCACHE="$(go env GOMODCACHE)"
GOCACHE="$(go env GOCACHE)"
GOPATH="$(go env GOPATH)"
export GOMODCACHE GOCACHE GOPATH
export DOCKER_CONFIG="${DOCKER_CONFIG:-$REAL_HOME/.docker}"
PW_DIR="${E2E_PW_DIR:-${XDG_CACHE_HOME:-$REAL_HOME/.cache}/aisdlc-e2e/pw}"
export HOME="$WORK/home"
mkdir -p "$HOME"
# git cô lập: không đọc cấu hình hệ thống/toàn cục của máy (hook, alias, ký commit...), không thừa biến GIT_* của shell.
unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE \
  GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_CEILING_DIRECTORIES \
  GIT_NAMESPACE GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT XDG_CONFIG_HOME
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$WORK/gitconfig"
: >"$GIT_CONFIG_GLOBAL"
SHOTS="$OUTDIR/screens"
[ -z "${SCREENSHOTS:-}" ] || mkdir -p "$SHOTS"

if [ "$WEB" = 1 ]; then
  step "Web: Postgres (docker run, nhãn $LABEL, cổng ngẫu nhiên), aisdlc-web nền, tạo user"
  PG_CTR="$LABEL-pg-$$-$RANDOM"
  PG_HOST="$(docker_host)"
  pub="127.0.0.1::5432"
  [ "$PG_HOST" = 127.0.0.1 ] || pub="5432"
  run 0 docker run -d --name "$PG_CTR" --label "$LABEL=1" --label "$LABEL.pid=$$" \
    -e POSTGRES_USER=aisdlc -e POSTGRES_PASSWORD=aisdlc -e POSTGRES_DB=aisdlc -p "$pub" postgres:16-alpine
  PG_PORT="$(docker port "$PG_CTR" 5432/tcp | head -n 1 | sed 's/.*://')"
  [ -n "$PG_PORT" ] || die "không đọc được cổng Postgres"
  # Ảnh postgres khởi động lại một lần sau initdb: chờ kết nối TCP được.
  for i in $(seq 1 60); do
    docker exec "$PG_CTR" psql -h 127.0.0.1 -U aisdlc -d aisdlc -tAc 'select 1' >/dev/null 2>&1 && break
    [ "$i" -lt 60 ] || die "Postgres không sẵn sàng sau 60 giây"
    sleep 1
  done
  WEB_DB_URL="postgres://$PG_HOST:$PG_PORT/aisdlc?sslmode=disable"
  AISDLC_WEB_PORT="$(free_port)"
  WEB_URL="http://127.0.0.1:$AISDLC_WEB_PORT"
  mkdir -p "$WORK/web-data"
  web_start
  # Không dùng run(): output có token, tuyệt đối không ghi vào $LOG.
  rc=0
  UOUT="$(web_admin add dev "Dev E2E" --email dev@example.com 2>&1)" || rc=$?
  [ "$rc" -eq 0 ] || die "'user add' exit $rc"
  WEB_TOKEN="$(sed -n 's/^Token: *//p' <<<"$UOUT" | tail -n 1 | tr -d '[:space:]')"
  unset UOUT
  SECRETS+=("$WEB_TOKEN")
  echo "user add dev: OK (token đã ẩn)" >>"$LOG"
  [ -n "$WEB_TOKEN" ] || die "không lấy được token từ 'user add'"
  export AISDLC_WEB_URL="$WEB_URL" AISDLC_WEB_TOKEN="$WEB_TOKEN"
fi

# =============================================================================================
if part local; then
step "Repo dịch vụ: git init, commit, aisdlc setup, commit"
new_repo "$REPO"
run 0 aisdlc setup --approvals local
has "Giữ nguyên aisdlc/config.yaml (đã có)."
has "Đã tạo aisdlc/gitlab-ci.aisdlc.yml"
git add -A && git commit -qm "aisdlc setup"
SETUP="$(git rev-parse HEAD)"

step "Chính sách rủi ro từ file: setup giữ aisdlc/risk-policy.yaml của fixture, policy show đọc file"
grep -q 'id: E1' "$REPO/aisdlc/risk-policy.yaml" || die "setup không được ghi đè aisdlc/risk-policy.yaml của fixture"
run 0 aisdlc policy show
has "(file)"
has "E1   high   Chặn giao dịch theo hạn mức (E2E)"

# =============================================================================================
KEY=PAY-1
P="$REPO/aisdlc/features/$KEY"
step "$KEY init (FULL)"
run 0 aisdlc init "$KEY" --title "Kiểm tra hạn mức ngày"

step "$KEY BRD: submit, lệnh dev không có console bị từ chối, approve qua pty"
cp "$SOL/$KEY/brd.md" "$P/brd.md"
run 0 aisdlc submit brd
hasnt "Rủi ro" # không còn mức tạm ở BRD: xếp loại một lần lúc submit design
if [ "$WEB" = 1 ]; then
  web_assert "board: PAY-1 ở cột BRD, chờ duyệt BRD" /api/board \
    "$(web_card PAY-1) | (.column==\"BRD\" and (.labels|index(\"AWAITING_APPROVAL:BRD\")!=null))"
  web_assert "inbox có 'aisdlc approve brd'" /api/board \
    '[.inbox[] | select(.key=="PAY-1" and (.command|contains("aisdlc approve brd")))] | length > 0'
fi
run 3 aisdlc approve brd </dev/null
has "chỉ dev"
dev approve brd
has "Đã duyệt cổng BRD"

step "$KEY hook: ghi code trước khi qua cổng → deny"
hook Write "{\"file_path\":\"$REPO/internal/limit/web/handler.go\",\"content\":\"x\"}"
has '"deny"'

step "$KEY design: chưa có risk-agent.json → submit bị chặn (RISK_AGENT_MISSING), không ghi risk.yaml"
cp "$SOL/$KEY/design.md" "$SOL/$KEY/design.yaml" "$P/"
cp "$SOL/$KEY/db/"*.sql "$P/db/"
run 1 aisdlc submit design
has "RISK_AGENT_MISSING"
[ ! -e "$P/risk.yaml" ] || die "submit design bị chặn không được ghi risk.yaml"

step "$KEY design: subagent xếp loại (risk-agent.json giả lập); tài liệu đổi sau đó → kết quả cũ bị chặn"
risk_agent
has "aisdlc/features/$KEY/brd.md"
has "aisdlc/features/$KEY/db/00002_daily_limit.sql"
echo "Ghi chú thêm sau khi xếp loại." >>"$P/design.md"
run 1 aisdlc submit design
has "RISK_AGENT_INVALID"
has "inputsHash không khớp"
[ ! -e "$P/risk.yaml" ] || die "submit design bị chặn không được ghi risk.yaml"
cp "$SOL/$KEY/design.md" "$P/design.md" # trả design.md về đúng bản đã xếp loại: inputsHash khớp lại

step "$KEY design: submit (migration-check thật), approve"
run 0 aisdlc submit design
has "schema-expected.json"
has "Rủi ro: high"
has "E1 ("
grep -q 'inputsHash:' "$P/risk.yaml" || die "risk.yaml phải có llm.inputsHash"
[ "$(risk_level "$P")" = high ] || die "risk.yaml: level phải là high, được '$(risk_level "$P")'"
if [ "$WEB" = 1 ]; then
  web_assert "board: PAY-1 riskLevel high" /api/board "$(web_card PAY-1) | .riskLevel==\"high\""
fi
T0=$(date +%s%N)
run 0 aisdlc review design
T1=$(date +%s%N)
has "SCOPE_OVERLAP"
has "SCOPE_MATCHES_OLD_MIGRATION"
[ $(( (T1 - T0) / 1000000 )) -lt 1000 ] || die "aisdlc review design chậm: $(( (T1 - T0) / 1000000 )) ms"
run 0 aisdlc review design --json
[ "$(jq -r '.design.apiRows | length' <<<"$OUT")" = 3 ] || die "review design --json: .design.apiRows phải có 3 dòng"
dev approve design
has "Review card"
if [ "$WEB" = 1 ]; then
  web_assert "web: reviewCards.DESIGN có cờ SCOPE_OVERLAP HIGH" /api/features/local/fixture-service/PAY-1 \
    '[.snapshot.reviewCards.DESIGN.flags[] | select(.code=="SCOPE_OVERLAP" and .level=="HIGH")] | length > 0'
  web_assert "web: reviewCards.DESIGN có cờ SCOPE_MATCHES_OLD_MIGRATION WARN" /api/features/local/fixture-service/PAY-1 \
    '[.snapshot.reviewCards.DESIGN.flags[] | select(.code=="SCOPE_MATCHES_OLD_MIGRATION" and .level=="WARN")] | length > 0'
  web_assert "web: reviewCards.DESIGN.design.tables có daily_limit NEW" /api/features/local/fixture-service/PAY-1 \
    '[.snapshot.reviewCards.DESIGN.design.tables[] | select(.name=="daily_limit" and .status=="NEW")] | length > 0'
  web_assert "web: reviewCards.DESIGN.design.apiRows có 3 dòng" /api/features/local/fixture-service/PAY-1 \
    '.snapshot.reviewCards.DESIGN.design.apiRows | length == 3'
fi

step "$KEY cases: mức high mà thiếu case gửi trùng → submit bị chặn (RISK_REQ 'gửi trùng')"
cp "$SOL/$KEY/risk/cases-no-repeat.yaml" "$P/cases.yaml"
run 1 aisdlc submit cases # phát hiện in ra stdout như mọi cổng
has "RISK_REQ: Mức high yêu cầu Case gửi trùng"
has "POST /api/v1/limits/check"
hasnt "check\`"

step "$KEY cases: thêm case repeat → submit (phải có case fail trước khi code), approve"
cp "$SOL/$KEY/cases.yaml" "$P/cases.yaml"
run 0 aisdlc submit cases
dev approve cases

step "$KEY hook: Bash sửa cases.yaml đã khóa → deny"
hook Bash "{\"command\":\"sed -i s/a/b/ aisdlc/features/$KEY/cases.yaml\"}"
has '"deny"'

step "$KEY S1: start, hook cho ghi file trong phạm vi, finish (pass), accept"
run 0 aisdlc step start S1
hook Write "{\"file_path\":\"$REPO/internal/limit/web/handler.go\",\"content\":\"x\"}"
hasnt '"deny"'
copy "$SOL/$KEY/S1"
run 0 aisdlc step finish S1
has "2/2 pass"
dev step accept S1

step "$KEY S2: file ngoài phạm vi → scope allow → finish lại sạch → accept"
run 0 aisdlc step start S2
copy "$SOL/$KEY/S2"
echo "ghi chú tạm" >"$REPO/README2.txt"
if [ "$WEB" = 1 ]; then
  hook_post Write "{\"file_path\":\"$REPO/README2.txt\",\"content\":\"x\"}"
  web_assert "events: FILE_EDIT README2.txt ngoài phạm vi" /api/features/local/fixture-service/PAY-1/events \
    '[(if type=="array" then . else .events end)[] | select(.type=="FILE_EDIT" and .outOfScope==true)] | length > 0'
fi
run 1 aisdlc step finish S2
has "Ngoài phạm vi: README2.txt"
dev scope allow README2.txt
run 0 aisdlc step start S2
run 0 aisdlc step finish S2
has "7/7 pass"
has "Ngoài phạm vi: không có"
dev step accept S2

step "$KEY eval → ĐẠT"
if [ "$WEB" = 1 ]; then
  # Web chết: eval và submit review vẫn phải pass, bản tin nằm lại outbox rồi push bù khi web sống lại.
  web_stop
fi
run 0 aisdlc eval
[ "$(head -n 1 "$P/eval/report.md")" = "Eval $KEY: ĐẠT" ] || die "report.md dòng đầu: $(head -n 1 "$P/eval/report.md")"
grep -qx '## Rủi ro' "$P/eval/report.md" || die "report.md thiếu mục '## Rủi ro'"

step "$KEY review: submit, approve"
cp "$SOL/$KEY/review-agent.json" "$P/"
run 0 aisdlc submit review
grep -qE '^\| AGENT_REVIEW \| (OK|WARN) \|' "$P/eval/report.md" || die "report.md chưa có kết quả AGENT_REVIEW"
dev approve review
has "Agent review:"
if [ "$WEB" = 1 ]; then
  run 0 aisdlc web status
  pending="$(sed -n 's/^Đang chờ gửi: *//p' <<<"$OUT")"
  [ "${pending:-0}" -gt 0 ] || die "web tắt mà 'Đang chờ gửi' = '$pending', mong đợi > 0"
  web_start
  run 0 aisdlc web push
  run 0 aisdlc web status
  has "Đang chờ gửi: 0"
  web_assert "board: PAY-1 eval.passed" /api/board "$(web_card PAY-1) | .eval.passed==true"
  web_assert "board: PAY-1 ở cột DONE sau approve review" /api/board "$(web_card PAY-1) | .column==\"DONE\""
  web_assert "web (xong): reviewCards.DESIGN còn cờ SCOPE_OVERLAP HIGH" /api/features/local/fixture-service/PAY-1 \
    '[.snapshot.reviewCards.DESIGN.flags[] | select(.code=="SCOPE_OVERLAP" and .level=="HIGH")] | length > 0'
  web_assert "web (xong): reviewCards.CASES tồn tại" /api/features/local/fixture-service/PAY-1 '.snapshot.reviewCards.CASES != null'
  [ -z "${SCREENSHOTS:-}" ] || screenshots
fi

step "$KEY commit (MR) và eval --ci so với commit setup"
git add -A && git commit -qm "$KEY"
PAY="$(git rev-parse HEAD)"
run 0 aisdlc eval --ci --base "$SETUP"
has "Eval $KEY: ĐẠT"
has "| RISK_LLM | ĐẠT |" # inputsHash trong risk.yaml khớp tài liệu và chính sách hiện tại, không gọi LLM
hasnt "GATE:"
git checkout -q -- aisdlc # CI ghi đè eval/report.*; bỏ, giữ bản đã commit

# Review Focus 2: CI của dịch vụ đích không có Docker socket → báo lỗi rõ, thoát khác 0, không treo (timeout 300 giây
# chỉ để chắc không treo; hết giờ thì exit 124 và bước này fail). testcontainers bỏ qua DOCKER_HOST không với tới được
# và thử tiếp các socket cục bộ (/var/run/docker.sock, socket rootless trong $XDG_RUNTIME_DIR, ~/.docker/run...), nên
# máy có socket cục bộ thì eval chạy trong container golang không gắn socket nào (đúng như job CI không có Docker; Docker
# rootless thì chạy bằng root của container = user thật). Không có socket cục bộ (CI dind) thì chỉ cần DOCKER_HOST sai,
# bỏ XDG_RUNTIME_DIR và cấu hình docker (context). Nếu testcontainers vẫn tìm được Docker (cấu hình lạ) thì báo BỎ QUA
# rõ ràng, không fail oan.
step "$KEY âm tính: eval --ci khi DOCKER_HOST sai, không có socket Docker → lỗi rõ, exit khác 0, không treo"
local_sock=""
for sk in /var/run/docker.sock "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/docker.sock" "/run/user/$(id -u)/docker.sock" \
  "$REAL_HOME/.docker/run/docker.sock" "$REAL_HOME/.docker/desktop/docker.sock"; do
  [ -S "$sk" ] && { local_sock="$sk"; break; }
done
case "${DOCKER_HOST:-}" in unix://*) local_sock="${DOCKER_HOST#unix://}" ;; esac
T0=$(date +%s)
skip=""
rc=0
if [ -n "$local_sock" ] && [ "$(docker_host)" = 127.0.0.1 ]; then
  arch="$(uname -m)"; case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
  user="$(id -u):$(id -g)"
  docker info -f '{{.SecurityOptions}}' 2>/dev/null | grep -q rootless && user=0:0
  IMG=golang:1.26-bookworm
  if docker image inspect "$IMG" >/dev/null 2>&1 || docker pull -q "$IMG" >>"$LOG" 2>&1; then
    OUT="$(docker run --rm --label "$LABEL=1" --label "$LABEL.pid=$$" --user "$user" \
      -e HOME=/tmp -e GOMODCACHE=/gomod -e GOCACHE=/gocache -e GOTOOLCHAIN=local \
      -e DOCKER_HOST=tcp://127.0.0.1:1 -v "$GOMODCACHE:/gomod:ro" -v "$GOCACHE:/gocache" -v "$REPO:$REPO" -w "$REPO" \
      -v "$ROOT/plugin/lib/aisdlc-linux-$arch:/usr/local/bin/aisdlc:ro" \
      -v "$ROOT/plugin/lib/aisdlc-harness-linux-$arch:/usr/local/bin/aisdlc-harness:ro" "$IMG" \
      timeout 300 aisdlc eval --ci --base "$SETUP" 2>&1)" || rc=$?
  else
    skip="không có ảnh $IMG (cần mạng lần đầu) để chạy eval ở nơi không có socket Docker"
  fi
else
  mkdir -p "$WORK/no-docker-config"
  OUT="$(env -u XDG_RUNTIME_DIR -u TESTCONTAINERS_HOST_OVERRIDE -u DOCKER_CONTEXT DOCKER_CONFIG="$WORK/no-docker-config" \
    DOCKER_HOST=tcp://127.0.0.1:1 timeout 300 aisdlc eval --ci --base "$SETUP" 2>&1)" || rc=$?
fi
if [ -z "$skip" ]; then
  redact
  printf '$ aisdlc eval --ci --base %s (không có Docker)\n%s\n[exit %s]\n' "$SETUP" "$OUT" "$rc" >>"$LOG"
  if [ "$rc" != 124 ] && grep -q 'Resolved Docker Host' <<<"$OUT"; then
    skip="testcontainers vẫn tìm được Docker ($(grep -m1 -o 'Resolved Docker Host: .*' <<<"$OUT"))"
  fi
fi
if [ -n "$skip" ]; then
  echo "BỎ QUA bước eval không có Docker: $skip" | tee -a "$LOG"
else
  [ "$rc" = 1 ] || die "eval không có Docker: exit $rc, mong đợi 1 (124 = treo tới timeout)"
  has "Eval $KEY: KHÔNG ĐẠT"
  has "không khởi động được Postgres: "
  echo "eval không có Docker: exit 1 sau $(( $(date +%s) - T0 )) giây, $(grep -m1 -o 'không khởi động được Postgres: .\{0,80\}' <<<"$OUT")" | tee -a "$LOG"
fi
git checkout -q -- aisdlc

step "$KEY âm tính: sửa cases.yaml sau khi duyệt → eval fail CASES_LOCKED"
echo "# sửa sau khi duyệt" >>"$P/cases.yaml"
run 1 aisdlc eval
grep -qE '^\| CASES_LOCKED \| KHÔNG ĐẠT' "$P/eval/report.md" || die "report.md không có CASES_LOCKED KHÔNG ĐẠT"
git checkout -q -- aisdlc

# Review Focus 5: dịch vụ không tắt êm (bỏ qua SIGTERM) → harness SIGKILL sau shutdownTimeoutSeconds (10 giây ở
# fixture), coverage N/A kèm lý do; chỉ số mềm nên eval vẫn đạt. Biến đi qua môi trường của eval (dịch vụ thừa hưởng
# env của harness): sửa extraEnv trong aisdlc/config.yaml thì eval fail SCOPE (cấu hình phải đổi ở MR riêng).
step "$KEY âm tính: fixture bỏ qua SIGTERM (FIXTURE_IGNORE_SIGTERM=1) → eval báo coverage N/A kèm lý do"
run 0 env FIXTURE_IGNORE_SIGTERM=1 aisdlc eval
grep -E '^\| COVERAGE_CHANGED_LINES \|' "$P/eval/report.md" >"$WORK/cov-row" || die "report.md thiếu dòng COVERAGE_CHANGED_LINES"
grep -qE '^\| COVERAGE_CHANGED_LINES \| NA \| dịch vụ không tắt êm khi nhận SIGTERM .*SIGKILL' "$WORK/cov-row" \
  || die "coverage phải N/A kèm lý do không tắt êm, được: $(cat "$WORK/cov-row")"
echo "coverage khi fixture bỏ qua SIGTERM: $(cat "$WORK/cov-row")" | tee -a "$LOG"
left="$(svc_pids)"
[ -z "$left" ] || die "harness để sót dịch vụ mẫu (bỏ qua SIGTERM) đang chạy: pid $left"
git checkout -q -- aisdlc

# =============================================================================================
KEY=FIX-2
P="$REPO/aisdlc/features/$KEY"
step "$KEY init (LITE)"
run 0 aisdlc init "$KEY" --lite --title "Sửa làm tròn phí"
[ ! -e "$P/brd.md" ] && [ ! -e "$P/design.md" ] || die "luồng nhẹ không được có brd/design"
if [ "$WEB" = 1 ]; then
  web_assert "board: FIX-2 (LITE) bắt đầu ở cột CASES" /api/board "$(web_card FIX-2) | .column==\"CASES\""
fi

step "$KEY cases: subagent xếp loại (feature.yaml + cases.yaml), submit (migration-check + case fail), approve"
cp "$SOL/$KEY/cases.yaml" "$P/cases.yaml"
risk_agent
has "aisdlc/features/$KEY/cases.yaml"
run 0 aisdlc submit cases
has "Rủi ro: low"
run 0 aisdlc review cases
hasnt "[HIGH]"
hasnt "[WARN]"
dev approve cases

step "$KEY S1: start, finish (pass), accept"
run 0 aisdlc step start S1
copy "$SOL/$KEY/S1"
run 0 aisdlc step finish S1
has "3/3 pass"
dev step accept S1

step "$KEY eval, review"
run 0 aisdlc eval
[ "$(head -n 1 "$P/eval/report.md")" = "Eval $KEY: ĐẠT" ] || die "report.md dòng đầu: $(head -n 1 "$P/eval/report.md")"
cp "$SOL/$KEY/review-agent.json" "$P/"
run 0 aisdlc submit review
dev approve review

step "$KEY commit (MR) và eval --ci so với commit $KEY trước đó"
git add -A && git commit -qm "$KEY"
run 0 aisdlc eval --ci --base "$PAY"
has "Eval $KEY: ĐẠT"
hasnt "Eval PAY-1"
fi # part local

# =============================================================================================
# Duyệt trên web (approvals: web): repo thứ hai local/web-service. FIX-2 duyệt local trước khi chuyển chế độ (để
# `aisdlc web migrate` + chủ tính năng "Xác nhận" trên trang B); PAY-1 đi trọn luồng web: BRD duyệt trên trang B
# (Playwright), DESIGN/CASES từ terminal (token approve), web chết, cache bị sửa, REVIEW trên web, eval --ci hỏi web.
if [ "$WEB" = 1 ] && part web; then
  WREPO="$WORK/web-service"
  WREPO_ID="local/web-service"
  REPO="$WREPO" # hook()/copy() dùng $REPO
  # CLI chế độ web dùng ~/.config/aisdlc/web.json (token ingest) chứ không dùng biến môi trường (token login).
  unset AISDLC_WEB_URL AISDLC_WEB_TOKEN

  step "web-service: git init, setup, FIX-2 duyệt CASES ở local (chưa bật web)"
  new_repo "$WREPO"
  run 0 aisdlc setup --approvals local
  git add -A && git commit -qm "aisdlc setup"
  KEY=FIX-2
  run 0 aisdlc init "$KEY" --lite --title "Sửa làm tròn phí"
  P="$WREPO/aisdlc/features/$KEY"
  cp "$SOL/$KEY/cases.yaml" "$P/cases.yaml"
  risk_agent
  run 0 aisdlc submit cases
  dev approve cases
  has "Đã duyệt cổng CASES"
  FIX_GATES="$WREPO/aisdlc/features/FIX-2/gates.json"
  [ -f "$FIX_GATES" ] || die "FIX-2 duyệt local phải có gates.json"
  FIX_GATES_SUM="$(sha256sum "$FIX_GATES")"

  # baseRef: main: FIX-2 đã nằm trên main (như đã merge) nên không còn "đang mở"; PAY-1 làm tiếp là tính năng mở duy
  # nhất (không thì lệnh không --feature báo nhiều tính năng đang mở: FIX-2 mới duyệt CASES, REVIEW chưa duyệt).
  step "web-service: bật approvals: web, baseRef main, commit"
  printf '# Duyệt cổng trên web (E2E)\napprovals: web\nbaseRef: main\n' >>"$WREPO/aisdlc/config.yaml"
  git add -A && git commit -qm "approvals: web"
  WBASE="$(git rev-parse HEAD)"

  step "Token: phiên login tạo token ingest + approve ở /api/me/tokens; ingest không mở phiên, không duyệt"
  web_session
  INGEST="$(web_token_create ingest e2e-cli)"
  APPROVE="$(web_token_create approve e2e-terminal)"
  [ -n "$INGEST" ] && [ -n "$APPROVE" ] || die "không tạo được token ingest/approve"
  SECRETS+=("$INGEST" "$APPROVE")
  echo "tạo token ingest + approve: OK (token đã ẩn)" >>"$LOG"
  [ "$(web_login_status "$INGEST")" = 401 ] || die "token ingest không được mở phiên (/api/login phải 401)"
  code="$(web_status_with "$INGEST" POST /api/gates/approve '{"repo":"local/web-service","feature":"PAY-1","gate":"BRD","hash":"x"}')"
  case "$code" in 401|403) ;; *) die "token ingest gọi /api/gates/approve trả $code, mong đợi 401/403" ;; esac
  web_assert "/api/me/tokens có ingest và approve" /api/me/tokens \
    '([.[] | select(.kind=="ingest" and .label=="e2e-cli")] | length == 1) and ([.[] | select(.kind=="approve")] | length == 1)'

  step "aisdlc web login (pty, token ingest) và web approve-token (pty) vào HOME riêng"
  run 3 aisdlc web login </dev/null
  has "Lệnh này chỉ dev được chạy, trong terminal thật (không tìm thấy console). Agent không được chạy lệnh này."
  dev_secret "$WEB_URL"$'\n'"$INGEST" web login
  has "Đã lưu $HOME/.config/aisdlc/web.json"
  dev_secret "$APPROVE" web approve-token
  has "Đã lưu $HOME/.config/aisdlc/approve-token"
  [ "$(stat -c %a "$HOME/.config/aisdlc/web.json")" = 600 ] || die "web.json phải có quyền 600"
  ! grep -rqF -- "$INGEST" "$WREPO" || die "token ingest lọt vào repo"

  step "policy pull: repo kết nối web lần đầu → web nhập aisdlc/risk-policy.yaml làm v1 (import); pull lại không thêm bản"
  run 0 aisdlc policy pull
  has "Chính sách v1 (web)"
  [ -f "$WREPO/.aisdlc/web/policy.json" ] || die "policy pull phải ghi .aisdlc/web/policy.json"
  run 0 aisdlc policy pull
  has "Chính sách v1 (web)"
  web_assert "policy local/web-service: đúng một phiên bản v1, nguồn import" "/api/policy/versions?repo=$WREPO_ID" \
    'length == 1 and .[0].version == 1 and .[0].source == "import"'
  web_assert "policy local/web-service: v1 schema 2, có rule E1 (mô tả bằng lời) của fixture" "/api/policy?repo=$WREPO_ID" \
    '.version == 1 and .policy.schema == 2
     and ([.policy.rules[] | select(.id=="E1" and .level=="high" and (.describe | contains("hạn mức")))] | length == 1)'
  run 0 aisdlc policy show
  has "Chính sách v1 (web)"

  step "aisdlc web migrate: FIX-2 CASES lên web, chủ tính năng bấm Xác nhận trên trang B"
  run 0 aisdlc web migrate
  has "Đã gửi 1 bản duyệt"
  web_assert "repo local/web-service ghim chế độ web" /api/repos \
    '[.[] | select(.id=="local/web-service" and .approvals=="web")] | length == 1'
  web_assert "FIX-2: CASES chờ xác nhận nhập" /api/features/local/web-service/FIX-2 \
    '.approvalsMode=="web" and ([.pendingImports[] | select(.gate=="CASES" and .originalApprover=="Dev E2E")] | length == 1)'
  web_ui confirm-import CASES "${SCREENSHOTS:+$SHOTS/import-confirm.png}" FIX-2
  web_assert "FIX-2: CASES APPROVED sau xác nhận, hết dải nhập" /api/features/local/web-service/FIX-2 \
    '(.pendingImports | length == 0) and ([.snapshot.gates[] | select(.gate=="CASES" and .state=="APPROVED")] | length == 1)'
  [ "$(sha256sum "$FIX_GATES")" = "$FIX_GATES_SUM" ] || die "migrate không được sửa gates.json của FIX-2"

  # ------------------------------------------------------------------------------------------
  KEY=PAY-1
  P="$WREPO/aisdlc/features/$KEY"
  step "web $KEY init, BRD: submit lên web, duyệt trên trang B (Playwright)"
  run 0 aisdlc init "$KEY" --title "Kiểm tra hạn mức ngày"
  cp "$SOL/$KEY/brd.md" "$P/brd.md"
  run 0 aisdlc submit brd
  hook Write "{\"file_path\":\"$WREPO/internal/limit/web/handler.go\",\"content\":\"x\"}"
  has '"deny"'
  web_assert "web $KEY: BRD chờ duyệt" /api/features/local/web-service/PAY-1 \
    '[.snapshot.gates[] | select(.gate=="BRD" and .state=="AWAITING_APPROVAL")] | length == 1'
  web_ui approve BRD
  run 0 aisdlc status
  [ "$(cache_state "$KEY" | jq -r '[.approvals[] | select(.gate=="BRD")][-1].channel')" = web ] \
    || die "cache: approval BRD phải có channel web"
  want="$(web_get /api/features/local/web-service/PAY-1 | jq -c \
    '[.snapshot.reviewCards.BRD.flags[]? | select(.level=="HIGH" or .level=="WARN") | {code, level}] | sort')"
  got="$(web_sql "SELECT flags FROM approvals WHERE repo_id='local/web-service' AND feature_key='PAY-1' AND gate='BRD' AND channel='web'" | jq -c 'map({code, level}) | sort')"
  [ -n "$want" ] && [ "$want" != "[]" ] && [ "$want" != null ] \
    || die "review card BRD phải có cờ HIGH/WARN để kiểm flags của approval (được: '$want')"
  [ "$got" = "$want" ] || die "flags của approval BRD: $got, mong đợi $want"
  echo "web OK: BRD channel web, flags $got" | tee -a "$LOG"
  [ ! -e "$P/gates.json" ] || die "chế độ web không được tạo gates.json"

  step "web $KEY DESIGN: submit, duyệt từ terminal (token approve) → channel terminal, flags từ thẻ CLI"
  cp "$SOL/$KEY/design.md" "$SOL/$KEY/design.yaml" "$P/"
  cp "$SOL/$KEY/db/"*.sql "$P/db/"
  risk_agent
  run 0 aisdlc submit design
  has "Rủi ro: high (chính sách v1)"
  [ "$(risk_level "$P")" = high ] || die "risk.yaml (web): level phải là high, được '$(risk_level "$P")'"

  step "web $KEY DESIGN: trả lại trên web (nhắc mục API), sửa đúng mục đó, nộp lại → diff lần 2, chỉ dẫn từ trả lại"
  web_assert "web $KEY: DESIGN chờ duyệt (lần 1)" /api/features/local/web-service/PAY-1 \
    '[.snapshot.gates[] | select(.gate=="DESIGN" and .state=="AWAITING_APPROVAL")] | length == 1'
  web_ctl reject GATE=DESIGN TEXT="Mục API: ghi rõ gọi lặp có ghi used_amount không."
  cp "$SOL/$KEY/resubmit/design.md" "$P/design.md"
  risk_agent # design.md đổi → chạy lại subagent (inputsHash mới)
  run 0 aisdlc submit design
  web_assert "resubmission DESIGN: n = 2, góp ý RELATED_CHANGED (mục API), design.md đổi" \
    "/api/features/$WREPO_ID/$KEY/resubmission?gate=DESIGN" \
    '.n == 2 and ([.feedback[] | select(.status=="RELATED_CHANGED")] | length == 1)
     and ([.files[] | select(.path | endswith("design.md")) | select(.status=="CHANGED" or .status=="MODIFIED")] | length == 1)'
  [ -e "$WREPO/.aisdlc/web/control/inbox" ] || die "submit design phải tải chỉ dẫn từ trả lại và bật cờ inbox"
  hook_post Read "{\"file_path\":\"$WREPO/go.mod\"}"
  [ "$(jq -r '.hookSpecificOutput.additionalContext' <<<"$OUT")" != null ] || die "PostToolUse phải có additionalContext"
  has "Cổng DESIGN bị trả lại bởi dev: Mục API"
  has "Chỉ dẫn của dev ("
  hook_post Read "{\"file_path\":\"$WREPO/go.mod\"}"
  hasnt "additionalContext" # giao đúng một lần
  [ ! -e "$WREPO/.aisdlc/web/control/inbox" ] || die "giao xong chỉ dẫn mà cờ inbox còn"
  if [ -n "${SCREENSHOTS:-}" ]; then
    web_ctl expect PAGE=gate GATE=DESIGN TEXT="Thay đổi so với lần nộp trước" SHOT="$SHOTS/design-resubmission.png"
    web_assert "web $KEY: DESIGN chờ duyệt" /api/features/local/web-service/PAY-1 \
      '[.snapshot.gates[] | select(.gate=="DESIGN" and .state=="AWAITING_APPROVAL")] | length == 1'
    web_ui preview DESIGN "$SHOTS/approve-confirm.png"
  fi
  dev approve design
  has "Đã duyệt cổng DESIGN trên web"
  [ "$(cache_state "$KEY" | jq -r '[.approvals[] | select(.gate=="DESIGN")][-1].channel')" = terminal ] \
    || die "cache: approval DESIGN phải có channel terminal"
  got="$(web_sql "SELECT flags FROM approvals WHERE repo_id='local/web-service' AND feature_key='PAY-1' AND gate='DESIGN' AND channel='terminal'")"
  [ "$(jq '[.[] | select(.code=="SCOPE_OVERLAP" and .level=="HIGH")] | length' <<<"$got")" = 1 ] \
    || die "flags DESIGN (terminal) thiếu SCOPE_OVERLAP HIGH: $got"
  echo "web OK: DESIGN channel terminal, flags $got" | tee -a "$LOG"

  step "web $KEY CASES: submit, duyệt từ terminal"
  cp "$SOL/$KEY/cases.yaml" "$P/cases.yaml"
  run 0 aisdlc submit cases
  dev approve cases
  has "Đã duyệt cổng CASES trên web"

  step "web $KEY rủi ro trên web: /risk/$WREPO_ID#rules có rule E1, panel Rủi ro (CASES) mức high, Xếp loại LLM (DESIGN)"
  web_assert "board: PAY-1 (web-service) riskLevel high" /api/board \
    '.cards[] | select(.key=="PAY-1" and .repoId=="local/web-service") | .riskLevel=="high"'
  web_risk

  step "web $KEY cache bị sửa 1 byte → hook chặn sửa code; aisdlc status làm mới → cho lại"
  run 0 aisdlc step start S1
  W_IN="{\"file_path\":\"$WREPO/internal/limit/web/handler.go\",\"content\":\"x\"}"
  hook Write "$W_IN"
  hasnt '"deny"'
  CACHE="$WREPO/.aisdlc/web/gates/$KEY.json"
  jq -c '.payload |= ((length / 2 | floor) as $i
    | .[:$i] + (if .[$i:$i+1] == "B" then "C" else "B" end) + .[$i+1:])' "$CACHE" >"$CACHE.tmp"
  mv "$CACHE.tmp" "$CACHE"
  hook Write "$W_IN"
  has '"deny"'
  run 0 aisdlc status
  hook Write "$W_IN"
  hasnt '"deny"'

  step "web $KEY chỉ dẫn: gửi trên màn C → flush → PostToolUse (Read) giao đúng một lần → web 'đã giao lúc'"
  INSTR="Dùng float64? Không: giữ BIGINT như design (e2e)."
  web_ctl instruct TEXT="$INSTR"
  run 0 aisdlc web flush
  [ -e "$WREPO/.aisdlc/web/control/inbox" ] || die "web flush phải bật cờ inbox khi còn chỉ dẫn chưa giao"
  hook_post Read "{\"file_path\":\"$WREPO/go.mod\"}"
  has "Chỉ dẫn của dev ("
  has "$INSTR"
  hook_post Read "{\"file_path\":\"$WREPO/go.mod\"}"
  hasnt "additionalContext"
  run 0 aisdlc web flush # gửi INSTRUCTION_DELIVERED (tiến trình nền có thể đã gửi trước)
  web_control_assert "control: chỉ dẫn đã giao (deliveredAt)" \
    "[.instructions[] | select(.text==\"$INSTR\" and .deliveredAt != null)] | length == 1"
  web_ctl expect PAGE=live TEXT="đã giao lúc"

  step "web $KEY tạm dừng: màn C Tạm dừng → flush → PreToolUse ghi bị chặn, Stop không chặn; Tiếp tục → prompt → ghi được ngay"
  hook_stop
  has '"block"' # chưa tạm dừng: S1 IN_PROGRESS → Stop bị chặn
  web_ctl pause SHOT="${SCREENSHOTS:+$SHOTS/control-paused.png}"
  run 0 aisdlc web flush
  hook Write "$W_IN"
  has '"deny"'
  has "Dev đã tạm dừng tại web: dev. Dừng lại và chờ. Khi dev báo tiếp tục thì thử lại."
  hook Bash '{"command":"touch internal/x.txt"}'
  has "Dev đã tạm dừng tại web: dev."
  hook_stop
  hasnt '"block"'
  run 0 aisdlc status
  has "Đang tạm dừng từ web (dev,"
  # tiến trình nền do các lần bị chặn mở đã xong: cache chưa có chỉ dẫn và cờ tạm dừng vẫn bật khi dev gõ prompt
  wait_no_flush 30
  INSTR2="Chỉ dẫn gửi lúc tạm dừng (e2e)."
  web_ctl instruct TEXT="$INSTR2"
  web_ctl resume
  # Như phiên thật: dev bấm Tiếp tục rồi gõ prompt ở terminal. Không chạy flush tay, không thử lại: UserPromptSubmit
  # mở `web flush` ở nền, PreToolUse ghi ngay sau đó chờ tiến trình này (tối đa 2 giây) rồi được phép ở lần đầu.
  hook_prompt "tiếp đi"
  t0=$(date +%s%N)
  hook Write "$W_IN"
  RESUME_MS=$(( ($(date +%s%N) - t0) / 1000000 ))
  hasnt '"deny"'
  echo "web OK: Tiếp tục → prompt → PreToolUse Write được phép ở lần đầu (${RESUME_MS} ms, gồm chờ web flush)" | tee -a "$LOG"
  # chỉ dẫn gửi lúc tạm dừng: tới ngay ở PostToolUse của lần ghi đầu tiên sau đó
  hook_post Write "$W_IN"
  has "$INSTR2"
  hook_stop
  has '"block"'

  # spec §7: hook dưới 50 ms (p95). Đo bằng tiến trình thật (wrapper plugin/bin/aisdlc + binary Go), 20 lần mỗi loại/vòng,
  # cùng khuôn `printf JSON | lệnh` có đồng hồ quanh. Ngưỡng tuyệt đối: p95 > 50 ms là FAIL; E2E_LATENCY_SLACK_MS (mặc
  # định 0) nới cho máy CI chậm. Nền ngoài sản phẩm (không dùng làm ngưỡng): /bin/true và một script bash rỗng cùng khuôn
  # (chi phí fork/exec, khởi động bash), và chính hook với cwd ngoài repo AISDLC (khởi động wrapper + binary).
  step "web $KEY độ trễ hook (tiến trình thật): PreToolUse Edit có file điều khiển, PostToolUse Read không inbox"
  [ -f "$WREPO/.aisdlc/web/control/$KEY.json" ] || die "thiếu file điều khiển .aisdlc/web/control/$KEY.json"
  [ ! -e "$WREPO/.aisdlc/web/control/inbox" ] || die "đo PostToolUse Read cần không có cờ inbox"
  E_IN="{\"file_path\":\"$WREPO/internal/limit/web/handler.go\",\"old_string\":\"a\",\"new_string\":\"b\"}"
  # Cô lập phép đo: tiến trình gửi nền do các hook trước mở còn chạy thì chờ nó xong (tối đa 30 giây).
  wait_no_flush 30
  # Máy dùng chung có nhiễu (tiến trình khác chiếm CPU từng đợt): đo tối đa 3 vòng, dừng ở vòng đầu đạt cả hai; vượt
  # ngưỡng ở cả 3 vòng mới FAIL. Hồi quy thật (chậm đều) vẫn fail; mọi vòng đều in ra.
  printf '#!/usr/bin/env bash\ncat >/dev/null\n' >"$WORK/noop.sh"
  mkdir -p "$WORK/no-repo"
  LIMIT=$(( 50 + ${E2E_LATENCY_SLACK_MS:-0} ))
  hook_ms pre-tool-use Edit "$E_IN" >/dev/null # làm ấm cache đĩa
  LAT_SUMMARY=""
  for round in 1 2 3; do
    PRE_MS="$(stats20 hook_ms pre-tool-use Edit "$E_IN")"
    POST_MS="$(stats20 hook_ms post-tool-use Read "{\"file_path\":\"$WREPO/go.mod\"}")"
    TRUE_MS="$(stats20 pipe_ms /bin/true)"
    BASH_MS="$(stats20 pipe_ms bash "$WORK/noop.sh")"
    OUTSIDE_MS="$(HOOK_CWD="$WORK/no-repo" stats20 hook_ms pre-tool-use Edit "$E_IN")"
    VERDICT=ĐẠT
    { [ "${PRE_MS%% *}" -le "$LIMIT" ] && [ "${POST_MS%% *}" -le "$LIMIT" ]; } || VERDICT="KHÔNG ĐẠT"
    LAT_SUMMARY+="$(printf '%s\n' \
      "Độ trễ hook vòng $round (p95 trung-vị, ms; 20 lần; load $(cut -d' ' -f1-3 /proc/loadavg)):" \
      "  PreToolUse Edit (có điều khiển): ${PRE_MS}" \
      "  PostToolUse Read (không inbox):  ${POST_MS}" \
      "  nền /bin/true cùng khuôn:        ${TRUE_MS}" \
      "  nền script bash rỗng:            ${BASH_MS}" \
      "  hook, cwd ngoài repo AISDLC:     ${OUTSIDE_MS}" \
      "  ngưỡng p95 ≤ ${LIMIT} ms (spec 50 + E2E_LATENCY_SLACK_MS ${E2E_LATENCY_SLACK_MS:-0}): ${VERDICT}")"$'\n'
    [ "$VERDICT" = ĐẠT ] && break
  done
  # Bảng độ trễ in một lần ở tổng kết cuối; chỉ in ngay ở đây khi FAIL (die thoát trước tổng kết).
  [ "$VERDICT" = ĐẠT ] || printf '%s' "$LAT_SUMMARY" | tee -a "$LOG"
  [ "${PRE_MS%% *}" -le "$LIMIT" ] || die "PreToolUse Edit chậm: p95 ${PRE_MS%% *} ms > ${LIMIT} ms"
  [ "${POST_MS%% *}" -le "$LIMIT" ] || die "PostToolUse Read chậm: p95 ${POST_MS%% *} ms > ${LIMIT} ms"

  step "web $KEY S1: finish, Accept S1 trên trang B → aisdlc status áp dụng → steps.json ACCEPTED, web 'Đã áp dụng'"
  copy "$SOL/$KEY/S1"
  run 0 aisdlc step finish S1
  has "2/2 pass"
  web_assert "web $KEY: S1 AWAITING_DEV" "/api/features/$WREPO_ID/$KEY" \
    '[.snapshot.steps[] | select(.id=="S1" and .status=="AWAITING_DEV")] | length == 1'
  web_ctl accept STEP=S1 SHOT="${SCREENSHOTS:+$SHOTS/step-accept.png}"
  run 0 aisdlc status
  has "Áp dụng từ web: đã accept S1 (dev)"
  [ "$(step_status S1)" = ACCEPTED ] || die "steps.json: S1 phải ACCEPTED, được '$(step_status S1)'"
  jq -e '[.. | objects | select(.id=="S1" and .acceptedBy=="dev (web)")] | length == 1' \
    "$P/steps.json" >/dev/null || die "steps.json: S1 acceptedBy phải là 'dev (web)'"
  run 0 aisdlc web flush
  web_control_assert "control: ACCEPT_STEP S1 APPLIED" \
    '[.actions[] | select(.kind=="ACCEPT_STEP" and .step=="S1" and .status=="APPLIED")] | length == 1'
  web_ctl expect PAGE=gate GATE=S1 TEXT="Đã áp dụng"

  step "web $KEY cho phép file: PreToolUse ghi ngoài phạm vi bị chặn → trang B Cho phép → lần sau được phép"
  run 0 aisdlc step start S2
  OOS_IN="{\"file_path\":\"$WREPO/README2.txt\",\"content\":\"x\"}"
  hook Write "$OOS_IN"
  has '"deny"'
  has "Ngoài phạm vi S2"
  web_assert "events: SCOPE_DENIED README2.txt ở S2" "/api/features/$WREPO_ID/$KEY/events" \
    '[(if type=="array" then . else .events end)[] | select(.type=="SCOPE_DENIED" and .path=="README2.txt" and .step=="S2")] | length > 0'
  web_ctl allow STEP=S2 FILE=README2.txt SHOT="${SCREENSHOTS:+$SHOTS/step-allow.png}"
  run 0 aisdlc web flush # tải lệnh chờ về cache; hook áp dụng (không gọi mạng)
  hook Write "$OOS_IN"
  hasnt '"deny"'
  jq -e '[.. | strings | select(.=="README2.txt")] | length > 0' "$P/scope-extra.json" >/dev/null \
    || die "scope-extra.json phải có README2.txt sau khi hook áp dụng lệnh từ web"
  run 0 aisdlc web flush
  web_control_assert "control: ALLOW_SCOPE README2.txt APPLIED" \
    '[.actions[] | select(.kind=="ALLOW_SCOPE" and .path=="README2.txt" and .status=="APPLIED")] | length == 1'
  web_ctl expect PAGE=gate GATE=S2 TEXT="Đã áp dụng"

  # Lệnh hết hiệu lực. Mọi lệnh CLI có mạng (kể cả `step start S2`) làm mới điều khiển và áp dụng lệnh Accept trước
  # khi đổi gì. Nên: bấm Accept S2 khi S2 AWAITING_DEV, rồi `step start S2`, sửa code, `step finish S2` khi đang
  # backoff (ghi .aisdlc/web/backoff-until: CLI bỏ mạng, cache điều khiển chưa có lệnh S2); gỡ backoff, `aisdlc status`
  # tải lệnh về → STALE vì endTree đã khác.
  step "web $KEY lệnh hết hiệu lực: Accept S2 trên web, S2 làm tiếp ra cây code khác khi backoff → status báo hết hiệu lực"
  copy "$SOL/$KEY/S2"
  run 0 aisdlc step finish S2
  has "7/7 pass"
  web_assert "web $KEY: S2 AWAITING_DEV" "/api/features/$WREPO_ID/$KEY" \
    '[.snapshot.steps[] | select(.id=="S2" and .status=="AWAITING_DEV")] | length == 1'
  # 900 giây: đủ cho Playwright + step start/finish trên máy chậm; gỡ ngay sau finish.
  date -u -d '+900 seconds' +%Y-%m-%dT%H:%M:%SZ >"$WREPO/.aisdlc/web/backoff-until"
  wait_no_flush 10 # tiến trình gửi nền mở trước khi backoff có thể còn chạy và tải điều khiển
  web_ctl accept STEP=S2
  run 0 aisdlc step start S2
  hasnt "Áp dụng từ web"
  echo "// sửa thêm sau khi dev bấm Accept trên web" >>"$WREPO/internal/limit/service.go"
  run 0 aisdlc step finish S2
  has "7/7 pass"
  [ "$(step_status S2)" = AWAITING_DEV ] || die "steps.json: S2 phải AWAITING_DEV, được '$(step_status S2)'"
  S2_SUM="$(sha256sum "$P/steps.json")"
  rm -f "$WREPO/.aisdlc/web/backoff-until"
  run 0 aisdlc status
  has "Lệnh từ web hết hiệu lực: Bước S2 đã đổi sau khi dev bấm"
  [ "$(sha256sum "$P/steps.json")" = "$S2_SUM" ] || die "lệnh hết hiệu lực không được sửa steps.json"
  [ "$(step_status S2)" = AWAITING_DEV ] || die "steps.json: S2 phải còn AWAITING_DEV"
  run 0 aisdlc web flush
  web_control_assert "control: ACCEPT_STEP S2 STALE" \
    '[.actions[] | select(.kind=="ACCEPT_STEP" and .step=="S2" and .status=="STALE")] | length == 1'
  web_ctl expect PAGE=gate GATE=S2 TEXT="Hết hiệu lực: Bước S2 đã đổi sau khi dev bấm"
  web_ctl expect PAGE=gate GATE=S2 TEXT="— bấm lại"

  step "web $KEY web chết (S2): hook vẫn cho sửa code trong phạm vi nhờ cache; S2, eval chạy được"
  run 0 aisdlc step start S2
  web_stop
  CACHE_SUM="$(sha256sum "$CACHE")" # sau khi web tắt: không tiến trình nền nào làm mới được cache nữa
  hook Write "{\"file_path\":\"$WREPO/internal/limit/service.go\",\"content\":\"x\"}"
  hasnt '"deny"'
  copy "$SOL/$KEY/S2"
  run 0 aisdlc step finish S2
  has "7/7 pass"
  dev step accept S2
  run 0 aisdlc eval
  [ "$(head -n 1 "$P/eval/report.md")" = "Eval $KEY: ĐẠT" ] || die "report.md dòng đầu: $(head -n 1 "$P/eval/report.md")"

  step "web $KEY web chết: submit review báo lỗi rõ, không ghi cache/gates.json"
  cp "$SOL/$KEY/review-agent.json" "$P/"
  run 2 aisdlc submit review
  has "Không nộp được lên web"
  [ "$(sha256sum "$CACHE")" = "$CACHE_SUM" ] || die "submit lỗi không được sửa cache"
  [ ! -e "$P/gates.json" ] || die "web chết: không được tạo gates.json"
  web_start
  run 0 aisdlc web push

  step "web $KEY REVIEW: submit, duyệt trên trang B"
  run 0 aisdlc submit review
  web_assert "web $KEY: REVIEW chờ duyệt" /api/features/local/web-service/PAY-1 \
    '[.snapshot.gates[] | select(.gate=="REVIEW" and .state=="AWAITING_APPROVAL")] | length == 1'
  web_ui approve REVIEW
  run 0 aisdlc status
  [ "$(cache_state "$KEY" | jq -r '[.approvals[] | select(.gate=="REVIEW")][-1].channel')" = web ] \
    || die "cache: approval REVIEW phải có channel web"
  [ ! -e "$P/gates.json" ] || die "chế độ web không được tạo gates.json"
  [ "$(sha256sum "$FIX_GATES")" = "$FIX_GATES_SUM" ] || die "gates.json của FIX-2 bị sửa trong luồng web"

  step "web $KEY eval --ci hỏi approval trên web (token ci): PASS; sửa code sau duyệt → FAIL"
  rc=0
  UOUT="$(web_admin ci-token e2e 2>&1)" || rc=$?
  [ "$rc" -eq 0 ] || die "'user ci-token' exit $rc"
  CI_TOKEN="$(sed -n 's/^Token: *//p' <<<"$UOUT" | tail -n 1 | tr -d '[:space:]')"
  unset UOUT
  [ -n "$CI_TOKEN" ] || die "không lấy được token ci"
  SECRETS+=("$CI_TOKEN")
  echo "user ci-token e2e: OK (token đã ẩn)" >>"$LOG"
  git add -A && git commit -qm "$KEY (web)"
  # Token CI chỉ qua biến môi trường (không nằm trên dòng lệnh, không vào log).
  export AISDLC_CI_TOKEN="$CI_TOKEN"
  run 1 aisdlc eval --ci --base "$WBASE" # thiếu AISDLC_WEB_URL ở chế độ web → FAIL
  has "Thiếu biến AISDLC_WEB_URL hoặc AISDLC_CI_TOKEN để hỏi web (chế độ web)"
  hasnt "Chưa \`aisdlc web login\`"
  git checkout -q -- aisdlc
  export AISDLC_WEB_URL="$WEB_URL"
  run 0 aisdlc eval --ci --base "$WBASE"
  has "Eval $KEY: ĐẠT"
  has "| GATE:REVIEW | ĐẠT | đã duyệt trên web"
  has "| RISK_LLM | ĐẠT |" # chính sách lấy từ web bằng token ci (GET /api/cli/policy)
  git checkout -q -- aisdlc
  echo "// sửa sau khi duyệt REVIEW" >>"$WREPO/internal/limit/web/handler.go"
  git add -A && git commit -qm "$KEY sửa sau duyệt"
  run 1 aisdlc eval --ci --base "$WBASE"
  has "REVIEW đã đổi sau duyệt"
  git checkout -q -- aisdlc
  unset AISDLC_WEB_URL AISDLC_CI_TOKEN
  [ ! -e "$P/gates.json" ] || die "chế độ web không được tạo gates.json"

  # rewind (chế độ web): quay PAY-1 về đầu S2 (S2 đã ACCEPTED, REVIEW đã duyệt). Chạy sau eval --ci vì xoá eval/review.
  step "web $KEY rewind S2: nhập key → S2 chưa bắt đầu, S1 giữ, ref dự phòng, web 'Lần làm 2' và archive"
  git add -A && git commit -qm "$KEY trước rewind" || true
  S1_BEFORE="$(step_status S1)"
  dev rewind S2
  has "Sẽ quay $KEY về đầu S2"
  has "Đã quay $KEY về đầu S2"
  has "aisdlc step start S2"
  run 0 aisdlc status
  [ "$(step_status S2)" = NOT_STARTED ] || die "rewind S2: S2 phải NOT_STARTED, được '$(step_status S2)'"
  [ "$(step_status S1)" = "$S1_BEFORE" ] || die "rewind S2: S1 không được đổi"
  [ -n "$(git for-each-ref "refs/aisdlc/backup/$KEY/")" ] || die "rewind: thiếu ref dự phòng refs/aisdlc/backup/$KEY/"
  [ ! -e "$P/eval" ] || die "rewind S2: eval/ phải bị xoá"
  run 0 aisdlc web flush
  web_assert "web $KEY: attempt 2" "/api/features/$WREPO_ID/$KEY" '.attempt == 2'
  web_assert "web $KEY: archive có lần làm 1" "/api/features/$WREPO_ID/$KEY/attempts" \
    '(if type=="array" then . else .attempts end) | map(select(.attempt==1)) | length == 1'
  web_ctl expect PAGE=gate GATE=REVIEW TEXT="Lần làm 2" SHOT="${SCREENSHOTS:+$SHOTS/rewind-attempt.png}"
  web_ctl expect PAGE=gate GATE=REVIEW TEXT="Các lần làm trước"

  if [ -n "${SCREENSHOTS:-}" ]; then
    step "Chụp /me (token) của luồng web"
    PAGE=/me web_ui shot "" "$SHOTS/me-tokens.png"
    for f in approve-confirm me-tokens design-resubmission control-paused step-accept step-allow risk-rules risk-levels risk-feature; do
      [ -s "$SHOTS/$f.png" ] || die "thiếu ảnh $SHOTS/$f.png"
      echo "Ảnh: $SHOTS/$f.png"
    done
  fi
fi # part web

# =============================================================================================
# Quy trình theo mức rủi ro (flow): repo thứ ba local/flow-service với chính sách e2e/solutions/flow/risk-policy.yaml
# (cùng luật với fixture, có flow: low tự duyệt Thiết kế, Case và bước). Cổng BRD luôn duyệt tay. Hai repo trên giữ
# chính sách fixture không có flow (mọi cổng duyệt tay), nên các luồng ở trên không đổi.
# WEB=0: CLI tự duyệt (gates.json, approver auto). WEB=1: approvals: web, policy pull nhập chính sách có flow làm v1,
# server tự duyệt khi nhận bản nộp (channel auto); CLI thấy qua cache cổng có chữ ký.
if part flow; then
FREPO="$WORK/flow-service"
REPO="$FREPO" # hook()/copy()/step_status() dùng $REPO
unset AISDLC_WEB_URL AISDLC_WEB_TOKEN
if [ "$WEB" = 1 ] && [ ! -f "$HOME/.config/aisdlc/web.json" ]; then die "flow với WEB=1 cần phần web (web login) chạy trước"; fi

# gate_line <cổng> <regex phần sau tên cổng>: dòng cổng trong output `aisdlc status` (đang ở $OUT) khớp.
gate_line() { grep -qE "^  $1 +$2" <<<"$OUT" || die "aisdlc status: dòng cổng $1 không khớp '$2'"; }

step "flow-service: git init, chính sách có flow, setup$([ "$WEB" = 1 ] && echo ' (mặc định approvals: web), policy pull')"
# config.yaml do setup tạo (dò cmd/server và db/migrations) để thấy chế độ duyệt mặc định như repo mới thật.
new_repo "$FREPO"
git rm -q aisdlc/config.yaml
cp "$SOL/flow/risk-policy.yaml" "$FREPO/aisdlc/risk-policy.yaml"
git add -A && git commit -qm "chính sách có flow"
run 0 aisdlc setup
has "Đã tạo aisdlc/config.yaml"
grep -qx '  main: ./cmd/server' "$FREPO/aisdlc/config.yaml" || die "setup phải dò được service.main ./cmd/server"
git add -A && git commit -qm "aisdlc setup"
if [ "$WEB" = 1 ]; then
  WREPO="$FREPO" # cache_state()
  WREPO_ID="local/flow-service" # web_ui()/web_ctl()
  # Máy đã web login nên setup mặc định ghi approvals: web vào config.yaml mới.
  has "Chế độ duyệt: web"
  grep -qx 'approvals: web' "$FREPO/aisdlc/config.yaml" || die "setup không ghi approvals: web mặc định"
  run 0 aisdlc policy pull
  has "Chính sách v1 (web)"
  web_assert "policy $WREPO_ID: v1 có flow.low toàn auto, flow.high toàn manual" "/api/policy?repo=$WREPO_ID" \
    '.version == 1 and ([.policy.flow.low[]] | unique == ["auto"]) and ([.policy.flow.high[]] | unique == ["manual"])'
fi

# web_gate <cổng> <biểu thức jq trên phần tử cổng của snapshot> <mô tả>: trạng thái cổng của $KEY trên web.
web_gate() {
  web_assert "web $KEY: $3" "/api/features/$WREPO_ID/$KEY" "[.snapshot.gates[] | select(.gate==\"$1\") | $2] == [true]"
}

# auto_ok <cổng>: cổng vừa nộp đã được tự duyệt ở mức low. WEB=0: CLI in và ghi gates.json (approver auto, rule
# flow:low). WEB=1: server ghi (channel auto), `aisdlc status` làm mới cache có chữ ký thấy channel auto.
auto_ok() {
  local g=$1 lower
  lower="$(tr '[:upper:]' '[:lower:]' <<<"$g")"
  hasnt "aisdlc approve $lower"
  if [ "$WEB" = 1 ]; then
    has "Đã nộp cổng $g lên web, không có phát hiện: tự duyệt (mức low)." # phong bì trả về đã có bản tự duyệt
    web_gate "$g" '.state=="APPROVED" and .approval.label=="tự duyệt (mức low)"' "$g tự duyệt trên server"
    run 0 aisdlc status
    [ "$(cache_state "$KEY" | jq -r --arg g "$g" '[.approvals[] | select(.gate==$g)][-1] | "\(.channel) \(.rule)"')" \
      = "auto flow:low" ] || die "cache: approval $g phải có channel auto, rule flow:low"
  else
    has "Đã nộp cổng $g, không có phát hiện: tự duyệt (mức low)."
    [ "$(jq -r --arg g "$g" '.approvals[$g] | "\(.approver) \(.rule)"' "$P/gates.json")" = "auto flow:low" ] \
      || die "gates.json: approval $g phải có approver auto, rule flow:low"
  fi
}

# ---------------------------------------------------------------------------------------------
KEY=FEE-3
P="$FREPO/aisdlc/features/$KEY"
step "$KEY mức thấp: BRD chờ dev duyệt (luôn duyệt tay), rồi Thiết kế → Case → S1 → eval chạy một mạch, không step accept"
run 0 aisdlc init "$KEY" --title "Làm tròn phí tới đồng gần nhất"
cp "$SOL/$KEY/brd.md" "$P/brd.md"
run 0 aisdlc submit brd
has "Đã nộp cổng BRD"
hasnt "tự duyệt"
hasnt "Rủi ro"
run 0 aisdlc status
gate_line BRD AWAITING_APPROVAL
dev approve brd
has "Đã duyệt cổng BRD"
cp "$SOL/$KEY/design.md" "$SOL/$KEY/design.yaml" "$P/"
risk_agent
run 0 aisdlc submit design
has "Rủi ro: low"
has "không rule nào khớp"
[ "$(risk_level "$P")" = low ] || die "risk.yaml: level phải là low, được '$(risk_level "$P")'"
auto_ok DESIGN
cp "$SOL/$KEY/cases.yaml" "$P/cases.yaml"
run 0 aisdlc submit cases
auto_ok CASES
run 0 aisdlc step start S1
copy "$SOL/FIX-2/S1" # cùng sửa fee.go như FIX-2
run 0 aisdlc step finish S1
has "3/3 pass"
has "Đã chấp nhận bước S1: tự duyệt (mức low)."
[ "$(step_status S1)" = ACCEPTED ] || die "steps.json: S1 phải ACCEPTED, được '$(step_status S1)'"
run 0 aisdlc eval
[ "$(head -n 1 "$P/eval/report.md")" = "Eval $KEY: ĐẠT" ] || die "report.md dòng đầu: $(head -n 1 "$P/eval/report.md")"
cp "$SOL/$KEY/review-agent.json" "$P/"
run 0 aisdlc submit review
has "Đã nộp cổng REVIEW"
hasnt "tự duyệt"

step "$KEY cuối luồng: chỉ cổng Review chờ dev duyệt; dev duyệt Review$([ "$WEB" = 1 ] && echo ' trên trang B, web hiện tự duyệt')"
run 0 aisdlc status
gate_line BRD APPROVED
for g in DESIGN CASES; do gate_line "$g" "APPROVED +tự duyệt \\(mức low\\)"; done
gate_line REVIEW AWAITING_APPROVAL
if [ "$WEB" = 1 ]; then
  web_gate REVIEW '.state=="AWAITING_APPROVAL"' "chỉ REVIEW chờ duyệt"
  web_gate BRD '.state=="APPROVED" and .approval.approver!="auto" and .approval.label==null' "BRD do dev duyệt, không tự duyệt"
  web_ctl expect PAGE=gate GATE=DESIGN TEXT="tự duyệt (mức low)" SHOT="${SCREENSHOTS:+$SHOTS/gate-auto.png}"
  web_ui approve REVIEW
  run 0 aisdlc status
  [ "$(cache_state "$KEY" | jq -r '[.approvals[] | select(.gate=="REVIEW")][-1].channel')" = web ] \
    || die "cache: approval REVIEW phải có channel web"
  [ ! -e "$P/gates.json" ] || die "chế độ web không được tạo gates.json"
  if [ -n "${SCREENSHOTS:-}" ]; then
    [ -s "$SHOTS/gate-auto.png" ] || die "thiếu ảnh $SHOTS/gate-auto.png"
    echo "Ảnh: $SHOTS/gate-auto.png"
  fi
else
  [ "$(jq -r '.approvals.BRD.approver' "$P/gates.json")" = "Dev E2E" ] || die "gates.json: BRD phải do dev duyệt"
  dev approve review
  has "Đã duyệt cổng REVIEW"
  [ "$(jq -r '.approvals.REVIEW.approver' "$P/gates.json")" = "Dev E2E" ] || die "gates.json: REVIEW phải do dev duyệt"
fi
run 0 aisdlc status
gate_line REVIEW APPROVED
git add -A && git commit -qm "$KEY"

# ---------------------------------------------------------------------------------------------
KEY=PAY-1
P="$FREPO/aisdlc/features/$KEY"
step "$KEY mức high: BRD chờ dev duyệt; subagent xếp high (E1) ở submit design → Thiết kế chờ dev, không tự duyệt"
run 0 aisdlc init "$KEY" --title "Kiểm tra hạn mức ngày"
cp "$SOL/$KEY/brd.md" "$P/brd.md"
run 0 aisdlc submit brd
has "Đã nộp cổng BRD"
hasnt "tự duyệt"
run 0 aisdlc status
gate_line BRD AWAITING_APPROVAL
hook Write "{\"file_path\":\"$FREPO/internal/limit/web/handler.go\",\"content\":\"x\"}"
has '"deny"'
has "cổng BRD chưa được duyệt" # chính BRD (cổng đầu chưa mở) chặn agent viết code
dev approve brd
has "Đã duyệt cổng BRD"
cp "$SOL/$KEY/design.md" "$SOL/$KEY/design.yaml" "$P/"
cp "$SOL/$KEY/db/"*.sql "$P/db/"
risk_agent
run 0 aisdlc submit design
has "Rủi ro: high"
has "E1 ("
hasnt "tự duyệt"
[ "$(risk_level "$P")" = high ] || die "risk.yaml: level phải là high, được '$(risk_level "$P")'"
if [ "$WEB" = 1 ]; then
  web_gate DESIGN '.state=="AWAITING_APPROVAL" and .approval==null' "DESIGN chờ dev duyệt (mức high)"
else
  [ "$(jq -r '.approvals.DESIGN // "null"' "$P/gates.json")" = null ] || die "gates.json: DESIGN không được tự duyệt"
fi
run 0 aisdlc status
gate_line BRD APPROVED
gate_line DESIGN AWAITING_APPROVAL

step "$KEY dev duyệt DESIGN"
dev approve design
has "Đã duyệt cổng DESIGN"
if [ "$WEB" = 1 ]; then
  web_gate DESIGN '.state=="APPROVED"' "DESIGN đã duyệt"
  [ "$(cache_state "$KEY" | jq -r '[.approvals[] | select(.gate=="BRD")][-1].channel')" = terminal ] \
    || die "cache: approval BRD phải có channel terminal"
else
  [ "$(jq -r '[.approvals.BRD.approver, .approvals.DESIGN.approver] | join(",")' "$P/gates.json")" = "Dev E2E,Dev E2E" ] \
    || die "gates.json: BRD và DESIGN phải do dev duyệt"
fi
run 0 aisdlc status
gate_line DESIGN APPROVED
fi # part flow

# ---------------------------------------------------------------------------------------------
# Nhiều session song song và duyệt trong Claude Code (sessionApprove). Repo riêng, không cần Docker/web.
if part session; then
SREPO="$WORK/session-service"
unset AISDLC_WEB_URL AISDLC_WEB_TOKEN # repo duyệt local, không gửi web (phần local có thể đã đặt)
# sess_hook <session_id> <permission_mode> <lệnh bash>: PreToolUse Bash của Claude Code (đúng JSON hook gửi).
sess_hook() {
  local json
  json="$(printf '{"session_id":"%s","hook_event_name":"PreToolUse","permission_mode":"%s","cwd":"%s","tool_name":"Bash","tool_input":{"command":"%s"}}' \
    "$1" "$2" "$SREPO" "$3")"
  run 0 bash -c 'printf "%s" "$1" | aisdlc hook pre-tool-use' _ "$json"
}
# gate_line <cổng> <regex phần sau tên cổng>: dòng cổng trong output `aisdlc status` (đang ở $OUT) khớp.
gate_line() { grep -qE "^  $1 +$2" <<<"$OUT" || die "aisdlc status: dòng cổng $1 không khớp '$2'"; }

step "Session: repo mới, sessionApprove bật ở baseRef (main), hai tính năng"
new_repo "$SREPO"
run 0 aisdlc setup --approvals local
if grep -q '^baseRef:' aisdlc/config.yaml; then sed -i 's#^baseRef:.*#baseRef: main#' aisdlc/config.yaml
else echo "baseRef: main" >>aisdlc/config.yaml; fi
echo "sessionApprove: true" >>aisdlc/config.yaml
git add -A && git commit -qm "aisdlc setup, sessionApprove"
AISDLC_SESSION=sa run 0 aisdlc init SES-1 --title "Tính năng một"
AISDLC_SESSION=sb run 0 aisdlc init SES-2 --title "Tính năng hai"
cp "$SOL/PAY-1/brd.md" "$SREPO/aisdlc/features/SES-1/brd.md"
cp "$SOL/PAY-1/brd.md" "$SREPO/aisdlc/features/SES-2/brd.md"

step "Session: hai AISDLC_SESSION chạy song song trên cùng thư mục, mỗi bên đúng tính năng của mình"
AISDLC_SESSION=sa aisdlc submit brd >"$WORK/par-a.out" 2>&1 & PA=$!
AISDLC_SESSION=sb aisdlc submit brd >"$WORK/par-b.out" 2>&1 & PB=$!
wait "$PA" || die "session sa: submit brd lỗi: $(cat "$WORK/par-a.out")"
wait "$PB" || die "session sb: submit brd lỗi: $(cat "$WORK/par-b.out")"
cat "$WORK/par-a.out" "$WORK/par-b.out" >>"$LOG"
head -n 1 "$WORK/par-a.out" | grep -qF "[SES-1]" || die "session sa phải thao tác trên SES-1: $(head -n 1 "$WORK/par-a.out")"
head -n 1 "$WORK/par-b.out" | grep -qF "[SES-2]" || die "session sb phải thao tác trên SES-2: $(head -n 1 "$WORK/par-b.out")"
AISDLC_SESSION=sa run 0 aisdlc status
has "[SES-1]"
hasnt "SES-2"
has "Duyệt trong Claude Code: bật"
AISDLC_SESSION=sb run 0 aisdlc status
has "[SES-2]"
hasnt "SES-1"
run 0 aisdlc status --feature SES-1
has "[SES-1]"

step "Session: không phiếu, không console → exit 3 (agent không tự duyệt)"
AISDLC_SESSION=sa run 3 aisdlc approve brd </dev/null
has "Chạy lại lệnh duyệt một mình"

step "Session: chế độ quyền không hỏi người và dạng gián tiếp bị deny, lệnh đơn được hỏi (ask)"
sess_hook sa bypassPermissions "aisdlc approve brd"
has '"deny"'
has "chế độ quyền không hỏi người"
sess_hook sa default "bash -c 'aisdlc approve brd'"
has '"deny"'
has "gọi thẳng"
[ ! -e "$SREPO/.aisdlc/grants/sa.json" ] || die "deny không được ghi phiếu"
sess_hook sa default "aisdlc approve brd"
has '"ask"'
has "[SES-1]"
[ -e "$SREPO/.aisdlc/grants/sa.json" ] || die "ask phải ghi phiếu của session sa"

step "Session: phiếu của sa không dùng được cho session khác; sa dùng phiếu → duyệt SES-1, phiếu dùng một lần"
AISDLC_SESSION=sb run 3 aisdlc approve brd </dev/null
has "[SES-2]"
has "Không có xác nhận trong Claude Code (hoặc đã hết hạn)."
hasnt "Đã duyệt cổng"
AISDLC_SESSION=sa run 0 aisdlc approve brd </dev/null
has "Đã duyệt cổng BRD"
AISDLC_SESSION=sa run 0 aisdlc status
gate_line BRD "APPROVED +duyệt trong Claude Code"
AISDLC_SESSION=sb run 0 aisdlc status
gate_line BRD AWAITING_APPROVAL
AISDLC_SESSION=sa run 2 aisdlc approve brd </dev/null # cổng đã duyệt, phiếu đã dùng
has "Cổng BRD đang ở trạng thái APPROVED"

step "Chính sách schema 1 (luật so khớp cũ): risk inputs báo lỗi kèm hướng dẫn, policy migrate in diff rồi --write"
cat >"$SREPO/aisdlc/risk-policy.yaml" <<'YAML'
schema: 1
preset: { name: generic, version: "1" }
levels: [low, medium, high]
default: low
rules:
  - { id: G1, kind: label, match: money-movement, level: high, note: Có dòng tiền hoặc giá trị đi qua }
  - { id: E1, kind: keyword, match: "hạn mức", level: high, note: Chặn giao dịch theo hạn mức }
classification:
  agent: raise
  devLower: reason
YAML
if [ -f "$HOME/.config/aisdlc/web.json" ]; then
  # máy đã web login (phần web đã chạy): chính sách lấy từ web, migrate chỉ nhắc chuyển trên trang chính sách
  run 2 aisdlc policy migrate
  has "Chế độ web: chuyển chính sách trên trang chính sách của web."
fi
# HOME chưa web login: nguồn chính sách là file trong repo
mkdir -p "$WORK/home-noweb"
NOWEB=(env HOME="$WORK/home-noweb")
run 2 "${NOWEB[@]}" aisdlc risk inputs --feature SES-1
has "aisdlc policy migrate"
run 0 "${NOWEB[@]}" aisdlc policy migrate
has "SCHEMA_MIGRATED"
has "Chặn giao dịch theo hạn mức (từ khóa cũ: hạn mức)"
has "--write"
grep -q '^schema: 1' "$SREPO/aisdlc/risk-policy.yaml" || die "policy migrate không --write không được ghi file"
run 0 "${NOWEB[@]}" aisdlc policy migrate --write
grep -q '^schema: 2' "$SREPO/aisdlc/risk-policy.yaml" || die "policy migrate --write phải ghi schema: 2"
run 0 "${NOWEB[@]}" aisdlc risk inputs --feature SES-1
has "inputsHash: "
git checkout -q -- aisdlc/risk-policy.yaml
cd "$E2E"
fi # part session

# ---------------------------------------------------------------------------------------------
# Tiếng Anh (spec i18n, mục Kiểm thử): một lượt luồng nhẹ FIX-2 trên repo `locale: en` (chính sách rủi ro, case và
# kết quả subagent giả lập bằng tiếng Anh ở e2e/solutions/FIX-2-en). Mọi output CLI và eval/report.md không được có
# ký tự tiếng Việt. WEB=1: người dùng chọn EN trên web (bộ chọn EN/VI) → UI tiếng Anh, lưu users.locale, phiên mới
# cũng tiếng Anh; xong đặt lại VI.
if part en; then
EREPO="$WORK/en-service"
REPO="$EREPO" # copy() dùng $REPO
unset AISDLC_WEB_URL AISDLC_WEB_TOKEN # repo duyệt local, không gửi web (phần local có thể đã đặt)
EN_SOL="$SOL/FIX-2-en"
# vi_grep <văn bản>: grep -P tìm chữ Latin có dấu (U+00C0–U+1EF9: mọi ký tự tiếng Việt có dấu, cả dạng tổ hợp NFD vì
# chữ gốc + dấu U+0300–U+036F nằm trong dải); in các dòng khớp. Exit 0 = có, 1 = không, khác = grep lỗi (không PCRE).
vi_grep() { LC_ALL=C.UTF-8 grep -nP '[\x{00C0}-\x{1EF9}]' <<<"$1"; }
# no_vi <mô tả> <văn bản>: văn bản không có tiếng Việt; grep lỗi thì die, không bao giờ coi là sạch.
no_vi() {
  local hit rc=0
  hit="$(vi_grep "$2")" || rc=$?
  case "$rc" in
    0) die "$1 có ký tự tiếng Việt: $(head -n 3 <<<"$hit")" ;;
    1) ;;
    *) die "no_vi: grep -P lỗi (exit $rc; cần grep có PCRE)" ;;
  esac
}
# Đối chứng: vi_grep phải bắt được tiếng Việt (cả NFD) và để yên ASCII, không thì mọi kiểm tra dưới đây vô nghĩa.
rc=0; vi_grep "Cổng" >/dev/null || rc=$?
[ "$rc" = 0 ] || die "no_vi: không bắt được tiếng Việt (grep exit $rc)"
rc=0; vi_grep "$(printf 'Vie\xcc\xa3\xcc\x82t')" >/dev/null || rc=$?
[ "$rc" = 0 ] || die "no_vi: không bắt được tiếng Việt dạng NFD (grep exit $rc)"
no_vi "đối chứng ASCII" "plain ASCII — arrows → ok"
# en <exit> <lệnh...>: như run, rồi output không được có tiếng Việt.
en() { run "$@"; no_vi "output của '${*:2}'" "$OUT"; }
# en_dev <đối số aisdlc...>: lệnh chỉ dev qua pty (như dev), output không có tiếng Việt.
en_dev() { dev "$@"; no_vi "output của 'aisdlc $*'" "$OUT"; }

step "EN: repo locale: en (config.yaml), chính sách rủi ro tiếng Anh, setup in tiếng Anh"
new_repo "$EREPO"
printf 'locale: en\n' >>aisdlc/config.yaml
cp "$EN_SOL/risk-policy.yaml" aisdlc/risk-policy.yaml
git add -A && git commit -qm "locale: en"
en 0 aisdlc setup --approvals local
has "Kept aisdlc/config.yaml"
git add -A && git commit -qm "aisdlc setup"
EN_SETUP="$(git rev-parse HEAD)"
en 2 aisdlc status # chưa có tính năng: exit 2, vẫn in dòng locale
has "Locale: en"

KEY=FIX-2
P="$EREPO/aisdlc/features/$KEY"
step "EN $KEY init (LITE), cases: subagent xếp loại, submit, review, approve"
en 0 aisdlc init "$KEY" --lite --title "Fix fee rounding"
cp "$EN_SOL/cases.yaml" "$P/cases.yaml"
run 0 aisdlc risk inputs --feature "$KEY"
no_vi "output của 'aisdlc risk inputs'" "$OUT"
hash="$(sed -n 's/^inputsHash: *//p' <<<"$OUT")"
[ -n "$hash" ] || die "aisdlc risk inputs không in inputsHash"
jq --arg h "$hash" '.inputsHash = $h' "$EN_SOL/risk-agent.json" >"$P/risk-agent.json"
en 0 aisdlc submit cases
has "Risk: low"
en 0 aisdlc review cases
hasnt "[HIGH]"
hasnt "[WARN]"
en_dev approve cases

step "EN $KEY S1: start, finish (pass), accept"
en 0 aisdlc step start S1
copy "$SOL/FIX-2/S1"
en 0 aisdlc step finish S1
has "3/3 pass"
en_dev step accept S1

step "EN $KEY eval, review: eval/report.md tiếng Anh"
en 0 aisdlc eval
[ "$(head -n 1 "$P/eval/report.md")" = "Eval $KEY: PASS" ] || die "report.md dòng đầu: $(head -n 1 "$P/eval/report.md")"
no_vi "eval/report.md" "$(cat "$P/eval/report.md")"
cp "$EN_SOL/review-agent.json" "$P/"
en 0 aisdlc submit review
en_dev approve review
en 0 aisdlc status

step "EN $KEY commit (MR) và eval --ci so với commit setup"
git add -A && git commit -qm "$KEY"
en 0 aisdlc eval --ci --base "$EN_SETUP"
has "Eval $KEY: PASS"
# chỉ report.md: diff-by-step.md trích nguyên văn code của dịch vụ (comment tiếng Việt của fixture), không phải chữ CLI
no_vi "eval/report.md sau eval --ci" "$(cat "$P/eval/report.md")"

if [ "$WEB" = 1 ]; then
  step "EN web: chọn EN trên bộ chọn EN/VI → UI tiếng Anh, users.locale = en, phiên mới cũng tiếng Anh; đặt lại VI"
  pw_install
  WEB_URL="$WEB_URL" WEB_TOKEN="$WEB_TOKEN" node "$PW_DIR/web-locale.mjs" >>"$LOG" 2>&1 \
    || die "Playwright: web-locale thất bại (xem log)"
fi
cd "$E2E"
fi # part en

[ -z "$LAT_SUMMARY" ] || printf '%s' "$LAT_SUMMARY" | tee -a "$LOG"
step "xong"
if log_has_secret; then die "token đã lọt vào $LOG"; fi
echo "E2E PASS"
