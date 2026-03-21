# OHMF Phase 2 Implementation Roadmap

## Overview

This document consolidates all deferred and blocked items from Phase 1 implementation. The OHMF mini-app platform completed 28/29 checklist items in Phase 1 (96.6% complete). This roadmap details the remaining 20+ implementation tasks organized by dependency layers, infrastructure requirements, and implementation complexity.

**Status**: Phase 1 Complete (2026-03-21)
**Next**: Phase 2 Planning & Resource Allocation
**Total Phase 2 Items**: ~22 tasks across 5 categories

---

## Priority Category: P4.3 Realtime Fanout

**Current Status**: BLOCKED (awaiting architecture decision)
**Impact**: High - enables true real-time mini-app session updates
**Blocker**: Infrastructure choice (WebSocket vs SSE vs polling)

### P4.3.1 - Core Realtime Infrastructure Setup
- **Effort**: Medium (3-5 days)
- **Dependencies**: None (but decision needed first)
- **Reason Blocking**: Requires choosing between:
  - **Option A**: WebSocket streaming (full duplex, lower latency)
  - **Option B**: Server-Sent Events (simpler, works over HTTP)
  - **Option C**: Keep polling endpoint (current workaround)
- **Implementation Tasks**:
  - [ ] Architecture review and decision (stakeholder sign-off)
  - [ ] Redis pub/sub channel setup for session events
  - [ ] Event handler registration in websocket/SSE layer
  - [ ] Client SDK subscription logic implementation
  - [ ] Connection heartbeat and reconnection handling

**Acceptance Criteria**:
- Session events delivered to client within 100ms of creation
- Auto-reconnection on network failure
- No missing events during reconnect window
- Load tested with 100+ concurrent sessions

### P4.3.2 - Polling Endpoint (Interim Solution)
- **Effort**: Small (1 day)
- **Dependencies**: None
- **Status**: Handler exists but NOT wired in router
- **Implementation Tasks**:
  - [ ] Register `GetSessionEvents` handler in `/cmd/api/main.go`
  - [ ] Add endpoint: `GET /v1/apps/sessions/{id}/events`
  - [ ] Add endpoint: `GET /v1/miniapps/sessions/{id}/events`
  - [ ] Add query parameters: `event_type`, `limit`, `offset`, `since_seq`
  - [ ] Add paging cursor support for long-term polling

**Acceptance Criteria**:
- Clients can poll events with backoff strategy
- Supports cursor-based pagination
- Returns: event_seq, event_type, actor_id, body, created_at

### P4.3.3 - Redis Event Distribution
- **Effort**: Small (1 day)
- **Dependencies**: P4.3.1 (architecture decision)
- **Implementation Tasks**:
  - [ ] Add Redis pub/sub in `AppendEvent()` (miniapp/service.go)
  - [ ] Channel naming: `miniapp:session:{sessionID}:events`
  - [ ] Publish full event JSON to channel
  - [ ] Add event deduplication key in Redis

**Acceptance Criteria**:
- Events published within 10ms of database write
- No duplicate events to subscribers
- Redis failover doesn't lose in-flight events

### P4.3.4 - WebSocket/SSE Subscription Handler
- **Effort**: Medium (2 days)
- **Dependencies**: P4.3.3
- **Implementation Tasks**:
  - [ ] Update WS handler in realtime/ws.go to subscribe to session channels
  - [ ] Add session event frame type to v1 and v2 protocols
  - [ ] Implement keep-alive heartbeat for long connections
  - [ ] Add connection timeout and graceful disconnect
  - [ ] Event delivery confirmation tracking

**Acceptance Criteria**:
- Client receives events via WebSocket
- Connection stays alive during idle periods
- Graceful cleanup on disconnect

### P4.3.5 - Client SDK Updates
- **Effort**: Medium (2 days)
- **Dependencies**: P4.3.1 (architecture decision)
- **Implementation Tasks**:
  - [ ] Update miniapp-sdk.js to subscribe to session events
  - [ ] Implement backoff strategy for reconnection
  - [ ] Add local event cache with deduplication
  - [ ] Implement sequence number tracking
  - [ ] Add event listener pattern for app code

**Acceptance Criteria**:
- Apps receive session events in real-time
- Handles network disconnection gracefully
- No memory leaks from unclosed subscriptions

