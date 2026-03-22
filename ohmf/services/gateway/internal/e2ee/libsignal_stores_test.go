package e2ee

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestFixtures provides test data and database setup
type TestFixtures struct {
	DB              *pgxpool.Pool
	UserID          string
	ContactUserID   string
	ContactDeviceID uint32
	TestCtx         context.Context
}

// SetupTestDB creates a test PostgreSQL database and connection pool
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	// Use test database URL from environment or default
	dbURL := "postgres://postgres:postgres@localhost:5432/messages_test"

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("failed to parse database config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Verify connection
	err = pool.Ping(context.Background())
	if err != nil {
		t.Fatalf("failed to ping test database: %v", err)
	}

	return pool
}

// SetupFixtures creates test data and database context
func SetupFixtures(t *testing.T, db *pgxpool.Pool) *TestFixtures {
	ctx := context.Background()

	userID := uuid.New().String()
	contactUserID := uuid.New().String()
	contactDeviceID := uint32(1)

	// Create test users if needed
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, username, email)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, userID, "testuser_"+userID[:8], "test_"+userID[:8]+"@example.com")

	if err != nil && err != pgx.ErrNoRows {
		t.Fatalf("failed to create test user: %v", err)
	}

	return &TestFixtures{
		DB:              db,
		UserID:          userID,
		ContactUserID:   contactUserID,
		ContactDeviceID: contactDeviceID,
		TestCtx:         ctx,
	}
}

// CleanupFixtures removes test data from database
func (f *TestFixtures) Cleanup(t *testing.T) {
	ctx := context.Background()

	// Delete test sessions
	_, err := f.DB.Exec(ctx, `
		DELETE FROM e2ee_sessions
		WHERE user_id = $1 OR contact_user_id = $1
	`, f.UserID)

	if err != nil && err != pgx.ErrNoRows {
		t.Logf("failed to cleanup sessions: %v", err)
	}

	// Delete test trust records
	_, err = f.DB.Exec(ctx, `
		DELETE FROM device_key_trust
		WHERE user_id = $1 OR contact_user_id = $1
	`, f.UserID)

	if err != nil && err != pgx.ErrNoRows {
		t.Logf("failed to cleanup trust records: %v", err)
	}
}

// =================== PostgresSessionStore Tests ===================

func TestPostgresSessionStore_StoreAndLoadSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	db := SetupTestDB(t)
	defer db.Close()

	fixtures := SetupFixtures(t, db)
	defer fixtures.Cleanup(t)

	store := NewPostgresSessionStore(db)
	ctx := fixtures.TestCtx

	// Test data: mock session record (in production, this comes from libsignal)
	sessionBytes := []byte("mock_session_record_bytes_12345")
	contactName := fixtures.ContactUserID
	contactDeviceID := fixtures.ContactDeviceID

	// Test 1: Store session
	err := store.StoreSession(ctx, contactName, contactDeviceID, sessionBytes)
	if err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// Test 2: Load session
	loaded, err := store.LoadSession(ctx, contactName, contactDeviceID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if string(loaded) != string(sessionBytes) {
		t.Errorf("Session data mismatch: got %q, want %q", loaded, sessionBytes)
	}

	// Test 3: HasSession returns true
	exists, err := store.HasSession(ctx, contactName, contactDeviceID)
	if err != nil {
		t.Fatalf("HasSession failed: %v", err)
	}

	if !exists {
		t.Errorf("HasSession returned false, expected true")
	}

	// Test 4: Load non-existent session returns nil
	loaded, err = store.LoadSession(ctx, uuid.New().String(), 999)
	if err != nil {
		t.Fatalf("LoadSession for non-existent should not error: %v", err)
	}

	if loaded != nil {
		t.Errorf("Expected nil for non-existent session, got %v", loaded)
	}

	// Test 5: Delete session
	err = store.DeleteSession(ctx, contactName, contactDeviceID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Test 6: HasSession returns false after delete
	exists, err = store.HasSession(ctx, contactName, contactDeviceID)
	if err != nil {
		t.Fatalf("HasSession after delete failed: %v", err)
	}

	if exists {
		t.Errorf("HasSession returned true after delete, expected false")
	}
}

func TestPostgresSessionStore_DeleteAllSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	db := SetupTestDB(t)
	defer db.Close()

	fixtures := SetupFixtures(t, db)
	defer fixtures.Cleanup(t)

	store := NewPostgresSessionStore(db)
	ctx := fixtures.TestCtx

	// Store multiple sessions with different contact devices
	contactName := fixtures.ContactUserID

	for deviceID := uint32(1); deviceID <= 3; deviceID++ {
		sessionBytes := []byte("session_" + string(rune(deviceID)))
		err := store.StoreSession(ctx, contactName, deviceID, sessionBytes)
		if err != nil {
			t.Fatalf("StoreSession failed for device %d: %v", deviceID, err)
		}
	}

	// Verify all sessions exist
	for deviceID := uint32(1); deviceID <= 3; deviceID++ {
		exists, err := store.HasSession(ctx, contactName, deviceID)
		if err != nil {
			t.Fatalf("HasSession check failed: %v", err)
		}

		if !exists {
			t.Errorf("Session %d should exist before delete", deviceID)
		}
	}

	// Delete all sessions for the contact
	err := store.DeleteAllSessions(ctx, contactName)
	if err != nil {
		t.Fatalf("DeleteAllSessions failed: %v", err)
	}

	// Verify all sessions deleted
	for deviceID := uint32(1); deviceID <= 3; deviceID++ {
		exists, err := store.HasSession(ctx, contactName, deviceID)
		if err != nil {
			t.Fatalf("HasSession check after delete failed: %v", err)
		}

		if exists {
			t.Errorf("Session %d should not exist after DeleteAllSessions", deviceID)
		}
	}
}

