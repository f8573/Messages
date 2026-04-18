# OHMF Test Gates

This repository exposes stable test entrypoints from the repo root:

```powershell
npm run test:unit
npm run test:integration
npm run test:web
npm run test:e2e
npm run test:live
npm run test:perf
npm run test:stress
npm run test:staging
```

List gates and suite-level tags:

```powershell
npm run test:list
```

## Gate Definitions

- `test:unit`: fast backend unit and contract coverage through the existing root Go test runner.
- `test:integration`: container-backed integration coverage through the existing Docker-based runner.
- `test:web`: fast web `node:test` coverage for shell helpers and browser-independent UI contracts.
- `test:e2e`: mocked Playwright coverage for deterministic browser flows.
- `test:live`: live Playwright coverage against a running OHMF stack. Requires a reachable API and frontend.
- `test:perf`: targeted race detection and benchmark coverage for gateway realtime, messaging, and E2EE paths.
- `test:stress`: stateful end-to-end messaging validation with real users, linked devices, WebSockets, persistence checks, and report artifacts under [testing/stress/](C:/Users/James/Downloads/Messages/testing/stress).
- `test:staging`: staging/manual signoff gate. Prints the release checklist by default and optionally runs automation first when `OHMF_RUN_STAGING_AUTOMATION=1`.

Important:
- `test:perf` remains the lightweight race-and-benchmark gate.
- `test:stress` is the runnable end-to-end harness for the distributed messaging pipeline.
- The architecture contract, instrumentation gaps, and remaining expansion work are documented in [testing/STRESS_TESTING_PLAN.md](C:/Users/James/Downloads/Messages/testing/STRESS_TESTING_PLAN.md).

## Environment Contract

These variables are the supported inputs for the standardized gates:

| Variable | Purpose |
|---|---|
| `OHMF_RUN_INTEGRATION` | Enables gateway integration tests where the Go suite expects integration mode. |
| `OHMF_E2E_LIVE` | Enables live Playwright browser flows. |
| `OHMF_API_BASE_URL` | Overrides the gateway base URL for web live tests. |
| `OHMF_E2E_BASE_URL` | Overrides the frontend base URL for Playwright. |
| `OHMF_STRESS_BASE_URL` | Gateway base URL for the stress harness. Defaults to `OHMF_API_BASE_URL` or `http://127.0.0.1:18080`. |
| `OHMF_STRESS_SCENARIO` | Stress scenario selector: `smoke`, `throughput`, or `reconnect`. |
| `OHMF_STRESS_WS_VERSION` | Stress websocket mode: `v1` or `v2`. |
| `OHMF_STRESS_USERS` / `OHMF_STRESS_DEVICES_PER_USER` | Logical user and linked-device counts for the stress harness. |
| `OHMF_STRESS_MESSAGES` / `OHMF_STRESS_RATE` / `OHMF_STRESS_DURATION_MS` | Message volume and rate controls for stress runs. |
| `OHMF_STRESS_METRICS_URLS` | Comma-separated list of raw metrics endpoints to snapshot before and after a stress run. |
| `OHMF_STRESS_REPORT_DIR` | Overrides the output directory for stress reports. |
| `OHMF_STRESS_DRY_RUN` | When set to `1`, resolves the stress configuration and exits without hitting the stack. |
| `TEST_DATABASE_URL` | Overrides the database DSN for gateway DB-backed tests. |
| `POSTGRES_URL` / `DB_DSN` | Alternate DB DSN inputs already honored by existing scripts. |
| `OHMF_TEST_TAG` | Optional suite-level tag filter for any gate. Equivalent to `--tag`. |
| `OHMF_RUN_STAGING_AUTOMATION` | When set to `1`, `test:staging` runs integration and live automation before manual signoff. |

## Capability Tags

The standardized runner supports suite-level filtering for these tags:

- `auth`
- `messages`
- `conversations`
- `sync`
- `realtime`
- `devices`
- `privacy`
- `miniapp`
- `media`
- `relay`
- `e2ee`
- `search`
- `migration`
- `perf`

Example:

```powershell
npm run test:integration -- --tag auth
```

## CI Gate Intent

- PR gate: `test:unit`, `test:web`, and OpenAPI/schema validation.
- Merge gate: `test:integration` and `test:e2e`.
- Nightly gate: `test:live`, `test:perf`, `test:stress`, and migration sweeps where infra is available.
- Pre-release gate: `test:staging` plus the manual checklist in [testing/STAGING_CHECKLIST.md](C:/Users/James/Downloads/Messages/testing/STAGING_CHECKLIST.md).
- End-to-end load validation details and expansion work: [testing/STRESS_TESTING_PLAN.md](C:/Users/James/Downloads/Messages/testing/STRESS_TESTING_PLAN.md).

## Coverage Policy

A feature is only considered covered when it has:

- one happy-path automated test when runnable in this repo
- one validation or authorization failure assertion
- one state consistency or persistence assertion
- one manual script or checklist item if automation is not yet possible