### P4.3.6 - Testing & Observability
- **Effort**: Medium (2-3 days)
- **Dependencies**: All P4.3.* subtasks
- **Implementation Tasks**:
  - [ ] Integration tests for event delivery latency
  - [ ] Load tests: 100+ concurrent session event streams
  - [ ] Failure injection: network partition recovery
  - [ ] Metrics: event delivery latency (p50, p95, p99)
  - [ ] Metrics: event queue depth and Redis memory
  - [ ] Log sampling for event delivery traces

**Acceptance Criteria**:
- All scenarios tested and passing
- Latency <100ms p95
- Zero event loss in failure scenarios

---

## Category A: Infrastructure Dependencies

These items require external infrastructure provisioning (AWS, GCS, DNS) before implementation can begin.

### A1 - P2.2 CDN/Object Storage Phase 2
- **Effort**: Large (5-7 days)
- **Blocked By**: AWS/GCS account provisioning, DNS wildcard setup
- **Phase 1 Status**: Architecture documented, no code changes needed
- **Implementation Tasks**:
  - [ ] Provision S3 buckets (dev, staging, prod) or GCS equivalent
  - [ ] Configure CloudFront/CDN origins for miniapp storage
  - [ ] Set up wildcard DNS: `*.miniapp.cdn.example.com`
  - [ ] Configure CORS on S3/CloudFront
  - [ ] Set up access keys and IAM policies
  - [ ] Implement signed URL generation for uploads
  - [ ] Add CDN URL construction in handler.go
  - [ ] Test cross-origin asset loading with CSP headers

**Dependencies**: None (but unblocks P3.4, P3.5)

**Acceptance Criteria**:
- Mini-app assets served from CDN
- Isolation between dev/staging/prod storage
- CORS headers allow iframe loading
- Signed URLs work for user uploads
- Performance: <100ms to CDN edge

---

### A2 - P3.4 CORS Strategy Phase 2 (CDN/S3 CORS)
- **Effort**: Small (1-2 days)
- **Blocked By**: P2.2 (CDN infrastructure)
- **Phase 1 Status**: Bearer token auth validated, gateway CORS working
- **Implementation Tasks**:
  - [ ] Configure S3 CORS policy for asset domain
  - [ ] Configure CloudFront cache behavior for CORS headers
  - [ ] Test preflight requests from iframe to CDN
  - [ ] Verify CORS headers propagate correctly
  - [ ] Add signed URL system for authenticated S3 access

**Acceptance Criteria**:
- Iframes can load images from CDN
- Preflight requests return correct headers
- No blocked CORS requests in browser console

---

### A3 - P3.5 Edge Case Fixes Phase 2 (Image Proxy & Analytics)
- **Effort**: Medium (3-4 days)
- **Blocked By**: P2.2 (CDN infrastructure for image proxy)
- **Phase 1 Status**: Constraints documented, CDN blocked by design
- **Implementation Tasks**:
  - [ ] Image proxy endpoint: `GET /v1/miniapps/proxy/images`
  - [ ] Validate image MIME types (whitelist)
  - [ ] Strip EXIF metadata from proxied images
  - [ ] Cache proxied images (Redis + disk)
  - [ ] Implement redirect validation (prevent open redirect)
  - [ ] Add analytics bridge method: `host.reportAnalyticsEvent()`
  - [ ] Validate analytics events against manifest permissions
  - [ ] Send analytics events to tracking endpoint

**Dependencies**: P2.2 (infrastructure) and P1.2 (capability enforcement)

**Acceptance Criteria**:
- External images load through proxy with proper CORS
- EXIF metadata stripped from user images
- Analytics events validated by capability system
- No 302 redirect loops in proxy

---

## Category B: UI Implementation

### B1 - P0.4 Permission Expansion & Re-Consent UI (Phase 2)
- **Effort**: Medium (3-4 days per platform)
- **Blocked By**: UI implementation required (not backend)
- **Phase 1 Status**: Backend validation complete, requires_reconsent field implemented
- **Implementation Tasks** (Web):
  - [ ] Create re-consent modal component
  - [ ] Display required vs optional permissions
  - [ ] Permission descriptions with examples
  - [ ] User approval flow
  - [ ] Rejection handling
  - [ ] Persist consent audit trail

**Implementation Tasks** (Android):
  - [ ] Update WebView host to show re-consent UI
  - [ ] Display permission changes clearly
  - [ ] Implement user approval flow
  - [ ] Update mini-app manifest permissions on consent

**Dependencies**: Backend permission expansion (P0.4 Phase 1) ✅

**Acceptance Criteria**:
- Users see clear re-consent UI when app requests expanded permissions
- Can approve or deny individual permissions
- App updates proceed only on user approval
- Audit trail captures consent decision

