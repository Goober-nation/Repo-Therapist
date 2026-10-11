package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"aggregator-service/internal/domain"
)

// DefaultAuditRetentionDays is how long audit entries are kept (AUDIT_RETENTION_DAYS).
const DefaultAuditRetentionDays = 30

// auditPurgeInterval is how often old entries are removed; retention is in days, so hourly is plenty.
const auditPurgeInterval = time.Hour

// RecordAudit validates and stores one audit event. The Aggregator is the only writer of the log;
// the Gateway reports the events it sees (admin denials) here instead of writing the table itself.
func (as *AggregatorService) RecordAudit(ctx context.Context, event domain.AuditEvent) error {
	if as.AuditRepo == nil {
		return fmt.Errorf("%w: audit log is not configured", domain.ErrInvalidAudit)
	}

	if err := event.Validate(); err != nil {
		return err
	}

	return as.AuditRepo.InsertAudit(ctx, event)
}

// ListAudit returns one page of the log, newest first. The limit defaults and is capped; the cursor
// for the next page is the ID of the last entry returned, and 0 once the log is exhausted.
func (as *AggregatorService) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	if as.AuditRepo == nil {
		return domain.AuditPage{}, fmt.Errorf("%w: audit log is not configured", domain.ErrInvalidAudit)
	}

	if filter.Limit < 0 || filter.BeforeID < 0 {
		return domain.AuditPage{}, fmt.Errorf("%w: limit and cursor must not be negative", domain.ErrInvalidAudit)
	}

	if filter.Limit == 0 {
		filter.Limit = domain.DefaultAuditPageSize
	}

	if filter.Limit > domain.MaxAuditPageSize {
		filter.Limit = domain.MaxAuditPageSize
	}

	if filter.Outcome != "" && filter.Outcome != domain.AuditOutcomeOK &&
		filter.Outcome != domain.AuditOutcomeDenied && filter.Outcome != domain.AuditOutcomeError {
		return domain.AuditPage{}, fmt.Errorf("%w: unknown outcome %q", domain.ErrInvalidAudit, filter.Outcome)
	}

	if filter.Scope != "" && filter.Scope != domain.AuditScopeAdmin && filter.Scope != domain.AuditScopeUsers {
		return domain.AuditPage{}, fmt.Errorf("%w: unknown scope %q", domain.ErrInvalidAudit, filter.Scope)
	}

	if len(filter.Action) > domain.MaxAuditActionLength+1 {
		return domain.AuditPage{}, fmt.Errorf("%w: action filter is too long", domain.ErrInvalidAudit)
	}

	// One extra row tells whether a next page exists without a second query.
	probe := filter
	probe.Limit++

	entries, err := as.AuditRepo.ListAudit(ctx, probe)
	if err != nil {
		return domain.AuditPage{}, err
	}

	page := domain.AuditPage{Entries: entries}

	if len(entries) > filter.Limit {
		page.Entries = entries[:filter.Limit]
		page.NextBeforeID = page.Entries[len(page.Entries)-1].ID
	}

	return page, nil
}

// StartAuditRetention deletes audit entries older than retentionDays now and then every hour, until
// ctx ends. A non-positive retentionDays keeps the log forever.
func (as *AggregatorService) StartAuditRetention(ctx context.Context) {
	if as.AuditRepo == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(auditPurgeInterval)
		defer ticker.Stop()

		for {
			// Read each round: AUDIT_RETENTION_DAYS can change at runtime; 0 keeps everything.
			if days := as.auditRetentionDays(); days > 0 {
				as.purgeAudit(ctx, days)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (as *AggregatorService) purgeAudit(ctx context.Context, retentionDays int) {
	cutoff := as.currentTime().AddDate(0, 0, -retentionDays)

	removed, err := as.AuditRepo.DeleteAuditBefore(ctx, cutoff)
	if err != nil {
		log.Printf("purge audit log: %v", err)

		return
	}

	if removed > 0 {
		log.Printf("purged %d audit entries older than %d days", removed, retentionDays)
	}
}
