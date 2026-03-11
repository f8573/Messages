package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// NotificationPayload represents a minimal notification payload.
type NotificationPayload struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data,omitempty"`
}

// Send enqueues a notification for a given user. This simply inserts a
// row into the notifications table; a worker should pick this up later.
func (s *Service) Send(ctx context.Context, userID string, p NotificationPayload) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
        INSERT INTO notifications (user_id, payload, status, created_at)
        VALUES ($1::uuid, $2::jsonb, 'pending', $3)
    `, userID, string(b), time.Now())
	return err
}
