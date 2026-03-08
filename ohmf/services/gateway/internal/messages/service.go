package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/limit"
)

type Message struct {
	MessageID      string         `json:"message_id"`
	ConversationID string         `json:"conversation_id"`
	SenderUserID   string         `json:"sender_user_id"`
	ContentType    string         `json:"content_type"`
	Content        map[string]any `json:"content"`
	Transport      string         `json:"transport"`
	ServerOrder    int64          `json:"server_order"`
	Status         string         `json:"status,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

type SendResult struct {
	Message      Message `json:"message"`
	Queued       bool    `json:"queued,omitempty"`
	AckTimeoutMS int64   `json:"ack_timeout_ms,omitempty"`
}

type Options struct {
	UseKafkaSend      bool
	UseCassandraReads bool
	AckTimeout        time.Duration
	Async             *AsyncPipeline
	Cassandra         *CassandraStore
	RateLimiter       *limit.TokenBucket
}

type Service struct {
	db                *pgxpool.Pool
	useKafkaSend      bool
	useCassandraReads bool
	ackTimeout        time.Duration
	async             *AsyncPipeline
	cassandra         *CassandraStore
	rateLimiter       *limit.TokenBucket
}

var (
	ErrConversationAccess = errors.New("conversation_access_denied")
	ErrRateLimited        = errors.New("rate_limited")
)

type RateLimitError struct {
	Scope      string
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string {
	return "rate_limited"
}

func NewService(db *pgxpool.Pool, opts Options) *Service {
	ackTimeout := opts.AckTimeout
	if ackTimeout <= 0 {
		ackTimeout = 2 * time.Second
	}
	return &Service{
		db:                db,
		useKafkaSend:      opts.UseKafkaSend,
		useCassandraReads: opts.UseCassandraReads,
		ackTimeout:        ackTimeout,
		async:             opts.Async,
		cassandra:         opts.Cassandra,
		rateLimiter:       opts.RateLimiter,
	}
}

func (s *Service) Send(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any, traceID string, ip string) (SendResult, error) {
	if err := s.enforceSendRate(ctx, userID, conversationID, ip); err != nil {
		return SendResult{}, err
	}
	if s.useKafkaSend && s.async != nil {
		return s.sendAsync(ctx, userID, conversationID, idemKey, contentType, content, "OHMF", "", "/v1/messages", traceID)
	}
	msg, err := s.sendSync(ctx, userID, conversationID, idemKey, contentType, content)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Message: msg}, nil
}

func (s *Service) SendToPhone(ctx context.Context, userID, phoneE164, idemKey, contentType string, content map[string]any, traceID string, ip string) (SendResult, error) {
	conversationID, err := s.ensurePhoneConversation(ctx, userID, phoneE164)
	if err != nil {
		return SendResult{}, err
	}
	if err := s.enforceSendRate(ctx, userID, conversationID, ip); err != nil {
		return SendResult{}, err
	}
	if s.useKafkaSend && s.async != nil {
		return s.sendAsync(ctx, userID, conversationID, idemKey, contentType, content, "SMS", phoneE164, "/v1/messages/phone", traceID)
	}
	msg, err := s.sendToPhoneSync(ctx, userID, phoneE164, idemKey, contentType, content)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Message: msg}, nil
}

func (s *Service) sendAsync(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any, transportIntent, phoneE164, endpoint, traceID string) (SendResult, error) {
	if ok, err := s.hasMembership(ctx, s.db, userID, conversationID); err != nil {
		return SendResult{}, err
	} else if !ok {
		return SendResult{}, ErrConversationAccess
	}

	cached, cachedStatus, err := s.loadIdempotency(ctx, userID, endpoint, idemKey)
	if err != nil {
		return SendResult{}, err
	}
	if cached != nil {
		if cachedStatus == 202 {
			return SendResult{
				Message:      *cached,
				Queued:       true,
				AckTimeoutMS: s.ackTimeout.Milliseconds(),
			}, nil
		}
		return SendResult{Message: *cached}, nil
	}

	evt := NewIngressEvent(userID, conversationID, endpoint, idemKey, contentType, transportIntent, phoneE164, traceID, content)
	provisional := evt.ProvisionalMessage()

	if err := s.upsertIdempotency(ctx, userID, endpoint, idemKey, provisional, 202); err != nil {
		return SendResult{}, err
	}
	if err := s.async.PublishIngress(ctx, evt); err != nil {
		return SendResult{}, err
	}

	ack, ok, err := s.async.WaitAck(ctx, evt.EventID, s.ackTimeout)
	if err != nil {
		return SendResult{}, err
	}
	if !ok {
		return SendResult{
			Message:      provisional,
			Queued:       true,
			AckTimeoutMS: s.ackTimeout.Milliseconds(),
		}, nil
	}

	msg := Message{
		MessageID:      ack.MessageID,
		ConversationID: ack.ConversationID,
		SenderUserID:   userID,
		ContentType:    contentType,
		Content:        content,
		Transport:      ack.Transport,
		ServerOrder:    ack.ServerOrder,
		Status:         ack.Status,
		CreatedAt:      time.UnixMilli(ack.PersistedAtMS).UTC().Format(time.RFC3339),
	}
	if err := s.upsertIdempotency(ctx, userID, endpoint, idemKey, msg, 201); err != nil {
		return SendResult{}, err
	}
	return SendResult{Message: msg}, nil
}

func (s *Service) List(ctx context.Context, actor, conversationID string) ([]Message, error) {
	if ok, err := s.hasMembership(ctx, s.db, actor, conversationID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrConversationAccess
	}

	if s.useCassandraReads && s.cassandra != nil {
		items, err := s.cassandra.ListConversation(ctx, conversationID, 100)
		if err == nil && len(items) > 0 {
			return items, nil
		}
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			m.id::text,
			m.conversation_id::text,
			m.sender_user_id::text,
			m.content_type,
			m.content,
			m.transport,
			m.server_order,
			CASE
				WHEN m.sender_user_id = $2::uuid AND m.transport = 'OTT' THEN
					CASE
						WHEN EXISTS (
							SELECT 1
							FROM conversation_members other
							WHERE other.conversation_id = m.conversation_id
							  AND other.user_id <> $2::uuid
						)
						AND NOT EXISTS (
							SELECT 1
							FROM conversation_members other
							WHERE other.conversation_id = m.conversation_id
							  AND other.user_id <> $2::uuid
							  AND other.last_read_server_order < m.server_order
						) THEN 'READ'
						ELSE 'SENT'
					END
				WHEN m.sender_user_id = $2::uuid AND m.transport = 'SMS' THEN 'SENT'
				ELSE ''
			END AS delivery_status,
			m.created_at
		FROM messages m
		WHERE m.conversation_id = $1::uuid
		ORDER BY server_order ASC
		LIMIT 100
	`, conversationID, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Message, 0, 16)
	for rows.Next() {
		var m Message
		var contentRaw []byte
		var status string
		var created time.Time
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.SenderUserID, &m.ContentType, &contentRaw, &m.Transport, &m.ServerOrder, &status, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(contentRaw, &m.Content)
		if status != "" {
			m.Status = status
		}
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
	_, _ = s.db.Exec(ctx, `
		UPDATE conversations
		SET updated_at = now()
		WHERE id = $1::uuid
	`, conversationID)
	return nil
}

