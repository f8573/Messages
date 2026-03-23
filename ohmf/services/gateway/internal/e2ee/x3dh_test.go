package e2ee

import (
	"bytes"
	"testing"
)

// TestX3DHBasicAgreement tests X3DH key agreement produces same secret
func TestX3DHBasicAgreement(t *testing.T) {
	// Alice's keys
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	aliceEphPub, aliceEphPriv, _ := X25519Keypair()

	// Bob's keys
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()
	bobOTPPub, bobOTPPriv, _ := X25519Keypair()

	// Alice initiates X3DH
	aliceShared, err := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   aliceIdentPriv,
		SenderIdentityPublic:    aliceIdentPub,
		SenderEphemeralPrivate:  aliceEphPriv,
		SenderEphemeralPublic:   aliceEphPub,
		RecipientIdentityPublic: bobIdentPub,
		RecipientSignedPrekey:   bobSPKPub,
		RecipientOneTimePrekey:  bobOTPPub,
	})
	if err != nil {
		t.Fatalf("Alice X3DH failed: %v", err)
	}

	// Bob receives ephemeral key and computes X3DH
	bobShared, err := PerformX3DHResponder(
		aliceEphPub,
		bobIdentPriv,
		bobIdentPub,
		bobSPKPriv,
		bobSPKPub,
		bobOTPPriv,
		aliceIdentPub,
	)
	if err != nil {
		t.Fatalf("Bob X3DH failed: %v", err)
	}

	// Both should compute same shared secret
	if aliceShared != bobShared {
		t.Error("X3DH failed: initiator and responder computed different secrets")
	}
}

// TestX3DHWithoutOneTimePrekey tests X3DH without optional one-time prekey
func TestX3DHWithoutOneTimePrekey(t *testing.T) {
	// Alice's keys
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	aliceEphPub, aliceEphPriv, _ := X25519Keypair()

	// Bob's keys (without one-time prekey)
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()

	// Alice initiates without one-time prekey
	aliceShared, err := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   aliceIdentPriv,
		SenderIdentityPublic:    aliceIdentPub,
		SenderEphemeralPrivate:  aliceEphPriv,
		SenderEphemeralPublic:   aliceEphPub,
		RecipientIdentityPublic: bobIdentPub,
		RecipientSignedPrekey:   bobSPKPub,
		RecipientOneTimePrekey:  [32]byte{}, // Empty
	})
	if err != nil {
		t.Fatalf("Alice X3DH failed: %v", err)
	}

	// Bob responds without one-time prekey
	bobShared, err := PerformX3DHResponder(
		aliceEphPub,
		bobIdentPriv,
		bobIdentPub,
		bobSPKPriv,
		bobSPKPub,
		[32]byte{}, // Empty
		aliceIdentPub,
	)
	if err != nil {
		t.Fatalf("Bob X3DH failed: %v", err)
	}

	if aliceShared != bobShared {
		t.Error("X3DH without OTP failed: secrets don't match")
	}
}

// TestX3DHInitiatorWrapper tests the initiator convenience function
func TestX3DHInitiatorWrapper(t *testing.T) {
	// Alice's keys
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()

	// Bob's keys
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

	// Use initiator wrapper
	sharedSecret, ephPub, ephPriv, err := PerformX3DHInitiator(
		aliceIdentPriv,
		aliceIdentPub,
		bobIdentPub,
		bobSPKPub,
		[32]byte{}, // No OTP
	)
	if err != nil {
		t.Fatalf("X3DHInitiator failed: %v", err)
	}

	if sharedSecret == [32]byte{} {
		t.Error("shared secret should not be zero")
	}
	if ephPub == [32]byte{} {
		t.Error("ephemeral public key should not be zero")
	}
	if ephPriv == [32]byte{} {
		t.Error("ephemeral private key should not be zero")
	}

	// Ephemeral keys should be valid X25519 keypair
	testSecret, err := X25519SharedSecret(ephPriv, bobIdentPub)
	if err != nil || testSecret == [32]byte{} {
		t.Error("ephemeral keypair should be valid")
	}
}

