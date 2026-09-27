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
	ErrLimitExceeded         = errors.New("request limit exceeded")
	ErrRequestFailed         = errors.New("request failed")
	ErrTimeout               = errors.New("rpc client timeout")
	ErrInvalidRequest        = errors.New("invalid request payload")
	ErrNotFound              = errors.New("not found")
	ErrForbidden             = errors.New("access denied")
	ErrPending               = errors.New("result is pending")
	ErrConflict              = errors.New("conflicting request")
	ErrUnavailable           = errors.New("upstream unavailable")
)
