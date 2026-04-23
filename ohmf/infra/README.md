# 17 — Infrastructure Overview

Mapping: OHMF spec section 17 (Infrastructure & Deployment)

Purpose
- Document infra components, environment setups, and deployment patterns for local dev and production.

Expected behavior
- Provide reproducible Docker Compose for local dev.
- Provide manifests for production (Kubernetes/Helm, Terraform references).

Key elements
- Local compose: Postgres, Redis, Kafka (or nats), gateway, auth, users, messages.
- Secrets management: Vault or KMS recommended.
- Networking: mTLS for service-to-service.

Implementation constraints
- IaC must be idempotent and tested in CI.

Security considerations
- Use least privileged IAM roles; rotate secrets.

Observability and operational notes
- Centralized logs (ELK/Opensearch), metrics (Prometheus), traces (OTel collector).
- Local Prometheus and Grafana assets now live under `infra/observability`.

Testing requirements
- Infrastructure integration tests (smoke tests) in CI.

References
- infra/docker for compose, infra/docker/README.md for examples, infra/observability for metrics and dashboards.
- infra/k8s for the first-pass distributed performance and chaos-test layout.
