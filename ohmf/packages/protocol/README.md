# 7 — Protocols and Schemas (Packages: protocol)

Mapping: OHMF spec section 7 (Protocol, Schemas, Events)

Purpose
- Centralized canonical protocol definitions: protobuf envelopes, JSON Schema for events and REST payloads, OpenAPI references, and mapping to SQL persisted forms.

Expected behavior
- Provide single source-of-truth for message envelopes, event topics, and schema evolution processes.

Full specification details
- Maintain:
	- Proto definitions in `proto/`
	- JSON Schema in `schemas/`
	- OpenAPI fragments in `openapi/`
	- SQL canonical schema snippets in `sql/`
- Versioning:
	- Use semver for package versions.
	- When changing schema, add new `v2` message and maintain backward compatibility rules.

Example protobuf envelope (again)
```proto
syntax = "proto3";
package ohmf.protocol;
message Envelope {
	string message_id = 1;
	string conversation_id = 2;
	string from = 3;
	string to = 4;
	bytes body = 5;
	map<string,string> metadata = 6;
	int64 received_at = 7;
}
```

JSON Schema example (message envelope)
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"$id":"https://ohmf.example/schemas/envelope.json",
	"title":"Envelope",
	"type":"object",
	"required":["message_id","conversation_id","from","to","body","received_at"],
	"properties": {
		"message_id":{"type":"string"},
		"conversation_id":{"type":"string"},
		"from":{"type":"string"},
		"to":{"type":"string"},
		"body":{"type":"string"},
		"metadata":{"type":"object","additionalProperties":{"type":"string"}},
		"received_at":{"type":"string","format":"date-time"}
	}
}
```

Implementation constraints
- Provide code generation toolchain for protobuf and JSON Schema validation libraries in target languages (Go, TypeScript).

Security considerations
- Ensure schema does not permit arbitrary executable content (e.g., no eval-able payloads).

Observability and operational notes
- Track schema usage and deprecations.

Testing requirements
- Schema validation tests; message roundtrip between proto and JSON.

References
- See `proto`, `openapi`, `sql` subfolders.