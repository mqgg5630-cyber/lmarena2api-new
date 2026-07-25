<#
.SYNOPSIS
    Windows (PowerShell) 下启动 lmarena2api。

.DESCRIPTION
    从 deploy/.env 读取环境变量并启动 lmarena2api.exe。
    README 里的 `nohup env KEY=V ./xxx > log 2>&1 &` 是 bash 语法，PowerShell 用不了，
    本脚本是它的 Windows 等价实现。

.PARAMETER Background
    后台运行（日志写入 logfile.log），不加则前台运行、Ctrl+C 停止。

.PARAMETER Exe
    可执行文件路径，默认自动在仓库根目录 / 当前目录查找 lmarena2api.exe。

.EXAMPLE
    .\deploy\start.ps1
    .\deploy\start.ps1 -Background
#>
[CmdletBinding()]
param(
    [switch]$Background,
    [string]$Exe,
    [string]$EnvFile
)

$ErrorActionPreference = 'Stop'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot  = Split-Path -Parent $scriptDir

# ---------- 1. 定位 .env ----------
if (-not $EnvFile) { $EnvFile = Join-Path $scriptDir '.env' }
if (-not (Test-Path $EnvFile)) {
    $sample = Join-Path $scriptDir '.env.example'
    Write-Host "[!] 未找到配置文件: $EnvFile" -ForegroundColor Yellow
    if (Test-Path $sample) {
        Copy-Item $sample $EnvFile
        Write-Host "[+] 已根据 .env.example 生成 ${EnvFile}，请先填写 LA_COOKIE / CF_CLEARANCE / USER_AGENT 后重新运行。" -ForegroundColor Cyan
    }
    exit 1
}

# ---------- 2. 解析 .env ----------
$envMap = @{}
Get-Content $EnvFile -Encoding UTF8 | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq '' -or $line.StartsWith('#')) { return }
    $idx = $line.IndexOf('=')
    if ($idx -lt 1) { return }
    $key = $line.Substring(0, $idx).Trim()
    $val = $line.Substring($idx + 1).Trim()
    if ($val.Length -ge 2 -and (($val.StartsWith('"') -and $val.EndsWith('"')) -or ($val.StartsWith("'") -and $val.EndsWith("'")))) {
        $val = $val.Substring(1, $val.Length - 2)
    }
    if ($val -ne '') { $envMap[$key] = $val }
}

# ---------- 3. 必填校验 ----------
foreach ($required in @('LA_COOKIE')) {
    if (-not $envMap.ContainsKey($required)) {
        Write-Host "[x] $EnvFile 中缺少必填项 $required" -ForegroundColor Red
        exit 1
    }
}
if (-not $envMap.ContainsKey('CF_CLEARANCE')) {
    Write-Host "[i] 未设置 CF_CLEARANCE（多数账号本就没有此 cookie），请求将不携带它。" -ForegroundColor DarkGray
}
if (-not $envMap.ContainsKey('USER_AGENT')) {
    Write-Host "[!] 未设置 USER_AGENT，程序将使用内置的 Mac UA；若 Cloudflare 校验失败，请在 .env 中填入浏览器真实 UA。" -ForegroundColor Yellow
}

# ---------- 4. 注入环境变量 ----------
foreach ($k in $envMap.Keys) {
    Set-Item -Path "Env:$k" -Value $envMap[$k]
}

# ---------- 5. 定位可执行文件 ----------
if (-not $Exe) {
    $candidates = @(
        (Join-Path $repoRoot 'lmarena2api.exe'),
        (Join-Path (Get-Location) 'lmarena2api.exe'),
        (Join-Path $scriptDir 'lmarena2api.exe')
    )
    $Exe = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $Exe -or -not (Test-Path $Exe)) {
    Write-Host "[x] 找不到 lmarena2api.exe。请从 Releases 下载并放到仓库根目录，或用 -Exe 指定路径。" -ForegroundColor Red
    Write-Host "    也可以自行编译： go build -o lmarena2api.exe" -ForegroundColor DarkGray
    exit 1
}

$port = if ($envMap.ContainsKey('PORT')) { $envMap['PORT'] } else { '10088' }
Write-Host "[+] 可执行文件 : $Exe"
Write-Host "[+] 监听端口   : $port"
Write-Host "[+] 接口地址   : http://127.0.0.1:$port/v1/chat/completions"

# ---------- 6. 启动 ----------
if ($Background) {
    $log    = Join-Path $repoRoot 'logfile.log'
    $errLog = Join-Path $repoRoot 'logfile.err.log'
    $p = Start-Process -FilePath $Exe -WorkingDirectory $repoRoot -WindowStyle Hidden `
        -RedirectStandardOutput $log -RedirectStandardError $errLog -PassThru
    $p.Id | Out-File (Join-Path $repoRoot 'lmarena2api.pid') -Encoding ascii
    Write-Host "[+] 已后台启动，PID=$($p.Id)，日志: $log"
    Write-Host "    查看日志: Get-Content -Wait .\logfile.log"
    Write-Host "    停止服务: .\deploy\stop.ps1"
} else {
    Write-Host "[+] 前台运行，Ctrl+C 停止`n"
    & $Exe
}
