#!/usr/bin/env pwsh
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $root "infra\docker\docker-compose.yml"
$gatewayDir = Join-Path $root "services\gateway"
$localGo = Join-Path $root ".tools\go\bin\go.exe"

Write-Host "Resetting Docker DB volume..."
docker compose -f $composeFile down -v
docker compose -f $composeFile up -d db redis

Write-Host "Building gateway with Go..."
Push-Location $gatewayDir
if (Get-Command go -ErrorAction SilentlyContinue) {
  go build ./...
} elseif (Test-Path $localGo) {
  & $localGo build ./...
} else {
  Pop-Location
  throw "Go not found. Install Go or use .tools/go."
}
Pop-Location
