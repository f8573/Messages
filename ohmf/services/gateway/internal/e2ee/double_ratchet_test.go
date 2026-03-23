package e2ee

import (
	"bytes"
	"testing"
)

// TestDoubleRatchetInitialization tests state creation from root key
func TestDoubleRatchetInitialization(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, err := InitializeDoubleRatchetState(rootKey)
	if err != nil {
		t.Fatalf("initialization failed: %v", err)
	}

	if dr.RootKey == [32]byte{} {
		t.Error("root key not set")
	}
	if dr.SendChainKey == [32]byte{} {
		t.Error("send chain key not initialized")
	}
	if dr.RecvChainKey == [32]byte{} {
		t.Error("recv chain key not initialized")
	}
	if dr.SendMessageIndex != 0 {
		t.Error("send message index should start at 0")
	}
	if dr.RecvMessageIndex != 0 {
		t.Error("recv message index should start at 0")
	}
	if dr.DhRatchetCounter != 0 {
		t.Error("DH ratchet counter should start at 0")
	}
}

// TestDoubleRatchetInitializationInvalidRootKey tests error handling
func TestDoubleRatchetInitializationInvalidRootKey(t *testing.T) {
	_, err := InitializeDoubleRatchetState([]byte("short"))
	if err == nil {
		t.Error("should reject short root key")
	}

	_, err = InitializeDoubleRatchetState(make([]byte, 64))
	if err == nil {
		t.Error("should reject oversized root key")
	}
}

// TestDoubleRatchetForwardSecrecy tests that old keys are lost after ratcheting
func TestDoubleRatchetForwardSecrecy(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)
	initialChainKey := dr.SendChainKey

	// Ratchet 5 times
	keys := make([][32]byte, 5)
	for i := 0; i < 5; i++ {
		keys[i], _ = dr.RatchetSendMessageKey()
	}

	// Chain key should have evolved
	if dr.SendChainKey == initialChainKey {
		t.Error("chain key should evolve after ratcheting")
	}

	// Message keys should all be different
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			if keys[i] == keys[j] {
				t.Errorf("message key %d should differ from %d", i, j)
			}
		}
	}
}

// TestDoubleRatchetSendMessageIncrement tests message counter advancement
func TestDoubleRatchetSendMessageIncrement(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	for i := 0; i < 10; i++ {
		if dr.SendMessageIndex != i {
			t.Errorf("expected message index %d, got %d", i, dr.SendMessageIndex)
		}
		dr.RatchetSendMessageKey()
	}

	if dr.SendMessageIndex != 10 {
		t.Errorf("expected final index 10, got %d", dr.SendMessageIndex)
	}
}

// TestDoubleRatchetRecvMessageInOrder tests in-order message receipt
func TestDoubleRatchetRecvMessageInOrder(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	// Receive messages 0-4 in order
	for msgIdx := 0; msgIdx < 5; msgIdx++ {
		_, err := dr.RatchetRecvMessageKey(msgIdx)
		if err != nil {
			t.Errorf("failed to derive key for message %d: %v", msgIdx, err)
		}
		if dr.RecvMessageIndex != msgIdx+1 {
			t.Errorf("expected recv index %d, got %d", msgIdx+1, dr.RecvMessageIndex)
		}
	}
}

// TestDoubleRatchetRecvMessageOutOfOrder tests out-of-order message handling
func TestDoubleRatchetRecvMessageOutOfOrder(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	// Receive message 5 (skipping 0-4)
	_, err := dr.RatchetRecvMessageKey(5)
	if err != nil {
		t.Errorf("failed to derive key for message 5: %v", err)
	}

	if dr.RecvMessageIndex != 6 {
		t.Errorf("expected recv index 6, got %d", dr.RecvMessageIndex)
	}

	// Now receive message 2 (out of order, but within window)
	_, err = dr.RatchetRecvMessageKey(2)
	if err == nil {
		t.Error("should reject message index 2 (already at 6)")
	}
}

// TestDoubleRatchetReplayDetection tests rejection of replayed messages
func TestDoubleRatchetReplayDetection(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	// Receive message 3
	_, err := dr.RatchetRecvMessageKey(3)
	if err != nil {
		t.Fatalf("failed to derive message 3: %v", err)
	}

	// Try to receive message 1 (before current index)
	_, err = dr.RatchetRecvMessageKey(1)
	if err == nil {
		t.Error("should reject replayed message (index < current)")
	}
}

