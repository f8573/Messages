package miniapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSessionCreatedEvent verifies that session creation logs the session_created event
func TestSessionCreatedEvent(t *testing.T) {
	// Setup: Create test database connection
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Test: Create a session
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	viewerID := uuid.New().String()

	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: viewerID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: viewerID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read", "storage.write"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Verify: session_created event exists in event log
	events, err := svc.GetSessionEvents(ctx, sessionID, nil, 100, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents failed: %v", err)
	}

	if len(events) == 0 {
		t.Fatal("No events found for session")
	}

	firstEvent := events[0]
	if firstEvent.EventType != EventTypeSessionCreated {
		t.Errorf("Expected event_type=%s, got %s", EventTypeSessionCreated, firstEvent.EventType)
	}

	if firstEvent.ActorID == nil || *firstEvent.ActorID != viewerID {
		t.Errorf("Expected actor_id=%s, got %v", viewerID, firstEvent.ActorID)
	}

	metadata := firstEvent.Body
	if metadata["participant_count"] != float64(1) {
		t.Errorf("Expected participant_count=1, got %v", metadata["participant_count"])
	}
}

// TestSessionJoinedEvent verifies that joining a session logs the session_joined event
func TestSessionJoinedEvent(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Setup: Create a session
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	creatorID := uuid.New().String()
	joinerID := uuid.New().String()

	// Creator joins first
	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: creatorID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: creatorID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Test: Second participant joins
	_, err = svc.JoinSession(ctx, joinerID, sessionID, []string{"storage.read"})
	if err != nil {
		t.Fatalf("JoinSession failed: %v", err)
	}

	// Verify: session_joined event exists
	events, err := svc.GetSessionEvents(ctx, sessionID, nil, 100, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents failed: %v", err)
	}

	var joinEvent *SessionEvent
	for _, e := range events {
		if e.EventType == EventTypeSessionJoined {
			joinEvent = &e
			break
		}
	}

	if joinEvent == nil {
		t.Fatal("No session_joined event found")
	}

	if joinEvent.ActorID == nil || *joinEvent.ActorID != joinerID {
		t.Errorf("Expected actor_id=%s, got %v", joinerID, joinEvent.ActorID)
	}
}

// TestSnapshotWrittenEvent verifies that snapshots log the snapshot_written event
func TestSnapshotWrittenEvent(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Setup: Create a session
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	userID := uuid.New().String()

	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: userID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: userID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read", "storage.write"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Test: Snapshot the session
	newState := map[string]any{"level": 5, "score": 1000}
	version, err := svc.SnapshotSession(ctx, sessionID, newState, 2, userID)
	if err != nil {
		t.Fatalf("SnapshotSession failed: %v", err)
	}

	// Verify: snapshot_written event exists
	events, err := svc.GetSessionEvents(ctx, sessionID, nil, 100, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents failed: %v", err)
	}

	var snapshotEvent *SessionEvent
	for _, e := range events {
		if e.EventType == EventTypeSnapshotWritten {
			snapshotEvent = &e
			break
		}
	}

	if snapshotEvent == nil {
		t.Fatal("No snapshot_written event found")
	}

	if snapshotEvent.ActorID == nil || *snapshotEvent.ActorID != userID {
		t.Errorf("Expected actor_id=%s, got %v", userID, snapshotEvent.ActorID)
	}

	metadata := snapshotEvent.Body
	if int(metadata["state_version"].(float64)) != version {
		t.Errorf("Expected state_version=%d, got %v", version, metadata["state_version"])
	}
}

// TestAppendOnlyEnforcement verifies that events cannot be modified after creation
func TestAppendOnlyEnforcement(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	// Test: Attempt to update an event (should fail)
	sessionID := uuid.New().String()
	err = pool.QueryRow(ctx, `
		UPDATE miniapp_events
		SET body = '{}'
		WHERE app_session_id = $1 AND event_seq = 1
	`, sessionID).Scan()

	if err == nil {
		t.Error("Expected UPDATE to fail, but it succeeded")
	}

	// Verify the error message indicates append-only constraint
	if err.Error() != "miniapp_events table is append-only: UPDATE not allowed" {
		t.Errorf("Expected append-only error, got: %v", err)
	}

	// Test: Attempt to delete an event (should fail)
	err = pool.QueryRow(ctx, `
		DELETE FROM miniapp_events
		WHERE app_session_id = $1 AND event_seq = 1
	`, sessionID).Scan()

	if err == nil {
		t.Error("Expected DELETE to fail, but it succeeded")
	}

	if err.Error() != "miniapp_events table is append-only: DELETE not allowed" {
		t.Errorf("Expected append-only error, got: %v", err)
	}
}

