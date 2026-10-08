package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

const unlockPassword = "correct horse battery staple"

type fakeGrants struct {
	byID map[string]string
	ttl  time.Duration
	err  error
}

func (f *fakeGrants) Grant(_ context.Context, sessionID, fingerprint string, ttl time.Duration) error {
	if f.err != nil {
		return f.err
	}

	f.byID[sessionID], f.ttl = fingerprint, ttl

	return nil
}

func (f *fakeGrants) Fingerprint(_ context.Context, sessionID string) (string, error) {
	return f.byID[sessionID], f.err
}

func (f *fakeGrants) Revoke(_ context.Context, sessionID string) (bool, error) {
	_, had := f.byID[sessionID]
	delete(f.byID, sessionID)

	return had, f.err
}

// fakeGuard locks a key for lockFor after max failures, like the Redis guard.
type fakeGuard struct {
	max      int
	lockFor  time.Duration
	failures map[string]int
	locked   map[string]bool
	resets   [][]string
	checks   int
}

func newFakeGuard(max int, lockFor time.Duration) *fakeGuard {
	return &fakeGuard{max: max, lockFor: lockFor, failures: map[string]int{}, locked: map[string]bool{}}
}

func (g *fakeGuard) LockedFor(_ context.Context, keys ...string) (time.Duration, error) {
	g.checks++

	for _, key := range keys {
		if g.locked[key] {
			return g.lockFor, nil
		}
	}

	return 0, nil
}

func (g *fakeGuard) RecordFailure(_ context.Context, keys ...string) (bool, error) {
	locked := false

	for _, key := range keys {
		g.failures[key]++

		if g.failures[key] >= g.max {
			g.locked[key], locked = true, true
			delete(g.failures, key)
		}
	}

	return locked, nil
}

func (g *fakeGuard) Reset(_ context.Context, keys ...string) error {
	g.resets = append(g.resets, keys)

	for _, key := range keys {
		delete(g.failures, key)
	}

	return nil
}

type unlockFixture struct {
	*adminFixture
	grants *fakeGrants
	guard  *fakeGuard
}

func newUnlockFixture(t *testing.T) *unlockFixture {
	t.Helper()

	f := &unlockFixture{
		adminFixture: newAdminFixture(t, nil),
		grants:       &fakeGrants{byID: map[string]string{}},
		guard:        newFakeGuard(5, 15*time.Minute),
	}
	f.svc.SetAdminUnlock(unlockPassword, f.grants, f.guard, 24*time.Hour)

	return f
}

func (f *unlockFixture) auditDump() string {
	var all strings.Builder
	for _, event := range f.agg.recorded {
		all.WriteString(event.String())
	}

	return all.String()
}

func TestUnlockAvailabilityNeedsPasswordStoresAndSecret(t *testing.T) {
	f := newAdminFixture(t, nil)

	if f.svc.UnlockAvailable() {
		t.Error("no password configured: the unlock must be off")
	}

	grants, guard := &fakeGrants{byID: map[string]string{}}, newFakeGuard(5, time.Minute)

	f.svc.SetAdminUnlock("", grants, guard, time.Hour)

	if f.svc.UnlockAvailable() {
		t.Error("an empty password must keep the unlock off")
	}

	f.svc.SetAdminUnlock("pw", nil, guard, time.Hour)

	if f.svc.UnlockAvailable() {
		t.Error("without a grant store the unlock must be off")
	}

	f.svc.SetAdminUnlock("pw", grants, guard, time.Hour)

	if !f.svc.UnlockAvailable() {
		t.Error("password and stores must turn it on")
	}

	bare := NewGatewayService(nil, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)
	bare.SetAdminUnlock("pw", grants, guard, time.Hour)

	if bare.UnlockAvailable() || bare.AdminEnabled() {
		t.Error("without the hash secret nothing may be on, even with a password")
	}
}

func TestAdminEnabledByPasswordAlone(t *testing.T) {
	svc := NewGatewayService(nil, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)
	svc.SetAdmin(nil, "secret", nil)
	svc.SetAdminUnlock("pw", &fakeGrants{byID: map[string]string{}}, newFakeGuard(5, time.Minute), time.Hour)

	if !svc.AdminEnabled() {
		t.Error("a password with the hash secret is a way in even with an empty list")
	}
}

