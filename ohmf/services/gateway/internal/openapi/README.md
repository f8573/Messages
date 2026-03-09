# 19.4 — Gateway: OpenAPI & API Documentation

Mapping: OHMF spec section 19 (Gateway) and 7 (API definitions).

Purpose
- Host and serve the canonical OpenAPI 3.0 / 3.1 definitions for client SDKs, provide a versioned spec, and produce server/client stubs for SDK generation.

Expected behavior
- Expose /openapi/v1/openapi.yaml and JSON.
- Include securitySchemes for BearerAuth and ApiKeyAuth.
- Include machine-readable examples and JSON Schema references.

OpenAPI snippet (YAML excerpt)
```yaml
openapi: 3.1.0
info:
	title: OHMF Gateway API
	version: 'v1'
paths:
	/api/v1/messages:
		post:
			summary: Ingest message
			requestBody:
				required: true
				content:
					application/json:
						schema:
							$ref: '#/components/schemas/GatewayMessageRequest'
			responses:
				'202':
					description: Accepted
components:
	securitySchemes:
		bearerAuth:
			type: http
			scheme: bearer
			bearerFormat: JWT
	schemas:
		GatewayMessageRequest:
			$ref: 'https://ohmf.example/schemas/gateway.message.request.json'
```

Implementation constraints
- Versioned specs with stable URLs and checksum headers.
- CI pipeline must validate OpenAPI format and run contract tests with stubs.

Security considerations
- Ensure examples in published OpenAPI do not include real secrets.

Observability and operational notes
- Serve spec from CDN or gateway with proper caching headers.
- Track downloads for client SDK generation analytics.

Testing requirements
- Contract tests that exercise spec examples against implementation (e.g., using Dredd or Schemathesis).

References
- packages/protocol for shared schema URIs.