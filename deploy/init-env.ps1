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

# ---------- 1. 取 Cookie 字符串 ----------
if (-not $CookieString) {
    Write-Host "【第 1 步】获取 Cookie" -ForegroundColor Yellow
    Write-Host "  1) 浏览器打开 https://lmarena.ai 并正常发一次对话"
    Write-Host "  2) 按 F12 -> 切到「网络 / Network」标签"
    Write-Host "  3) 再发一次对话，在左侧请求列表找到  create-evaluation"
    Write-Host "  4) 点它 -> 右侧「标头 / Headers」-> 找到「请求标头」里的  Cookie  那一行"
    Write-Host "  5) 右键该行 -> 复制值 (Copy value)。整行都复制，不用自己截取。"
    Write-Host ""

    $clip = ''
    try { $clip = (Get-Clipboard -Raw -ErrorAction SilentlyContinue) } catch { }
    if ($clip -and ($clip -match 'arena-auth-prod-v1=' -or $clip -match 'cf_clearance=')) {
        Write-Host "  [i] 检测到剪贴板里已有 cookie 内容，直接使用。" -ForegroundColor Green
        $CookieString = $clip
    } else {
        Write-Host "  复制好之后，在这里右键粘贴再回车：" -ForegroundColor Yellow
        $CookieString = Read-Host "  Cookie"
    }
}

$CookieString = ($CookieString -replace '^\s*[Cc]ookie:\s*', '').Trim()

# ---------- 2. 切出两个值 ----------
function Get-CookieValue([string]$all, [string]$name) {
    # 匹配 name=value，value 到分号或结尾为止
    $m = [regex]::Match($all, [regex]::Escape($name) + '=([^;]+)')
    if ($m.Success) { return $m.Groups[1].Value.Trim() }
    return $null
}

$la = Get-CookieValue $CookieString 'arena-auth-prod-v1'
$cf = Get-CookieValue $CookieString 'cf_clearance'

if (-not $la) {
    Write-Host "[x] 没能从粘贴内容里找到 arena-auth-prod-v1，请确认复制的是完整的 Cookie 行。" -ForegroundColor Red
    exit 1
}
if (-not $cf) {
    Write-Host "[!] 没找到 cf_clearance。" -ForegroundColor Yellow
    Write-Host "    它可能不在这个请求里，可换一个请求再复制，或单独粘贴它的值：" -ForegroundColor Yellow
    $cf = Read-Host "  CF_CLEARANCE（直接回车跳过）"
    $cf = $cf.Trim()
}

Write-Host ""
Write-Host "  [+] LA_COOKIE    已取到，长度 $($la.Length) 字符，开头: $($la.Substring(0,[Math]::Min(20,$la.Length)))..." -ForegroundColor Green
if ($cf) {
    Write-Host "  [+] CF_CLEARANCE 已取到，长度 $($cf.Length) 字符，开头: $($cf.Substring(0,[Math]::Min(20,$cf.Length)))..." -ForegroundColor Green
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
