# OHMF Stress Harness

This directory contains the first runnable end-to-end stress harness for OHMF messaging.

It is designed to validate the distributed message lifecycle rather than only HTTP route health:

```text
Client -> Gateway -> Kafka/processors -> storage -> Redis/realtime -> client devices
```

## What It Does Today

The harness provisions real users and linked devices through the existing OTP flow, opens live WebSocket sessions, sends messages through the gateway, validates message persistence through the conversation API, and writes machine-readable reports.

If you are using it to validate the Kafka-backed release path, run it against a stack with async messaging enabled and check `queued_accepts` in the summary output. A run with `queued_accepts: 0` exercised the synchronous path instead.

Implemented scenarios:

- `smoke`: small deterministic end-to-end validation
- `throughput`: fixed-rate message stream across multiple DM conversations
- `reconnect`: disconnect one linked device mid-run, then verify convergence after reconnect
- `connect`: connection-ramp validation with real device provisioning and WebSocket fan-in, without message traffic
- `reconnect-storm`: coordinated disconnect and batched reconnect validation, optionally reusing a saved device topology
- `send-abort`: forward a message to the gateway, drop the client connection before the response returns, then retry the same `idempotency_key`
- `high-latency-link`: delay and drop the first send before it reaches the gateway, force a client timeout, then retry the same `idempotency_key`
- `block-race`: race `POST /v1/messages` against `POST /v1/blocks/{id}` and verify that later sends are blocked even when the first race winner varies

There is also a suite runner for multi-stage validation:

- `full-suite.js`: provisions a reusable topology, runs a 10k-class connect stage, message stages, processor outage injection, and a reconnect storm as one reproducible suite

Artifacts are written under `testing/stress/reports/`.

## Kubernetes Note

For Kubernetes-based performance and chaos testing, use the single-scenario runner in `run.js` together with the manifests under `ohmf/infra/k8s`.

The multi-stage suite runners (`capacity-suite.js` and `full-suite.js`) still inject failures and recover services by calling local Docker directly. They are appropriate for the local Docker stack, but not yet the correct control plane for in-cluster failure injection.

## Running It

From the repo root:

```powershell
npm run test:stress
```

Direct usage:

```powershell
node .\testing\stress\run.js --scenario smoke --base-url http://127.0.0.1:18080
node .\testing\stress\run.js --scenario throughput --users 4 --devices-per-user 2 --rate 20 --duration-ms 30000
node .\testing\stress\run.js --scenario reconnect --devices-per-user 2 --messages 12
node .\testing\stress\run.js --scenario connect --users 500 --devices-per-user 1 --connect-delay-ms 20 --connect-timeout-ms 1000 --hold-ms 15000
node .\testing\stress\run.js --scenario reconnect-storm --topology-file .\testing\stress\topologies\500-devices.json --reconnect-storm-size 500 --reconnect-batch-size 50 --reconnect-batch-interval-ms 1000 --hold-ms 5000
node .\testing\stress\run.js --scenario send-abort --users 2 --devices-per-user 1 --send-timeout-ms 5000
node .\testing\stress\run.js --scenario high-latency-link --users 2 --devices-per-user 1 --send-timeout-ms 5000 --fault-request-delay-ms 5500
node .\testing\stress\run.js --scenario block-race --users 2 --devices-per-user 1 --race-iterations 10
node .\testing\stress\full-suite.js --total-clients 10000 --unique-user-ratio 0.75 --messages-per-stage 600 --rate 120
```

Container image for cluster jobs:

```powershell
docker build -f testing/stress/Dockerfile -t ohmf-stress:dev .
```

## Key Options

