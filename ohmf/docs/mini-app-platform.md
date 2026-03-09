Mini-App Platform (Section 30)

This document expands Section 30 of the spec with an actionable runtime model, manifest format, capability model, bridge protocol, session lifecycle, security considerations, and testing notes.

Runtime model
- Mini-apps are small web-first applications served by a mini-app registry and executed inside a host runtime (mobile/web/native). The host provides a controlled bridge for select capabilities (e.g., storage, network, contacts, crypto).
- Mini-apps are loaded by fetching their `entrypoint` and executing within an isolated context (iframe, WebView, or sandboxed renderer).

Manifest format (high level)
- The mini-app manifest describes identification, runtime entrypoint, UI metadata, requested permissions, declared capabilities, and an optional cryptographic signature used to validate authenticity.
- Core fields: `app_id`, `name`, `version`, `entrypoint`, `icons`, `permissions`, `capabilities`, `signature`.

Capabilities model
- Capabilities are the declarative contract between host and mini-app. The manifest declares capabilities the app can consume (e.g., `storage`, `ui.dialogs`, `network`, `contacts`, `crypto`).
- Hosts map capability names to runtime-provided APIs. Each capability may have a scoped permission requirement and runtime quota.

Bridge protocol (summary)
- Communication is a JSON RPC-like message envelope over postMessage or native bridge. Messages include `id`, `type`, `method`, `params`, and `origin`.
- Hosts MUST validate message `origin` and correlate replies using `id`.
- Example bridge flow: mini-app requests `storage.get('key')` -> host validates permission -> host returns `result` or `error`.

Session lifecycle
- Install: registry stores manifest and host caches it.
- Launch: host fetches manifest, validates signature (when present), requests user consent for listed `permissions`, and then loads `entrypoint`.
- Active session: bridge is opened, capabilities are bound. Hosts may enforce timeouts and revoke permissions dynamically.
- Termination: host tears down the bridge, clears ephemeral state, and optionally revokes access tokens.

Security considerations
- Always validate manifests: check JSON schema and cryptographic `signature` when provided.
- Enforce least-privilege: prompt users for permissions and allow granular revocation.
- Isolate execution: run mini-apps in sandboxed contexts (iframe with `sandbox` attrs, WebView with disabled universal access) to limit cross-origin attack surface.
- Origin checks: hosts must verify message origins and only accept messages from the expected mini-app context.
- Input validation: hosts and apps must validate all inputs crossing the bridge.

Testing notes
- Unit tests: validate manifest schema against examples; test capability mapping and permission enforcement.
- Integration tests: use a test mini-app served from a local registry; assert bridge requests are authorized/denied as expected.
- Fuzzing: validate that malformed manifests or unexpected capability payloads are safely rejected.

References
- Manifest schema: `packages/protocol/schemas/miniapp_manifest.schema.json`
- Example mini-app: `packages/miniapp/examples/eightball/manifest.json`
