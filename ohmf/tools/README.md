# Tools

This folder contains helper scripts for local development and testing.

Files
- `install_and_run_tests.ps1` — PowerShell helper that prefers an existing system `go`, or attempts to download a matching Go release into `.tools` and then runs `go test ./...`.

When to use
- Use this when you want a convenient one-command way to run the repository Go tests on Windows.

Basic usage
PowerShell (from the repository root):

```powershell
# run tests, prefer system 'go' if present
.\ohmf\tools\install_and_run_tests.ps1

# force the script to download and use the bundled Go into .tools
.\ohmf\tools\install_and_run_tests.ps1 -ForceInstall
```

Notes & alternatives
- If you already have Go installed system-wide and on your PATH, the script will prefer that and just run `go test ./...`.
- If the script cannot access the Go release metadata (network restrictions) it will exit with an explanatory message. In that case either:
  - Install Go manually and add it to your PATH (recommended for iterative development).
  - Place a pre-downloaded Go ZIP in `.tools` and extract it to `.tools\go` yourself.
- CI: The repository has GitHub Actions configured to run `go test` and OpenAPI validation on push/PR; if local runs fail due to environment/network, pushing a branch will run tests in CI.

Troubleshooting
- If the script exits with messages about missing `%TEMP%` or download failures, create a writable folder at `.tools\temp` and re-run with `-ForceInstall`.
- If you prefer not to download Go at all, install Go 1.20+ from https://go.dev/dl and ensure `go` is available on your PATH.

Contact
- If you want, I can also add a Linux/macOS variant or modify the script to accept a local Go archive path; tell me which you prefer.