---

## Category C: Android Implementation

### C1 - P5.1 Android Backend Integration
- **Effort**: Large (5-7 days)
- **Blocked By**: Separate Android project setup required
- **Item ID**: P5.1
- **Status**: Android host shell scaffold started, WebView integration pending
- **Implementation Tasks**:
  - [ ] Set up Android project toolchain (Kotlin, Gradle, AGP)
  - [ ] Integrate Android mini-app host shell (already scaffolded)
  - [ ] Implement WebView bridge to gateway APIs
  - [ ] Add session creation and management
  - [ ] Handle postMessage bridge protocol
  - [ ] Implement app snapshot/state persistence
  - [ ] Add device relay integration

**Acceptance Criteria**:
- Mini-apps load and render in Android WebView
- Bridge method calls work (getState, setState, etc.)
- Sessions persist across app restarts
- Device relay methods functional

---

### C2 - P5.2 Android Security Validation
- **Effort**: Large (5-7 days)
- **Blocked By**: P5.1 (WebView implementation)
- **Status**: Architecture documented, not yet tested on device
- **Implementation Tasks**:
  - [ ] Validate origin isolation in Android WebView
  - [ ] Verify CSP headers enforced by WebView
  - [ ] Test iframe sandbox attributes
  - [ ] Validate app storage isolation
  - [ ] Test Bearer token auth correctness
  - [ ] Audit WebView permission grants
  - [ ] Test device attestation integration

**Acceptance Criteria**:
- All security controls verified on Android devices
- Origin isolation tested in isolation testing framework
- CSP blocking tested for non-compliant resource loads

---

### C3 - P5.3 Android CI/CD & Testing
- **Effort**: Large (4-5 days)
- **Blocked By**: P5.1 and P5.2
- **Implementation Tasks**:
  - [ ] Set up Android CI/CD pipeline (GitHub Actions or Bitrise)
  - [ ] Configure automated APK builds
  - [ ] Add automated UI testing (Espresso)
  - [ ] Add device farm testing (Firebase Test Lab or Browserstack)
  - [ ] Set up performance profiling
  - [ ] Configure code signing and release builds
  - [ ] Document release procedure

**Acceptance Criteria**:
- CI/CD pipeline automatically builds and tests each commit
- Automated tests pass on multiple Android versions
- Performance baselines established

---

## Category D: Testing & Validation Infrastructure

### D1 - P6 Stress Testing Framework
- **Effort**: Large (7-10 days)
- **Blocked By**: Test infrastructure provisioning
- **Status**: No tests yet; requires dedicated environment

#### D1a - P6.1 Load Testing
- **Implementation Tasks**:
  - [ ] Set up load testing infrastructure (k6, Locust, or JMeter)
  - [ ] Create test scenarios (100, 500, 1000 concurrent users)
  - [ ] Message throughput test (messages/sec)
  - [ ] Session creation rate test
  - [ ] Track latency percentiles (p50, p95, p99, p99.9)

**Acceptance Criteria**:
- Gateway handles 1000 concurrent users
- Message send latency <500ms p95
- Session creation <200ms p95

#### D1b - P6.2 Soak Testing
- **Implementation Tasks**:
  - [ ] Run sustained 100 concurrent users for 24 hours
  - [ ] Monitor memory/connection leaks
  - [ ] Track GC pause times
  - [ ] Verify stability metrics
  - [ ] Database connection pool health

**Acceptance Criteria**:
- Zero memory leaks after 24 hour run
- Consistent latency over time
- No hanging connections

#### D1c - P6.3 Failure Injection
- **Implementation Tasks**:
  - [ ] Database connection failure scenarios
  - [ ] Redis unavailability handling
  - [ ] Network partition simulation
  - [ ] Rate limit trigger testing
  - [ ] Message delivery retry verification

**Acceptance Criteria**:
- Graceful degradation on service failure
- Automatic recovery when service restored
- No data loss in failure scenarios

#### D1d - P6.4 Performance Profiling
- **Implementation Tasks**:
  - [ ] CPU profiling (go pprof)
  - [ ] Memory profiling and heap analysis
  - [ ] Goroutine leak detection
  - [ ] Database query analysis (slow query log)
  - [ ] Identify and document hot paths

**Acceptance Criteria**:
- CPU usage baseline <20% at 100 users
- Memory usage stable over time
- No unbounded goroutine growth

#### D1e - P6.5 Chaos Engineering
- **Implementation Tasks**:
  - [ ] Random failure injection
  - [ ] Service restart during load
  - [ ] Network latency injection
  - [ ] Packet loss simulation
  - [ ] Clock skew testing (NTP issues)

