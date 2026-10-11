package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gateway-service/internal/domain"
)

// Cooldown lets one key through once per its period. Acquire answers 0 when the key got through and
// how long is left of the running period otherwise.
type Cooldown interface {
	Acquire(ctx context.Context, key string) (time.Duration, error)
}

// SetForceRegradeCooldown sets how often one user may force a regrade of one repo; nil means no limit.
func (s *GatewayService) SetForceRegradeCooldown(cooldown Cooldown) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.forceCooldown = cooldown
}

// checkForceRegrade lets a forced regrade through for a signed-in session once per repo per cooldown.
// The cooldown is per user, so signing in again does not reset it.
func (s *GatewayService) checkForceRegrade(ctx context.Context, request domain.GetGradingDTO) error {
	user, err := s.adminFromSession(ctx, request.SessionID)
	if errors.Is(err, domain.ErrSessionNotFound) {
		return fmt.Errorf("%w: force regrade needs a signed-in session", domain.ErrUnauthenticated)
	}

	if err != nil {
		return fmt.Errorf("check force regrade session: %w", err)
	}

	s.liveMu.RLock()
	cooldown := s.forceCooldown
	s.liveMu.RUnlock()
	if cooldown == nil {
		return nil
	}

	key := "force_regrade:" + user.ID.String() + ":" + strings.ToLower(request.OrgSlug+"/"+request.RepoSlug)

	retryAfter, err := cooldown.Acquire(ctx, key)
	if err != nil {
		return fmt.Errorf("%w: force regrade cooldown: %v", domain.ErrUnavailable, err)
	}

	if retryAfter > 0 {
		return domain.ForceRegradeCooldownError{RetryAfter: retryAfter}
	}

	return nil
}