func TestCorrectPasswordMakesTheSignedInAccountAnAdmin(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	before, _ := f.svc.AdminStatus(ctx, "sess-member")
	if !before.SignedIn || before.IsAdmin || !before.UnlockAvailable || before.ViaPassword {
		t.Fatalf("before = %+v", before)
	}

	if err := f.svc.UnlockAdmin(ctx, "7.7.7.7", "sess-member", unlockPassword); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	after, _ := f.svc.AdminStatus(ctx, "sess-member")
	if !after.IsAdmin || !after.ViaPassword || !after.SignedIn {
		t.Errorf("after = %+v", after)
	}

	if f.grants.ttl != 24*time.Hour {
		t.Errorf("grant ttl = %v, want the session lifetime", f.grants.ttl)
	}

	identity, err := f.svc.AuthenticateAdmin(ctx, "7.7.7.7", "sess-member", "/admin/audit")
	if err != nil || identity.ActorHash != f.svc.ActorHash(memberYID) {
		t.Errorf("admin routes after unlock: %+v, %v", identity, err)
	}

	if len(f.guard.resets) != 1 {
		t.Errorf("a correct password must reset the failure counters, resets = %v", f.guard.resets)
	}
}

func TestUnlockIsPerSessionNotPerAccountOrPerUser(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.UnlockAdmin(ctx, "ip", "sess-member", unlockPassword); err != nil {
		t.Fatal(err)
	}

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-ghost"); isAdmin {
		t.Error("another session must not inherit the grant")
	}

	if _, err := f.svc.AuthenticateAdmin(ctx, "ip", "sess-ghost", "/admin/audit"); err == nil {
		t.Error("another session must still be refused")
	}

	delete(f.sessions.users, "sess-member") // the session ends

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-member"); isAdmin {
		t.Error("when the session ends the rights end with it")
	}
}

func TestChangingThePasswordRevokesEveryGrant(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.UnlockAdmin(ctx, "ip", "sess-member", unlockPassword); err != nil {
		t.Fatal(err)
	}

	f.svc.SetAdminUnlock("a different password", f.grants, f.guard, time.Hour)

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-member"); isAdmin {
		t.Error("a grant given for the old password must stop counting")
	}

	if _, err := f.svc.AuthenticateAdmin(ctx, "ip", "sess-member", "/admin/audit"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestWrongPasswordIsRefusedCountedAndAuditedWithoutLeaks(t *testing.T) {
	f := newUnlockFixture(t)

	err := f.svc.UnlockAdmin(context.Background(), "9.9.9.9", "sess-member", "guess-123")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}

	if len(f.grants.byID) != 0 {
		t.Error("a wrong password must not grant anything")
	}

	if f.guard.failures["ip:9.9.9.9"] != 1 || len(f.guard.failures) != 2 {
		t.Errorf("failures = %v, want one for the IP and one for the account", f.guard.failures)
	}

	if len(f.agg.recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(f.agg.recorded))
	}

	event := f.agg.recorded[0]
	if event.GetAction() != "admin.unlock" || event.GetOutcome() != domain.AuditOutcomeDenied ||
		event.GetActorKind() != domain.AuditActorUser || event.GetActorHash() != f.svc.ActorHash(memberYID) ||
		!strings.Contains(string(event.GetDetails()), "wrong_password") {
		t.Errorf("event = %v", event)
	}

	dump := f.auditDump()
	for _, secret := range []string{"guess-123", unlockPassword, memberYID, "9.9.9.9", "sess-member", "test-secret"} {
		if strings.Contains(dump, secret) {
			t.Errorf("the audit log leaks %q: %s", secret, dump)
		}
	}
}

func TestSuccessfulUnlockIsAuditedAsAnAdminWithoutLeaks(t *testing.T) {
	f := newUnlockFixture(t)

	if err := f.svc.UnlockAdmin(context.Background(), "9.9.9.9", "sess-member", unlockPassword); err != nil {
		t.Fatal(err)
	}

	if len(f.agg.recorded) != 1 {
		t.Fatalf("recorded %d events", len(f.agg.recorded))
	}

	event := f.agg.recorded[0]
	if event.GetAction() != "admin.unlock" || event.GetOutcome() != domain.AuditOutcomeOK || event.GetActorKind() != domain.AuditActorAdmin ||
		event.GetActorHash() != f.svc.ActorHash(memberYID) || !strings.Contains(string(event.GetDetails()), "password") {
		t.Errorf("event = %v", event)
	}

	dump := f.auditDump()
	for _, secret := range []string{unlockPassword, memberYID, "9.9.9.9", "sess-member", "test-secret"} {
		if strings.Contains(dump, secret) {
			t.Errorf("the audit log leaks %q: %s", secret, dump)
		}
	}

	for _, fingerprint := range f.grants.byID {
		if strings.Contains(fingerprint, unlockPassword) {
			t.Error("the stored grant must not contain the password")
		}
	}
}

