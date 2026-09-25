#!/usr/bin/env bash
# 一键部署到远端服务器。使用前请设置 SERVER 环境变量或修改默认值。
set -euo pipefail

: "${SERVER:=root@YOUR_SERVER_IP}"
: "${SSH_PORT:=22}"
: "${REMOTE_DIR:=/opt/epsilon}"

cd "$(dirname "$0")/.."

echo "==> build"
./scripts/build.sh

echo "==> ensure remote dirs"
ssh -p "$SSH_PORT" "$SERVER" "mkdir -p $REMOTE_DIR/configs $REMOTE_DIR/data/downloads /var/log/epsilon"

echo "==> upload binaries"
scp -P "$SSH_PORT" bin/epsilon-linux       "$SERVER":"$REMOTE_DIR/epsilon.new"
scp -P "$SSH_PORT" bin/epsilon-admin-linux "$SERVER":"$REMOTE_DIR/epsilon-admin"

echo "==> upload systemd unit"
scp -P "$SSH_PORT" deploy/epsilon.service  "$SERVER":/etc/systemd/system/epsilon.service

echo "==> upload config example (只在远端不存在真实 config 时才生效)"
scp -P "$SSH_PORT" configs/config.example.yaml "$SERVER":"$REMOTE_DIR/configs/config.example.yaml"

echo "==> restart"
ssh -p "$SSH_PORT" "$SERVER" "
  set -e
  chmod +x $REMOTE_DIR/epsilon.new $REMOTE_DIR/epsilon-admin
  mv $REMOTE_DIR/epsilon.new $REMOTE_DIR/epsilon
  if [ ! -f $REMOTE_DIR/configs/config.yaml ]; then
    echo '!! 远端 configs/config.yaml 不存在，请先编辑 config.example.yaml 并另存为 config.yaml'
    exit 2
  fi
  systemctl daemon-reload
  systemctl enable epsilon
  systemctl restart epsilon
  sleep 1
  systemctl status epsilon --no-pager | head -20
"

echo "==> deploy ok"