**Acceptance Criteria**:
- System recovers from any single failure
- No cascading failures
- Message delivery guarantees maintained

---

### D2 - P7 Developer Experience & Tooling
- **Effort**: Medium (3-4 days)
- **Blocked By**: None (low priority for production)
- **Status**: No tooling yet

**Implementation Tasks**:
  - [ ] Local mini-app emulator (mock host + SDK)
  - [ ] Hot reload for mini-app development
  - [ ] CI/CD pipeline setup (GitHub Actions)
  - [ ] Local test fixtures and mock data
  - [ ] SDK documentation and API reference
  - [ ] Developer onboarding guide
  - [ ] Integration test harness

**Acceptance Criteria**:
- Developers can iterate on mini-apps locally
- Tests run on each commit
- Releases automated with version bumping
- New team members can onboard in <1 hour

---

### D3 - P8 Architecture Documentation & Specification
- **Effort**: Medium (2-3 days)
- **Blocked By**: Completion of all other phases
- **Status**: Partial (see OHMF_COMPREHENSIVE_SPECIFICATION.md)

**Implementation Tasks**:
  - [ ] Update architecture docs after Phase 2 completion
  - [ ] Document architectural invariants and guarantees
  - [ ] Create system design diagrams
  - [ ] Document all deployment topologies
  - [ ] Write operational runbooks
  - [ ] Create incident response guides
  - [ ] Document scaling strategies

**Acceptance Criteria**:
- New engineers understand system in <4 hours
- All operational procedures documented
- Clear escalation and troubleshooting guides

---

## Dependency Graph

```
P4.3 Realtime Fanout (BLOCKED - needs decision)
├── P4.3.1 Architecture Decision (BLOCKER)
├── P4.3.2 Polling Endpoint (can proceed independently)
├── P4.3.3 Redis Distribution (needs decision)
├── P4.3.4 WebSocket/SSE Handler (needs decision)
└── P4.3.5 Client SDK Updates (needs decision)

A1 CDN/Object Storage (BLOCKED - infrastructure)
├── A2 CORS Strategy Phase 2
└── A3 Image Proxy & Analytics

B1 Re-Consent UI (independent)

C1 Android Backend Integration (independent start)
├── C2 Android Security Validation
└── C3 Android CI/CD

D1 Stress Testing (independent, low priority)
D2 Developer Tooling (independent, low priority)
D3 Architecture Docs (dependent on all above)
```

---

## Resource Allocation Recommendation

### Immediately Available (Can start now):
- [ ] P4.3.2 Polling Endpoint (1 day - frontend uses today)
- [ ] B1 Re-Consent UI (3-4 days - improves UX)
- [ ] D2 Developer Tooling (3-4 days - improves velocity)

### Pending Decision (Blocked on architecture choice):
- [ ] P4.3.1-5 Realtime infrastructure (needs WebSocket vs SSE decision)

### Pending Infrastructure:
- [ ] A1 CDN/Object Storage (needs AWS/GCS provisioning)
- [ ] A2-A3 CORS & Image Proxy (blocked on CDN)

### Separate Track (Requires separate team):
- [ ] C1-C3 Android Implementation (3 weeks, separate project/environment)

### Lowest Priority:
- [ ] D1 Stress Testing (6+ days)
- [ ] D3 Architecture Docs (2-3 days, do last)

---

## Success Criteria for Phase 2

✅ **P4.3 Realtime Fanout**: Session events delivered real-time with <100ms latency
✅ **A1 CDN Infrastructure**: Assets served from CDN with isolation
✅ **B1 Re-Consent UI**: Users can re-consent to expanded permissions
✅ **C1-C3 Android**: Mini-apps functional on Android
✅ **D1 Stress Testing**: System validated at 1000 concurrent users
✅ **D2 Developer Tools**: Local development faster than current

---

## Notes for Phase 2 Planning

1. **Prioritize Architecture Decision for P4.3** - This is the only blocker preventing realtime work from proceeding
2. **Parallelize Infrastructure Work** - CDN provisioning can happen while development continues
3. **Consider MVP Android** - Simple WebView integration first, security/testing afterward
4. **Start Developer Tooling Early** - Improves velocity for all subsequent work
5. **Defer Stress Testing** - Do only after Phase 2 features stabilize

---

**Document Status**: Created 2026-03-21
**Last Updated**: 2026-03-21
**Next Review**: Before Phase 2 planning session
