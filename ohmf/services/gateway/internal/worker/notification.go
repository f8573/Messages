package worker

import (
    "context"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type NotificationWorker struct {
    pool *pgxpool.Pool
    stop chan struct{}
}

func NewNotificationWorker(pool *pgxpool.Pool) *NotificationWorker {
    return &NotificationWorker{pool: pool, stop: make(chan struct{})}
}

func (n *NotificationWorker) Name() string { return "notification" }

func (n *NotificationWorker) Start(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-n.stop:
            return nil
        default:
        }
        var exists bool
        if err := n.pool.QueryRow(ctx, `SELECT to_regclass('public.notifications') IS NOT NULL`).Scan(&exists); err == nil && exists {
            _, _ = n.pool.Exec(ctx, `
                UPDATE notifications
                SET delivered = true, delivered_at = now()
                WHERE id IN (
                    SELECT id FROM notifications WHERE COALESCE(delivered,false)=false LIMIT 100
                )
            `)
        }
        time.Sleep(1 * time.Second)
    }
}

func (n *NotificationWorker) Stop(ctx context.Context) error {
    close(n.stop)
    return nil
}
