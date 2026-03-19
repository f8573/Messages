package users

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/replication"
)

type Service struct {
	db          *pgxpool.Pool
	replication *replication.Store
}

type Profile struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	PhoneE164   string `json:"primary_phone_e164,omitempty"`
}

func NewService(db *pgxpool.Pool, store *replication.Store) *Service {
	return &Service{db: db, replication: store}
}

func (s *Service) GetProfile(ctx context.Context, userID string) (Profile, error) {
	var profile Profile
	if err := s.db.QueryRow(ctx, `
		SELECT id::text, COALESCE(display_name, ''), COALESCE(avatar_url, ''), COALESCE(primary_phone_e164, '')
		FROM users
		WHERE id = $1::uuid
	`, userID).Scan(&profile.UserID, &profile.DisplayName, &profile.AvatarURL, &profile.PhoneE164); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (s *Service) UpdateProfile(ctx context.Context, userID string, displayName, avatarURL *string) (Profile, error) {
	var displayNameArg any
	if displayName != nil {
		displayNameArg = strings.TrimSpace(*displayName)
	}
	var avatarURLArg any
	if avatarURL != nil {
		avatarURLArg = strings.TrimSpace(*avatarURL)
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE users
		SET display_name = CASE WHEN $2::bool THEN NULLIF($3::text, '') ELSE display_name END,
		    avatar_url = CASE WHEN $4::bool THEN NULLIF($5::text, '') ELSE avatar_url END,
		    updated_at = now()
		WHERE id = $1::uuid
	`, userID, displayName != nil, displayNameArg, avatarURL != nil, avatarURLArg); err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, userID)
}

func (s *Service) ResolveProfiles(ctx context.Context, userIDs []string) ([]Profile, error) {
	if len(userIDs) == 0 {
		return []Profile{}, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, COALESCE(display_name, ''), COALESCE(avatar_url, ''), COALESCE(primary_phone_e164, '')
		FROM users
		WHERE id = ANY($1::uuid[])
	`, dedupeUUIDs(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Profile, 0, len(userIDs))
	for rows.Next() {
		var profile Profile
		if err := rows.Scan(&profile.UserID, &profile.DisplayName, &profile.AvatarURL, &profile.PhoneE164); err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

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
	if err != nil {
		return err
	}
	return s.emitBlockStateUpdates(ctx, actorID, targetID)
}

// UnblockUser removes a block record.
func (s *Service) UnblockUser(ctx context.Context, actorID, targetID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM user_blocks WHERE blocker_user_id = $1::uuid AND blocked_user_id = $2::uuid`, actorID, targetID)
	if err != nil {
		return err
	}
	return s.emitBlockStateUpdates(ctx, actorID, targetID)
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

func (s *Service) emitBlockStateUpdates(ctx context.Context, actorID, targetID string) error {
	if s.replication == nil {
		return nil
	}
	actorBlockedTarget, err := s.HasBlocked(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	targetBlockedActor, err := s.HasBlocked(ctx, targetID, actorID)
	if err != nil {
		return err
	}

	rows, err := s.db.Query(ctx, `
		SELECT c.id::text
		FROM conversations c
		JOIN conversation_members me
		  ON me.conversation_id = c.id
		 AND me.user_id = $1::uuid
		JOIN conversation_members them
		  ON them.conversation_id = c.id
		 AND them.user_id = $2::uuid
	`, actorID, targetID)
	if err != nil {
		return err
	}
	defer rows.Close()

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for rows.Next() {
		var conversationID string
		if err := rows.Scan(&conversationID); err != nil {
			return err
		}
		_, err = s.replication.EmitUserEvent(ctx, actorID, conversationID, replication.UserEventConversationStateUpdated, map[string]any{
			"conversation_id":   conversationID,
			"blocked":           actorBlockedTarget || targetBlockedActor,
			"blocked_by_viewer": actorBlockedTarget,
			"blocked_by_other":  targetBlockedActor,
			"updated_at":        updatedAt,
		})
		if err != nil {
			return err
		}
		_, err = s.replication.EmitUserEvent(ctx, targetID, conversationID, replication.UserEventConversationStateUpdated, map[string]any{
			"conversation_id":   conversationID,
			"blocked":           actorBlockedTarget || targetBlockedActor,
			"blocked_by_viewer": targetBlockedActor,
			"blocked_by_other":  actorBlockedTarget,
			"updated_at":        updatedAt,
		})
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func dedupeUUIDs(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, err := uuid.Parse(item); err != nil {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
