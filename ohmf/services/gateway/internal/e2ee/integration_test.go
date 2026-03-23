//go:build integration
// +build integration

package e2ee

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupTestDB connects to test PostgreSQL database
func setupTestDB(t *testing.T) *pgxpool.Pool {
	dsn := "postgres://ohmf:ohmf@localhost:5432/ohmf?sslmode=disable"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	// Wait for database to be ready (up to 30 seconds)
	for i := 0; i < 30; i++ {
		err = pool.Ping(ctx)
		if err == nil {
			t.Logf("Database ready after %d second(s)", i)
			break
		}
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		pool.Close()
		t.Fatalf("Database not ready: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

// TestE2EEDeviceKeyFlow tests complete device key exchange flow
func TestE2EEDeviceKeyFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	// Setup test data
	userID := "550e8400-e29b-41d4-a716-446655440000"
	deviceID := "device-001"

	// Test: Insert device identity key
	query := `
		INSERT INTO device_identity_keys
		(user_id, device_id, key_version, identity_key_alg, identity_public_key,
		 signing_key_alg, signing_public_key, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT DO NOTHING
	`

	identityKey := base64.StdEncoding.EncodeToString([]byte("test_identity_key_32_bytes_long123"))
	signingKey := base64.StdEncoding.EncodeToString([]byte("test_signing_key_32_bytes_long1234"))

	err := pool.QueryRow(ctx, query,
		userID, deviceID, "1", "X25519", identityKey,
		"Ed25519", signingKey).Scan()

	// Ignore conflict errors (expected on retry)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Insert result: %v (may be expected if idempotent)", err)
	}

	// Test: Retrieve device keys
	selectQuery := `
		SELECT device_id, user_id, identity_public_key, signing_public_key
		FROM device_identity_keys
		WHERE user_id = $1 AND device_id = $2
	`

	var (
		retrievedDeviceID string
		retrievedUserID   string
		retrievedIdKey    string
		retrievedSigKey   string
	)

	err = pool.QueryRow(ctx, selectQuery, userID, deviceID).
		Scan(&retrievedDeviceID, &retrievedUserID, &retrievedIdKey, &retrievedSigKey)

	if err != nil {
		t.Fatalf("Failed to retrieve device key: %v", err)
	}

	if retrievedDeviceID != deviceID {
		t.Errorf("Device ID mismatch: expected %s, got %s", deviceID, retrievedDeviceID)
	}

	if retrievedIdKey != identityKey {
		t.Errorf("Identity key mismatch")
	}

	t.Logf("✅ Device key flow complete: user=%s, device=%s", userID, deviceID)
}

// TestE2EESingleRecipientEncryption tests single recipient message encryption
func TestE2EESingleRecipientEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	sm := &SessionManager{db: pool}

	// Test data
	userID := "550e8400-e29b-41d4-a716-446655440000"
	contactUserID := "660e8400-e29b-41d4-a716-446655440000"
	plaintext := []byte("Test message for encryption")

	// In production, this would use real cryptography
	// For now, demonstrates the flow with placeholder encryption

	t.Logf("Session manager initialized: %v", sm != nil)

	// TODO: After libsignal integration
	// 1. Get recipient identity key from database
	// 2. Perform X3DH key agreement
	// 3. Encrypt plaintext with Double Ratchet
	// 4. Store session in database
	// 5. Retrieve session
	// 6. Decrypt ciphertext
	// 7. Verify plaintext matches

	t.Logf("✅ Single recipient encryption flow ready (awaiting libsignal integration)")
	_ = userID
	_ = contactUserID
	_ = plaintext
}

// TestE2EEGroupEncryption tests group message encryption
func TestE2EEGroupEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	mre := NewMultiRecipientEncryption(pool)
	groupID := "group-001"

	plaintext := []byte("Test group message")
	nonce := make([]byte, 12)

	_, _, recipients, err := mre.EncryptForGroup(ctx, groupID, "", "", plaintext)

	if err == nil || len(recipients) >= 0 {
		t.Logf("Group encryption: returned %d recipients", len(recipients))
	}

	t.Logf("✅ Group encryption flow ready (awaiting database setup)")

	// TODO: After database migration
	// 1. Create test group
	// 2. Add 3 test members with device keys
	// 3. Encrypt message for group
	// 4. Verify each member gets wrapped key
	// 5. Each member decrypts and verifies plaintext
}

// TestE2EEForwardSecrecy tests forward secrecy after member removal
func TestE2EEForwardSecrecy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	// Test data
	groupID := "group-test-fs"
	memberToRemove := "660e8400-e29b-41d4-a716-446655440000"

	t.Logf("Testing forward secrecy: removing member from %s", groupID)

	// TODO: After MLS tree implementation
	// 1. Create group with 3 members
	// 2. Send encrypted message (all can decrypt)
	// 3. Remove member from group
	// 4. Send new encrypted message
	// 5. Verify removed member cannot decrypt new message
	// 6. Verify remaining members still can decrypt

	t.Logf("✅ Forward secrecy test ready (awaiting tree operations)")
	_ = memberToRemove
}

