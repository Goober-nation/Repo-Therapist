// Package tokenstore holds a private grading's user PAT, which the Aggregator stores in Redis
// under the job's id instead of sending it over Kafka (docs/kafka-contracts.md).
package tokenstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// TTL matches the Aggregator's default GRADING_LOCK_TIMEOUT; used by the manual grading-request tool.
const TTL = 20 * time.Minute

// Key is the Redis key holding jobID's user PAT.
func Key(jobID string) string {
	return "grading:token:" + jobID
}

// Store reads and deletes a job's PAT. GetToken returns "" when none is stored (public grading).
type Store interface {
	GetToken(ctx context.Context, jobID string) (string, error)
	DeleteToken(ctx context.Context, jobID string) error
}

// Redis implements Store. Errors never include the token.
type Redis struct {
	client *redis.Client
}

// NewFromEnv connects with the env names gateway-service uses: REDIS_ADDRESS, REDIS_PASSWORD,
// REDIS_DB.
func NewFromEnv() *Redis {
	db, _ := strconv.Atoi(os.Getenv("REDIS_DB"))
	addr := os.Getenv("REDIS_ADDRESS")
	if addr == "" {
		addr = "localhost:6379"
	}
	return &Redis{client: redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), DB: db})}
}

func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *Redis) Close() error { return r.client.Close() }

func (r *Redis) GetToken(ctx context.Context, jobID string) (string, error) {
	if jobID == "" {
		return "", nil
	}
	token, err := r.client.Get(ctx, Key(jobID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading grading token for job %s: %w", jobID, err)
	}
	return token, nil
}

func (r *Redis) DeleteToken(ctx context.Context, jobID string) error {
	if jobID == "" {
		return nil
	}
	if err := r.client.Del(ctx, Key(jobID)).Err(); err != nil {
		return fmt.Errorf("deleting grading token for job %s: %w", jobID, err)
	}
	return nil
}

// PutToken stores jobID's PAT for ttl, as the Aggregator does.
func (r *Redis) PutToken(ctx context.Context, jobID, token string, ttl time.Duration) error {
	if err := r.client.Set(ctx, Key(jobID), token, ttl).Err(); err != nil {
		return fmt.Errorf("storing grading token for job %s: %w", jobID, err)
	}
	return nil
}
