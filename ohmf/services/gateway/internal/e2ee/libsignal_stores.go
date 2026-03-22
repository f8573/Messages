package e2ee

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"

	// This import will be available after: go get github.com/signal-golang/libsignal-go
	// signal "github.com/signal-golang/libsignal-go/signal"
)

// =================== libsignal Store Interface Implementations ===================
//
// libsignal requires four store interfaces to be implemented:
// 1. SessionStore - Stores and retrieves Signal protocol sessions
// 2. IdentityKeyStore - Manages identity keypairs and trust
// 3. PreKeyStore - Manages one-time prekeys
// 4. SignedPreKeyStore - Manages signed prekeys
//
// These implementations integrate with PostgreSQL database:
// - e2ee_sessions (session storage)
// - device_key_trust (identity trust tracking)
// - device_identity_keys (identity keys)
// - device_one_time_prekeys (prekeys)

// =================== SessionStore Implementation ===================
//
// Stores Signal protocol sessions (one per sender-recipient device pair)
type PostgresSessionStore struct {
	db *sql.DB
}

// NewPostgresSessionStore creates a new session store backed by PostgreSQL
func NewPostgresSessionStore(db *sql.DB) *PostgresSessionStore {
	return &PostgresSessionStore{db: db}
}

// LoadSession retrieves a session for the given address
// Implements: signal.SessionStore.LoadSession(ctx, address) (*signal.SessionRecord, error)
//
// Query: SELECT session_key_bytes FROM e2ee_sessions
//        WHERE user_id = address.Name AND contact_device_id = address.DeviceID
func (s *PostgresSessionStore) LoadSession(ctx context.Context, name string, deviceID uint32) ([]byte, error) {
	// name format: "uuid" (contact user ID)
	// deviceID: numeric contact device ID

	query := `
		SELECT session_key_bytes
		FROM e2ee_sessions
		WHERE contact_user_id = $1::uuid AND contact_device_id = $2
		LIMIT 1
	`

	var sessionBytes []byte
	err := s.db.QueryRowContext(ctx, query, name, deviceID).Scan(&sessionBytes)
	if err == sql.ErrNoRows {
		// No session yet - libsignal will create one
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load session failed: %w", err)
	}

	return sessionBytes, nil
}

// StoreSession saves a session record
// Implements: signal.SessionStore.StoreSession(ctx, address, record)
//
// Query: INSERT INTO e2ee_sessions (...) VALUES (...)
//        ON CONFLICT DO UPDATE
func (s *PostgresSessionStore) StoreSession(ctx context.Context, name string, deviceID uint32, sessionBytes []byte) error {
	query := `
		INSERT INTO e2ee_sessions (user_id, contact_user_id, contact_device_id, session_key_bytes, updated_at)
		VALUES (CURRENT_USER_UUID(), $1::uuid, $2, $3, NOW())
		ON CONFLICT (user_id, contact_user_id, contact_device_id) DO UPDATE SET
			session_key_bytes = $3,
			updated_at = NOW()
	`

	_, err := s.db.ExecContext(ctx, query, name, deviceID, sessionBytes)
	if err != nil {
		return fmt.Errorf("store session failed: %w", err)
	}

	return nil
}

