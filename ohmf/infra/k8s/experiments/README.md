# OHMF Chaos Experiments

These manifests assume Chaos Mesh CRDs are installed in the cluster.

Use them together with one of the loadgen jobs under `../jobs/`:

- run a throughput job
- wait for steady traffic or reconnect activity
- apply one experiment
- measure reconnect convergence, correctness, and service recovery

Files:

- `gateway-pod-kill.yaml`: kill one gateway pod during live traffic
- `gateway-to-redis-delay.yaml`: inject latency from gateway pods to Redis
- `node-offline-runbook.md`: manual drain and hard-outage steps for app nodes
