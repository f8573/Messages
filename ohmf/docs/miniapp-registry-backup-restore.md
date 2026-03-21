# Mini-App Registry Backup And Restore

Date: 2026-03-21

This document covers the current backup and restore procedure for the mini-app registry service in `ohmf/services/apps`.

## Scope

The PostgreSQL-backed registry persists its control-plane state in:

- `miniapp_registry_apps`
- `miniapp_registry_releases`
- `miniapp_registry_installs`
- `miniapp_registry_publisher_keys`
- `miniapp_registry_review_audit_log`
- `miniapp_registry_schema_migrations`

If the registry is running in fallback file mode, the relevant artifact is the JSON state file configured by `DATA_FILE`.

## Backup

For the Docker-based local stack, use a logical Postgres dump of the registry tables:

```powershell
docker exec ohmf-db pg_dump -U ohmf -d ohmf `
  --data-only `
  --column-inserts `
  --table=miniapp_registry_apps `
  --table=miniapp_registry_releases `
  --table=miniapp_registry_installs `
  --table=miniapp_registry_publisher_keys `
  --table=miniapp_registry_review_audit_log `
  --table=miniapp_registry_schema_migrations `
  > miniapp-registry-backup.sql
```

Recommended operational practice:

- run backups after schema migrations have completed successfully
- keep the SQL dump with a timestamped filename
- store the dump outside the container host
- back up the main Postgres cluster configuration separately if this is not a disposable environment

For fallback file mode:

```powershell
Copy-Item .\ohmf\services\apps\data\registry.json .\miniapp-registry-backup.json
```

## Restore

1. Start the Postgres instance.
2. Start the registry once with migrations enabled so the target tables exist.
3. Stop the registry writer if you need a clean restore window.
4. Restore the SQL dump:

```powershell
Get-Content .\miniapp-registry-backup.sql | docker exec -i ohmf-db psql -U ohmf -d ohmf
```

5. Restart the registry service.

For file mode:

```powershell
Copy-Item .\miniapp-registry-backup.json .\ohmf\services\apps\data\registry.json -Force
```

## Verification

After restore, verify:

- `GET /healthz` on the registry returns `200`
- `GET /v1/apps` returns expected catalog entries
- `GET /v1/apps/installed` for a known test user returns expected installs
- release review states match the expected pre-backup values

Optional SQL spot checks:

```sql
SELECT count(*) FROM miniapp_registry_apps;
SELECT count(*) FROM miniapp_registry_releases;
SELECT count(*) FROM miniapp_registry_installs;
SELECT count(*) FROM miniapp_registry_review_audit_log;
```

## Notes

- The current implementation stores registry state in normalized tables but still serializes the in-memory state model during writes.
- Asset binaries are not part of this backup because durable registry asset storage is not implemented yet.
- Session/runtime state lives in the gateway, not in the registry backup scope described here.
