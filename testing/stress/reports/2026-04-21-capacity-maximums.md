# OHMF Capacity Maximums

Date: 2026-04-21  
Environment: local Docker stack at `http://127.0.0.1:18080`  
WebSocket mode: `v1`  
Primary direct-capacity measurement state: `1b8e5b6`  
Delayed ingress comparison state: `56e1aa1` plus `testing/stress/tcp-delay-proxy.js`  
Client-to-user mix: `75%` unique users (`100` clients -> `75` logical users)

## Test Conditions

These capacity runs were measured after the suite sequencing fix landed. During the capacity search, the gateway OTP, websocket admission, and send-rate limits were temporarily raised so the measured ceilings reflected application/runtime capacity rather than abuse-limit throttling. The gateway was restored to its default local limits after measurement.

Primary direct-capacity artifacts:

- Worst-case passing suite: `testing/stress/reports/2026-04-21T20-09-43Z-capacity-worst-case-1300-max-search/capacity-suite-summary.json`
- Worst-case next failure: `testing/stress/reports/2026-04-21T19-25-33Z-capacity-worst-case-1350-max-search/capacity-suite-summary.json`
- Standard-case passing suite: `testing/stress/reports/2026-04-21T20-14-11Z-capacity-normal-3100-max-search/capacity-suite-summary.json`
- Standard-case next failure: `testing/stress/reports/2026-04-21T19-27-33Z-capacity-normal-3250-max-search/capacity-suite-summary.json`

Representative delayed multi-process comparison artifacts:

- Worst-case delayed comparison: `testing/stress/reports/2026-04-21T21-41-56Z-capacity-worst-case-1000-delay30ms/capacity-suite-summary.json`
- Standard-case delayed comparison: `testing/stress/reports/2026-04-21T21-47-13Z-capacity-normal-2500-delay30ms/capacity-suite-summary.json`

## Capacity Summary

### Maximum Worst Case

- Highest verified passing client count: `1300`
- Logical users: `975`
- Active conversations: `250`
- Next tested failure bound: `1350` clients / `1013` logical users

Worst-case suite parameters:

- `messages-heavy-*`: `600` requested messages at `120 msg/s`, `send_concurrency=8`
- `reconnect-storm-*`: `1000` forced reconnects, `250` reconnect batch size, `250 ms` between batches, `1000 ms` pause before reconnect, `3000 ms` hold after reconnect
- `messages-delivery-outage`: `ohmf-delivery-processor` stopped after `2000 ms` for `8000 ms`
- `send-abort`: same-key retry after induced client disconnect, `send_timeout_ms=5000`
- `high-latency-link`: first send delayed/dropped with `fault_request_delay_ms=5500`, same-key retry after `250 ms`
- `block-race`: `10` concurrent send-vs-block races
- `messages-persist-outage`: `ohmf-messages-processor` stopped after `2000 ms` for `8000 ms`

Passing worst-case stage metrics at `1300`:

- `connect`: duration `24054 ms`; connected `1300`; requested/accepted/persisted `0/0/0`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `0/0/0/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`.
- `messages-heavy-a`: duration `33394 ms`; connected `1300`; requested/accepted/persisted `600/600/600`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `800/800/800/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `285 ms`; delivery latency p95 `286 ms`.
- `reconnect-storm-a`: duration `13852 ms`; connected `1300`; requested/accepted/persisted `0/0/0`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `0/0/0/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`.
- `messages-delivery-outage`: duration `33537 ms`; connected `1300`; requested/accepted/persisted `600/600/600`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `800/800/800/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `327 ms`; delivery latency p95 `299 ms`.
- `send-abort`: duration `7073 ms`; connected `1300`; requested/accepted/persisted `1/1/1`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `1/1/1/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `398 ms`; delivery latency p95 `93 ms`.
- `high-latency-link`: duration `11438 ms`; connected `1300`; requested/accepted/persisted `1/1/1`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `1/1/1/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `5382 ms`; delivery latency p95 `5328 ms`.
- `block-race`: duration `8768 ms`; connected `1300`; requested/accepted/persisted `10/10/10`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `10/10/10/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `214 ms`; delivery latency p95 `131 ms`.
- `reconnect-storm-b`: duration `13793 ms`; connected `1300`; requested/accepted/persisted `0/0/0`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `0/0/0/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`.
- `messages-persist-outage`: duration `39449 ms`; connected `1300`; requested/accepted/persisted `600/600/600`; queued accepts `24`; deliveries expected/successful/realtime/sync/lost `800/800/800/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `789 ms`; delivery latency p95 `796 ms`.

