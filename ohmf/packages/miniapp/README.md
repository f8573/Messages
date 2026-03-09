# 12 — Miniapp Platform (Packages: miniapp)

Mapping: OHMF spec section 12 (Mini-app Platform)

Purpose
- Define the miniapp packaging format, lifecycle (install/uninstall), UI integration points, secure execution sandbox, and the protocol for miniapp events.

Expected behavior
- Miniapps are signed JSON manifests with declared capabilities and scopes.
- Install flow includes validation of manifest against JSON Schema and verification of signature.

Miniapp manifest JSON Schema (Draft 2020-12)
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"MiniAppManifest",
	"type":"object",
	"required":["id","version","name","entrypoint","capabilities"],
	"properties":{
		"id":{"type":"string"},
		"version":{"type":"string"},
		"name":{"type":"string"},
		"entrypoint":{"type":"string","format":"uri"},
		"capabilities":{"type":"array","items":{"type":"string"}},
		"scopes":{"type":"array","items":{"type":"string"}},
		"signature":{"type":"string"}
	}
}
```

Implementation constraints
- Sandbox execution (no arbitrary host FS or direct DB access).
- Capabilities model must be enforced with runtime privilege checks.

Security considerations
- Validate manifest signature against trusted keyring.
- Cap installed only by users with proper consent.

Observability and operational notes
- Track installs, usage, crashes, and resource consumption.

Testing requirements
- Validate manifest parsing, signature verification, and capability enforcement.

References
- docs/mini-app-platform.md and packages/protocol for event delivery.