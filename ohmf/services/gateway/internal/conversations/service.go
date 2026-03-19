package conversations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/devicekeys"
	"ohmf/services/gateway/internal/phone"
	"ohmf/services/gateway/internal/replication"
)

type Conversation struct {
	ConversationID     string              `json:"conversation_id"`
	Type               string              `json:"type"`
	Title              string              `json:"title,omitempty"`
	AvatarURL          string              `json:"avatar_url,omitempty"`
	CreatorUserID      string              `json:"creator_user_id,omitempty"`
	EncryptionState    string              `json:"encryption_state,omitempty"`
	EncryptionEpoch    int                 `json:"encryption_epoch,omitempty"`
	Participants       []string            `json:"participants"`
	ExternalPhones     []string            `json:"external_phones,omitempty"`
	UpdatedAt          string              `json:"updated_at"`
	LastMessagePreview string              `json:"last_message_preview,omitempty"`
	UnreadCount        int64               `json:"unread_count,omitempty"`
	Nickname           string              `json:"nickname,omitempty"`
	Closed             bool                `json:"closed,omitempty"`
	Archived           bool                `json:"archived,omitempty"`
	Pinned             bool                `json:"pinned,omitempty"`
	MutedUntil         string              `json:"muted_until,omitempty"`
	Blocked            bool                `json:"blocked,omitempty"`
	BlockedByViewer    bool                `json:"blocked_by_viewer,omitempty"`
	BlockedByOther     bool                `json:"blocked_by_other,omitempty"`
	ThreadKeys         []map[string]string `json:"thread_keys,omitempty"`
}

type CreateRequest struct {
	Type            string
	Participants    []string
	ParticipantPhones []string
	Title           string
	AvatarURL       string
	EncryptionState string
}

var ErrNotFound = errors.New("conversation_not_found")
var ErrEncryptedConversationNotReady = errors.New("encrypted_conversation_not_ready")

type Service struct {
	db          *pgxpool.Pool
	replication *replication.Store
}

func NewService(db *pgxpool.Pool, store *replication.Store) *Service {
	return &Service{db: db, replication: store}
}