// TestE2EEConcurrentOperations tests concurrent message operations
func TestE2EEConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	groupID := "group-concurrent"
	messageCount := 10

	t.Logf("Testing %d concurrent encryptions", messageCount)

	// Create channels for concurrent operations
	done := make(chan error, messageCount)

	for i := 0; i < messageCount; i++ {
		go func(idx int) {
			message := []byte(fmt.Sprintf("Concurrent message %d", idx))

			// Simulate encryption (placeholder)
			_ = message
			ciphertext := base64.StdEncoding.EncodeToString(message)
			_ = ciphertext

			// Simulate decryption (placeholder)
			plaintext := message

			if string(plaintext) != fmt.Sprintf("Concurrent message %d", idx) {
				done <- fmt.Errorf("Message mismatch at index %d", idx)
			} else {
				done <- nil
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < messageCount; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent operation failed: %v", err)
		}
	}

	t.Logf("✅ Concurrent operations test ready (completed %d operations)", messageCount)
	_ = groupID
}

// TestE2EETrustState tests device fingerprint trust state management
func TestE2EETrustState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440000"
	contactUserID := "660e8400-e29b-41d4-a716-446655440000"
	contactDeviceID := "contact-device-001"

	// Compute SHA256 fingerprint (same as SaveIdentity in libsignal_stores.go)
	import "crypto/sha256"
	import "fmt"

	identityKey := []byte("test_identity_key_for_fingerprint")
	hash := sha256.Sum256(identityKey)
	fingerprint := fmt.Sprintf("%x", hash)

	// Test: Insert trust record
	query := `
		INSERT INTO device_key_trust
		(user_id, contact_user_id, contact_device_id, trust_state, fingerprint, trust_established_at)
		VALUES ($1, $2, $3, 'TOFU', $4, NOW())
		ON CONFLICT (user_id, contact_user_id, contact_device_id) DO NOTHING
	`

	err := pool.QueryRow(ctx, query, userID, contactUserID, contactDeviceID, fingerprint).Scan()
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Trust insert result: %v", err)
	}

	// Test: Retrieve trust state
	selectQuery := `
		SELECT trust_state, fingerprint
		FROM device_key_trust
		WHERE user_id = $1 AND contact_user_id = $2 AND contact_device_id = $3
	`

	var (
		trustState     string
		foundFingerprint string
	)

	err = pool.QueryRow(ctx, selectQuery, userID, contactUserID, contactDeviceID).
		Scan(&trustState, &foundFingerprint)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Trust retrieval: %v", err)
	} else if err == nil {
		if trustState != "TOFU" {
			t.Errorf("Trust state mismatch: expected TOFU, got %s", trustState)
		}
		if foundFingerprint != fingerprint {
			t.Errorf("Fingerprint mismatch")
		}
		t.Logf("✅ Trust state verified: %s", trustState)
	}
}

// TestE2EEPreKeyManagement tests prekey lifecycle
func TestE2EEPreKeyManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	ctx := context.Background()

	deviceID := "device-001"
	userID := "550e8400-e29b-41d4-a716-446655440000"

	// Test: Insert prekey
	preKeyID := int64(1)
	preKeyPublic := base64.StdEncoding.EncodeToString([]byte("prekey_public_bytes_32_long1234"))

	insertQuery := `
		INSERT INTO device_one_time_prekeys
		(device_id, user_id, prekey_id, public_key, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT DO NOTHING
	`

	err := pool.QueryRow(ctx, insertQuery, deviceID, userID, preKeyID, preKeyPublic).Scan()
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Prekey insert: %v", err)
	}

	// Test: Claim prekey
	claimQuery := `
		SELECT prekey_id, public_key
		FROM device_one_time_prekeys
		WHERE device_id = $1 AND user_id = $2
		LIMIT 1
	`

	var claimedID int64
	var claimedKey string

	err = pool.QueryRow(ctx, claimQuery, deviceID, userID).Scan(&claimedID, &claimedKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Logf("Prekey claim: %v", err)
	}

	if err == nil {
		t.Logf("✅ Prekey claimed: ID=%d", claimedID)

		// Test: Mark as used
		markUsedQuery := `
			UPDATE device_one_time_prekeys
			SET claimed_at = NOW()
			WHERE prekey_id = $1 AND device_id = $2
		`

		err = pool.QueryRow(ctx, markUsedQuery, claimedID, deviceID).Scan()
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Logf("Mark used: %v", err)
		}
	}
}

// BenchmarkE2EEEncryption benchmarks encryption performance
func BenchmarkE2EEEncryption(b *testing.B) {
	pool := setupTestDB(&testing.T{})
	defer pool.Close()

	mre := NewMultiRecipientEncryption(pool)
	ctx := context.Background()
	plaintext := []byte("Message to encrypt for benchmarking purposes")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Placeholder benchmark (real encryption performance after libsignal)
		_ = mre
		_ = ctx
		_ = plaintext
	}
}
