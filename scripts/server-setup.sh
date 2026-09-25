#!/usr/bin/env bash
# ============================================================
# Epsilon 首次部署脚本 —— 在服务器 root 用户下运行一次
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/wuxin159/Epsilon_Web/main/scripts/server-setup.sh | bash
#   或者:
#   git clone https://github.com/wuxin159/Epsilon_Web.git /opt/epsilon-src
#   bash /opt/epsilon-src/scripts/server-setup.sh
# ============================================================
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/wuxin159/Epsilon_Web.git}"
SRC_DIR="${SRC_DIR:-/opt/epsilon-src}"
APP_DIR="${APP_DIR:-/opt/epsilon}"
GO_VERSION="${GO_VERSION:-1.22.5}"

log() { echo "==> $*"; }

# ---------- 依赖 ----------
log "安装基础工具 (git curl openssl)"
apt-get update -qq
apt-get install -y -qq git curl openssl ca-certificates >/dev/null

# ---------- Go ----------
if ! command -v /usr/local/go/bin/go &>/dev/null; then
    log "安装 Go ${GO_VERSION}"
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm -f /tmp/go.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
fi
export PATH=$PATH:/usr/local/go/bin
log "Go 版本: $(go version)"

# ---------- 拉代码 ----------
if [ -d "$SRC_DIR/.git" ]; then
    log "更新已有仓库 $SRC_DIR"
    git -C "$SRC_DIR" pull --ff-only
else
    log "克隆仓库到 $SRC_DIR"
    git clone "$REPO_URL" "$SRC_DIR"
fi

# ---------- 目录 ----------
mkdir -p "$APP_DIR/configs" "$APP_DIR/data/downloads" /var/log/epsilon

# ---------- 编译 ----------
cd "$SRC_DIR"
log "编译 server"
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$APP_DIR/epsilon"       ./cmd/server
log "编译 admin"
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$APP_DIR/epsilon-admin" ./cmd/admin
chmod +x "$APP_DIR/epsilon" "$APP_DIR/epsilon-admin"

# ---------- 首次生成 config.yaml ----------
if [ ! -f "$APP_DIR/configs/config.yaml" ]; then
    SECRET=$(openssl rand -hex 32)
    ADMIN_PASS=$(openssl rand -base64 18 | tr -d '=+/' | cut -c1-24)
    cat > "$APP_DIR/configs/config.yaml" <<EOF
server:
  addr: ":8080"
  mode: "release"

database:
  path: "$APP_DIR/data/epsilon.db"

auth:
  secret: "$SECRET"
  timestamp_window: 300

download:
  root_dir: "$APP_DIR/data/downloads"

admin:
  username: "wuxin"
  password: "$ADMIN_PASS"
EOF
    chmod 600 "$APP_DIR/configs/config.yaml"
    log "已生成 $APP_DIR/configs/config.yaml"
    log "  admin 用户名: wuxin"
    log "  admin 密码  : $ADMIN_PASS   <-- 记下来！"
    log "  HMAC secret : 已随机生成（客户端要用同一个 secret 计算签名）"
    echo
else
    log "已有 config.yaml，跳过生成 (如需重置请先删除 $APP_DIR/configs/config.yaml)"
fi

# ---------- systemd ----------
log "安装 systemd unit"
cp "$SRC_DIR/deploy/epsilon.service" /etc/systemd/system/epsilon.service
systemctl daemon-reload
systemctl enable epsilon >/dev/null 2>&1
systemctl restart epsilon
sleep 1

# ---------- 状态 ----------
log "服务状态:"
systemctl status epsilon --no-pager | head -12
echo
log "监听端口:"
ss -tlnp | grep -E ':8080|epsilon' || echo "  (未监听 8080，去 journalctl -u epsilon 看日志)"

SERVER_IP=$(curl -s --max-time 3 ifconfig.me 2>/dev/null || echo "<你的公网IP>")
echo
log "首次部署完成。下一步:"
echo "   1. 云厂商安全组放行端口 8080 (或走 Nginx 反代到 80/443)"
echo "   2. curl http://127.0.0.1:8080/health   # 服务器上自测"
echo "   3. curl http://${SERVER_IP}:8080/health   # 外网自测"
echo "   4. 浏览器打开 http://${SERVER_IP}:8080/admin/  用上面的账号登录"
echo "   5. 如需域名 + HTTPS: 在宝塔面板配 Nginx 反代 127.0.0.1:8080"
