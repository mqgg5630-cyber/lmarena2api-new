# 部署指南

## 一、部署在哪？Windows 还是 Linux？

**推荐：部署在你自己电脑的 Windows 上（或同一台机器的 WSL）。**

原因是本项目强依赖 `CF_CLEARANCE`（`check/checker.go` 中未设置会直接退出），而 Cloudflare 的
`cf_clearance` cookie 是**和「出口 IP + User-Agent」绑定**的：

| 部署位置 | 可行性 | 说明 |
|---|---|---|
| **Windows 本机** ✅ 推荐 | 好 | 与取 cookie 的浏览器同一出口 IP，cookie 直接有效 |
| WSL2 | 好 | 走 NAT 共用宿主机公网 IP，与 Windows 等价 |
| 云服务器 Linux / Docker | 差 | 出口 IP 不同，cf_clearance 立即失效，除非用 `PROXY_URL` 把流量代理回取 cookie 的那个 IP |

需要在外网访问时，本机跑服务 + 内网穿透（frp / cloudflared）即可，不要直接扔云上。

> ⚠️ 另一个高频坑：`common/config/config.go` 里 `USER_AGENT` 的默认值是 **Mac 的 UA**。
> 如果你在 Windows Chrome 里取的 cookie，必须在配置里把 `USER_AGENT` 改成你浏览器的真实 UA，
> 否则 UA 与 cookie 不匹配，Cloudflare 校验一样会失败。

---

## 二、为什么 README 里那条命令在 PowerShell 报错

```powershell
nohup env CF_CLEARANCE=xxx ./lmarena2api-macos > logfile.log 2>&1 &
# ParserError: 不允许使用与号(&)
```

那是 **bash/macOS 的语法**：PowerShell 没有 `nohup`、没有 `env`，`&` 也不是后台运算符。
本目录下的脚本就是它在各平台的等价实现。

---

## 三、Windows 部署（推荐路线）

### 1. 准备文件

