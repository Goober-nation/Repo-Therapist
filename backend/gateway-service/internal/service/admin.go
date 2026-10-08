package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"
)

// actorHashLength is how many hex characters of the HMAC the audit log keeps: enough to tell actors
// apart, too short to be worth reversing.
const actorHashLength = 16

// Reasons a guarded admin route was refused, recorded in the audit details.
const (
	denyNoSession      = "no_session"
	denyInvalidSession = "invalid_session"
	denyNotAdmin       = "not_admin"
)

// AdminMetrics counts admin attempts for monitoring (docs/metrics.md). Reasons and results are the fixed
// words of the audit log.
type AdminMetrics interface {
	AdminDenied(reason string)
	AdminUnlock(result string)
}

// SetMetrics turns on the counters of refused admin attempts and unlock attempts; nil leaves them off.
func (s *GatewayService) SetMetrics(m AdminMetrics) { s.adminMetrics = m }

func (s *GatewayService) countDenied(reason string) {
	if s.adminMetrics != nil {
		s.adminMetrics.AdminDenied(reason)
	}
}

func (s *GatewayService) countUnlock(result string) {
	if s.adminMetrics != nil {
		s.adminMetrics.AdminUnlock(result)
	}
}

// SetAdmin turns the admin routes on: ids are the admins' Yandex IDs and secret keys the actor
// hash. Without both the routes stay off, because the audit log must never hold a raw ID. deniedLog
// limits how many refused attempts per IP are recorded; nil records them all.
func (s *GatewayService) SetAdmin(ids []string, secret string, deniedLog Limiter) {
	s.adminIDs = make(map[string]struct{}, len(ids))
	for _, id := range ids {
		s.adminIDs[id] = struct{}{}
	}

	s.auditSecret = []byte(secret)
	s.adminDeniedLog = deniedLog
}

// AdminEnabled reports whether any admin can get in: the hash secret plus at least one way in, an
// admin ID on the list or a configured unlock password.
func (s *GatewayService) AdminEnabled() bool {
	return len(s.auditSecret) > 0 && (len(s.adminIDs) > 0 || s.adminPassword != "")
}

// ActorHash is the audit-log form of a Yandex ID: a keyed hash, truncated.
func (s *GatewayService) ActorHash(yandexID string) string {
	mac := hmac.New(sha256.New, s.auditSecret)
	mac.Write([]byte(yandexID))

	return hex.EncodeToString(mac.Sum(nil))[:actorHashLength]
}

// adminFromSession resolves a session cookie to the user's Yandex ID. A missing or unknown session is
// ErrSessionNotFound; only infrastructure failures are other errors.
func (s *GatewayService) adminFromSession(ctx context.Context, sessionID string) (domain.User, error) {
	if sessionID == "" || s.sessionRepository == nil || s.userRepository == nil {
		return domain.User{}, domain.ErrSessionNotFound
	}

	userID, err := s.sessionRepository.GetSession(ctx, sessionID)
	if err != nil {
		return domain.User{}, err
	}

	user, err := s.userRepository.GetUserByID(ctx, userID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return domain.User{}, domain.ErrSessionNotFound
	}

	return user, err
}

// AdminStatus tells a caller about their own admin rights. It never audits: the UI calls it on every
// page load to decide which tabs to show, so "no" is the normal answer.
func (s *GatewayService) AdminStatus(ctx context.Context, sessionID string) (domain.AdminStatus, error) {
	status := domain.AdminStatus{UnlockAvailable: s.UnlockAvailable()}

	// Whether the session is valid is answered whatever the admin settings are: the UI uses it to notice
	// that its own "signed in" state has outlived the Gateway's session.
	user, err := s.adminFromSession(ctx, sessionID)
	if errors.Is(err, domain.ErrSessionNotFound) {
		return status, nil
	}

	if err != nil {
		return domain.AdminStatus{}, fmt.Errorf("check admin session: %w", err)
	}

	status.SignedIn = true

	if !s.AdminEnabled() {
		return status, nil
	}

	if _, listed := s.adminIDs[user.YandexID]; listed {
		status.IsAdmin = true
	}

	granted, err := s.hasGrant(ctx, sessionID)
	if err != nil {
		return domain.AdminStatus{}, err
	}

	status.ViaPassword = granted
	status.IsAdmin = status.IsAdmin || granted

	return status, nil
}

// IsAdmin reports whether the session belongs to an admin, by the list or by a password unlock.
func (s *GatewayService) IsAdmin(ctx context.Context, sessionID string) (bool, error) {
	status, err := s.AdminStatus(ctx, sessionID)

	return status.IsAdmin, err
}

// isAdminAccount reports whether a signed-in user may use the admin routes in this session.
func (s *GatewayService) isAdminAccount(ctx context.Context, user domain.User, sessionID string) (bool, error) {
	if _, listed := s.adminIDs[user.YandexID]; listed {
		return true, nil
	}

	return s.hasGrant(ctx, sessionID)
}

