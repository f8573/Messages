Observability helpers

This package provides shared request correlation and Prometheus export for the lightweight OHMF services in the root module.

Contents:
- `observability.go`: logger initialization, request ID and traceparent propagation, request metrics, and `/metrics` handler support.

Current metrics:
- `ohmf_http_requests_total`
- `ohmf_http_request_duration_seconds`
- `ohmf_http_requests_in_flight`
