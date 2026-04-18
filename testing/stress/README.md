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

Artifacts are written under `testing/stress/reports/`.

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
```

## Key Options

- `--scenario`: `smoke`, `throughput`, or `reconnect`
- `--base-url`: gateway base URL
- `--ws-version`: `v1` or `v2`
- `--users`: logical users to provision
- `--devices-per-user`: linked devices per user
- `--messages`: total messages to send
- `--rate`: target send rate in messages per second
- `--duration-ms`: duration budget for throughput mode when `--messages` is omitted
- `--metrics-url`: raw metrics endpoint to snapshot before and after the run
- `--report-dir`: output directory for reports

Equivalent `OHMF_STRESS_*` environment variables are also supported.

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
- `messages.json`
- `topology.json`
- `metrics/*.prom` when metrics URLs are configured

The summary includes:

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
- concurrency-ramp orchestration
- failure-injection helpers
- richer processor/Kafka lag ingestion
- dashboard capture automation