// TestEventOrdering verifies that events are always in order by event_seq
func TestEventOrdering(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Setup: Create a session and perform multiple operations
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	userID := uuid.New().String()

	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: userID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: userID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read", "storage.write"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Perform multiple operations
	joinerID := uuid.New().String()
	_, _ = svc.JoinSession(ctx, joinerID, sessionID, []string{"storage.read"})

	// Snapshot multiple times
	_, _ = svc.SnapshotSession(ctx, sessionID, map[string]any{"v": 1}, 2, userID)
	_, _ = svc.SnapshotSession(ctx, sessionID, map[string]any{"v": 2}, 3, userID)
	_, _ = svc.SnapshotSession(ctx, sessionID, map[string]any{"v": 3}, 4, userID)

	// Verify: All events in order
	events, err := svc.GetSessionEvents(ctx, sessionID, nil, 100, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents failed: %v", err)
	}

	if len(events) < 5 {
		t.Fatalf("Expected at least 5 events, got %d", len(events))
	}

	// Verify ordering by event_seq
	for i := 0; i < len(events)-1; i++ {
		if events[i].EventSeq >= events[i+1].EventSeq {
			t.Errorf("Events out of order: event[%d]=%d, event[%d]=%d",
				i, events[i].EventSeq, i+1, events[i+1].EventSeq)
		}
	}

	// Verify event type order: created, joined, snapshot, snapshot, snapshot
	expectedTypes := []string{
		EventTypeSessionCreated,
		EventTypeSessionJoined,
		EventTypeSnapshotWritten,
		EventTypeSnapshotWritten,
		EventTypeSnapshotWritten,
	}

	for i, expectedType := range expectedTypes {
		if events[i].EventType != expectedType {
			t.Errorf("Event[%d]: expected type=%s, got %s", i, expectedType, events[i].EventType)
		}
	}
}

// TestGetSessionEventsFiltering verifies that event_type filtering works
func TestGetSessionEventsFiltering(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Setup: Create session with multiple event types
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	userID := uuid.New().String()

	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: userID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: userID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read", "storage.write"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create more joined events
	for i := 0; i < 3; i++ {
		joinerID := uuid.New().String()
		_, _ = svc.JoinSession(ctx, joinerID, sessionID, []string{"storage.read"})
	}

	// Test: Filter by event_type
	eventType := EventTypeSessionJoined
	events, err := svc.GetSessionEvents(ctx, sessionID, &eventType, 100, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents with filter failed: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 session_joined events, got %d", len(events))
	}

	for _, e := range events {
		if e.EventType != EventTypeSessionJoined {
			t.Errorf("Filter failed: got event_type=%s", e.EventType)
		}
	}
}

// TestGetSessionEventsPagination verifies that pagination works
func TestGetSessionEventsPagination(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer pool.Close()

	svc := NewService(pool, testConfig, nil, nil)

	// Setup: Create session with many events
	sessionID := uuid.New().String()
	conversationID := uuid.New().String()
	userID := uuid.New().String()

	input := CreateSessionInput{
		ManifestID:         uuid.New().String(),
		ConversationID:     conversationID,
		Viewer:             SessionParticipant{UserID: userID, Role: "PLAYER"},
		Participants:       []SessionParticipant{{UserID: userID, Role: "PLAYER"}},
		GrantedPermissions: []string{"storage.read", "storage.write"},
		TTL:                30 * time.Minute,
	}

	_, _, err = svc.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create 20 snapshot events
	for i := 0; i < 20; i++ {
		_, _ = svc.SnapshotSession(ctx, sessionID, map[string]any{"i": i}, i+2, userID)
	}

	// Test: Pagination with limit=5, offset=0
	events1, err := svc.GetSessionEvents(ctx, sessionID, nil, 5, 0)
	if err != nil {
		t.Fatalf("GetSessionEvents page 1 failed: %v", err)
	}

	if len(events1) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events1))
	}

	// Test: Pagination with limit=5, offset=5
	events2, err := svc.GetSessionEvents(ctx, sessionID, nil, 5, 5)
	if err != nil {
		t.Fatalf("GetSessionEvents page 2 failed: %v", err)
	}

	if len(events2) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events2))
	}

	// Verify no overlap
	for _, e1 := range events1 {
		for _, e2 := range events2 {
			if e1.EventSeq == e2.EventSeq {
				t.Errorf("Events overlap: seq=%d appears in both pages", e1.EventSeq)
			}
		}
	}
}
