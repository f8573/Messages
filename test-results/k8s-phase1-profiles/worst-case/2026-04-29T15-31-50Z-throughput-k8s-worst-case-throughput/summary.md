# OHMF Stress Run

- Scenario: `throughput`
- Run label: `k8s-worst-case-throughput`
- Commit: `unknown`
- Base URL: `http://ohmf-gateway.ohmf-app.svc.cluster.local:8080`
- WebSocket mode: `v1`
- Started: 2026-04-29T15:31:50.953Z
- Completed: 2026-04-29T15:34:17.644Z
- Duration: 146691 ms
- Connected devices (final): 1000
- Logical users: 750

## Results

- Messages requested: 600
- Messages accepted: 600
- Queued accepts: 0
- Messages persisted: 600
- Expected deliveries: 800
- Successful deliveries: 632
- Realtime deliveries: 632
- Sync recoveries: 0
- Duplicate receipts: 0
- Lost deliveries: 168
- Unpersisted messages: 0
- Ordering violations: 0
- Send failures: 0
- Client errors: 0

## Latency

- Accept p95: 152 ms
- Delivery p95: 147 ms

## Validation Warnings

- timed out waiting for all expected device receipts after 60000ms

## Missing Receipts

- 3da92cb9-fd88-468c-8244-c62790d1df27 -> 75c3b9e8-56ab-41cf-b4af-48f9ba4f0cf3/93e4a8d8-3d59-4b15-8925-4344cdc441e2
- 8012c574-834d-4d62-a422-ebfe2d5a9da8 -> 89b636f2-9edf-4f40-a02d-7df299c695f4/64311d3f-cd35-4be1-a118-a2498d18b6a3
- 54ffcd31-dd43-4331-b352-fdd24615a3a0 -> 642affa3-98a5-4fad-9fd1-e5023e680366/a0ab9862-5328-43f4-9b59-07dde7ba2f93
- 54ffcd31-dd43-4331-b352-fdd24615a3a0 -> 642affa3-98a5-4fad-9fd1-e5023e680366/757b0f03-70f0-40d9-81e8-4c38060c1a24
- c60ec88b-19e8-4a64-8250-2acd0b76c7c2 -> fc984b70-2213-43b1-a468-e22ef0bedc42/35484eb9-7844-43af-82b0-557000ecf067
- a82cd1b5-5f2a-4fe5-8a80-556e7caa5bc3 -> c0cf6bd9-a3cb-4d89-8ab6-245e0dc0566c/c0cfa756-36f3-4ca2-88f8-e9d72616e49a
- 4288a689-0d6f-49ba-96a2-f0af23d14727 -> 35030122-6d5c-4707-9dae-fcb8d68f375d/17933db0-ea6e-4983-a90a-a7c56cfad9f7
- 12105a3f-f8c3-47cf-bbbd-2ef5b733c3a3 -> dea69eb9-c700-42e9-88de-b9bd33bfdd0e/a61ac6b5-301c-40d3-97fa-4a3ef2ab3bc0
- 4bb8acc6-ff32-4808-9c9c-a31d9a247849 -> e4fe70a4-c2df-4e15-b42b-e6156c2ad1d8/7e3d8932-704f-4e1d-a080-f68d0fd2204a
- 4bb8acc6-ff32-4808-9c9c-a31d9a247849 -> e4fe70a4-c2df-4e15-b42b-e6156c2ad1d8/d7f45f57-60ba-41ab-89be-1515803104e5
- 57e9c102-04e1-4ccf-91ef-bb13f063775d -> b6a9cd32-0048-4d9a-a80b-e657873c2f80/1873f13f-b555-4f59-809c-ff682ec78cd7
- 44ca7429-3552-47f2-8390-4c9b59446f1e -> 3b32d214-fda9-4856-b777-a109f14d305f/14d1a55e-4aa3-4dd9-bfb6-f331f4c60e20
- 91fd3677-bdde-436e-aec2-01af51d178dc -> b0f0907d-11c2-4f6a-9b51-4f50ddacf315/2df02e56-0b2d-4a17-af75-b1f7debc247c
- 91fd3677-bdde-436e-aec2-01af51d178dc -> b0f0907d-11c2-4f6a-9b51-4f50ddacf315/26525004-fb5b-4f32-8df9-4def9abbba06
- 1b298853-c012-48d4-8186-e2607a901a7e -> 40d20e2e-eb35-449f-bd0a-75cb6f1317fe/b42b69a0-00ba-4886-82dd-2327f1a647fb
- 50637208-2c35-478d-ba21-d1c2995a18e5 -> 1acc22d3-9a47-47ff-9d09-98b5efab0ecc/398ac4a1-c4cd-45bc-a033-5977f696efb3
- 36c1c939-f15e-4ca4-a1a5-242b9ef65273 -> 48e6534d-aa91-43ea-97af-0b5f9c6ac93f/e1595010-2148-4e9c-b7a8-fa90bb6a9ee7
- 36c1c939-f15e-4ca4-a1a5-242b9ef65273 -> 48e6534d-aa91-43ea-97af-0b5f9c6ac93f/28fa7378-5f14-4e46-bb7b-16cae37615a6
- 81cf41c7-1451-4bbc-9fbf-718c803a48d4 -> 7956a093-592a-4a2f-a450-7dff0fdfc9b7/e3688ad4-23f1-45bc-9952-5bdb50208ef8
- 6664a55d-ad86-4147-a088-8600444d3425 -> 3f583e3f-1cc5-41f0-b727-36d5bf6c0e75/13a6a6f5-283f-49bc-b6a8-8de968466cf9
