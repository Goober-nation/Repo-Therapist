package domain

import (
	"context"

	"github.com/google/uuid"
)

type UserRepository interface {
	CreateUser(ctx context.Context, user User) error
	UpdateUser(ctx context.Context, user User) error
	DeleteUserByYID(ctx context.Context, yID string) error
	GetUserByID(ctx context.Context, userID uuid.UUID) error
	GetUserByYID(ctx context.Context, yID string) error
}

type SessionRepository interface {
	CreateSession(ctx context.Context, userID uuid.UUID) (string, error)
	GetSession(ctx context.Context, sessionID string) (uuid.UUID, error)
	DeleteSession(ctx context.Context, sessionID string) error
	UpdateSession(ctx context.Context, sessionID string) error
}
