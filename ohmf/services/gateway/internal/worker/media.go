package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MediaWorker struct {
	pool *pgxpool.Pool
	stop chan struct{}
}

func NewMediaWorker(pool *pgxpool.Pool) *MediaWorker {
	return &MediaWorker{pool: pool, stop: make(chan struct{})}
}

func (m *MediaWorker) Name() string { return "media" }

func (m *MediaWorker) Start(ctx context.Context) error {
	// run loop until context done or stop requested
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.stop:
			return nil
		default:
		}
		// safe-check for table existence and process up to 50 items
		var exists bool
		if err := m.pool.QueryRow(ctx, `SELECT to_regclass('public.media') IS NOT NULL`).Scan(&exists); err == nil && exists {
			// mark unprocessed media as processed (placeholder for AV scan)
			_, _ = m.pool.Exec(ctx, `
                UPDATE media SET media_processed = true
                WHERE id IN (
                    SELECT id FROM media WHERE COALESCE(media_processed,false)=false LIMIT 50
                )
            `)
		}
		// also check upload_tokens table if present
		if err := m.pool.QueryRow(ctx, `SELECT to_regclass('public.upload_tokens') IS NOT NULL`).Scan(&exists); err == nil && exists {
			_, _ = m.pool.Exec(ctx, `
                UPDATE upload_tokens SET processed = true
                WHERE id IN (
                    SELECT id FROM upload_tokens WHERE COALESCE(processed,false)=false LIMIT 50
                )
            `)
		}
		// placeholder: AV scan hook would be called here
		time.Sleep(2 * time.Second)
	}
}

func (m *MediaWorker) Stop(ctx context.Context) error {
	close(m.stop)
	return nil
}
