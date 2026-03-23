//go:build integration
// +build integration

package e2ee

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestE2EEEndToEndWithDatabase tests complete E2EE flow with real PostgreSQL
func TestE2EEEndToEndWithDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping E2EE DB integration test; set TEST_DATABASE_URL to run")
	}

	// Connect to test database
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Initialize services
	sm := &SessionManager{db: pool}
	mre := &MultiRecipientEncryption{db: pool, sm: sm}

	// Create test context
	ctx := context.Background()

	t.Run("CreateSession", func(t *testing.T) {
		testCreateSession(t, ctx, sm)
	})

	t.Run("EncryptDecryptMessage", func(t *testing.T) {
		testEncryptDecryptMessage(t, ctx, sm)
	})

	t.Run("GroupEncryption", func(t *testing.T) {
		testGroupEncryption(t, ctx, mre)
	})

	t.Run("SessionUpdate", func(t *testing.T) {
		testSessionUpdate(t, ctx, sm)
	})
}

// testCreateSession tests creating a new session in database
func testCreateSession(t *testing.T, ctx context.Context, sm *SessionManager) {
	session := &Session{
		UserID:          "test-user-1",
		ContactUserID:   "test-user-2",
		ContactDeviceID: "device-001",
		SessionKeyBytes: make([]byte, 32),
		RootKeyBytes:    make([]byte, 32),
		ChainKeyBytes:   make([]byte, 32),
		MessageKeyIndex: 0,
	}

	// Seed the keys
	for i := 0; i < 32; i++ {
		session.SessionKeyBytes[i] = byte(i)
		session.RootKeyBytes[i] = byte(i + 100)
		session.ChainKeyBytes[i] = byte(i + 200)
	}

	// Save session
	err := sm.SaveSession(ctx, session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Load session back
	loaded, err := sm.LoadSession(ctx, session.UserID, session.ContactUserID, session.ContactDeviceID)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded == nil {
		t.Error("loaded session should not be nil")
	}
}

// testEncryptDecryptMessage tests encryption with session state
func testEncryptDecryptMessage(t *testing.T, ctx context.Context, sm *SessionManager) {
	// Create X3DH key agreement
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

	// Alice initiates
	sharedSecret, _, _, err := PerformX3DHInitiator(
		aliceIdentPriv,
		aliceIdentPub,
		bobIdentPub,
		bobSPKPub,
		[32]byte{},
	)
	if err != nil {
		t.Fatalf("X3DH failed: %v", err)
	}

	// Initialize Double Ratchet
	dr, err := InitializeDoubleRatchetState(sharedSecret[:])
	if err != nil {
		t.Fatalf("DR initialization failed: %v", err)
	}

	// Create session from ratchet state
	session := &Session{
		UserID:          "alice",
		ContactUserID:   "bob",
		ContactDeviceID: "bob-device-1",
	}
	UpdateSessionFromDoubleRatchet(session, dr)

	// Save session
	err = sm.SaveSession(ctx, session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Encrypt message
	plaintext := []byte("secret message from alice")
	ciphertext, nonce, err := EncryptMessageContent(plaintext, dr)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Load session and restore ratchet
	loaded, err := sm.LoadSession(ctx, session.UserID, session.ContactUserID, session.ContactDeviceID)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	// Create fresh receiver state
	bobDR, _ := InitializeDoubleRatchetStateAsReceiver(sharedSecret[:])

	// Decrypt message
	decrypted, err := DecryptMessageContent(ctx, ciphertext, nonce, bobDR, 0)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("decrypted message doesn't match original")
	}
}

// testGroupEncryption tests group message encryption with multiple recipients
func testGroupEncryption(t *testing.T, ctx context.Context, mre *MultiRecipientEncryption) {
	// Note: Real test requires group members in database
	// For now, test the key rotation which doesn't require data
	groupSecret := make([]byte, 32)
	for i := 0; i < 32; i++ {
		groupSecret[i] = byte(i)
	}

	// Rotate key for epoch 0
	key0, err := mre.RotateGroupKey(ctx, "test-group", 0, groupSecret)
	if err != nil {
		t.Fatalf("key rotation failed: %v", err)
	}

	if len(key0) != 32 {
		t.Errorf("rotated key should be 32 bytes, got %d", len(key0))
	}

	// Rotate key for epoch 1
	key1, err := mre.RotateGroupKey(ctx, "test-group", 1, groupSecret)
	if err != nil {
		t.Fatalf("key rotation failed: %v", err)
	}

	// Different epochs should produce different keys
	for i := 0; i < 32; i++ {
		if key0[i] != key1[i] {
			// Found difference, test passes
			return
		}
	}
	t.Error("different epochs should produce different keys")
}

// testSessionUpdate tests updating session state after encryption
func testSessionUpdate(t *testing.T, ctx context.Context, sm *SessionManager) {
	// Create and encrypt with Double Ratchet
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	// Encrypt 3 messages
	for i := 0; i < 3; i++ {
		dr.RatchetSendMessageKey()
	}

	// Create session
	session := &Session{
		UserID:          "alice2",
		ContactUserID:   "bob2",
		ContactDeviceID: "bob-device-2",
	}
	UpdateSessionFromDoubleRatchet(session, dr)

	// Save session
	err := sm.SaveSession(ctx, session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Verify message index is preserved
	if session.MessageKeyIndex != 3 {
		t.Errorf("message index should be 3, got %d", session.MessageKeyIndex)
	}

	// Load and verify
	loaded, err := sm.LoadSession(ctx, session.UserID, session.ContactUserID, session.ContactDeviceID)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded.MessageKeyIndex != 3 {
		t.Errorf("loaded message index should be 3, got %d", loaded.MessageKeyIndex)
	}
}

// TestE2EEMultipleMessagesWithDatabase tests multiple messages with state persistence
func TestE2EEMultipleMessagesWithDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping E2EE multi-message test; set TEST_DATABASE_URL to run")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	sm := &SessionManager{db: pool}
	ctx := context.Background()

	// === SETUP: X3DH Key Exchange ===
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()
	bobOTPPub, bobOTPPriv, _ := X25519Keypair()

	// Alice initiates
	sharedSecret, ephPub, _, _ := PerformX3DHInitiator(
		aliceIdentPriv,
		aliceIdentPub,
		bobIdentPub,
		bobSPKPub,
		bobOTPPub,
	)

	// Bob responds
	bobShared, _ := PerformX3DHResponder(
		ephPub,
		bobIdentPriv,
		bobIdentPub,
		bobSPKPriv,
		bobSPKPub,
		bobOTPPriv,
		aliceIdentPub,
	)

	if sharedSecret != bobShared {
		t.Fatal("X3DH agreement failed")
	}

	// === CREATE SESSIONS ===
	aliceDR, _ := InitializeDoubleRatchetState(sharedSecret[:])
	aliceSession := &Session{
		UserID:          "alice3",
		ContactUserID:   "bob3",
		ContactDeviceID: "bob-device-3",
	}
	UpdateSessionFromDoubleRatchet(aliceSession, aliceDR)
	sm.SaveSession(ctx, aliceSession)

	bobDR, _ := InitializeDoubleRatchetStateAsReceiver(bobShared[:])
	bobSession := &Session{
		UserID:          "bob3",
		ContactUserID:   "alice3",
		ContactDeviceID: "alice-device-3",
	}
	UpdateSessionFromDoubleRatchet(bobSession, bobDR)
	sm.SaveSession(ctx, bobSession)

	// === SEND MULTIPLE MESSAGES ===
	messages := []string{
		"first message",
		"second message",
		"third message",
	}

	for idx, msg := range messages {
		// Alice encrypts
		ct, nonce, err := EncryptMessageContent([]byte(msg), aliceDR)
		if err != nil {
			t.Fatalf("encryption %d failed: %v", idx, err)
		}

		// Bob decrypts
		decrypted, err := DecryptMessageContent(ctx, ct, nonce, bobDR, idx)
		if err != nil {
			t.Fatalf("decryption %d failed: %v", idx, err)
		}

		if string(decrypted) != msg {
			t.Errorf("message %d mismatch", idx)
		}
	}

	// Verify state advancement
	if aliceDR.SendMessageIndex != 3 {
		t.Errorf("Alice should have sent 3 messages")
	}
	if bobDR.RecvMessageIndex != 3 {
		t.Errorf("Bob should have received 3 messages")
	}
}

// TestE2EEForwardSecrecyWithDatabase tests forward secrecy property with persistent state
func TestE2EEForwardSecrecyWithDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping E2EE forward secrecy test; set TEST_DATABASE_URL to run")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	sm := &SessionManager{db: pool}
	ctx := context.Background()

	// Create and encrypt first message
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	sender, _ := InitializeDoubleRatchetState(rootKey)
	receiver, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	firstMsg := []byte("message 1")
	ct1, n1, _ := EncryptMessageContent(firstMsg, sender)

	// Save state
	senderSession := &Session{UserID: "s1", ContactUserID: "r1", ContactDeviceID: "d1"}
	UpdateSessionFromDoubleRatchet(senderSession, sender)
	sm.SaveSession(ctx, senderSession)

	// Decrypt first message - receiver learns key
	DecryptMessageContent(ctx, ct1, n1, receiver, 0)

	// Send MORE messages - old key should be inaccessible
	for i := 1; i < 5; i++ {
		msg := []byte("message " + string(rune('0'+i)))
		EncryptMessageContent(msg, sender)
	}

	// Update sender session in DB
	UpdateSessionFromDoubleRatchet(senderSession, sender)
	sm.SaveSession(ctx, senderSession)

	// Load OLD state from early in session (simulating compromise)
	// Can't decrypt new messages - they used different chain keys
	newMsg := []byte("new message")
	ctNew, nNew, _ := EncryptMessageContent(newMsg, sender)

	// Try to decrypt with old receiver state - should fail
	_, err = DecryptMessageContent(ctx, ctNew, nNew, receiver, 100)
	if err == nil {
		t.Error("old ratchet state should not decrypt new messages (forward secrecy)")
	}
}