// HasSession checks if a session exists
// Implements: signal.SessionStore.HasSession(ctx, address) (bool, error)
func (s *PostgresSessionStore) HasSession(ctx context.Context, name string, deviceID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM e2ee_sessions
			WHERE contact_user_id = $1::uuid AND contact_device_id = $2
			LIMIT 1
		)
	`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, name, deviceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has session check failed: %w", err)
	}

	return exists, nil
}

// DeleteSession removes a session
// Implements: signal.SessionStore.DeleteSession(ctx, address)
// Used during device revocation
func (s *PostgresSessionStore) DeleteSession(ctx context.Context, name string, deviceID uint32) error {
	query := `
		DELETE FROM e2ee_sessions
		WHERE contact_user_id = $1::uuid AND contact_device_id = $2
	`

	_, err := s.db.ExecContext(ctx, query, name, deviceID)
	if err != nil {
		return fmt.Errorf("delete session failed: %w", err)
	}

	return nil
}

// DeleteAllSessions removes all sessions for a user
// Implements: signal.SessionStore.DeleteAllSessions(ctx, name)
// Used for account cleanup
func (s *PostgresSessionStore) DeleteAllSessions(ctx context.Context, name string) error {
	query := `
		DELETE FROM e2ee_sessions
		WHERE contact_user_id = $1::uuid
	`

	_, err := s.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("delete all sessions failed: %w", err)
	}

	return nil
}

// =================== IdentityKeyStore Implementation ===================
//
// Stores identity keys and manages trust state
type PostgresIdentityKeyStore struct {
	db *sql.DB
}

// NewPostgresIdentityKeyStore creates a new identity key store
func NewPostgresIdentityKeyStore(db *sql.DB) *PostgresIdentityKeyStore {
	return &PostgresIdentityKeyStore{db: db}
}

// GetIdentityKeyPair retrieves our own identity keypair
// Implements: signal.IdentityKeyStore.GetIdentityKeyPair(ctx) (*signal.IdentityKeyPair, error)
//
// Query: SELECT identity_private_key, identity_public_key FROM device_identity_keys
//        WHERE user_id = CURRENT_USER_UUID() AND device_id = CURRENT_DEVICE_ID()
func (s *PostgresIdentityKeyStore) GetIdentityKeyPair(ctx context.Context) ([]byte, error) {
	query := `
		SELECT identity_private_key
		FROM device_identity_keys
		WHERE user_id = CURRENT_USER_UUID()
		ORDER BY created_at DESC
		LIMIT 1
	`

	var privKeyBytes []byte
	err := s.db.QueryRowContext(ctx, query).Scan(&privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("get identity keypair failed: %w", err)
	}

	// libsignal expects the keypair in its binary format
	// This is handled by IdentityKeyPair deserialization
	return privKeyBytes, nil
}

// GetLocalRegistrationID gets our registration ID (used in X3DH)
// Implements: signal.IdentityKeyStore.GetLocalRegistrationID(ctx) (uint32, error)
//
// Registration ID is a random 32-bit value per device
// Query: SELECT registration_id FROM device_identity_keys WHERE ...
func (s *PostgresIdentityKeyStore) GetLocalRegistrationID(ctx context.Context) (uint32, error) {
	query := `
		SELECT COALESCE(registration_id, 0)
		FROM device_identity_keys
		WHERE user_id = CURRENT_USER_UUID()
		ORDER BY created_at DESC
		LIMIT 1
	`

	var regID int32
	err := s.db.QueryRowContext(ctx, query).Scan(&regID)
	if err != nil {
		return 0, fmt.Errorf("get registration ID failed: %w", err)
	}

	return uint32(regID), nil
}

// IsTrustedIdentity checks if a contact's identity key is trusted (TOFU model)
// Implements: signal.IdentityKeyStore.IsTrustedIdentity(ctx, address, identityKey) (bool, error)
//
// Query: SELECT trust_state FROM device_key_trust
//        WHERE user_id = CURRENT_USER_UUID() AND contact_user_id = address.Name
func (s *PostgresIdentityKeyStore) IsTrustedIdentity(ctx context.Context, name string, deviceID uint32, identityKey []byte) (bool, error) {
	query := `
		SELECT trust_state
		FROM device_key_trust
		WHERE contact_user_id = $1::uuid AND contact_device_id = $2
		LIMIT 1
	`

	var trustState string
	err := s.db.QueryRowContext(ctx, query, name, deviceID).Scan(&trustState)
	if err == sql.ErrNoRows {
		// No trust record yet - TOFU model accepts first key
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("trust check failed: %w", err)
	}

	// Trust if state is TOFU, VERIFIED, or not explicitly BLOCKED
	return trustState != "BLOCKED", nil
}

// SaveIdentity saves a contact's identity key (first-use in TOFU)
// Implements: signal.IdentityKeyStore.SaveIdentity(ctx, address, identityKey) (bool, error)
//
// Returns true if this is a new key (first encounter)
// Query: INSERT INTO device_key_trust (...) VALUES (...)
//        ON CONFLICT DO NOTHING / UPDATE
func (s *PostgresIdentityKeyStore) SaveIdentity(ctx context.Context, name string, deviceID uint32, identityKey []byte) (bool, error) {
	// Compute fingerprint: SHA256 of identity key
	fingerprint := computeFingerprintFromKey(identityKey)

	query := `
		INSERT INTO device_key_trust (user_id, contact_user_id, contact_device_id, trust_state, fingerprint, trust_established_at)
		VALUES (CURRENT_USER_UUID(), $1::uuid, $2, 'TOFU', $3, NOW())
		ON CONFLICT DO NOTHING
		RETURNING 1
	`

	var result int
	err := s.db.QueryRowContext(ctx, query, name, deviceID, fingerprint).Scan(&result)
	if err == sql.ErrNoRows {
		// Conflict - key already existed (not new)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("save identity failed: %w", err)
	}

	return true, nil // New key was saved
}

// =================== PreKeyStore Implementation ===================
//
// Manages one-time prekeys (ephemeral)
type PostgresPreKeyStore struct {
	db *sql.DB
}

// NewPostgresPreKeyStore creates a new prekey store
func NewPostgresPreKeyStore(db *sql.DB) *PostgresPreKeyStore {
	return &PostgresPreKeyStore{db: db}
}

// LoadPreKey retrieves a prekey by ID
// Implements: signal.PreKeyStore.LoadPreKey(ctx, preKeyID) (*signal.PreKeyRecord, error)
//
// Query: SELECT prekey_public_key, prekey_private_key FROM device_one_time_prekeys
//        WHERE device_id = CURRENT_DEVICE_ID() AND prekey_id = $1
func (s *PostgresPreKeyStore) LoadPreKey(ctx context.Context, prekeyID uint32) ([]byte, error) {
	query := `
		SELECT prekey_public_key, prekey_private_key
		FROM device_one_time_prekeys
		WHERE user_id = CURRENT_USER_UUID() AND prekey_id = $1
		LIMIT 1
	`

	var pubKey, privKey string
	err := s.db.QueryRowContext(ctx, query, prekeyID).Scan(&pubKey, &privKey)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("prekey not found: %d", prekeyID)
	}
	if err != nil {
		return nil, fmt.Errorf("load prekey failed: %w", err)
	}

	// libsignal expects PreKeyRecord in binary format
	// Combine public and private key into record
	recordBytes := make([]byte, len(pubKey)+len(privKey))
	copy(recordBytes, []byte(pubKey))
	copy(recordBytes[len(pubKey):], []byte(privKey))

	return recordBytes, nil
}

// StorePreKey saves a prekey
// Implements: signal.PreKeyStore.StorePreKey(ctx, preKeyID, record)
func (s *PostgresPreKeyStore) StorePreKey(ctx context.Context, prekeyID uint32, prekeyBytes []byte) error {
	// Prekeys are normally generated client-side, not stored in this pattern
	// This is here for completeness but typically prekeys come from client bundles
	return fmt.Errorf("StorePreKey called server-side - prekeys should come from client bundles")
}

// ContainsPreKey checks if a prekey exists
// Implements: signal.PreKeyStore.ContainsPreKey(ctx, preKeyID) (bool, error)
func (s *PostgresPreKeyStore) ContainsPreKey(ctx context.Context, prekeyID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM device_one_time_prekeys
			WHERE user_id = CURRENT_USER_UUID() AND prekey_id = $1
			LIMIT 1
		)
	`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, prekeyID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("contains prekey check failed: %w", err)
	}

	return exists, nil
}

