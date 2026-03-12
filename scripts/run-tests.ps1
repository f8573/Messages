param(
  [switch]$Integration
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path (Resolve-Path "$root\..")
$go = $env:GO_CMD
if (-not $go) { $go = 'go' }

# Find go.mod files
$mods = Get-ChildItem -Path . -Recurse -Depth 4 -Filter go.mod | Where-Object { $_.FullName -notlike '*\\.tools\\*' } | Sort-Object FullName
if (-not $mods) {
  Write-Host "No go.mod files found; nothing to test."; exit 0
}

foreach ($m in $mods) {
  $dir = Split-Path $m.FullName -Parent
  Write-Host "== Module: $dir =="
  Push-Location $dir
  $pkgs = & $go list -f "{{if or .GoFiles .TestGoFiles}}{{.ImportPath}}{{end}}" ./... 2>$null | Where-Object { $_ -ne '' }
  if (-not $pkgs) { Write-Host "No testable packages in $dir; skipping."; Pop-Location; continue }

  if ($Integration) {
    Write-Host "Running integration-enabled tests for $dir"
    # If TEST_DATABASE_URL not set, try to start a temporary Postgres docker container
    $startedContainer = $false
    $containerName = 'messages-itest-pg'
    if (-not $env:TEST_DATABASE_URL) {
      try {
        & docker version > $null 2>&1
      } catch {
        Write-Host "Docker not available; cannot start temporary Postgres. Set TEST_DATABASE_URL externally to run integration tests."; throw
      }

      # If a container with our name exists, inspect it
      $exists = (& docker ps -a --filter "name=$containerName" --format '{{.ID}}') -join ''
      if ($exists) {
        $isRunning = (& docker inspect -f '{{.State.Running}}' $containerName) -eq 'true'
        if (-not $isRunning) {
          Write-Host "Removing existing container $containerName"
          & docker rm -f $containerName > $null 2>&1 || true
        }
      }

      if (-not $exists) {
        # pick an ephemeral host port to avoid collisions
        $rand = Get-Random -Minimum 55000 -Maximum 59999
        $hostPort = $rand
        Write-Host "Starting temporary Postgres container $containerName mapping host port $hostPort -> container 5432"
        $img = 'postgres:15-alpine'
        & docker run -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=tests -p ${hostPort}:5432 -d --name $containerName $img | Out-Null
        $startedContainer = $true
      } else {
        # get mapped port
        $portInfo = & docker port $containerName 5432/tcp 2>$null
        if ($portInfo) {
          if ($portInfo -match ":(\d+)$") { $hostPort = $Matches[1] }
        } else {
          throw "Failed to determine port mapping for existing container $containerName"
        }
      }

      # Wait for Postgres to be ready
      $ready = $false
      for ($i = 0; $i -lt 60; $i++) {
        try {
          $res = & docker exec $containerName pg_isready -U postgres 2>&1
          if ($LASTEXITCODE -eq 0 -or ($res -match 'accepting connections')) { $ready = $true; break }
        } catch { }
        Start-Sleep -Seconds 1
      }
      if (-not $ready) {
        if ($startedContainer) { & docker logs $containerName | Write-Host }
        throw "Postgres did not become ready in time"
      }

      # Set TEST_DATABASE_URL for tests
      $env:TEST_DATABASE_URL = "postgres://postgres:postgres@127.0.0.1:$hostPort/tests?sslmode=disable"
      Write-Host "TEST_DATABASE_URL set to $env:TEST_DATABASE_URL"
    }

    try {
      & $go test -v $pkgs
    } finally {
      if ($startedContainer) {
        Write-Host "Stopping and removing temporary container $containerName"
        & docker rm -f $containerName > $null 2>&1 || true
      }
    }
  } else {
    foreach ($p in $pkgs) {
      Write-Host "-- testing $p --"
      try { & $go test -run Test -v $p } catch { & $go test -v $p }
    }
  }
  Pop-Location
}
