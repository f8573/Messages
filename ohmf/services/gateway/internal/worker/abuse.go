package worker

import (
    "context"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

type AbuseAggregatorWorker struct {
    pool *pgxpool.Pool
}

func NewAbuseAggregatorWorker(pool *pgxpool.Pool) *AbuseAggregatorWorker {
    return &AbuseAggregatorWorker{pool: pool}
}

func (a *AbuseAggregatorWorker) Name() string { return "abuse_aggregator" }

func (a *AbuseAggregatorWorker) Start(ctx context.Context) error {
    // run once: aggregate counts and upsert into abuse_scores
    var exists bool
    if err := a.pool.QueryRow(ctx, `SELECT to_regclass('public.abuse_events') IS NOT NULL`).Scan(&exists); err != nil || !exists {
        return nil
    }
    // create table if migration not applied
    _, _ = a.pool.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS abuse_scores (
            phone_e164 TEXT PRIMARY KEY,
            score INT NOT NULL
        )
    `)

    rows, err := a.pool.Query(ctx, `SELECT COALESCE(phone_e164,'') AS phone, count(*) as cnt FROM abuse_events GROUP BY phone_e164`)
    if err != nil {
        return err
    }
    defer rows.Close()
    tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)
    for rows.Next() {
        var phone string
        var cnt int64
        if err := rows.Scan(&phone, &cnt); err != nil {
            return err
        }
        _, _ = tx.Exec(ctx, `INSERT INTO abuse_scores (phone_e164, score) VALUES ($1,$2) ON CONFLICT (phone_e164) DO UPDATE SET score = EXCLUDED.score`, phone, int(cnt))
    }
    if err := tx.Commit(ctx); err != nil {
        return err
    }
    return nil
}

func (a *AbuseAggregatorWorker) Stop(ctx context.Context) error { return nil }
