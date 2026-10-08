package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// AdminGrantStore keeps the admin password unlock of a session in Redis, one key per session.
type AdminGrantStore struct {
	client *redis.Client
}

func NewAdminGrantStore(client *redis.Client) *AdminGrantStore {
	return &AdminGrantStore{client: client}
}

func grantKey(sessionID string) string {
	return "admin_grant:" + sessionID
}

func (s *AdminGrantStore) Grant(ctx context.Context, sessionID, fingerprint string, ttl time.Duration) error {
	if err := s.client.Set(ctx, grantKey(sessionID), fingerprint, ttl).Err(); err != nil {
		return fmt.Errorf("store admin grant: %w", err)
	}

	return nil
}

func (s *AdminGrantStore) Fingerprint(ctx context.Context, sessionID string) (string, error) {
	value, err := s.client.Get(ctx, grantKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("read admin grant: %w", err)
	}

	return value, nil
}

func (s *AdminGrantStore) Revoke(ctx context.Context, sessionID string) (bool, error) {
	removed, err := s.client.Del(ctx, grantKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("revoke admin grant: %w", err)
	}

	return removed > 0, nil
}

// AttemptGuard counts wrong admin passwords in Redis. A key's failures are counted over one lock
// period; reaching the limit locks the key for that period.
type AttemptGuard struct {
	client      *redis.Client
	maxAttempts int
	lockFor     time.Duration
}

func NewAttemptGuard(client *redis.Client, maxAttempts int, lockFor time.Duration) *AttemptGuard {
	return &AttemptGuard{client: client, maxAttempts: maxAttempts, lockFor: lockFor}
}

func failKey(key string) string { return "admin_unlock:fail:" + key }
func lockKey(key string) string { return "admin_unlock:lock:" + key }

func (g *AttemptGuard) LockedFor(ctx context.Context, keys ...string) (time.Duration, error) {
	var longest time.Duration

	for _, key := range keys {
		ttl, err := g.client.PTTL(ctx, lockKey(key)).Result()
		if err != nil {
			return 0, fmt.Errorf("read admin unlock lock: %w", err)
		}

		// A negative TTL means the key is missing (-2) or has no expiry (-1, never set by us).
		if ttl > longest {
			longest = ttl
		}
	}

	return longest, nil
}

// failureScript counts one failure and, at the limit, locks the key and forgets the count, in one step.
// It returns 1 when the key is now locked.
var failureScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
if count >= tonumber(ARGV[1]) then
	redis.call("SET", KEYS[2], "1", "PX", ARGV[2])
	redis.call("DEL", KEYS[1])
	return 1
end
return 0
`)

func (g *AttemptGuard) RecordFailure(ctx context.Context, keys ...string) (bool, error) {
	locked := false

	for _, key := range keys {
		result, err := failureScript.Run(ctx, g.client, []string{failKey(key), lockKey(key)}, g.maxAttempts, g.lockFor.Milliseconds()).Int()
		if err != nil {
			return locked, fmt.Errorf("record admin unlock failure: %w", err)
		}

		locked = locked || result == 1
	}

	return locked, nil
}

func (g *AttemptGuard) Reset(ctx context.Context, keys ...string) error {
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, failKey(key))
	}

	if len(names) == 0 {
		return nil
	}

	if err := g.client.Del(ctx, names...).Err(); err != nil {
		return fmt.Errorf("reset admin unlock failures: %w", err)
	}

	return nil
}