func (s *Service) CreateConversation(ctx context.Context, actor string, req CreateRequest) (Conversation, error) {
	t := strings.ToUpper(strings.TrimSpace(req.Type))
	if t == "" {
		t = "DM"
	}
	switch t {
	case "DM", "GROUP":
	default:
		return Conversation{}, errors.New("invalid_conversation_type")
	}
	participants, err := s.resolveParticipantUserIDs(ctx, req.Participants, req.ParticipantPhones)
	if err != nil {
		return Conversation{}, err
	}
	encryptionState := normalizeEncryptionState(t, req.EncryptionState)
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (type, transport_policy, title, avatar_url, created_by_user_id, encryption_state, encryption_epoch)
		VALUES ($1, 'AUTO', NULLIF($2, ''), NULLIF($3, ''), $4::uuid, $5, $6)
		RETURNING id::text
	`, t, strings.TrimSpace(req.Title), strings.TrimSpace(req.AvatarURL), actor, encryptionState, encryptionEpochForState(encryptionState)).Scan(&id)
	if err != nil {
		return Conversation{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_counters (conversation_id, next_server_order) VALUES ($1::uuid, 1)`, id)
	if err != nil {
		return Conversation{}, err
	}

	all := dedupeUsers(append([]string{actor}, participants...))
	for _, u := range all {
		role := "MEMBER"
		if u == actor && t == "GROUP" {
			role = "OWNER"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (conversation_id, user_id) DO NOTHING
		`, id, u, role)
		if err != nil {
			return Conversation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, actor, id)
}

func (s *Service) CreateDM(ctx context.Context, actor string, participants []string, t string) (Conversation, error) {
	return s.CreateConversation(ctx, actor, CreateRequest{Type: t, Participants: participants})
}

func (s *Service) FindOrCreatePhoneDM(ctx context.Context, actor, phoneE164 string) (Conversation, error) {
	if phoneE164 == "" {
		return Conversation{}, errors.New("phone_e164_required")
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var conversationID string
	err = tx.QueryRow(ctx, `
		SELECT c.id::text
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		JOIN conversation_external_members cem ON cem.conversation_id = c.id
		JOIN external_contacts ec ON ec.id = cem.external_contact_id
		WHERE c.type = 'PHONE_DM'
		  AND cm.user_id = $1::uuid
		  AND ec.phone_e164 = $2
		LIMIT 1
	`, actor, phoneE164).Scan(&conversationID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, err
		}

		var externalID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO external_contacts (phone_e164)
			VALUES ($1)
			ON CONFLICT (phone_e164) DO UPDATE SET phone_e164 = EXCLUDED.phone_e164
			RETURNING id::text
		`, phoneE164).Scan(&externalID); err != nil {
			return Conversation{}, err
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO conversations (type, transport_policy, encryption_state)
			VALUES ('PHONE_DM', 'AUTO', 'CARRIER_PLAINTEXT')
			RETURNING id::text
		`).Scan(&conversationID); err != nil {
			return Conversation{}, err
		}

		if _, err := tx.Exec(ctx, `INSERT INTO conversation_counters (conversation_id, next_server_order) VALUES ($1::uuid, 1)`, conversationID); err != nil {
			return Conversation{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'MEMBER')`, conversationID, actor); err != nil {
			return Conversation{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_external_members (conversation_id, external_contact_id) VALUES ($1::uuid, $2::uuid)`, conversationID, externalID); err != nil {
			return Conversation{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, actor, conversationID)
}

// List returns up to `limit` conversations for the actor, ordered by updated_at
// Desc; it also returns a nextCursor string (empty when no further pages).
func (s *Service) List(ctx context.Context, actor string, limit int) ([]Conversation, string, error) {
	if limit <= 0 {
		limit = 100
	}
	// fetch one extra row to detect whether more pages exist
	q := `
		SELECT c.id::text, c.type, c.updated_at
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		WHERE cm.user_id = $1::uuid
		ORDER BY c.updated_at DESC
		LIMIT $2
	`
	rows, err := s.db.Query(ctx, q, actor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var id, typ string
		var updated time.Time
		if err := rows.Scan(&id, &typ, &updated); err != nil {
			return nil, "", err
		}
		item, err := s.Get(ctx, actor, id)
		if err != nil {
			return nil, "", err
		}
		if item.Type == "" {
			item.Type = typ
		}
		if item.UpdatedAt == "" {
			item.UpdatedAt = updated.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	// if we fetched more than limit, compute cursor from the (limit)th item
	if len(out) > limit {
		// there is at least one more page; produce cursor from the last returned item's UpdatedAt
		last := out[limit-1]
		// trim to limit
		out = out[:limit]
		return out, last.UpdatedAt, nil
	}
	return out, "", nil
}

func (s *Service) ListProjected(ctx context.Context, actor string, limit int) ([]Conversation, string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `
		SELECT
			c.id::text,
			c.type,
			COALESCE(c.title, '') AS title,
			COALESCE(c.avatar_url, '') AS avatar_url,
			COALESCE(c.created_by_user_id::text, '') AS creator_user_id,
			COALESCE(c.encryption_state, 'PLAINTEXT') AS encryption_state,
			COALESCE(c.encryption_epoch, 0) AS encryption_epoch,
			COALESCE(ucs.updated_at, c.updated_at) AS updated_at,
			COALESCE(ucs.last_message_preview, '') AS last_message_preview,
			COALESCE(ucs.unread_count, 0) AS unread_count,
			COALESCE(ucs.nickname, '') AS nickname,
			COALESCE(ucs.is_closed, false) AS is_closed,
			COALESCE(ucs.is_archived, false) AS is_archived,
			COALESCE(ucs.is_pinned, false) AS is_pinned,
			ucs.muted_until,
			EXISTS (
				SELECT 1
				FROM conversation_members others
				JOIN user_blocks ub
				  ON ub.blocker_user_id = cm.user_id
				 AND ub.blocked_user_id = others.user_id
				WHERE others.conversation_id = c.id
				  AND others.user_id <> cm.user_id
			) AS blocked_by_viewer,
			EXISTS (
				SELECT 1
				FROM conversation_members others
				JOIN user_blocks ub
				  ON ub.blocker_user_id = others.user_id
				 AND ub.blocked_user_id = cm.user_id
				WHERE others.conversation_id = c.id
				  AND others.user_id <> cm.user_id
			) AS blocked_by_other
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		LEFT JOIN user_conversation_state ucs
		  ON ucs.conversation_id = c.id
		 AND ucs.user_id = cm.user_id
		WHERE cm.user_id = $1::uuid
		ORDER BY COALESCE(ucs.updated_at, c.updated_at) DESC
		LIMIT $2
	`, actor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var item Conversation
		var updated time.Time
		var mutedUntil sql.NullTime
		if err := rows.Scan(
			&item.ConversationID,
			&item.Type,
			&item.Title,
			&item.AvatarURL,
			&item.CreatorUserID,
			&item.EncryptionState,
			&item.EncryptionEpoch,
			&updated,
			&item.LastMessagePreview,
			&item.UnreadCount,
			&item.Nickname,
			&item.Closed,
			&item.Archived,
			&item.Pinned,
			&mutedUntil,
			&item.BlockedByViewer,
			&item.BlockedByOther,
		); err != nil {
			return nil, "", err
		}
		parts, externalPhones, err := s.participants(ctx, item.ConversationID)
		if err != nil {
			return nil, "", err
		}
		tkeys, err := s.threadKeys(ctx, item.ConversationID)
		if err != nil {
			return nil, "", err
		}
		item.Participants = parts
		item.ExternalPhones = externalPhones
		item.ThreadKeys = tkeys
		item.UpdatedAt = updated.UTC().Format(time.RFC3339)
		if mutedUntil.Valid {
			item.MutedUntil = mutedUntil.Time.UTC().Format(time.RFC3339Nano)
		}
		item.Blocked = item.BlockedByViewer || item.BlockedByOther
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) > limit {
		last := out[limit-1]
		out = out[:limit]
		return out, last.UpdatedAt, nil
	}
	return out, "", nil
}

func (s *Service) Get(ctx context.Context, actor, id string) (Conversation, error) {
	var out Conversation
	var updated time.Time
	var mutedUntil sql.NullTime
	err := s.db.QueryRow(ctx, `
		SELECT
			c.type,
			COALESCE(c.title, '') AS title,
			COALESCE(c.avatar_url, '') AS avatar_url,
			COALESCE(c.created_by_user_id::text, '') AS creator_user_id,
			COALESCE(c.encryption_state, 'PLAINTEXT') AS encryption_state,
			COALESCE(c.encryption_epoch, 0) AS encryption_epoch,
			COALESCE(ucs.updated_at, c.updated_at) AS updated_at,
			COALESCE(ucs.last_message_preview, '') AS last_message_preview,
			COALESCE(ucs.unread_count, 0) AS unread_count,
			COALESCE(ucs.nickname, '') AS nickname,
			COALESCE(ucs.is_closed, false) AS is_closed,
			COALESCE(ucs.is_archived, false) AS is_archived,
			COALESCE(ucs.is_pinned, false) AS is_pinned,
			ucs.muted_until,
			EXISTS (
				SELECT 1
				FROM conversation_members others
				JOIN user_blocks ub
				  ON ub.blocker_user_id = $2::uuid
				 AND ub.blocked_user_id = others.user_id
				WHERE others.conversation_id = c.id
				  AND others.user_id <> $2::uuid
			) AS blocked_by_viewer,
			EXISTS (
				SELECT 1
				FROM conversation_members others
				JOIN user_blocks ub
				  ON ub.blocker_user_id = others.user_id
				 AND ub.blocked_user_id = $2::uuid
				WHERE others.conversation_id = c.id
				  AND others.user_id <> $2::uuid
			) AS blocked_by_other
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		LEFT JOIN user_conversation_state ucs
		  ON ucs.conversation_id = c.id
		 AND ucs.user_id = cm.user_id
		WHERE c.id = $1::uuid AND cm.user_id = $2::uuid
	`, id, actor).Scan(
		&out.Type,
		&out.Title,
		&out.AvatarURL,
		&out.CreatorUserID,
		&out.EncryptionState,
		&out.EncryptionEpoch,
		&updated,
		&out.LastMessagePreview,
		&out.UnreadCount,
		&out.Nickname,
		&out.Closed,
		&out.Archived,
		&out.Pinned,
		&mutedUntil,
		&out.BlockedByViewer,
		&out.BlockedByOther,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	parts, externalPhones, err := s.participants(ctx, id)
	if err != nil {
		return Conversation{}, err
	}
	tkeys, err := s.threadKeys(ctx, id)
	if err != nil {
		return Conversation{}, err
	}
	out.ConversationID = id
	out.Participants = parts
	out.ExternalPhones = externalPhones
	out.ThreadKeys = tkeys
	out.UpdatedAt = updated.UTC().Format(time.RFC3339)
	if mutedUntil.Valid {
		out.MutedUntil = mutedUntil.Time.UTC().Format(time.RFC3339Nano)
	}
	out.Blocked = out.BlockedByViewer || out.BlockedByOther
	return out, nil
}

// threadKeys returns a slice of thread key maps like {"kind":"...","value":"..."}
func (s *Service) threadKeys(ctx context.Context, conversationID string) ([]map[string]string, error) {
	rows, err := s.db.Query(ctx, `SELECT kind, value FROM conversation_thread_keys WHERE conversation_id = $1::uuid ORDER BY created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]string, 0)
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"kind": kind, "value": value})
	}
	return out, rows.Err()
}

