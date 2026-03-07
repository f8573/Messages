package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	up := filepath.Join(dir, "000001_init.up.sql")
	b, err := os.ReadFile(up)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", up, err)
	}
	_, err = pool.Exec(ctx, string(b))
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