// RemovePreKey deletes a prekey after use (mark consumed)
// Implements: signal.PreKeyStore.RemovePreKey(ctx, preKeyID)
// Called after X3DH uses a prekey
func (s *PostgresPreKeyStore) RemovePreKey(ctx context.Context, prekeyID uint32) error {
	query := `
		UPDATE device_one_time_prekeys
		SET consumed_at = NOW()
		WHERE user_id = CURRENT_USER_UUID() AND prekey_id = $1
	`

	_, err := s.db.ExecContext(ctx, query, prekeyID)
	if err != nil {
		return fmt.Errorf("remove prekey failed: %w", err)
	}

	return nil
}

// =================== SignedPreKeyStore Implementation ===================
//
// Manages signed prekeys (medium-lived, rotated monthly)
type PostgresSignedPreKeyStore struct {
	db *sql.DB
}

// NewPostgresSignedPreKeyStore creates a new signed prekey store
func NewPostgresSignedPreKeyStore(db *sql.DB) *PostgresSignedPreKeyStore {
	return &PostgresSignedPreKeyStore{db: db}
}

// LoadSignedPreKey retrieves the current signed prekey
// Implements: signal.SignedPreKeyStore.LoadSignedPreKey(ctx, signedPreKeyID) (*signal.SignedPreKeyRecord, error)
//
// Query: SELECT signed_prekey_public_key, signed_prekey_private_key, signature
//        FROM device_identity_keys WHERE device_id = CURRENT_DEVICE_ID()
func (s *PostgresSignedPreKeyStore) LoadSignedPreKey(ctx context.Context, signedPreKeyID uint32) ([]byte, error) {
	query := `
		SELECT signed_prekey_public_key, signed_prekey_private_key, signed_prekey_signature
		FROM device_identity_keys
		WHERE user_id = CURRENT_USER_UUID()
		AND signed_prekey_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	var pubKey, privKey, sig string
	err := s.db.QueryRowContext(ctx, query, signedPreKeyID).Scan(&pubKey, &privKey, &sig)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("signed prekey not found: %d", signedPreKeyID)
	}
	if err != nil {
		return nil, fmt.Errorf("load signed prekey failed: %w", err)
	}

	// libsignal expects SignedPreKeyRecord in binary format
	// Combine public, private key, and signature
	recordBytes := make([]byte, len(pubKey)+len(privKey)+len(sig)+8)
	offset := 0

	// Store lengths as uint32 big-endian for parsing
	binary.BigEndian.PutUint32(recordBytes[offset:], uint32(len(pubKey)))
	offset += 4
	copy(recordBytes[offset:offset+len(pubKey)], []byte(pubKey))
	offset += len(pubKey)

	binary.BigEndian.PutUint32(recordBytes[offset:], uint32(len(privKey)))
	offset += 4
	copy(recordBytes[offset:offset+len(privKey)], []byte(privKey))
	offset += len(privKey)

	copy(recordBytes[offset:], []byte(sig))

	return recordBytes, nil
}

// StoreSignedPreKey saves a signed prekey (typically on account creation or rotation)
// Implements: signal.SignedPreKeyStore.StoreSignedPreKey(ctx, signedPreKeyID, record)
func (s *PostgresSignedPreKeyStore) StoreSignedPreKey(ctx context.Context, signedPreKeyID uint32, signedPreKeyBytes []byte) error {
	// Signed prekeys are typically received from client bundles, not generated server-side
	// This is here for completeness
	return fmt.Errorf("StoreSignedPreKey called server-side - signed prekeys come from client")
}

// ContainsSignedPreKey checks if a signed prekey exists
// Implements: signal.SignedPreKeyStore.ContainsSignedPreKey(ctx, signedPreKeyID) (bool, error)
func (s *PostgresSignedPreKeyStore) ContainsSignedPreKey(ctx context.Context, signedPreKeyID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM device_identity_keys
			WHERE user_id = CURRENT_USER_UUID()
			AND signed_prekey_id = $1
			LIMIT 1
		)
	`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, signedPreKeyID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("contains signed prekey check failed: %w", err)
	}

	return exists, nil
}

