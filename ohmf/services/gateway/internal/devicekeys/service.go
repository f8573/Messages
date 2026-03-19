package devicekeys

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// DB returns the underlying database pool for handlers
func (s *Service) DB() *pgxpool.Pool {
	return s.pool
}

// removed: placeholder implementations for deleted devicekeys service
