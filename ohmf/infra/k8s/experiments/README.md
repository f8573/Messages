# OHMF Chaos Experiments

These manifests assume Chaos Mesh CRDs are installed in the cluster.

Use them together with one of the loadgen jobs under `../jobs/`:

- run a throughput job
- wait for steady traffic or reconnect activity
- apply one experiment
- measure reconnect convergence, correctness, and service recovery

Files:

- `gateway-pod-kill.yaml`: kill one gateway pod during live traffic
- `gateway-reconnect-storm-pod-kill.yaml`: kill 25% of gateway pods during a reconnect storm
- `processor-pod-restart.yaml`: force one processor pod restart while sends are active
- `gateway-to-redis-delay.yaml`: inject latency from gateway pods to Redis
- `gateway-to-kafka-delay.yaml`: inject latency from gateway pods to Kafka
- `node-offline-runbook.md`: manual drain and hard-outage steps for app nodes
- `dependency-failure-runbook.md`: broker, Redis, and Postgres failure drill checklist

Apply individual experiments for controlled runs. Applying the whole directory runs every included Chaos Mesh experiment:

```powershell
kubectl apply -k ohmf/infra/k8s/experiments
```