// SetThreadKeys upserts thread keys for a conversation. Actor must be a member.
func (s *Service) SetThreadKeys(ctx context.Context, actor, conversationID string, keys []map[string]string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid)`, conversationID, actor).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	// delete existing keys for this conversation
	if _, err := tx.Exec(ctx, `DELETE FROM conversation_thread_keys WHERE conversation_id = $1::uuid`, conversationID); err != nil {
		return err
	}
	// insert provided keys
	for _, k := range keys {
		kind := k["kind"]
		value := k["value"]
		if kind == "" || value == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_thread_keys (conversation_id, kind, value) VALUES ($1::uuid, $2, $3)`, conversationID, kind, value); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Service) UpdatePreferences(ctx context.Context, actor, conversationID string, nickname *string, closed, archived, pinned *bool, mutedUntil *string) (Conversation, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid)`, conversationID, actor).Scan(&exists); err != nil {
		return Conversation{}, err
	}
	if !exists {
		return Conversation{}, ErrNotFound
	}

	if err := s.ensureConversationStateTx(ctx, tx, actor, conversationID); err != nil {
		return Conversation{}, err
	}

	var nicknameArg any
	if nickname != nil {
		trimmed := strings.TrimSpace(*nickname)
		if trimmed == "" {
			nicknameArg = nil
		} else {
			nicknameArg = trimmed
		}
	}
	var closedArg any
	if closed != nil {
		closedArg = *closed
	}
	var archivedArg any
	if archived != nil {
		archivedArg = *archived
	}
	var pinnedArg any
	if pinned != nil {
		pinnedArg = *pinned
	}
	var mutedUntilArg any
	if mutedUntil != nil {
		trimmed := strings.TrimSpace(*mutedUntil)
		if trimmed == "" {
			mutedUntilArg = nil
		} else {
			t, err := time.Parse(time.RFC3339Nano, trimmed)
			if err != nil {
				return Conversation{}, err
			}
			mutedUntilArg = t.UTC()
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE user_conversation_state
		SET nickname = CASE
		      WHEN $3::text IS NULL AND $5::bool THEN NULL
		      WHEN $3::text IS NULL THEN nickname
		      ELSE $3::text
		    END,
		    is_closed = COALESCE($4::bool, is_closed),
		    is_archived = COALESCE($6::bool, is_archived),
		    is_pinned = COALESCE($7::bool, is_pinned),
		    muted_until = CASE
		      WHEN $8::bool THEN $9::timestamptz
		      ELSE muted_until
		    END,
		    updated_at = now()
		WHERE user_id = $1::uuid
		  AND conversation_id = $2::uuid
	`, actor, conversationID, nicknameArg, closedArg, nickname != nil, archivedArg, pinnedArg, mutedUntil != nil, mutedUntilArg); err != nil {
		return Conversation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}

	updated, err := s.Get(ctx, actor, conversationID)
	if err != nil {
		return Conversation{}, err
	}
	if s.replication != nil {
		_, _ = s.replication.EmitUserEvent(ctx, actor, conversationID, replication.UserEventConversationStateUpdated, map[string]any{
			"conversation_id":   conversationID,
			"nickname":          updated.Nickname,
			"closed":            updated.Closed,
			"archived":          updated.Archived,
			"pinned":            updated.Pinned,
			"muted_until":       updated.MutedUntil,
			"blocked":           updated.Blocked,
			"blocked_by_viewer": updated.BlockedByViewer,
			"blocked_by_other":  updated.BlockedByOther,
			"updated_at":        updated.UpdatedAt,
		})
	}
	return updated, nil
}

