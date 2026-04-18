# OHMF End-To-End Stress Testing Plan

This document defines how stress testing should plug into the current OHMF architecture and what must be proven before performance claims are treated as release-quality evidence.

## Why This Exists

The existing performance gate is useful but narrow:
- `npm run test:perf` currently runs race detection and targeted benchmarks through [`scripts/test-gates.js`](C:/Users/James/Downloads/Messages/scripts/test-gates.js)
- `scripts/run-perf-race.sh` exercises gateway internals only

That is not enough to validate the distributed messaging system as a whole.

The stress test target is the full production-style path:

```text
Client -> Gateway -> Kafka -> Processors -> DB (Postgres/Cassandra) -> Redis -> Realtime delivery -> Clients
```

The goal is not "HTTP throughput". The goal is proving the end-to-end messaging lifecycle under load.

## Testable Contract

Before writing a load harness, define the system contract for a single message.

A message is only considered successful when it is:
- accepted by the gateway
- published to Kafka
- processed by the message processor
- persisted to durable storage
- dispatched for downstream delivery
- delivered to every intended recipient device
- acknowledged or otherwise observed as received by the target clients

For every generated message, the validator must prove:
- accepted
- persisted
- delivered to all expected devices
- not duplicated
- not lost

This contract is the ground truth for every stress report.

## Current Repo Hooks

The current repo already provides enough structure to anchor stress testing:

- Deployment stack: [`ohmf/infra/docker/docker-compose.yml`](C:/Users/James/Downloads/Messages/ohmf/infra/docker/docker-compose.yml)
- Standardized gates: [`scripts/test-gates.js`](C:/Users/James/Downloads/Messages/scripts/test-gates.js)
- Test gate overview: [`TESTING.md`](C:/Users/James/Downloads/Messages/TESTING.md)
- Release/staging signoff: [`testing/STAGING_CHECKLIST.md`](C:/Users/James/Downloads/Messages/testing/STAGING_CHECKLIST.md)
- Prometheus/Grafana assets: [`ohmf/infra/observability/README.md`](C:/Users/James/Downloads/Messages/ohmf/infra/observability/README.md)

Current metrics already exposed:
- lightweight services: HTTP totals, duration, in-flight via [`ohmf/pkg/observability/observability.go`](C:/Users/James/Downloads/Messages/ohmf/pkg/observability/observability.go)
- gateway: HTTP totals, route latency, in-flight requests, active WebSocket connections, and WebSocket message counts via [`ohmf/services/gateway/internal/observability/metrics.go`](C:/Users/James/Downloads/Messages/ohmf/services/gateway/internal/observability/metrics.go)

Current gap:
- the repo now includes a first-pass stateful harness under [testing/stress/](C:/Users/James/Downloads/Messages/testing/stress), but it still needs richer presets and failure orchestration
- the repo still does not expose enough processor, Kafka, delivery, and DB-level metrics to prove the messaging contract under load on its own

## Required Instrumentation

Stress testing should extend the current observability baseline rather than bypass it.

### Gateway

Already present:
- active WebSocket connections
- HTTP request counts and latency
- WebSocket message counters

Must add:
- incoming message rate
- auth latency breakdown
- send-message latency by route
- p50/p95/p99 request latency dashboards

### Kafka Layer

Must expose or collect:
- topic backlog / consumer lag
- messages in per topic
- messages out per topic
- consumer lag by partition and consumer group

Relevant topic set from the current compose stack:
- `msg.ingress.v1`
- `msg.persisted.v1`
- `msg.delivery.v1`
- `msg.sms.dispatch.v1`
- `msg.ingress.dlq.v1`
- `msg.delivery.dlq.v1`
- `msg.sms.dlq.v1`

### Message Processor

Must add:
- processing latency histogram
- processed-success count
- processed-failure count
- retry count
- backlog size / queue age

### Delivery Processor

Must add:
- delivery attempts
- delivery success count
- delivery failure count
- retry count
- duplicate-detection count

### Database And Cache

Must add or collect:
- Postgres query latency
- Postgres connection pool usage
- Cassandra query latency when enabled
- Redis latency and connection pressure

### Dashboard Requirement

Prometheus and Grafana should be the default output path for stress runs. Every serious run should produce:
- latency graphs
- connection graphs
- resource graphs
- Kafka lag graphs

## Load Generator Requirements

Do not treat this as a generic stateless HTTP benchmark.

The harness must be a stateful client simulator.

It should simulate:
- users
- 1 to 3 devices per user
- independent session tokens per device
- independent WebSocket connections per device
- conversation membership
- send, receive, ack, disconnect, reconnect, and sync behavior