// AuthenticateAdmin guards an admin route. No session or an unknown one is ErrSessionNotFound (401), a
// signed-in non-admin is ErrForbidden (403); both are recorded in the audit log, rate-limited per IP,
// with the route path and a reason but no query string and no IP. With the admin routes off it answers
// ErrForbidden without recording anything.
func (s *GatewayService) AuthenticateAdmin(ctx context.Context, clientIP, sessionID, route string) (domain.AdminIdentity, error) {
	if !s.AdminEnabled() {
		return domain.AdminIdentity{}, domain.ErrForbidden
	}

	if sessionID == "" {
		s.recordDenied(ctx, clientIP, domain.AuditActorAnonymous, "", route, denyNoSession)

		return domain.AdminIdentity{}, domain.ErrSessionNotFound
	}

	user, err := s.adminFromSession(ctx, sessionID)
	if errors.Is(err, domain.ErrSessionNotFound) {
		s.recordDenied(ctx, clientIP, domain.AuditActorAnonymous, "", route, denyInvalidSession)

		return domain.AdminIdentity{}, domain.ErrSessionNotFound
	}

	if err != nil {
		return domain.AdminIdentity{}, fmt.Errorf("check admin session: %w", err)
	}

	hash := s.ActorHash(user.YandexID)

	isAdmin, err := s.isAdminAccount(ctx, user, sessionID)
	if err != nil {
		return domain.AdminIdentity{}, err
	}

	if !isAdmin {
		s.recordDenied(ctx, clientIP, domain.AuditActorUser, hash, route, denyNotAdmin)

		return domain.AdminIdentity{}, domain.ErrForbidden
	}

	return domain.AdminIdentity{UserID: user.ID, ActorHash: hash}, nil
}

// recordDenied reports a refused admin attempt. It is best effort: a failed or rate-limited record
// never changes the answer to the caller.
func (s *GatewayService) recordDenied(ctx context.Context, clientIP, actorKind, actorHash, route, reason string) {
	s.countDenied(reason) // every refusal is counted, also those the audit budget skips

	if s.adminDeniedLog != nil {
		allowed, err := s.adminDeniedLog.Allow(ctx, "rate_limit:gateway:admin_denied:ip:"+clientIP)
		if err != nil || !allowed {
			return
		}
	}

	details, _ := json.Marshal(map[string]string{"reason": reason})

	err := s.RecordAudit(ctx, domain.AuditEvent{
		ActorKind:  actorKind,
		ActorHash:  actorHash,
		Action:     "admin.denied",
		TargetKind: domain.AuditTargetRoute,
		Target:     route,
		Outcome:    domain.AuditOutcomeDenied,
		Details:    details,
	})
	if err != nil {
		log.Printf("record denied admin attempt: %v", err)
	}
}

// RecordAudit reports one event to the Aggregator, the only writer of the audit log.
func (s *GatewayService) RecordAudit(ctx context.Context, event domain.AuditEvent) error {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	_, err := s.aggregatorClient.RecordAudit(rpcCtx, &agg.RecordAuditRequest{
		ActorKind:  event.ActorKind,
		ActorHash:  event.ActorHash,
		Action:     event.Action,
		TargetKind: event.TargetKind,
		Target:     event.Target,
		Outcome:    event.Outcome,
		Details:    event.Details,
	})
	if err != nil {
		return rpcError("record audit", err)
	}

	return nil
}

// ListAudit returns one page of the audit log, newest first.
func (s *GatewayService) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.ListAudit(rpcCtx, &agg.ListAuditRequest{
		Limit:    filter.Limit,
		BeforeId: filter.BeforeID,
		Action:   filter.Action,
		Outcome:  filter.Outcome,
		Scope:    filter.Scope,
	})
	if err != nil {
		return domain.AuditPage{}, rpcError("list audit", err)
	}

	page := domain.AuditPage{
		Entries:      make([]domain.AuditEntry, 0, len(resp.GetEntries())),
		NextBeforeID: resp.GetNextBeforeId(),
	}

	for _, entry := range resp.GetEntries() {
		page.Entries = append(page.Entries, domain.AuditEntry{
			ID:         entry.GetId(),
			OccurredAt: entry.GetOccurredAt().AsTime(),
			AuditEvent: domain.AuditEvent{
				ActorKind:  entry.GetActorKind(),
				ActorHash:  entry.GetActorHash(),
				Action:     entry.GetAction(),
				TargetKind: entry.GetTargetKind(),
				Target:     entry.GetTarget(),
				Outcome:    entry.GetOutcome(),
				Details:    entry.GetDetails(),
			},
		})
	}

	return page, nil
}