func (s *Service) UpdateMetadata(ctx context.Context, actor, conversationID string, title, avatarURL, encryptionState *string) (Conversation, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid)`, conversationID, actor).Scan(&exists); err != nil {
		return Conversation{}, err
	}
	if !exists {
		return Conversation{}, ErrNotFound
	}

	var conversationType string
	var currentEncryptionState string
	if err := tx.QueryRow(ctx, `
		SELECT type, COALESCE(encryption_state, 'PLAINTEXT')
		FROM conversations
		WHERE id = $1::uuid
	`, conversationID).Scan(&conversationType, &currentEncryptionState); err != nil {
		return Conversation{}, err
	}

	var titleArg any
	if title != nil {
		titleArg = strings.TrimSpace(*title)
	}
	var avatarArg any
	if avatarURL != nil {
		avatarArg = strings.TrimSpace(*avatarURL)
	}

	encryptionArg := ""
	bumpEpoch := false
	if encryptionState != nil {
		encryptionArg = normalizeEncryptionState(conversationType, *encryptionState)
		if encryptionArg == "ENCRYPTED" {
			if strings.ToUpper(strings.TrimSpace(conversationType)) != "DM" {
				return Conversation{}, ErrEncryptedConversationNotReady
			}
			ready, err := s.encryptionReadyForDM(ctx, tx, conversationID)
			if err != nil {
				return Conversation{}, err
			}
			if !ready {
				return Conversation{}, ErrEncryptedConversationNotReady
			}
			bumpEpoch = strings.ToUpper(strings.TrimSpace(currentEncryptionState)) != "ENCRYPTED"
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET title = CASE WHEN $2::bool THEN NULLIF($3::text, '') ELSE title END,
		    avatar_url = CASE WHEN $4::bool THEN NULLIF($5::text, '') ELSE avatar_url END,
		    encryption_state = CASE WHEN $6::bool THEN $7 ELSE encryption_state END,
		    encryption_epoch = CASE
		      WHEN $6::bool AND $8::bool THEN encryption_epoch + 1
		      ELSE encryption_epoch
		    END,
		    updated_at = now()
		WHERE id = $1::uuid
	`, conversationID, title != nil, titleArg, avatarURL != nil, avatarArg, encryptionState != nil, encryptionArg, bumpEpoch); err != nil {
		return Conversation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, actor, conversationID)
}

