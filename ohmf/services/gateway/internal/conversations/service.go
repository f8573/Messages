package conversations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Conversation struct {
	ConversationID string              `json:"conversation_id"`
	Type           string              `json:"type"`
	Participants   []string            `json:"participants"`
	ExternalPhones []string            `json:"external_phones,omitempty"`
	UpdatedAt      string              `json:"updated_at"`
	ThreadKeys     []map[string]string `json:"thread_keys,omitempty"`
}

var ErrNotFound = errors.New("conversation_not_found")

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) CreateDM(ctx context.Context, actor string, participants []string, t string) (Conversation, error) {
	if t == "" {
		t = "DM"
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (type, transport_policy)
		VALUES ($1, 'AUTO')
		RETURNING id::text
	`, t).Scan(&id)
	if err != nil {
		return Conversation{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_counters (conversation_id, next_server_order) VALUES ($1::uuid, 1)`, id)
	if err != nil {
		return Conversation{}, err
	}

	all := dedupeUsers(append([]string{actor}, participants...))
	for _, u := range all {
		_, err = tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role)
			VALUES ($1::uuid, $2::uuid, 'MEMBER')
			ON CONFLICT (conversation_id, user_id) DO NOTHING
		`, id, u)
		if err != nil {
			return Conversation{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, err
	}
	return Conversation{ConversationID: id, Type: t, Participants: all, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}, nil
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
			INSERT INTO conversations (type, transport_policy)
			VALUES ('PHONE_DM', 'AUTO')
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
		parts, externalPhones, err := s.participants(ctx, id)
		if err != nil {
			return nil, "", err
		}
		tkeys, err := s.threadKeys(ctx, id)
		if err != nil {
			return nil, "", err
		}
		out = append(out, Conversation{ConversationID: id, Type: typ, Participants: parts, ExternalPhones: externalPhones, UpdatedAt: updated.UTC().Format(time.RFC3339), ThreadKeys: tkeys})
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

func (s *Service) Get(ctx context.Context, actor, id string) (Conversation, error) {
	var typ string
	var updated time.Time
	err := s.db.QueryRow(ctx, `
		SELECT c.type, c.updated_at
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		WHERE c.id = $1::uuid AND cm.user_id = $2::uuid
	`, id, actor).Scan(&typ, &updated)
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
	return Conversation{ConversationID: id, Type: typ, Participants: parts, ExternalPhones: externalPhones, UpdatedAt: updated.UTC().Format(time.RFC3339), ThreadKeys: tkeys}, nil
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
