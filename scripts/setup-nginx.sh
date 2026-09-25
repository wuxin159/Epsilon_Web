#!/usr/bin/env bash
# ============================================================
# 配置 Nginx 反代 80 端口 → epsilon (127.0.0.1:8080)
# 让外网通过 http://your-ip/ 访问 (不用带 :8080)
#
# 用法 (在服务器上):
#   curl -fsSL https://raw.githubusercontent.com/wuxin159/Epsilon_Web/main/scripts/setup-nginx.sh | bash
# ============================================================
set -euo pipefail

log() { echo "==> $*"; }

# ---------- 1. 确认前置条件 ----------
if ! command -v nginx &>/dev/null; then
    echo "!! 未装 Nginx，先在宝塔面板装 LNMP 或运行 apt install nginx"
    exit 1
fi

if ! curl -sf http://127.0.0.1:8080/health >/dev/null; then
    echo "!! 本地 8080 不通，先确认 epsilon 服务在跑: systemctl status epsilon"
    exit 1
fi

# ---------- 2. 判断 vhost 目录 ----------
if [ -d /www/server/panel/vhost/nginx ]; then
    VHOST_DIR=/www/server/panel/vhost/nginx           # 宝塔
elif [ -d /etc/nginx/conf.d ]; then
    VHOST_DIR=/etc/nginx/conf.d                       # 官方 Nginx
elif [ -d /etc/nginx/sites-enabled ]; then
    VHOST_DIR=/etc/nginx/sites-enabled                # Debian/Ubuntu Nginx
else
    echo "!! 找不到 Nginx vhost 目录"
    exit 1
fi
log "vhost 目录: $VHOST_DIR"

# ---------- 3. 备份并处理默认 default_server 冲突 ----------
log "扫描现有 default_server (避免冲突)"
grep -rlE "listen\s+80.*default_server" "$VHOST_DIR" 2>/dev/null | while read f; do
    if [ "$f" != "$VHOST_DIR/epsilon.conf" ]; then
        log "  发现冲突 $f, 备份为 $f.bak"
        cp "$f" "$f.bak"
        sed -i 's/default_server//g' "$f"
    fi
done

# ---------- 4. 写反代配置 ----------
CONF="$VHOST_DIR/epsilon.conf"
log "写入 $CONF"
cat > "$CONF" <<'EOF'
server {
    listen 80 default_server;
    server_name _;

    access_log /var/log/nginx/epsilon-access.log;
    error_log  /var/log/nginx/epsilon-error.log;

    # 上传文件大小限制 (下载/授权接口用不到大 body, 保守设小点)
    client_max_body_size 4m;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
        proxy_send_timeout 60s;
    }

    location = /health {
        proxy_pass http://127.0.0.1:8080;
        access_log off;
    }
}
EOF

# ---------- 5. 测试 + 重载 ----------
log "测试 Nginx 配置"
nginx -t

log "重载 Nginx"
nginx -s reload

# ---------- 6. 自测 ----------
sleep 1
log "服务器本机测试 (走 Nginx :80 → epsilon :8080)"
if curl -sf http://127.0.0.1/health; then
    echo "  ✓ Nginx 反代 OK"
else
    echo "  ✗ 反代失败，看 /var/log/nginx/epsilon-error.log"
    exit 1
fi

echo
SERVER_IP=$(curl -s --max-time 3 ipinfo.io/ip 2>/dev/null || echo "<你的公网IP>")
log "完成! 从外网访问试试:"
echo "   curl http://${SERVER_IP}/health"
echo "   浏览器打开 http://${SERVER_IP}/admin/"
echo ""
log "如果外网仍然 502, 那就是雨云的边缘层在拦 80 端口, 需要:"
echo "   1. 联系雨云工单确认 80/443 端口需不需要 ICP 备案"
echo "   2. 或者绑域名走 HTTPS/443 (通常不被拦)"
