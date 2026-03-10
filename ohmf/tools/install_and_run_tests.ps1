Param(
    [switch]$ForceInstall
)

$RepoRoot = Split-Path -Parent $PSScriptRoot
$GoDir = Join-Path $RepoRoot ".tools\go"
$GoExe = Join-Path $GoDir "bin\go.exe"

function Info($m) { Write-Host "[run_tests] $m" }

if (-not (Test-Path $GoExe) -or $ForceInstall) {
    Info "Go not found at $GoExe. Will attempt to use system 'go' or download into $GoDir."

    # If system go is available and user didn't force install, prefer it to avoid download
    $sysGo = Get-Command go -ErrorAction SilentlyContinue
    if ($sysGo -and -not $ForceInstall) {
        Info "Found system go at $($sysGo.Source); using it to run tests."
        $go = $sysGo.Source
    } else {
        Info "No system go (or force install requested); will attempt to download latest Go."

        # Query release metadata from go.dev and pick the latest windows-amd64 archive
        try {
            $meta = Invoke-WebRequest -Uri "https://go.dev/dl/?mode=json" -UseBasicParsing -ErrorAction Stop | ConvertFrom-Json
        } catch {
            Write-Host ('Failed to fetch Go release metadata: ' + ($_.ToString()));
            exit 2
        }

        $entry = $meta | Select-Object -First 1
        if (-not $entry) { Write-Host "No Go release metadata found"; exit 2 }

        $file = $entry.files | Where-Object { $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and ($_.kind -eq 'archive' -or $_.kind -eq 'installer') } | Select-Object -First 1
        if (-not $file -or -not $file.url) {
            Write-Host "Unable to determine windows-amd64 download URL from Go release metadata. Please install Go manually or re-run with -ForceInstall after fixing network access.";
            exit 2
        }

        $url = "https://go.dev" + $file.url
        $fileName = [IO.Path]::GetFileName($file.url)
        if (-not $fileName) { Write-Host "Invalid download filename; aborting"; exit 2 }

        # Ensure a usable temp path exists; fall back to .tools/temp inside the repo
        if (-not (Test-Path $env:TEMP)) {
            $tmpDir = Join-Path $RepoRoot ".tools\temp"
            if (-not (Test-Path $tmpDir)) { New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null }
            $tmp = Join-Path $tmpDir $fileName
        } else {
            $tmp = Join-Path $env:TEMP $fileName
        }

        Info "Downloading $url to $tmp..."
        try {
            Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing -ErrorAction Stop
        } catch {
            Write-Host ('Failed to download ' + $url + ': ' + ($_.ToString()));
            exit 2
        }

    if (!(Test-Path (Join-Path $RepoRoot ".tools"))) { New-Item -ItemType Directory -Path (Join-Path $RepoRoot ".tools") -Force | Out-Null }

        Info "Extracting to .tools..."
        try {
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            [System.IO.Compression.ZipFile]::ExtractToDirectory($tmp, (Join-Path $RepoRoot ".tools"))
            Remove-Item $tmp -Force
        } catch {
            Write-Host ('Failed to extract ' + $tmp + ': ' + ($_.ToString()));
            exit 3
        }
    }

    if (!(Test-Path $GoExe)) {
        Write-Host "Extraction did not produce $GoExe";
        exit 4
    }
}

if (-not $go) {
    if (Test-Path $GoExe) { $go = $GoExe }
}

if (-not $go) {
    $sys = Get-Command go -ErrorAction SilentlyContinue
    if ($sys) { $go = $sys.Source } else { Write-Host "No 'go' found on PATH and no local .tools/go installed. Re-run with -ForceInstall to try installing or install Go manually."; exit 5 }
}

Info "Using go: $go"

$proc = Start-Process -FilePath $go -ArgumentList 'test','./...','-v' -NoNewWindow -Wait -PassThru
if ($proc.ExitCode -ne 0) { Write-Host "go test failed with exit code $($proc.ExitCode)"; exit $proc.ExitCode }
Info "go test completed successfully." 