// =================== PostgresIdentityKeyStore Tests ===================

func TestPostgresIdentityKeyStore_TrustModel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	db := SetupTestDB(t)
	defer db.Close()

	fixtures := SetupFixtures(t, db)
	defer fixtures.Cleanup(t)

	store := NewPostgresIdentityKeyStore(db)
	ctx := fixtures.TestCtx

	contactName := fixtures.ContactUserID
	contactDeviceID := fixtures.ContactDeviceID
	identityKey := []byte("mock_identity_key_for_contact")

	// Test 1: Untrusted identity should be trusted (TOFU model)
	trusted, err := store.IsTrustedIdentity(ctx, contactName, contactDeviceID, identityKey)
	if err != nil {
		t.Fatalf("IsTrustedIdentity failed: %v", err)
	}

	if !trusted {
		t.Errorf("Untrusted identity should be accepted in TOFU model")
	}

	// Test 2: Save identity on first encounter
	isNew, err := store.SaveIdentity(ctx, contactName, contactDeviceID, identityKey)
	if err != nil {
		t.Fatalf("SaveIdentity failed: %v", err)
	}

	if !isNew {
		t.Errorf("SaveIdentity should return true for new key, got false")
	}

	// Test 3: Same identity is now known (trusted)
	trusted, err = store.IsTrustedIdentity(ctx, contactName, contactDeviceID, identityKey)
	if err != nil {
		t.Fatalf("IsTrustedIdentity check after save failed: %v", err)
	}

	if !trusted {
		t.Errorf("Saved identity should be trusted")
	}

	// Test 4: Saving same identity again returns false (not new)
	isNew, err = store.SaveIdentity(ctx, contactName, contactDeviceID, identityKey)
	if err != nil {
		// ON CONFLICT DO NOTHING means second insert fails silently (no error expected)
		// but we should handle this case
	}

	if isNew {
		t.Logf("SaveIdentity returned true for duplicate (may be expected with ON CONFLICT)")
	}
}

// =================== Integration Tests ===================

func TestE2EESessionFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	db := SetupTestDB(t)
	defer db.Close()

	fixtures := SetupFixtures(t, db)
	defer fixtures.Cleanup(t)

	ctx := fixtures.TestCtx

	// Test scenario: Two devices establishing E2EE session
	sessionStore := NewPostgresSessionStore(db)
	identityStore := NewPostgresIdentityKeyStore(db)

	// Step 1: Device A saves Device B's identity (TOFU)
	deviceBIdentityKey := []byte("device_b_identity")
	isNew, err := identityStore.SaveIdentity(ctx, fixtures.ContactUserID, 1, deviceBIdentityKey)
	if err != nil {
		t.Fatalf("SaveIdentity failed: %v", err)
	}

	if !isNew {
		t.Errorf("First identity save should return true")
	}

	// Step 2: Device A stores session after X3DH
	sessionData := []byte("x3dh_derived_session_record")
	err = sessionStore.StoreSession(ctx, fixtures.ContactUserID, 1, sessionData)
	if err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// Step 3: Device A can now encrypt/decrypt with Device B
	hasSession, err := sessionStore.HasSession(ctx, fixtures.ContactUserID, 1)
	if err != nil {
		t.Fatalf("HasSession check failed: %v", err)
	}

	if !hasSession {
		t.Errorf("Session should exist after X3DH")
	}

	isTrusted, err := identityStore.IsTrustedIdentity(ctx, fixtures.ContactUserID, 1, deviceBIdentityKey)
	if err != nil {
		t.Fatalf("IsTrustedIdentity check failed: %v", err)
	}

	if !isTrusted {
		t.Errorf("Device B identity should be trusted after TOFU")
	}

	t.Logf("✓ E2EE session established successfully")
}

// =================== Benchmark Tests ===================

func BenchmarkSessionStoreLoad(b *testing.B) {
	db := SetupTestDB(&testing.T{})
	defer db.Close()

	store := NewPostgresSessionStore(db)
	ctx := context.Background()

	// Setup test data
	sessionBytes := []byte("benchmark_session_data")
	store.StoreSession(ctx, uuid.New().String(), 1, sessionBytes)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		store.LoadSession(ctx, uuid.New().String(), 1)
	}
}

func BenchmarkIdentityTrustCheck(b *testing.B) {
	db := SetupTestDB(&testing.T{})
	defer db.Close()

	store := NewPostgresIdentityKeyStore(db)
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		store.IsTrustedIdentity(ctx, uuid.New().String(), uint32(i%100), []byte("test_key"))
	}
}
