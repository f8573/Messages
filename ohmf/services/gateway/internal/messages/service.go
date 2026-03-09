package messages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/limit"
	"ohmf/services/gateway/internal/middleware"
)

type Message struct {
	MessageID      string         `json:"message_id"`
	ConversationID string         `json:"conversation_id"`
	SenderUserID   string         `json:"sender_user_id"`
	ContentType    string         `json:"content_type"`
	Content        map[string]any `json:"content"`
	ClientGeneratedID string      `json:"client_generated_id,omitempty"`
	Transport      string         `json:"transport"`
	ServerOrder    int64          `json:"server_order"`
	Status         string         `json:"status,omitempty"`
	CreatedAt      string         `json:"created_at"`
	Source         string         `json:"source,omitempty"`
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

type DeliveryRecord struct {
	ID                string `json:"id"`
	MessageID         string `json:"message_id"`
	RecipientUserID   string `json:"recipient_user_id,omitempty"`
	RecipientDeviceID string `json:"recipient_device_id,omitempty"`
	RecipientPhone    string `json:"recipient_phone_e164,omitempty"`
	Transport         string `json:"transport"`
	State             string `json:"state"`
	Provider          string `json:"provider,omitempty"`
	SubmittedAt       string `json:"submitted_at,omitempty"`
	UpdatedAt         string `json:"updated_at"`
	FailureCode       string `json:"failure_code,omitempty"`
}

// Redact removes personal content from a message while preserving identifiers
// required for timeline integrity. Only the original sender may redact their
// own message. Redaction replaces the message content with an empty JSON
// object, sets `redacted_at` and `visibility_state` = 'REDACTED'.
func (s *Service) Redact(ctx context.Context, actorUserID, messageID string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var senderID string
	var convID string
	err = tx.QueryRow(ctx, `SELECT sender_user_id::text, conversation_id::text FROM messages WHERE id = $1`, messageID).Scan(&senderID, &convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("message_not_found")
		}
		return err
	}
	if senderID != actorUserID {
		// Only sender may redact (policy). Future: allow admins or owners.
		return fmt.Errorf("forbidden")
	}

	// Perform redaction: empty content, set redacted_at and visibility state.
	_, err = tx.Exec(ctx, `
		UPDATE messages
		SET content = '{}'::jsonb,
			redacted_at = now(),
			visibility_state = 'REDACTED',
			updated_at = now()
		WHERE id = $1
	`, messageID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1::uuid`, convID)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// DeleteMessage performs a privacy-aware deletion flow for a message.
// Only the original sender may delete their message.
func (s *Service) DeleteMessage(ctx context.Context, actorUserID, messageID string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var senderID string
	var convID string
	err = tx.QueryRow(ctx, `SELECT sender_user_id::text, conversation_id::text FROM messages WHERE id = $1`, messageID).Scan(&senderID, &convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("message_not_found")
		}
		return err
	}
	if senderID != actorUserID {
		return fmt.Errorf("forbidden")
	}

	// mark deleted_at and redact content fields and set visibility to SOFT_DELETED
	_, err = tx.Exec(ctx, `
		UPDATE messages
		SET content = NULL,
			redacted_at = now(),
			deleted_at = now(),
			visibility_state = 'SOFT_DELETED',
			updated_at = now()
		WHERE id = $1
	`, messageID)
	if err != nil {
		return err
	}

	// collect attachments to instruct media service to delete
	rows, err := tx.Query(ctx, `SELECT attachment_id::text FROM attachments WHERE message_id = $1`, messageID)
	if err != nil {
		return err
	}
	var attachments []string
	for rows.Next() {
		var aid string
		if err := rows.Scan(&aid); err != nil {
			rows.Close()
			return err
		}
		attachments = append(attachments, aid)
	}
	rows.Close()

	if len(attachments) > 0 {
		// delete attachment rows (actual object deletion handled by media service)
		if _, err := tx.Exec(ctx, `DELETE FROM attachments WHERE message_id = $1`, messageID); err != nil {
			return err
		}
	}

	// update conversation updated_at
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1::uuid`, convID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// emit a deletion envelope for downstream processors (media purge, index invalidation)
	if s.async != nil {
		env := Envelope{
			SpecVersion:    "2026-03-01",
			EventID:        messageID,
			EventType:      "message.delete",
			IssuedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			ConversationID: convID,
			Transport:      "OTT",
			IdempotencyKey: "",
			Payload:        []byte(fmt.Sprintf(`{"message_id":"%s","attachments":%q}`, messageID, attachments)),
			Actor:          &Actor{UserID: actorUserID},
			Trace:          &Trace{},
		}
		_ = s.async.PublishEnvelope(context.Background(), convID, env)
	}
	return nil
}

// RecordDelivery inserts or updates a delivery record for a message.
func (s *Service) RecordDelivery(ctx context.Context, messageID string, dr DeliveryRecord) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO message_deliveries (id, message_id, recipient_user_id, recipient_device_id, recipient_phone_e164, transport, state, provider, submitted_at, updated_at, failure_code)
		VALUES (gen_random_uuid(), $1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, NULLIF($4, ''), $5, $6, NULLIF($7, ''), NULLIF($8, '')::timestamptz, now(), NULLIF($9, ''))
	`, messageID, dr.RecipientUserID, dr.RecipientDeviceID, dr.RecipientPhone, dr.Transport, dr.State, dr.Provider, nullableTimestamp(dr.SubmittedAt), dr.FailureCode)
	return err
}

// AddReaction adds a reaction emoji by a user to a message. Reactions are
// separate records and do not mutate the original message content.
func (s *Service) AddReaction(ctx context.Context, actorUserID, messageID, emoji string) error {
	// ensure membership
	var convID string
	if err := s.db.QueryRow(ctx, `SELECT conversation_id::text FROM messages WHERE id = $1`, messageID).Scan(&convID); err != nil {
		return err
	}
	ok, err := s.hasMembership(ctx, s.db, actorUserID, convID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConversationAccess
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO message_reactions (message_id, user_id, emoji, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT DO NOTHING
	`, messageID, actorUserID, emoji)
	return err
}

