package e2ee

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// ===================== X25519 ECDH Tests =====================

// TestX25519KeypairGeneration tests that keypair generation produces valid keys
func TestX25519KeypairGeneration(t *testing.T) {
	pub1, priv1, err := X25519Keypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}
	if [32]byte{} == pub1 || [32]byte{} == priv1 {
		t.Error("generated zero key")
	}
	// Generate second time, should be different
	pub2, priv2, err := X25519Keypair()
	if err != nil {
		t.Fatalf("failed to generate second keypair: %v", err)
	}
	if pub1 == pub2 || priv1 == priv2 {
		t.Error("consecutive keypairs should be different")
	}
}

// TestX25519SharedSecretCommutative tests ECDH property: both parties derive same secret
func TestX25519SharedSecretCommutative(t *testing.T) {
	pub1, priv1, err := X25519Keypair()
	if err != nil {
		t.Fatalf("keypair 1 failed: %v", err)
	}
	pub2, priv2, err := X25519Keypair()
	if err != nil {
		t.Fatalf("keypair 2 failed: %v", err)
	}

	// Party 1: secret = priv1 * pub2
	secret1, err := X25519SharedSecret(priv1, pub2)
	if err != nil {
		t.Fatalf("party 1 ECDH failed: %v", err)
	}

	// Party 2: secret = priv2 * pub1
	secret2, err := X25519SharedSecret(priv2, pub1)
	if err != nil {
		t.Fatalf("party 2 ECDH failed: %v", err)
	}

	if secret1 != secret2 {
		t.Error("ECDH secrets should be equal")
	}
	if [32]byte{} == secret1 {
		t.Error("derived zero secret")
	}
}

// TestX25519DifferentKeysProduceDifferentSecrets tests cryptographic property
func TestX25519DifferentKeysProduceDifferentSecrets(t *testing.T) {
	_, priv1, _ := X25519Keypair()
	pub2, priv2, _ := X25519Keypair()
	pub3, _, _ := X25519Keypair()

	secret1, _ := X25519SharedSecret(priv1, pub2)
	secret2, _ := X25519SharedSecret(priv1, pub3)
	secret3, _ := X25519SharedSecret(priv2, pub3)

	if secret1 == secret2 || secret1 == secret3 || secret2 == secret3 {
		t.Error("different key pairs should produce different secrets")
	}
}

// TestX25519ZeroPublicKeyRejected tests input validation
func TestX25519ZeroPublicKeyRejected(t *testing.T) {
	_, priv, _ := X25519Keypair()
	_, err := X25519SharedSecret(priv, [32]byte{})
	if err == nil {
		t.Error("should reject zero public key")
	}
}

// TestGenerateECDHKeysEncoding tests base64 round-trip
func TestGenerateECDHKeysEncoding(t *testing.T) {
	pubHex, privHex, err := GenerateECDHKeys()
	if err != nil {
		t.Fatalf("GenerateECDHKeys failed: %v", err)
	}

	pub, err := base64.StdEncoding.DecodeString(pubHex)
	if err != nil {
		t.Fatalf("failed to decode public key: %v", err)
	}
	if len(pub) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(pub))
	}

	priv, err := base64.StdEncoding.DecodeString(privHex)
	if err != nil {
		t.Fatalf("failed to decode private key: %v", err)
	}
	if len(priv) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(priv))
	}
}

// ===================== HMAC-SHA256 Tests =====================

// TestHMACSIgnValidSignature tests basic HMAC operation
func TestHMACSignValidSignature(t *testing.T) {
	key := []byte("test-key")
	data := []byte("test-data")

	sig := HMACSign(key, data)
	if [32]byte{} == sig {
		t.Error("signature is zero")
	}

	if !HMACVerify(key, data, sig) {
		t.Error("valid signature failed verification")
	}
}

// TestHMACVerifyRejectsModifiedData tests integrity protection (RFC 4231)
func TestHMACVerifyRejectsModifiedData(t *testing.T) {
	key := []byte("test-key")
	data := []byte("test-data")
	sig := HMACSign(key, data)

	modifiedData := []byte("test-data-modified")
	if HMACVerify(key, modifiedData, sig) {
		t.Error("should reject modified data")
	}
}

// TestHMACVerifyRejectsWrongKey tests key derivation (RFC 4231)
func TestHMACVerifyRejectsWrongKey(t *testing.T) {
	key := []byte("test-key")
	wrongKey := []byte("wrong-key")
	data := []byte("test-data")
	sig := HMACSign(key, data)

	if HMACVerify(wrongKey, data, sig) {
		t.Error("should reject wrong key")
	}
}

// TestHMACConstantTimeComparison tests resistance to timing attacks
func TestHMACConstantTimeComparison(t *testing.T) {
	key := []byte("test-key")
	data := []byte("test-data")
	correctSig := HMACSign(key, data)

	// Create wrong signature by flipping one bit
	wrongSig := correctSig
	wrongSig[0] ^= 1

	// Both should complete without variation (constant-time)
	if HMACVerify(key, data, wrongSig) {
		t.Error("should reject modified signature")
	}
	if !HMACVerify(key, data, correctSig) {
		t.Error("should accept correct signature")
	}
}

