<#
.SYNOPSIS
    停止后台运行的 lmarena2api。
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'SilentlyContinue'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot  = Split-Path -Parent $scriptDir
$pidFile   = Join-Path $repoRoot 'lmarena2api.pid'

$stopped = $false

if (Test-Path $pidFile) {
    $procId = (Get-Content $pidFile | Select-Object -First 1).Trim()
    if ($procId) {
        $p = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($p) {
            Stop-Process -Id $procId -Force
            Write-Host "[+] 已停止 PID=$procId"
            $stopped = $true
        }
    }
    Remove-Item $pidFile -ErrorAction SilentlyContinue
}

if (-not $stopped) {
    Get-Process -Name 'lmarena2api' -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.Id -Force
        Write-Host "[+] 已停止 PID=$($_.Id)"
        $stopped = $true
    }
}

if (-not $stopped) { Write-Host "[i] 没有正在运行的 lmarena2api 进程" }
