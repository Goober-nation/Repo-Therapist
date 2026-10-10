package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeCooldown lets each key through once and then answers the canned wait.
type fakeCooldown struct {
	taken map[string]bool
	wait  time.Duration
	err   error
}

func (f *fakeCooldown) Acquire(_ context.Context, key string) (time.Duration, error) {
	if f.err != nil {
		return 0, f.err
	}

	if f.taken[key] {
		return f.wait, nil
	}

	f.taken[key] = true

	return 0, nil
}

func newForceFixture(t *testing.T) (*adminFixture, *fakeCooldown) {
	t.Helper()

	f := newAdminFixture(t, nil)
	f.agg.grade = &agg.RepoGrade{SourcecraftUuid: uuid.NewString(), GradedAt: timestamppb.Now()}
	cooldown := &fakeCooldown{taken: map[string]bool{}, wait: 42 * time.Minute}
	f.svc.SetForceRegradeCooldown(cooldown)

	return f, cooldown
}

func forceDTO(session string) domain.GetGradingDTO {
	return domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r", Force: true, SessionID: session}
}

func TestForceRegradeForwardsForce(t *testing.T) {
	f, _ := newForceFixture(t)

	if _, err := f.svc.GetGrading(context.Background(), "ip", "", forceDTO("sess-member")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.agg.gradingIn == nil || !f.agg.gradingIn.GetForce() {
		t.Fatalf("request = %+v, want force", f.agg.gradingIn)
	}
}

func TestForceRegradeNeedsSession(t *testing.T) {
	f, _ := newForceFixture(t)

	for _, session := range []string{"", "sess-unknown", "sess-ghost"} {
		_, err := f.svc.GetGrading(context.Background(), "ip", "", forceDTO(session))
		if !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("session %q: err = %v, want ErrUnauthenticated", session, err)
		}
	}

	if f.agg.gradingIn != nil {
		t.Error("a refused force regrade must not reach the aggregator")
	}
}

func TestForceRegradeCooldownPerUserAndRepo(t *testing.T) {
	f, _ := newForceFixture(t)
	ctx := context.Background()

	if _, err := f.svc.GetGrading(ctx, "ip", "", forceDTO("sess-member")); err != nil {
		t.Fatalf("first force: %v", err)
	}

	f.agg.gradingIn = nil
	_, err := f.svc.GetGrading(ctx, "ip", "", forceDTO("sess-member"))

	var cooldown domain.ForceRegradeCooldownError
	if !errors.As(err, &cooldown) || cooldown.RetryAfter != 42*time.Minute || !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("second force err = %v, want the cooldown with 42m left", err)
	}

	if f.agg.gradingIn != nil {
		t.Error("a force regrade in cooldown must not reach the aggregator")
	}

	// Another user, and the same user on another repo, are not held back.
	if _, err := f.svc.GetGrading(ctx, "ip", "", forceDTO("sess-admin")); err != nil {
		t.Errorf("other user: %v", err)
	}

	other := forceDTO("sess-member")
	other.RepoSlug = "other"
	if _, err := f.svc.GetGrading(ctx, "ip", "", other); err != nil {
		t.Errorf("other repo: %v", err)
	}

	// A plain grading request is never held back by the cooldown.
	if _, err := f.svc.GetGrading(ctx, "ip", "", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r", SessionID: "sess-member"}); err != nil {
		t.Errorf("plain request: %v", err)
	}

	if f.agg.gradingIn.GetForce() {
		t.Error("a plain request must not force")
	}
}

func TestForceRegradeCooldownFailureRefuses(t *testing.T) {
	f, cooldown := newForceFixture(t)
	cooldown.err = errors.New("redis down")

	_, err := f.svc.GetGrading(context.Background(), "ip", "", forceDTO("sess-member"))
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
