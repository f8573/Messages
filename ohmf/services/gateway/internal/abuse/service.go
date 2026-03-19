package abuse

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// removed: placeholder implementations for deleted abuse service
func (s *Service) RecordEvent(ctx context.Context, userID, targetID, eventType, ip string, details map[string]any) error {
	return nil
}

func (s *Service) GetScore(ctx context.Context, userID string) (int, error) {
	return 0, nil
}

func (s *Service) GetDestinationScore(ctx context.Context, destination string) (int, error) {
	return 0, nil
}

func (s *Service) CheckOTPThrottle(ctx context.Context, key string, duration time.Duration, limit int) (bool, error) {
	return true, nil
}
