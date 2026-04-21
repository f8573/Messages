package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/bus"
)

const (
	ackKeyPrefix = "msg:ack:"
	ackPollDelay = 50 * time.Millisecond
	ackPollLimit = 250 * time.Millisecond
)

type IngressEvent struct {
	EventID           string         `json:"event_id"`
	MessageID         string         `json:"message_id"`
	ConversationID    string         `json:"conversation_id"`
	SenderUserID      string         `json:"sender_user_id"`
	IdempotencyKey    string         `json:"idempotency_key"`
	Endpoint          string         `json:"endpoint"`
	ClientGeneratedID string         `json:"client_generated_id,omitempty"`
	ContentType       string         `json:"content_type"`
	Content           map[string]any `json:"content"`
	TransportIntent   string         `json:"transport_intent"`
	RecipientPhone    string         `json:"recipient_phone,omitempty"`
	SentAtMS          int64          `json:"sent_at_ms"`
	TraceID           string         `json:"trace_id"`
}

type PersistedAck struct {
	EventID        string `json:"event_id"`
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	ServerOrder    int64  `json:"server_order"`
	Status         string `json:"status"`
	Transport      string `json:"transport"`
	PersistedAtMS  int64  `json:"persisted_at_ms"`
}

type AsyncPipeline struct {
	producer bus.IngressProducer
	redis    *redis.Client
}

func NewAsyncPipeline(producer bus.IngressProducer, redisClient *redis.Client) *AsyncPipeline {
	if producer == nil || redisClient == nil {
		return nil
	}
	return &AsyncPipeline{
		producer: producer,
		redis:    redisClient,
	}
}

func (p *AsyncPipeline) PublishIngress(ctx context.Context, evt IngressEvent) error {
	if p == nil || p.producer == nil {
		return nil
	}
	return p.producer.PublishIngress(ctx, evt.ConversationID, evt)
}

// PublishEnvelope publishes an already-built Envelope to the ingress topic.
func (p *AsyncPipeline) PublishEnvelope(ctx context.Context, conversationID string, env Envelope) error {
	if p == nil || p.producer == nil {
		return nil
	}
	return p.producer.PublishIngress(ctx, conversationID, env)
}

func (p *AsyncPipeline) WaitAck(ctx context.Context, eventID string, timeout time.Duration) (PersistedAck, bool, error) {
	if p == nil || p.redis == nil {
		return PersistedAck{}, false, nil
	}
	key := ackKeyPrefix + eventID
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pollTimeout := ackPollLimit
		if remaining := time.Until(deadline); remaining > 0 && remaining < pollTimeout {
			pollTimeout = remaining
		}
		pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
		payload, err := p.redis.Get(pollCtx, key).Result()
		cancel()
		if err == nil {
			var ack PersistedAck
			if uErr := json.Unmarshal([]byte(payload), &ack); uErr != nil {
				return PersistedAck{}, false, uErr
			}
			return ack, true, nil
		}
		if ctx.Err() != nil {
			return PersistedAck{}, false, ctx.Err()
		}
		// Ack lookup is best-effort. If Redis is briefly unavailable while the
		// async worker is still recovering, fall back to the queued response
		// instead of failing the entire send request.
		select {
		case <-ctx.Done():
			return PersistedAck{}, false, ctx.Err()
		case <-time.After(ackPollDelay):
		}
	}
	return PersistedAck{}, false, nil
}

func NewIngressEvent(userID, conversationID, endpoint, idemKey, contentType, transportIntent, recipientPhone, clientGeneratedID, traceID string, content map[string]any) IngressEvent {
	if traceID == "" {
		traceID = uuid.NewString()
	}
	return IngressEvent{
		EventID:           uuid.NewString(),
		MessageID:         uuid.NewString(),
		ConversationID:    conversationID,
		SenderUserID:      userID,
		IdempotencyKey:    idemKey,
		Endpoint:          endpoint,
		ClientGeneratedID: clientGeneratedID,
		ContentType:       contentType,
		Content:           content,
		TransportIntent:   transportIntent,
		RecipientPhone:    recipientPhone,
		SentAtMS:          time.Now().UTC().UnixMilli(),
		TraceID:           traceID,
	}
}

func (e IngressEvent) ProvisionalMessage() Message {
	return Message{
		MessageID:         e.MessageID,
		ConversationID:    e.ConversationID,
		SenderUserID:      e.SenderUserID,
		ContentType:       e.ContentType,
		Content:           e.Content,
		Transport:         e.TransportIntent,
		ClientGeneratedID: e.ClientGeneratedID,
		ServerOrder:       0,
		Status:            "QUEUED",
		CreatedAt:         time.UnixMilli(e.SentAtMS).UTC().Format(time.RFC3339),
	}
}

func AckRedisKey(eventID string) string {
	return fmt.Sprintf("%s%s", ackKeyPrefix, eventID)
}
