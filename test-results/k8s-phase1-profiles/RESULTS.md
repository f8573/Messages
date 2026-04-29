# Kubernetes Phase 1 Profile Test Results

Run date: 2026-04-29  
Workspace: `C:\Users\James\Downloads\Messages`  
Plan followed: `ohmf/infra/k8s/PLAN.md`

## Design Used

Used a Phase 1 hybrid design:

- Kubernetes load generator Jobs ran in namespace `ohmf-loadgen`.
- The OHMF gateway was treated as the external Phase 1 app target.
- Postgres and Redis were external to the Kubernetes loadgen cluster through Docker.
- Kafka/Cassandra/processor failure drills were not run, matching the plan boundary that `testing/stress/run.js` is Kubernetes-safe while the suite-level Docker fault hooks are not Kubernetes-native.

Local lab constraints:

- Docker Desktop Kubernetes was not configured, so a local `kind` cluster named `ohmf-lab` was created.
- Docker memory limit was about 7.4 GiB.
- The full Kafka/Cassandra external stack could not coexist with kind under that limit; the control-plane was killed with exit 137 during the first attempt.
- The final target used gateway + Postgres + Redis with `APP_USE_KAFKA_SEND=false` and `APP_USE_CASSANDRA_READS=false`, so `queued_accepts=0` means the synchronous gateway path was exercised.

## Kubernetes Setup

Namespaces and priority classes were applied from:

- `ohmf/infra/k8s/base/namespaces.yaml`
- `ohmf/infra/k8s/base/priorityclasses.yaml`

Stress image:

- Built from `testing/stress/Dockerfile`
- Tagged `ohmf-stress:dev`
- Loaded into kind

Report persistence:

- The repo example Jobs use `emptyDir`, which cannot be copied after completion because `kubectl cp` uses container exec.
- The final evidence runs used PVC-backed `/var/reports` volumes:
  - `ohmf-standard-reports`
  - `ohmf-worst-case-reports`

Connectivity validation:

- Kubernetes pod to gateway health check passed: `200 ok`
- Kubernetes smoke run passed with:
  - `messages_accepted=8`
  - `successful_deliveries=8`
  - `lost_deliveries=0`
  - `client_errors=0`

## Harness Finding

The Kubernetes Job examples pass the gateway as `OHMF_STRESS_BASE_URL`, which works.

During debugging, direct use of `--base-url` failed in Kubernetes because `testing/stress/run.js` parses `--base-url` as `baseUrl`, but `buildConfig()` reads `args.baseURL`. That silently fell back to the default `http://127.0.0.1:18080`, which is wrong inside a pod.

I added a small diagnostic improvement to `testing/stress/run.js` so top-level failures print stack traces and nested causes. That is why the root cause became visible:

`Cause: Error: connect ECONNREFUSED 127.0.0.1:18080`

## Standard Profile

Profile arguments:

- Scenario: `throughput`
- Total clients: `2500`
- Unique user ratio: `0.75`
- Logical users derived by harness: `1875`
- Active conversations: `120`
- Messages: `180`
- Rate: `2.5 msg/s`
- Send concurrency: `3`
- WebSocket mode: `v1`

Outcome: PASS

Summary:

- Duration: `397782 ms`
- Connected devices: `2500`
- Messages requested: `180`
- Messages accepted: `180`
- Messages persisted: `180`
- Queued accepts: `0`
- Expected deliveries: `240`
- Successful deliveries: `240`
- Lost deliveries: `0`
- Duplicate receipts: `0`
- Unpersisted messages: `0`
- Ordering violations: `0`
- Send failures: `0`
- Client errors: `0`
- Accept latency p95/p99: `590 ms / 1078 ms`
- Delivery latency p95/p99: `534 ms / 1055 ms`

Artifacts:

- `test-results/k8s-phase1-profiles/standard/2026-04-29T06-29-42Z-throughput-k8s-standard-throughput-phase1-local-pvc/summary.json`
- `test-results/k8s-phase1-profiles/standard/2026-04-29T06-29-42Z-throughput-k8s-standard-throughput-phase1-local-pvc/summary.md`
- `test-results/k8s-phase1-profiles/standard/2026-04-29T06-29-42Z-throughput-k8s-standard-throughput-phase1-local-pvc/config.json`
- `test-results/k8s-phase1-profiles/standard/2026-04-29T06-29-42Z-throughput-k8s-standard-throughput-phase1-local-pvc/messages.json`
- `test-results/k8s-phase1-profiles/standard/2026-04-29T06-29-42Z-throughput-k8s-standard-throughput-phase1-local-pvc/topology.json`
- Metrics snapshots under `metrics/`

## Worst-Case Profile

Profile arguments:

- Scenario: `throughput`
- Total clients: `1000`
- Unique user ratio: `0.75`
- Logical users derived by harness: `750`
- Active conversations: `250`
- Messages: `600`
- Rate: `120 msg/s`
- Send concurrency: `8`
- WebSocket mode: `v1`

Outcome: FAIL during topology provisioning

Failure:

```text
Error: failed to connect stress-user-425/stress-user-425-device-1
    at provisionTopology (/app/testing/stress/run.js:659:15)
    at process.processTicksAndRejections (node:internal/process/task_queues:103:5)
    at async main (/app/testing/stress/run.js:1581:16)
```

Observed resource pressure during the worst-case attempt:

- `ohmf-api` reached about `6.124 GiB / 7.416 GiB` Docker memory limit.
- `ohmf-api` CPU was observed over `600%`.
- Redis logged pubsub connection write timeouts during/after the stress attempt.

Artifacts:

- The harness created partial metrics only because it failed before writing `summary.json`.
- Partial metrics are under `test-results/k8s-phase1-profiles/worst-case/2026-04-29T06-36-55Z-throughput-k8s-worst-case-throughput-phase1-local-pvc/metrics/`.

## Interpretation

The standard profile passed cleanly for correctness under Kubernetes-run load generation.

The worst-case profile did not reach the 600-message throughput stage because the local Phase 1 target could not finish provisioning and connecting the 1000-client topology. The failure was an environment/capacity limit in this local lab shape, not a completed worst-case correctness failure. To properly complete worst-case per `PLAN.md`, use the recommended multi-node or higher-memory Phase 1 lab, or run Phase 2 with in-cluster/managed data dependencies sized separately from the loadgen node.

## Follow-Up Required For Full Worst-Case Validation

Run the worst-case profile on a cluster closer to the plan sizing:

- Loadgen nodes separate from app nodes.
- App target with enough memory for 1000 WebSocket clients and provisioning churn.
- Redis isolated from the gateway host.
- Kafka/Cassandra enabled if validating async queued delivery behavior.
- Preserve PVC-backed report volumes or replace `emptyDir` with an artifact upload step.

Also fix `--base-url` handling in `testing/stress/run.js` or standardize only on `OHMF_STRESS_BASE_URL` for Kubernetes Jobs.