func (s *Service) encryptionReadyForDM(ctx context.Context, tx pgx.Tx, conversationID string) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id::text
		FROM conversation_members
		WHERE conversation_id = $1::uuid
		ORDER BY joined_at ASC
	`, conversationID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	members := make([]string, 0, 2)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return false, err
		}
		members = append(members, userID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(members) != 2 {
		return false, nil
	}

	for _, userID := range members {
		var ready bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM device_identity_keys dik
				JOIN devices d ON d.id = dik.device_id
				WHERE dik.user_id = $1::uuid
				  AND dik.bundle_version = $2
				  AND d.capabilities @> $3::text[]
			)
		`, userID, devicekeys.BundleVersionSignalV1, []string{devicekeys.RequiredDeviceCapability}).Scan(&ready); err != nil {
			return false, err
		}
		if !ready {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) AddMembers(ctx context.Context, actor, conversationID string, userIDs []string) (Conversation, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var role, conversationType string
	if err := tx.QueryRow(ctx, `
		SELECT cm.role, c.type
		FROM conversation_members cm
		JOIN conversations c ON c.id = cm.conversation_id
		WHERE cm.conversation_id = $1::uuid AND cm.user_id = $2::uuid
	`, conversationID, actor).Scan(&role, &conversationType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	if conversationType != "GROUP" {
		return Conversation{}, errors.New("member_changes_not_supported")
	}
	if role != "OWNER" {
		return Conversation{}, errors.New("forbidden")
	}

	for _, userID := range dedupeUsers(userIDs) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1::uuid, $2::uuid, 'MEMBER')
			ON CONFLICT (conversation_id, user_id) DO NOTHING
		`, conversationID, userID); err != nil {
			return Conversation{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1::uuid`, conversationID); err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, actor, conversationID)
}