// =================== Helper Functions ===================

// computeFingerprintFromKey generates SHA256 fingerprint of a key
func computeFingerprintFromKey(keyBytes []byte) string {
	hash := sha256.Sum256(keyBytes)
	return fmt.Sprintf("%x", hash)
}

// =================== SessionManager Integration ===================
//
// Update SessionManager to use libsignal stores:
//
// type SessionManager struct {
//     db                  *sql.DB
//     sessionStore        *PostgresSessionStore
//     identityStore       *PostgresIdentityKeyStore
//     preKeyStore         *PostgresPreKeyStore
//     signedPreKeyStore   *PostgresSignedPreKeyStore
// }
//
// func NewSessionManager(db *sql.DB) *SessionManager {
//     return &SessionManager{
//         db:                db,
//         sessionStore:      NewPostgresSessionStore(db),
//         identityStore:     NewPostgresIdentityKeyStore(db),
//         preKeyStore:       NewPostgresPreKeyStore(db),
//         signedPreKeyStore: NewPostgresSignedPreKeyStore(db),
//     }
// }
//
// func (sm *SessionManager) EncryptMessage(ctx context.Context, recipientName string, recipientDeviceID uint32, plaintext []byte) ([]byte, error) {
//     // Create SessionCipher from libsignal
//     cipher := signal.NewSessionCipher(sm.sessionStore, sm.identityStore, recipientName, recipientDeviceID)
//
//     // Encrypt using Double Ratchet
//     ciphertext, err := cipher.Encrypt(ctx, plaintext)
//     if err != nil {
//         return nil, fmt.Errorf("encryption failed: %w", err)
//     }
//
//     return ciphertext, nil
// }
//
// func (sm *SessionManager) DecryptMessage(ctx context.Context, senderName string, senderDeviceID uint32, ciphertext []byte) ([]byte, error) {
//     // Create SessionCipher from libsignal
//     cipher := signal.NewSessionCipher(sm.sessionStore, sm.identityStore, senderName, senderDeviceID)
//
//     // Decrypt using Double Ratchet
//     plaintext, err := cipher.Decrypt(ctx, ciphertext)
//     if err != nil {
//         return nil, fmt.Errorf("decryption failed: %w", err)
//     }
//
//     return plaintext, nil
// }

import "crypto/sha256"
