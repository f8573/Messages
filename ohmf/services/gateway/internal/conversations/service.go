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
	ConversationID string   `json:"conversation_id"`
	Type           string   `json:"type"`
	Participants   []string `json:"participants"`
	ExternalPhones []string `json:"external_phones,omitempty"`
	UpdatedAt      string   `json:"updated_at"`
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

func (s *Service) List(ctx context.Context, actor string) ([]Conversation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id::text, c.type, c.updated_at
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		WHERE cm.user_id = $1::uuid
		ORDER BY c.updated_at DESC
		LIMIT 100
	`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var id, typ string
		var updated time.Time
		if err := rows.Scan(&id, &typ, &updated); err != nil {
			return nil, err
		}
		parts, externalPhones, err := s.participants(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, Conversation{ConversationID: id, Type: typ, Participants: parts, ExternalPhones: externalPhones, UpdatedAt: updated.UTC().Format(time.RFC3339)})
	}
	return out, rows.Err()
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
	return Conversation{ConversationID: id, Type: typ, Participants: parts, ExternalPhones: externalPhones, UpdatedAt: updated.UTC().Format(time.RFC3339)}, nil
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
