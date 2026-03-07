param(
  [string]$Action = "up"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $root "infra\docker\docker-compose.yml"

switch ($Action) {
  "up" {
    docker compose -f $composeFile up --build
  }
  "down" {
    docker compose -f $composeFile down -v
  }
  "logs" {
    docker compose -f $composeFile logs -f api
  }
  default {
    Write-Error "Unsupported action: $Action"
  }
}
