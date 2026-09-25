# Epsilon

授权验证 + 文件分发后端。客户端携带**机器码 + 时间戳 + HMAC 签名**发起请求，服务器判断是否授权 / 是否过期，返回到期时间。附带**云更新**（文件清单 + MD5 对比）和**审计日志**（所有增删改可回溯）。

- 语言 Go 1.22+，纯 Go 无 CGO，交叉编译即可跑
- 数据库 SQLite（`modernc.org/sqlite`，一个文件搞定）
- HTTP 框架 [Gin](https://github.com/gin-gonic/gin)
- 部署方式 Git-based：本地 `git push` → 服务器 `git pull` 重编重启

## 功能

- **授权管理**：机器码 + 到期时间；支持覆盖式续期、`±N 天`调整
- **文件分发**：管理端上传（带 MD5），客户端签名后下载
- **云更新协议**：客户端拉取文件清单，比对本地 MD5 增量下载
- **Web 管理面板**：Basic Auth 保护，三 Tab（授权 / 文件 / 日志）
- **CLI 工具**：批量脚本或 SSH 里操作授权
- **审计日志**：所有 license / file 的增删改都可回溯

---

## 目录结构

```
Epsilon/
├── cmd/
│   ├── server/           # HTTP 服务入口
│   └── admin/            # 授权管理 CLI (老工具, 现在 Web 面板更好用)
├── internal/
│   ├── config/           # YAML 配置加载
│   ├── handler/
│   │   ├── auth.go             # POST /api/v1/auth/check
│   │   ├── download.go         # GET  /api/v1/download/:file
│   │   ├── updates.go          # GET  /api/v1/updates (客户端拉清单)
│   │   ├── admin.go            # /admin/* BasicAuth 入口
│   │   ├── admin_licenses.go   # 授权 CRUD + 调整时长
│   │   ├── admin_files.go      # 文件上传/下载/删除/列表
│   │   ├── admin_audit.go      # 审计日志查询
│   │   ├── admin_page.html     # 三 Tab 前端 (嵌入二进制)
│   │   └── middleware.go       # 日志中间件
│   ├── service/          # HMAC 签名/验签
│   └── storage/          # licenses / files / audit_log 三张表
├── configs/
│   ├── config.example.yaml   # 配置模板 (入库)
│   └── config.yaml           # 真实配置 (gitignored)
├── deploy/
│   ├── epsilon.service   # systemd unit
│   └── nginx.conf        # Nginx 反代片段
├── scripts/
│   ├── build.sh          # 本地交叉编译 Linux amd64
│   ├── server-setup.sh   # 服务器首次部署 (装 Go + 编译 + systemd)
│   ├── server-update.sh  # 服务器增量更新
│   ├── setup-nginx.sh    # 配 Nginx 反代 80 → epsilon:8080
│   └── deploy.sh         # 本地一键部署 (push + 触发 update)
├── data/                 # SQLite + 上传文件 (gitignored)
│   ├── epsilon.db
│   └── downloads/        # 上传的文件真身
└── .deploy.env           # 本地部署配置 (gitignored, 含服务器 IP)
```

---

## API 协议

所有客户端接口（`/api/v1/*`）都要**签名 + 时间戳**，防重放。

### 签名算法

```
message = machine_code + "|" + timestamp
sign    = hex(HMAC-SHA256(secret, message))
```

- `secret` 是服务器 `configs/config.yaml` 里的 `auth.secret`
- `timestamp` 是 Unix 秒（客户端本地时间）
- 服务器允许 `±timestamp_window` 秒偏差（默认 300 秒）

**Go 客户端示例**：

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
)

func sign(secret, machineCode string, ts int64) string {
    h := hmac.New(sha256.New, []byte(secret))
    fmt.Fprintf(h, "%s|%d", machineCode, ts)
    return hex.EncodeToString(h.Sum(nil))
}
```

**C++ 客户端**：用 OpenSSL `HMAC(EVP_sha256(), ...)` 或 mbedTLS，输入拼接和输出十六进制小写。

---

### `POST /api/v1/auth/check` — 授权校验

**请求体**：

```json
{
  "machine_code": "ABCDEF1234",
  "timestamp": 1706123456,
  "sign": "hex(HMAC-SHA256(secret, machine_code + \"|\" + timestamp))"
}
```

**响应**：

```json
{
  "code": 0,
  "message": "ok",
  "expire_at": 1735689600,
  "server_time": 1706123456
}
```

**响应码**：

| code | 含义 |
|---|---|
| `0` | OK |
| `1` | 未授权（机器码不存在） |
| `2` | 已过期 |
| `3` | 签名错误 |
| `4` | 时间戳过期（防重放） |
| `5` | 请求格式错误 |
| `6` | 服务器内部错误 |

---

### `GET /api/v1/updates` — 拉取更新文件清单

客户端定期调用，比对本地 MD5 决定是否需要下载新版本。

**请求**：`?machine_code=X&timestamp=T&sign=S`

**响应**：

```json
{
  "code": 0,
  "expire_at": 1735689600,
  "server_time": 1706123456,
  "files": [
    {
      "name": "dfm-client-v1.2.3.exe",
      "size": 12345678,
      "md5": "57ebf009f53289e62532565d6ab677ca",
      "uploaded_at": 1706100000
    }
  ]
}
```

响应码含义同 `/auth/check`。**未授权或已过期**的机器码拿不到清单。

---

### `GET /api/v1/download/:file` — 授权后下载

**请求**：`?machine_code=X&timestamp=T&sign=S`

签名规则和 `/auth/check` 一致。服务器校验签名 + 授权状态 + 未过期后返回文件流（支持 Range，可断点续传）。

---

## 客户端云更新流程

```
每次启动 / 定时:
  1. POST /api/v1/auth/check       ← 拿到 code=0 才继续
  2. GET  /api/v1/updates          ← 拿文件清单
  3. 对每个关心的文件:
     if 本地 MD5 != server MD5:
         GET /api/v1/download/:name  ← 下载覆盖