### Maximum Standard Case

- Highest verified passing client count: `3100`
- Logical users: `2325`
- Active conversations: `116`
- Next tested failure bound: `3250` clients / `2438` logical users

Standard-case suite parameters:

- `messages-steady-*`: `180` requested messages at `2.5 msg/s`, `send_concurrency=3`
- `reconnect-storm-light`: `155` forced reconnects, `50` reconnect batch size, `1000 ms` between batches, `1000 ms` pause before reconnect, `2000 ms` hold after reconnect
- `high-latency-link-rare`: one induced timeout/delay case with `send_timeout_ms=5000`, `fault_request_delay_ms=5500`, `fault_retry_delay_ms=250`

Passing standard-case stage metrics at `3100`:

- `connect`: duration `55012 ms`; connected `3100`; requested/accepted/persisted `0/0/0`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `0/0/0/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`.
- `messages-steady-a`: duration `93864 ms`; connected `3100`; requested/accepted/persisted `180/180/180`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `241/241/241/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `170 ms`; delivery latency p95 `144 ms`.
- `reconnect-storm-light`: duration `44543 ms`; connected `3100`; requested/accepted/persisted `0/0/0`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `0/0/0/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`.
- `messages-steady-b`: duration `89206 ms`; connected `3100`; requested/accepted/persisted `180/180/180`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `241/241/241/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `79 ms`; delivery latency p95 `56 ms`.
- `high-latency-link-rare`: duration `22589 ms`; connected `3100`; requested/accepted/persisted `1/1/1`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `1/1/1/0/0`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `5417 ms`; delivery latency p95 `5349 ms`.

## Why The Failures Happened

### Worst-Case Failure At `1350`

This failure is evidence-backed as a delivery / receipt-convergence collapse, not an ingress or persistence failure.

Stage summary evidence:

- `600/600` messages were accepted.
- `600/600` messages were persisted.
- `0/799` expected deliveries completed.
- `799` deliveries were marked lost.
- `0` client errors were recorded.
- Validation warning: `timed out waiting for all expected device receipts after 20000ms`

Start/end metric snapshot deltas from `testing/stress/reports/2026-04-21T19-26-11Z-throughput-worst-case-1350-max-search-messages-heavy-a/metrics`:

These are stage-local pre/post scrapes. A `0 -> N` websocket count here means the throughput stage rebuilt its own topology during the run rather than inheriting open sockets from the earlier suite stage.

- gateway RSS: `12.84 GB -> 14.83 GB`
- goroutines: `53,087 -> 67,590`
- active websocket connections: `0 -> 1350`
- `POST /v1/auth/refresh 200`: `6000 -> 7350` (`+1350`)
- `POST /v1/messages 201`: `182 -> 782` (`+600`)
- websocket `delivery_update` sends: `484 -> 484` (`+0`)

Why that points at delivery fanout instead of ingress:

