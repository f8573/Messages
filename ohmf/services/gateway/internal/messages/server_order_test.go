package messages

import (
    "context"
    "os"
    "sync"
    "testing"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrentServerOrderMonotonic(t *testing.T) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("skipping DB integration test; set TEST_DATABASE_URL to run")
    }
    ctx := context.Background()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        t.Fatalf("connect: %v", err)
    }
    defer pool.Close()

    // apply migrations
    mig, err := os.ReadFile("../../migrations/000001_init.up.sql")
    if err != nil {
        mig, err = os.ReadFile("../migrations/000001_init.up.sql")
        if err != nil {
            mig, err = os.ReadFile("migrations/000001_init.up.sql")
            if err != nil {
                t.Fatalf("read migration: %v", err)
            }
        }
    }
    if _, err := pool.Exec(ctx, string(mig)); err != nil {
        t.Fatalf("apply migration: %v", err)
    }

    // create a user
    var userID string
    if err := pool.QueryRow(ctx, `INSERT INTO users (primary_phone_e164) VALUES ($1) ON CONFLICT DO NOTHING RETURNING id::text`, "+10000000999").Scan(&userID); err != nil {
        // select if exists
        if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE primary_phone_e164 = $1`, "+10000000999").Scan(&userID); err != nil {
            t.Fatalf("ensure user: %v", err)
        }
    }

    // create a conversation
    convID := uuid.New().String()
    if _, err := pool.Exec(ctx, `INSERT INTO conversations (id, type) VALUES ($1, 'PRIVATE')`, convID); err != nil {
        t.Fatalf("insert conversation: %v", err)
    }
    if _, err := pool.Exec(ctx, `INSERT INTO conversation_counters (conversation_id) VALUES ($1)`, convID); err != nil {
        t.Fatalf("insert counter: %v", err)
    }

    const N = 50
    var wg sync.WaitGroup
    wg.Add(N)
    errs := make(chan error, N)

    for i := 0; i < N; i++ {
        go func(i int) {
            defer wg.Done()
            var order int64
            if err := pool.QueryRow(ctx, `UPDATE conversation_counters SET next_server_order = next_server_order + 1 WHERE conversation_id = $1 RETURNING next_server_order - 1`, convID).Scan(&order); err != nil {
                errs <- err
                return
            }
            // insert a message with that server_order
            if _, err := pool.Exec(ctx, `INSERT INTO messages (conversation_id, sender_user_id, content_type, content, server_order) VALUES ($1, $2::uuid, $3, $4, $5)`, convID, userID, "text", `{"text":"hi"}`, order); err != nil {
                errs <- err
                return
            }
        }(i)
    }
    wg.Wait()
    close(errs)
    for err := range errs {
        t.Fatalf("concurrent op failed: %v", err)
    }

    // verify server_order monotonic
    rows, err := pool.Query(ctx, `SELECT server_order FROM messages WHERE conversation_id = $1 ORDER BY server_order`, convID)
    if err != nil {
        t.Fatalf("select messages: %v", err)
    }
    defer rows.Close()
    var got int
    var expect int64 = 1
    for rows.Next() {
        var so int64
        if err := rows.Scan(&so); err != nil {
            t.Fatalf("scan: %v", err)
        }
        if so != expect {
            t.Fatalf("expected server_order %d, got %d", expect, so)
        }
        expect++
        got++
    }
    if got != N {
        t.Fatalf("expected %d messages, got %d", N, got)
    }
}