// TestDoubleRatchetDoSProtection tests rejection of messages too far ahead
func TestDoubleRatchetDoSProtection(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	// Try to receive message way ahead (possible DoS)
	_, err := dr.RatchetRecvMessageKey(100000)
	if err == nil {
		t.Error("should reject message index too far ahead")
	}
}

// TestDoubleRatchetEncryptDecryptRoundTrip tests full encryption pipeline
func TestDoubleRatchetEncryptDecryptRoundTrip(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	// Create sender and receiver states with swapped chains for real communication
	sender, _ := InitializeDoubleRatchetState(rootKey)
	receiver, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	plaintext := []byte("hello world")

	// Sender encrypts
	ciphertext, nonce, err := sender.EncryptMessageWithDoubleRatchet(plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Receiver decrypts
	decrypted, err := receiver.DecryptMessageWithDoubleRatchet(ciphertext, nonce, 0)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted text doesn't match original")
	}
}

// TestDoubleRatchetMultipleMessages tests encryption of multiple messages
func TestDoubleRatchetMultipleMessages(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	sender, _ := InitializeDoubleRatchetState(rootKey)
	receiver, _ := InitializeDoubleRatchetStateAsReceiver(rootKey)

	messages := []string{
		"first message",
		"second message",
		"third message",
		"fourth message",
		"fifth message",
	}

	for idx, msg := range messages {
		plaintext := []byte(msg)

		// Sender encrypts
		ciphertext, nonce, err := sender.EncryptMessageWithDoubleRatchet(plaintext)
		if err != nil {
			t.Fatalf("message %d encryption failed: %v", idx, err)
		}

		// Receiver decrypts
		decrypted, err := receiver.DecryptMessageWithDoubleRatchet(ciphertext, nonce, idx)
		if err != nil {
			t.Fatalf("message %d decryption failed: %v", idx, err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("message %d: decrypted text doesn't match original", idx)
		}
	}

	if sender.SendMessageIndex != 5 {
		t.Errorf("sender should have sent 5 messages, got %d", sender.SendMessageIndex)
	}
	if receiver.RecvMessageIndex != 5 {
		t.Errorf("receiver should have received 5 messages, got %d", receiver.RecvMessageIndex)
	}
}

// TestDoubleRatchetEncryptedContentNotRecoverable tests forward secrecy
// Old message keys should not be recoverable from current chain key
func TestDoubleRatchetEncryptedContentNotRecoverable(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	plaintext := []byte("secret message")

	// Encrypt and save state
	ciphertext, nonce, _ := dr.EncryptMessageWithDoubleRatchet(plaintext)
	oldChainKey := dr.SendChainKey

	// Ratchet forward several more times
	for i := 0; i < 10; i++ {
		dr.RatchetSendMessageKey()
	}

	// Try to decrypt with current chain key (should fail - forward secrecy)
	_, err := dr.DecryptMessageWithDoubleRatchet(ciphertext, nonce, 0)
	if err == nil {
		t.Error("should not be able to decrypt old message with new chain key (forward secrecy failed)")
	}

	// Verify that the old chain key was actually advanced
	if oldChainKey == dr.SendChainKey {
		t.Error("chain key should have advanced")
	}
}

// TestDoubleRatchetDH tests DH ratchet for key rotation
func TestDoubleRatchetDH(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr, _ := InitializeDoubleRatchetState(rootKey)

	oldRootKey := dr.RootKey
	oldChainKey := dr.SendChainKey

	// Generate ephemeral key for DH ratchet
	ephPub, ephPriv, _ := X25519Keypair()

	// Perform DH ratchet
	err := dr.RatchetDH(ephPub, ephPriv)
	if err != nil {
		t.Fatalf("DH ratchet failed: %v", err)
	}

	// Root key should have changed
	if dr.RootKey == oldRootKey {
		t.Error("root key should evolve after DH ratchet")
	}

	// Chain keys should have been reset
	if dr.SendChainKey == oldChainKey {
		t.Error("send chain key should evolve after DH ratchet")
	}

	// Counters should be reset
	if dr.SendMessageIndex != 0 {
		t.Error("send message index should reset after DH ratchet")
	}
	if dr.RecvMessageIndex != 0 {
		t.Error("recv message index should reset after DH ratchet")
	}

	// DH counter should increment
	if dr.DhRatchetCounter != 1 {
		t.Error("DH ratchet counter should increment")
	}
}

// TestDoubleRatchetSessionPersistence tests saving/loading from Session
func TestDoubleRatchetSessionPersistence(t *testing.T) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	dr1, _ := InitializeDoubleRatchetState(rootKey)

	// Advance state
	for i := 0; i < 5; i++ {
		dr1.RatchetSendMessageKey()
	}

	// Save to Session
	session := &Session{
		UserID:          "user1",
		ContactUserID:   "user2",
		ContactDeviceID: "device2",
	}
	UpdateSessionFromDoubleRatchet(session, dr1)

	// Restore from Session
	dr2, err := CreateDoubleRatchetStateFromSession(session)
	if err != nil {
		t.Fatalf("failed to restore from session: %v", err)
	}

	// State should match
	if dr2.RootKey != dr1.RootKey {
		t.Error("root key should match after restore")
	}
	if dr2.SendChainKey != dr1.SendChainKey {
		t.Error("send chain key should match after restore")
	}
	if dr2.SendMessageIndex != dr1.SendMessageIndex {
		t.Error("send message index should match after restore")
	}
}

