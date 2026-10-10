package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cooldown lets one key through once per period: the first Acquire sets the key with the period as
// its expiry, later ones answer what is left of it.
type Cooldown struct {
	client *redis.Client
	period time.Duration
}

func NewCooldown(client *redis.Client, period time.Duration) *Cooldown {
	return &Cooldown{client: client, period: period}
}

func (c *Cooldown) Acquire(ctx context.Context, key string) (time.Duration, error) {
	set, err := c.client.SetNX(ctx, key, 1, c.period).Result()
	if err != nil {
		return 0, fmt.Errorf("set cooldown: %w", err)
	}

	if set {
		return 0, nil
	}

	ttl, err := c.client.PTTL(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("read cooldown: %w", err)
	}

	// The key expired between the two calls (-2): it is free, but only a new Acquire may take it.
	if ttl == -2 {
		return time.Second, nil
	}

	// No expiry (-1, never set by us): wait a full period.
	if ttl <= 0 {
		return c.period, nil
	}

	return ttl, nil
}
