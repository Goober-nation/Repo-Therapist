package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type UserRepository interface {
	CreateUser(ctx context.Context, user User) error
	DeleteUserByYID(ctx context.Context, yID string) error
	GetUserByID(ctx context.Context, userID uuid.UUID) (User, error)
	GetUserByYID(ctx context.Context, yID string) (User, error)
}

type SessionRepository interface {
	CreateSession(ctx context.Context, userID uuid.UUID) (string, error)
	GetSession(ctx context.Context, sessionID string) (uuid.UUID, error)
	// DeleteSession removes the session and reports ErrSessionNotFound when there was none.
	DeleteSession(ctx context.Context, sessionID string) error
	//UpdateSession(ctx context.Context, sessionID string) error
}

// AdminGrantStore keeps the password unlock of a session. A grant lives as long as the session and
// holds a fingerprint of the password it was given for, so changing the password revokes every grant.
type AdminGrantStore interface {
	Grant(ctx context.Context, sessionID, fingerprint string, ttl time.Duration) error
	// Fingerprint returns the grant's fingerprint, or "" when the session has none.
	Fingerprint(ctx context.Context, sessionID string) (string, error)
	// Revoke removes the grant and reports whether there was one.
	Revoke(ctx context.Context, sessionID string) (bool, error)
}

// AttemptGuard counts wrong passwords per key (a client IP, an account) and locks a key for a while
// once it has too many.
type AttemptGuard interface {
	// LockedFor returns how long the longest lock among the keys still lasts; 0 when none is locked.
	LockedFor(ctx context.Context, keys ...string) (time.Duration, error)
	// RecordFailure counts one wrong password for every key and reports whether any key is now locked.
	RecordFailure(ctx context.Context, keys ...string) (bool, error)
	// Reset forgets the failures of the keys after a correct password.
	Reset(ctx context.Context, keys ...string) error
}