// TestSkippedMessageKeys tests skipped key store
func TestSkippedMessageKeys(t *testing.T) {
	smk := NewSkippedMessageKeys()

	if smk.Size() != 0 {
		t.Error("new store should be empty")
	}

	// Store keys
	key1 := [32]byte{1}
	key2 := [32]byte{2}
	smk.Store(5, key1)
	smk.Store(10, key2)

	if smk.Size() != 2 {
		t.Error("should have 2 keys stored")
	}

	// Get key 5
	retrieved, exists := smk.Get(5)
	if !exists || retrieved != key1 {
		t.Error("failed to retrieve key 5")
	}

	// Should be deleted after get
	if smk.Size() != 1 {
		t.Error("key should be deleted after Get")
	}

	// Get non-existent key
	_, exists = smk.Get(99)
	if exists {
		t.Error("should not find non-existent key")
	}

	// Clear
	smk.Clear()
	if smk.Size() != 0 {
		t.Error("should be empty after Clear")
	}
}

// BenchmarkDoubleRatchetSendKey benchmarks send key ratcheting
func BenchmarkDoubleRatchetSendKey(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	dr, _ := InitializeDoubleRatchetState(rootKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dr.RatchetSendMessageKey()
	}
}

// BenchmarkDoubleRatchetRecvKey benchmarks receive key ratcheting
func BenchmarkDoubleRatchetRecvKey(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	dr, _ := InitializeDoubleRatchetState(rootKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dr.RatchetRecvMessageKey(i)
	}
}

// BenchmarkDoubleRatchetEncrypt benchmarks message encryption
func BenchmarkDoubleRatchetEncrypt(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	dr, _ := InitializeDoubleRatchetState(rootKey)
	plaintext := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dr.EncryptMessageWithDoubleRatchet(plaintext)
	}
}

// BenchmarkDoubleRatchetDecrypt benchmarks message decryption
func BenchmarkDoubleRatchetDecrypt(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}
	dr, _ := InitializeDoubleRatchetState(rootKey)
	plaintext := make([]byte, 1024)

	ciphertext, nonce, _ := dr.EncryptMessageWithDoubleRatchet(plaintext)

	// Reset for decryption attempts
	dr, _ = InitializeDoubleRatchetState(rootKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dr.DecryptMessageWithDoubleRatchet(ciphertext, nonce, i%100)
	}
}

// BenchmarkDoubleRatchetDH benchmarks DH key rotation
func BenchmarkDoubleRatchetDH(b *testing.B) {
	rootKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		rootKey[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dr, _ := InitializeDoubleRatchetState(rootKey)
		ephPub, ephPriv, _ := X25519Keypair()
		dr.RatchetDH(ephPub, ephPriv)
	}
}
