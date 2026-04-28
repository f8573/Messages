# Docker-Kubernetes Profile Test Results

Date: 2026-04-28  
Commit: `e3a72f3`  
Environment: local Docker stack using `ohmf/infra/docker/docker-compose.yml`  
Gateway: `http://127.0.0.1:18080`  
WebSocket mode: `v1`  
Metrics endpoint: `http://127.0.0.1:18080/metrics`

## Setup

Started the Docker stack with locally raised OTP, WebSocket, and send-rate limits so the Kubernetes load profiles were not blocked by local abuse controls:

```powershell
docker compose -f .\ohmf\infra\docker\docker-compose.yml up -d --build
```

Gateway readiness check passed against:

```powershell
http://127.0.0.1:18080/healthz
```

The requested profiles correspond to the Kubernetes load job parameters in:

- `ohmf/infra/k8s/jobs/standard-throughput/job.yaml`
- `ohmf/infra/k8s/jobs/worst-case-throughput/job.yaml`

## Summary

| Profile | Result | Connected Devices | Logical Users | Messages | Persisted | Deliveries | Lost | Duplicates | Client Errors | Accept p95 | Delivery p95 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Standard throughput | Pass | 2500 | 1875 | 180 / 180 | 180 | 240 / 240 | 0 | 0 | 0 | 87 ms | 75 ms |
| Worst-case throughput | Pass | 1000 | 750 | 600 / 600 | 600 | 800 / 800 | 0 | 0 | 0 | 357 ms | 345 ms |

Both profiles completed with:

- `0` lost deliveries
- `0` duplicate receipts
- `0` unpersisted messages
- `0` ordering violations
- `0` send failures
- `0` client errors
- `0` scenario failures

## Standard Throughput

Command:

```powershell
node .\testing\stress\run.js --scenario throughput --total-clients 2500 --unique-user-ratio 0.75 --active-conversations 120 --messages 180 --rate 2.5 --send-concurrency 3 --report-dir .\test-results --metrics-url http://127.0.0.1:18080/metrics --run-label docker-kubernetes-standard-throughput
```

Artifact directory:

```text
test-results/2026-04-28T18-15-09Z-throughput-docker-kubernetes-standard-throughput
```

Key metrics:

- Duration: `397435 ms`
- Connected devices: `2500`
- Logical users: `1875`
- Messages requested/accepted/persisted: `180 / 180 / 180`
- Queued accepts: `0`
- Expected/successful deliveries: `240 / 240`
- Realtime deliveries: `240`
- Lost deliveries: `0`
- Duplicate receipts: `0`
- Unpersisted messages: `0`
- Ordering violations: `0`
- Send failures: `0`
- Client errors: `0`
- Accept latency: min `79 ms`, avg `84.31 ms`, p50 `83 ms`, p95 `87 ms`, p99 `93 ms`, max `228 ms`
- Delivery latency: min `59 ms`, avg `68.84 ms`, p50 `67 ms`, p95 `75 ms`, p99 `83 ms`, max `258 ms`

## Worst-Case Throughput

Command:

```powershell
node .\testing\stress\run.js --scenario throughput --total-clients 1000 --unique-user-ratio 0.75 --active-conversations 250 --messages 600 --rate 120 --send-concurrency 8 --report-dir .\test-results --metrics-url http://127.0.0.1:18080/metrics --run-label docker-kubernetes-worst-case-throughput
```

Artifact directory:

```text
test-results/2026-04-28T18-21-52Z-throughput-docker-kubernetes-worst-case-throughput
```

Key metrics:

- Duration: `343663 ms`
- Connected devices: `1000`
- Logical users: `750`
- Messages requested/accepted/persisted: `600 / 600 / 600`
- Queued accepts: `0`
- Expected/successful deliveries: `800 / 800`
- Realtime deliveries: `800`
- Lost deliveries: `0`
- Duplicate receipts: `0`
- Unpersisted messages: `0`
- Ordering violations: `0`
- Send failures: `0`
- Client errors: `0`
- Accept latency: min `88 ms`, avg `319.53 ms`, p50 `301 ms`, p95 `357 ms`, p99 `360 ms`, max `365 ms`
- Delivery latency: min `85 ms`, avg `314.13 ms`, p50 `315 ms`, p95 `345 ms`, p99 `357 ms`, max `373 ms`

## Raw Artifacts

Each run directory contains:

- `config.json`
- `summary.json`
- `summary.md`
- `messages.json`
- `topology.json`
- `metrics/start-1.prom`
- `metrics/connected-1.prom`
- `metrics/end-1.prom`