func (s *Service) enforceSendRate(ctx context.Context, userID, conversationID, ip string) error {
	if s.rateLimiter == nil {
		return nil
	}
	if userID != "" {
		userDecision, err := s.rateLimiter.Allow(ctx, "rate:send:user:"+userID, 30, 10*time.Second, 60, 1)
		if err != nil {
			return err
		}
		if !userDecision.Allowed {
			return RateLimitError{Scope: "user", RetryAfter: userDecision.RetryAfter}
		}
	}
	if conversationID != "" {
		convDecision, err := s.rateLimiter.Allow(ctx, "rate:send:conversation:"+conversationID, 300, 10*time.Second, 500, 1)
		if err != nil {
			return err
		}
		if !convDecision.Allowed {
			return RateLimitError{Scope: "conversation", RetryAfter: convDecision.RetryAfter}
		}
	}
	if ip != "" {
		ipDecision, err := s.rateLimiter.Allow(ctx, "rate:send:ip:"+ip, 120, 10*time.Second, 240, 1)
		if err != nil {
			return err
		}
		if !ipDecision.Allowed {
			return RateLimitError{Scope: "ip", RetryAfter: ipDecision.RetryAfter}
		}
	}
	return nil
}

func (s *Service) loadIdempotency(ctx context.Context, userID, endpoint, idemKey string) (*Message, int, error) {
	var payload []byte
	var statusCode int
	err := s.db.QueryRow(ctx, `
		SELECT response_payload, COALESCE(status_code, 201)
		FROM idempotency_keys
		WHERE actor_user_id = $1::uuid AND endpoint = $2 AND key = $3 AND expires_at > now()
	`, userID, endpoint, idemKey).Scan(&payload, &statusCode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if len(payload) == 0 {
		return nil, statusCode, nil
	}
	var m Message
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, 0, err
	}
	return &m, statusCode, nil
}

