# Observability Stack

Local observability assets live here.

Contents
- `prometheus.yml`: baseline scrape config for the gateway and Prometheus itself.
- `alerting.yml`: starter alert rules for gateway availability and WebSocket pressure.
- `grafana/`: auto-provisioned datasource and dashboard definitions.

Runtime
- Prometheus listens on `http://localhost:19090`.
- Grafana listens on `http://localhost:13000` with `admin` / `admin` by default in local compose.

References
- `infra/docker/docker-compose.yml`
- `services/gateway/internal/observability`
- `pkg/observability/observability.go`
