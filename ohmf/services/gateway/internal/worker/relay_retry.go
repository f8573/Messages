package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RelayRetryWorker struct {
	pool *pgxpool.Pool
	stop chan struct{}
}

func NewRelayRetryWorker(pool *pgxpool.Pool) *RelayRetryWorker {
	return &RelayRetryWorker{pool: pool, stop: make(chan struct{})}
}

func (r *RelayRetryWorker) Name() string { return "relay_retry" }

func (r *RelayRetryWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.stop:
			return nil
		default:
		}
		// ensure retry_count exists
		_, _ = r.pool.Exec(ctx, `ALTER TABLE relay_jobs ADD COLUMN IF NOT EXISTS retry_count INT DEFAULT 0`)
		// bump retry_count for failed jobs and requeue up to 3
		_, _ = r.pool.Exec(ctx, `
            UPDATE relay_jobs SET retry_count = COALESCE(retry_count,0)+1, status = 'queued'
            WHERE status = 'CARRIER_SEND_FAILED' AND COALESCE(retry_count,0) < 3
        `)
		// sleep between runs
		select {
		case <-ctx.Done():
			return nil
		case <-r.stop:
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func (r *RelayRetryWorker) Stop(ctx context.Context) error {
	close(r.stop)
	return nil
}