// RemoveReaction deletes a reaction record.
func (s *Service) RemoveReaction(ctx context.Context, actorUserID, messageID, emoji string) error {
	var convID string
	if err := s.db.QueryRow(ctx, `SELECT conversation_id::text FROM messages WHERE id = $1`, messageID).Scan(&convID); err != nil {
		return err
	}
	ok, err := s.hasMembership(ctx, s.db, actorUserID, convID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConversationAccess
	}
	_, err = s.db.Exec(ctx, `
		DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3
	`, messageID, actorUserID, emoji)
	return err
}

// ListReactions returns aggregated reaction counts for a message.
func (s *Service) ListReactions(ctx context.Context, messageID string) (map[string]int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT emoji, count(*) FROM message_reactions WHERE message_id = $1 GROUP BY emoji
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var emoji string
		var cnt int64
		if err := rows.Scan(&emoji, &cnt); err != nil {
			return nil, err
		}
		out[emoji] = cnt
	}
	return out, rows.Err()
}

func nullableTimestamp(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// ListDeliveries returns delivery records for a message.
func (s *Service) ListDeliveries(ctx context.Context, messageID string) ([]DeliveryRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, message_id::text, recipient_user_id::text, recipient_device_id::text, recipient_phone_e164, transport, state, provider, submitted_at, updated_at, failure_code
		FROM message_deliveries WHERE message_id = $1::uuid ORDER BY updated_at ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DeliveryRecord, 0)
	for rows.Next() {
		var d DeliveryRecord
		var ru, rd sql.NullString
		var submitted sql.NullTime
		var updated time.Time
		if err := rows.Scan(&d.ID, &d.MessageID, &ru, &rd, &d.RecipientPhone, &d.Transport, &d.State, &d.Provider, &submitted, &updated, &d.FailureCode); err != nil {
			return nil, err
		}
		if ru.Valid {
			d.RecipientUserID = ru.String
		}
		if rd.Valid {
			d.RecipientDeviceID = rd.String
		}
		if submitted.Valid {
			d.SubmittedAt = submitted.Time.UTC().Format(time.RFC3339)
		}
		d.UpdatedAt = updated.UTC().Format(time.RFC3339)
		out = append(out, d)
	}
	return out, rows.Err()
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

func (s *Service) Send(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any, clientGeneratedID string, traceID string, ip string) (SendResult, error) {
	if err := s.enforceSendRate(ctx, userID, conversationID, ip); err != nil {
		return SendResult{}, err
	}
	if s.useKafkaSend && s.async != nil {
		return s.sendAsync(ctx, userID, conversationID, idemKey, contentType, content, clientGeneratedID, "OHMF", "", "/v1/messages", traceID)
	}
	msg, err := s.sendSync(ctx, userID, conversationID, idemKey, contentType, content, clientGeneratedID)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Message: msg}, nil
}

