# Run tests using workspace Go if present, otherwise fall back to system `go`.
# Usage: ./tools/run_tests.ps1

$repoRoot = (Get-Location).ProviderPath
$workspaceGo = Join-Path $repoRoot "ohmf\.tools\go\bin\go.exe"

if (Test-Path $workspaceGo) {
    Write-Host "Using workspace Go: $workspaceGo"
    & $workspaceGo test ./... -v
    exit $LASTEXITCODE
}

$sysGo = Get-Command go -ErrorAction SilentlyContinue
if ($null -ne $sysGo) {
    Write-Host "Using system 'go' at: $($sysGo.Path)"
    go test ./... -v
    exit $LASTEXITCODE
}

Write-Host "No Go executable found."
Write-Host "To run tests locally, either:"
Write-Host " 1) Install Go (https://go.dev/dl/) and ensure 'go' is on your PATH, or"
Write-Host " 2) Place a Go binary at 'ohmf\\.tools\\go\\bin\\go.exe' and re-run this script."
Write-Host "As an alternative, push your branch to trigger CI which runs tests automatically."
exit 2
