Title: gateway: adapt DB Exec signature and fix vet issues (pgx adapter & tests)

Summary
-------
This PR introduces a small, low-risk adapter change in the `gateway` service to
address `go vet` and type-compatibility issues between `pgx/v5` and `pgxmock`.

What I changed
--------------
- Adjusted the package-local `DB` interface in `internal/carrier` so that
  `Exec` returns `(any, error)` instead of `pgconn.CommandTag`. This avoids
  fragile conversions in tests and better reflects that command tags are
  generally ignored by callers.
- Updated the runtime adapter (`cmd/api/main.go` `pgxAdapter`) to match the new
  signature.
- Updated the test adapter (`internal/carrier/test_adapter_test.go`) to return
  the mock's exec result as `any` (no string -> `pgconn.CommandTag` conversion).
- Fixed a malformed struct tag in `internal/carrier/handler.go` and removed an
  unused import flagged by `go vet`.

Why this change
---------------
`pgxmock` exposes older pgx types which are not strictly compatible with
`pgx/v5` types; converting to `pgconn.CommandTag` in test shims caused
`go vet`/type-check complaints. Introducing a tiny, package-local interface and
adapting both runtime and test adapters keeps changes local, minimizes
dependency churn, and fixes vet/build/test issues with minimal risk.

Testing performed
-----------------
- Ran `go vet ./...`, `go build ./...`, and `go test ./... -cover` across the
  repository modules (skipped `.tools`) using the repo-local Go binary
  (`.tools/go/bin/go.exe`).
- Fixed issues surfaced by `go vet` (unused import, malformed struct tag).
- Confirmed `ohmf/services/gateway` packages build and unit tests run; the
  `internal/carrier` package tests pass locally (coverage noted in CI outputs).

How to reproduce locally
------------------------
From the repository root run (PowerShell):

```powershell
Set-Location 'C:\Users\James\Downloads\Messages\ohmf'
$goexe = Join-Path $PWD '.tools\go\bin\go.exe'
Set-Location 'ohmf/services/gateway'
& $goexe vet ./...
& $goexe build ./...
& $goexe test ./... -cover
```

Notes & follow-ups
------------------
- This is a targeted, minimal change to avoid upgrading mocks or pgx major
  versions. If you prefer, we can instead upgrade `pgxmock` (or the test
  tooling) to a pgx/v5-compatible release — that is a larger dependency change.
- If you want, I can also open the PR in the browser and paste this description
  into the PR body for you.

Files changed (high level)
- `ohmf/services/gateway/internal/carrier/service.go`
- `ohmf/services/gateway/internal/carrier/test_adapter_test.go` (new/updated)
- `ohmf/services/gateway/cmd/api/main.go`
- `ohmf/services/gateway/internal/carrier/handler.go`
- misc whitespace/formatting and test fixes across gateway packages

Reviewer notes
--------------
- The core behavior of the carrier service is unchanged; these edits are
  interface/adaptor adjustments and test shims to maintain compatibility with
  `pgxmock` and to satisfy static analysis (`go vet`).
\nCI rerun: 2026-03-10T01:06:32.5193256-05:00
