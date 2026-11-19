# Builds, vets and tests the agent, then stamps the version into the binary.
param(
    [string]$Version = "0.0.0-dev",
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$output = Join-Path $root $OutDir
New-Item -ItemType Directory -Force -Path $output | Out-Null

Push-Location $root
try {
    $env:CGO_ENABLED = "0"

    Write-Host "go vet ./..."
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    Write-Host "go test ./..."
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }

    $target = Join-Path $output "pos-print-agent.exe"
    Write-Host "go build -o $target"
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $target ./cmd/pos-print-agent
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    Write-Host "Built $target ($Version)"
} finally {
    Pop-Location
}
