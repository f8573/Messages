package devices

import (
	"context"
	"encoding/base64"
	"errors"
	"io"

	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/sqlutil"
)

// Service contains business logic for device management
type Service struct {
	db                *pgxpool.Pool
	subscriptionKey   []byte
}

// NewService creates a device service with encrypted subscription support
func NewService(db *pgxpool.Pool, subscriptionKey []byte) *Service {
	return &Service{
		db:              db,
		subscriptionKey: subscriptionKey,
	}
}

// RegisterDevice creates a new device for a user
func (s *Service) RegisterDevice(ctx context.Context, userID string, d Device) (string, error) {
	d.Capabilities = normalizeCapabilities(d.Platform, d.Capabilities)
	encryptedSubscription, err := s.encryptSubscription(d.PushSubscription)
	if err != nil {
		return "", err
	}
	var id string
	err = s.db.QueryRow(ctx, `
		INSERT INTO devices (
			user_id,
			platform,
			device_name,
			client_version,
			capabilities,
			sms_role_state,
			push_token,
			push_provider,
			push_subscription,
			push_subscription_updated_at,
			public_key,
			last_seen_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,CASE WHEN NULLIF($9, '') IS NULL THEN NULL ELSE now() END,$10,now())
		RETURNING id::text
	`, userID, d.Platform, d.DeviceName, d.ClientVersion, d.Capabilities, d.SMSRoleState, sqlutil.Nullable(d.PushToken), sqlutil.Nullable(d.PushProvider), sqlutil.Nullable(encryptedSubscription), sqlutil.Nullable(d.PublicKey)).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateDevice updates device information for a user
func (s *Service) UpdateDevice(ctx context.Context, userID, deviceID string, d Device) (Device, error) {
	encryptedSubscription, err := s.encryptSubscription(d.PushSubscription)
	if err != nil {
		return Device{}, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE devices
		SET device_name = COALESCE(NULLIF($3, ''), device_name),
		    client_version = COALESCE(NULLIF($4, ''), client_version),
		    capabilities = CASE WHEN $5::bool THEN $6 ELSE capabilities END,
		    push_token = CASE WHEN $7::bool THEN NULLIF($8, '') ELSE push_token END,
		    push_provider = CASE WHEN $9::bool THEN NULLIF($10, '') ELSE push_provider END,
		    push_subscription = CASE WHEN $11::bool THEN NULLIF($12, '') ELSE push_subscription END,
		    push_subscription_updated_at = CASE WHEN $11::bool THEN now() ELSE push_subscription_updated_at END,
		    public_key = CASE WHEN $13::bool THEN NULLIF($14, '') ELSE public_key END,
		    last_seen_at = now(),
		    updated_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid
	`, deviceID, userID,
		d.DeviceName,
		d.ClientVersion,
		len(d.Capabilities) > 0, normalizeCapabilities(d.Platform, d.Capabilities),
		d.PushToken != "", d.PushToken,
		d.PushProvider != "", d.PushProvider,
		d.PushSubscription != "", encryptedSubscription,
		d.PublicKey != "", d.PublicKey,
	)
	if err != nil {
		return Device{}, err
	}
	if tag.RowsAffected() == 0 {
		return Device{}, ErrDeviceNotFound
	}
	return s.GetDevice(ctx, userID, deviceID)
}

// GetDevice retrieves a single device
func (s *Service) GetDevice(ctx context.Context, userID, deviceID string) (Device, error) {
	var d Device
	var caps []string
	var encryptedSubscription string
	err := s.db.QueryRow(ctx, `
		SELECT
			id::text,
			user_id::text,
			platform,
			COALESCE(device_name, ''),
			COALESCE(client_version, ''),
			COALESCE(capabilities, ARRAY[]::text[]),
			COALESCE(sms_role_state, ''),
			COALESCE(push_token, ''),
			COALESCE(push_provider, ''),
			COALESCE(push_subscription, ''),
			COALESCE(public_key, ''),
			COALESCE(last_seen_at, now())
		FROM devices
		WHERE user_id = $1 AND id = $2::uuid
	`, userID, deviceID).Scan(&d.ID, &d.UserID, &d.Platform, &d.DeviceName, &d.ClientVersion, &caps, &d.SMSRoleState, &d.PushToken, &d.PushProvider, &encryptedSubscription, &d.PublicKey, &d.LastSeenAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Device{}, ErrDeviceNotFound
		}
		return Device{}, err
	}
	d.Capabilities = caps
	d.HasPushSubscription = encryptedSubscription != ""
	return d, nil
}

// ListDevices retrieves all devices for a user
func (s *Service) ListDevices(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			id::text,
			user_id::text,
			platform,
			COALESCE(device_name, ''),
			COALESCE(client_version, ''),
			COALESCE(capabilities, ARRAY[]::text[]),
			COALESCE(sms_role_state, ''),
			COALESCE(push_token, ''),
			COALESCE(push_provider, ''),
			COALESCE(push_subscription, ''),
			COALESCE(public_key, ''),
			COALESCE(last_seen_at, now())
		FROM devices
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var caps []string
		var encryptedSubscription string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Platform, &d.DeviceName, &d.ClientVersion, &caps, &d.SMSRoleState, &d.PushToken, &d.PushProvider, &encryptedSubscription, &d.PublicKey, &d.LastSeenAt); err != nil {
			return nil, err
		}
		d.Capabilities = caps
		d.HasPushSubscription = encryptedSubscription != ""
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice deletes a device
func (s *Service) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM devices WHERE user_id = $1 AND id = $2::uuid`, userID, deviceID)
	return err
}

// ListWebPushSubscriptions retrieves all web push subscriptions for a user
func (s *Service) ListWebPushSubscriptions(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(push_subscription, '')
		FROM devices
		WHERE user_id = $1::uuid
		  AND UPPER(COALESCE(push_provider, '')) = 'WEBPUSH'
		  AND NULLIF(push_subscription, '') IS NOT NULL
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 2)
	for rows.Next() {
		var encrypted string
		if err := rows.Scan(&encrypted); err != nil {
			return nil, err
		}
		decrypted, err := s.decryptSubscription(encrypted)
		if err != nil || decrypted == "" {
			continue
		}
		out = append(out, decrypted)
	}
	return out, rows.Err()
}

// encryptSubscription encrypts a web push subscription using AES-GCM
func (s *Service) encryptSubscription(raw string) (string, error) {
	raw = raw // TODO: trim space if needed
	if raw == "" {
		return "", nil
	}
	block, err := aes.NewCipher(s.subscriptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(raw), nil)
	payload := append(nonce, ciphertext...)
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

// decryptSubscription decrypts a web push subscription using AES-GCM
func (s *Service) decryptSubscription(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.subscriptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid_push_subscription")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
