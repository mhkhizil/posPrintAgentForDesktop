#Requires -RunAsAdministrator
<#
.SYNOPSIS
Installs and starts the Lnkits POS Print Agent, then checks it answers.

.EXAMPLE
.\scripts\install-service.ps1 -AllowOrigin https://pos.yourvenue.com
#>
param(
    [string]$ExePath = (Join-Path (Split-Path -Parent $PSScriptRoot) "dist\pos-print-agent.exe"),
    [string]$AllowOrigin = ""
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $ExePath)) {
    throw "Agent executable not found: $ExePath. Run .\build\build.ps1 first."
}

$installArgs = @("--install")
if ($AllowOrigin) {
    $installArgs += @("--allow-origin", $AllowOrigin)
}

& $ExePath @installArgs
if ($LASTEXITCODE -ne 0) {
    throw "Install failed with exit code $LASTEXITCODE"
}

Start-Sleep -Seconds 2
sc.exe query LnkitsPrintAgent

try {
    $health = Invoke-RestMethod -Uri "http://127.0.0.1:8735/v1/health" -TimeoutSec 5
    Write-Host ("Agent {0} on {1}; elevated={2}; origins={3}" -f `
        $health.agentVersion, $health.platform, $health.elevated, ($health.allowOrigins -join ", "))
    Write-Host ("Config: {0}" -f (Join-Path $env:PROGRAMDATA "LnkitsPrintAgent\config.json"))
} catch {
    Write-Warning "The service is installed but /v1/health did not answer yet: $($_.Exception.Message)"
    Write-Warning "Check the log at $env:PROGRAMDATA\LnkitsPrintAgent\agent.log"
}
