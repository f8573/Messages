# OHMF Capacity Maximums

Date: 2026-04-21  
Environment: local Docker stack at `http://127.0.0.1:18080`  
WebSocket mode: `v1`  
Measured code state: `1b8e5b6`  
Client-to-user mix: `75%` unique users (`100` clients -> `75` logical users)

## Test Conditions

These capacity runs were measured with the stress harness after the suite sequencing fix landed. During the runs, the gateway OTP, websocket admission, and send-rate limits were temporarily raised so the measured ceiling reflected application/runtime capacity rather than abuse-limit throttling. The gateway was restored to its default local limits after measurement.

Harness references:

- Worst-case passing suite: `testing/stress/reports/2026-04-21T20-09-43Z-capacity-worst-case-1300-max-search/capacity-suite-summary.json`
- Worst-case next failure: `testing/stress/reports/2026-04-21T19-25-33Z-capacity-worst-case-1350-max-search/capacity-suite-summary.json`
- Standard-case passing suite: `testing/stress/reports/2026-04-21T20-14-11Z-capacity-normal-3100-max-search/capacity-suite-summary.json`
- Standard-case next failure: `testing/stress/reports/2026-04-21T19-27-33Z-capacity-normal-3250-max-search/capacity-suite-summary.json`

## Capacity Summary

### Maximum Worst Case

- Highest verified passing client count: `1300`
- Logical users: `975`
- Active conversations: `250`
- Next tested failure bound: `1350` clients / `1013` logical users
- Failure signature at `1350`: `connect` passed, but `messages-heavy-a` accepted and persisted all `600` messages while completing `0/799` expected deliveries and losing `799` deliveries.

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

Next tested worst-case failure at `1350`:

- `connect`: duration `35243 ms`; connected `1350`; no client errors.
- `messages-heavy-a`: duration `76726 ms`; connected `1350`; requested/accepted/persisted `600/600/600`; queued accepts `0`; deliveries expected/successful/realtime/sync/lost `799/0/0/0/799`; duplicates `0`; unpersisted `0`; unexpected receipts `0`; ordering violations `0`; send failures `0`; client errors `0`; accept latency p95 `553 ms`; delivery latency p95 `null`.

### Maximum Standard Case

- Highest verified passing client count: `3100`
- Logical users: `2325`
- Active conversations: `116`
- Next tested failure bound: `3250` clients / `2438` logical users
- Failure signature at `3250`: `connect` passed, but the first `messages-steady-a` stage never converged and was killed by the suite timeout after `1800000 ms`.

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

Next tested standard-case failure at `3250`:

- `connect`: duration `404943 ms`; connected `3250`; no client errors.
- `messages-steady-a`: no summary artifact was written because the stage was killed by the suite timeout. Top-level stage result: `timed_out=true`, `exit_signal=SIGKILL`, stderr `failed to connect stress-user-2028/stress-user-2028-device-1`.

## Bottom Line

- Verified maximum worst-case capacity on this machine and stack: `1300` clients / `975` logical users at `75%` unique-user ratio.
- Verified maximum standard-case capacity on this machine and stack: `3100` clients / `2325` logical users at `75%` unique-user ratio.
- Current tested failure bounds:
  - worst-case: fails by `1350`
  - standard-case: fails or fails to converge by `3250`