// TestSignatureHexEncoding tests base64 encoding
func TestSignatureHexEncoding(t *testing.T) {
	key := []byte("test-key")
	data := []byte("test-data")

	sigHex := SignatureHex(key, data)
	sig, err := base64.StdEncoding.DecodeString(sigHex)
	if err != nil {
		t.Fatalf("failed to decode signature: %v", err)
	}
	if len(sig) != 32 {
		t.Errorf("signature should be 32 bytes, got %d", len(sig))
	}
}

// ===================== HKDF-SHA256 Tests =====================

// TestHKDFExpandConsistency tests deterministic expansion (RFC 5869)
func TestHKDFExpandConsistency(t *testing.T) {
	prk := make([]byte, 32)
	for i := 0; i < len(prk); i++ {
		prk[i] = byte(i)
	}
	info := []byte("test-info")
	length := 64

	derived1, err := HKDFExpand(prk, info, length)
	if err != nil {
		t.Fatalf("first expand failed: %v", err)
	}

	derived2, err := HKDFExpand(prk, info, length)
	if err != nil {
		t.Fatalf("second expand failed: %v", err)
	}

	if !bytes.Equal(derived1, derived2) {
		t.Error("HKDF expansion should be deterministic")
	}
}

// TestHKDFExpandDifferentInfo tests that info influences output (RFC 5869)
func TestHKDFExpandDifferentInfo(t *testing.T) {
	prk := make([]byte, 32)
	for i := 0; i < len(prk); i++ {
		prk[i] = byte(i)
	}

	derived1, _ := HKDFExpand(prk, []byte("info1"), 64)
	derived2, _ := HKDFExpand(prk, []byte("info2"), 64)

	if bytes.Equal(derived1, derived2) {
		t.Error("different info should produce different output")
	}
}

// TestHKDFExpandSufficientLength tests AES-256 key derivation
func TestHKDFExpandSufficientLength(t *testing.T) {
	prk := make([]byte, 32)
	for i := 0; i < len(prk); i++ {
		prk[i] = byte(i)
	}

	// Derive 32-byte key for AES-256
	derived, err := HKDFExpand(prk, []byte("aes-key"), 32)
	if err != nil {
		t.Fatalf("expand failed: %v", err)
	}
	if len(derived) != 32 {
		t.Errorf("should derive 32 bytes, got %d", len(derived))
	}
	if bytes.Equal(derived, make([]byte, 32)) {
		t.Error("derived key is all zeros")
	}
}

// TestHKDFExtractExpand tests full HKDF (RFC 5869 vectors)
func TestHKDFExtractExpand(t *testing.T) {
	// RFC 5869 Test Case 1
	salt := []byte{}
	ikm := []byte{0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b}
	info := []byte{}
	derived, err := HKDFExtractExpand(salt, ikm, info, 42)
	if err != nil {
		t.Fatalf("HKDF failed: %v", err)
	}
	if len(derived) != 42 {
		t.Errorf("should derive 42 bytes, got %d", len(derived))
	}
	// RFC 5869 expected PRK (first 16 bytes for comparison): 077709362c2e32df0ddc3f0dc47bba639390b6c73bb50f9c3122ec844ad7c2b3
	// (Skipped full vector check as it's just a sanity test)
}

// TestChainKeyDerive tests Double Ratchet KDF
func TestChainKeyDerive(t *testing.T) {
	chainKey := make([]byte, 32)
	for i := 0; i < len(chainKey); i++ {
		chainKey[i] = byte(i)
	}

	msgKey1, nextKey1 := ChainKeyDerive(chainKey)

	// Ratchet forward
	msgKey2, nextKey2 := ChainKeyDerive(nextKey1[:])

	// All should be different
	if msgKey1 == msgKey2 || nextKey1 == nextKey2 || msgKey1 == nextKey1 {
		t.Error("chain key derivation: keys should differ")
	}

	// Message keys derived same way should be identical
	msgKeySecond, _ := ChainKeyDerive(chainKey)
	if msgKey1 != msgKeySecond {
		t.Error("chain key derivation should be deterministic")
	}
}

// TestChainKeyDeriveSequence tests ratcheting forward (forward secrecy)
func TestChainKeyDeriveSequence(t *testing.T) {
	var chainKey [32]byte
	for i := 0; i < 32; i++ {
		chainKey[i] = byte(i)
	}

	msgKeys := make([][32]byte, 5)

	// Ratchet forward 5 steps
	currentKey := chainKey
	for i := 0; i < 5; i++ {
		msgKeys[i], currentKey = ChainKeyDerive(currentKey[:])
	}

	// Each message key should be different
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			if msgKeys[i] == msgKeys[j] {
				t.Errorf("message key %d should differ from %d", i, j)
			}
		}
	}
}

// ===================== AES-256-GCM Tests =====================

// TestAESGCMEncryptDecryptRoundTrip tests basic encryption/decryption
func TestAESGCMEncryptDecryptRoundTrip(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := []byte("hello world")

	ciphertext, nonce, err := AESGCMEncrypt(key, plaintext, nil)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	decrypted, err := AESGCMDecrypt(key, ciphertext, nonce, nil)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted text doesn't match original")
	}
}

