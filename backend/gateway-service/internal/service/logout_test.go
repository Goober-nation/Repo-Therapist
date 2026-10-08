package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

func TestLogoutEndsTheSessionAtOnce(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	before, _ := f.svc.AdminStatus(ctx, "sess-member")
	if !before.SignedIn {
		t.Fatal("sanity: the fixture session is valid")
	}

	if err := f.svc.Logout(ctx, "ip", "sess-member"); err != nil {
		t.Fatalf("logout: %v", err)
	}

	after, err := f.svc.AdminStatus(ctx, "sess-member")
	if err != nil || after.SignedIn || after.IsAdmin {
		t.Errorf("after logout the cookie must be dead: %+v, %v", after, err)
	}

	if _, err := f.svc.AuthenticateAdmin(ctx, "ip", "sess-member", "/admin/audit"); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("admin routes after logout: err = %v, want ErrSessionNotFound", err)
	}
}

func TestLogoutTakesThePasswordUnlockAndAuditsIt(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.UnlockAdmin(ctx, "ip", "sess-member", unlockPassword); err != nil {
		t.Fatal(err)
	}

	f.agg.recorded = nil

	if err := f.svc.Logout(ctx, "9.9.9.9", "sess-member"); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if len(f.grants.byID) != 0 {
		t.Error("the unlock must not outlive the session it was given to")
	}

	if len(f.agg.recorded) != 1 {
		t.Fatalf("recorded %d events, want the dropped unlock", len(f.agg.recorded))
	}

	event := f.agg.recorded[0]
	if event.GetAction() != "admin.lock" || event.GetOutcome() != domain.AuditOutcomeOK || event.GetActorKind() != domain.AuditActorAdmin ||
		event.GetActorHash() != f.svc.ActorHash(memberYID) || !strings.Contains(string(event.GetDetails()), "logout") {
		t.Errorf("event = %v", event)
	}

	dump := f.auditDump()
	for _, secret := range []string{unlockPassword, memberYID, "9.9.9.9", "sess-member"} {
		if strings.Contains(dump, secret) {
			t.Errorf("the audit log leaks %q: %s", secret, dump)
		}
	}
}

func TestLogoutWithoutAnUnlockWritesNothingToTheLog(t *testing.T) {
	f := newUnlockFixture(t)

	if err := f.svc.Logout(context.Background(), "ip", "sess-member"); err != nil {
		t.Fatal(err)
	}

	if len(f.agg.recorded) != 0 {
		t.Errorf("a plain logout is not an admin event, recorded %d", len(f.agg.recorded))
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.Logout(ctx, "ip", ""); err != nil {
		t.Errorf("no cookie: %v", err)
	}

	if err := f.svc.Logout(ctx, "ip", "sess-nope"); err != nil {
		t.Errorf("an unknown session: %v", err)
	}

	if err := f.svc.Logout(ctx, "ip", "sess-member"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Logout(ctx, "ip", "sess-member"); err != nil {
		t.Errorf("a second logout of the same session must still succeed, got %v", err)
	}

	if len(f.sessions.deleted) == 0 || f.sessions.deleted[0] != "sess-nope" {
		t.Errorf("deleted = %v: an empty cookie must not reach the store", f.sessions.deleted)
	}
}

func TestLogoutEndsAListedAdminToo(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-admin"); !isAdmin {
		t.Fatal("sanity: the listed admin is an admin")
	}

	if err := f.svc.Logout(ctx, "ip", "sess-admin"); err != nil {
		t.Fatal(err)
	}

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-admin"); isAdmin {
		t.Error("after logout even a listed admin must be signed out")
	}
}

func TestLogoutSurfacesInfrastructureFailures(t *testing.T) {
	f := newUnlockFixture(t)
	f.sessions.deleteErr = errors.New("redis down")

	if err := f.svc.Logout(context.Background(), "ip", "sess-member"); err == nil || errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("a failed delete must not look like success, got %v", err)
	}

	g := newUnlockFixture(t)
	g.grants.err = errors.New("redis down")

	if err := g.svc.Logout(context.Background(), "ip", "sess-member"); err == nil {
		t.Error("a failed unlock revoke must not look like success")
	}

	if len(g.sessions.deleted) != 0 {
		t.Error("the session must not be deleted while the unlock could not be dropped: a half logout would leave the unlock behind")
	}
}

func TestLogoutIsRateLimitedPerIP(t *testing.T) {
	f := newUnlockFixture(t)
	limiter := &countingLimiter{allow: false}
	f.svc.rateLimiter = limiter

	if err := f.svc.Logout(context.Background(), "5.5.5.5", "sess-member"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}

	if len(f.sessions.deleted) != 0 {
		t.Error("a limited request must not touch the session")
	}
}

func TestSignedInIsAnsweredWhateverTheAdminSettingsAre(t *testing.T) {
	f := newAdminFixture(t, nil)
	// A gateway with no admins and no password at all, but with the user and session stores.
	plain := NewGatewayService(f.agg, nil, nil, allowAll{}, f.svc.userRepository, f.sessions, time.Second, time.Second, time.Second)

	status, err := plain.AdminStatus(context.Background(), "sess-member")
	if err != nil || !status.SignedIn || status.IsAdmin || status.UnlockAvailable {
		t.Errorf("status = %+v, %v: a valid session is signed in even with the admin features off", status, err)
	}

	visitor, _ := plain.AdminStatus(context.Background(), "sess-nope")
	if visitor.SignedIn {
		t.Errorf("an unknown session must not read as signed in: %+v", visitor)
	}
}
