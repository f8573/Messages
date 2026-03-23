package e2ee

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MultiRecipientEncryption wraps Signal protocol for group messaging
// Encrypts once to group session, then wraps session key for each recipient
type MultiRecipientEncryption struct {
	db *pgxpool.Pool
	sm *SessionManager
}

// NewMultiRecipientEncryption creates a multi-recipient encryption service
func NewMultiRecipientEncryption(pool *pgxpool.Pool) *MultiRecipientEncryption {
	return &MultiRecipientEncryption{
		db: pool,
		sm: &SessionManager{db: pool}, // removed: NewSessionManager wrapper
	}
}

// RecipientWrappedKey represents session key wrapped for a recipient device
type RecipientWrappedKey struct {
	UserID         string `json:"user_id"`
	DeviceID       string `json:"device_id"`
	WrappedKey     string `json:"wrapped_key"`     // Base64(X3DH wrap(sessionKey))
	WrappedKeyNonce string `json:"wrapped_key_nonce"`
}

// EncryptForGroup encrypts plaintext for all members of a group
// Returns ciphertext + per-recipient wrapped session keys
func (m *MultiRecipientEncryption) EncryptForGroup(
	ctx context.Context,
	groupID string,
	senderUserID string,
	senderDeviceID string,
	plaintext []byte,
) (
	ciphertext string,       // Base64 encrypted plaintext
	groupSessionNonce string, // Encryption IV/nonce
	recipients []RecipientWrappedKey,
	err error,
) {
	// Query MLS tree to get all members and devices
	query := `
		SELECT DISTINCT user_id, device_id
		FROM group_member_tree_leaves
		WHERE group_id = $1::uuid
		ORDER BY user_id, device_id
	`
	rows, err := m.db.Query(ctx, query, groupID)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to query group members: %w", err)
	}
	defer rows.Close()

	// Generate group session key (AES-256 for Double Ratchet)
	groupSessionKey := make([]byte, 32)
	if _, err := rand.Read(groupSessionKey); err != nil {
		return "", "", nil, err
	}

	// Generate nonce for group session
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", nil, err
	}
	groupSessionNonce = base64.StdEncoding.EncodeToString(nonce)

	// Encrypt plaintext with group session key using AES-256-GCM
	var sessionKeyArray [32]byte
	copy(sessionKeyArray[:], groupSessionKey)
	encryptedData, _, err := AESGCMEncrypt(sessionKeyArray, plaintext, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("encryption failed: %w", err)
	}
	ciphertext = base64.StdEncoding.EncodeToString(encryptedData)

	// Wrap session key for each recipient using X3DH per-device
	recipients, hasMember := make([]RecipientWrappedKey, 0, 10), false

	// Create temporary Double Ratchet state for key wrapping
	// In production, this would use actual session state from database
	tempDRState, err := InitializeDoubleRatchetState(groupSessionKey)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create temporary ratchet state: %w", err)
	}

	for rows.Next() {
		hasMember = true
		var userID, deviceID string
		if err := rows.Scan(&userID, &deviceID); err != nil {
			return "", "", nil, err
		}

		var agreementPublicKeyB64 string
		err := m.db.QueryRow(ctx, `
			SELECT identity_public_key FROM device_identity_keys
			WHERE device_id = $1::uuid AND user_id = $2::uuid
		`, deviceID, userID).Scan(&agreementPublicKeyB64)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return "", "", nil, fmt.Errorf("failed to load recipient key: %w", err)
		}

		// Use X3DH to wrap the session key for this recipient
		wrappedKey, wrappedNonce, err := GenerateRecipientWrappedKey(agreementPublicKeyB64, tempDRState)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to wrap key for %s/%s: %w", userID, deviceID, err)
		}

		recipients = append(recipients, RecipientWrappedKey{
			UserID:          userID,
			DeviceID:        deviceID,
			WrappedKey:      wrappedKey,
			WrappedKeyNonce: wrappedNonce,
		})
	}

	if err := rows.Err(); err != nil {
		return "", "", nil, err
	}

	if !hasMember {
		return "", "", nil, fmt.Errorf("group has no members")
	}

	return ciphertext, groupSessionNonce, recipients, nil
}

