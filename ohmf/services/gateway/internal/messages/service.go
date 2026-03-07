package messages

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Message struct {
	MessageID      string         `json:"message_id"`
	ConversationID string         `json:"conversation_id"`
	SenderUserID   string         `json:"sender_user_id"`
	ContentType    string         `json:"content_type"`
	Content        map[string]any `json:"content"`
	ServerOrder    int64          `json:"server_order"`
	CreatedAt      string         `json:"created_at"`
}

var ErrConversationAccess = errors.New("conversation_access_denied")

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) Send(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any) (Message, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	if ok, err := s.hasMembership(ctx, tx, userID, conversationID); err != nil {
		return Message{}, err
	} else if !ok {
		return Message{}, ErrConversationAccess
	}

	var cached []byte
	err = tx.QueryRow(ctx, `
		SELECT response_payload
		FROM idempotency_keys
		WHERE actor_user_id = $1::uuid AND endpoint = '/v1/messages' AND key = $2 AND expires_at > now()
	`, userID, idemKey).Scan(&cached)
	if err == nil {
		var m Message
		if err := json.Unmarshal(cached, &m); err == nil {
			if err := tx.Commit(ctx); err != nil {
				return Message{}, err
			}
			return m, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Message{}, err
	}

	var next int64
	err = tx.QueryRow(ctx, `
		UPDATE conversation_counters
		SET next_server_order = next_server_order + 1, updated_at = now()
		WHERE conversation_id = $1::uuid
		RETURNING next_server_order - 1
	`, conversationID).Scan(&next)
	if err != nil {
		return Message{}, err
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return Message{}, err
	}

	var msgID string
	var created time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_user_id, content_type, content, server_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, $5)
		RETURNING id::text, created_at
	`, conversationID, userID, contentType, string(contentJSON), next).Scan(&msgID, &created)
	if err != nil {
		return Message{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE conversations
		SET last_message_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
	`, conversationID, msgID)
	if err != nil {
		return Message{}, err
	}

	msg := Message{
		MessageID:      msgID,
		ConversationID: conversationID,
		SenderUserID:   userID,
		ContentType:    contentType,
		Content:        content,
		ServerOrder:    next,
		CreatedAt:      created.UTC().Format(time.RFC3339),
	}
	msgPayload, _ := json.Marshal(msg)
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (actor_user_id, endpoint, key, response_payload, status_code, expires_at)
		VALUES ($1::uuid, '/v1/messages', $2, $3::jsonb, 201, now() + interval '24 hour')
		ON CONFLICT (actor_user_id, endpoint, key)
		DO UPDATE SET response_payload = EXCLUDED.response_payload, status_code = EXCLUDED.status_code
	`, userID, idemKey, string(msgPayload))
	if err != nil {
		return Message{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (s *Service) SendToPhone(ctx context.Context, userID, phoneE164, idemKey, contentType string, content map[string]any) (Message, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	conversationID, err := s.findOrCreatePhoneConversation(ctx, tx, userID, phoneE164)
	if err != nil {
		return Message{}, err
	}

	var cached []byte
	err = tx.QueryRow(ctx, `
		SELECT response_payload
		FROM idempotency_keys
		WHERE actor_user_id = $1::uuid AND endpoint = '/v1/messages/phone' AND key = $2 AND expires_at > now()
	`, userID, idemKey).Scan(&cached)
	if err == nil {
		var m Message
		if err := json.Unmarshal(cached, &m); err == nil {
			if err := tx.Commit(ctx); err != nil {
				return Message{}, err
			}
			return m, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Message{}, err
	}

	var next int64
	err = tx.QueryRow(ctx, `
		UPDATE conversation_counters
		SET next_server_order = next_server_order + 1, updated_at = now()
		WHERE conversation_id = $1::uuid
		RETURNING next_server_order - 1
	`, conversationID).Scan(&next)
	if err != nil {
		return Message{}, err
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return Message{}, err
	}

	var msgID string
	var created time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_user_id, content_type, content, transport, server_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, 'SMS', $5)
		RETURNING id::text, created_at
	`, conversationID, userID, contentType, string(contentJSON), next).Scan(&msgID, &created)
	if err != nil {
		return Message{}, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE conversations
		SET last_message_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
	`, conversationID, msgID)
	if err != nil {
		return Message{}, err
	}

	msg := Message{
		MessageID:      msgID,
		ConversationID: conversationID,
		SenderUserID:   userID,
		ContentType:    contentType,
		Content:        content,
		ServerOrder:    next,
		CreatedAt:      created.UTC().Format(time.RFC3339),
	}
	msgPayload, _ := json.Marshal(msg)
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (actor_user_id, endpoint, key, response_payload, status_code, expires_at)
		VALUES ($1::uuid, '/v1/messages/phone', $2, $3::jsonb, 201, now() + interval '24 hour')
		ON CONFLICT (actor_user_id, endpoint, key)
		DO UPDATE SET response_payload = EXCLUDED.response_payload, status_code = EXCLUDED.status_code
	`, userID, idemKey, string(msgPayload))
	if err != nil {
		return Message{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (s *Service) List(ctx context.Context, actor, conversationID string) ([]Message, error) {
	if ok, err := s.hasMembership(ctx, s.db, actor, conversationID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrConversationAccess
	}

	rows, err := s.db.Query(ctx, `
		SELECT id::text, conversation_id::text, sender_user_id::text, content_type, content, server_order, created_at
		FROM messages
		WHERE conversation_id = $1::uuid
		ORDER BY server_order ASC
		LIMIT 100
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Message, 0, 16)
	for rows.Next() {
		var m Message
		var contentRaw []byte
		var created time.Time
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.SenderUserID, &m.ContentType, &contentRaw, &m.ServerOrder, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(contentRaw, &m.Content)
		m.CreatedAt = created.UTC().Format(time.RFC3339)
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Service) MarkRead(ctx context.Context, actor, conversationID string, through int64) error {
	res, err := s.db.Exec(ctx, `
		UPDATE conversation_members
		SET last_read_server_order = GREATEST(last_read_server_order, $3)
		WHERE conversation_id = $1::uuid AND user_id = $2::uuid
	`, conversationID, actor, through)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrConversationAccess
	}
	return nil
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) hasMembership(ctx context.Context, q querier, userID, conversationID string) (bool, error) {
	var one int
	err := q.QueryRow(ctx, `
		SELECT 1
		FROM conversation_members
		WHERE conversation_id = $1::uuid AND user_id = $2::uuid
	`, conversationID, userID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) findOrCreatePhoneConversation(ctx context.Context, tx pgx.Tx, userID, phoneE164 string) (string, error) {
	var conversationID string
	err := tx.QueryRow(ctx, `
		SELECT c.id::text
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id
		JOIN conversation_external_members cem ON cem.conversation_id = c.id
		JOIN external_contacts ec ON ec.id = cem.external_contact_id
		WHERE c.type = 'PHONE_DM'
		  AND cm.user_id = $1::uuid
		  AND ec.phone_e164 = $2
		LIMIT 1
	`, userID, phoneE164).Scan(&conversationID)
	if err == nil {
		return conversationID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	var externalID string
	err = tx.QueryRow(ctx, `
		INSERT INTO external_contacts (phone_e164)
		VALUES ($1)
		ON CONFLICT (phone_e164) DO UPDATE SET phone_e164 = EXCLUDED.phone_e164
		RETURNING id::text
	`, phoneE164).Scan(&externalID)
	if err != nil {
		return "", err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO conversations (type, transport_policy)
		VALUES ('PHONE_DM', 'FORCE_SMS')
		RETURNING id::text
	`).Scan(&conversationID)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx, `INSERT INTO conversation_counters (conversation_id, next_server_order) VALUES ($1::uuid, 1)`, conversationID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'MEMBER')`, conversationID, userID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_external_members (conversation_id, external_contact_id) VALUES ($1::uuid, $2::uuid)`, conversationID, externalID)
	if err != nil {
		return "", err
	}
	return conversationID, nil
}
