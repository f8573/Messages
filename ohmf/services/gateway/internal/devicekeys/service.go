package devicekeys

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/securityaudit"
)

type Service struct {
	pool *pgxpool.Pool
}

type KeyBackup struct {
	BackupName         string         `json:"backup_name"`
	SourceDeviceID     string         `json:"source_device_id,omitempty"`
	EncryptedBlob      string         `json:"encrypted_blob,omitempty"`
	WrappingAlg        string         `json:"wrapping_alg"`
	WrappedKey         string         `json:"wrapped_key,omitempty"`
	RecoveryData       map[string]any `json:"recovery_data,omitempty"`
	AttestationType    string         `json:"attestation_type,omitempty"`
	AttestationPayload map[string]any `json:"attestation_payload,omitempty"`
	BackupHash         string         `json:"backup_hash,omitempty"`
	CreatedAt          string         `json:"created_at,omitempty"`
	UpdatedAt          string         `json:"updated_at,omitempty"`
	LastRestoredAt     string         `json:"last_restored_at,omitempty"`
}

type UpsertBackupRequest struct {
	SourceDeviceID     string         `json:"source_device_id,omitempty"`
	EncryptedBlob      string         `json:"encrypted_blob"`
	WrappingAlg        string         `json:"wrapping_alg,omitempty"`
	WrappedKey         string         `json:"wrapped_key,omitempty"`
	RecoveryData       map[string]any `json:"recovery_data,omitempty"`
	AttestationType    string         `json:"attestation_type,omitempty"`
	AttestationPayload map[string]any `json:"attestation_payload,omitempty"`
	BackupHash         string         `json:"backup_hash,omitempty"`
}

var ErrBackupNotFound = errors.New("backup_not_found")

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// DB returns the underlying database pool for handlers
func (s *Service) DB() *pgxpool.Pool {
	return s.pool
}

func (s *Service) PublishBundle(ctx context.Context, actorUserID, deviceID string, req PublishRequest) (Bundle, error) {
	return (&Handler{DB: s.pool}).PublishBundle(ctx, actorUserID, deviceID, req)
}

func (s *Service) AddOneTimePrekeys(ctx context.Context, actorUserID, deviceID string, prekeys []OneTimePrekey) (Bundle, error) {
	return (&Handler{DB: s.pool}).AddOneTimePrekeys(ctx, actorUserID, deviceID, prekeys)
}

func (s *Service) ListBundlesForUser(ctx context.Context, userID string) ([]Bundle, error) {
	return (&Handler{DB: s.pool}).ListBundlesForUser(ctx, userID)
}

func (s *Service) ClaimBundles(ctx context.Context, userID string) ([]Bundle, error) {
	return (&Handler{DB: s.pool}).ClaimBundles(ctx, userID)
}

func (s *Service) UpsertBackup(ctx context.Context, actorUserID, backupName string, req UpsertBackupRequest) (KeyBackup, error) {
	backupName = strings.TrimSpace(backupName)
	if backupName == "" {
		backupName = "default"
	}
	req.EncryptedBlob = strings.TrimSpace(req.EncryptedBlob)
	if req.EncryptedBlob == "" {
		return KeyBackup{}, errors.New("encrypted_blob_required")
	}
	if req.WrappingAlg == "" {
		req.WrappingAlg = "X25519_AES256GCM"
	}
	if req.RecoveryData == nil {
		req.RecoveryData = map[string]any{}
	}
	if req.AttestationPayload == nil {
		req.AttestationPayload = map[string]any{}
	}
	if strings.TrimSpace(req.SourceDeviceID) != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM devices
				WHERE id = $1::uuid AND user_id = $2::uuid
			)
		`, req.SourceDeviceID, actorUserID).Scan(&exists); err != nil {
			return KeyBackup{}, err
		}
		if !exists {
			return KeyBackup{}, ErrDeviceNotOwned
		}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO device_key_backups (
			user_id,
			source_device_id,
			backup_name,
			encrypted_blob,
			wrapping_alg,
			wrapped_key,
			recovery_data,
			attestation_type,
			attestation_payload,
			backup_hash,
			updated_at,
			deleted_at
		)
		VALUES (
			$1::uuid,
			NULLIF($2, '')::uuid,
			$3,
			$4,
			$5,
			NULLIF($6, ''),
			$7::jsonb,
			NULLIF($8, ''),
			$9::jsonb,
			NULLIF($10, ''),
			now(),
			NULL
		)
		ON CONFLICT (user_id, backup_name) WHERE deleted_at IS NULL
		DO UPDATE SET
			source_device_id = EXCLUDED.source_device_id,
			encrypted_blob = EXCLUDED.encrypted_blob,
			wrapping_alg = EXCLUDED.wrapping_alg,
			wrapped_key = EXCLUDED.wrapped_key,
			recovery_data = EXCLUDED.recovery_data,
			attestation_type = EXCLUDED.attestation_type,
			attestation_payload = EXCLUDED.attestation_payload,
			backup_hash = EXCLUDED.backup_hash,
			updated_at = now(),
			deleted_at = NULL
	`, actorUserID, req.SourceDeviceID, backupName, req.EncryptedBlob, req.WrappingAlg, req.WrappedKey, mustJSON(req.RecoveryData), req.AttestationType, mustJSON(req.AttestationPayload), req.BackupHash)
	if err != nil {
		return KeyBackup{}, err
	}
	_ = securityaudit.Append(ctx, s.pool, actorUserID, actorUserID, "device_key_backup_upserted", map[string]any{
		"backup_name":      backupName,
		"source_device_id": req.SourceDeviceID,
		"backup_hash":      req.BackupHash,
	})
	return s.GetBackup(ctx, actorUserID, backupName, false)
}

