# Mini-App Test Harness

This folder provides a lightweight browser-side harness for exercising mini-apps without the full OHMF web shell.

## Included

- `mock-host.js`
  - responds to the standard bridge envelope
  - maintains mock launch context, transcript, storage, and state version
  - can dispatch host events into a mounted frame

## Current use

For a richer manual harness, the repo also includes:

- `apps/web/miniapp-runtime.html`
- `apps/web/miniapp-runtime.js`

Those pages are the current interactive runtime lab. `mock-host.js` is the reusable low-level test primitive for browser tests and local SDK experiments.
