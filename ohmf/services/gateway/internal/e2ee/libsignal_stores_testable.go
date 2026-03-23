package e2ee

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionStoreWithUser wraps SessionStore with an authenticated user context
type SessionStoreWithUser struct {
	store    *PostgresSessionStore
	userID   string
	deviceID int64
}

// NewSessionStoreWithUser creates a store bound to a specific user
func NewSessionStoreWithUser(db *pgxpool.Pool, userID string, deviceID int64) *SessionStoreWithUser {
	return &SessionStoreWithUser{
		store:    &PostgresSessionStore{db: db},
		userID:   userID,
		deviceID: deviceID,
	}
}

// TESTABLE VERSION: LoadSession - No CURRENT_USER_UUID() function needed
func (s *SessionStoreWithUser) LoadSession(ctx context.Context, contactUserID string, contactDeviceID uint32) ([]byte, error) {
	query := `
		SELECT session_key_bytes
		FROM e2ee_sessions
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3
		LIMIT 1
	`

	var sessionBytes []byte
	err := s.store.db.QueryRow(ctx, query, s.userID, contactUserID, contactDeviceID).Scan(&sessionBytes)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No session yet
		}
		return nil, fmt.Errorf("load session failed: %w", err)
	}

	return sessionBytes, nil
}

// TESTABLE VERSION: StoreSession - No CURRENT_USER_UUID() function needed
func (s *SessionStoreWithUser) StoreSession(ctx context.Context, contactUserID string, contactDeviceID uint32, sessionBytes []byte) error {
	query := `
		INSERT INTO e2ee_sessions (user_id, contact_user_id, contact_device_id, session_key_bytes, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, NOW())
		ON CONFLICT (user_id, contact_user_id, contact_device_id) DO UPDATE SET
			session_key_bytes = $4,
			updated_at = NOW()
	`

	_, err := s.store.db.Exec(ctx, query, s.userID, contactUserID, contactDeviceID, sessionBytes)
	if err != nil {
		return fmt.Errorf("store session failed: %w", err)
	}

	return nil
}

// TESTABLE VERSION: HasSession - No CURRENT_USER_UUID() needed
func (s *SessionStoreWithUser) HasSession(ctx context.Context, contactUserID string, contactDeviceID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM e2ee_sessions
			WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3
			LIMIT 1
		)
	`

	var exists bool
	err := s.store.db.QueryRow(ctx, query, s.userID, contactUserID, contactDeviceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has session check failed: %w", err)
	}

	return exists, nil
}

// TESTABLE VERSION: DeleteSession - No CURRENT_USER_UUID() needed
func (s *SessionStoreWithUser) DeleteSession(ctx context.Context, contactUserID string, contactDeviceID uint32) error {
	query := `
		DELETE FROM e2ee_sessions
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3
	`

	_, err := s.store.db.Exec(ctx, query, s.userID, contactUserID, contactDeviceID)
	if err != nil {
		return fmt.Errorf("delete session failed: %w", err)
	}

	return nil
}

// TESTABLE VERSION: DeleteAllSessions - No CURRENT_USER_UUID() needed
func (s *SessionStoreWithUser) DeleteAllSessions(ctx context.Context, contactUserID string) error {
	query := `
		DELETE FROM e2ee_sessions
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid
	`

	_, err := s.store.db.Exec(ctx, query, s.userID, contactUserID)
	if err != nil {
		return fmt.Errorf("delete all sessions failed: %w", err)
	}

	return nil
}

// =================== IdentityKeyStore with User Context ===================

type IdentityStoreWithUser struct {
	store  *PostgresIdentityKeyStore
	userID string
}

// NewIdentityStoreWithUser creates an identity store bound to a specific user
func NewIdentityStoreWithUser(db *pgxpool.Pool, userID string) *IdentityStoreWithUser {
	return &IdentityStoreWithUser{
		store:  &PostgresIdentityKeyStore{db: db},
		userID: userID,
	}
}

// TESTABLE VERSION: IsTrustedIdentity - Fixed to use user context
func (s *IdentityStoreWithUser) IsTrustedIdentity(ctx context.Context, contactUserID string, contactDeviceID uint32, identityKey []byte) (bool, error) {
	query := `
		SELECT trust_state
		FROM device_key_trust
		WHERE user_id = $1::uuid AND contact_user_id = $2::uuid AND contact_device_id = $3
		LIMIT 1
	`

	var trustState string
	err := s.store.db.QueryRow(ctx, query, s.userID, contactUserID, contactDeviceID).Scan(&trustState)
	if err != nil {
		if err == pgx.ErrNoRows {
			// No trust record yet - TOFU model accepts first key
			return true, nil
		}
		return false, fmt.Errorf("trust check failed: %w", err)
	}

	// Trust if state is TOFU, VERIFIED, or not explicitly BLOCKED
	return trustState != "BLOCKED", nil
}

// TESTABLE VERSION: SaveIdentity - Fixed to use user context
func (s *IdentityStoreWithUser) SaveIdentity(ctx context.Context, contactUserID string, contactDeviceID uint32, identityKey []byte) (bool, error) {
	// Compute fingerprint: SHA256 of identity key
	hash := sha256.Sum256(identityKey)
	fingerprint := fmt.Sprintf("%x", hash)

	query := `
		INSERT INTO device_key_trust (user_id, contact_user_id, contact_device_id, trust_state, fingerprint, trust_established_at)
		VALUES ($1::uuid, $2::uuid, $3, 'TOFU', $4, NOW())
		ON CONFLICT DO NOTHING
		RETURNING 1
	`

	var result int
	err := s.store.db.QueryRow(ctx, query, s.userID, contactUserID, contactDeviceID, fingerprint).Scan(&result)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Conflict - key already existed (not new)
			return false, nil
		}
		return false, fmt.Errorf("save identity failed: %w", err)
	}

	return true, nil // New key was saved
}

// =================== PreKeyStore (Simpler - doesn't use CURRENT_USER_UUID) ===================

type PreKeyStoreWithUser struct {
	store  *PostgresPreKeyStore
	userID string
}

// NewPreKeyStoreWithUser creates a prekey store for a user
func NewPreKeyStoreWithUser(db *pgxpool.Pool, userID string) *PreKeyStoreWithUser {
	return &PreKeyStoreWithUser{
		store:  &PostgresPreKeyStore{db: db},
		userID: userID,
	}
}

// TESTABLE VERSION: LoadPreKey
func (s *PreKeyStoreWithUser) LoadPreKey(ctx context.Context, prekeyID uint32) ([]byte, error) {
	query := `
		SELECT prekey_public_key, prekey_private_key
		FROM device_one_time_prekeys
		WHERE user_id = $1::uuid AND prekey_id = $2
		LIMIT 1
	`

	var pubKey, privKey string
	err := s.store.db.QueryRow(ctx, query, s.userID, prekeyID).Scan(&pubKey, &privKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("prekey not found: %d", prekeyID)
		}
		return nil, fmt.Errorf("load prekey failed: %w", err)
	}

	// Combine into prekey record
	recordBytes := make([]byte, len(pubKey)+len(privKey))
	copy(recordBytes, []byte(pubKey))
	copy(recordBytes[len(pubKey):], []byte(privKey))

	return recordBytes, nil
}

// TESTABLE VERSION: ContainsPreKey
func (s *PreKeyStoreWithUser) ContainsPreKey(ctx context.Context, prekeyID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM device_one_time_prekeys
			WHERE user_id = $1::uuid AND prekey_id = $2
			LIMIT 1
		)
	`

	var exists bool
	err := s.store.db.QueryRow(ctx, query, s.userID, prekeyID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("contains prekey check failed: %w", err)
	}

	return exists, nil
}

