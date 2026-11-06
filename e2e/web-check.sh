#!/usr/bin/env bash
# Hàm kiểm web cho run-e2e.sh (source, không chạy riêng). Cần: curl, jq; die() và biến WEB_URL, WEB_TOKEN.
# Dùng đường dẫn thuần (middleware xác thực coi URI có '%', ';', '//', '/.' là không hợp lệ).

# web_get <đường dẫn>: in JSON của API đọc (xác thực bằng token qua header Bearer).
web_get() { curl -fsS -H "Authorization: Bearer $WEB_TOKEN" "$WEB_URL$1"; }

# web_wait_ready: chờ /api/login trả 400/401 (tối đa 90 giây).
web_wait_ready() {
  local i code
  for i in $(seq 1 90); do
    code="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$WEB_URL/api/login" || true)"
    case "$code" in 400|401) return 0 ;; esac
    sleep 1
  done
  die "web không sẵn sàng sau 90 giây (mã cuối: $code)"
}

# web_assert <mô tả> <đường dẫn> <biểu thức jq trả true>: thử lại tối đa 15 giây (event gửi bất đồng bộ).
web_assert() {
  local what=$1 path=$2 expr=$3 i json=""
  for i in $(seq 1 15); do
    json="$(web_get "$path" 2>/dev/null || true)"
    if [ -n "$json" ] && [ "$(jq -r "$expr" <<<"$json" 2>/dev/null)" = "true" ]; then
      echo "web OK: $what" | tee -a "$LOG"; return 0
    fi
    sleep 1
  done
  printf 'web JSON cuối (%s):\n%s\n' "$path" "$json" >>"$LOG"
  die "web: $what (jq: $expr)"
}

# web_card <key>: biểu thức jq chọn thẻ của feature trên /api/board.
web_card() { printf '.cards[] | select(.key=="%s")' "$1"; }

# ---- Duyệt trên web (approvals: web) ----
# Token chỉ đi qua stdin của curl (printf là builtin: không lộ trong danh sách tiến trình), không bao giờ ra $LOG.

# web_session: đăng nhập bằng token login $WEB_TOKEN; cookie phiên vào $WEB_JAR, CSRF vào $WEB_CSRF.
web_session() {
  WEB_JAR="$WORK/web-cookies"
  local body
  body="$(printf '{"token":"%s"}' "$WEB_TOKEN" | curl -fsS -c "$WEB_JAR" -H 'Content-Type: application/json' \
    --data-binary @- "$WEB_URL/api/login")" || die "web: đăng nhập phiên bằng token login thất bại"
  WEB_CSRF="$(jq -r '.csrf // empty' <<<"$body")"
  [ -n "$WEB_CSRF" ] || die "web: /api/login không trả csrf"
}

# web_token_create <kind> <label>: tạo token của chính mình qua /api/me/tokens (phiên + CSRF); in token ra stdout.
web_token_create() {
  curl -fsS -b "$WEB_JAR" -H "X-CSRF-Token: $WEB_CSRF" -H 'Content-Type: application/json' \
    --data-binary "{\"kind\":\"$1\",\"label\":\"$2\"}" "$WEB_URL/api/me/tokens" | jq -r '.token // empty'
}

# web_status_with <token> <method> <đường dẫn> [body]: chỉ in mã HTTP (token qua header đọc từ stdin của curl).
web_status_with() {
  local token=$1 method=$2 path=$3 body=${4:-'{}'}
  printf 'header = "Authorization: Bearer %s"\n' "$token" | curl -s -o /dev/null -w '%{http_code}' -K - \
    -X "$method" -H 'Content-Type: application/json' --data-binary "$body" "$WEB_URL$path"
}

# web_login_status <token>: mã HTTP của POST /api/login với token này.
web_login_status() {
  printf '{"token":"%s"}' "$1" | curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
    --data-binary @- "$WEB_URL/api/login"
}

# web_sql <câu SQL>: chạy trên container Postgres của web ($PG_CTR), in kết quả dạng thô (-tA).
web_sql() { docker exec "$PG_CTR" psql -U aisdlc -d aisdlc -tAc "$1"; }

# cache_state <KEY>: payload đã giải base64 của phong bì trạng thái cổng trong cache CLI (không xác minh chữ ký).
cache_state() { jq -r '.payload | @base64d' "$WREPO/.aisdlc/web/gates/$1.json"; }

# ---- Điều khiển agent từ web (2b-2) ----

# web_control_assert <mô tả> <biểu thức jq trên GET /api/features/<repo>/<key>/control> (repo $WREPO_ID, tính năng $KEY).
web_control_assert() { web_assert "$1" "/api/features/$WREPO_ID/$KEY/control" "$2"; }
