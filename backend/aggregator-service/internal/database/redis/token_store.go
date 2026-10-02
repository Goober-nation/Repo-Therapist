// Package redis stores private grading tokens for Grade and Repository services to read.
package redis

import (
	"aggregator-service/internal/domain"
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenStore implements domain.GradingTokenStore on Redis. Errors never include the token.
type TokenStore struct {
	client *redis.Client
}

func NewTokenStore(client *redis.Client) *TokenStore {
	return &TokenStore{client: client}
}

func (s *TokenStore) PutToken(ctx context.Context, jobID, token string, ttl time.Duration) error {
	if err := s.client.Set(ctx, domain.GradingTokenKey(jobID), token, ttl).Err(); err != nil {
		return fmt.Errorf("redis set grading token: %w", err)
	}

	return nil
}

func (s *TokenStore) DeleteToken(ctx context.Context, jobID string) error {
	if err := s.client.Del(ctx, domain.GradingTokenKey(jobID)).Err(); err != nil {
		return fmt.Errorf("redis delete grading token: %w", err)
	}

	return nil
}
