# OHMF Kubernetes Observability

These manifests are for clusters that already run Prometheus Operator and kube-state-metrics.

They add:

- a `ServiceMonitor` for the gateway `/metrics` endpoint
- starter `PrometheusRule` alerts for gateway availability, p99 accept latency, and failed loadgen jobs

The stress jobs still write correctness measurements such as duplicates, lost deliveries, ordering violations, accept latency, and delivery latency to `/var/reports`. Prometheus covers cluster and gateway signals; the job report remains the source of truth for end-to-end correctness.

Apply after the app base or perf overlay:

```powershell
kubectl apply -k ohmf/infra/k8s/observability
```
