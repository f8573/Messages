# OHMF Kubernetes Load Jobs

These jobs use `testing/stress/run.js`, not the local Docker-oriented suite runners.

That is deliberate:

- `run.js` works against any reachable OHMF gateway URL.
- `capacity-suite.js` and `full-suite.js` still use Docker stop/start calls for outage injection and recovery.

## Build the Image

From the repo root:

```powershell
docker build -f testing/stress/Dockerfile -t ohmf-stress:dev .
```

Push that image to the registry your cluster can pull from, or preload it on the nodes.

## Jobs

- `standard-throughput`: standard-rate throughput at `75%` unique users
- `worst-case-throughput`: heavy throughput at `75%` unique users
- `reconnect-storm`: reconnect surge against a `75%` unique-user topology

Each job writes reports under `/var/reports` inside the pod. The throughput examples use PVC-backed report volumes so artifacts survive completed pods and can be copied with a short-lived helper pod if needed.
