package users

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// ExportAccount returns a simple export payload for the given user.
// This is a best-effort export intended for developer/ops use; it includes
// basic profile and device information. A production implementation should
// expand this to include messages, attachments, and conversation history.
func (s *Service) ExportAccount(ctx context.Context, userID string) (map[string]any, error) {
	var primaryPhone, displayName, avatarURL string
	var createdAt, updatedAt time.Time
	if err := s.db.QueryRow(ctx, `
		SELECT primary_phone_e164, COALESCE(display_name, ''), COALESCE(avatar_url, ''), created_at, updated_at
		FROM users WHERE id = $1
	`, userID).Scan(&primaryPhone, &displayName, &avatarURL, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, `SELECT id::text, platform, device_name, push_token, public_key, created_at, updated_at FROM devices WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]map[string]any, 0)
	for rows.Next() {
		var id, platform, deviceName, pushToken, publicKey string
		var ca, ua time.Time
		if err := rows.Scan(&id, &platform, &deviceName, &pushToken, &publicKey, &ca, &ua); err != nil {
			return nil, err
		}
		devices = append(devices, map[string]any{
			"device_id":   id,
			"platform":    platform,
			"device_name": deviceName,
			"push_token":  pushToken,
			"public_key":  publicKey,
			"created_at":  ca,
			"updated_at":  ua,
		})
	}

	payload := map[string]any{
		"user": map[string]any{
			"user_id":            userID,
			"primary_phone_e164": primaryPhone,
			"display_name":       displayName,
			"avatar_url":         avatarURL,
			"created_at":         createdAt,
			"updated_at":         updatedAt,
		},
		"devices": devices,
	}
	return payload, nil
}

// DeleteAccount performs an account deletion request by removing identity
// mappings, revoking sessions, deleting devices, clearing profile fields
// and removing discovery indexes. It intentionally preserves the user row
// (and therefore authored messages) to maintain conversation integrity.
func (s *Service) DeleteAccount(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var primaryPhone string
	if err := tx.QueryRow(ctx, `SELECT primary_phone_e164 FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&primaryPhone); err != nil {
		return err
	}

	// revoke all refresh tokens
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}

	// delete devices
	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE user_id = $1`, userID); err != nil {
		return err
	}

	// remove discovery index entries for the phone
	if primaryPhone != "" {
		if _, err := tx.Exec(ctx, `DELETE FROM external_contacts WHERE phone_e164 = $1`, primaryPhone); err != nil {
			return err
		}
	}

	// clear profile fields and move phone to a non-conflicting deleted marker
	deletedPhone := fmt.Sprintf("deleted:%s", userID)
	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET primary_phone_e164 = $2, phone_verified_at = NULL, display_name = NULL, avatar_url = NULL, updated_at = now()
		WHERE id = $1
	`, userID, deletedPhone); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// BlockUser adds a block record where actor blocks target.
func (s *Service) BlockUser(ctx context.Context, actorID, targetID string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_blocks (blocker_user_id, blocked_user_id, created_at)
		VALUES ($1::uuid, $2::uuid, now())
		ON CONFLICT DO NOTHING
	`, actorID, targetID)
	return err
}

// UnblockUser removes a block record.
func (s *Service) UnblockUser(ctx context.Context, actorID, targetID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM user_blocks WHERE blocker_user_id = $1::uuid AND blocked_user_id = $2::uuid`, actorID, targetID)
	return err
}

// HasBlocked returns true if blocker has blocked blockedID.
func (s *Service) HasBlocked(ctx context.Context, blockerID, blockedID string) (bool, error) {
	var one int
	err := s.db.QueryRow(ctx, `SELECT 1 FROM user_blocks WHERE blocker_user_id = $1::uuid AND blocked_user_id = $2::uuid`, blockerID, blockedID).Scan(&one)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
