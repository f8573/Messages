# Real-Time Session Event Integration Tests

This document describes the real-time integration tests for mini-app session event delivery via WebSocket.

## Overview

The real-time tests (`miniapp_realtime_test.go`) verify that session events are delivered to WebSocket clients in real-time with low latency and consistency guarantees.

## Test Coverage

### Test Scenarios

#### 1. Single Client Real-Time Delivery (`TestSingleClientRealtimeEventDelivery`)

**Purpose**: Verify that a single WebSocket client receives real-time events when subscribed to a session.

**Flow**:
1. Create a mini-app session
2. Establish WebSocket v2 connection with authentication
3. Subscribe to session using `subscribe_session` message
4. Trigger event via `AppendEvent`
5. Assert event received on WebSocket within 100ms

**Validation**:
- Event received within latency window (100ms)
- Event sequence number matches database
- Event payload contains correct type, actor_id, and body

**Key Details**:
- Uses miniredis for Redis pub/sub testing
- Tests real-time fanout via Redis channel: `miniapp:session:{sessionID}:events`
- Verifies event_seq ordering

#### 2. Multiple Clients Receiving Same Event (`TestMultipleClientsReceiveEvent`)

**Purpose**: Verify that multiple clients subscribed to the same session receive identical event payloads.

**Flow**:
1. Create session with two participants
2. Establish two WebSocket connections for both users
3. Both clients subscribe to the same session
4. Trigger event from client 1
5. Assert both clients receive identical event_seq and payload

**Validation**:
- Both clients receive the event
- event_seq is identical for both clients
- Event payloads are identical
- No event loss or duplication

**Key Details**:
- Tests fanout to multiple subscribers
- Verifies Redis pub/sub delivers to all subscribers
- Validates event_seq consistency across clients

#### 3. Reconnect with Cursor-Based Resume (`TestReconnectWithCursorResume`)

**Purpose**: Verify that clients can reconnect and retrieve missed events using polling with cursor-based pagination.

**Flow**:
1. Create session and append N events
2. Use `GetSessionEvents` with `since_seq` cursor to retrieve missed events
3. Verify all events returned in order with correct sequence numbers

**Validation**:
- Events retrieved in correct order (event_seq ascending)
- Cursor pagination works correctly
- No event loss during disconnection period

**Key Details**:
- Tests polling fallback when WebSocket disconnects
- Cursor is based on event_seq (not offset)
- Useful for bridging the gap between disconnection and reconnection

#### 4. Unsubscribe Stops Event Delivery (`TestUnsubscribeStopsEventDelivery`)

**Purpose**: Verify that closing the WebSocket connection stops event delivery.

**Flow**:
1. Subscribe to session and receive one event
2. Close WebSocket connection
3. Trigger another event
4. Verify no event is received (connection closed)

**Validation**:
- Connection cleanup is automatic
- No events leak to closed connections
- Subscription state is properly cleaned up

**Key Details**:
- Tests subscription cleanup on disconnect
- Simulates network failure or client closing browser
- Validates memory cleanup (no subscription leaks)

#### 5. Subscription Persistence Across State Updates (`TestSubscriptionPersistencyAcrossStateUpdates`)

**Purpose**: Verify that subscriptions remain active and all events arrive in order even when state changes occur.

**Flow**:
1. Subscribe to session
2. Append event 1
3. Call `SnapshotSession` (async state update)
4. Append event 2 and 3
5. Collect all events and verify order

**Validation**:
- Subscription active throughout state changes
- All events received in correct order (event_seq)
- No events lost during concurrent operations
- event_seq increments strictly

**Key Details**:
- Tests concurrent access patterns
- Snapshot doesn't disrupt subscription
- Events can include both StorageUpdated and SnapshotWritten events

## Running the Tests

### Prerequisites

1. **PostgreSQL Database**: Set `TEST_DATABASE_URL` environment variable
   ```bash
   export TEST_DATABASE_URL="postgres://user:pass@localhost/test_gateway"
   ```