func TestFiveWrongPasswordsLockEvenTheRightOne(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := f.svc.UnlockAdmin(ctx, "3.3.3.3", "sess-member", "wrong"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("try %d: err = %v", i+1, err)
		}
	}

	err := f.svc.UnlockAdmin(ctx, "3.3.3.3", "sess-member", unlockPassword)

	var locked domain.AdminLockedError
	if !errors.As(err, &locked) || locked.RetryAfter != 15*time.Minute || !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("err = %v, want a 15 minute lock", err)
	}

	if len(f.grants.byID) != 0 {
		t.Error("the right password must not work while locked: the lock hides whether it was right")
	}

	// The account is locked on another IP too, and the IP is locked for another account.
	if err := f.svc.UnlockAdmin(ctx, "4.4.4.4", "sess-member", unlockPassword); !errors.As(err, &locked) {
		t.Errorf("same account from another IP: err = %v, want locked", err)
	}

	if err := f.svc.UnlockAdmin(ctx, "3.3.3.3", "sess-admin", unlockPassword); !errors.As(err, &locked) {
		t.Errorf("another account from the same IP: err = %v, want locked", err)
	}

	if err := f.svc.UnlockAdmin(ctx, "5.5.5.5", "sess-admin", unlockPassword); err != nil {
		t.Errorf("an unrelated IP and account must be unaffected: %v", err)
	}
}

func TestLockedAttemptsAreAuditedWithinTheDeniedBudget(t *testing.T) {
	limiter := &countingLimiter{allow: true}
	f := newUnlockFixture(t)
	f.svc.SetAdmin([]string{adminYID}, "test-secret", limiter)

	f.guard.locked["ip:1.1.1.1"] = true

	if err := f.svc.UnlockAdmin(context.Background(), "1.1.1.1", "sess-member", unlockPassword); err == nil {
		t.Fatal("want a lock error")
	}

	if len(f.agg.recorded) != 1 || !strings.Contains(string(f.agg.recorded[0].GetDetails()), "locked") {
		t.Fatalf("recorded = %v", f.agg.recorded)
	}

	if len(limiter.keys) != 1 {
		t.Errorf("limiter keys = %v: locked attempts can repeat without limit, so they use the denied budget", limiter.keys)
	}

	limiter.allow = false
	before := len(f.agg.recorded)
	_ = f.svc.UnlockAdmin(context.Background(), "1.1.1.1", "sess-member", unlockPassword)

	if len(f.agg.recorded) != before {
		t.Error("over the budget a locked attempt must not be recorded")
	}
}

func TestUnlockNeedsASignedInSession(t *testing.T) {
	for name, session := range map[string]string{"no cookie": "", "unknown session": "sess-nope", "user is gone": "sess-ghost"} {
		f := newUnlockFixture(t)

		err := f.svc.UnlockAdmin(context.Background(), "ip", session, unlockPassword)
		if !errors.Is(err, domain.ErrSessionNotFound) {
			t.Errorf("%s: err = %v, want ErrSessionNotFound", name, err)
		}

		if len(f.grants.byID) != 0 || f.guard.checks != 0 {
			t.Errorf("%s: nothing may be granted or counted without a session", name)
		}

		if len(f.agg.recorded) != 1 || f.agg.recorded[0].GetActorKind() != domain.AuditActorAnonymous || f.agg.recorded[0].GetActorHash() != "" {
			t.Errorf("%s: recorded = %v, want one anonymous denial", name, f.agg.recorded)
		}
	}

	if _, ok := newUnlockFixture(t).sessions.users["guest"]; ok {
		t.Error("sanity: the fixture has no guest session")
	}
}