- `--scenario`: `smoke`, `throughput`, `reconnect`, `connect`, `reconnect-storm`, `send-abort`, `high-latency-link`, or `block-race`
- `--base-url`: gateway base URL
- `--ws-version`: `v1` or `v2`
- `--users`: logical users to provision
- `--devices-per-user`: linked devices per user
- `--total-clients`: total connected devices to provision when using a mixed per-user topology
- `--unique-user-ratio`: fraction of clients that should map to unique users; `0.75` means `100` clients are spread across `75` users
- `--active-conversations`: limit for throughput/reconnect message fanout conversations; useful when the connected topology is much larger than the active message set
- `--messages`: total messages to send
- `--rate`: target send rate in messages per second
- `--duration-ms`: duration budget for throughput mode when `--messages` is omitted
- `--hold-ms`: how long to keep the ramped or reconnected devices open before shutdown
- `--connect-timeout-ms`: handshake timeout before a stalled socket is counted as a failed connection
- `--reconnect-storm-size`: how many already-connected devices to drop and reconnect in `reconnect-storm`
- `--reconnect-batch-size`: reconnect batch size during a reconnect storm
- `--reconnect-batch-interval-ms`: wait time between reconnect batches
- `--reconnect-pause-ms`: pause between the coordinated disconnect and the reconnect batches
- `--race-iterations`: how many send-vs-block races to run in `block-race`
- `--fault-request-delay-ms`: local proxy delay before forwarding a targeted `/v1/messages` request
- `--fault-response-delay-ms`: local proxy delay after the gateway responds but before the client receives it
- `--fault-retry-delay-ms`: pause between the induced client-side fault and the same-key retry
- `--topology-file`: reusable topology state file with device ids and access tokens; when it exists, the harness reuses those devices instead of provisioning new ones
- `--metrics-url`: raw metrics endpoint to snapshot before and after the run
- `--report-dir`: output directory for reports

Equivalent `OHMF_STRESS_*` environment variables are also supported.

For large synthetic provisioning runs against the local Docker stack, keep the normal OTP abuse controls on by default and override them explicitly for the stress session instead of editing handler code. The Docker compose stack now exposes the gateway auth-limit env knobs, so you can raise `APP_OTP_START_PER_IP_LIMIT`, `APP_OTP_START_PER_SUBNET_LIMIT`, and `APP_OTP_VERIFY_PER_IP_LIMIT` only for the duration of the run.

For reconnect-storm work, prefer a two-step flow:

1. Create a reusable topology once with `--topology-file`.
2. Run `reconnect-storm` against that same topology file so the test exercises websocket admission and reconnect behavior rather than OTP provisioning throughput.

For large mixed-device topologies, prefer `--total-clients` with `--unique-user-ratio` instead of forcing a uniform `--devices-per-user` value. The harness will provision one device for every user first, then distribute the remaining devices across the user set.

For the edge-case scenarios:

1. `send-abort` proves that a client-side disconnect after the gateway has accepted the request still converges to one persisted message on a same-key retry.
2. `high-latency-link` proves that a client timeout before the gateway sees the request still converges to one persisted message on a same-key retry.
3. `block-race` proves that an in-flight send can win the race, but later sends with new keys are blocked once the block relationship commits.

## WebSocket Mode

Default mode is `v1`.

Reason:

- `v1` validates immediate Redis-backed realtime delivery (`message_created`, `delivery_update`)
- it is the simplest live proof path for recipient fanout across multiple devices

`v2` is also supported for environments where user-inbox replay is part of the validation target. That mode uses `/v2/ws`, the `hello` handshake, and cursor acknowledgements.

## Reports

Each run writes:

- `config.json`
- `summary.json`
- `summary.md`
- `details.json` for scenario-specific evidence such as proxy stats, retry responses, and race outcomes
- `messages.json`
- `topology.json`
- `metrics/*.prom` when metrics URLs are configured

The summary includes:

- final connected device count
- messages requested and accepted
- messages persisted
- expected and successful deliveries
- duplicate and lost delivery counts
- ordering violations
- accept and end-to-end delivery latency percentiles

## Current Boundaries

This harness is the executable foundation, not the complete release program yet.

Still planned:

- long-duration soak presets
- richer concurrency-ramp orchestration presets beyond `connect` + `--connect-delay-ms`
- broader failure-injection presets for backend outages, latency/loss shaping, and websocket-send-specific faults
- richer processor/Kafka lag ingestion
- dashboard capture automation
