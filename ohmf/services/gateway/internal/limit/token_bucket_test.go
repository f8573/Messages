package limit

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestTokenBucketAllowAndBlock(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	bucket := NewTokenBucket(rdb)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		decision, err := bucket.Allow(ctx, "test:bucket", 2, time.Second, 2, 1)
		if err != nil {
			t.Fatalf("allow request %d: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	decision, err := bucket.Allow(ctx, "test:bucket", 2, time.Second, 2, 1)
	if err != nil {
		t.Fatalf("third request: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("third request should be blocked")
	}
	if decision.RetryAfter <= 0 {
		t.Fatalf("expected retry_after to be > 0")
	}
}
