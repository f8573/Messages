# Mini-App Loader Platform

## Current Shape

The app loader is now split into four concrete layers:

- `services/apps`
  - registry and review source of truth
- `services/gateway/internal/miniapp`
  - host-facing adapter for catalog, install, and runtime session flows
- `apps/web`
  - browser host runtime and picker
- `apps/android/miniapp-host`
  - Android host scaffold using the same bridge envelope

## Registry Rules

- Releases are immutable by `app_id + version`.
- Review states are `draft`, `submitted`, `approved`, `rejected`, `revoked`.
- Normal users should only install `approved` releases.
- Developer-mode localhost manifests remain supported for local testing.

## Host Runtime Rules

- Catalog/install lookups go through `/v1/apps*`.
- Session/share state stays in the gateway.
- Web and Android use the same request, response, and host-event bridge envelopes.

## Remaining Hardening

- replace the current `allow-scripts allow-same-origin` web iframe compromise with a stricter production runtime model
- pin production runtime origins to registry-issued app origins
- move the file-backed registry store to durable backing storage for production
- add stronger signature verification and publisher key management
