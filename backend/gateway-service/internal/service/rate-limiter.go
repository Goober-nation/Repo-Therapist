package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// RedisLimiter allows limit requests per key per window. Limit and window are atomics so
// SetLimits can change them while requests are served (config reloads, admin overrides).
type RedisLimiter struct {
	client *redis.Client
	limit  atomic.Int64
	window atomic.Int64 // nanoseconds
}

func NewRedisLimiter(
	client *redis.Client,
	limit int,
	window time.Duration,
) *RedisLimiter {
	rl := &RedisLimiter{client: client}
	rl.SetLimits(limit, window)

	return rl
}

// SetLimits changes the limit and window; a key's current window keeps its expiry.
func (rl *RedisLimiter) SetLimits(limit int, window time.Duration) {
	rl.limit.Store(int64(limit))
	rl.window.Store(int64(window))
}

var rateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])

if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end

if current > tonumber(ARGV[2]) then
	return 0
end

return 1
`)

func (rl *RedisLimiter) Allow(
	ctx context.Context,
	key string,
) (bool, error) {
	limit, window := rl.limit.Load(), time.Duration(rl.window.Load())
	if limit <= 0 {
		return false, fmt.Errorf("rate limit must be greater than zero")
	}

	if window <= 0 {
		return false, fmt.Errorf("rate limit window must be greater than zero")
	}

	result, err := rateLimitScript.Run(
		ctx,
		rl.client,
		[]string{key},
		window.Milliseconds(),
		limit,
	).Int()
	if err != nil {
		return false, fmt.Errorf("check rate limit: %w", err)
	}

	return result == 1, nil
}
