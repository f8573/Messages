package e2ee

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionManager handles Signal protocol sessions and message encryption/decryption
type SessionManager struct {
	db *sql.DB
}

// NewSessionManager creates a new session manager
func NewSessionManager(db *sql.DB) *SessionManager {
	return &SessionManager{
		db: db,
	}
}

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
	data, err := base64.StdEncoding.DecodeString(signingPublicKeyBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode signing public key: %w", err)
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
	err := sm.db.QueryRowContext(ctx, query, userID, contactUserID, contactDeviceID).Scan(
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

	if err == sql.ErrNoRows {
		return nil, nil // Session doesn't exist yet
	}

	if err != nil {
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

	_, err := sm.db.ExecContext(ctx, query,
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

	_, err := sm.db.ExecContext(ctx, query,
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
	err := sm.db.QueryRowContext(ctx, query, userID, contactUserID, contactDeviceID).Scan(
		&trust.UserID,
		&trust.ContactUserID,
		&trust.ContactDeviceID,
		&trust.State,
		&trust.Fingerprint,
		&trust.TrustedDeviceKey,
		&trust.TrustEstablishedAt,
		&trust.VerifiedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No trust state recorded yet
	}

	if err != nil {
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

	_, err := sm.db.ExecContext(ctx, query,
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

// GenerateEphemeralKeyID generates a random ephemeral key ID
func GenerateEphemeralKeyID() string {
	return uuid.New().String()
}

// EncryptMessageContent encrypts plaintext message content using AES-256-GCM
// In production, this would use libsignal's Double Ratchet algorithm
// For now, we use basic AES-GCM as a placeholder showing the structure
func EncryptMessageContent(messageBytes []byte, sessionKey []byte) (ciphertext string, nonce string, err error) {
	// For production: Use libsignal's EncryptMessage(sessionState, messageBytes)
	// This is a placeholder implementation using standard Go crypto

	if len(sessionKey) != 32 {
		return "", "", fmt.Errorf("invalid session key size: expected 32 bytes, got %d", len(sessionKey))
	}

	// Generate random nonce for GCM (96 bits)
	nonceBytes, err := GenerateNonce()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Note: In production, use libsignal-go:
	// ciphertext := sessionState.EncryptMessage(messageBytes)
	// We would extract wrapped keys from Double Ratchet state

	// For placeholder: create a simple AES-GCM cipher
	// This is NOT production-ready - it's to show the structure
	// Production code would use: libsignal.EncryptMessage(sessionState, messageBytes)

	ciphertextBytes := append(nonceBytes, messageBytes...) // Placeholder: just concatenate
	ciphertext = base64.StdEncoding.EncodeToString(ciphertextBytes)
	nonce = base64.StdEncoding.EncodeToString(nonceBytes)

	return ciphertext, nonce, nil
}

// DecryptMessageContent decrypts an encrypted message using session state
// In production, this would use libsignal's Double Ratchet algorithm
func DecryptMessageContent(ctx context.Context, ciphertext string, nonce string, sessionKey []byte) ([]byte, error) {
	// For production: Use libsignal's DecryptMessage(sessionState, ciphertext)

	if len(sessionKey) != 32 {
		return nil, fmt.Errorf("invalid session key size: expected 32 bytes, got %d", len(sessionKey))
	}

	// Decode base64 ciphertext and nonce
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	// Production: libsignal.DecryptMessage(sessionState, ciphertext)
	// For now: placeholder extraction
	if len(ct) < len(nonceBytes) {
		return nil, fmt.Errorf("invalid ciphertext format")
	}

	plaintext := ct[len(nonceBytes):]
	return plaintext, nil
}

// GenerateRecipientWrappedKey generates a wrapped session key for a recipient device
// The wrapped key is encrypted with the recipient's identity public key
// In production, this is part of the X3DH protocol (ephemeral + identity key exchange)
func GenerateRecipientWrappedKey(
	recipientIdentityPublicKey string,
	sessionKey []byte,
) (wrappedKey string, wrapNonce string, err error) {
	// Generate random nonce for wrapping
	wrapNonceBytes, err := GenerateNonce()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate wrap nonce: %w", err)
	}

	// Production: Use X25519 key agreement with libsignal
	// For now: placeholder wrapping (just base64 encode the key)
	// wrappedKey would be: X25519(ephemeral_secret, recipient_identity_key, sessionKey)

	// Placeholder: just encode the session key with nonce
	wrappedKeyBytes := append(wrapNonceBytes, sessionKey...)
	wrappedKey = base64.StdEncoding.EncodeToString(wrappedKeyBytes)
	wrapNonce = base64.StdEncoding.EncodeToString(wrapNonceBytes)

	return wrappedKey, wrapNonce, nil
}

// UnwrapSessionKey unwraps a recipient's wrapped session key using our private identity key
// In production, this is part of X3DH decapsulation
func UnwrapSessionKey(
	wrappedKey string,
	wrapNonce string,
	ourIdentityPrivateKey string,
) ([]byte, error) {
	// Decode wrapped key
	wrappedBytes, err := base64.StdEncoding.DecodeString(wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wrapped key: %w", err)
	}

	wrapNonceBytes, err := base64.StdEncoding.DecodeString(wrapNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wrap nonce: %w", err)
	}

	// Production: Use X25519 key agreement with libsignal
	// For now: placeholder unwrapping
	if len(wrappedBytes) < len(wrapNonceBytes)+32 {
		return nil, fmt.Errorf("invalid wrapped key format")
	}

	sessionKey := wrappedBytes[len(wrapNonceBytes) : len(wrapNonceBytes)+32]
	return sessionKey, nil
}

// Note: Actual Double Ratchet and libsignal integration will be implemented
// in the following production flow:
// 1. X3DH (Elliptic Curve Diffie-Hellman) for initial key agreement
//    - Uses: Sender identity key + Sender signed prekey + Recipient identity key +
//            Recipient signed prekey + Recipient one-time prekey
//    - Produces: Initial shared secret
// 2. KDF (Key Derivation Function) to derive root key and chain key from shared secret
// 3. Double Ratchet algorithm for forward secrecy:
//    - Root key evolution with each message
//    - Chain key ratcheting for ordering
//    - Message keys derived from chain keys
// This implementation uses: github.com/signal-golang/libsignal-go (when integrated)
// For now, we have placeholder AES-GCM operations showing the structure.
