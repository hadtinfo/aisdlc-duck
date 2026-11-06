#!/usr/bin/env bash
# Cài plugin AISDLC: build hai binary Go cho máy hiện tại vào plugin/lib (aisdlc: CLI và hook; aisdlc-harness: harness
# mà aisdlc gọi khi chạy case/eval, tìm cạnh binary aisdlc), symlink lệnh `aisdlc` cho dev.
set -euo pipefail
ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
cd "$ROOT"
# Nền tảng như plugin/bin/aisdlc chọn binary lúc chạy.
OS="$(uname -s | tr 'A-Z' 'a-z')"
case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) ARCH="$(uname -m)" ;; esac
if command -v go >/dev/null 2>&1; then
  mkdir -p plugin/lib
  CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -o "plugin/lib/aisdlc-$OS-$ARCH" ./cmd/aisdlc
  CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -o "plugin/lib/aisdlc-harness-$OS-$ARCH" ./cmd/aisdlc-harness
fi
# Cả hai binary cho nền tảng này phải có (thiếu aisdlc-harness thì mọi case ERROR, thiếu aisdlc thì hook chặn ghi).
for b in "aisdlc-$OS-$ARCH" "aisdlc-harness-$OS-$ARCH"; do
  if [ ! -x "plugin/lib/$b" ]; then
    echo "AISDLC: thiếu plugin/lib/$b cho $OS/$ARCH. Cài Go rồi chạy lại, hoặc chạy 'make plugin-dist' trên máy có Go." >&2
    exit 1
  fi
done
mkdir -p "$HOME/.local/bin"
chmod +x plugin/bin/aisdlc
ln -sf "$ROOT/plugin/bin/aisdlc" "$HOME/.local/bin/aisdlc"
echo "Đã cài lệnh aisdlc tại ~/.local/bin/aisdlc (đảm bảo thư mục này nằm trong PATH)."
echo "Dev chạy lệnh duyệt (approve/reject/reopen/step accept/scope allow) trong terminal riêng bằng lệnh này."
echo "Trong Claude Code chạy:"
echo "  /plugin marketplace add $ROOT"
echo "  /plugin install aisdlc@aisdlc"