- The stage did the expensive front-half work successfully. Devices authenticated, connected, and every intended send returned `201`.
- The stage did not do the back-half work. No delivery latencies were recorded, no receipts converged, and the websocket `delivery_update` counter stayed flat even while `600` new messages were accepted.
- The relevant live-delivery paths are the delivery processor and the gateway online-delivery fallback, both of which require a Redis presence hit plus a delivery-row insert before publishing `delivery:user:*` updates.
  - `ohmf/services/delivery-processor/cmd/processor/main.go:105`
  - `ohmf/services/delivery-processor/cmd/processor/main.go:121`
  - `ohmf/services/delivery-processor/cmd/processor/main.go:139`
  - `ohmf/services/delivery-processor/cmd/processor/main.go:141`
  - `ohmf/services/gateway/internal/messages/service.go:3389`
  - `ohmf/services/gateway/internal/messages/service.go:3402`
  - `ohmf/services/gateway/internal/messages/service.go:3408`
  - `ohmf/services/gateway/internal/messages/service.go:3432`

Likely subsystem under stress:

- realtime delivery fanout and receipt publication for online users
- not the send ingress path
- not durable message persistence

The end snapshot did not show a stuck Postgres pool (`0` acquired, `32` idle), so this does not look like the earlier pool-saturation issue. That does not prove there was no transient database pressure during the run, only that the stage was not wedged there when the final metrics were sampled.

### Standard-Case Failure At `3250`

This failure is evidence-backed as a topology-bootstrap / reconnect non-convergence failure, not a message-correctness failure.

Stage summary evidence:

- The `connect` stage still reached `3250` connected devices.
- The first steady-message stage never produced a summary artifact.
- The suite killed that stage after `1800000 ms`.
- Stage stderr: `failed to connect stress-user-2028/stress-user-2028-device-1`

Start/end metric snapshot deltas from `testing/stress/reports/2026-04-21T19-34-39Z-throughput-normal-3250-max-search-messages-steady-a/metrics`:

These are stage-local pre/post scrapes. A `0 -> N` websocket count here means the steady-message stage was still rebuilding its own topology when the first scrape was taken.

- gateway RSS: `21.92 GB -> 26.30 GB`
- goroutines: `84,075 -> 107,110`
- active websocket connections: `0 -> 2797`
- `POST /v1/auth/refresh 200`: `7350 -> 10600` (`+3250`)
- `POST /v1/messages 201`: `782 -> 782` (`+0`)
- websocket `delivery_update` sends: `484 -> 484` (`+0`)

Why that points at bootstrap saturation instead of messaging:

- The stage never sent any test traffic. The message-ingress counter stayed exactly flat, and no new delivery updates were emitted.
- What did move was connection setup work: the stage performed `3250` successful token refreshes, drove memory and goroutine growth sharply upward, and still finished with only `2797` active sockets, `453` short of the required `3250`.
- The stress harness throws before message sending when a non-optional device cannot reconnect into the active topology:
  - `testing/stress/run.js:659`

Likely subsystem under stress:

- websocket client bootstrap and reconnect convergence
- session/presence establishment under a large steady-state rebuild
- not the message send, persistence, or delivery-correctness path, because those code paths never started for this stage

This is why the `3250` result should be read as "the system can pass a dedicated connect ramp at this size, but it cannot reliably rebuild the full messaging topology and enter steady traffic beyond `3100`."

## Scaling Efficiency

### Worst-Case Load Curve

Worst-case reference stage: `messages-heavy-a`

| Clients | Logical Users | Accept P95 | Delivery P95 | Deliveries | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| 1000 | 750 | 283 ms | 281 ms | 800 / 800 | Pass |
| 1200 | 900 | 283 ms | 284 ms | 800 / 800 | Pass |
| 1275 | 957 | 333 ms | 306 ms | 800 / 800 | Pass |
| 1300 | 975 | 285 ms | 286 ms | 800 / 800 | Pass |
| 1350 | 1013 | 553 ms | n/a | 0 / 799 | Fail |

Interpretation:

- Scaling is effectively flat through `1300` clients for the worst-case heavy-send stage. The system completes the entire delivery budget at every tested step up to `1300`, and p95 latency stays in the same rough band rather than drifting continuously upward.
- There is no graceful taper after that. The system cliffs between `1300` and `1350`: latency doubles on accepts, delivery p95 disappears because no deliveries converge, and the run fails despite full message persistence.