func (s *Service) RemoveMember(ctx context.Context, actor, conversationID, targetUserID string) (Conversation, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var role, conversationType string
	if err := tx.QueryRow(ctx, `
		SELECT cm.role, c.type
		FROM conversation_members cm
		JOIN conversations c ON c.id = cm.conversation_id
		WHERE cm.conversation_id = $1::uuid AND cm.user_id = $2::uuid
	`, conversationID, actor).Scan(&role, &conversationType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrNotFound
		}
		return Conversation{}, err
	}
	if conversationType != "GROUP" {
		return Conversation{}, errors.New("member_changes_not_supported")
	}
	if targetUserID != actor && role != "OWNER" {
		return Conversation{}, errors.New("forbidden")
	}

	if _, err := tx.Exec(ctx, `DELETE FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid`, conversationID, targetUserID); err != nil {
		return Conversation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1::uuid`, conversationID); err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	if targetUserID == actor {
		return Conversation{ConversationID: conversationID, Type: conversationType}, nil
	}
	return s.Get(ctx, actor, conversationID)
}

func (s *Service) UpdateTransportPolicy(ctx context.Context, actor, conversationID, policy string) (Conversation, error) {
	// validate policy
	switch policy {
	case "AUTO", "FORCE_OTT", "FORCE_SMS", "FORCE_MMS", "BLOCK_CARRIER_RELAY":
		// ok
	default:
		return Conversation{}, errors.New("invalid_transport_policy")
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	// check membership
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1::uuid AND user_id = $2::uuid)`, conversationID, actor).Scan(&exists)
	if err != nil {
		return Conversation{}, err
	}
	if !exists {
		return Conversation{}, ErrNotFound
	}

	_, err = tx.Exec(ctx, `UPDATE conversations SET transport_policy = $2, updated_at = now() WHERE id = $1::uuid`, conversationID, policy)
	if err != nil {
		return Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return s.Get(ctx, actor, conversationID)
}