func TestNoGuestAccountIsEverCreated(t *testing.T) {
	f := newUnlockFixture(t)
	users := f.svc.userRepository.(*fakeUsers)
	before := len(users.byID)

	_ = f.svc.UnlockAdmin(context.Background(), "ip", "", unlockPassword)
	_ = f.svc.UnlockAdmin(context.Background(), "ip", "sess-nope", unlockPassword)

	if len(users.byID) != before {
		t.Error("an unlock without a session must never create an account")
	}
}

func TestUnlockRejectsEmptyAndOversizedPasswordsWithoutCounting(t *testing.T) {
	f := newUnlockFixture(t)

	for name, password := range map[string]string{"empty": "", "huge": strings.Repeat("a", MaxAdminPasswordLength+1)} {
		err := f.svc.UnlockAdmin(context.Background(), "ip", "sess-member", password)
		if !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}

	if len(f.guard.failures) != 0 {
		t.Errorf("a malformed request is not a wrong guess: failures = %v", f.guard.failures)
	}
}

func TestUnlockOffRefusesWithoutTouchingAnything(t *testing.T) {
	f := newAdminFixture(t, nil)

	if err := f.svc.UnlockAdmin(context.Background(), "ip", "sess-member", unlockPassword); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}

	if len(f.agg.recorded) != 0 {
		t.Error("with the unlock off nothing is recorded")
	}
}

func TestStoreFailureIsNotAnAdmin(t *testing.T) {
	f := newUnlockFixture(t)
	f.grants.err = errors.New("redis down")

	if err := f.svc.UnlockAdmin(context.Background(), "ip", "sess-member", unlockPassword); err == nil || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("err = %v, want a plain infrastructure error", err)
	}

	if len(f.agg.recorded) != 0 {
		t.Error("a failed grant must not be audited as a successful unlock")
	}

	if _, err := f.svc.AuthenticateAdmin(context.Background(), "ip", "sess-member", "/admin/audit"); err == nil || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a store outage must surface as an error, not as 'not an admin': %v", err)
	}
}

func TestLockTakesTheGrantOffAndIsAudited(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.UnlockAdmin(ctx, "ip", "sess-member", unlockPassword); err != nil {
		t.Fatal(err)
	}

	f.agg.recorded = nil

	if err := f.svc.LockAdmin(ctx, "sess-member"); err != nil {
		t.Fatal(err)
	}

	if isAdmin, _ := f.svc.IsAdmin(ctx, "sess-member"); isAdmin {
		t.Error("after lock the account must no longer be an admin")
	}

	if len(f.agg.recorded) != 1 || f.agg.recorded[0].GetAction() != "admin.lock" || f.agg.recorded[0].GetOutcome() != domain.AuditOutcomeOK {
		t.Errorf("recorded = %v", f.agg.recorded)
	}

	f.agg.recorded = nil
	if err := f.svc.LockAdmin(ctx, "sess-member"); err != nil || len(f.agg.recorded) != 0 {
		t.Errorf("locking an already-locked session is a quiet no-op: %v, %d events", err, len(f.agg.recorded))
	}

	if err := f.svc.LockAdmin(ctx, ""); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("lock without a session: err = %v, want ErrSessionNotFound", err)
	}
}

func TestListedAdminStaysAdminAfterLock(t *testing.T) {
	f := newUnlockFixture(t)
	ctx := context.Background()

	if err := f.svc.UnlockAdmin(ctx, "ip", "sess-admin", unlockPassword); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.LockAdmin(ctx, "sess-admin"); err != nil {
		t.Fatal(err)
	}

	status, _ := f.svc.AdminStatus(ctx, "sess-admin")
	if !status.IsAdmin || status.ViaPassword {
		t.Errorf("status = %+v, want an admin by the list, not by password", status)
	}
}

func TestStatusForVisitorsAndWhenOff(t *testing.T) {
	f := newUnlockFixture(t)

	visitor, err := f.svc.AdminStatus(context.Background(), "")
	if err != nil || visitor.SignedIn || visitor.IsAdmin || !visitor.UnlockAvailable {
		t.Errorf("visitor = %+v, %v", visitor, err)
	}

	off := NewGatewayService(nil, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)

	status, err := off.AdminStatus(context.Background(), "sess-member")
	if err != nil || status != (domain.AdminStatus{}) {
		t.Errorf("with everything off: %+v, %v", status, err)
	}
}
