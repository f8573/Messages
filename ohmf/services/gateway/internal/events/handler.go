package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/middleware"
)

const streamPresenceTTL = 90 * time.Second

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type deliverySyncer interface {
	DeliverPendingToUser(ctx context.Context, recipientUserID string) ([]map[string]any, error)
}

type Handler struct {
	db      DB
	redis   *redis.Client
	syncer  deliverySyncer
}

func NewHandler(db DB, redisClient *redis.Client, syncer deliverySyncer) *Handler {
	return &Handler{db: db, redis: redisClient, syncer: syncer}
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "missing auth", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	cursor, err := h.snapshot(r.Context(), userID)
	if err == nil {
		_ = writeEvent(w, "sync_required", map[string]string{"cursor": cursor})
	} else {
		_ = writeEvent(w, "error", map[string]string{"message": "snapshot_unavailable"})
	}
	h.markPresence(r.Context(), userID)
	h.syncPendingDeliveries(r.Context(), userID)
	var pubsub *redis.PubSub
	var pubsubCh <-chan *redis.Message
	if h.redis != nil {
		pubsub = h.redis.Subscribe(r.Context(), "message:user:"+userID, "delivery:user:"+userID)
		defer pubsub.Close()
		pubsubCh = pubsub.Channel()
	}
	if err := flush(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-pubsubCh:
			if !ok {
				pubsubCh = nil
				continue
			}
			switch msg.Channel {
			case "message:user:" + userID:
				_, _ = fmt.Fprintf(w, "event: message_created\n")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
				_ = flush(w)
			case "delivery:user:" + userID:
				_, _ = fmt.Fprintf(w, "event: delivery_update\n")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
				_ = flush(w)
			}
		case <-ticker.C:
			h.markPresence(r.Context(), userID)
			next, err := h.snapshot(r.Context(), userID)
			if err != nil {
				_ = writeEvent(w, "error", map[string]string{"message": "snapshot_failed"})
				_ = flush(w)
				continue
			}
			if next != cursor {
				cursor = next
				_ = writeEvent(w, "sync_required", map[string]string{"cursor": cursor})
			} else {
				_, _ = w.Write([]byte(": keepalive\n\n"))
			}
			_ = flush(w)
		}
	}
}

func (h *Handler) markPresence(ctx context.Context, userID string) {
	if h.redis == nil || userID == "" {
		return
	}
	_ = h.redis.Set(ctx, "presence:user:"+userID, "online", streamPresenceTTL).Err()
}

func (h *Handler) syncPendingDeliveries(ctx context.Context, userID string) {
	if h.syncer == nil || h.redis == nil || userID == "" {
		return
	}
	updates, err := h.syncer.DeliverPendingToUser(ctx, userID)
	if err != nil {
		return
	}
	for _, update := range updates {
		senderUserID, _ := update["sender_user_id"].(string)
		if senderUserID == "" {
			continue
		}
		body, err := json.Marshal(update)
		if err != nil {
			continue
		}
		_ = h.redis.Publish(ctx, "delivery:user:"+senderUserID, body).Err()
	}
}

func (h *Handler) snapshot(ctx context.Context, userID string) (string, error) {
	var convMax time.Time
	var msgMax time.Time
	var deliveryMax time.Time
	var total int64
	err := h.db.QueryRow(ctx, `
		SELECT
			COALESCE(MAX(c.updated_at), to_timestamp(0)),
			COALESCE(MAX(m.created_at), to_timestamp(0)),
			COALESCE(MAX(md.updated_at), to_timestamp(0)),
			COALESCE(COUNT(DISTINCT m.id), 0)
		FROM conversation_members my
		JOIN conversations c ON c.id = my.conversation_id
		LEFT JOIN messages m ON m.conversation_id = c.id
		LEFT JOIN message_deliveries md ON md.message_id = m.id
		WHERE my.user_id = $1::uuid
	`, userID).Scan(&convMax, &msgMax, &deliveryMax, &total)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d:%d", convMax.UTC().UnixNano(), msgMax.UTC().UnixNano(), deliveryMax.UTC().UnixNano(), total), nil
}

func writeEvent(w http.ResponseWriter, name string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
		return err
	}
	return nil
}

func flush(w http.ResponseWriter) error {
	if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
