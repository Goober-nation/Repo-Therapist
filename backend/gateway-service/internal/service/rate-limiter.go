package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type RedisLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

func NewRedisLimiter(
	client *redis.Client,
	limit int,
	window time.Duration,
) *RedisLimiter {
	return &RedisLimiter{
		client: client,
		limit:  limit,
		window: window,
	}
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
	if rl.limit <= 0 {
		return false, fmt.Errorf("rate limit must be greater than zero")
	}

	if rl.window <= 0 {
		return false, fmt.Errorf("rate limit window must be greater than zero")
	}

	result, err := rateLimitScript.Run(
		ctx,
		rl.client,
		[]string{key},
		rl.window.Milliseconds(),
		rl.limit,
	).Int()
	if err != nil {
		return false, fmt.Errorf("check rate limit: %w", err)
	}

	return result == 1, nil
}
