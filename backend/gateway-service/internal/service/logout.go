package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gateway-service/internal/domain"
)

// Logout ends the session: the cookie stops working at once, and a password unlock held by the
// session goes with it. It is idempotent: no session, or one that is already gone, is success, so a
// repeated or stale logout never fails. The unlock is audited when there was one.
func (s *GatewayService) Logout(ctx context.Context, clientIP, sessionID string) error {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return err
	}

	if sessionID == "" || s.sessionRepository == nil {
		return nil
	}

	// The account is looked up only to audit an unlock being dropped; a missing one changes nothing.
	user, userErr := s.adminFromSession(ctx, sessionID)
	if userErr != nil && !errors.Is(userErr, domain.ErrSessionNotFound) {
		return fmt.Errorf("check session for logout: %w", userErr)
	}

	revoked := false

	if s.UnlockAvailable() {
		var err error

		revoked, err = s.adminGrants.Revoke(ctx, sessionID)
		if err != nil {
			return err
		}
	}

	if err := s.sessionRepository.DeleteSession(ctx, sessionID); err != nil && !errors.Is(err, domain.ErrSessionNotFound) {
		return fmt.Errorf("delete session: %w", err)
	}

	if revoked && userErr == nil {
		s.recordAdminEvent(ctx, domain.AuditEvent{
			ActorKind:  domain.AuditActorAdmin,
			ActorHash:  s.ActorHash(user.YandexID),
			Action:     "admin.lock",
			TargetKind: domain.AuditTargetRoute,
			Target:     "/auth/logout",
			Outcome:    domain.AuditOutcomeOK,
			Details:    json.RawMessage(`{"via":"logout"}`),
		})
	}

	return nil
}
