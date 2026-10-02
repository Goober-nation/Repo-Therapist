// Package tokenstore reads a private grading's user PAT, which the Aggregator stores in Redis
// under the job's id instead of sending it over Kafka (docs/kafka-contracts.md).
package tokenstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// Key is the Redis key holding jobID's user PAT.
func Key(jobID string) string {
	return "grading:token:" + jobID
}

// Getter returns jobID's user PAT, or "" when none is stored (public grading).
type Getter interface {
	GetToken(ctx context.Context, jobID string) (string, error)
}

// Redis implements Getter. Errors never include the token.
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