无需 `git clone`，只要一个可执行文件即可：从
[Releases](https://github.com/deanxv/lmarena2api/releases) 下载 `lmarena2api.exe`
放到仓库根目录（你已经放在 `E:\1AI\lmarena2api\` 了）。

如果你要用本目录的脚本，把仓库 clone 下来，再把 exe 放进仓库根目录：

```powershell
cd E:\1AI
git clone https://github.com/mqgg5630-cyber/lmarena2api-new.git
cd lmarena2api-new
# 把已下载的 lmarena2api.exe 复制进来
Copy-Item E:\1AI\lmarena2api\lmarena2api.exe .
```

### 2. 获取三个值（详细步骤）

浏览器打开 <https://beta.lmarena.ai/>（若跳转到 canary 域名则用跳转后的域名），
**先正常发一次对话**，确保已通过 Cloudflare 人机验证。

#### 方法 A：Application 面板（最直观，推荐）

按 `F12` → 顶部 **Application**（应用）标签 → 左侧 **Storage → Cookies** → 点开
`https://beta.lmarena.ai`。你会看到一张 Name / Value 表格，找这两行：

| Name（不要复制这一列） | Value（复制这一列） |
|---|---|
| `cf_clearance` | 形如 `AbC...xyz-1747743176-1.2.1.1-QC67qVWtt...` → 填 `CF_CLEARANCE` |
| `arena-auth-prod-v1` | 形如 `base64-eyJhY2Nlc3NfdG9rZW4iOiJ...`（很长）→ 填 `LA_COOKIE` |

双击 Value 单元格 → 全选（`Ctrl+A`）→ 复制（`Ctrl+C`）。

> `arena-auth-prod-v1` 的值可能长达上千字符，务必确认完整复制。
> 如果 Value 被截断显示，右键该行选 **Show Requests With This Cookie** 或改用方法 B。

#### 方法 B：Network 面板

F12 → **Network** → 在页面发一次对话 → 找到 `create-evaluation` 请求 →
**Headers** → **Request Headers** → `cookie:` 那一整行。它长这样：

```
cookie: cf_clearance=AbC...-1747743176-1.2.1.1-QC67...; arena-auth-prod-v1=base64-eyJhY2Nl...; _ga=GA1.1.xxx
```

按 `;` 切开，取你要的两段的**等号后面部分**。

#### 三个值分别怎么填

```ini
# ✅ 正确：只要值本身
CF_CLEARANCE=AbC...-1747743176-1.2.1.1-QC67qVWtt...
LA_COOKIE=base64-eyJhY2Nlc3NfdG9rZW4iOiJ...

# ❌ 错误：带了 cookie 名
CF_CLEARANCE=cf_clearance=AbC...
# ❌ 错误：带了分号或引号
LA_COOKIE="base64-eyJhY2Nl...";
# ❌ 错误：把整行 cookie 都粘进来
LA_COOKIE=cf_clearance=xxx; arena-auth-prod-v1=yyy
```

#### USER_AGENT 怎么填

F12 → **Console**（控制台）→ 输入下面这行回车：

```js
navigator.userAgent
```

输出形如 `"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"`，
**去掉两端的双引号**后填入 `USER_AGENT=`。

> 这一步不能省：`cf_clearance` 是与 UA 绑定签发的，UA 对不上 Cloudflare 直接判失败。
> 注意本项目原先把 UA 硬编码成了 Mac 的值、`USER_AGENT` 变量根本没接线，
> 本分支已修复为真正读取该环境变量（见 `lmarena-api/api.go`、`common/config/config.go`）。

#### 填完长这样（示意）

```ini
LA_COOKIE=base64-eyJhY2Nlc3NfdG9rZW4iOiJleUpoYkdjaU9pSklVekkxTmlJ...（很长，一行）
CF_CLEARANCE=0hQKZ9x1mAbcd.efGH-1747743176-1.2.1.1-QC67qVWttXKkZs3RaGtGRs4xgz...
USER_AGENT=Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36
API_SECRET=123456
PORT=10088
DEBUG=true
```

> `API_SECRET` 不用去哪里找，是你自己定的密码，之后客户端用
> `Authorization: Bearer 123456` 来调用。

### 3. 填配置

```powershell
Copy-Item deploy\.env.example deploy\.env
notepad deploy\.env      # 填 LA_COOKIE / CF_CLEARANCE / USER_AGENT / API_SECRET
```

`deploy/.env` 已在 `.gitignore` 中，不会被提交。

### 4. 启动

```powershell
# 前台运行（能直接看日志，Ctrl+C 停止）—— 第一次建议用这个
.\deploy\start.ps1

# 后台运行（日志写入 logfile.log）
.\deploy\start.ps1 -Background

# 停止后台服务
.\deploy\stop.ps1
```

如果 PowerShell 拒绝执行脚本：

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
```

看到 `lmarena2api start success. enjoy it! ^_^` 即为成功。

### 5. 验证

```powershell
curl.exe http://127.0.0.1:10088/v1/models -H "Authorization: Bearer 123456"
```

对话测试：

```powershell
curl.exe http://127.0.0.1:10088/v1/chat/completions `
  -H "Content-Type: application/json" `
  -H "Authorization: Bearer 123456" `
  -d '{\"model\":\"gpt-4.1-2025-04-14\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'
```

### 6.（可选）不想每次手动启动

把服务注册成开机自启的计划任务：

```powershell
$exe = "E:\1AI\lmarena2api-new\deploy\start.ps1"
schtasks /create /tn "lmarena2api" /sc onlogon /rl highest `
  /tr "powershell -NoProfile -ExecutionPolicy Bypass -File `"$exe`" -Background"
```

---

## 四、Linux / WSL 部署

```bash
cd ~/projects
git clone https://github.com/mqgg5630-cyber/lmarena2api-new.git
cd lmarena2api-new

cp deploy/.env.example deploy/.env
vim deploy/.env            # 填 LA_COOKIE / CF_CLEARANCE / USER_AGENT

# 没有二进制时脚本会自动 go build（需已装 Go 1.23+）
./deploy/start.sh          # 前台
./deploy/start.sh -d       # 后台，日志 logfile.log
./deploy/stop.sh           # 停止
tail -f logfile.log
```

WSL 起的服务，Windows 侧用 `http://127.0.0.1:10088` 一般可以直接访问（WSL2 有端口转发）。

---

## 五、Docker 部署（仅在服务器与取 cookie 同 IP，或已配代理时使用）

```bash
docker compose up -d
```

用之前先把 `docker-compose.yml` 里的 `LA_COOKIE` / `CF_CLEARANCE` 换成真实值，
并补上 `USER_AGENT` 和（如需）`PROXY_URL`。

---

## 六、常见问题

| 现象 | 原因 / 处理 |
|---|---|
| 启动即退出，日志 `环境变量 LA_COOKIE 未设置` | `.env` 没填或脚本没读到，检查 `deploy/.env` |
| 请求返回 403 / Cloudflare 拦截页 | ① `cf_clearance` 过期（有效期通常几十分钟到几小时，需重新抓）② `USER_AGENT` 与抓 cookie 的浏览器不一致 ③ 部署机 IP 与抓 cookie 的 IP 不同 |
| 401 Unauthorized | 请求头 `Authorization: Bearer <API_SECRET>` 与配置不符 |
| 端口被占用 | 改 `.env` 里的 `PORT` |
| 想看更多日志 | `.env` 中设 `DEBUG=true` |

`cf_clearance` 会过期，这是本项目的固有限制——过期后重抓 cookie 更新 `.env` 再重启即可。