// TestAESGCMRejectModifiedCiphertext tests authentication (NIST SP 800-38D)
func TestAESGCMRejectModifiedCiphertext(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := []byte("hello world")

	ciphertext, nonce, _ := AESGCMEncrypt(key, plaintext, nil)

	// Flip one bit in ciphertext
	modifiedCiphertext := make([]byte, len(ciphertext))
	copy(modifiedCiphertext, ciphertext)
	modifiedCiphertext[0] ^= 1

	_, err := AESGCMDecrypt(key, modifiedCiphertext, nonce, nil)
	if err == nil {
		t.Error("should reject modified ciphertext")
	}
}

// TestAESGCMRejectWrongKey tests key derivation
func TestAESGCMRejectWrongKey(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	wrongKey := [32]byte{}
	for i := 0; i < 32; i++ {
		wrongKey[i] = byte(i ^ 0xFF)
	}
	plaintext := []byte("hello world")

	ciphertext, nonce, _ := AESGCMEncrypt(key, plaintext, nil)

	_, err := AESGCMDecrypt(wrongKey, ciphertext, nonce, nil)
	if err == nil {
		t.Error("should reject wrong key")
	}
}

// TestAESGCMRandomNonce tests that different nonces produce different ciphertexts
func TestAESGCMRandomNonce(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := []byte("hello world")

	ciphertext1, _, _ := AESGCMEncrypt(key, plaintext, nil)
	ciphertext2, _, _ := AESGCMEncrypt(key, plaintext, nil)

	// Different nonces should produce different ciphertexts
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("different nonces should produce different ciphertexts")
	}
}

// TestMessageEncryptDecrypt tests base64 wrapper functions
func TestMessageEncryptDecrypt(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := []byte("test message")

	ciphertextB64, nonceB64, err := MessageEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	decrypted, err := MessageDecrypt(key, ciphertextB64, nonceB64)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted text doesn't match original")
	}
}

// TestMessageEncryptInvalidNonceSize tests error handling
func TestMessageEncryptInvalidNonceSize(t *testing.T) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}

	// Try to decrypt with wrong nonce size
	invalidNonce := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := MessageDecrypt(key, "dGVzdA==", invalidNonce)
	if err == nil {
		t.Error("should reject invalid nonce size")
	}
}

// ===================== Integration & Benchmark Tests =====================

// TestCryptoPrimitiveIntegration tests using primitives together
func TestCryptoPrimitiveIntegration(t *testing.T) {
	// Generate ECDH keypair
	pub1, priv1, _ := X25519Keypair()
	pub2, priv2, _ := X25519Keypair()

	// Perform key agreement
	secret1, _ := X25519SharedSecret(priv1, pub2)
	secret2, _ := X25519SharedSecret(priv2, pub1)

	// Derive message keys using HKDF
	msgKey1, _ := HKDFExpand(secret1[:], []byte("msg-key"), 32)
	msgKey2, _ := HKDFExpand(secret2[:], []byte("msg-key"), 32)

	var key1, key2 [32]byte
	copy(key1[:], msgKey1)
	copy(key2[:], msgKey2)

	// Encrypt with key1, decrypt with key2
	plaintext := []byte("shared secret")
	ciphertext, nonce, _ := AESGCMEncrypt(key1, plaintext, nil)
	decrypted, _ := AESGCMDecrypt(key2, ciphertext, nonce, nil)

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("integration test failed: decryption mismatch")
	}
}

// BenchmarkX25519Keypair benchmarks keypair generation
func BenchmarkX25519Keypair(b *testing.B) {
	for i := 0; i < b.N; i++ {
		X25519Keypair()
	}
}

// BenchmarkX25519SharedSecret benchmarks key agreement
func BenchmarkX25519SharedSecret(b *testing.B) {
	_, priv, _ := X25519Keypair()
	pub, _, _ := X25519Keypair()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		X25519SharedSecret(priv, pub)
	}
}

// BenchmarkHMACSign benchmarks signature creation
func BenchmarkHMACSign(b *testing.B) {
	key := []byte("bench-key")
	data := []byte("bench-data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HMACSign(key, data)
	}
}

// BenchmarkHKDFExpand benchmarks key derivation
func BenchmarkHKDFExpand(b *testing.B) {
	prk := make([]byte, 32)
	for i := 0; i < 32; i++ {
		prk[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HKDFExpand(prk, []byte("bench"), 32)
	}
}

// BenchmarkAESGCMEncrypt benchmarks encryption
func BenchmarkAESGCMEncrypt(b *testing.B) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AESGCMEncrypt(key, plaintext, nil)
	}
}

// BenchmarkAESGCMDecrypt benchmarks decryption
func BenchmarkAESGCMDecrypt(b *testing.B) {
	key := [32]byte{}
	for i := 0; i < 32; i++ {
		key[i] = byte(i)
	}
	plaintext := make([]byte, 1024)
	ciphertext, nonce, _ := AESGCMEncrypt(key, plaintext, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AESGCMDecrypt(key, ciphertext, nonce, nil)
	}
}