func (s *Service) SendToPhone(ctx context.Context, userID, phoneE164, idemKey, contentType string, content map[string]any, clientGeneratedID string, traceID string, ip string) (SendResult, error) {
	conversationID, err := s.ensurePhoneConversation(ctx, userID, phoneE164)
	if err != nil {
		return SendResult{}, err
	}
	if err := s.enforceSendRate(ctx, userID, conversationID, ip); err != nil {
		return SendResult{}, err
	}
	if s.useKafkaSend && s.async != nil {
		return s.sendAsync(ctx, userID, conversationID, idemKey, contentType, content, clientGeneratedID, "SMS", phoneE164, "/v1/messages/phone", traceID)
	}
	msg, err := s.sendToPhoneSync(ctx, userID, phoneE164, idemKey, contentType, content, clientGeneratedID)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{Message: msg}, nil
}

func (s *Service) sendAsync(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any, clientGeneratedID, transportIntent, phoneE164, endpoint, traceID string) (SendResult, error) {
	if ok, err := s.hasMembership(ctx, s.db, userID, conversationID); err != nil {
		return SendResult{}, err
	} else if !ok {
		return SendResult{}, ErrConversationAccess
	}

	// Enforce block rules for async path as well
	if blocked, blocker, err := s.checkBlockedRecipients(ctx, s.db, userID, conversationID); err != nil {
		return SendResult{}, err
	} else if blocked {
		return SendResult{}, fmt.Errorf("blocked_by:%s", blocker)
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

	evt := NewIngressEvent(userID, conversationID, endpoint, idemKey, contentType, transportIntent, phoneE164, clientGeneratedID, traceID, content)
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
		MessageID:         ack.MessageID,
		ConversationID:    ack.ConversationID,
		SenderUserID:      userID,
		ContentType:       contentType,
		Content:           content,
		ClientGeneratedID: provisional.ClientGeneratedID,
		Transport:         ack.Transport,
		ServerOrder:       ack.ServerOrder,
		Status:            ack.Status,
		CreatedAt:         time.UnixMilli(ack.PersistedAtMS).UTC().Format(time.RFC3339),
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
			m.client_generated_id,
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
		var clientGenID sql.NullString
		var status string
		var created time.Time
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.SenderUserID, &m.ContentType, &contentRaw, &clientGenID, &m.Transport, &m.ServerOrder, &status, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(contentRaw, &m.Content)
		if clientGenID.Valid {
			m.ClientGeneratedID = clientGenID.String
		}
		if status != "" {
			m.Status = status
		}
		m.CreatedAt = created.UTC().Format(time.RFC3339)
		items = append(items, m)
	}
	return items, rows.Err()
}

