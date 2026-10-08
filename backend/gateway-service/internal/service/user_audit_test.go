package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"
)

// onceLimiter lets each key through once, like the Redis limiter set to one per window.
type onceLimiter struct {
	seen map[string]bool
	err  error
}

func (l *onceLimiter) Allow(_ context.Context, key string) (bool, error) {
	if l.err != nil {
		return false, l.err
	}

	if l.seen == nil {
		l.seen = map[string]bool{}
	}

	if l.seen[key] {
		return false, nil
	}

	l.seen[key] = true

	return true, nil
}

func userAuditFixture(t *testing.T) *adminFixture {
	t.Helper()

	f := newAdminFixture(t, nil)
	f.svc.SetUserAudit(&onceLimiter{})

	return f
}

func gradingAction(err error) domain.UserAction {
	return domain.UserAction{Action: domain.AuditActionGrading, SessionID: "sess-member", ClientIP: "203.0.113.9", OrgSlug: "Acme", RepoSlug: "Tools", Err: err}
}

func TestUserActionIsRecordedWithTheHashedActorAndThePublicSlug(t *testing.T) {
	f := userAuditFixture(t)

	f.svc.RecordUserAction(context.Background(), gradingAction(nil))

	if len(f.agg.recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(f.agg.recorded))
	}

	event := f.agg.recorded[0]
	if event.GetActorKind() != domain.AuditActorUser || event.GetActorHash() != f.svc.ActorHash(memberYID) {
		t.Errorf("actor = %s/%s, want the member's hash", event.GetActorKind(), event.GetActorHash())
	}

	if event.GetAction() != "grading.request" || event.GetTargetKind() != "repo" || event.GetTarget() != "acme/tools" || event.GetOutcome() != "ok" {
		t.Errorf("event = %+v", event)
	}

	if got := string(event.GetDetails()); !strings.Contains(got, `"state":"ready"`) || !strings.Contains(got, `"access":"public"`) {
		t.Errorf("details = %s", got)
	}

	if strings.Contains(fmt.Sprint(event), "203.0.113.9") || strings.Contains(fmt.Sprint(event), memberYID) {
		t.Errorf("the event holds the IP or the raw Yandex ID: %v", event)
	}
}

func TestAnAnonymousVisitorIsRecordedWithoutAHash(t *testing.T) {
	for _, session := range []string{"", "nonsense", "sess-ghost"} {
		f := userAuditFixture(t)
		action := gradingAction(nil)
		action.SessionID = session

		f.svc.RecordUserAction(context.Background(), action)

		if len(f.agg.recorded) != 1 || f.agg.recorded[0].GetActorKind() != domain.AuditActorAnonymous || f.agg.recorded[0].GetActorHash() != "" {
			t.Errorf("session %q: recorded %+v, want one anonymous event without a hash", session, f.agg.recorded)
		}
	}
}

func TestARequestWithATokenNeverRecordsTheSlug(t *testing.T) {
	f := userAuditFixture(t)
	action := gradingAction(nil)
	action.HasToken = true
	action.OrgSlug, action.RepoSlug = "secret-org", "secret-repo"

	f.svc.RecordUserAction(context.Background(), action)

	if len(f.agg.recorded) != 1 {
		t.Fatalf("recorded %d, want 1", len(f.agg.recorded))
	}

	event := f.agg.recorded[0]
	if strings.Contains(strings.ToLower(fmt.Sprint(event)), "secret") {
		t.Errorf("the event holds the slug: %v", event)
	}

	if len(event.GetTarget()) != actorHashLength || strings.Trim(event.GetTarget(), "0123456789abcdef") != "" {
		t.Errorf("target = %q, want a hash", event.GetTarget())
	}

	if event.GetTarget() != f.svc.ActorHash("repo:secret-org/secret-repo") {
		t.Errorf("the hash must be stable, so the same repo gives the same target")
	}

	if !strings.Contains(string(event.GetDetails()), `"access":"token"`) {
		t.Errorf("details = %s", event.GetDetails())
	}
}

