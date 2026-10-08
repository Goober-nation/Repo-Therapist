package postgres_repositories

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditRepository struct {
	db *pgxpool.Pool
}

func NewAuditRepository(db *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) InsertAudit(ctx context.Context, event domain.AuditEvent) error {
	details := event.Details
	if len(details) == 0 {
		details = []byte("{}")
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO audit_log (actor_kind, actor_hash, action, target_kind, target, outcome, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ActorKind, event.ActorHash, event.Action, event.TargetKind, event.Target, event.Outcome, details,
	)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}

	return nil
}

// ListAudit returns up to filter.Limit entries, newest first. The WHERE text is assembled from fixed
// fragments only; every value goes in as a parameter.
func (r *AuditRepository) ListAudit(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditEntry, error) {
	var (
		where []string
		args  []any
	)

	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}

	if filter.BeforeID > 0 {
		add("id < $%d", filter.BeforeID)
	}

	if filter.Action != "" {
		if prefix, ok := strings.CutSuffix(filter.Action, "."); ok {
			add("action LIKE $%d", escapeLike(prefix)+".%")
		} else {
			add("action = $%d", filter.Action)
		}
	}

	if filter.Outcome != "" {
		add("outcome = $%d", filter.Outcome)
	}

	query := "SELECT id, occurred_at, actor_kind, actor_hash, action, target_kind, target, outcome, details FROM audit_log"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	args = append(args, filter.Limit)
	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.AuditEntry, 0, filter.Limit)

	for rows.Next() {
		var entry domain.AuditEntry
		if err := rows.Scan(&entry.ID, &entry.OccurredAt, &entry.ActorKind, &entry.ActorHash, &entry.Action,
			&entry.TargetKind, &entry.Target, &entry.Outcome, &entry.Details); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit: %w", err)
	}

	return entries, nil
}

func (r *AuditRepository) DeleteAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM audit_log WHERE occurred_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete audit: %w", err)
	}

	return tag.RowsAffected(), nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(text string) string {
	return likeEscaper.Replace(text)
}