func (s *Service) ListBackups(ctx context.Context, actorUserID string) ([]KeyBackup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			backup_name,
			COALESCE(source_device_id::text, ''),
			wrapping_alg,
			COALESCE(attestation_type, ''),
			COALESCE(backup_hash, ''),
			created_at,
			updated_at,
			last_restored_at
		FROM device_key_backups
		WHERE user_id = $1::uuid
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC
	`, actorUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]KeyBackup, 0, 2)
	for rows.Next() {
		var item KeyBackup
		var createdAt time.Time
		var updatedAt time.Time
		var lastRestoredAt *time.Time
		if err := rows.Scan(&item.BackupName, &item.SourceDeviceID, &item.WrappingAlg, &item.AttestationType, &item.BackupHash, &createdAt, &updatedAt, &lastRestoredAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		if lastRestoredAt != nil {
			item.LastRestoredAt = lastRestoredAt.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetBackup(ctx context.Context, actorUserID, backupName string, markRestored bool) (KeyBackup, error) {
	backupName = strings.TrimSpace(backupName)
	if backupName == "" {
		backupName = "default"
	}
	if markRestored {
		if _, err := s.pool.Exec(ctx, `
			UPDATE device_key_backups
			SET last_restored_at = now(),
			    updated_at = now()
			WHERE user_id = $1::uuid
			  AND backup_name = $2
			  AND deleted_at IS NULL
		`, actorUserID, backupName); err != nil {
			return KeyBackup{}, err
		}
		_ = securityaudit.Append(ctx, s.pool, actorUserID, actorUserID, "device_key_backup_restored", map[string]any{
			"backup_name": backupName,
		})
	}
	var item KeyBackup
	var recoveryRaw []byte
	var attestationRaw []byte
	var createdAt time.Time
	var updatedAt time.Time
	var lastRestoredAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT
			backup_name,
			COALESCE(source_device_id::text, ''),
			encrypted_blob,
			wrapping_alg,
			COALESCE(wrapped_key, ''),
			recovery_data,
			COALESCE(attestation_type, ''),
			attestation_payload,
			COALESCE(backup_hash, ''),
			created_at,
			updated_at,
			last_restored_at
		FROM device_key_backups
		WHERE user_id = $1::uuid
		  AND backup_name = $2
		  AND deleted_at IS NULL
	`, actorUserID, backupName).Scan(
		&item.BackupName,
		&item.SourceDeviceID,
		&item.EncryptedBlob,
		&item.WrappingAlg,
		&item.WrappedKey,
		&recoveryRaw,
		&item.AttestationType,
		&attestationRaw,
		&item.BackupHash,
		&createdAt,
		&updatedAt,
		&lastRestoredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KeyBackup{}, ErrBackupNotFound
		}
		return KeyBackup{}, err
	}
	_ = decodeJSONMap(recoveryRaw, &item.RecoveryData)
	_ = decodeJSONMap(attestationRaw, &item.AttestationPayload)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if lastRestoredAt != nil {
		item.LastRestoredAt = lastRestoredAt.UTC().Format(time.RFC3339Nano)
	}
	return item, nil
}

func (s *Service) DeleteBackup(ctx context.Context, actorUserID, backupName string) error {
	backupName = strings.TrimSpace(backupName)
	if backupName == "" {
		backupName = "default"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE device_key_backups
		SET deleted_at = now(),
		    updated_at = now(),
		    encrypted_blob = '',
		    wrapped_key = NULL,
		    recovery_data = '{}'::jsonb,
		    attestation_payload = '{}'::jsonb
		WHERE user_id = $1::uuid
		  AND backup_name = $2
		  AND deleted_at IS NULL
	`, actorUserID, backupName)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBackupNotFound
	}
	_ = securityaudit.Append(ctx, s.pool, actorUserID, actorUserID, "device_key_backup_deleted", map[string]any{
		"backup_name": backupName,
	})
	return nil
}

func mustJSON(value map[string]any) string {
	if value == nil {
		value = map[string]any{}
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func decodeJSONMap(raw []byte, target *map[string]any) error {
	if len(raw) == 0 {
		*target = map[string]any{}
		return nil
	}
	return json.Unmarshal(raw, target)
}
