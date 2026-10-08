package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"gateway-service/internal/domain"
)

// repoSlugPattern is what a slug must look like to be written to the log as it is. Anything else is
// not recorded: the log keeps what happened, not arbitrary text a visitor typed into a URL.
var repoSlugPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// SetUserAudit turns on the audit of user actions. dedup lets one actor's identical action on one
// target through once per its window, so a page that polls cannot fill the log; nil turns it off.
func (s *GatewayService) SetUserAudit(dedup Limiter) {
	s.userAuditDedup = dedup
}

// RecordUserAction reports what a user did to the audit log. It is best effort: a failure or a
// skipped event never changes the answer the user gets.
//
// Privacy: a request that carries an access token may be about a private repository, so its target
// is a keyed hash and never the slug. A token-less request is recorded by slug only when it got an
// answer about a public repo; a refusal (the repo exists but is private) is hashed as well. The actor is the keyed hash of the Yandex ID, or anonymous. Nothing else of
// the request (IP, query string, token) is stored.
func (s *GatewayService) RecordUserAction(ctx context.Context, action domain.UserAction) {
	if s.userAuditDedup == nil || len(s.auditSecret) == 0 {
		return
	}

	if !repoSlugPattern.MatchString(action.OrgSlug) || !repoSlugPattern.MatchString(action.RepoSlug) {
		return
	}

	outcome, details, ok := userActionOutcome(action)
	if !ok {
		return
	}

	slug := strings.ToLower(action.OrgSlug + "/" + action.RepoSlug)
	target, access := slug, "public"

	if action.HasToken {
		access = "token"
	}

	if action.HasToken || outcome == domain.AuditOutcomeDenied {
		target = s.ActorHash("repo:" + slug)
	}

	details["access"] = access

	body, err := json.Marshal(details)
	if err != nil {
		return
	}

	event := domain.AuditEvent{
		ActorKind:  domain.AuditActorAnonymous,
		Action:     action.Action,
		TargetKind: domain.AuditTargetRepo,
		Target:     target,
		Outcome:    outcome,
		Details:    body,
	}

	actorKey := "ip:" + action.ClientIP

	if user, err := s.adminFromSession(ctx, action.SessionID); err == nil {
		event.ActorKind, event.ActorHash = domain.AuditActorUser, s.ActorHash(user.YandexID)
		actorKey = event.ActorHash
	}

	key := fmt.Sprintf("audit:user_action:%s:%s:%s:%s:%s", action.Action, actorKey, target, outcome, details["state"]+details["reason"])

	allowed, err := s.userAuditDedup.Allow(ctx, key)
	if err != nil || !allowed {
		return
	}

	if err := s.RecordAudit(ctx, event); err != nil {
		log.Printf("record user action %s: %v", action.Action, err)
	}
}

// userActionOutcome maps what the request returned to an audit outcome and its fixed reason words.
// ok is false for results that are not worth a row: a malformed request and the per-client request
// limit, which the metrics already count.
func userActionOutcome(action domain.UserAction) (string, map[string]string, bool) {
	var limit domain.GradingLimitError

	switch {
	case action.Err == nil:
		return domain.AuditOutcomeOK, map[string]string{"state": "ready"}, true
	case errors.Is(action.Err, domain.ErrPending):
		return domain.AuditOutcomeOK, map[string]string{"state": "pending"}, true
	case errors.As(action.Err, &limit):
		return domain.AuditOutcomeDenied, map[string]string{"reason": "grading_limit"}, true
	case errors.Is(action.Err, domain.ErrLimitExceeded), errors.Is(action.Err, domain.ErrInvalidRequest):
		return "", nil, false
	case errors.Is(action.Err, domain.ErrForbidden), errors.Is(action.Err, domain.ErrUnauthenticated):
		return domain.AuditOutcomeDenied, map[string]string{"reason": "no_access"}, true
	case errors.Is(action.Err, domain.ErrNotFound):
		return domain.AuditOutcomeError, map[string]string{"reason": "not_found"}, true
	case errors.Is(action.Err, domain.ErrGradingInterrupted):
		return domain.AuditOutcomeError, map[string]string{"reason": "interrupted"}, true
	default:
		return domain.AuditOutcomeError, map[string]string{"reason": "failed"}, true
	}
}
