# 7.1 — Protocol: Protobuf Definitions

Mapping: OHMF spec section 7.1 (Protobuf definitions)

Purpose
- House canonical .proto files for envelope, events, and service contracts. Provide guidance for generating language-specific bindings.

Expected behavior
- Protobuf files must:
	- Use package `ohmf.protocol`.
	- Include field options for JSON names.
	- Be backward-compatible following established guidelines.

Protobuf example (envelope and event)
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
	int64 received_at = 7; // epoch millis
}

message MessageIngressEvent {
	Envelope envelope = 1;
	string origin = 2;
}
```

Implementation constraints
- Use `proto3` only; avoid reserved keywords; add `option go_package` for Go generation.
- Keep message sizes bounded; prefer streaming for large payloads.

Security considerations
- Do not include secrets in protos.

Observability and operational notes
- Include comments in proto files for OpenAPI generation.

Testing requirements
- Ensure generated code compiles in CI for all supported languages.

References
- packages/protocol README.