Operational statement: linear enough until about `1300`, then a hard degradation cliff after `1300`.

### Standard-Case Load Curve

Standard-case reference stage: `messages-steady-a`

| Clients | Logical Users | Accept P95 | Delivery P95 | Deliveries | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| 1900 | 1425 | 79 ms | 56 ms | 239 / 239 | Pass |
| 2500 | 1875 | 124 ms | 90 ms | 239 / 239 | Pass |
| 3000 | 2250 | 102 ms | 81 ms | 239 / 239 | Pass |
| 3100 | 2325 | 170 ms | 144 ms | 241 / 241 | Pass |
| 3250 | 2438 | n/a | n/a | n/a | Timed out before summary |

Interpretation:

- Standard traffic is stable through `3000` clients. Message completion stays perfect and p95 latency remains well below `200 ms`.
- There is a visible knee at `3100`: the run still passes, but accept and delivery p95 both jump meaningfully.
- By `3250`, the system no longer degrades gracefully. It fails to converge into the steady-message stage and eventually times out.

Operational statement: close to linear through about `3000`, noticeable degradation around `3100`, and failure to converge by `3250`.

## Simulated Multi-Node / Network-Delay Comparison

The deployment is already split across separate processes and containers in `ohmf/infra/docker/docker-compose.yml`: gateway API, messages processor, delivery processor, Redis, Kafka, Postgres, and Cassandra are all separate runtime boundaries.

To approximate an extra node-to-node network hop on the client-facing side without rewriting the stack, I added `testing/stress/tcp-delay-proxy.js`, a raw TCP reverse proxy that sits between the stress clients and the gateway. For these comparison runs it injected:

- `15 ms` client-to-gateway delay
- `15 ms` gateway-to-client delay
- effective simulated ingress/realtime RTT of about `30 ms`

This is a representative ingress/realtime hop simulation. It does not simulate added latency between the backend containers themselves.

### Direct vs Delayed Comparison

| Profile | Load | Stage | Direct Duration | Delayed Duration | Direct Accept P95 | Delayed Accept P95 | Direct Delivery P95 | Delayed Delivery P95 | Direct Deliveries | Delayed Deliveries |
| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Worst-case | 1000 | `messages-heavy-a` | 29813 ms | 51952 ms | 283 ms | 316 ms | 281 ms | 292 ms | 800 / 800 | 800 / 800 |
| Standard | 2500 | `messages-steady-a` | 88191 ms | 96337 ms | 124 ms | 126 ms | 90 ms | 101 ms | 239 / 239 | 239 / 239 |

Interpretation:

- The added RTT increases wall-clock stage duration and message latency, as expected.
- The worst-case profile is more sensitive to the added hop. At `1000` clients, stage duration increased by about `74%`, while accept p95 rose by about `12%`.
- The standard profile is less sensitive at `2500` clients. Stage duration increased by about `9%`, accept p95 by about `2%`, and delivery p95 by about `12%`.
- Most importantly, correctness did not regress in either delayed comparison run: no delivery loss, no duplicates, no ordering violations, and no client errors.

This delayed section is a representative comparison, not a new delayed-maximum claim. The max figures at the top of this report remain the direct-to-gateway capacity maxima.

## Bottom Line

- Verified maximum worst-case capacity on this machine and stack: `1300` clients / `975` logical users at `75%` unique-user ratio.
- Verified maximum standard-case capacity on this machine and stack: `3100` clients / `2325` logical users at `75%` unique-user ratio.
- Worst-case failure mechanism at the next step (`1350`) is delivery/receipt convergence collapse, not message persistence failure.
- Standard-case failure mechanism at the next step (`3250`) is connection/bootstrap non-convergence, not message correctness failure.
- Representative multi-node ingress-delay simulation (`30 ms` RTT) increases latency and wall-clock duration but does not break correctness at `1000` worst-case and `2500` standard load.
