#!/usr/bin/env bash
# 交叉编译成 Linux amd64 二进制 (在 Windows Git Bash 里也能跑)
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p bin

echo "==> build server (linux/amd64)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags="-s -w" \
  -o bin/epsilon-linux ./cmd/server

echo "==> build admin  (linux/amd64)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags="-s -w" \
  -o bin/epsilon-admin-linux ./cmd/admin

echo "==> done"
ls -lh bin/