```

---

## 本地开发

```bash
# 1. 拉依赖
go mod tidy

# 2. 配好 config.yaml (真密钥不入库)
cp configs/config.example.yaml configs/config.yaml
# 编辑 configs/config.yaml:
#   - auth.secret 用 openssl rand -hex 32 生成
#   - admin.password 改成强密码

# 3. 起服务
go run ./cmd/server

# 4. 测试
curl http://localhost:8080/health
```

---

## 部署到服务器

### 首次部署（服务器上一条命令）

```bash
curl -fsSL https://raw.githubusercontent.com/wuxin159/Epsilon_Web/main/scripts/server-setup.sh | bash
```

脚本会自动：装 Go → 克隆仓库 → 编译 → 生成随机密钥的 `config.yaml` → 装 systemd → 启动服务。跑完打印 admin 密码和 HMAC secret，**记下来**（脚本不会打印第二次）。

> ⚠️ 国内服务器（大陆）需要走 ICP 备案才能对外提供 80/8080 端口的 HTTP 服务，否则会被云厂商网络层拦截返回 502。**建议直接买香港 / 新加坡节点，避坑。**

### 后续更新

本地：
```bash
git commit -am "改了 xxx"
./scripts/deploy.sh          # 需在 .deploy.env 设置 SERVER=root@x.x.x.x
```

或服务器直接：
```bash
bash /opt/epsilon-src/scripts/server-update.sh
```

### 可选：Nginx 反代到 80 端口

如果要用不带端口的地址（`http://ip/admin/`）：

```bash
curl -fsSL https://raw.githubusercontent.com/wuxin159/Epsilon_Web/main/scripts/setup-nginx.sh | bash
```

---

## 管理后台（Web 面板）

浏览器打开 `http://your-server:8080/admin/`，Basic Auth 登录（账号在 `config.yaml` 的 `admin` 段）。三个 Tab：

### Tab 1 · 授权设备

- **新增/覆盖表单**：机器码 + 天数 + 备注 → 保存
- **列表**：机器码 / 到期时间 / 状态色（有效/临期/过期）/ 备注 / 更新时间
- **每行按钮**：
  - `+7d` / `+30d` / `+1y` — 快捷延长
  - `-7d` — 缩短（会弹确认）
  - `±` — 自定义天数（弹窗，正数=延长，负数=缩短）
  - `续期` — 把机器码填到顶部表单
  - `删除` — 吊销
- **搜索**：机器码 / 备注模糊匹配

### Tab 2 · 文件管理

- **拖拽/点击上传**：单个 ≤ 500MB，同名覆盖并记日志
- **列表**：文件名 / 大小 / MD5 / 上传时间 / 上传者 / 备注
- **MD5 一键复制**（需要 HTTPS 环境浏览器才允许剪贴板）
- **下载**（管理员直接下载，不走签名）
- **删除**（磁盘 + 数据库同时清）

### Tab 3 · 操作日志

- **全部增删改**都自动记录：`create` / `replace` / `extend` / `reduce` / `delete` / `upload`
- **过滤**：按类别（授权/文件）、对象名
- **点击详情**展开 JSON 完整字段（含 old/new 值，可复盘）

---

## 管理方式对比

| 方式 | 场景 |
|---|---|
| Web 面板 `/admin/` ⭐ | 日常操作，最方便 |
| CLI `./bin/epsilon-admin` | 服务器批量脚本、SSH 里操作 |
| HTTP API 直调 | 自动化对接（比如收款系统付款后自动加授权） |

### HTTP API 快查