2. **Go 1.21+**: Tests use standard Go testing framework

3. **Dependencies**:
   - miniredis (in-memory Redis for testing)
   - gorilla/websocket
   - stretchr/testify
   - pgx (PostgreSQL driver)

### Running All Real-Time Tests

```bash
cd services/gateway
export TEST_DATABASE_URL="postgres://localhost/test_gateway"
go test -v ./internal/miniapp -run TestRealtime
```

### Running Individual Tests

```bash
# Single client delivery
go test -v ./internal/miniapp -run TestSingleClientRealtimeEventDelivery

# Multiple clients
go test -v ./internal/miniapp -run TestMultipleClientsReceiveEvent

# Cursor-based resume
go test -v ./internal/miniapp -run TestReconnectWithCursorResume

# Unsubscribe
go test -v ./internal/miniapp -run TestUnsubscribeStopsEventDelivery

# Persistence across state updates
go test -v ./internal/miniapp -run TestSubscriptionPersistencyAcrossStateUpdates
```

### Running with Race Detector

```bash
go test -race -v ./internal/miniapp -run TestRealtime
```

This helps detect concurrency issues like race conditions in the subscription cleanup code.

### Running in Short Mode (Skip Integration Tests)

```bash
go test -short -v ./internal/miniapp
```

Skips all integration tests that require `TEST_DATABASE_URL`.

## Test Harness Implementation

### Helper Functions

#### `setupTestGateway`

Creates a test HTTP server with the WebSocket handler:
- Initializes token service
- Creates realtime handler with miniapp service
- Returns HTTP test server and WebSocket URL

#### `createWSConnectionV2`

Establishes an authenticated WebSocket v2 connection:
- Generates test access token for user
- Connects to WebSocket with Bearer token in Authorization header
- Returns active WebSocket connection

#### `subscribeSession`

Sends a `subscribe_session` message:
```json
{
  "event": "subscribe_session",
  "data": { "session_id": "session-uuid" }
}
```

#### `waitForMessage`

Blocks until a WebSocket message is received or timeout:
- Used to wait for subscription acknowledgment
- Used to wait for session_event messages
- Returns parsed envelope with event name and data

#### `waitForMessageNonBlocking`

Attempts to read a message with timeout without blocking:
- Used to collect multiple events
- Returns nil if no message received
- Useful for verifying events arrive within window

#### `createTestSession`

Helper to create a session:
- Registers manifest
- Creates session with specified app_id and user
- Returns session_id

## Message Formats

### Subscribe Session Request

