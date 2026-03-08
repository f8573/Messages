package limit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRateLimited = errors.New("rate_limited")
)

type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

type TokenBucket struct {
	redis  *redis.Client
	script *redis.Script
}

func NewTokenBucket(redisClient *redis.Client) *TokenBucket {
	return &TokenBucket{
		redis: redisClient,
		script: redis.NewScript(`
local key = KEYS[1]
local now_ms = tonumber(ARGV[1])
local refill_per_ms = tonumber(ARGV[2])
local capacity = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])

if tokens == nil then
  tokens = capacity
  ts = now_ms
end

if now_ms > ts then
  local delta = now_ms - ts
  tokens = math.min(capacity, tokens + (delta * refill_per_ms))
  ts = now_ms
end

local allowed = 0
local retry_ms = 0

if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
else
  local deficit = requested - tokens
  if refill_per_ms > 0 then
    retry_ms = math.ceil(deficit / refill_per_ms)
  else
    retry_ms = ttl_ms
  end
end

redis.call('HMSET', key, 'tokens', tokens, 'ts', ts)
redis.call('PEXPIRE', key, ttl_ms)

return { allowed, math.floor(tokens), retry_ms }
`),
	}
}

func (b *TokenBucket) Allow(ctx context.Context, key string, refillTokens int64, refillWindow time.Duration, capacity int64, requested int64) (Decision, error) {
	if refillTokens <= 0 || refillWindow <= 0 || capacity <= 0 || requested <= 0 {
		return Decision{}, fmt.Errorf("invalid token bucket parameters")
	}
	now := time.Now().UnixMilli()
	refillPerMS := float64(refillTokens) / float64(refillWindow.Milliseconds())
	ttlMS := int64(math.Max(float64(refillWindow.Milliseconds()*2), 1000))
	raw, err := b.script.Run(ctx, b.redis, []string{key}, now, strconv.FormatFloat(refillPerMS, 'f', 12, 64), capacity, requested, ttlMS).Result()
	if err != nil {
		return Decision{}, err
	}
	parts, ok := raw.([]any)
	if !ok || len(parts) < 3 {
		return Decision{}, fmt.Errorf("unexpected token bucket script result")
	}
	allowed := asInt(parts[0]) == 1
	remaining := asInt(parts[1])
	retryMS := asInt(parts[2])
	return Decision{
		Allowed:    allowed,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryMS) * time.Millisecond,
	}, nil
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		p, _ := strconv.ParseInt(n, 10, 64)
		return p
	default:
		return 0
	}
}
