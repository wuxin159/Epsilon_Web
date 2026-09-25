#!/usr/bin/env bash
# ============================================================
# Epsilon 增量部署 —— 在服务器上运行，拉最新代码并重启
#
# 用法 (在服务器上):
#   bash /opt/epsilon-src/scripts/server-update.sh
#
# 或从本地一键触发:
#   ssh root@<你的服务器IP> 'bash /opt/epsilon-src/scripts/server-update.sh'
# ============================================================
set -euo pipefail

SRC_DIR="${SRC_DIR:-/opt/epsilon-src}"
APP_DIR="${APP_DIR:-/opt/epsilon}"
export PATH=$PATH:/usr/local/go/bin
export GOPROXY="https://goproxy.cn,direct"
export GOSUMDB="sum.golang.google.cn"

log() { echo "==> $*"; }

log "拉取最新代码"
cd "$SRC_DIR"
git fetch --all --prune
git reset --hard origin/main

COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILT_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X main.buildCommit=$COMMIT -X main.buildTime=$BUILT_AT"
log "编译 (commit=$COMMIT, built=$BUILT_AT)"
CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$APP_DIR/epsilon.new"        ./cmd/server
CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$APP_DIR/epsilon-admin.new"  ./cmd/admin

log "热替换二进制"
mv "$APP_DIR/epsilon.new"       "$APP_DIR/epsilon"
mv "$APP_DIR/epsilon-admin.new" "$APP_DIR/epsilon-admin"
chmod +x "$APP_DIR/epsilon" "$APP_DIR/epsilon-admin"

log "重启服务"
systemctl restart epsilon
sleep 1

log "状态:"
systemctl status epsilon --no-pager | head -8

log "健康检查:"
curl -sf http://127.0.0.1:8080/health && echo "  OK" || echo "  FAIL — 去 journalctl -u epsilon -n 30 看日志"
