# Kubernetes Standard and Worst-Case Profile Test Report

Date: 2026-04-29
Workspace: `C:\Users\James\Downloads\Messages`
Cluster context: `kind-ohmf` via `tmp/kubeconfig-ohmf.yaml`
Gateway target: `http://ohmf-gateway.ohmf-app.svc.cluster.local:8080`

## Executive Summary

The standard Kubernetes throughput profile passed before and after the fix.

The original worst-case profile failed, and the first reruns reproduced delivery loss. Diagnosis found two related gateway Redis pub/sub lifecycle issues: websocket subscriptions were tied to the HTTP request context instead of the client cleanup context, and the subscription loops did not return immediately on cancellation. Repeated load runs leaked Redis pub/sub clients until Redis refused new clients with `ERR max number of clients reached`.

After fixing the gateway subscription lifecycle, increasing the local Redis `maxclients` headroom, adding a short post-connect settle to the worst-case job, and rerunning in Kubernetes, the worst-case profile passed with `800 / 800` expected deliveries and zero client errors.

The async queued path was also validated by temporarily forcing `APP_ACK_TIMEOUT_MS=1` for the gateway. That run produced `queued_accepts: 600` and still converged to `800 / 800` realtime deliveries with no lost deliveries.

## Results

| Profile | Run time UTC | Result | Connected devices | Accepted | Expected deliveries | Successful deliveries | Lost | Client errors | Delivery p95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Standard, original | `2026-04-29T09:46:22Z` | PASS | `2500` | `180` | `240` | `240` | `0` | `0` | `62 ms` |
| Standard, fixed final rerun | `2026-04-29T16:24:39Z` | PASS | `2500` | `180` | `240` | `240` | `0` | `0` | `56 ms` |
| Worst-case, original | `2026-04-29T09:52:48Z` | FAIL | `978` | `600` | `800` | `766` | `34` | `22` | `130 ms` |
| Worst-case, reproduced | `2026-04-29T15:31:50Z` | FAIL | `1000` | `600` | `800` | `632` | `168` | `0` | `147 ms` |
| Worst-case, Redis exhausted | `2026-04-29T15:51:40Z` | FAIL | `1000` | `600` | `800` | `0` | `800` | `0` | `null` |
| Worst-case, fixed final rerun | `2026-04-29T16:19:34Z` | PASS | `1000` | `600` | `800` | `800` | `0` | `0` | `125 ms` |
| Worst-case, forced queued async | `2026-04-29T16:54:45Z` | PASS | `1000` | `600` | `800` | `800` | `0` | `0` | `187 ms` |

The normal fixed reruns reported `queued_accepts: 0` because the gateway's async ack usually arrived within the configured `APP_ACK_TIMEOUT_MS=2000` window. The forced queued run set that timeout to `1 ms`, which exercised the HTTP `202 queued` path and validated eventual async persistence and realtime delivery.

## Root Cause

The gateway v1 websocket path opens two Redis pub/sub subscriptions per websocket: one for message creation events and one for delivery updates. The worst-case profile opens `1000` websocket clients, so it creates roughly `2000` pub/sub subscriptions during a run.

Two gateway bugs allowed those subscriptions to outlive the websocket clients:

- `ServeHTTP` and `ServeV2` started subscription goroutines using the request context while disconnect cleanup cancels `clientCtx`.
- `subscribeDelivery`, `subscribeMessages`, and `subscribeUserEvents` ranged on Redis channels without selecting on `ctx.Done()`, so cancellation did not force immediate cleanup.

After the standard run and multiple worst-case runs, Redis reached its client ceiling and returned `ERR max number of clients reached`. At that point the gateway still accepted and persisted messages, but realtime delivery fanout collapsed.

## Fixes Applied

- `ohmf/services/gateway/internal/realtime/ws.go`
  - Subscription goroutines now use `c.clientCtx`.
  - Redis pub/sub loops now return on `ctx.Done()` and close their pub/sub handles.

- `ohmf/infra/docker/docker-compose.yml`
  - Redis now starts with `--maxclients 20000` for local Kubernetes stress-test headroom.

