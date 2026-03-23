package e2ee

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
)

// TestEncryptMessageContentRoundTrip tests message encryption/decryption
func TestEncryptMessageContentRoundTrip(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	senderState, _ := InitializeDoubleRatchetState(rootKey)
	receiverState, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	plaintext := []byte("secret message")

	// Sender encrypts
	ciphertext, nonce, err := EncryptMessageContent(plaintext, senderState)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Receiver decrypts
	decrypted, err := DecryptMessageContent(context.Background(), ciphertext, nonce, receiverState, 0)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted text doesn't match original")
	}
}

// TestGenerateRecipientWrappedKeyForwarding tests key wrapping/unwrapping
func TestGenerateRecipientWrappedKeyForwarding(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	recipientState, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	// Encode sender's public key for recipient
	senderKeyB64 := ""
	{
		pub, _, _ := X25519Keypair()
		senderKeyB64 = base64.StdEncoding.EncodeToString(pub[:])
	}

	// Recipient wraps their state's root key for sender
	wrappedKey, wrapNonce, err := GenerateRecipientWrappedKey(senderKeyB64, recipientState)
	if err != nil {
		t.Fatalf("failed to generate wrapped key: %v", err)
	}

	if wrappedKey == "" || wrapNonce == "" {
		t.Error("wrapped key or nonce is empty")
	}
}

// TestEncryptMessageContentMultiple tests multiple consecutive messages
func TestEncryptMessageContentMultiple(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	senderState, _ := InitializeDoubleRatchetState(rootKey)
	receiverState, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	messages := []string{
		"message one",
		"message two",
		"message three",
	}

	for idx, msg := range messages {
		plaintext := []byte(msg)

		// Sender encrypts
		ciphertext, nonce, err := EncryptMessageContent(plaintext, senderState)
		if err != nil {
			t.Fatalf("message %d encryption failed: %v", idx, err)
		}

		// Receiver decrypts
		decrypted, err := DecryptMessageContent(context.Background(), ciphertext, nonce, receiverState, idx)
		if err != nil {
			t.Fatalf("message %d decryption failed: %v", idx, err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("message %d: decrypted text doesn't match original", idx)
		}
	}
}

// TestDecryptMessageContentInvalidState tests error handling
func TestDecryptMessageContentInvalidState(t *testing.T) {
	_, err := DecryptMessageContent(context.Background(), "invalid", "invalid", nil, 0)
	if err == nil {
		t.Error("should reject nil state")
	}
}

// TestEncryptMessageContentInvalidState tests error handling
func TestEncryptMessageContentInvalidState(t *testing.T) {
	_, _, err := EncryptMessageContent([]byte("test"), nil)
	if err == nil {
		t.Error("should reject nil state")
	}
}

// TestSessionManagerEncryptionIntegration tests encryption with database session
func TestSessionManagerEncryptionIntegration(t *testing.T) {
	// Note: This test is skipped because real database would be needed
	// The EncryptMessageWithSession method requires a working database pool
	t.Skip("integration test requires database - tested via crypto functions directly")
}

// TestSessionPersistenceRoundTrip tests saving/loading encryption state
func TestSessionPersistenceRoundTrip(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr1, _ := InitializeDoubleRatchetState(rootKey)

	// Advance state with several messages
	for i := 0; i < 3; i++ {
		dr1.RatchetSendMessageKey()
	}

	// Save to session
	session := &Session{}
	UpdateSessionFromDoubleRatchet(session, dr1)

	// Verify session contains the right state
	if len(session.RootKeyBytes) != 32 {
		t.Error("session should have root key")
	}
	if len(session.ChainKeyBytes) != 32 {
		t.Error("session should have chain key")
	}
	if session.MessageKeyIndex != 3 {
		t.Errorf("session should have message index 3, got %d", session.MessageKeyIndex)
	}

	// Restore from session
	dr2, err := CreateDoubleRatchetStateFromSession(session)
	if err != nil {
		t.Fatalf("failed to restore: %v", err)
	}

	// States should match
	if dr2.SendChainKey != dr1.SendChainKey {
		t.Error("send chain key should match after restore")
	}
	if dr2.SendMessageIndex != dr1.SendMessageIndex {
		t.Error("send message index should match after restore")
	}
	if dr2.RootKey != dr1.RootKey {
		t.Error("root key should match after restore")
	}
}

// BenchmarkEncryptMessageContent benchmarks message encryption
func BenchmarkEncryptMessageContent(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	state, _ := InitializeDoubleRatchetState(rootKey)
	plaintext := []byte("test message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncryptMessageContent(plaintext, state)
	}
}

// BenchmarkDecryptMessageContent benchmarks message decryption
func BenchmarkDecryptMessageContent(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	senderState, _ := InitializeDoubleRatchetState(rootKey)
	receiverState, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	plaintext := []byte("test message")
	ciphertext, nonce, _ := EncryptMessageContent(plaintext, senderState)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecryptMessageContent(context.Background(), ciphertext, nonce, receiverState, i%100)
	}
}

// BenchmarkGenerateRecipientWrappedKey benchmarks key wrapping
func BenchmarkGenerateRecipientWrappedKey(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	state, _ := InitializeDoubleRatchetState(rootKey)

	keyB64 := ""
	{
		pub, _, _ := X25519Keypair()
		keyB64 = base64.StdEncoding.EncodeToString(pub[:])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateRecipientWrappedKey(keyB64, state)
	}
}
