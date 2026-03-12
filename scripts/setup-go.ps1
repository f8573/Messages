<#
.SYNOPSIS
  Configure the current PowerShell session to use the repo-local Go binary.
#>
Param()

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$localGo = Join-Path $repoRoot.Path "ohmf\.tools\go\bin\go.exe"

if (Test-Path $localGo) {
    $env:GO_CMD = $localGo
    $goDir = Split-Path $localGo
    if (-not ($env:PATH -like "*$goDir*")) {
        $env:PATH = "$goDir;$env:PATH"
    }
    Write-Host "Using local go at $localGo"
} else {
    Write-Host "Local go not found at $localGo; leaving GO_CMD/PATH unchanged"
}

Write-Host 'To persist for this session, run:'
Write-Host '.\scripts\setup-go.ps1'