```json
{
  "event": "subscribe_session",
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### Subscribe Session Acknowledgment

```json
{
  "event": "subscribe_session_ack",
  "data": {
    "status": "ok",
    "session_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### Session Event Message

```json
{
  "event": "session_event",
  "data": {
    "event_seq": 1,
    "event_type": "storage_updated",
    "actor_id": "user-uuid",
    "body": { "key": "value" },
    "created_at": "2026-03-21T12:34:56Z"
  }
}
```

## Performance Expectations

### Latency

- **P95 (95th percentile)**: < 100ms from AppendEvent to WebSocket delivery
- **P99 (99th percentile)**: < 500ms
- **Typical**: 10-50ms with local Redis

### Throughput

- Single session: 1000+ events/sec per client
- Multiple subscribers: Linear scaling (each subscriber independent)

### Resource Usage

- Memory per connection: ~1-2 KB baseline
- Memory per subscription: ~500 bytes
- No memory leaks on disconnect (verified with test cleanup)

## Debugging Failed Tests

### WebSocket Connection Fails

**Issue**: `websocket: failed to read handshake response`

**Solution**:
1. Verify `setupTestGateway` is returning valid URL
2. Check token service is initialized correctly
3. Verify Bearer token is valid

**Debug**:
```bash
go test -v ./internal/miniapp -run TestSingleClientRealtimeEventDelivery -v -count=1
```

### Events Not Received

**Issue**: `timed out waiting for websocket message`

**Solution**:
1. Verify Redis connection works (miniredis should start)
2. Check `AppendEvent` returns successfully
3. Verify session subscription was acknowledged

**Debug**:
```go
// Add logging in your test
t.Logf("AppendEvent returned seq=%d, err=%v", seq, err)
t.Logf("Subscription ack: %v", ack)
```

### Event Sequence Mismatch

**Issue**: `expected event_seq to be X, got Y`

**Solution**:
1. Verify no events are dropped in Redis pub/sub
2. Check database for all appended events
3. Verify event_seq is strictly incrementing

**Debug**:
```bash
# Query database for events
SELECT event_seq, event_type, created_at FROM miniapp_events
WHERE app_session_id = 'session-id'
ORDER BY event_seq;
```

### Multiple Clients Receive Different Events

**Issue**: Clients have different event_seq values for same event

**Solution**:
1. Verify both clients subscribe before event is published
2. Check Redis pub/sub is connected before AppendEvent
3. Verify no filtering or rate limiting is applied

## Extending Tests

### Adding New Event Types

1. Add event type constant to `service.go`:
   ```go
   const EventTypeCustom = "custom_event"
   ```

2. Create test for the event:
   ```go
   func TestCustomEventDelivery(t *testing.T) {
       // Follow the pattern of existing tests
       seq, err := miniappSvc.AppendEvent(ctx, sessionID, userID, EventTypeCustom, "method", map[string]any{})
       // Assert event received on WebSocket
   }
   ```

### Adding Negative Test Cases

Examples of negative tests to add:

```go
// Unauthorized subscription (different user)
func TestUnauthorizedSubscriptionRejected(t *testing.T) {
    // Try to subscribe to session owned by different user
    // Expect 403 error
}

// Invalid session_id
func TestInvalidSessionIDRejected(t *testing.T) {
    // Subscribe with non-existent session_id
    // Expect 400 error
}

// Subscription limit exceeded
func TestSubscriptionLimitEnforced(t *testing.T) {
    // Subscribe to 100+ sessions
    // Verify 429 error on limit
}
```

### Adding Load Tests

```go
// Test with many concurrent subscribers
func TestManySubscribersReceiveEvent(t *testing.T) {
    // Create 100 clients
    // All subscribe to same session
    // Send event
    // Verify all receive it
}

// Test with high event rate
func TestHighEventRateDelivery(t *testing.T) {
    // Append 1000 events rapidly
    // Verify all received in order on WebSocket
}
```

## Architecture Overview

### Event Flow

1. **AppendEvent** (miniapp service):
   - Writes event to PostgreSQL
   - Publishes to Redis channel asynchronously
   - Returns event_seq

2. **Redis Pub/Sub** (realtime handler):
   - Subscribers listening on `miniapp:session:{id}:events`
   - Each client goroutine reads from subscription

3. **WebSocket Send** (realtime handler):
   - Client send loop reads from channel
   - Encodes event as JSON
   - Writes to WebSocket connection

4. **Client Receive** (JavaScript):
   - WebSocket message listener
   - Parses envelope
   - Updates UI

### Concurrency Model

- One goroutine per WebSocket connection
- One goroutine per session subscription (per connection)
- Redis pub/sub handles fanout
- No locking needed on event delivery (Redis guarantees ordering)

### Failure Modes

1. **Redis unavailable**: Events not published, but database record survives
2. **WebSocket disconnects**: Subscription cleaned up automatically
3. **Event during subscription setup**: May be delivered or missed (best-effort)
4. **Subscriber joins after event**: Event already in database, available via polling

## References

- WebSocket Protocol: RFC 6455
- Mini-App Session Management: `/docs/miniapp/`
- Real-Time Handler: `/services/gateway/internal/realtime/`
- Event Model: `/docs/miniapp/event-model.md`