func (s *Service) participants(ctx context.Context, conversationID string) ([]string, []string, error) {
	rows, err := s.db.Query(ctx, `SELECT user_id::text FROM conversation_members WHERE conversation_id = $1::uuid ORDER BY joined_at`, conversationID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		items = append(items, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	extRows, err := s.db.Query(ctx, `
		SELECT ec.phone_e164
		FROM conversation_external_members cem
		JOIN external_contacts ec ON ec.id = cem.external_contact_id
		WHERE cem.conversation_id = $1::uuid
		ORDER BY cem.joined_at
	`, conversationID)
	if err != nil {
		return nil, nil, err
	}
	defer extRows.Close()
	externalPhones := make([]string, 0, 1)
	for extRows.Next() {
		var p string
		if err := extRows.Scan(&p); err != nil {
			return nil, nil, err
		}
		externalPhones = append(externalPhones, p)
	}
	return items, externalPhones, extRows.Err()
}

func (s *Service) resolveParticipantUserIDs(ctx context.Context, userIDs, participantPhones []string) ([]string, error) {
	out := dedupeUsers(userIDs)
	phones, err := normalizeParticipantPhones(participantPhones)
	if err != nil {
		return nil, err
	}
	if len(phones) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, primary_phone_e164
		FROM users
		WHERE primary_phone_e164 = ANY($1::text[])
	`, phones)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resolved := make(map[string]string, len(phones))
	for rows.Next() {
		var userID string
		var phoneE164 string
		if err := rows.Scan(&userID, &phoneE164); err != nil {
			return nil, err
		}
		resolved[phoneE164] = userID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, phoneE164 := range phones {
		userID, ok := resolved[phoneE164]
		if !ok {
			return nil, fmt.Errorf("unknown participant phone: %s", phoneE164)
		}
		out = append(out, userID)
	}
	return dedupeUsers(out), nil
}

func dedupeUsers(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it == "" {
			continue
		}
		if _, err := uuid.Parse(it); err != nil {
			continue
		}
		if _, ok := seen[it]; ok {
			continue
		}
		seen[it] = struct{}{}
		out = append(out, it)
	}
	return out
}

func normalizeParticipantPhones(items []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		normalized := phone.NormalizeE164(item)
		if normalized == "" {
			if strings.TrimSpace(item) == "" {
				continue
			}
			return nil, fmt.Errorf("invalid participant phone: %s", strings.TrimSpace(item))
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func (s *Service) ensureConversationStateTx(ctx context.Context, tx pgx.Tx, actor, conversationID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_conversation_state (
			user_id,
			conversation_id,
			last_message_id,
			last_message_preview,
			last_message_at,
			unread_count,
			last_read_server_order,
			last_delivered_server_order,
			updated_at
		)
		SELECT
			cm.user_id,
			c.id,
			c.last_message_id,
			'',
			c.updated_at,
			0,
			cm.last_read_server_order,
			cm.last_delivered_server_order,
			now()
		FROM conversations c
		JOIN conversation_members cm
		  ON cm.conversation_id = c.id
		WHERE c.id = $2::uuid
		  AND cm.user_id = $1::uuid
		ON CONFLICT (user_id, conversation_id) DO NOTHING
	`, actor, conversationID)
	return err
}

func normalizeEncryptionState(conversationType, raw string) string {
	if strings.ToUpper(strings.TrimSpace(conversationType)) == "PHONE_DM" {
		return "CARRIER_PLAINTEXT"
	}
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	switch normalized {
	case "", "PLAINTEXT":
		return "PLAINTEXT"
	case "PENDING_E2EE", "ENCRYPTED", "CARRIER_PLAINTEXT":
		return normalized
	default:
		return "PLAINTEXT"
	}
}

func encryptionEpochForState(state string) int {
	if strings.ToUpper(strings.TrimSpace(state)) == "ENCRYPTED" {
		return 1
	}
	return 0
}
