# App Node Offline Runbook

Use this while a stress job is active.

## Graceful Drain

1. Pick one node from the app pool:

```powershell
kubectl get nodes -l ohmf.node-role=app -o wide
```

2. Drain it:

```powershell
kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data --grace-period=30 --timeout=5m
```

3. Watch:
   - gateway pod rescheduling
   - websocket reconnect time
   - message accept and delivery latency
   - any duplicates or lost deliveries

4. Return the node:

```powershell
kubectl uncordon <node-name>
```

## Hard Outage

For a real node-loss drill, stop the VM or instance through the infrastructure provider instead of deleting the Node object.

Expected validation points:

- remaining gateway replicas stay available
- PDBs prevent total voluntary eviction during normal drain
- reconnect storm converges without message duplication
- worker lag recovers after rescheduling