// DecryptGroupMessage decrypts a message encrypted for group
// Uses per-device wrapped session key to recover group session key, then decrypts
func (m *MultiRecipientEncryption) DecryptGroupMessage(
	ctx context.Context,
	groupID string,
	recipientUserID string,
	recipientDeviceID string,
	wrappedSessionKey string,
	wrappedKeyNonce string,
	ciphertext string,
	groupSessionNonce string,
) ([]byte, error) {
	// Decode nonce for wrapped key
	nonceBytes, err := base64.StdEncoding.DecodeString(wrappedKeyNonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}

	if len(nonceBytes) != 12 {
		return nil, fmt.Errorf("invalid wrap nonce size: %d", len(nonceBytes))
	}

	// Query recipient's private identity key from database
	var recipientPrivateKeyB64 string
	err = m.db.QueryRow(ctx, `
		SELECT identity_private_key FROM device_identity_keys
		WHERE device_id = $1::uuid AND user_id = $2::uuid
	`, recipientDeviceID, recipientUserID).Scan(&recipientPrivateKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipient private key: %w", err)
	}

	// Create temporary receiver state for unwrapping
	// In production, would use actual session state with full history
	tempDRState, err := InitializeDoubleRatchetStateAsReceiver(make([]byte, 32))
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary receiver state: %w", err)
	}

	// Unwrap session key using X3DH
	wrappedSessionKeyBytes, err := UnwrapSessionKeyWithDoubleRatchet(
		recipientPrivateKeyB64,
		wrappedSessionKey,
		wrappedKeyNonce,
		tempDRState,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap session key: %w", err)
	}

	if len(wrappedSessionKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid unwrapped key size: %d", len(wrappedSessionKeyBytes))
	}

	// Decode ciphertext
	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext encoding: %w", err)
	}

	// Decode group session nonce
	groupNonceBytes, err := base64.StdEncoding.DecodeString(groupSessionNonce)
	if err != nil {
		return nil, fmt.Errorf("invalid group session nonce encoding: %w", err)
	}

	if len(groupNonceBytes) != 12 {
		return nil, fmt.Errorf("invalid group nonce size: %d", len(groupNonceBytes))
	}

	// Decrypt with unwrapped session key using AES-256-GCM
	var sessionKeyArray [32]byte
	copy(sessionKeyArray[:], wrappedSessionKeyBytes)

	var nonceArray [12]byte
	copy(nonceArray[:], groupNonceBytes)

	plaintext, err := AESGCMDecrypt(sessionKeyArray, ciphertextBytes, nonceArray, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// RotateGroupKey derives new group session key after member change
// Called after member add/remove to rekey all remaining members
func (m *MultiRecipientEncryption) RotateGroupKey(
	ctx context.Context,
	groupID string,
	currentEpoch int64,
	groupSecret []byte,
) ([]byte, error) {
	if len(groupSecret) != 32 {
		return nil, fmt.Errorf("invalid group secret size: %d", len(groupSecret))
	}

	// Derive new key using HKDF with epoch as context
	epochBytes := []byte(fmt.Sprintf("group-key-rotation-epoch-%d", currentEpoch))
	newKeyBytes, err := HKDFExtractExpand(
		[]byte("group-secret"),  // salt from group secret
		groupSecret,             // input key material
		epochBytes,              // info: include epoch number
		32,                      // output 32 bytes
	)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	return newKeyBytes, nil
}

// ValidateRecipientList verifies all recipients are members of group
func (m *MultiRecipientEncryption) ValidateRecipientList(
	ctx context.Context,
	groupID string,
	recipients []RecipientWrappedKey,
) error {
	// Query group members
	query := `
		SELECT COUNT(DISTINCT device_id)
		FROM group_member_tree_leaves
		WHERE group_id = $1::uuid
	`
	var expectedCount int
	err := m.db.QueryRow(ctx, query, groupID).Scan(&expectedCount)
	if err != nil {
		return err
	}

	if len(recipients) != expectedCount {
		return fmt.Errorf("recipient count mismatch: expected %d, got %d", expectedCount, len(recipients))
	}

	// Verify each recipient is in group
	recipientMap := make(map[string]bool)
	for _, r := range recipients {
		recipientMap[r.UserID+":"+r.DeviceID] = true
	}

	leaves, err := m.db.Query(ctx, `
		SELECT user_id, device_id FROM group_member_tree_leaves
		WHERE group_id = $1::uuid
	`, groupID)
	if err != nil {
		return err
	}
	defer leaves.Close()

	for leaves.Next() {
		var userID, deviceID string
		if err := leaves.Scan(&userID, &deviceID); err != nil {
			return err
		}
		if !recipientMap[userID+":"+deviceID] {
			return fmt.Errorf("invalid recipient: %s/%s not member of group", userID, deviceID)
		}
	}

	return leaves.Err()
}
