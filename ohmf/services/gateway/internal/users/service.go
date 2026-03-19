package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/replication"
)

// Service handles all user-related business logic
type Service struct {
	db          *pgxpool.Pool
	replication *replication.Store
}

// NewService creates a user service
func NewService(db *pgxpool.Pool, replicationStore *replication.Store) *Service {
	return &Service{
		db:          db,
		replication: replicationStore,
	}
}

// Profile represents user profile information
type Profile struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	PhoneE164   string `json:"phone_e164,omitempty"`
}

// GetProfile fetches a user's profile information
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

// UpdateProfile updates user profile display name and avatar
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

// ResolveProfiles retrieves multiple user profiles with deduplication and validation
func (s *Service) ResolveProfiles(ctx context.Context, userIDs []string) ([]Profile, error) {
	if len(userIDs) == 0 {
		return []Profile{}, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, COALESCE(display_name, ''), COALESCE(avatar_url, ''), COALESCE(primary_phone_e164, '')
		FROM users
		WHERE id = ANY($1::uuid[])
	`, s.dedupeAndValidateUUIDs(userIDs))
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

// dedupeAndValidateUUIDs deduplicates and validates UUIDs
func (s *Service) dedupeAndValidateUUIDs(items []string) []string {
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

// ExportAccount exports user account data including devices
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

// DeleteAccount performs multi-step account deletion with transaction
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

// BlockUser creates a block relationship between two users
func (s *Service) BlockUser(ctx context.Context, actorID, targetID string) error {
	if _, err := s.db.Exec(ctx, `
		INSERT INTO user_blocks (user_id, blocked_user_id, created_at)
		VALUES ($1::uuid, $2::uuid, now())
		ON CONFLICT DO NOTHING
	`, actorID, targetID); err != nil {
		return err
	}
	return s.emitBlockStateUpdates(ctx, actorID, targetID)
}

// UnblockUser removes a block relationship between two users
func (s *Service) UnblockUser(ctx context.Context, actorID, targetID string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM user_blocks WHERE user_id = $1::uuid AND blocked_user_id = $2::uuid`, actorID, targetID); err != nil {
		return err
	}
	return s.emitBlockStateUpdates(ctx, actorID, targetID)
}

// HasBlocked checks if one user has blocked another
func (s *Service) HasBlocked(ctx context.Context, blockerID, blockedID string) (bool, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM user_blocks WHERE user_id = $1::uuid AND blocked_user_id = $2::uuid)
	`, blockerID, blockedID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return exists, nil
}

// emitBlockStateUpdates notifies about block state changes in shared conversations
func (s *Service) emitBlockStateUpdates(ctx context.Context, actorID, targetID string) error {
	if s.replication == nil {
		return nil
	}

	// Check current block states
	actorBlocksTarget, err := s.HasBlocked(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	targetBlocksActor, err := s.HasBlocked(ctx, targetID, actorID)
	if err != nil {
		return err
	}

	// Get shared conversations
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT c.id::text
		FROM conversations c
		JOIN conversation_members cm1 ON cm1.conversation_id = c.id AND cm1.user_id = $1::uuid
		JOIN conversation_members cm2 ON cm2.conversation_id = c.id AND cm2.user_id = $2::uuid
	`, actorID, targetID)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Emit events for each shared conversation
	for rows.Next() {
		var conversationID string
		if err := rows.Scan(&conversationID); err != nil {
			return err
		}

		// Emit for actor
		s.replication.EmitUserEvent(ctx, actorID, replication.UserEventConversationStateUpdated{
			ConversationID: conversationID,
			BlockState: &replication.BlockState{
				ActorBlockedTarget:  actorBlocksTarget,
				TargetBlockedActor:  targetBlocksActor,
				StateUpdatedAtNanos: time.Now().UnixNano(),
			},
		})

		// Emit for target
		s.replication.EmitUserEvent(ctx, targetID, replication.UserEventConversationStateUpdated{
			ConversationID: conversationID,
			BlockState: &replication.BlockState{
				ActorBlockedTarget:  actorBlocksTarget,
				TargetBlockedActor:  targetBlocksActor,
				StateUpdatedAtNanos: time.Now().UnixNano(),
			},
		})
	}

	return rows.Err()
}

// ListBlockedUsers retrieves users blocked by the current user
func (s *Service) ListBlockedUsers(ctx context.Context, userID string) ([]Profile, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id::text, COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''), COALESCE(u.primary_phone_e164, '')
		FROM users u
		INNER JOIN user_blocks ub ON ub.blocked_user_id = u.id
		WHERE ub.user_id = $1::uuid
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocked []Profile
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UserID, &p.DisplayName, &p.AvatarURL, &p.PhoneE164); err != nil {
			return nil, err
		}
		blocked = append(blocked, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return blocked, nil
}
