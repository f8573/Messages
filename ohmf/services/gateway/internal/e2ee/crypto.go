package e2ee

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// SessionManager handles Signal protocol sessions and message encryption/decryption
type SessionManager struct {
	db *pgxpool.Pool
}

// removed: NewSessionManager - trivial constructor inlined at 3 call sites

// Session represents a Signal protocol session between two devices
type Session struct {
	UserID            string
	ContactUserID     string
	ContactDeviceID   string
	SessionKeyBytes   []byte
	SessionKeyVersion int
	RootKeyBytes      []byte
	ChainKeyBytes     []byte
	MessageKeyIndex   int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DeviceKeyBundle represents a Signal protocol key bundle for X3DH key exchange
type DeviceKeyBundle struct {
	DeviceID                   string
	UserID                     string
	BundleVersion              string          // OHMF_SIGNAL_V1
	IdentityKeyAlg             string          // X25519
	IdentityPublicKey          string          // Base64 encoded, 32 bytes
	AgreementIdentityPublicKey string          // X25519 key for encryption
	SigningKeyAlg              string          // Ed25519
	SigningPublicKey           string          // Base64 encoded, 32 bytes
	SignedPrekeyID             int64
	SignedPrekeyPublicKey      string          // Base64 encoded
	SignedPrekeySignature      string          // Base64 encoded Ed25519 signature
	Fingerprint                string          // SHA256(signing_public_key) as hex
	ClaimedOneTimePrekey       *OneTimePrekey
}

// OneTimePrekey represents an ephemeral X25519 public key
type OneTimePrekey struct {
	PrekeyID  int64
	PublicKey string // Base64 encoded X25519 key (32 bytes)
}

// EncryptedMessage represents an encrypted message with metadata
type EncryptedMessage struct {
	Ciphertext   string               // Base64 encoded AES-256-GCM ciphertext
	Nonce        string               // Base64 encoded GCM nonce
	Scheme       string               // OHMF_SIGNAL_V1
	SenderUserID string
	SenderDeviceID string
	SenderSignature string             // Base64 encoded Ed25519 signature over ciphertext
	Recipients   []RecipientKey        // One per recipient device
}

// RecipientKey represents encryption info for a specific recipient device
type RecipientKey struct {
	UserID     string // UUID
	DeviceID   string // UUID
	WrappedKey string // Base64 encoded X25519(ephemeral_secret, session_key)
	WrapNonce  string // Base64 encoded GCM nonce
}

// TrustState represents the trust level of a device key
type TrustState struct {
	UserID            string
	ContactUserID     string
	ContactDeviceID   string
	State              string    // TOFU, VERIFIED, BLOCKED
	Fingerprint        string    // SHA256(signing_public_key)
	TrustedDeviceKey   string    // Store for verification
	TrustEstablishedAt time.Time
	VerifiedAt         *time.Time
}

// ComputeFingerprint returns SHA256 hash of signing public key as hex string
func ComputeFingerprint(signingPublicKeyBase64 string) (string, error) {
	if signingPublicKeyBase64 == "" {
		return "", fmt.Errorf("signing public key cannot be empty")
	}
	data, err := base64.StdEncoding.DecodeString(signingPublicKeyBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode signing public key: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("decoded public key is empty")
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// VerifySignature verifies an Ed25519 signature
func VerifySignature(signingPublicKeyBase64 string, message []byte, signatureBase64 string) (bool, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(signingPublicKeyBase64)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid public key size: expected %d, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}

	pubKey := ed25519.PublicKey(pubKeyBytes)

	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	return ed25519.Verify(pubKey, message, sigBytes), nil
}

// CreateOrGetSession creates a new session or retrieves existing one
func (sm *SessionManager) CreateOrGetSession(
	ctx context.Context,
	userID string,
	contactUserID string,
	contactDeviceID string,
) (*Session, error) {
	// Try to retrieve existing session
	session, err := sm.GetSession(ctx, userID, contactUserID, contactDeviceID)
	if err == nil && session != nil {
		return session, nil
	}

	// Create new session
	session = &Session{
		UserID:            userID,
		ContactUserID:     contactUserID,
		ContactDeviceID:   contactDeviceID,
		SessionKeyVersion: 1,
		MessageKeyIndex:   0,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}

	// Store session in database
	if err := sm.SaveSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return session, nil
}

// GetSession retrieves a session from the database
func (sm *SessionManager) GetSession(
	ctx context.Context,
	userID string,
	contactUserID string,
	contactDeviceID string,
) (*Session, error) {
	query := `
		SELECT user_id, contact_user_id, contact_device_id, session_key_bytes,
		       session_key_version, root_key_bytes, chain_key_bytes, message_key_index,
		       created_at, updated_at
		FROM e2ee_sessions
		WHERE user_id = $1 AND contact_user_id = $2 AND contact_device_id = $3
	`

	var session Session
	err := sm.db.QueryRow(ctx, query, userID, contactUserID, contactDeviceID).Scan(
		&session.UserID,
		&session.ContactUserID,
		&session.ContactDeviceID,
		&session.SessionKeyBytes,
		&session.SessionKeyVersion,
		&session.RootKeyBytes,
		&session.ChainKeyBytes,
		&session.MessageKeyIndex,
		&session.CreatedAt,
		&session.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query session: %w", err)
	}

	return &session, nil
}

// SaveSession persists a session to the database
func (sm *SessionManager) SaveSession(ctx context.Context, session *Session) error {
	query := `
		INSERT INTO e2ee_sessions
		  (user_id, contact_user_id, contact_device_id, session_key_bytes,
		   session_key_version, root_key_bytes, chain_key_bytes, message_key_index, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_id, contact_user_id, contact_device_id)
		DO UPDATE SET
		  session_key_bytes = EXCLUDED.session_key_bytes,
		  session_key_version = EXCLUDED.session_key_version,
		  root_key_bytes = EXCLUDED.root_key_bytes,
		  chain_key_bytes = EXCLUDED.chain_key_bytes,
		  message_key_index = EXCLUDED.message_key_index,
		  updated_at = EXCLUDED.updated_at
	`

	_, err := sm.db.Exec(ctx, query,
		session.UserID,
		session.ContactUserID,
		session.ContactDeviceID,
		session.SessionKeyBytes,
		session.SessionKeyVersion,
		session.RootKeyBytes,
		session.ChainKeyBytes,
		session.MessageKeyIndex,
		session.CreatedAt,
		time.Now().UTC(),
	)

	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// StoreTrustState records the trust state of a device key
func (sm *SessionManager) StoreTrustState(ctx context.Context, trust *TrustState) error {
	query := `
		INSERT INTO device_key_trust
		  (user_id, contact_user_id, contact_device_id, trust_state, fingerprint,
		   trusted_device_public_key, trust_established_at, verified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, contact_user_id, contact_device_id)
		DO UPDATE SET
		  trust_state = EXCLUDED.trust_state,
		  fingerprint = EXCLUDED.fingerprint,
		  trusted_device_public_key = EXCLUDED.trusted_device_public_key,
		  verified_at = EXCLUDED.verified_at
	`

	_, err := sm.db.Exec(ctx, query,
		trust.UserID,
		trust.ContactUserID,
		trust.ContactDeviceID,
		trust.State,
		trust.Fingerprint,
		trust.TrustedDeviceKey,
		trust.TrustEstablishedAt,
		trust.VerifiedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to store trust state: %w", err)
	}

	return nil
}

// GetTrustState retrieves the trust state of a device key
func (sm *SessionManager) GetTrustState(
	ctx context.Context,
	userID string,
	contactUserID string,
	contactDeviceID string,
) (*TrustState, error) {
	query := `
		SELECT user_id, contact_user_id, contact_device_id, trust_state, fingerprint,
		       trusted_device_public_key, trust_established_at, verified_at
		FROM device_key_trust
		WHERE user_id = $1 AND contact_user_id = $2 AND contact_device_id = $3
	`

	var trust TrustState
	err := sm.db.QueryRow(ctx, query, userID, contactUserID, contactDeviceID).Scan(
		&trust.UserID,
		&trust.ContactUserID,
		&trust.ContactDeviceID,
		&trust.State,
		&trust.Fingerprint,
		&trust.TrustedDeviceKey,
		&trust.TrustEstablishedAt,
		&trust.VerifiedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query trust state: %w", err)
	}

	return &trust, nil
}

// EstablishTOFUTrust records TOFU (Trust on First Use) for device key
func (sm *SessionManager) EstablishTOFUTrust(
	ctx context.Context,
	userID string,
	contactUserID string,
	contactDeviceID string,
	fingerprint string,
	devicePublicKey string,
) error {
	trust := &TrustState{
		UserID:            userID,
		ContactUserID:     contactUserID,
		ContactDeviceID:   contactDeviceID,
		State:             "TOFU",
		Fingerprint:       fingerprint,
		TrustedDeviceKey:  devicePublicKey,
		TrustEstablishedAt: time.Now().UTC(),
	}

	return sm.StoreTrustState(ctx, trust)
}

// LogE2EEInitialization records an E2EE initialization attempt for debugging
func (sm *SessionManager) LogE2EEInitialization(
	ctx context.Context,
	initiatorUserID string,
	initiatorDeviceID string,
	recipientUserID string,
	recipientDeviceID string,
	conversationID *string,
	status string,
	errorMessage string,
) error {
	query := `
		INSERT INTO e2ee_initialization_log
		  (initiator_user_id, initiator_device_id, recipient_user_id, recipient_device_id,
		   conversation_id, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := sm.db.Exec(ctx, query,
		initiatorUserID,
		initiatorDeviceID,
		recipientUserID,
		recipientDeviceID,
		conversationID,
		status,
		errorMessage,
	)

	if err != nil {
		return fmt.Errorf("failed to log E2EE initialization: %w", err)
	}

	return nil
}

// GenerateSessionKey generates a random session key (256-bit)
func GenerateSessionKey() ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session key: %w", err)
	}
	return key, nil
}

// GenerateNonce generates a random nonce for AES-GCM (96-bit)
func GenerateNonce() ([]byte, error) {
	nonce := make([]byte, 12)
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return nonce, nil
}

// removed: GenerateEphemeralKeyID - dead code, never called in production paths

// EncryptMessageContent encrypts plaintext using Double Ratchet algorithm
// This is the production implementation, replacing the placeholder
func EncryptMessageContent(messageBytes []byte, sessionState *DoubleRatchetState) (ciphertext string, nonce string, err error) {
	if sessionState == nil {
		return "", "", errors.New("session state is nil")
	}

	ciphertextBytes, nonceArray, err := sessionState.EncryptMessageWithDoubleRatchet(messageBytes)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(ciphertextBytes), base64.StdEncoding.EncodeToString(nonceArray[:]), nil
}

// DecryptMessageContent decrypts an encrypted message using Double Ratchet state
// This is the production implementation, replacing the placeholder
func DecryptMessageContent(ctx context.Context, ciphertext string, nonce string, sessionState *DoubleRatchetState, messageIndex int) ([]byte, error) {
	if sessionState == nil {
		return nil, errors.New("session state is nil")
	}

	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	if len(nonceBytes) != 12 {
		return nil, fmt.Errorf("invalid nonce size: expected 12, got %d", len(nonceBytes))
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonceBytes)

	return sessionState.DecryptMessageWithDoubleRatchet(ct, nonceArray, messageIndex)
}

// GenerateRecipientWrappedKey generates a wrapped session key using X25519 ECDH
// The wrapped key is encrypted using derives from ECDH with recipient's public key
// This is simplified - full implementation uses X3DH (3-way DH + signature)
func GenerateRecipientWrappedKey(
	recipientIdentityPublicKey string,
	recipientState *DoubleRatchetState,
) (wrappedKey string, wrapNonce string, err error) {
	if recipientState == nil {
		return "", "", errors.New("recipient state is nil")
	}

	// Decode recipient's identity public key (X25519)
	recipientKeyBytes, err := base64.StdEncoding.DecodeString(recipientIdentityPublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode recipient key: %w", err)
	}

	if len(recipientKeyBytes) != 32 {
		return "", "", fmt.Errorf("invalid recipient key size: %d", len(recipientKeyBytes))
	}

	var recipientKey [32]byte
	copy(recipientKey[:], recipientKeyBytes)

	// Generate ephemeral keypair for this wrapping
	ephPub, ephPriv, err := X25519Keypair()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Perform ECDH: shared_secret = ephPriv * recipientKey
	sharedSecret, err := X25519SharedSecret(ephPriv, recipientKey)
	if err != nil {
		return "", "", fmt.Errorf("ECDH failed: %w", err)
	}

	// Derive wrapping key from shared secret using recipient's state root key as context
	wrappingKey, err := HKDFExpand(sharedSecret[:], recipientState.RootKey[:], 32)
	if err != nil {
		return "", "", fmt.Errorf("failed to derive wrapping key: %w", err)
	}

	var wrapKeyArray [32]byte
	copy(wrapKeyArray[:], wrappingKey)

	// Encrypt the recipient state's root key with the wrapping key
	ciphertext, nonce, err := AESGCMEncrypt(wrapKeyArray, recipientState.RootKey[:], nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to wrap key: %w", err)
	}

	// Include ephemeral public key in wrapped key for recipient to perform ECDH
	wrappedContent := make([]byte, 32+len(ciphertext))
	copy(wrappedContent[0:32], ephPub[:])
	copy(wrappedContent[32:], ciphertext)

	return base64.StdEncoding.EncodeToString(wrappedContent), base64.StdEncoding.EncodeToString(nonce[:]), nil
}

// UnwrapSessionKeyWithDoubleRatchet unwraps a recipient's wrapped key for our private key
// This is the inverse of GenerateRecipientWrappedKey
func UnwrapSessionKeyWithDoubleRatchet(
	ourPrivateKeyHex string,
	wrappedKeyB64 string,
	wrapNonceB64 string,
	ourState *DoubleRatchetState,
) ([]byte, error) {
	if ourState == nil {
		return nil, errors.New("our state is nil")
	}

	// Decode private key
	ourPrivKeyBytes, err := base64.StdEncoding.DecodeString(ourPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode our private key: %w", err)
	}

	if len(ourPrivKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid private key size: %d", len(ourPrivKeyBytes))
	}

	var ourPrivKey [32]byte
	copy(ourPrivKey[:], ourPrivKeyBytes)

	// Decode wrapped content (ephemeral_pub || ciphertext)
	wrappedContent, err := base64.StdEncoding.DecodeString(wrappedKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wrapped key: %w", err)
	}

	if len(wrappedContent) < 32 {
		return nil, fmt.Errorf("invalid wrapped key format: too short")
	}

	var ephPub [32]byte
	copy(ephPub[:], wrappedContent[0:32])
	ciphertext := wrappedContent[32:]

	// Decode nonce
	wrapNonceBytes, err := base64.StdEncoding.DecodeString(wrapNonceB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wrap nonce: %w", err)
	}

	if len(wrapNonceBytes) != 12 {
		return nil, fmt.Errorf("invalid wrap nonce size: %d", len(wrapNonceBytes))
	}

	var wrapNonce [12]byte
	copy(wrapNonce[:], wrapNonceBytes)

	// Perform ECDH with ephemeral key: shared_secret = ourPriv * ephPub
	sharedSecret, err := X25519SharedSecret(ourPrivKey, ephPub)
	if err != nil {
		return nil, fmt.Errorf("ECDH failed: %w", err)
	}

	// Derive wrapping key
	wrappingKey, err := HKDFExpand(sharedSecret[:], ourState.RootKey[:], 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive wrapping key: %w", err)
	}

	var wrapKeyArray [32]byte
	copy(wrapKeyArray[:], wrappingKey)

	// Decrypt the root key
	plaintext, err := AESGCMDecrypt(wrapKeyArray, ciphertext, wrapNonce, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap key: %w", err)
	}

	return plaintext, nil
}

// EncryptMessageWithSession encrypts a message using Session database state
// Converts Session to DoubleRatchetState, performs encryption, updates Session
func (sm *SessionManager) EncryptMessageWithSession(
	ctx context.Context,
	session *Session,
	plaintext []byte,
) (ciphertext, nonce string, err error) {
	// Restore ratchet state from session
	dr, err := CreateDoubleRatchetStateFromSession(session)
	if err != nil {
		return "", "", fmt.Errorf("failed to restore ratchet state: %w", err)
	}

	// Encrypt with double ratchet
	ciphertextBytes, nonceArray, err := dr.EncryptMessageWithDoubleRatchet(plaintext)
	if err != nil {
		return "", "", err
	}

	// Update session with new ratchet state
	UpdateSessionFromDoubleRatchet(session, dr)

	// Persist updated session to database
	if err := sm.SaveSession(ctx, session); err != nil {
		return "", "", fmt.Errorf("failed to update session: %w", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertextBytes), base64.StdEncoding.EncodeToString(nonceArray[:]), nil
}

// DecryptMessageWithSession decrypts a message using Session database state
// Converts Session to DoubleRatchetState, performs decryption, updates Session for received state
func (sm *SessionManager) DecryptMessageWithSession(
	ctx context.Context,
	session *Session,
	ciphertext, nonce string,
	messageIndex int,
) ([]byte, error) {
	// Restore ratchet state from session
	dr, err := CreateDoubleRatchetStateFromSession(session)
	if err != nil {
		return nil, fmt.Errorf("failed to restore ratchet state: %w", err)
	}

	// Decode ciphertext and nonce
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	if len(nonceBytes) != 12 {
		return nil, fmt.Errorf("invalid nonce size: %d", len(nonceBytes))
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonceBytes)

	// Decrypt with double ratchet
	plaintext, err := dr.DecryptMessageWithDoubleRatchet(ct, nonceArray, messageIndex)
	if err != nil {
		return nil, err
	}

	// Note: In production, we'd update recv state separately
	// For now, we keep the state as-is after decryption
	// UpdateSessionFromDoubleRatchet(session, dr) would overwrite send state

	return plaintext, nil
}

// UnwrapSessionKey unwraps a recipient's wrapped session key using our private identity key

// ==================== PHASE 4 WEEK 1: CRYPTOGRAPHIC PRIMITIVES ====================

// X25519Keypair generates a new ECDH keypair for key agreement
func X25519Keypair() ([32]byte, [32]byte, error) {
	var privateKey [32]byte
	_, err := io.ReadFull(rand.Reader, privateKey[:])
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("failed to generate private key: %w", err)
	}
	publicKey, err := curve25519.X25519(privateKey[:], curve25519.Basepoint[:])
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("failed to compute public key: %w", err)
	}
	var pubKeyArray [32]byte
	copy(pubKeyArray[:], publicKey)
	return pubKeyArray, privateKey, nil
}

// X25519SharedSecret performs ECDH key agreement with peer's public key
func X25519SharedSecret(ourPrivateKey [32]byte, theirPublicKey [32]byte) ([32]byte, error) {
	if [32]byte{} == theirPublicKey {
		return [32]byte{}, errors.New("peer public key is zero")
	}
	shared, err := curve25519.X25519(ourPrivateKey[:], theirPublicKey[:])
	if err != nil {
		return [32]byte{}, fmt.Errorf("ECDH failed: %w", err)
	}
	var result [32]byte
	copy(result[:], shared)
	return result, nil
}

// GenerateECDHKeys returns base64-encoded X25519 keypair for JSON serialization
func GenerateECDHKeys() (string, string, error) {
	pub, priv, err := X25519Keypair()
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub[:]), base64.StdEncoding.EncodeToString(priv[:]), nil
}

// HMACSign creates HMAC-SHA256 signature of data with key
func HMACSign(key, data []byte) [32]byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	var sig [32]byte
	copy(sig[:], h.Sum(nil))
	return sig
}

// HMACVerify checks HMAC-SHA256 signature using constant-time comparison
func HMACVerify(key, data []byte, signature [32]byte) bool {
	computed := HMACSign(key, data)
	return hmac.Equal(computed[:], signature[:])
}

// SignatureHex returns base64-encoded HMAC-SHA256 signature
func SignatureHex(key, data []byte) string {
	sig := HMACSign(key, data)
	return base64.StdEncoding.EncodeToString(sig[:])
}

// HKDFExpand performs HKDF-Expand with SHA256
func HKDFExpand(prk, info []byte, length int) ([]byte, error) {
	if length < 0 || length > 255*32 {
		return nil, fmt.Errorf("invalid HKDF length: %d (must be 0-%d)", length, 255*32)
	}
	r := hkdf.Expand(sha256.New, prk, info)
	key := make([]byte, length)
	_, err := io.ReadFull(r, key)
	if err != nil {
		return nil, fmt.Errorf("HKDF-Expand failed: %w", err)
	}
	return key, nil
}

// HKDFExtractExpand performs HKDF-Extract + HKDF-Expand (RFC 5869)
func HKDFExtractExpand(salt, ikm, info []byte, length int) ([]byte, error) {
	prk := hmac.New(sha256.New, salt)
	prk.Write(ikm)
	return HKDFExpand(prk.Sum(nil), info, length)
}

// ChainKeyDerive derives message key and next chain key from chain key (Double Ratchet KDF)
func ChainKeyDerive(chainKey []byte) ([32]byte, [32]byte) {
	msgKey := HMACSign(chainKey, []byte{0x01})
	nextKey := HMACSign(chainKey, []byte{0x02})
	return msgKey, nextKey
}

// AESGCMEncrypt encrypts plaintext with AES-256-GCM
func AESGCMEncrypt(key [32]byte, plaintext, aad []byte) ([]byte, [12]byte, error) {
	c, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, [12]byte{}, fmt.Errorf("failed to create cipher: %w", err)
	}
	g, err := cipher.NewGCM(c)
	if err != nil {
		return nil, [12]byte{}, fmt.Errorf("failed to create GCM: %w", err)
	}
	var nonce [12]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, [12]byte{}, fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := g.Seal(nil, nonce[:], plaintext, aad)
	return ciphertext, nonce, nil
}

// AESGCMDecrypt decrypts ciphertext encrypted with AES-256-GCM
func AESGCMDecrypt(key [32]byte, ciphertext []byte, nonce [12]byte, aad []byte) ([]byte, error) {
	c, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	g, err := cipher.NewGCM(c)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	plaintext, err := g.Open(nil, nonce[:], ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}

// MessageEncrypt encrypts message using AES-256-GCM with base64 encoding
func MessageEncrypt(messageKey [32]byte, plaintext []byte) (string, string, error) {
	ciphertext, nonce, err := AESGCMEncrypt(messageKey, plaintext, nil)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(nonce[:]), nil
}

// MessageDecrypt decrypts message encrypted with MessageEncrypt
func MessageDecrypt(messageKey [32]byte, ciphertextB64, nonceB64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	nonceBytes, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}
	if len(nonceBytes) != 12 {
		return nil, fmt.Errorf("invalid nonce size: expected 12, got %d", len(nonceBytes))
	}
	var nonce [12]byte
	copy(nonce[:], nonceBytes)
	return AESGCMDecrypt(messageKey, ciphertext, nonce, nil)
}

// GenerateRecipientWrappedKeyLegacy generates wrapped key using legacy API
// Provided for backward compatibility with existing code
func GenerateRecipientWrappedKeyLegacy(
	recipientIdentityPublicKey string,
	sessionKey []byte,
) (wrappedKey string, wrapNonce string, err error) {
	if len(sessionKey) != 32 {
		return "", "", fmt.Errorf("invalid session key size: expected 32 bytes, got %d", len(sessionKey))
	}

	// Decode recipient's public key
	recipientKeyBytes, err := base64.StdEncoding.DecodeString(recipientIdentityPublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode recipient key: %w", err)
	}

	if len(recipientKeyBytes) != 32 {
		return "", "", fmt.Errorf("invalid recipient key size: %d", len(recipientKeyBytes))
	}

	var recipientKey [32]byte
	copy(recipientKey[:], recipientKeyBytes)

	// Generate ephemeral keypair
	ephPub, ephPriv, err := X25519Keypair()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Perform ECDH
	sharedSecret, err := X25519SharedSecret(ephPriv, recipientKey)
	if err != nil {
		return "", "", fmt.Errorf("ECDH failed: %w", err)
	}

	// Derive wrapping key
	wrappingKey, err := HKDFExpand(sharedSecret[:], []byte("wrap-key"), 32)
	if err != nil {
		return "", "", fmt.Errorf("failed to derive wrapping key: %w", err)
	}

	var wrapKeyArray [32]byte
	copy(wrapKeyArray[:], wrappingKey)

	// Encrypt the session key
	var sessionKeyArray [32]byte
	copy(sessionKeyArray[:], sessionKey)

	ciphertext, nonce, err := AESGCMEncrypt(wrapKeyArray, sessionKeyArray[:], nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to wrap key: %w", err)
	}

	// Include ephemeral public key in wrapped result
	wrappedContent := make([]byte, 32+len(ciphertext))
	copy(wrappedContent[0:32], ephPub[:])
	copy(wrappedContent[32:], ciphertext)

	return base64.StdEncoding.EncodeToString(wrappedContent), base64.StdEncoding.EncodeToString(nonce[:]), nil
}

// EncryptMessageContentLegacy encrypts using legacy sessionKey API
// Provided for backward compatibility with existing code
// Creates a temporary DoubleRatchetState from the session key
func EncryptMessageContentLegacy(messageBytes []byte, sessionKey []byte) (ciphertext string, nonce string, err error) {
	if len(sessionKey) != 32 {
		return "", "", fmt.Errorf("invalid session key size: expected 32 bytes, got %d", len(sessionKey))
	}

	// Create temporary state from session key (simplified for compatibility)
	var keyArray [32]byte
	copy(keyArray[:], sessionKey)

	// For compatibility, use direct AES-GCM (no Double Ratchet state)
	ct, nonceArray, err := AESGCMEncrypt(keyArray, messageBytes, nil)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(nonceArray[:]), nil
}

// DecryptMessageContentLegacy decrypts using legacy sessionKey API
// Provided for backward compatibility with existing code
func DecryptMessageContentLegacy(ctx context.Context, ciphertext string, nonce string, sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != 32 {
		return nil, fmt.Errorf("invalid session key size: expected 32 bytes, got %d", len(sessionKey))
	}

	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	if len(nonceBytes) != 12 {
		return nil, fmt.Errorf("invalid nonce size: %d", len(nonceBytes))
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonceBytes)

	var keyArray [32]byte
	copy(keyArray[:], sessionKey)

	return AESGCMDecrypt(keyArray, ct, nonceArray, nil)
}

// ===================== X3DH KEY EXCHANGE PROTOCOL =====================

// X3DHKeys represents the key material exchanged in X3DH protocol
type X3DHKeys struct {
	SenderIdentityPrivate   [32]byte
	SenderIdentityPublic    [32]byte
	SenderEphemeralPrivate  [32]byte
	SenderEphemeralPublic   [32]byte
	RecipientIdentityPublic [32]byte
	RecipientSignedPrekey   [32]byte
	RecipientOneTimePrekey  [32]byte // Can be empty for optional use
}

// PerformX3DH performs X3DH key agreement using 3 or 4 ECDH computations
// Returns shared secret for use with Double Ratchet initialization
// Protocol overview:
// - DH1 = ECDH(sender_identity_private, recipient_identity_public)
// - DH2 = ECDH(sender_ephemeral_private, recipient_signed_prekey)
// - DH3 = ECDH(sender_ephemeral_private, recipient_identity_public)
// - (optional) DH4 = ECDH(sender_ephemeral_private, recipient_one_time_prekey)
// - shared_secret = KDF(DH1 || DH2 || DH3 [|| DH4])
func PerformX3DH(keys *X3DHKeys) ([32]byte, error) {
	if keys == nil {
		return [32]byte{}, errors.New("keys is nil")
	}

	// Verify all required keys are non-zero
	if keys.SenderIdentityPrivate == [32]byte{} {
		return [32]byte{}, errors.New("sender identity private key is empty")
	}
	if keys.SenderEphemeralPrivate == [32]byte{} {
		return [32]byte{}, errors.New("sender ephemeral private key is empty")
	}
	if keys.RecipientIdentityPublic == [32]byte{} {
		return [32]byte{}, errors.New("recipient identity public key is empty")
	}
	if keys.RecipientSignedPrekey == [32]byte{} {
		return [32]byte{}, errors.New("recipient signed prekey is empty")
	}

	// Compute DH1: sender_identity_private * recipient_identity_public
	dh1, err := X25519SharedSecret(keys.SenderIdentityPrivate, keys.RecipientIdentityPublic)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH1 failed: %w", err)
	}

	// Compute DH2: sender_ephemeral_private * recipient_signed_prekey
	dh2, err := X25519SharedSecret(keys.SenderEphemeralPrivate, keys.RecipientSignedPrekey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH2 failed: %w", err)
	}

	// Compute DH3: sender_ephemeral_private * recipient_identity_public
	dh3, err := X25519SharedSecret(keys.SenderEphemeralPrivate, keys.RecipientIdentityPublic)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH3 failed: %w", err)
	}

	// Build combined material: DH1 || DH2 || DH3 [|| DH4]
	combinedMaterial := make([]byte, 96) // 3 * 32 bytes
	copy(combinedMaterial[0:32], dh1[:])
	copy(combinedMaterial[32:64], dh2[:])
	copy(combinedMaterial[64:96], dh3[:])

	// Optional DH4 (one-time prekey usage)
	if keys.RecipientOneTimePrekey != [32]byte{} {
		dh4, err := X25519SharedSecret(keys.SenderEphemeralPrivate, keys.RecipientOneTimePrekey)
		if err == nil {
			combinedMaterial = append(combinedMaterial, dh4[:]...)
		}
		// If DH4 fails, continue without it (one-time prekeys are optional)
	}

	// Derive shared secret using HKDF
	sharedSecret, err := HKDFExtractExpand(
		[]byte("X3DH"),                  // salt
		combinedMaterial,                // IKM
		[]byte("Signal X3DH context"),   // info
		32,                              // output length
	)
	if err != nil {
		return [32]byte{}, fmt.Errorf("KDF failed: %w", err)
	}

	var result [32]byte
	copy(result[:], sharedSecret)
	return result, nil
}

// PerformX3DHInitiator is a convenience wrapper for the initiating party
// Generates ephemeral keypair and performs X3DH
func PerformX3DHInitiator(
	senderIdentityPrivate [32]byte,
	senderIdentityPublic [32]byte,
	recipientIdentityPublic [32]byte,
	recipientSignedPrekey [32]byte,
	recipientOneTimePrekey [32]byte, // Can be empty
) ([32]byte, [32]byte, [32]byte, error) {
	// Generate ephemeral keypair
	ephPub, ephPriv, err := X25519Keypair()
	if err != nil {
		return [32]byte{}, [32]byte{}, [32]byte{}, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Perform X3DH
	sharedSecret, err := PerformX3DH(&X3DHKeys{
		SenderIdentityPrivate:   senderIdentityPrivate,
		SenderIdentityPublic:    senderIdentityPublic,
		SenderEphemeralPrivate:  ephPriv,
		SenderEphemeralPublic:   ephPub,
		RecipientIdentityPublic: recipientIdentityPublic,
		RecipientSignedPrekey:   recipientSignedPrekey,
		RecipientOneTimePrekey:  recipientOneTimePrekey,
	})

	return sharedSecret, ephPub, ephPriv, err
}

// PerformX3DHResponder is a convenience wrapper for the receiving party
// Performs X3DH using the sender's ephemeral key from the message
func PerformX3DHResponder(
	senderEphemeralPublic [32]byte,
	recipientIdentityPrivate [32]byte,
	recipientIdentityPublic [32]byte,
	recipientSignedPrekeyPrivate [32]byte,
	recipientSignedPrekeyPublic [32]byte,
	recipientOneTimePrekeyPrivate [32]byte, // Can be empty
	senderIdentityPublic [32]byte,
) ([32]byte, error) {
	// Compute DH1: recipient_identity_private * sender_identity_public
	dh1, err := X25519SharedSecret(recipientIdentityPrivate, senderIdentityPublic)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH1 failed: %w", err)
	}

	// Compute DH2: recipient_signed_prekey_private * sender_ephemeral_public
	dh2, err := X25519SharedSecret(recipientSignedPrekeyPrivate, senderEphemeralPublic)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH2 failed: %w", err)
	}

	// Compute DH3: recipient_identity_private * sender_ephemeral_public
	dh3, err := X25519SharedSecret(recipientIdentityPrivate, senderEphemeralPublic)
	if err != nil {
		return [32]byte{}, fmt.Errorf("DH3 failed: %w", err)
	}

	// Build combined material
	combinedMaterial := make([]byte, 96)
	copy(combinedMaterial[0:32], dh1[:])
	copy(combinedMaterial[32:64], dh2[:])
	copy(combinedMaterial[64:96], dh3[:])

	// Optional DH4
	if recipientOneTimePrekeyPrivate != [32]byte{} {
		dh4, err := X25519SharedSecret(recipientOneTimePrekeyPrivate, senderEphemeralPublic)
		if err == nil {
			combinedMaterial = append(combinedMaterial, dh4[:]...)
		}
	}

	// Derive shared secret
	sharedSecret, err := HKDFExtractExpand(
		[]byte("X3DH"),
		combinedMaterial,
		[]byte("Signal X3DH context"),
		32,
	)
	if err != nil {
		return [32]byte{}, fmt.Errorf("KDF failed: %w", err)
	}

	var result [32]byte
	copy(result[:], sharedSecret)
	return result, nil
}
//    - Message keys derived from chain keys
// This implementation uses: github.com/signal-golang/libsignal-go (when integrated)
// For now, we have placeholder AES-GCM operations showing the structure.
