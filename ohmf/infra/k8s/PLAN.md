# OHMF Kubernetes Cluster Plan

## Goal

Create a repeatable Kubernetes test lab that can answer three questions:

1. How does OHMF scale under realistic and worst-case client mixes?
2. What happens when app nodes, pods, or dependencies fail mid-run?
3. Does the distributed design preserve correctness while the cluster is degraded?

## Recommended Topology

Use a three-zone cluster with isolated node pools.

| Pool | Label | Suggested Size | Purpose |
| --- | --- | --- | --- |
| app | `ohmf.node-role=app` | 4 nodes, `8 vCPU / 32 GiB` | gateway and processors |
| loadgen | `ohmf.node-role=loadgen` | 2 nodes, `8 vCPU / 32 GiB` | stress harness jobs |
| observability | `ohmf.node-role=observability` | 2 nodes, `4 vCPU / 16 GiB` | Prometheus, Grafana, logs |
| data | separate operators or managed services | 3+ nodes per stateful system | Kafka, Cassandra, Redis, Postgres |

Node pools should be spread across at least three zones.

## Namespace Model

- `ohmf-app`: gateway and processors
- `ohmf-data`: stateful systems if they are brought in-cluster later
- `ohmf-loadgen`: stress jobs and reusable report pods
- `ohmf-observability`: Prometheus, Grafana, log shipping
- `ohmf-chaos`: Chaos Mesh or other failure-injection tooling

## Scheduling Rules

Every app deployment should use:

- `nodeSelector` for the app pool
- zone-aware `topologySpreadConstraints`
- per-host anti-affinity
- PDBs so one drain or voluntary disruption cannot evict the entire tier

Load jobs should never share nodes with the app tier. That keeps CPU starvation and kernel socket pressure out of the measurement path.

## Workload Shape

### Gateway

- Deployment
- base replica count: `3`
- perf-lab replica count: `6`
- HPA on CPU and memory
- ClusterIP service for in-cluster loadgen and ingress-backed external access
- readiness and liveness probes on `GET /healthz`

### Messages Processor

- Deployment
- base replica count: `3`
- perf-lab replica count: `6`
- HPA on CPU
- no public service

### Delivery Processor

- Deployment
- base replica count: `3`
- perf-lab replica count: `6`
- HPA on CPU
- no public service

### SMS Processor

- Deployment
- base replica count: `2`
- perf-lab replica count: `3`
- no public service

## Data Dependencies

Use one of these two modes:

### Phase 1: Hybrid

- gateway and processors in Kubernetes
- managed or externally hosted Postgres
- managed or externally hosted Redis
- managed Kafka
- optional external Cassandra

This is the fastest path to useful scaling numbers because it removes stateful-operator noise from the initial test cycle.

### Phase 2: Full Cluster

- Kafka via Strimzi
- Postgres via CloudNativePG or managed HA
- Cassandra via K8ssandra
- Redis via operator-backed HA or managed Redis

This is the right phase for broker failure, quorum, and storage-failure drills.

## Performance Profiles

### Standard Profile

- `75%` unique users to client ratio
- `180` messages per stage at `2.5 msg/s`
- moderate active conversation set
- occasional reconnects
- rare induced latency timeout

### Worst-Case Profile

- `75%` unique users to client ratio
- `600` messages per stage at `120 msg/s`
- larger active conversation set
- reconnect storms
- pod kills, node drains, and dependency latency injected during traffic

## Failure Scenarios

### App Tier

- kill one gateway pod during steady traffic
- kill 25% of gateway pods during reconnect storm
- drain one app node during throughput
- force one processor pod restart while sends are active

### Dependency Tier

- inject latency between gateway and Redis
- inject latency between gateway and Kafka
- fail a Kafka broker
- fail Redis primary
- force Postgres failover

### Distributed System Behavior

- rebuild topology after gateway eviction
- hold correctness during partial zone loss
- verify queued responses during dependency impairment

## Measurements

Collect these during every performance or chaos run:

- websocket active connections
- `/v1/ws` success, rejection, and failure rates
- `/v1/messages` `201`, `202`, and `5xx`
- gateway accept latency p95/p99
- end-to-end delivery latency p95/p99
- Kafka lag by consumer group
- Redis command latency
- Postgres pool saturation
- duplicates
- lost deliveries
- ordering violations
- reconnect convergence time

## Node-Offload and Node-Loss Drills

Use two kinds of node tests:

1. Graceful drain:
   - validates PDBs, rescheduling, and reconnect convergence
2. Hard outage:
   - validates what happens when a node disappears without eviction

The hard-outage test should be done through the cloud provider or node power-off path, not by deleting the Node object from Kubernetes.

## Known Gap

The current multi-stage suite runners in `testing/stress/capacity-suite.js` and `testing/stress/full-suite.js` still drive outage injection by calling Docker directly. They remain correct for the local Docker stack, but not for Kubernetes.

The Kubernetes path in this repo is therefore:

- deploy app tier with Kustomize
- run `testing/stress/run.js` in a loadgen Job
- inject faults through Kubernetes-native chaos or manual node operations

That separation is intentional until the stress harness grows a Kubernetes-aware failure backend.
