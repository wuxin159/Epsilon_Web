#!/usr/bin/env bash
# ============================================================
# 本地一键部署 —— push GitHub + 触发服务器拉取&重启
#
# 用法:
#   ./scripts/deploy.sh                       # 用默认服务器
#   SERVER=root@x.x.x.x ./scripts/deploy.sh   # 指定别的机器
# ============================================================
set -euo pipefail

# 服务器地址：默认从环境变量读，也可以写入 .deploy.env (gitignored)
if [ -f "$(dirname "$0")/../.deploy.env" ]; then
    # shellcheck disable=SC1091
    source "$(dirname "$0")/../.deploy.env"
fi
: "${SERVER:?请设置 SERVER 环境变量，或在项目根建 .deploy.env 写 SERVER=root@x.x.x.x}"
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
