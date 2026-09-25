#!/usr/bin/env bash
# ============================================================
# 本地一键部署 —— push GitHub + 触发服务器拉取&重启
#
# 用法:
#   ./scripts/deploy.sh                       # 用默认服务器
#   SERVER=root@x.x.x.x ./scripts/deploy.sh   # 指定别的机器
# ============================================================
set -euo pipefail

: "${SERVER:=root@YOUR_SERVER_IP}"
: "${SSH_PORT:=22}"

# 检查有没有未提交的东西
if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "!! 有未提交的改动，请先 git commit"
    git status --short
    exit 1
fi

echo "==> git push"
git push origin main

echo "==> 触发服务器更新 ($SERVER)"
ssh -p "$SSH_PORT" "$SERVER" 'bash /opt/epsilon-src/scripts/server-update.sh'

echo "==> 完成"