// TestX3DHResponderWrapper tests the responder convenience function
func TestX3DHResponderWrapper(t *testing.T) {
	// Alice initiates
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	aliceEphPub, aliceEphPriv, _ := X25519Keypair()

	// Bob's keys
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()

	// Bob responds
	bobShared, err := PerformX3DHResponder(
		aliceEphPub,
		bobIdentPriv,
		bobIdentPub,
		bobSPKPriv,
		bobSPKPub,
		[32]byte{},
		aliceIdentPub,
	)
	if err != nil {
		t.Fatalf("X3DHResponder failed: %v", err)
	}

	if bobShared == [32]byte{} {
		t.Error("shared secret should not be zero")
	}

	// Compare with Alice's computation
	aliceShared, _ := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   aliceIdentPriv,
		SenderIdentityPublic:    aliceIdentPub,
		SenderEphemeralPrivate:  aliceEphPriv,
		SenderEphemeralPublic:   aliceEphPub,
		RecipientIdentityPublic: bobIdentPub,
		RecipientSignedPrekey:   bobSPKPub,
	})

	if aliceShared != bobShared {
		t.Error("Alice and Bob should compute same shared secret")
	}
}

// TestX3DHMutualAuthentication tests X3DH provides mutual authentication
// If either party's identity key is different, shared secret will differ
func TestX3DHMutualAuthentication(t *testing.T) {
	// Alice's legitimate keys
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	aliceEphPub, aliceEphPriv, _ := X25519Keypair()

	// Bob's keys
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

	// Legitimate X3DH
	legitimateShared, _ := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   aliceIdentPriv,
		SenderIdentityPublic:    aliceIdentPub,
		SenderEphemeralPrivate:  aliceEphPriv,
		SenderEphemeralPublic:   aliceEphPub,
		RecipientIdentityPublic: bobIdentPub,
		RecipientSignedPrekey:   bobSPKPub,
	})

	// Attacker tries with forged identity
	attackerIdentPub, attackerIdentPriv, _ := X25519Keypair()
	forgedShared, _ := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   attackerIdentPriv,
		SenderIdentityPublic:    attackerIdentPub,
		SenderEphemeralPrivate:  aliceEphPriv,     // Same ephemeral
		SenderEphemeralPublic:   aliceEphPub,
		RecipientIdentityPublic: bobIdentPub,
		RecipientSignedPrekey:   bobSPKPub,
	})

	if legitimateShared == forgedShared {
		t.Error("forged identity should produce different shared secret")
	}
}

