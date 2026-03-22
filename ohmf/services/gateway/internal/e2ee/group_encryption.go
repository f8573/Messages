package e2ee

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

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
		sm: NewSessionManager(pool),
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

	// Collect recipient devices
	var recipientDevices []struct {
		UserID   string
		DeviceID string
	}
	for rows.Next() {
		var userID, deviceID string
		if err := rows.Scan(&userID, &deviceID); err != nil {
			return "", "", nil, err
		}
		recipientDevices = append(recipientDevices, struct {
			UserID   string
			DeviceID string
		}{userID, deviceID})
	}

	if err := rows.Err(); err != nil {
		return "", "", nil, err
	}

	if len(recipientDevices) == 0 {
		return "", "", nil, fmt.Errorf("group has no members")
	}

	// Generate group session key (AES-256 for Double Ratchet)
	groupSessionKey := make([]byte, 32)
	if _, err := rand.Read(groupSessionKey); err != nil {
		return "", "", nil, err
	}

	// Generate nonce for group session
	nonce := make([]byte, 12) // 96-bit nonce for GCM
	if _, err := rand.Read(nonce); err != nil {
		return "", "", nil, err
	}
	groupSessionNonce = base64.StdEncoding.EncodeToString(nonce)

	// Encrypt plaintext with group session key (placeholder - actual: AEAD cipher)
	// In production: aes.NewCipher + gcm.Seal
	encryptedData := make([]byte, len(plaintext))
	copy(encryptedData, plaintext) // Placeholder: actual implementation uses cipher
	ciphertext = base64.StdEncoding.EncodeToString(encryptedData)

	// Wrap session key for each recipient
	recipients = make([]RecipientWrappedKey, 0, len(recipientDevices))
	for _, recipient := range recipientDevices {
		// Skip self (messages encrypted by sender too for consistency)
		// but in practice, sender might not need wrapped key

		// Query recipient's identity/agreement key
		var agreementPublicKeyB64 string
		err := m.db.QueryRow(ctx, `
			SELECT identity_public_key FROM device_identity_keys
			WHERE device_id = $1::uuid AND user_id = $2::uuid
		`, recipient.DeviceID, recipient.UserID).Scan(&agreementPublicKeyB64)

		if err != nil {
			if err.Error() == "no rows in result set" {
				// Skip members with unpublished keys
				continue
			}
			return "", "", nil, fmt.Errorf("failed to load recipient key: %w", err)
		}

		// Wrap session key using X3DH (placeholder)
		wrappedKeyNonce := make([]byte, 12)
		if _, err := rand.Read(wrappedKeyNonce); err != nil {
			return "", "", nil, err
		}

		// In production: Perform X3DH wrap of groupSessionKey using recipient's identity key
		// For now: placeholder wrapping (actual: X25519 ECDH + KDF)
		wrappedKey := make([]byte, len(groupSessionKey))
		copy(wrappedKey, groupSessionKey)

		recipients = append(recipients, RecipientWrappedKey{
			UserID:          recipient.UserID,
			DeviceID:        recipient.DeviceID,
			WrappedKey:      base64.StdEncoding.EncodeToString(wrappedKey),
			WrappedKeyNonce: base64.StdEncoding.EncodeToString(wrappedKeyNonce),
		})
	}

	return ciphertext, groupSessionNonce, recipients, nil
}

// DecryptGroupMessage decrypts a message encrypted for group
// Uses per-device wrapped session key to recover group session key
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
	// Decode wrapped key
	wrappedKeyBytes, err := base64.StdEncoding.DecodeString(wrappedSessionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid wrapped_key encoding: %w", err)
	}

	// Decode nonce
	nonceBytes, err := base64.StdEncoding.DecodeString(wrappedKeyNonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce encoding: %w", err)
	}

	// Unwrap session key using recipient's DH secret
	// In production: X3DH unwrap of wrappedKeyBytes
	// For now: placeholder (session key = wrapped key)
	_ = nonceBytes

	// Decode ciphertext
	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext encoding: %w", err)
	}

	// Decrypt with session key
	// In production: aes.NewCipher(wrappedKeyBytes) + gcm.Open(...)
	plaintext := make([]byte, len(ciphertextBytes))
	copy(plaintext, ciphertextBytes)

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
	// In production: HKDF-Expand(groupSecret, "group_key_rotation")
	// For now: placeholder key derivation
	newKey := make([]byte, 32)
	copy(newKey, groupSecret)
	return newKey, nil
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