// TESTABLE VERSION: RemovePreKey
func (s *PreKeyStoreWithUser) RemovePreKey(ctx context.Context, prekeyID uint32) error {
	query := `
		UPDATE device_one_time_prekeys
		SET consumed_at = NOW()
		WHERE user_id = $1::uuid AND prekey_id = $2
	`

	_, err := s.store.db.Exec(ctx, query, s.userID, prekeyID)
	if err != nil {
		return fmt.Errorf("remove prekey failed: %w", err)
	}

	return nil
}

// =================== SignedPreKeyStore (Simpler) ===================

type SignedPreKeyStoreWithUser struct {
	store  *PostgresSignedPreKeyStore
	userID string
}

// NewSignedPreKeyStoreWithUser creates a signed prekey store for a user
func NewSignedPreKeyStoreWithUser(db *pgxpool.Pool, userID string) *SignedPreKeyStoreWithUser {
	return &SignedPreKeyStoreWithUser{
		store:  &PostgresSignedPreKeyStore{db: db},
		userID: userID,
	}
}

// TESTABLE VERSION: LoadSignedPreKey
func (s *SignedPreKeyStoreWithUser) LoadSignedPreKey(ctx context.Context, signedPreKeyID uint32) ([]byte, error) {
	query := `
		SELECT signed_prekey_public_key, signed_prekey_private_key, signed_prekey_signature
		FROM device_identity_keys
		WHERE user_id = $1::uuid AND signed_prekey_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	var pubKey, privKey, sig string
	err := s.store.db.QueryRow(ctx, query, s.userID, signedPreKeyID).Scan(&pubKey, &privKey, &sig)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("signed prekey not found: %d", signedPreKeyID)
		}
		return nil, fmt.Errorf("load signed prekey failed: %w", err)
	}

	// Combine into record
	recordBytes := make([]byte, len(pubKey)+len(privKey)+len(sig)+8)
	offset := 0

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

// TESTABLE VERSION: ContainsSignedPreKey
func (s *SignedPreKeyStoreWithUser) ContainsSignedPreKey(ctx context.Context, signedPreKeyID uint32) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM device_identity_keys
			WHERE user_id = $1::uuid AND signed_prekey_id = $2
			LIMIT 1
		)
	`

	var exists bool
	err := s.store.db.QueryRow(ctx, query, s.userID, signedPreKeyID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("contains signed prekey check failed: %w", err)
	}

	return exists, nil
}
