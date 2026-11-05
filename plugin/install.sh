#!/usr/bin/env bash
# Cài plugin AISDLC: build binary Go cho máy hiện tại vào plugin/lib, symlink lệnh `aisdlc` cho dev.
set -euo pipefail
ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
cd "$ROOT"
if command -v go >/dev/null 2>&1; then
  OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"
  mkdir -p plugin/lib
  CGO_ENABLED=0 go build -o "plugin/lib/aisdlc-$OS-$ARCH" ./cmd/aisdlc
elif ! ls plugin/lib/aisdlc-* >/dev/null 2>&1; then
  echo "AISDLC: không có Go và chưa có binary trong plugin/lib. Chạy 'make plugin-dist' trên máy có Go." >&2
  exit 1
fi
mkdir -p "$HOME/.local/bin"
chmod +x plugin/bin/aisdlc
ln -sf "$ROOT/plugin/bin/aisdlc" "$HOME/.local/bin/aisdlc"
echo "Đã cài lệnh aisdlc tại ~/.local/bin/aisdlc (đảm bảo thư mục này nằm trong PATH)."
echo "Dev chạy lệnh duyệt (approve/reject/reopen/step accept/scope allow) trong terminal riêng bằng lệnh này."
echo "Trong Claude Code chạy:"
echo "  /plugin marketplace add $ROOT"
echo "  /plugin install aisdlc@aisdlc"
