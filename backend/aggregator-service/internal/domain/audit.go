package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrInvalidAudit is returned for an audit event or filter that breaks the rules below.
var ErrInvalidAudit = errors.New("invalid audit event")

// Actor kinds, outcomes and target kinds of the audit log (docs/admin.md).
const (
	AuditActorAdmin     = "admin"
	AuditActorUser      = "user"
	AuditActorAnonymous = "anonymous"
	AuditActorSystem    = "system"

	AuditOutcomeOK     = "ok"
	AuditOutcomeDenied = "denied"
	AuditOutcomeError  = "error"

	AuditTargetNone  = ""
	AuditTargetRepo  = "repo"
	AuditTargetBatch = "batch"
	AuditTargetRoute = "route"
	// AuditTargetCatalog is a public repo catalog refresh; Target is its ID.
	AuditTargetCatalog = "catalog"

	// MaxAuditDetailsBytes bounds the free-form details: the log records what happened, not payloads.
	MaxAuditDetailsBytes = 2048
	MaxAuditTargetLength = 200
	MaxAuditActionLength = 64

	DefaultAuditPageSize = 50
	MaxAuditPageSize     = 200
)

var (
	auditActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	auditHashPattern   = regexp.MustCompile(`^[0-9a-f]{0,64}$`)

	auditActors   = map[string]bool{AuditActorAdmin: true, AuditActorUser: true, AuditActorAnonymous: true, AuditActorSystem: true}
	auditOutcomes = map[string]bool{AuditOutcomeOK: true, AuditOutcomeDenied: true, AuditOutcomeError: true}
	auditTargets  = map[string]bool{AuditTargetNone: true, AuditTargetRepo: true, AuditTargetBatch: true, AuditTargetRoute: true, AuditTargetCatalog: true}
)

// AuditEvent is what a caller reports. The ActorHash is computed by the caller from the Yandex ID,
// so a raw ID never reaches the Aggregator; Target is the slug of a public repo, an ID, or a hash.
type AuditEvent struct {
	ActorKind  string
	ActorHash  string
	Action     string
	TargetKind string
	Target     string
	Outcome    string
	// Details is a small JSON object with non-sensitive context (a reason, a count).
	Details json.RawMessage
}

// Validate checks the event against the audit rules. It does not inspect Details beyond shape and
// size: callers own keeping tokens and private slugs out of it.
func (e AuditEvent) Validate() error {
	switch {
	case !auditActors[e.ActorKind]:
		return fmt.Errorf("%w: unknown actor kind %q", ErrInvalidAudit, e.ActorKind)
	case !auditHashPattern.MatchString(e.ActorHash):
		return fmt.Errorf("%w: actor hash must be lowercase hex up to 64 characters", ErrInvalidAudit)
	case len(e.Action) > MaxAuditActionLength || !auditActionPattern.MatchString(e.Action):
		return fmt.Errorf("%w: action must be dotted lowercase words, up to %d characters", ErrInvalidAudit, MaxAuditActionLength)
	case !auditTargets[e.TargetKind]:
		return fmt.Errorf("%w: unknown target kind %q", ErrInvalidAudit, e.TargetKind)
	case len(e.Target) > MaxAuditTargetLength:
		return fmt.Errorf("%w: target is longer than %d characters", ErrInvalidAudit, MaxAuditTargetLength)
	case e.TargetKind == AuditTargetNone && e.Target != "":
		return fmt.Errorf("%w: a target needs a target kind", ErrInvalidAudit)
	case !auditOutcomes[e.Outcome]:
		return fmt.Errorf("%w: unknown outcome %q", ErrInvalidAudit, e.Outcome)
	case e.ActorKind == AuditActorAnonymous && e.ActorHash != "":
		return fmt.Errorf("%w: an anonymous actor has no hash", ErrInvalidAudit)
	}

	if len(e.Details) > MaxAuditDetailsBytes {
		return fmt.Errorf("%w: details are longer than %d bytes", ErrInvalidAudit, MaxAuditDetailsBytes)
	}

	if len(e.Details) > 0 {
		var object map[string]any
		if err := json.Unmarshal(e.Details, &object); err != nil || object == nil {
			return fmt.Errorf("%w: details must be a JSON object", ErrInvalidAudit)
		}
	}

	return nil
}

// AuditEntry is a stored audit event.
type AuditEntry struct {
	ID         int64
	OccurredAt time.Time
	AuditEvent
}

// Audit scopes of the list filter. User actions are the entries whose action starts with one of
// UserActionPrefixes; the rest is the admin journal.
const (
	AuditScopeAdmin = "admin"
	AuditScopeUsers = "users"
)

// UserActionPrefixes are the action prefixes of what ordinary users do (docs/admin.md).
var UserActionPrefixes = []string{"grading.", "recommendations."}

// AuditFilter selects a page of the log, newest first. BeforeID is the keyset cursor: only entries
// with a smaller ID are returned. Action matches an exact action or, ending in ".", a prefix.
type AuditFilter struct {
	Limit    int
	BeforeID int64
	Action   string
	Outcome  string
	// Scope splits the log in two: AuditScopeUsers keeps the user-action entries, AuditScopeAdmin
	// everything else; empty keeps all.
	Scope string
}

// AuditPage is one page and the cursor for the next one (0 when this was the last).
type AuditPage struct {
	Entries      []AuditEntry
	NextBeforeID int64
}

// AuditRepository stores the audit log. Only the Aggregator writes it.
type AuditRepository interface {
	InsertAudit(ctx context.Context, event AuditEvent) error
	ListAudit(ctx context.Context, filter AuditFilter) ([]AuditEntry, error)
	// DeleteAuditBefore removes entries older than cutoff and returns how many it removed.
	DeleteAuditBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