### Minimum Simulated Behaviors

Each virtual device should be able to:
- authenticate or reuse a valid session token
- establish a WebSocket connection
- join or resume known conversations
- send messages through the gateway
- receive realtime events
- confirm receipt and ordering
- disconnect and reconnect at random intervals
- run sync after reconnect and verify state convergence

### Multi-Device Correctness

This system explicitly supports multi-device sessions, so the stress harness must verify:
- a message reaches all devices for the same user when expected
- no duplicate deliveries occur across reconnects
- ordering remains consistent within a conversation
- sync after reconnect heals state correctly

This is mandatory. It is one of the strongest proofs the system can produce.

## Correctness Tracker

The stress harness needs a separate correctness tracker, not just throughput logs.

Track per message:

```text
message_id
sender_user_id
sender_device_id
conversation_id
expected_recipient_devices
actual_receipts
timestamps
delivery_attempts
```

From that, compute:
- delivery success rate
- duplicate rate
- loss rate
- ordering correctness
- reconnect recovery correctness

Example report table:

| Metric | Example |
|---|---|
| Messages sent | 250000 |
| Expected deliveries | 750000 |
| Successful deliveries | 750000 |
| Duplicates | 0 |
| Lost | 0 |

## Required Test Runs

The following runs should become the standard end-to-end stress suite.

### 1. Concurrency Ramp

Increase connected clients in steps:
- 100
- 500
- 1000
- 2000

Measure:
- connection stability
- gateway CPU and memory
- active WebSocket count
- Kafka lag
- p95 delivery latency

Primary output:
- max stable concurrent connected devices

### 2. Throughput Ramp

Hold clients roughly constant and increase send rate:
- 10 messages per second
- 50 messages per second
- 100 messages per second
- 200+ messages per second

Measure:
- queue backlog
- processor latency
- DB pressure
- end-to-end delivery latency

Primary output:
- sustained messages per second with stable latency and zero correctness failures

### 3. Soak Test

Run:
- 6 to 12 hours
- realistic message cadence
- reconnects
- group churn
- multi-device fanout

Measure:
- memory growth
- queue drift
- reconnection correctness
- duplicate or loss accumulation over time

Primary output:
- proof of no slow degradation under sustained traffic

### 4. Failure Injection

Inject failures while traffic is active:
- restart gateway
- stop message processor
- stop delivery processor
- slow or constrain DB
- trigger Kafka lag spike or broker disruption

Measure:
- reconnect success rate
- backlog growth
- backlog drain time
- recovery latency
- message loss and duplicate count during recovery

Primary output:
- graceful recovery time and correctness under failure

### 5. Reconnect Storm

Simulate:
- 500 to 1000 clients disconnecting at once
- simultaneous reconnects

Measure:
- auth success rate
- time to restore active sessions
- sync correctness after recovery

Primary output:
- full recovery time after coordinated reconnect surge

## Proof Artifacts

Every run should generate three categories of output.

### 1. Run Summary

Minimum required fields:

```text
Test name
Commit SHA
Date
Duration
Client/device counts
Send rate
Max stable connections
p95 send latency
p95 delivery latency
Message loss rate
Duplicate rate
Recovery time after failure
```

### 2. Graphs

Capture from Grafana:
- latency over time
- active connections over time
- CPU and memory
- Kafka lag
- processor backlog

### 3. Raw Outputs

Persist:
- correctness tracker CSV or JSON
- aggregate summary JSON
- validator error logs

This makes the run reproducible and reviewable.

## Recommended Repo Additions

The next implementation pass should add:
- long-running soak and concurrency-ramp presets on top of the existing harness
- dashboard panels for gateway, Kafka, processor, delivery, and DB stress views
- failure-injection helpers that integrate with the existing harness

Suggested future layout:

```text
testing/
  STRESS_TESTING_PLAN.md
  stress/
    README.md
    lib/
    reports/
```

## Release Signoff Expectation

Before making resume-style or release-level performance claims, the repo should be able to prove:
- max stable connected device count
- sustained message throughput
- p95 and p99 end-to-end delivery latency
- zero-loss or explicitly bounded-loss behavior
- duplicate rate
- multi-device correctness after reconnect
- failure recovery time

Until then, performance claims should be described as preliminary rather than validated.

## Short Version

If the repo only does route benchmarks and race tests, it has performance checks.

If the repo proves:
- accepted
- persisted
- delivered to every expected device
- not duplicated
- not lost
- stable under reconnect and failure

then it has a defensible end-to-end stress story.
