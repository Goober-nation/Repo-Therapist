package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gateway-service/internal/domain"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type SessionRepository struct {
	client     *redis.Client
	sessionTTL time.Duration
	keyPrefix  string
}

func NewSessionRepository(client *redis.Client, sessionTTL time.Duration,
	keyPrefix string) *SessionRepository {
	return &SessionRepository{
		client:     client,
		sessionTTL: sessionTTL,
		keyPrefix:  keyPrefix,
	}
}

func (sr *SessionRepository) CreateSession(ctx context.Context, userID uuid.UUID) (string, error) {
	sessionID := uuid.New().String()

	key := sr.buildSessionKey(sessionID)

	err := sr.client.Set(
		ctx,
		key,
		userID.String(),
		sr.sessionTTL,
	).Err()
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return sessionID, nil
}

func (sr *SessionRepository) GetSession(
	ctx context.Context,
	sessionID string,
) (uuid.UUID, error) {
	key := sr.buildSessionKey(sessionID)

	value, err := sr.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return uuid.Nil, domain.ErrSessionNotFound
		}

		return uuid.Nil, fmt.Errorf("get session: %w", err)
	}

	userID, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf(
			"parse user id from session: %w",
			err,
		)
	}

	return userID, nil
}

//func (sr *SessionRepository) DeleteSession(
//	ctx context.Context,
//	sessionID string,
//) error {
//	key := sr.buildSessionKey(sessionID)
//
//	result, err := sr.client.Del(ctx, key).Result()
//	if err != nil {
//		return fmt.Errorf("delete session: %w", err)
//	}
//
//	if result == 0 {
//		return domain.ErrSessionNotFound
//	}
//
//	return nil
//}
//
//func (sr *SessionRepository) UpdateSession(
//	ctx context.Context,
//	sessionID string,
//) error {
//	key := sr.buildSessionKey(sessionID)
//
//	updated, err := sr.client.Expire(
//		ctx,
//		key,
//		sr.sessionTTL,
//	).Result()
//	if err != nil {
//		return fmt.Errorf("update session: %w", err)
//	}
//
//	if !updated {
//		return domain.ErrSessionNotFound
//	}
//
//	return nil
//}

func (sr *SessionRepository) buildSessionKey(sessionID string) string {
	return sr.keyPrefix + sessionID
}
