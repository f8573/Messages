# OHMF Kubernetes Performance Lab

This tree is a first-pass Kubernetes layout for distributed performance and failure testing.

It is intentionally opinionated about the test shape:

- stateless app pods in one pool
- load generators in a separate pool
- observability in its own pool
- data systems either managed externally or run by their own operators
- chaos injected by Kubernetes-native tools instead of Docker restarts

The manifests here are Kustomize-based and focus on the app tier that already exists in this repo:

- `gateway`
- `messages-processor`
- `delivery-processor`
- `sms-processor`

This layout does not attempt to fully own Kafka, Cassandra, Redis, or Postgres. Those are better run as managed services or operator-backed stateful workloads. The app manifests expect those endpoints to be supplied through a secret.

## Layout

- `PLAN.md`: cluster structure, sizing, failure model, and test plan
- `base/`: namespaces, app deployments, HPAs, services, and PDBs
- `overlays/perf-lab/`: performance-test replica counts and relaxed admission limits
- `jobs/`: loadgen jobs that run the existing `testing/stress/run.js` scenarios in-cluster
- `observability/`: Prometheus Operator scrape and alert resources
- `experiments/`: chaos templates and node-offline runbook
- `templates/`: secret examples and operator-facing placeholders

## Quick Start

1. Label worker nodes by pool:
   - app nodes: `ohmf.node-role=app`
   - load generator nodes: `ohmf.node-role=loadgen`
   - observability nodes: `ohmf.node-role=observability`

2. Build the stress image from the repo root:

```powershell
docker build -f testing/stress/Dockerfile -t ohmf-stress:dev .
```

3. Create the app secret from the example in `templates/app-secrets.example.yaml`.

4. Render the performance overlay:

```powershell
kubectl kustomize ohmf/infra/k8s/overlays/perf-lab
```

5. Apply the performance overlay:

```powershell
kubectl apply -k ohmf/infra/k8s/overlays/perf-lab
```

6. Launch a load job:

```powershell
kubectl apply -k ohmf/infra/k8s/jobs/standard-throughput
kubectl apply -k ohmf/infra/k8s/jobs/worst-case-throughput
```

7. If Prometheus Operator is installed, apply the scrape and alert resources:

```powershell
kubectl apply -k ohmf/infra/k8s/observability
```

8. Run chaos while the job is active:
   - `kubectl apply -k ohmf/infra/k8s/experiments`
   - follow the node drain runbook in `experiments/node-offline-runbook.md`
   - follow the dependency drill runbook in `experiments/dependency-failure-runbook.md`

## Important Limits

The existing local multi-stage suites are not yet Kubernetes-native.

- `testing/stress/run.js` is safe to run in-cluster.
- `testing/stress/capacity-suite.js` and `testing/stress/full-suite.js` still depend on local Docker restart/stop behavior for outage injection and recovery.

That is why the Kubernetes jobs here use `run.js`, while outage and node-failure drills are modeled as separate chaos actions.
