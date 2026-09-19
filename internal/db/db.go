// Package db provides the shared Postgres connection helper.
// Per docs/db-design.md (moved to tmp/db-design.md pending final review): the Aggregator
// (core) service is the sole read-write client. Other services in this repo (Repository
// service, Grade service, Security wrapper) do not currently write to this DB directly —
// this package exists for when that changes, and isn't used by the first Repository-service
// slice yet.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return pool, nil
}
