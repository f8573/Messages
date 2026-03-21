# P1.3 Release Suspension / Kill Switch - Implementation Analysis

## STEP 1: UNDERSTAND

### What This Solves
- **Kill switch for malicious/broken apps**: Admins can suspend compromised or non-compliant releases without requiring publisher intervention
- **Revocation control**: Publishers can revoke their own releases; admins can suspend for policy violations
- **Fast propagation**: Real-time invalidation via Redis pubsub (target <5s latency)
- **Graceful session termination**: Active sessions are notified and closed cleanly, not crashed
- **Compliance**: Audit trail for all suspension actions and enforcement metrics

### Risk Without It
- Suspended apps still runnable (creates security/compliance gaps)
- Slow propagation (would require app restart or polling)
- Abrupt session crashes (poor UX for active sessions)
- No metrics for enforcement verification

## STEP 2: SCOPE

### Apps Service Controls
- Add `suspended_at` timestamp to release model
- Add `suspension_reason` field for audit trail
- Implement `/v1/admin/apps/{app_id}/releases/{version}/suspend` endpoint
- Publish invalidation signal to Redis pubsub on suspension/revocation
- Log all suspension actions to audit table

### Gateway Controls
- Subscribe to release invalidation topic on startup
- Check release status (suspended/revoked) during CreateSession
- Block new sessions with HTTP 403 (Forbidden) + error code "release_suspended"
- Periodically query active sessions to check for suspension
- Gracefully terminate active sessions via event notification

### Communication
- Redis pubsub topic: `miniapp:release:invalidation`
- Message format: JSON with event_type, app_id, version, timestamp
- Fallback: Periodic polling (30 seconds) if Redis down
- Audit logging in both apps and gateway services

## STEP 3: IMPLEMENTATION PLAN (Ordered Steps)

1. **Database Schema** - Add suspension tracking to releases
   - Add columns: `suspended_at`, `suspension_reason` to miniapp_registry_releases
   - Add tables: `miniapp_release_suspension_log`, `miniapp_cache_invalidation_events`
   - Add indexes for fast lookups

2. **Apps Service Model Updates**
   - Update `appRelease` struct to include SuspendedAt, SuspensionReason
   - Update registry.go load/save logic to handle new columns
   - Update transitionRelease handler to set SuspendedAt when statusSuspended

3. **Redis Pubsub Mechanism**
   - Create publishReleaseInvalidation() function in apps service
   - Publish on suspension/revocation in transitionRelease
   - Include app_id, version, suspension_reason, actor_user_id, timestamp

4. **Gateway Redis Subscription**
   - Create CacheInvalidationListener in miniapp service
   - Subscribe to `miniapp:release:invalidation` on service init
   - Implement message handler to clear manifest cache and mark sessions as needing check

5. **Session Creation Status Check**
   - In CreateSession: Check if release is suspended/revoked
   - Query apps service via RegistryClient for release status
   - Return 403 if suspended/revoked with error_code="release_suspended"
   - Log to audit trail with enforcement_timestamp

6. **Active Session Graceful Termination**
   - Query active sessions when invalidation event received
   - Send synthetic "release.suspended" event to each active session
   - Close session after notification (set ended_at = now())
   - Track affected session count in cache_invalidation_events table

7. **Audit Logging & Metrics**
   - Log suspension action in apps service audit table
   - Record cache invalidation events with propagation latency
   - Measure: suspension_time → gateway_notification_time → enforcement_time
   - Export metrics for compliance/monitoring

## STEP 4: IMPLEMENTATION

### Database Migrations
✅ Created: 000004_release_suspension.up.sql
   - Added suspended_at, suspension_reason to miniapp_registry_releases
   - Created miniapp_release_suspension_log table
   - Created miniapp_cache_invalidation_events table

### Apps Service Changes
✅ Updated: registry.go struct and DB queries
   - Added SuspendedAt, SuspensionReason to appRelease struct
   - Updated loadStateFromQuerier to read new columns
   - Updated saveStateToTx to save new columns

✅ Updated: handlers.go transitionRelease
   - Updated statusSuspended case to set SuspendedAt and SuspensionReason

⏳ Pending: Redis pubsub publish logic

### Gateway Service Changes
⏳ Pending: CacheInvalidationListener
⏳ Pending: Release status check in CreateSession
⏳ Pending: Graceful session termination logic

## STEP 5: VALIDATION

### Test Scenarios
1. **Suspend release → new sessions blocked**
   - POST /v1/admin/apps/{app_id}/releases/{version}/suspend
   - Create new session with same app_id
   - Verify: HTTP 403 with error_code="release_suspended"

2. **Active session → gracefully notified and ended**
   - Create session with app_id
   - Suspend release
   - Verify: Session receives "release.suspended" event
   - Verify: Session marked as ended_at = now()

3. **Reactivate release → new sessions allowed**
   - POST /v1/admin/apps/{app_id}/releases/{version}/approve
   - Create new session with same app_id
   - Verify: HTTP 201 (success)

4. **Latency measurement**
   - Suspend release at T0
   - Gateway receives pubsub message at T1
   - Enforcement begins at T2
   - Log: propagation_latency_ms = T2 - T0 (target <5000ms)

## STEP 6: STRESS/FAILURE SCENARIOS

### Redis Down
- Fallback to periodic polling (30s interval)
- Log warning but continue (non-fatal)
- Latency degrades to ~30s instead of <5s
- Resume normal operation when Redis reconnects

### Mass Suspension
- Multiple releases suspended simultaneously
- Gateway processes invalidation events serially
- Queue all affected sessions for notification
- Distribute notifications over 1-2 second window (avoid spike)
- Track queue depth and process latency

### Active Sessions with Bad Network
- Retry graceful close event 3 times (5s timeout each)
- Timeout and forcibly end session after 15s total
- Log: forcibly_ended_reason = "timeout"

### Deployment Rollout
- Old gateway without suspension logic receives message
- Ignores unknown event_type (graceful degradation)
- New gateway processes suspension normally
- No breaking changes between versions

## STEP 7: CHECKLIST UPDATE

- [x] Add database migration for suspension tracking
- [x] Update apps service model (appRelease struct)
- [x] Update database load/save logic
- [x] Update transitionRelease handler
- [ ] Implement Redis pubsub publish in apps service
- [ ] Implement Redis pubsub subscription in gateway
- [ ] Add status check in CreateSession handler
- [ ] Implement graceful session termination
- [ ] Add audit logging for suspension events
- [ ] Add metrics collection and export
- [ ] Integration tests for suspension flow
- [ ] E2E tests for cache invalidation latency