```bash
AUTH="wuxin:YOUR_PASSWORD"
BASE="http://your-server:8080/admin/api"

# 授权
curl -u $AUTH $BASE/licenses                                              # 列出
curl -u $AUTH -X POST $BASE/licenses \
     -H "Content-Type: application/json" \
     -d '{"machine_code":"XXX","days":365,"note":"客户 A"}'                # 新增/覆盖
curl -u $AUTH -X POST $BASE/licenses/adjust \
     -H "Content-Type: application/json" \
     -d '{"machine_code":"XXX","delta_days":30}'                          # +30 天
curl -u $AUTH -X POST $BASE/licenses/adjust \
     -H "Content-Type: application/json" \
     -d '{"machine_code":"XXX","delta_days":-7}'                          # -7 天
curl -u $AUTH -X DELETE $BASE/licenses/XXX                                # 删除

# 文件
curl -u $AUTH $BASE/files                                                 # 列出
curl -u $AUTH -F "file=@dfm.exe" -F "note=v1.2.3" $BASE/files/upload      # 上传
curl -u $AUTH -o dfm.exe $BASE/files/download/dfm.exe                     # 下载
curl -u $AUTH -X DELETE $BASE/files/dfm.exe                               # 删除

# 审计日志
curl -u $AUTH "$BASE/audit?category=license&limit=50"                     # 只看授权
curl -u $AUTH "$BASE/audit?target=MACHINE-XXX"                            # 看某个对象的所有历史
```

---

## ⚠️ 提交纪律（重要，仓库公开）

### 绝对不能进 Git 的文件

`.gitignore` 已经拦住，别手动 `-f` 强加：

| 文件 | 原因 |
|---|---|
| `configs/config.yaml` | 真实 HMAC secret、admin 密码 |
| `data/` | SQLite 数据库 + 上传文件（含全部客户机器码和产品文件） |
| `.deploy.env` | 服务器地址、SSH 端口 |
| `.env` | 任何环境变量 |
| `*.db` / `*.log` | 数据/日志 |
| `bin/` | 二进制产物 |

### 绝对不能出现在代码字面量的东西

- 生产服务器 **IP 地址**（走 `.deploy.env` 或 `SERVER` 环境变量）
- **密码、Token、私钥**（走 `config.yaml`）
- 客户机器码、备注、姓名等**业务数据**（走数据库）
- 内部 Slack/邮箱/域名等（不必要就别写）

### 提交前检查

```bash
# 每次 commit 前扫一眼要提交的内容
git diff --cached

# 扫描潜在的敏感字面量
grep -RIE "(password|secret|token).*=\s*['\"][^'\"]{8,}" --include='*.go' --include='*.yaml' .
```

### 如果不慎提交了敏感数据

**立即做三件事**（顺序不能乱）：

1. **失效泄露的凭据**：换新的 HMAC secret、admin 密码；如果是服务器登录密码，同时改 SSH
2. **洗历史**：
   ```bash
   git tag backup-emergency
   FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch --force --tree-filter \
       'find . -type f -exec sed -i "s/LEAKED_STRING/REDACTED/g" {} +' \
       --tag-name-filter cat -- --all
   git for-each-ref --format="%(refname)" refs/original/ | xargs -n1 git update-ref -d
   git reflog expire --expire=now --all && git gc --prune=now --aggressive
   git push --force origin main
   ```
3. **等 GitHub GC 或联系 Support**：孤立 commit 保留约 90 天

⚠️ Force push 只在**确认没有其他人 clone** 的情况下做；否则所有人得重新 clone。

---

## 运维要点

### 备份（最重要）

授权数据 + 上传文件都在 `/opt/epsilon/data/`。**这个目录丢了，客户授权和产品文件全没**。上线后立刻配自动备份：

```bash
# 例：每天凌晨 3 点备份整个 data 目录到 /root/backups，保留 30 天
cat > /etc/cron.d/epsilon-backup <<'EOF'
0 3 * * * root tar czf /root/backups/epsilon-$(date +\%Y\%m\%d).tar.gz -C /opt/epsilon data && find /root/backups -name 'epsilon-*.tar.gz' -mtime +30 -delete
EOF
mkdir -p /root/backups
```

生产环境建议再挂对象存储（阿里云 OSS / 腾讯云 COS，几毛钱一个月），万一服务器整个炸了还能救。

### 日志

```bash
systemctl status epsilon
journalctl -u epsilon -f                    # 实时
journalctl -u epsilon --since "1 hour ago"
```

Nginx 反代（如果配了）：
```bash
tail -f /var/log/nginx/epsilon-access.log
tail -f /var/log/nginx/epsilon-error.log
```

### 更新 admin 密码 / secret

改 `/opt/epsilon/configs/config.yaml` 后：

```bash
systemctl restart epsilon
```

⚠️ 改 `auth.secret` 会让**所有已部署的客户端失败**（因为签名 secret 变了），慎重。

### 数据库直查

想跳过 API 直接改数据库：

```bash
apt install sqlite3
sqlite3 /opt/epsilon/data/epsilon.db

# 常用查询
.tables                                                    # 看表
SELECT * FROM licenses;
SELECT * FROM files;
SELECT * FROM audit_log ORDER BY ts DESC LIMIT 20;
```

改完不用重启（SQLite 无缓存）。

---

## License

私有项目。
