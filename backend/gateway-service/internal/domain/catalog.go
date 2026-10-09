package domain

import "time"

// CatalogRefresh is one fetch of the full public repo listing by the Aggregator.
type CatalogRefresh struct {
	ID         int64
	StartedAt  time.Time
	FinishedAt *time.Time
	// Status is running | completed | failed | cancelled.
	Status string
	// Trigger is admin | startup.
	Trigger   string
	ActorHash string
	Pages     int32
	Repos     int32
	// Error is a fixed code (wrapper_error, too_many_pages, ...); empty otherwise.
	Error string
}

// CatalogStatus is the public repo catalog as the admin panel shows it.
type CatalogStatus struct {
	Size          int32
	UpdatedAt     *time.Time
	Current       *CatalogRefresh
	LastCompleted *CatalogRefresh
}