func TestUserActionOutcomes(t *testing.T) {
	for name, tc := range map[string]struct {
		err     error
		outcome string
		detail  string
		skipped bool
	}{
		"ready":     {nil, "ok", `"state":"ready"`, false},
		"pending":   {fmt.Errorf("x: %w", domain.ErrPending), "ok", `"state":"pending"`, false},
		"limit":     {domain.GradingLimitError{RetryAfter: time.Minute}, "denied", `"reason":"grading_limit"`, false},
		"forbidden": {domain.ErrForbidden, "denied", `"reason":"no_access"`, false},
		"not found": {domain.ErrNotFound, "error", `"reason":"not_found"`, false},
		"cut short": {domain.ErrGradingInterrupted, "error", `"reason":"interrupted"`, false},
		"other":     {errors.New("boom"), "error", `"reason":"failed"`, false},
		"rate":      {domain.ErrLimitExceeded, "", "", true},
		"bad input": {domain.ErrInvalidRequest, "", "", true},
	} {
		f := userAuditFixture(t)
		f.svc.RecordUserAction(context.Background(), gradingAction(tc.err))

		if tc.skipped {
			if len(f.agg.recorded) != 0 {
				t.Errorf("%s: recorded %v, want nothing", name, f.agg.recorded)
			}

			continue
		}

		if len(f.agg.recorded) != 1 || f.agg.recorded[0].GetOutcome() != tc.outcome || !strings.Contains(string(f.agg.recorded[0].GetDetails()), tc.detail) {
			t.Errorf("%s: recorded %+v, want %s with %s", name, f.agg.recorded, tc.outcome, tc.detail)
		}
	}
}

func TestTheSameActionIsRecordedOncePerWindowButADifferentOneIsNot(t *testing.T) {
	f := userAuditFixture(t)
	ctx := context.Background()

	for range 5 {
		f.svc.RecordUserAction(ctx, gradingAction(nil))
	}

	if len(f.agg.recorded) != 1 {
		t.Fatalf("5 identical polls recorded %d events, want 1", len(f.agg.recorded))
	}

	f.svc.RecordUserAction(ctx, gradingAction(domain.ErrPending)) // another state of the same repo
	other := gradingAction(nil)
	other.RepoSlug = "other"
	f.svc.RecordUserAction(ctx, other)

	recommend := gradingAction(nil)
	recommend.Action = domain.AuditActionRecommendations
	f.svc.RecordUserAction(ctx, recommend)

	if len(f.agg.recorded) != 4 {
		t.Errorf("recorded %d, want 4 (first, pending, other repo, recommendations)", len(f.agg.recorded))
	}
}

func TestUserActionAuditIsOffOrQuietWhenItCannotWork(t *testing.T) {
	ctx := context.Background()

	off := newAdminFixture(t, nil) // SetUserAudit never called
	off.svc.RecordUserAction(ctx, gradingAction(nil))

	if len(off.agg.recorded) != 0 {
		t.Errorf("an unset audit recorded %v", off.agg.recorded)
	}

	broken := newAdminFixture(t, nil)
	broken.svc.SetUserAudit(&onceLimiter{err: errors.New("redis down")})
	broken.svc.RecordUserAction(ctx, gradingAction(nil))

	if len(broken.agg.recorded) != 0 {
		t.Errorf("a failing limiter recorded %v", broken.agg.recorded)
	}

	failing := userAuditFixture(t)
	failing.agg.recErr = errors.New("aggregator down")
	failing.svc.RecordUserAction(ctx, gradingAction(nil)) // must not panic or return anything

	noSecret := NewGatewayService(&auditAggregator{}, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)
	noSecret.SetUserAudit(&onceLimiter{})
	noSecret.RecordUserAction(ctx, gradingAction(nil))
}

func TestOddSlugsAreNotRecorded(t *testing.T) {
	for _, slug := range []string{"", "a b", strings.Repeat("x", 65), "<script>", "a/b", "тест"} {
		f := userAuditFixture(t)
		action := gradingAction(nil)
		action.RepoSlug = slug

		f.svc.RecordUserAction(context.Background(), action)

		if len(f.agg.recorded) != 0 {
			t.Errorf("slug %q was recorded: %v", slug, f.agg.recorded)
		}
	}
}

func TestListAuditPassesTheScopeToTheAggregator(t *testing.T) {
	f := newAdminFixture(t, nil)
	f.agg.list = &agg.ListAuditResponse{}

	if _, err := f.svc.ListAudit(context.Background(), domain.AuditFilter{Scope: "users"}); err != nil {
		t.Fatal(err)
	}

	if f.agg.listIn.GetScope() != "users" {
		t.Errorf("scope sent = %q, want users", f.agg.listIn.GetScope())
	}
}

func TestARefusedRequestNeverRecordsTheSlugEvenWithoutAToken(t *testing.T) {
	f := userAuditFixture(t)
	action := gradingAction(domain.ErrForbidden)
	action.OrgSlug, action.RepoSlug = "secret-org", "secret-repo"

	f.svc.RecordUserAction(context.Background(), action)

	if len(f.agg.recorded) != 1 || strings.Contains(strings.ToLower(fmt.Sprint(f.agg.recorded[0])), "secret") {
		t.Fatalf("recorded %v, want one event without the slug", f.agg.recorded)
	}

	if f.agg.recorded[0].GetTarget() != f.svc.ActorHash("repo:secret-org/secret-repo") || !strings.Contains(string(f.agg.recorded[0].GetDetails()), `"access":"public"`) {
		t.Errorf("event = %+v", f.agg.recorded[0])
	}
}