// TestX3DHWithDoubleRatchetInitialization tests X3DH output can initialize Double Ratchet
func TestX3DHWithDoubleRatchetInitialization(t *testing.T) {
	// Perform X3DH
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

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

	// Use X3DH shared secret to initialize Double Ratchet
	dr, err := InitializeDoubleRatchetState(sharedSecret[:])
	if err != nil {
		t.Fatalf("Double Ratchet initialization failed: %v", err)
	}

	// Double Ratchet should be valid and capable of encrypting
	plaintext := []byte("test message")
	ciphertext, nonce, err := dr.EncryptMessageWithDoubleRatchet(plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if len(ciphertext) == 0 || nonce == [12]byte{} {
		t.Error("encryption should produce output")
	}
}

// TestX3DHFullProtocolFlow tests end-to-end X3DH + Double Ratchet flow
func TestX3DHFullProtocolFlow(t *testing.T) {
	// === SETUP PHASE ===
	// Alice generates identity keys
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()

	// Bob generates bundle: identity + signed prekey + one-time prekeys
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()
	bobOTPPub, bobOTPPriv, _ := X25519Keypair()

	// === INITIATION PHASE ===
	// Alice retrieves Bob's key bundle and initiates
	sharedSecret, ephPub, _, err := PerformX3DHInitiator(
		aliceIdentPriv,
		aliceIdentPub,
		bobIdentPub,
		bobSPKPub,
		bobOTPPub,
	)
	if err != nil {
		t.Fatalf("Alice X3DH failed: %v", err)
	}

	// Alice initializes Double Ratchet for sending
	aliceDR, _ := InitializeDoubleRatchetState(sharedSecret[:])

	// === RECIPIENT PHASE ===
	// Bob receives ephemeral key and computes shared secret
	bobShared, _ := PerformX3DHResponder(
		ephPub,
		bobIdentPriv,
		bobIdentPub,
		bobSPKPriv,
		bobSPKPub,
		bobOTPPriv,
		aliceIdentPub,
	)

	// Bob initializes Double Ratchet for receiving
	bobDR, _ := InitializeDoubleRatchetStateAsReceiver(bobShared[:])

	// === MESSAGING PHASE ===
	// Alice sends 3 messages
	messages := []string{"hello bob", "how are you", "goodbye"}
	for idx, msg := range messages {
		ct, nonce, err := aliceDR.EncryptMessageWithDoubleRatchet([]byte(msg))
		if err != nil {
			t.Fatalf("Alice encryption %d failed: %v", idx, err)
		}

		// Bob receives and decrypts
		plaintext, err := bobDR.DecryptMessageWithDoubleRatchet(ct, nonce, idx)
		if err != nil {
			t.Fatalf("Bob decryption %d failed: %v", idx, err)
		}

		if !bytes.Equal(plaintext, []byte(msg)) {
			t.Errorf("message %d decryption failed", idx)
		}
	}

	// Both should have advanced their state
	if aliceDR.SendMessageIndex != 3 {
		t.Error("Alice should have sent 3 messages")
	}
	if bobDR.RecvMessageIndex != 3 {
		t.Error("Bob should have received 3 messages")
	}
}

// TestX3DHDeterministicSecret tests that same inputs produce same secret
func TestX3DHDeterministicSecret(t *testing.T) {
	// Use fixed key material
	keys := &X3DHKeys{
		SenderIdentityPrivate:   [32]byte{1, 2, 3, 4, 5},
		SenderIdentityPublic:    [32]byte{10, 11, 12},
		SenderEphemeralPrivate:  [32]byte{20, 21, 22},
		SenderEphemeralPublic:   [32]byte{30, 31, 32},
		RecipientIdentityPublic: [32]byte{40, 41, 42},
		RecipientSignedPrekey:   [32]byte{50, 51, 52},
	}

	// First call - should fail due to invalid key format but establish baseline
	first, errFirst := PerformX3DH(keys)
	second, errSecond := PerformX3DH(keys)

	if (errFirst == nil && errSecond == nil) && first != second {
		t.Error("deterministic: same inputs should produce same secret")
	}
	if (errFirst != nil && errSecond != nil) {
		// Both failed with same error - that's consistent at least
		if errFirst.Error() != errSecond.Error() {
			t.Error("errors should be identical")
		}
	}
}

// TestX3DHErrorHandling tests error conditions
func TestX3DHErrorHandling(t *testing.T) {
	// Nil keys
	_, err := PerformX3DH(nil)
	if err == nil {
		t.Error("should reject nil keys")
	}

	// Empty sender identity
	_, err = PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   [32]byte{},
		SenderEphemeralPrivate:  [32]byte{1},
		RecipientIdentityPublic: [32]byte{1},
		RecipientSignedPrekey:   [32]byte{1},
	})
	if err == nil {
		t.Error("should reject empty sender identity")
	}

	// Empty ephemeral
	_, err = PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   [32]byte{1},
		SenderEphemeralPrivate:  [32]byte{},
		RecipientIdentityPublic: [32]byte{1},
		RecipientSignedPrekey:   [32]byte{1},
	})
	if err == nil {
		t.Error("should reject empty ephemeral key")
	}
}

// BenchmarkX3DH benchmarks X3DH key agreement
func BenchmarkX3DH(b *testing.B) {
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	aliceEphPub, aliceEphPriv, _ := X25519Keypair()
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PerformX3DH(&X3DHKeys{
			SenderIdentityPrivate:   aliceIdentPriv,
			SenderIdentityPublic:    aliceIdentPub,
			SenderEphemeralPrivate:  aliceEphPriv,
			SenderEphemeralPublic:   aliceEphPub,
			RecipientIdentityPublic: bobIdentPub,
			RecipientSignedPrekey:   bobSPKPub,
		})
	}
}

// BenchmarkX3DHInitiator benchmarks the initiator wrapper including ephemeral key generation
func BenchmarkX3DHInitiator(b *testing.B) {
	aliceIdentPub, aliceIdentPriv, _ := X25519Keypair()
	bobIdentPub, _, _ := X25519Keypair()
	bobSPKPub, _, _ := X25519Keypair()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PerformX3DHInitiator(
			aliceIdentPriv,
			aliceIdentPub,
			bobIdentPub,
			bobSPKPub,
			[32]byte{},
		)
	}
}

// BenchmarkX3DHResponder benchmarks the responder computation
func BenchmarkX3DHResponder(b *testing.B) {
	aliceIdentPub, _, _ := X25519Keypair()
	aliceEphPub, _, _ := X25519Keypair()
	bobIdentPub, bobIdentPriv, _ := X25519Keypair()
	bobSPKPub, bobSPKPriv, _ := X25519Keypair()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PerformX3DHResponder(
			aliceEphPub,
			bobIdentPriv,
			bobIdentPub,
			bobSPKPriv,
			bobSPKPub,
			[32]byte{},
			aliceIdentPub,
		)
	}
}
