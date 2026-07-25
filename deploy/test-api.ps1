<#
.SYNOPSIS
    测试 lmarena2api 是否正常工作（自动读取 deploy/.env 里的 API_SECRET 和 PORT）。

.EXAMPLE
    .\deploy\test-api.ps1
    .\deploy\test-api.ps1 -Model gpt-4.1-2025-04-14 -Prompt "用一句话介绍你自己"
#>
[CmdletBinding()]
param(
    [string]$Model  = 'gemini-2.0-flash-001',
    [string]$Prompt = '你好，请用一句话自我介绍',
    [string]$BaseUrl,
    [string]$ApiKey
)

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$envPath   = Join-Path $scriptDir '.env'

# 从 .env 读取 PORT / API_SECRET
$port = '10088'; $secret = '123456'
if (Test-Path $envPath) {
    Get-Content $envPath -Encoding UTF8 | ForEach-Object {
        $l = $_.Trim()
        if ($l -eq '' -or $l.StartsWith('#')) { return }
        $i = $l.IndexOf('='); if ($i -lt 1) { return }
        $k = $l.Substring(0,$i).Trim(); $v = $l.Substring($i+1).Trim()
        if ($k -eq 'PORT' -and $v) { $port = $v }
        if ($k -eq 'API_SECRET' -and $v) { $secret = ($v -split ',')[0].Trim() }
    }
}
if (-not $BaseUrl) { $BaseUrl = "http://127.0.0.1:$port" }
if (-not $ApiKey)  { $ApiKey  = $secret }

$headers = @{ 'Authorization' = "Bearer $ApiKey"; 'Content-Type' = 'application/json' }

Write-Host ""
Write-Host "服务地址: $BaseUrl" -ForegroundColor Cyan
Write-Host "API Key : $ApiKey" -ForegroundColor Cyan
Write-Host ""

# ---- 1. 模型列表 ----
Write-Host "[1/2] 测试 /v1/models ..." -ForegroundColor Yellow
try {
    $r = Invoke-RestMethod -Uri "$BaseUrl/v1/models" -Headers $headers -Method Get -TimeoutSec 20
    Write-Host "  [OK] 连接成功，可用模型 $($r.data.Count) 个，例如：" -ForegroundColor Green
    $r.data | Select-Object -First 5 | ForEach-Object { Write-Host "      $($_.id)" -ForegroundColor DarkGray }
} catch {
    Write-Host "  [x] 失败: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "      服务没启动？先运行 .\deploy\start.ps1" -ForegroundColor DarkGray
    Write-Host "      401 则说明 API_SECRET 与请求头不符。" -ForegroundColor DarkGray
    exit 1
}

# ---- 2. 对话 ----
Write-Host ""
Write-Host "[2/2] 测试 /v1/chat/completions (模型: $Model) ..." -ForegroundColor Yellow
$body = @{
    model    = $Model
    messages = @(@{ role = 'user'; content = $Prompt })
    stream   = $false
} | ConvertTo-Json -Depth 5

try {
    $resp = Invoke-RestMethod -Uri "$BaseUrl/v1/chat/completions" -Headers $headers `
            -Method Post -Body ([System.Text.Encoding]::UTF8.GetBytes($body)) -TimeoutSec 180
    $content = $null
    if ($resp -and $resp.choices -and $resp.choices.Count -gt 0) {
        $content = $resp.choices[0].message.content
    }
    if (-not $content) {
        Write-Host "  [x] 服务返回了空回复（choices 为空）。" -ForegroundColor Red
        Write-Host "      这几乎总是上游连接失败，请看服务端窗口的日志：" -ForegroundColor Yellow
        Write-Host "        curl (28) / Could not connect -> 需要配代理 PROXY_URL" -ForegroundColor DarkGray
        Write-Host "        403 / Just a moment           -> Cloudflare 拦截，需补 cf_clearance" -ForegroundColor DarkGray
        Write-Host "        401 / Unauthorized            -> LA_COOKIE 失效，需重新抓取" -ForegroundColor DarkGray
        exit 1
    }
    Write-Host "  [OK] 回复：" -ForegroundColor Green
    Write-Host "      $content" -ForegroundColor White
    Write-Host ""
    Write-Host "[OK] 一切正常，可以接入客户端了。" -ForegroundColor Green
} catch {
    Write-Host "  [x] 失败: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.ErrorDetails.Message) { Write-Host "      $($_.ErrorDetails.Message)" -ForegroundColor DarkGray }
    Write-Host ""
    Write-Host "  常见原因：cf_clearance 过期 / USER_AGENT 与抓 cookie 的浏览器不一致。" -ForegroundColor Yellow
    Write-Host "  重新抓 cookie： .\deploy\init-env.ps1" -ForegroundColor Yellow
    exit 1
}
