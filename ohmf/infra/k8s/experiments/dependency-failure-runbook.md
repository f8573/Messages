# Dependency Failure Runbook

Use this when Kafka, Redis, Postgres, or Cassandra are managed services or operator-owned stateful systems. Keep the stress job running while each dependency action is performed.

## Before Each Drill

1. Start either `standard-throughput` or `worst-case-throughput`.
2. Confirm gateway replicas are healthy:

```powershell
kubectl -n ohmf-app get deploy,pod -l app.kubernetes.io/name=gateway
```

3. Start a watch on loadgen logs and reports:

```powershell
kubectl -n ohmf-loadgen logs -f job/ohmf-worst-case-throughput
```

## Kafka Broker Failure

Use the Kafka operator or provider-native maintenance action to stop one broker. Do not delete PVCs.

Watch:

- consumer group lag recovery
- `/v1/messages` `202` rate
- delivery latency p95/p99
- lost deliveries and duplicates in the loadgen report

## Redis Primary Failure

Use the Redis operator or provider failover command to promote a replica.

Watch:

- gateway error rate
- Redis command latency
- websocket reconnect convergence
- duplicate and ordering violation counts

## Postgres Failover

Use CloudNativePG, the managed provider, or the HA proxy layer to force a primary failover.

Watch:

- connection pool saturation
- message persistence latency
- queued response behavior
- persisted message count versus accepted message count

## Cassandra Impairment

If Cassandra is in the run path, stop one node or inject operator-supported network delay.

Watch:

- quorum write errors
- delivery timeline query latency
- ordering violations after recovery

## Pass Criteria

- loadgen exits successfully
- duplicates are `0`
- lost deliveries are `0`
- ordering violations are `0`
- gateway replicas recover to the target count
- dependency lag or failover state returns to baseline
