<#
.SYNOPSIS
    交互式生成 deploy/.env -- 你只需整行粘贴浏览器的 Cookie，脚本自动切出
    LA_COOKIE 与 CF_CLEARANCE，无需手动截取。

.DESCRIPTION
    步骤：
      1. 浏览器打开 lmarena 并发一次对话
      2. F12 -> 网络(Network) -> 点 create-evaluation 请求 -> 标头(Headers)
         -> 请求标头里找到 Cookie 那一行 -> 右键 -> 复制值(Copy value)
      3. 回到 PowerShell 运行本脚本，按提示粘贴

.EXAMPLE
    .\deploy\init-env.ps1
#>
[CmdletBinding()]
param(
    [string]$CookieString,
    [string]$UserAgent,
    [string]$ApiSecret
)

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$envPath   = Join-Path $scriptDir '.env'

Write-Host ""
Write-Host "=== lmarena2api 配置生成向导 ===" -ForegroundColor Cyan
Write-Host ""

# ---------- 1. 取 Cookie ----------
function Get-CookieValue([string]$all, [string]$name) {
    # 匹配 name=value，value 取到分号或结尾为止
    $m = [regex]::Match($all, [regex]::Escape($name) + '=([^;\s]+)')
    if ($m.Success) { return $m.Groups[1].Value.Trim() }
    return $null
}

function Clean-Value([string]$v, [string]$name) {
    if (-not $v) { return $v }
    $v = $v.Trim().Trim([char]39).Trim([char]34).Trim()
    # 用户可能连名字一起粘了，去掉 "name=" 前缀
    if ($v -match ('^' + [regex]::Escape($name) + '=')) {
        $v = $v -replace ('^' + [regex]::Escape($name) + '='), ''
    }
    # 去掉结尾分号
    return $v.TrimEnd(';').Trim()
}

$la = $null; $cf = $null

if ($CookieString) {
    $CookieString = ($CookieString -replace '^\s*[Cc]ookie:\s*', '').Trim()
    $la = Get-CookieValue $CookieString 'arena-auth-prod-v1'
    $cf = Get-CookieValue $CookieString 'cf_clearance'
}

if (-not $la -or -not $cf) {
    Write-Host "【第 1 步】获取两个 Cookie 值" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  用【应用程序】面板最稳（不用去找某个具体请求）：" -ForegroundColor Cyan
    Write-Host "    1) 浏览器打开 https://lmarena.ai 并正常发一次对话（确保过了人机验证）"
    Write-Host "    2) 按 F12，切到「应用程序 / Application」标签"
    Write-Host "       （标签太多看不到就点 >> 展开，中文版叫「应用程序」）"
    Write-Host "    3) 左侧展开「存储 / Storage」-> 「Cookie / Cookies」-> 点站点域名"
    Write-Host "    4) 中间会出现 Name / Value 两列的表格，一会儿按提示找对应的行"
    Write-Host ""
    Write-Host "  复制方法：点中那一行 -> 双击 Value 单元格 -> Ctrl+A 全选 -> Ctrl+C" -ForegroundColor DarkGray
    Write-Host ""
}

# --- arena-auth-prod-v1 ---
if (-not $la) {
    Write-Host "  [1/2] 找到 Name 为  arena-auth-prod-v1  的那一行，复制它的 Value" -ForegroundColor Yellow
    Write-Host "        特征：以 base64- 开头，非常长（上千字符）" -ForegroundColor DarkGray
    $la = Read-Host "  粘贴 arena-auth-prod-v1 的值"
    $la = Clean-Value $la 'arena-auth-prod-v1'
}
if (-not $la) {
    Write-Host "[x] arena-auth-prod-v1 不能为空。若表格里找不到它，说明还没登录/没发过对话。" -ForegroundColor Red
    exit 1
}

# --- cf_clearance ---
if (-not $cf) {
    Write-Host ""
    Write-Host "  [2/2] 找到 Name 为  cf_clearance  的那一行，复制它的 Value" -ForegroundColor Yellow
    Write-Host "        特征：中间带一串数字时间戳，形如 xxx-1748314997-1.2.1.1-yyy" -ForegroundColor DarkGray
    Write-Host "        （如果整个表格里没有这一行，直接回车跳过，多数情况仍可用）" -ForegroundColor DarkGray
    $cf = Read-Host "  粘贴 cf_clearance 的值（可回车跳过）"
    $cf = Clean-Value $cf 'cf_clearance'
}

Write-Host ""
Write-Host "  [+] LA_COOKIE    长度 $($la.Length)，开头: $($la.Substring(0,[Math]::Min(24,$la.Length)))..." -ForegroundColor Green
if ($la -notmatch '^base64-') {
    Write-Host "  [!] 注意：正常值应以 base64- 开头，你粘的似乎不是，请确认复制的是 Value 列。" -ForegroundColor Yellow
}
if ($cf) {
    Write-Host "  [+] CF_CLEARANCE 长度 $($cf.Length)，开头: $($cf.Substring(0,[Math]::Min(24,$cf.Length)))..." -ForegroundColor Green
} else {
    Write-Host "  [i] CF_CLEARANCE 留空。若之后调用被 Cloudflare 拦截，再回来补上。" -ForegroundColor DarkGray
}

# ---------- 3. User-Agent ----------
Write-Host ""
Write-Host "【第 2 步】User-Agent" -ForegroundColor Yellow
if (-not $UserAgent) {
    Write-Host "  在同一个浏览器 F12 -> 控制台(Console) -> 输入下面这行回车："
    Write-Host "      navigator.userAgent" -ForegroundColor Cyan
    Write-Host "  把结果粘贴到这里（两端的引号会自动去掉）："
    $UserAgent = Read-Host "  USER_AGENT"
}
$UserAgent = $UserAgent.Trim().Trim([char]39).Trim([char]34).Trim()
if (-not $UserAgent) {
    Write-Host "[x] USER_AGENT 不能为空，它必须和抓 cookie 的浏览器一致。" -ForegroundColor Red
    exit 1
}
Write-Host "  [+] USER_AGENT = $UserAgent" -ForegroundColor Green

# ---------- 4. API_SECRET ----------
Write-Host ""
Write-Host "【第 3 步】接口密钥（你自己随便定一个，调用时当 API-KEY 用）" -ForegroundColor Yellow
if (-not $ApiSecret) {
    $ApiSecret = Read-Host "  API_SECRET（直接回车用默认 123456）"
}
if (-not $ApiSecret) { $ApiSecret = '123456' }

# ---------- 5. 写文件 ----------
if (Test-Path $envPath) {
    $bak = "$envPath.bak"
    Copy-Item $envPath $bak -Force
    Write-Host ""
    Write-Host "  [i] 已存在的 .env 备份为 $bak" -ForegroundColor DarkGray
}

$lines = @(
    "# 由 deploy/init-env.ps1 生成于 $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')",
    "LA_COOKIE=$la",
    "CF_CLEARANCE=$cf",
    "USER_AGENT=$UserAgent",
    "API_SECRET=$ApiSecret",
    "PORT=10088",
    "DEBUG=true"
)
# 用 UTF8 无 BOM 写出，避免第一行被 BOM 污染
[System.IO.File]::WriteAllLines($envPath, $lines, (New-Object System.Text.UTF8Encoding($false)))

Write-Host ""
Write-Host "[OK] 已生成 $envPath" -ForegroundColor Green
Write-Host ""
Write-Host "下一步：" -ForegroundColor Cyan
Write-Host "    .\deploy\start.ps1"
Write-Host ""
