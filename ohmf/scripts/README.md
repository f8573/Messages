# 18 — Developer Scripts & Tooling

Mapping: OHMF spec section 18 (Developer tooling & scripts)

Purpose
- Document repository scripts for developer workflows: database refresh, migrations, build helpers, local dev start, and codegen.

Key scripts (examples)
- `scripts/refresh-db-build.ps1` — rebuild local DB state.
- `scripts/dev.ps1` — start local dev environment with watchers.

Usage examples (PowerShell)
```powershell
# reset local DB
.\scripts\refresh-db-build.ps1

# run local gateway in dev mode
.\scripts\dev.ps1 gateway
```

Implementation constraints
- Scripts should be idempotent and check prerequisites (docker, go, node).

Security considerations
- Do not hardcode credentials; read from env or .env files.

Observability and operational notes
- Provide verbose and non-verbose modes.

Testing requirements
- Smoke tests for scripts in CI.

References
- infra/docker README for environment details.

# Spec validation helpers
You can run a small repository validator that checks that the codebase contains
the components referenced by OHMF spec section 1 (Purpose and Scope). The
checker emits a JSON report to `build/spec_section_1_report.json` and returns
non-zero if required items are missing.

Run (if you have a Go toolchain available):
```powershell
# from repository root
go run scripts/check_spec_section_1.go
```

The checker is lightweight — it validates presence of directories and README
signals for the services/features listed in section 1. Use the JSON report for
CI gating or audit automation.