func (s *Service) upsertIdempotency(ctx context.Context, userID, endpoint, idemKey string, msg Message, statusCode int) error {
	msgPayload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO idempotency_keys (actor_user_id, endpoint, key, response_payload, status_code, expires_at)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5, now() + interval '24 hour')
		ON CONFLICT (actor_user_id, endpoint, key)
		DO UPDATE SET response_payload = EXCLUDED.response_payload, status_code = EXCLUDED.status_code
	`, userID, endpoint, idemKey, string(msgPayload), statusCode)
	return err
}

func (s *Service) ensurePhoneConversation(ctx context.Context, userID, phoneE164 string) (string, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	conversationID, err := s.findOrCreatePhoneConversation(ctx, tx, userID, phoneE164)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return conversationID, nil
}

func (s *Service) sendSync(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any) (Message, error) {
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
		Transport:      "OTT",
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

func (s *Service) sendToPhoneSync(ctx context.Context, userID, phoneE164, idemKey, contentType string, content map[string]any) (Message, error) {
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
		Transport:      "SMS",
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
	var targetUserID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM users
		WHERE primary_phone_e164 = $1
		LIMIT 1
	`, phoneE164).Scan(&targetUserID)
	if err == nil && targetUserID != "" && targetUserID != userID {
		var dmConversationID string
		err = tx.QueryRow(ctx, `
			SELECT c.id::text
			FROM conversations c
			JOIN conversation_members me ON me.conversation_id = c.id AND me.user_id = $1::uuid
			JOIN conversation_members them ON them.conversation_id = c.id AND them.user_id = $2::uuid
			LEFT JOIN conversation_external_members cem ON cem.conversation_id = c.id
			WHERE cem.conversation_id IS NULL
			ORDER BY c.updated_at DESC
			LIMIT 1
		`, userID, targetUserID).Scan(&dmConversationID)
		if err == nil {
			return dmConversationID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO conversations (type, transport_policy)
			VALUES ('DM', 'AUTO')
			RETURNING id::text
		`).Scan(&dmConversationID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_counters (conversation_id, next_server_order) VALUES ($1::uuid, 1)`, dmConversationID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'MEMBER') ON CONFLICT (conversation_id, user_id) DO NOTHING`, dmConversationID, userID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'MEMBER') ON CONFLICT (conversation_id, user_id) DO NOTHING`, dmConversationID, targetUserID); err != nil {
			return "", err
		}
		return dmConversationID, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func (s *Service) PersistAck(ctx context.Context, userID, endpoint, idemKey string, msg Message) error {
	return s.upsertIdempotency(ctx, userID, endpoint, idemKey, msg, 201)
}

func buildTraceID(reqID string) string {
	if reqID == "" {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return reqID
}
