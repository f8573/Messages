# OHMF Web Playwright Suite

This workspace adds a browser-level QA layer on top of the existing `node:test` frontend checks and the Go gateway tests.

## Install

From the [repo root](c:/Users/James/Downloads/Messages):

```powershell
npm install
npx playwright install chromium firefox webkit
```

The standardized `npm run test:e2e` and `npm run test:live` entrypoints resolve Playwright from the repo-root install. A separate `ohmf/apps/web/node_modules` directory is optional.

## Run

Standardized root gates:

```powershell
npm run test:e2e
npm run test:live
```

From [apps/web](c:/Users/James/Downloads/Messages/ohmf/apps/web), the workspace-local commands still work after the root install:

Fast local checks:

```powershell
npm run test:unit
npm run test:smoke
```

Full mocked-browser suite:

```powershell
npm run test:e2e
```

Headed exploratory run:

```powershell
npm run test:e2e:headed
```

## Scope

- `node:test` remains the fastest contract layer for helper modules.
- Playwright covers browser rendering, modals, toggles, attachment staging, and evidence capture.
- Docker-backed gateway validation should still be run separately for live messaging, receipt fanout, mini-app session flows, and migration safety.

## Evidence

The suite writes screenshots into Playwright test outputs and attaches them to the HTML report. Use those as the baseline artifact for signoff and regressions.
