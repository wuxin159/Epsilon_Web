# Epsilon

一个极简的授权验证后端。客户端携带**机器码 + 时间戳 + HMAC 签名**发起请求，服务器判断是否授权 / 是否过期，返回到期时间。

- 语言 Go 1.22+，纯 Go 无 CGO，交叉编译即可跑
- 数据库 SQLite（`modernc.org/sqlite`，一个文件搞定）
- HTTP 框架 [Gin](https://github.com/gin-gonic/gin)
- 部署方式 Git-based：本地 `git push` → 服务器 `git pull` 重编重启

---

## 目录结构

```
Epsilon/
├── cmd/
│   ├── server/           # HTTP 服务入口
│   └── admin/            # 授权管理 CLI
├── internal/
│   ├── config/           # YAML 配置加载
│   ├── handler/          # HTTP 路由 (auth / download / admin panel)
│   ├── service/          # HMAC 签名/验签
│   └── storage/          # SQLite 授权表 CRUD
├── configs/
│   ├── config.example.yaml   # 配置模板（入库）
│   └── config.yaml           # 真实配置（gitignored）
├── deploy/
│   ├── epsilon.service   # systemd unit
│   └── nginx.conf        # Nginx 反代片段
├── scripts/
│   ├── build.sh          # 本地交叉编译 Linux amd64
│   ├── server-setup.sh   # 服务器首次部署（装 Go + 编译 + systemd）
│   ├── server-update.sh  # 服务器增量更新
│   └── deploy.sh         # 本地一键部署（push + 触发 update）
├── data/                 # SQLite 数据 (gitignored)
└── .deploy.env           # 本地部署配置 (gitignored)
```

---

## API 协议

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

### `GET /api/v1/download/:file`

授权后下载文件。参数走 query string：
`?machine_code=X&timestamp=T&sign=S`

签名规则和 `/auth/check` 一致，服务器校验签名 + 授权状态 + 未过期后返回文件。

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
    "time"
)

func sign(secret, machineCode string, ts int64) string {
    h := hmac.New(sha256.New, []byte(secret))
    fmt.Fprintf(h, "%s|%d", machineCode, ts)
    return hex.EncodeToString(h.Sum(nil))
}
```

**C++ 客户端**：用 OpenSSL `HMAC(EVP_sha256(), ...)` 或 mbedTLS，输入拼接和输出十六进制小写。

---

## 本地开发

```bash
# 1. 拉依赖
go mod tidy

# 2. 配好 config.yaml（真密钥不入库）
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

脚本会自动：装 Go → 克隆仓库 → 编译 → 生成随机密钥的 `config.yaml` → 装 systemd → 启动服务。跑完打印 admin 密码和 HMAC secret，**记下来**。

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

---

## 管理授权

三种方式，任选：

### 1. Web 面板（推荐）

浏览器打开 `http://your-server:8080/admin/`，Basic Auth 登录（账号在 `config.yaml` 的 `admin` 段）。

支持：增/删/查/搜索，状态色标（有效/临期/过期），点击"续期"快速填单。

### 2. CLI

```bash
./bin/epsilon-admin add MACHINE-XXX 365 "客户 A"    # 加/续期
./bin/epsilon-admin list                            # 列全部
./bin/epsilon-admin delete MACHINE-XXX              # 吊销
```

### 3. HTTP API（自动化）

```bash
# 加授权
curl -u wuxin:PASSWORD -X POST http://server/admin/api/licenses \
     -H "Content-Type: application/json" \
     -d '{"machine_code":"XXX","days":365,"note":"某客户"}'

# 列全部
curl -u wuxin:PASSWORD http://server/admin/api/licenses

# 删除
curl -u wuxin:PASSWORD -X DELETE http://server/admin/api/licenses/XXX
```

---

## ⚠️ 提交纪律（重要，仓库公开）

### 绝对不能进 Git 的文件

`.gitignore` 已经拦住，别手动 `-f` 强加：

| 文件 | 原因 |
|---|---|
| `configs/config.yaml` | 真实 HMAC secret、admin 密码 |
| `data/` | SQLite 数据库（含全部客户机器码） |
| `.deploy.env` | 服务器地址、SSH 端口 |
| `.env` | 任何环境变量 |
| `*.db` / `*.log` | 数据/日志 |
| `bin/` | 二进制产物 |

### 绝对不能出现在代码字面量的东西

- 生产服务器 **IP 地址**（`.deploy.env` 或 SERVER 环境变量传入）
- **密码、Token、私钥**（走 `config.yaml`）
- 客户机器码、备注、姓名等**业务数据**（走数据库）
- 内部 Slack/邮箱/域名等（不必要就别写）

### 提交前检查

```bash
# 每次 commit 前扫一眼要提交的内容
git diff --cached

# 或用 pre-commit hook（可选）——扫敏感字
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

### 备份

授权数据只在服务器的 `/opt/epsilon/data/epsilon.db`，**丢了所有客户授权全没**。上线后立刻配自动备份：

```bash
# 例：每天凌晨 3 点备份到 /root/backups，保留 30 天
echo "0 3 * * * root cp /opt/epsilon/data/epsilon.db /root/backups/epsilon-\$(date +\\%Y\\%m\\%d).db && find /root/backups -name 'epsilon-*.db' -mtime +30 -delete" > /etc/cron.d/epsilon-backup
```

生产环境建议再挂个对象存储（阿里云 OSS / 腾讯云 COS，几毛钱一个月）。

### 日志

```bash
systemctl status epsilon
journalctl -u epsilon -f          # 实时
journalctl -u epsilon --since "1 hour ago"
```

### 更新 admin 密码 / secret

改 `/opt/epsilon/configs/config.yaml` 后：

```bash
systemctl restart epsilon
```

⚠️ 改 `auth.secret` 会让**所有已部署的客户端失败**（因为签名 secret 变了），慎重。

---

## License

私有项目。
