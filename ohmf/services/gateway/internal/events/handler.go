package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"ohmf/services/gateway/internal/middleware"
)

type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Handler struct {
	db DB
}

func NewHandler(db DB) *Handler {
	return &Handler{db: db}
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
		case <-ticker.C:
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

func (h *Handler) snapshot(ctx context.Context, userID string) (string, error) {
	var convMax time.Time
	var msgMax time.Time
	var total int64
	err := h.db.QueryRow(ctx, `
		SELECT
			COALESCE(MAX(c.updated_at), to_timestamp(0)),
			COALESCE(MAX(m.created_at), to_timestamp(0)),
			COALESCE(COUNT(m.id), 0)
		FROM conversation_members my
		JOIN conversations c ON c.id = my.conversation_id
		LEFT JOIN messages m ON m.conversation_id = c.id
		WHERE my.user_id = $1::uuid
	`, userID).Scan(&convMax, &msgMax, &total)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", convMax.UTC().UnixNano(), msgMax.UTC().UnixNano(), total), nil
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
