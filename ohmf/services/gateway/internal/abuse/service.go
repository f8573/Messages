package abuse

import (
	"context"
	"encoding/json"
	"time"

	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// RecordEvent stores an abuse-related event for later scoring and analysis.
func (s *Service) RecordEvent(ctx context.Context, actorID, targetID, eventType, ip string, details any) error {
	id := uuid.New().String()
	var dbDetails interface{}
	if details != nil {
		b, err := json.Marshal(details)
		if err != nil {
			return err
		}
		dbDetails = string(b)
	}
	_, err := s.db.Exec(ctx, `
        INSERT INTO abuse_events (id, actor_user_id, target_user_id, event_type, details, ip_address, created_at)
        VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::jsonb, $6, now())
    `, id, nullableUUID(actorID), nullableUUID(targetID), eventType, dbDetails, ip)
	return err
}

// GetScore computes a simple sliding-window score (events in last 30 days)
// This is a simple heuristic for demonstration; a real deployment would
// use a dedicated scoring pipeline.
func (s *Service) GetScore(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
        SELECT count(*) FROM abuse_events WHERE target_user_id = $1::uuid AND created_at > now() - interval '30 days'
    `, userID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetDestinationScore computes a simple reputation score for a destination
// (phone number) based on abuse events referencing the phone in details.
// Lower is better; higher indicates more reports/failures.
func (s *Service) GetDestinationScore(ctx context.Context, phone string) (int, error) {
	if phone == "" {
		return 0, fmt.Errorf("phone required")
	}
	var count int
	err := s.db.QueryRow(ctx, `
        SELECT count(*) FROM abuse_events WHERE (details->>'phone') = $1 AND created_at > now() - interval '90 days'
    `, phone).Scan(&count)
	if err != nil {
		return 0, err
	}
	// simple heuristic: return count as score
	return count, nil
}

// CheckOTPThrottle returns true if the given phone or IP is allowed to attempt
// an OTP. Simple rule: max 5 attempts per hour per IP or phone.
func (s *Service) CheckOTPThrottle(ctx context.Context, key string, window time.Duration, maxAttempts int) (bool, error) {
	var count int
	// abuse_events.event_type = 'otp_attempt' and details may include 'phone'
	err := s.db.QueryRow(ctx, `
        SELECT count(*) FROM abuse_events WHERE (ip_address = $1 OR (details->>'phone') = $1) AND event_type = 'otp_attempt' AND created_at > now() - $2::interval
    `, key, fmtInterval(window)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count < maxAttempts, nil
}

// helper to convert duration to postgres interval string (seconds)
func fmtInterval(d time.Duration) string {
	return (time.Duration(d).String())
}

func nullableUUID(id string) interface{} {
	if id == "" {
		return nil
	}
	return id
}