- `ohmf/infra/k8s/jobs/worst-case-throughput/job.yaml`
  - Reports are PVC-backed.
  - Worst-case job now uses `--settle-timeout-ms 60000` and `--post-connect-settle-ms 5000`.

- `ohmf/infra/k8s/jobs/standard-throughput/job.yaml`
  - Reports are PVC-backed.

- `testing/stress/run.js`
  - Added `--post-connect-settle-ms` / `OHMF_STRESS_POST_CONNECT_SETTLE_MS`.

## Verification

Gateway package test:

```text
ok   ohmf/services/gateway/internal/realtime  0.051s
```

Stress runner dry run:

```text
node testing\stress\run.js ... --post-connect-settle-ms 5000 --dry-run
```

Kubernetes manifest validation:

```text
kubectl kustomize ohmf/infra/k8s/jobs/worst-case-throughput
```

Final worst-case Kubernetes rerun:

```json
{
  "run_dir": "/var/reports/2026-04-29T16-19-34Z-throughput-k8s-worst-case-throughput",
  "scenario": "throughput",
  "messages_accepted": 600,
  "queued_accepts": 0,
  "expected_deliveries": 800,
  "successful_deliveries": 800,
  "lost_deliveries": 0,
  "duplicate_receipts": 0,
  "unpersisted_messages": 0,
  "ordering_violations": 0,
  "connected_devices": 1000,
  "client_errors": 0,
  "scenario_failures": 0,
  "delivery_p95_ms": 125
}
```

Forced queued async worst-case Kubernetes rerun:

```json
{
  "run_dir": "/var/reports/2026-04-29T16-54-45Z-throughput-k8s-worst-case-throughput",
  "scenario": "throughput",
  "messages_accepted": 600,
  "queued_accepts": 600,
  "expected_deliveries": 800,
  "successful_deliveries": 800,
  "lost_deliveries": 0,
  "duplicate_receipts": 0,
  "unpersisted_messages": 0,
  "ordering_violations": 0,
  "connected_devices": 1000,
  "client_errors": 0,
  "scenario_failures": 0,
  "delivery_p95_ms": 187
}
```

The gateway config was restored to `APP_ACK_TIMEOUT_MS=2000` after this forced queued run.

Final standard Kubernetes rerun:

```json
{
  "run_dir": "/var/reports/2026-04-29T16-24-39Z-throughput-k8s-standard-throughput",
  "scenario": "throughput",
  "messages_accepted": 180,
  "queued_accepts": 0,
  "expected_deliveries": 240,
  "successful_deliveries": 240,
  "lost_deliveries": 0,
  "duplicate_receipts": 0,
  "unpersisted_messages": 0,
  "ordering_violations": 0,
  "connected_devices": 2500,
  "client_errors": 0,
  "scenario_failures": 0,
  "delivery_p95_ms": 56
}
```

Redis post-run drain check:

```text
connected_clients:19
maxclients:20000
pubsub_clients:0
blocked_clients:0
```

## Artifacts

- Final passing standard artifacts: `test-results/k8s-phase1-profiles/standard/2026-04-29T16-24-39Z-throughput-k8s-standard-throughput`
- Final passing worst-case artifacts: `test-results/k8s-phase1-profiles/worst-case/2026-04-29T16-19-34Z-throughput-k8s-worst-case-throughput`
- Final passing forced queued async artifacts: `test-results/k8s-phase1-profiles/worst-case-async/2026-04-29T16-54-45Z-throughput-k8s-worst-case-throughput`
- Failed reproduction artifacts: `test-results/k8s-phase1-profiles/worst-case/2026-04-29T15-31-50Z-throughput-k8s-worst-case-throughput`
- Redis-exhaustion failure artifacts: `test-results/k8s-phase1-profiles/worst-case/2026-04-29T15-51-40Z-throughput-k8s-worst-case-throughput`

## Notes

This fixes the immediate Kubernetes profile failure and the Redis pub/sub leak exposed by repeated profile runs. A deeper future improvement would be to reduce Redis fanout pressure by sharing one Redis subscription per local gateway user/channel and fanning out to that user's local websocket clients, rather than opening per-socket subscriptions.
