package domain

import "errors"

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUserAlreadyExists     = errors.New("user already exists")
	ErrInvalidUser           = errors.New("invalid user")
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrSessionNotFound       = errors.New("session not found")
	ErrSessionExpired        = errors.New("session expired")
	ErrIdentityNotFound      = errors.New("identity not found")
	ErrIdentityInvalid       = errors.New("invalid identity")
	ErrRelatedEntityNotFound = errors.New("related entity not found")
	ErrInvalidEntity         = errors.New("invalid entity")
)
