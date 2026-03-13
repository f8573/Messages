param(
    [switch]$Integration
)

Set-StrictMode -Version Latest

function Find-Docker {
    if (Get-Command docker -ErrorAction SilentlyContinue) { return "docker" }
    $possible = "C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe"
    if (Test-Path $possible) { return $possible }
    throw "docker not found. Ensure Docker Desktop is installed and running."
}

function Start-Postgres {
    if ($env:TEST_DATABASE_URL -or $env:POSTGRES_URL -or $env:DB_DSN) {
        Write-Output "External DB provided via environment; skipping postgres start."
        return $null
    }
    $docker = Find-Docker
    Write-Output "Attempting to bring up postgres via docker compose..."
    try {
        & $docker compose up -d postgres | Out-Null
        $cid = (& $docker compose ps -q postgres).Trim()
    } catch {
        Write-Output "No 'postgres' compose service or compose not available; starting standalone container..."
        # remove any existing test container to avoid name conflicts
        try {
            $existing = (& $docker ps -a -q -f name=ohmf_test_postgres).Trim()
            if ($existing) { & $docker rm -f ohmf_test_postgres | Out-Null }
        } catch {}
        & $docker run -d --name ohmf_test_postgres -e POSTGRES_USER=dev -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=dev -p 5432:5432 postgres:15-alpine | Out-Null
        $cid = (& $docker ps -q -f name=ohmf_test_postgres).Trim()
    }

    if (-not $cid) { throw "Could not determine postgres container id" }

    Write-Output "Waiting for Postgres to accept connections..."
    for ($i = 0; $i -lt 30; $i++) {
        try {
            & $docker exec $cid pg_isready -U dev -q | Out-Null
            Write-Output "Postgres is ready"
            return $cid
        } catch {
            Start-Sleep -Seconds 1
        }
    }
    throw "Postgres did not become ready in time"
}

function Stop-Postgres([string]$cid) {
    $docker = Get-Command docker -ErrorAction SilentlyContinue
    if ($null -eq $docker) { Write-Output "docker CLI not found; skipping cleanup"; return }

    # Remove standalone test container if it exists
    try {
        $id = (& docker ps -a -q -f name=ohmf_test_postgres).Trim()
        if ($id) {
            & docker rm -f ohmf_test_postgres | Out-Null
        }
    } catch {}

    # Stop and remove compose-managed postgres service if present
    try {
        $composeId = (& docker compose ps -q postgres) -join ""
        if ($composeId) {
            & docker compose stop postgres | Out-Null
            & docker compose rm -f postgres | Out-Null
        }
    } catch {}
}

try {
    $cid = Start-Postgres

    if ($Integration) {
        # Prefer local project go binary
        $localGo = Join-Path -Path (Get-Location) -ChildPath "ohmf\.tools\go\bin\go.exe"
        if (Test-Path $localGo) { $goCmd = $localGo } elseif (Get-Command go -ErrorAction SilentlyContinue) { $goCmd = "go" } else { Write-Error "Go (go) is not on PATH and local ohmf/.tools/go/bin/go.exe not found."; exit 1 }
        # Run integration tests using compose itest service for proper networked environment
        $docker = Find-Docker
        Write-Output "Starting compose itest for integration tests..."
        & $docker compose up --build --abort-on-container-exit --exit-code-from itest itest
        $rc = $LASTEXITCODE
        & $docker compose down -v | Out-Null
        if ($rc -ne 0) { exit $rc }
    } else {
        $localGo = Join-Path -Path (Get-Location) -ChildPath "ohmf\.tools\go\bin\go.exe"
        if (Test-Path $localGo) { $goCmd = $localGo } elseif (Get-Command go -ErrorAction SilentlyContinue) { $goCmd = "go" } else { Write-Error "Go (go) is not on PATH and local ohmf/.tools/go/bin/go.exe not found."; exit 1 }
        Push-Location -Path .\ohmf
        Write-Output "Running unit tests..."
        & $goCmd test ./... -v
        Pop-Location
    }
} catch {
    Write-Error "Test run failed: $_"
    exit 1
} finally {
    if ($cid) { Stop-Postgres $cid }
}