// ListUnified returns a merged timeline combining canonical server messages and
// optional mirrored carrier messages for a conversation. Items are ordered by
// created_at ascending to preserve display chronology.
func (s *Service) ListUnified(ctx context.Context, actor, conversationID string, limit int) ([]Message, error) {
	if ok, err := s.hasMembership(ctx, s.db, actor, conversationID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrConversationAccess
	}

	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// load canonical messages (up to limit)
	rows, err := s.db.Query(ctx, `
		SELECT
			m.id::text,
			m.conversation_id::text,
			m.sender_user_id::text,
			m.content_type,
			m.content,
			m.client_generated_id,
			m.transport,
			m.server_order,
			m.created_at
		FROM messages m
		WHERE m.conversation_id = $1::uuid
		ORDER BY server_order ASC
		LIMIT $2
	`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Message, 0, 32)
	var messageIDs []string
	for rows.Next() {
		var m Message
		var contentRaw []byte
		var clientGenID sql.NullString
		var created time.Time
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.SenderUserID, &m.ContentType, &contentRaw, &clientGenID, &m.Transport, &m.ServerOrder, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(contentRaw, &m.Content)
		if clientGenID.Valid {
			m.ClientGeneratedID = clientGenID.String
		}
		m.CreatedAt = created.UTC().Format(time.RFC3339)
		m.Source = "SERVER"
		items = append(items, m)
		messageIDs = append(messageIDs, m.MessageID)
	}

	// fetch thread keys for this conversation
	tkRows, err := s.db.Query(ctx, `SELECT value FROM conversation_thread_keys WHERE conversation_id = $1::uuid`, conversationID)
	if err == nil {
		defer tkRows.Close()
		var keys []string
		for tkRows.Next() {
			var v string
			if err := tkRows.Scan(&v); err == nil {
				keys = append(keys, v)
			}
		}
		// query carrier messages matching thread keys or linked to server messages
		if len(keys) > 0 || len(messageIDs) > 0 {
			// build query dynamically
			query := `SELECT id::text, device_id::text, thread_key, carrier_message_id, direction, transport, text, media_json, created_at, device_authoritative, server_message_id::text, raw_payload FROM carrier_messages WHERE `
			args := []any{}
			clauses := []string{}
			idx := 1
			if len(keys) > 0 {
				clauses = append(clauses, "thread_key = ANY($"+fmt.Sprint(idx)+"::text[])")
				args = append(args, keys)
				idx++
			}
			if len(messageIDs) > 0 {
				clauses = append(clauses, "server_message_id = ANY($"+fmt.Sprint(idx)+"::uuid[])")
				args = append(args, messageIDs)
				idx++
			}
			query += "(" + clauses[0]
			for i := 1; i < len(clauses); i++ {
				query += " OR " + clauses[i]
			}
			query += ") ORDER BY created_at ASC LIMIT $" + fmt.Sprint(idx)
			args = append(args, limit)

			crows, err := s.db.Query(ctx, query, args...)
			if err == nil {
				defer crows.Close()
				for crows.Next() {
					var id, deviceID, threadKey, carrierMessageID, direction, transport, text string
					var mediaJSON []byte
					var created time.Time
					var deviceAuth bool
					var serverMsgID sql.NullString
					var rawPayload []byte
					if err := crows.Scan(&id, &deviceID, &threadKey, &carrierMessageID, &direction, &transport, &text, &mediaJSON, &created, &deviceAuth, &serverMsgID, &rawPayload); err != nil {
						return nil, err
					}
					// build content map
					content := make(map[string]any)
					if text != "" {
						content["text"] = text
					}
					if len(mediaJSON) > 0 {
						var mj any
						_ = json.Unmarshal(mediaJSON, &mj)
						content["media"] = mj
					}
					m := Message{
						MessageID:      id,
						ConversationID: conversationID,
						SenderUserID:   "", // carrier origin does not map to server user
						ContentType:    "media",
						Content:        content,
						Transport:      transport,
						ServerOrder:    0,
						CreatedAt:      created.UTC().Format(time.RFC3339),
						Source:         "CARRIER",
					}
					items = append(items, m)
				}
			}
		}
	}

	// sort by created_at ascending
	sort.SliceStable(items, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, items[i].CreatedAt)
		tj, _ := time.Parse(time.RFC3339, items[j].CreatedAt)
		return ti.Before(tj)
	})

	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
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

