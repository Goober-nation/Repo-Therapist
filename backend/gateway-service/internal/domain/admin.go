package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AdminIdentity is an authenticated admin. ActorHash is the audit-log form of their Yandex ID; the
// raw ID is never kept past the check.
type AdminIdentity struct {
	UserID    uuid.UUID
	ActorHash string
}

// Audit actor kinds, outcomes and target kinds; they mirror the Aggregator's rules (docs/admin.md).
const (
	AuditActorAdmin     = "admin"
	AuditActorUser      = "user"
	AuditActorAnonymous = "anonymous"

	AuditOutcomeOK     = "ok"
	AuditOutcomeDenied = "denied"
	AuditOutcomeError  = "error"

	AuditTargetRoute = "route"
	AuditTargetRepo  = "repo"
)

// AuditEvent is one event the Gateway reports to the Aggregator, which owns the audit log.
type AuditEvent struct {
	ActorKind  string
	ActorHash  string
	Action     string
	TargetKind string
	Target     string
	Outcome    string
	Details    json.RawMessage
}

// AuditEntry is a stored audit event.
type AuditEntry struct {
	ID         int64
	OccurredAt time.Time
	AuditEvent
}

// AuditFilter selects a page of the log; zero values mean the Aggregator's defaults. Scope is
// "admin" (the admin journal), "users" (the user-action entries) or empty (both).
type AuditFilter struct {
	Limit    int32
	BeforeID int64
	Action   string
	Outcome  string
	Scope    string
}

// Audit actions of what a user does (docs/admin.md).
const (
	AuditActionGrading         = "grading.request"
	AuditActionForceGrading    = "grading.force"
	AuditActionRecommendations = "recommendations.request"
)

// UserAction is one user request to report to the audit log once it is answered. Err is what the
// request returned; HasToken tells whether it carried an access token (so the repo may be private).
type UserAction struct {
	Action    string
	SessionID string
	ClientIP  string
	HasToken  bool
	OrgSlug   string
	RepoSlug  string
	Err       error
}

// AuditPage is one page, newest first, and the cursor of the next (0 on the last page).
type AuditPage struct {
	Entries      []AuditEntry
	NextBeforeID int64
}

// AdminStatus is what the Gateway tells a caller about their own admin rights. UnlockAvailable says
// whether a password unlock is configured at all, so the UI can explain when it is not.
type AdminStatus struct {
	SignedIn        bool
	IsAdmin         bool
	UnlockAvailable bool
	// ViaPassword is true when the rights come from a password unlock of this session (and can be
	// taken off again), false for an admin on the list.
	ViaPassword bool
}

// AdminLockedError means too many wrong passwords: no attempt is accepted for RetryAfter. It matches
// ErrLimitExceeded with errors.Is.
type AdminLockedError struct {
	RetryAfter time.Duration
}

func (e AdminLockedError) Error() string {
	return "too many wrong passwords"
}

func (e AdminLockedError) Unwrap() error {
	return ErrLimitExceeded
}
