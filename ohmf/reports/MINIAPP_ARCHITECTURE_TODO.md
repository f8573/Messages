# Mini-App Architecture TODO

Date: 2026-03-21

This document tracks the mini-app architecture items that were not realistically implementable in the current pass. These are the larger structural or operational gaps that still need dedicated follow-up work.

## Asset Storage and Delivery

- Introduce durable object storage for release bundles, icons, previews, and manifests.
- Bind releases to immutable asset records with hash verification.
- Add controlled delivery URLs and revocation-aware asset lookup.
- Add CDN integration for published runtime assets.

## Publisher Trust Chain

- Implement publisher key registration, rotation, and revocation.
- Verify manifest signatures against publisher-owned active keys.
- Persist verification results, key ids, and failure reasons.
- Separate development trust from production trust more formally than the current developer-mode visibility rule.

## Review Workflow Expansion

- Add manifest diffing, permission diff review UI, reviewer assignment, and richer review checklists on top of the current expanded state model.

## Web Runtime Isolation

- Remove the current `allow-scripts allow-same-origin` compromise for untrusted apps.
- Enforce dedicated registry-issued runtime origins instead of relying on the main app origin plus iframe sandboxing.
- Add stronger CSP, frame-ancestor, connect-src, and origin policy controls for production runtime origins.

## Runtime Durability

- Move mini-app session hot state to Redis or equivalent.
- Add durable append-only session event persistence outside the current gateway tables alone.
- Add realtime pub/sub fanout for multi-instance session updates.
- Formalize event-sourced replay and durable resume semantics.

## Gateway Runtime Policy

- Add publisher/session quotas and rate limits that are backed by durable/shared infrastructure.
- Add deeper abuse controls, kill switches, and per-app throttling.
- Add more complete request schemas and standardized error models for all runtime endpoints.

## Install and Update Lifecycle

- Persist a fuller install state machine instead of deriving only from booleans and version comparisons.
- Add explicit `installing`, `updating`, `blocked`, and `uninstalled` lifecycle persistence.
- Add user re-consent and upgrade gating flows beyond the current permission-expansion detection metadata.

## Android Parity

- Replace the current Android scaffold’s local bridge/session model with gateway-backed sessions and share flows.
- Add install, uninstall, and update parity with the backend install lifecycle.
- Add Android-side conversation sharing, consent persistence, and update prompts.
- Build and test the Android host in an environment with the Android SDK and emulator coverage.

## Developer Platform

- Publish the SDK/test-harness package surface in a distributable form.
- Add a fuller local emulator with packaged examples and scripted test scenarios.
- Add a publisher/admin portal UI for app and release management.
- Add a compatibility matrix across bridge versions, manifest versions, host versions, and CLI versions.

## Observability and Operations

- Add structured metrics for catalog, installs, updates, sessions, and bridge failures.
- Add distributed tracing across host, gateway, registry, and asset delivery.
- Add runtime abuse analytics and release revocation propagation observability.