// decideTransport selects the transport for a send operation based on the
// conversation's transport_policy, the presence of other OHMF identities,
// and client profiles present in the request context.
func (s *Service) decideTransport(ctx context.Context, tx pgx.Tx, conversationID, senderUserID string) (string, error) {
	var policy string
	if err := tx.QueryRow(ctx, `SELECT transport_policy FROM conversations WHERE id = $1::uuid`, conversationID).Scan(&policy); err != nil {
		return "", err
	}
	switch policy {
	case "FORCE_OTT":
		return "OTT", nil
	case "FORCE_SMS":
		return "SMS", nil
	case "FORCE_MMS":
		return "MMS", nil
	case "BLOCK_CARRIER_RELAY":
		// treat as AUTO but disallow RELAY transports (we don't select RELAY here)
	}

	// AUTO-like decision: if there are other member users in the conversation,
	// prefer OTT unless policy forces SMS.
	var otherCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(1) FROM conversation_members WHERE conversation_id = $1::uuid AND user_id <> $2::uuid`, conversationID, senderUserID).Scan(&otherCount); err != nil {
		return "", err
	}
	if otherCount > 0 {
		return "OTT", nil
	}

	// If no other member users, check for external phone membership (PHONE_DM)
	var hasExternal int
	if err := tx.QueryRow(ctx, `SELECT COUNT(1) FROM conversation_external_members WHERE conversation_id = $1::uuid`, conversationID).Scan(&hasExternal); err == nil && hasExternal > 0 {
		return "SMS", nil
	}

	// Inspect client profiles from context (e.g., DEFAULT_SMS_HANDLER)
	if profiles, ok := middleware.ProfilesFromContext(ctx); ok {
		for _, p := range profiles {
			if p == "DEFAULT_SMS_HANDLER" {
				return "SMS", nil
			}
		}
	}

	// Fallback to OTT
	return "OTT", nil
}

func (s *Service) sendSync(ctx context.Context, userID, conversationID, idemKey, contentType string, content map[string]any, clientGeneratedID string) (Message, error) {
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

	// Enforce block rules: if any other member has blocked the sender, forbid sending.
	if blocked, blocker, err := s.checkBlockedRecipients(ctx, tx, userID, conversationID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, fmt.Errorf("blocked_by:%s", blocker)
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

	// decide transport according to conversation policy and client profiles
	chosenTransport, err := s.decideTransport(ctx, tx, conversationID, userID)
	if err != nil {
		return Message{}, err
	}

	var msgID string
	var created time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_user_id, content_type, content, client_generated_id, transport, server_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, $5, $6, $7)
		RETURNING id::text, created_at
	`, conversationID, userID, contentType, string(contentJSON), clientGeneratedID, chosenTransport, next).Scan(&msgID, &created)
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
		ClientGeneratedID: clientGeneratedID,
		Transport:      chosenTransport,
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

func (s *Service) sendToPhoneSync(ctx context.Context, userID, phoneE164, idemKey, contentType string, content map[string]any, clientGeneratedID string) (Message, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	conversationID, err := s.findOrCreatePhoneConversation(ctx, tx, userID, phoneE164)
	if err != nil {
		return Message{}, err
	}

	// Enforce block rules: if the (user) target has blocked sender, forbid send
	if blocked, blocker, err := s.checkBlockedRecipients(ctx, tx, userID, conversationID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, fmt.Errorf("blocked_by:%s", blocker)
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
		INSERT INTO messages (conversation_id, sender_user_id, content_type, content, client_generated_id, transport, server_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, $5, 'SMS', $6)
		RETURNING id::text, created_at
	`, conversationID, userID, contentType, string(contentJSON), clientGeneratedID, next).Scan(&msgID, &created)
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
		ClientGeneratedID: clientGeneratedID,
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
	Query(context.Context, string, ...any) (pgx.Rows, error)
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

// checkBlockedRecipients returns true and the blocking user id if any member of the
// conversation has blocked the sender.
func (s *Service) checkBlockedRecipients(ctx context.Context, q querier, senderUserID, conversationID string) (bool, string, error) {
	// iterate all other members and check user_blocks
	rows2, err := q.Query(ctx, `SELECT user_id::text FROM conversation_members WHERE conversation_id = $1::uuid AND user_id <> $2::uuid`, conversationID, senderUserID)
	if err != nil {
		return false, "", err
	}
	defer rows2.Close()
	for rows2.Next() {
		var uid string
		if err := rows2.Scan(&uid); err != nil {
			return false, "", err
		}
		var one int
		err := q.QueryRow(ctx, `SELECT 1 FROM user_blocks WHERE blocker_user_id = $1::uuid AND blocked_user_id = $2::uuid`, uid, senderUserID).Scan(&one)
		if err == nil {
			return true, uid, nil
		}
		if err != nil && err != pgx.ErrNoRows {
			return false, "", err
		}
	}
	return false, "", nil
}

// IsMember checks whether a user is a member of a conversation using the
// service's configured DB pool.
func (s *Service) IsMember(ctx context.Context, userID, conversationID string) (bool, error) {
	return s.hasMembership(ctx, s.db, userID, conversationID)
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
