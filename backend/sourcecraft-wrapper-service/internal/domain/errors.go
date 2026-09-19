package domain

import "errors"

var (
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrExternalAPI        = errors.New("external API error")
	ErrInvalidInput       = errors.New("invalid input")
	ErrInternalError      = errors.New("internal service error")
	ErrNoAccess           = errors.New("no access rights for the specified repository")
)
