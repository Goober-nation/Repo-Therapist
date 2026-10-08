package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"gateway-service/internal/domain"
)

// MaxAdminPasswordLength bounds the password a request may carry, so a huge body is never hashed.
const MaxAdminPasswordLength = 256

// SetAdminUnlock turns on the password unlock: a signed-in account that sends the right password is an
// admin for the rest of its session (grantTTL, the session lifetime). guard counts wrong passwords and
// locks. Without a password, grants or guard the unlock stays off.
func (s *GatewayService) SetAdminUnlock(password string, grants domain.AdminGrantStore, guard domain.AttemptGuard, grantTTL time.Duration) {
	s.adminPassword = password
	s.adminGrants = grants
	s.attemptGuard = guard
	s.grantTTL = grantTTL
}

// UnlockAvailable reports whether the password unlock works: a password, both stores and the hash
// secret that keys the grant fingerprint and the audit actor hash.
func (s *GatewayService) UnlockAvailable() bool {
	return s.adminPassword != "" && s.adminGrants != nil && s.attemptGuard != nil && len(s.auditSecret) > 0
}

// passwordFingerprint identifies the configured password without revealing it. A grant stores it, so
// changing ADMIN_PASSWORD stops every earlier grant from counting.
func (s *GatewayService) passwordFingerprint() string {
	mac := hmac.New(sha256.New, s.auditSecret)
	mac.Write([]byte("admin-unlock:" + s.adminPassword))

	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// hasGrant reports whether the session holds a valid password unlock.
func (s *GatewayService) hasGrant(ctx context.Context, sessionID string) (bool, error) {
	if !s.UnlockAvailable() || sessionID == "" {
		return false, nil
	}

	fingerprint, err := s.adminGrants.Fingerprint(ctx, sessionID)
	if err != nil {
		return false, err
	}

	return fingerprint != "" && hmac.Equal([]byte(fingerprint), []byte(s.passwordFingerprint())), nil
}

// passwordMatches compares in constant time: both sides are hashed first so the lengths are equal and
// neither the content nor the length of the real password shows in the timing.
func (s *GatewayService) passwordMatches(candidate string) bool {
	given := sha256.Sum256([]byte(candidate))
	configured := sha256.Sum256([]byte(s.adminPassword))

	return subtle.ConstantTimeCompare(given[:], configured[:]) == 1
}

// UnlockAdmin gives the signed-in account of the session admin rights, for the life of the session, when
// the password is right. It needs a Yandex session: no session is ErrSessionNotFound. A wrong password is
// ErrForbidden; after too many from one IP or one account every attempt is an AdminLockedError until the
// lock ends, without the password being checked. Every attempt is audited, never with the password.
func (s *GatewayService) UnlockAdmin(ctx context.Context, clientIP, sessionID, password string) error {
	if !s.UnlockAvailable() {
		return domain.ErrForbidden
	}

	user, err := s.adminFromSession(ctx, sessionID)
	if errors.Is(err, domain.ErrSessionNotFound) {
		reason := denyInvalidSession
		if sessionID == "" {
			reason = denyNoSession
		}

		s.recordUnlockDenied(ctx, clientIP, domain.AuditActorAnonymous, "", reason, true)

		return domain.ErrSessionNotFound
	}

	if err != nil {
		return fmt.Errorf("check unlock session: %w", err)
	}

	hash := s.ActorHash(user.YandexID)
	keys := []string{"ip:" + clientIP, "user:" + user.ID.String()}

	retryAfter, err := s.attemptGuard.LockedFor(ctx, keys...)
	if err != nil {
		return fmt.Errorf("check unlock lock: %w", err)
	}

	if retryAfter > 0 {
		s.recordUnlockDenied(ctx, clientIP, domain.AuditActorUser, hash, "locked", true)

		return domain.AdminLockedError{RetryAfter: retryAfter}
	}

	if password == "" || len(password) > MaxAdminPasswordLength {
		return fmt.Errorf("%w: password", domain.ErrInvalidRequest)
	}

	if !s.passwordMatches(password) {
		if _, err := s.attemptGuard.RecordFailure(ctx, keys...); err != nil {
			log.Printf("record wrong admin password: %v", err)
		}

		s.recordUnlockDenied(ctx, clientIP, domain.AuditActorUser, hash, "wrong_password", false)

		return domain.ErrForbidden
	}

	if err := s.attemptGuard.Reset(ctx, keys...); err != nil {
		log.Printf("reset admin unlock failures: %v", err)
	}

	if err := s.adminGrants.Grant(ctx, sessionID, s.passwordFingerprint(), s.grantTTL); err != nil {
		return err
	}

	s.countUnlock("ok")

	s.recordAdminEvent(ctx, domain.AuditEvent{
		ActorKind:  domain.AuditActorAdmin,
		ActorHash:  hash,
		Action:     "admin.unlock",
		TargetKind: domain.AuditTargetRoute,
		Target:     "/admin/unlock",
		Outcome:    domain.AuditOutcomeOK,
		Details:    json.RawMessage(`{"via":"password"}`),
	})

	return nil
}

// LockAdmin takes the password unlock off the session. It is a no-op when there was none, and an admin
// on the list stays an admin. It needs a session: none is ErrSessionNotFound.
func (s *GatewayService) LockAdmin(ctx context.Context, sessionID string) error {
	if !s.UnlockAvailable() {
		return domain.ErrForbidden
	}

	user, err := s.adminFromSession(ctx, sessionID)
	if err != nil {
		return err
	}

	removed, err := s.adminGrants.Revoke(ctx, sessionID)
	if err != nil {
		return err
	}

	if removed {
		s.recordAdminEvent(ctx, domain.AuditEvent{
			ActorKind:  domain.AuditActorAdmin,
			ActorHash:  s.ActorHash(user.YandexID),
			Action:     "admin.lock",
			TargetKind: domain.AuditTargetRoute,
			Target:     "/admin/lock",
			Outcome:    domain.AuditOutcomeOK,
			Details:    json.RawMessage(`{"via":"password"}`),
		})
	}

	return nil
}

// recordUnlockDenied audits a refused unlock attempt. limited applies the per-IP budget of denied
// records, for the cases a client can repeat without limit (no session, locked); a wrong password is
// already bounded by the lock.
func (s *GatewayService) recordUnlockDenied(ctx context.Context, clientIP, actorKind, actorHash, reason string, limited bool) {
	s.countUnlock(reason) // counted before the audit budget, so floods stay visible

	if limited && s.adminDeniedLog != nil {
		allowed, err := s.adminDeniedLog.Allow(ctx, "rate_limit:gateway:admin_denied:ip:"+clientIP)
		if err != nil || !allowed {
			return
		}
	}

	details, _ := json.Marshal(map[string]string{"reason": reason})

	s.recordAdminEvent(ctx, domain.AuditEvent{
		ActorKind:  actorKind,
		ActorHash:  actorHash,
		Action:     "admin.unlock",
		TargetKind: domain.AuditTargetRoute,
		Target:     "/admin/unlock",
		Outcome:    domain.AuditOutcomeDenied,
		Details:    details,
	})
}

// recordAdminEvent reports an event best effort: a failed write never changes the caller's answer.
func (s *GatewayService) recordAdminEvent(ctx context.Context, event domain.AuditEvent) {
	if err := s.RecordAudit(ctx, event); err != nil {
		log.Printf("record %s: %v", event.Action, err)
	}
}
