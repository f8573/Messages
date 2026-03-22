package e2ee

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// TestComputeFingerprint tests fingerprint computation
func TestComputeFingerprint(t *testing.T) {
	// Generate a test Ed25519 keypair
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// Encode public key to base64
	pubKeyBase64 := base64.StdEncoding.EncodeToString(pubKey)

	// Compute fingerprint
	fingerprint, err := ComputeFingerprint(pubKeyBase64)
	if err != nil {
		t.Fatalf("failed to compute fingerprint: %v", err)
	}

	// Verify fingerprint is not empty and is valid hex
	if fingerprint == "" {
		t.Error("fingerprint is empty")
	}

	if len(fingerprint) != 64 { // SHA256 in hex is 64 chars
		t.Errorf("fingerprint length mismatch: expected 64, got %d", len(fingerprint))
	}

	// Test consistency - same key should produce same fingerprint
	fingerprint2, err := ComputeFingerprint(pubKeyBase64)
	if err != nil {
		t.Fatalf("failed to compute fingerprint again: %v", err)
	}

	if fingerprint != fingerprint2 {
		t.Error("fingerprints don't match for same key")
	}
}

// TestVerifySignature tests Ed25519 signature verification
func TestVerifySignature(t *testing.T) {
	// Generate a keypair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	pubKeyBase64 := base64.StdEncoding.EncodeToString(pubKey)

	// Message to sign
	message := []byte("test message")

	// Sign the message
	signature := ed25519.Sign(privKey, message)
	signatureBase64 := base64.StdEncoding.EncodeToString(signature)

	// Verify signature
	valid, err := VerifySignature(pubKeyBase64, message, signatureBase64)
	if err != nil {
		t.Fatalf("failed to verify signature: %v", err)
	}

	if !valid {
		t.Error("signature verification failed")
	}

	// Test with modified message (should fail)
	modifiedMessage := []byte("modified message")
	valid, err = VerifySignature(pubKeyBase64, modifiedMessage, signatureBase64)
	if err != nil {
		t.Fatalf("failed to verify signature with modified message: %v", err)
	}

	if valid {
		t.Error("signature verification should fail for modified message")
	}

	// Test with wrong public key (should fail)
	_, privKey2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate second keypair: %v", err)
	}

	wrongPubKey := privKey2.Public().(ed25519.PublicKey)
	wrongPubKeyBase64 := base64.StdEncoding.EncodeToString(wrongPubKey)

	valid, err = VerifySignature(wrongPubKeyBase64, message, signatureBase64)
	if err != nil {
		t.Fatalf("failed to verify signature with wrong key: %v", err)
	}

	if valid {
		t.Error("signature verification should fail with wrong public key")
	}
}

// TestGenerateSessionKey tests random session key generation
func TestGenerateSessionKey(t *testing.T) {
	key1, err := GenerateSessionKey()
	if err != nil {
		t.Fatalf("failed to generate session key: %v", err)
	}

	if len(key1) != 32 {
		t.Errorf("session key length mismatch: expected 32, got %d", len(key1))
	}

	// Test uniqueness - generate two keys and verify they're different
	key2, err := GenerateSessionKey()
	if err != nil {
		t.Fatalf("failed to generate second session key: %v", err)
	}

	if len(key2) != 32 {
		t.Errorf("session key length mismatch: expected 32, got %d", len(key2))
	}

	// Keys should be different (extremely unlikely to be the same if random)
	same := true
	for i := 0; i < 32; i++ {
		if key1[i] != key2[i] {
			same = false
			break
		}
	}

	if same {
		t.Error("generated session keys are identical (randomness issue)")
	}
}

// TestGenerateNonce tests random nonce generation for AES-GCM
func TestGenerateNonce(t *testing.T) {
	nonce1, err := GenerateNonce()
	if err != nil {
		t.Fatalf("failed to generate nonce: %v", err)
	}

	if len(nonce1) != 12 {
		t.Errorf("nonce length mismatch: expected 12, got %d", len(nonce1))
	}

	// Test uniqueness
	nonce2, err := GenerateNonce()
	if err != nil {
		t.Fatalf("failed to generate second nonce: %v", err)
	}

	if len(nonce2) != 12 {
		t.Errorf("nonce length mismatch: expected 12, got %d", len(nonce2))
	}

	// Nonces should be different
	same := true
	for i := 0; i < 12; i++ {
		if nonce1[i] != nonce2[i] {
			same = false
			break
		}
	}

	if same {
		t.Error("generated nonces are identical (randomness issue)")
	}
}

// TestVerifySignatureErrors tests error handling
func TestVerifySignatureErrors(t *testing.T) {
	tests := []struct {
		name       string
		pubKeyB64  string
		message    []byte
		signatureB64 string
		expectError bool
	}{
		{
			name:        "invalid base64 public key",
			pubKeyB64:   "not-valid-base64!!!",
			message:     []byte("test"),
			signatureB64: "dGVzdA==",
			expectError: true,
		},
		{
			name:        "invalid base64 signature",
			pubKeyB64:   "dGVzdA==", // "test" in base64
			message:     []byte("test"),
			signatureB64: "not-valid-base64!!!",
			expectError: true,
		},
		{
			name:        "wrong public key size",
			pubKeyB64:   base64.StdEncoding.EncodeToString([]byte("short")),
			message:     []byte("test"),
			signatureB64: "dGVzdA==",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifySignature(tt.pubKeyB64, tt.message, tt.signatureB64)
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
		})
	}
}

// TestFingerprintErrors tests error handling for fingerprint
func TestFingerprintErrors(t *testing.T) {
	tests := []struct {
		name        string
		pubKeyB64   string
		expectError bool
	}{
		{
			name:        "invalid base64",
			pubKeyB64:   "not-valid-base64!!!",
			expectError: true,
		},
		{
			name:        "empty string",
			pubKeyB64:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComputeFingerprint(tt.pubKeyB64)
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
		})
	}
}
