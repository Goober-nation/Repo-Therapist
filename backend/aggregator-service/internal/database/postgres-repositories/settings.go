package postgres_repositories

import (
	"context"
	"errors"
	"fmt"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsRepository stores admin overrides of config/app.yaml (table settings_override).
type SettingsRepository struct {
	db *pgxpool.Pool
}

func NewSettingsRepository(db *pgxpool.Pool) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) ListOverrides(ctx context.Context) ([]domain.SettingOverride, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value, updated_by, updated_at FROM settings_override ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list setting overrides: %w", err)
	}
	defer rows.Close()

	var out []domain.SettingOverride

	for rows.Next() {
		var o domain.SettingOverride
		if err := rows.Scan(&o.Key, &o.Value, &o.UpdatedBy, &o.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan setting override: %w", err)
		}

		out = append(out, o)
	}

	return out, rows.Err()
}

func (r *SettingsRepository) UpsertOverride(ctx context.Context, o domain.SettingOverride) (*domain.SettingOverride, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	prev, err := lockedOverride(ctx, tx, o.Key)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO settings_override (key, value, updated_by, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by,
		                                updated_at = EXCLUDED.updated_at`,
		o.Key, o.Value, o.UpdatedBy, o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store setting override: %w", err)
	}

	return prev, tx.Commit(ctx)
}

func (r *SettingsRepository) DeleteOverride(ctx context.Context, key string) (*domain.SettingOverride, error) {
	var o domain.SettingOverride

	err := r.db.QueryRow(ctx, `DELETE FROM settings_override WHERE key = $1 RETURNING key, value, updated_by, updated_at`, key).
		Scan(&o.Key, &o.Value, &o.UpdatedBy, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("delete setting override: %w", err)
	}

	return &o, nil
}

func lockedOverride(ctx context.Context, tx pgx.Tx, key string) (*domain.SettingOverride, error) {
	var o domain.SettingOverride

	err := tx.QueryRow(ctx, `SELECT key, value, updated_by, updated_at FROM settings_override WHERE key = $1 FOR UPDATE`, key).
		Scan(&o.Key, &o.Value, &o.UpdatedBy, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read setting override: %w", err)
	}

	return &o, nil
}
