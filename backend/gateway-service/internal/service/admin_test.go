package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	adminYID  = "yandex-admin-1001"
	memberYID = "yandex-user-2002"
)

// auditAggregator records the audit calls and answers ListAudit from a canned response.
type auditAggregator struct {
	fakeAggregator

	recorded []*agg.RecordAuditRequest
	recErr   error
	list     *agg.ListAuditResponse
	listIn   *agg.ListAuditRequest
}

func (a *auditAggregator) RecordAudit(_ context.Context, in *agg.RecordAuditRequest, _ ...grpc.CallOption) (*agg.RecordAuditResponse, error) {
	a.recorded = append(a.recorded, in)
	return &agg.RecordAuditResponse{}, a.recErr
}

func (a *auditAggregator) ListAudit(_ context.Context, in *agg.ListAuditRequest, _ ...grpc.CallOption) (*agg.ListAuditResponse, error) {
	a.listIn = in
	return a.list, nil
}

type fakeSessions struct {
	users     map[string]uuid.UUID
	err       error
	deleted   []string
	deleteErr error
}

func (f *fakeSessions) CreateSession(context.Context, uuid.UUID) (string, error) { return "", nil }

func (f *fakeSessions) DeleteSession(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	if f.deleteErr != nil {
		return f.deleteErr
	}

	if _, ok := f.users[id]; !ok {
		return domain.ErrSessionNotFound
	}

	delete(f.users, id)

	return nil
}

func (f *fakeSessions) GetSession(_ context.Context, id string) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}

	user, ok := f.users[id]
	if !ok {
		return uuid.Nil, domain.ErrSessionNotFound
	}

	return user, nil
}

type fakeUsers struct{ byID map[uuid.UUID]domain.User }

func (f *fakeUsers) CreateUser(context.Context, domain.User) error { return nil }
func (f *fakeUsers) DeleteUserByYID(context.Context, string) error { return nil }
func (f *fakeUsers) GetUserByYID(context.Context, string) (domain.User, error) {
	return domain.User{}, domain.ErrUserNotFound
}

func (f *fakeUsers) GetUserByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	user, ok := f.byID[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}

	return user, nil
}

type adminFixture struct {
	svc      *GatewayService
	agg      *auditAggregator
	sessions *fakeSessions
	adminID  uuid.UUID
}

func newAdminFixture(t *testing.T, denied Limiter) *adminFixture {
	t.Helper()

	admin, member := uuid.New(), uuid.New()
	sessions := &fakeSessions{users: map[string]uuid.UUID{
		"sess-admin":  admin,
		"sess-member": member,
		"sess-ghost":  uuid.New(), // a session whose user no longer exists
	}}
	users := &fakeUsers{byID: map[uuid.UUID]domain.User{
		admin:  {ID: admin, YandexID: adminYID},
		member: {ID: member, YandexID: memberYID},
	}}
	aggregator := &auditAggregator{}

	svc := NewGatewayService(aggregator, nil, nil, allowAll{}, users, sessions, time.Second, time.Second, time.Second)
	svc.SetAdmin([]string{adminYID}, "test-secret", denied)

	return &adminFixture{svc: svc, agg: aggregator, sessions: sessions, adminID: admin}
}

func TestAdminEnabledNeedsIDsAndSecret(t *testing.T) {
	svc := NewGatewayService(nil, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)

	if svc.AdminEnabled() {
		t.Error("a fresh service must have no admin")
	}

	svc.SetAdmin([]string{adminYID}, "", nil)

	if svc.AdminEnabled() {
		t.Error("IDs without a hash secret must keep the routes off")
	}

	svc.SetAdmin(nil, "secret", nil)

	if svc.AdminEnabled() {
		t.Error("a secret without IDs must keep the routes off")
	}

	svc.SetAdmin([]string{adminYID}, "secret", nil)

	if !svc.AdminEnabled() {
		t.Error("IDs and a secret turn the routes on")
	}
}

func TestActorHashIsKeyedStableShortAndHidesTheID(t *testing.T) {
	f := newAdminFixture(t, nil)

	hash := f.svc.ActorHash(adminYID)
	if len(hash) != actorHashLength || strings.Trim(hash, "0123456789abcdef") != "" {
		t.Errorf("hash = %q, want %d lowercase hex characters", hash, actorHashLength)
	}

	if hash != f.svc.ActorHash(adminYID) || hash == f.svc.ActorHash(memberYID) {
		t.Error("the hash must be stable per ID and differ between IDs")
	}

	other := newAdminFixture(t, nil)
	other.svc.SetAdmin([]string{adminYID}, "another-secret", nil)

	if hash == other.svc.ActorHash(adminYID) {
		t.Error("a different secret must give a different hash")
	}

	if strings.Contains(hash, adminYID) {
		t.Error("the hash must not contain the ID")
	}
}

func TestIsAdminNeverAuditsAndAnswersFalseForEveryNonAdmin(t *testing.T) {
	f := newAdminFixture(t, nil)
	ctx := context.Background()

	for name, tc := range map[string]struct {
		session string
		want    bool
	}{
		"admin":        {"sess-admin", true},
		"member":       {"sess-member", false},
		"no cookie":    {"", false},
		"unknown":      {"sess-nope", false},
		"user is gone": {"sess-ghost", false},
	} {
		got, err := f.svc.IsAdmin(ctx, tc.session)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v, want %v", name, got, err, tc.want)
		}
	}

	if len(f.agg.recorded) != 0 {
		t.Errorf("the status probe recorded %d audit events; it runs on every page load", len(f.agg.recorded))
	}

	f.sessions.err = errors.New("redis down")

	if _, err := f.svc.IsAdmin(ctx, "sess-admin"); err == nil {
		t.Error("an infrastructure failure must surface, not read as 'not an admin'")
	}

	off := NewGatewayService(nil, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)
	if got, err := off.IsAdmin(ctx, "sess-admin"); got || err != nil {
		t.Errorf("with admin off: %v, %v", got, err)
	}
}

func TestAuthenticateAdminAcceptsAnAdmin(t *testing.T) {
	f := newAdminFixture(t, nil)

	admin, err := f.svc.AuthenticateAdmin(context.Background(), "1.1.1.1", "sess-admin", "/admin/audit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if admin.UserID != f.adminID || admin.ActorHash != f.svc.ActorHash(adminYID) {
		t.Errorf("identity = %+v", admin)
	}

	if len(f.agg.recorded) != 0 {
		t.Error("a successful authentication must not write a denial")
	}
}

func TestAuthenticateAdminRefusesAndAuditsWithoutLeakingAnything(t *testing.T) {
	cases := map[string]struct {
		session    string
		wantErr    error
		wantKind   string
		wantHash   bool
		wantReason string
	}{
		"no session":      {"", domain.ErrSessionNotFound, domain.AuditActorAnonymous, false, denyNoSession},
		"unknown session": {"sess-nope", domain.ErrSessionNotFound, domain.AuditActorAnonymous, false, denyInvalidSession},
		"user is gone":    {"sess-ghost", domain.ErrSessionNotFound, domain.AuditActorAnonymous, false, denyInvalidSession},
		"not an admin":    {"sess-member", domain.ErrForbidden, domain.AuditActorUser, true, denyNotAdmin},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAdminFixture(t, nil)

			_, err := f.svc.AuthenticateAdmin(context.Background(), "9.9.9.9", tc.session, "/admin/audit")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if len(f.agg.recorded) != 1 {
				t.Fatalf("recorded %d events, want 1", len(f.agg.recorded))
			}

			event := f.agg.recorded[0]
			if event.GetAction() != "admin.denied" || event.GetOutcome() != domain.AuditOutcomeDenied ||
				event.GetActorKind() != tc.wantKind || event.GetTargetKind() != domain.AuditTargetRoute || event.GetTarget() != "/admin/audit" {
				t.Errorf("event = %v", event)
			}

			if !strings.Contains(string(event.GetDetails()), tc.wantReason) {
				t.Errorf("details = %s, want reason %s", event.GetDetails(), tc.wantReason)
			}

			if tc.wantHash && event.GetActorHash() != newAdminFixture(t, nil).svc.ActorHash(memberYID) {
				t.Errorf("actor hash = %q", event.GetActorHash())
			}

			if !tc.wantHash && event.GetActorHash() != "" {
				t.Errorf("an actor without a valid session has no hash, got %q", event.GetActorHash())
			}

			dump := event.String()
			for _, secret := range []string{adminYID, memberYID, "9.9.9.9", "sess-member", "sess-admin", "test-secret"} {
				if strings.Contains(dump, secret) {
					t.Errorf("the audit event leaks %q: %s", secret, dump)
				}
			}
		})
	}
}

func TestAuthenticateAdminOffRefusesWithoutAuditing(t *testing.T) {
	aggregator := &auditAggregator{}
	svc := NewGatewayService(aggregator, nil, nil, nil, nil, nil, time.Second, time.Second, time.Second)

	if _, err := svc.AuthenticateAdmin(context.Background(), "ip", "any", "/admin/audit"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}

	if len(aggregator.recorded) != 0 {
		t.Error("with the admin routes off nothing is recorded")
	}
}

func TestAuthenticateAdminInfrastructureFailureIsNotADenial(t *testing.T) {
	f := newAdminFixture(t, nil)
	f.sessions.err = errors.New("redis down")

	_, err := f.svc.AuthenticateAdmin(context.Background(), "ip", "sess-admin", "/admin/audit")
	if err == nil || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("err = %v, want a plain infrastructure error", err)
	}

	if len(f.agg.recorded) != 0 {
		t.Error("an outage is not a refused attempt")
	}
}

func TestDeniedAttemptsAreRateLimitedAndAuditFailureKeepsTheAnswer(t *testing.T) {
	limiter := &countingLimiter{allow: false}
	f := newAdminFixture(t, limiter)

	if _, err := f.svc.AuthenticateAdmin(context.Background(), "5.5.5.5", "sess-member", "/admin/audit"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}

	if len(f.agg.recorded) != 0 {
		t.Error("a rate-limited denial must not be recorded")
	}

	if len(limiter.keys) != 1 || limiter.keys[0] != "rate_limit:gateway:admin_denied:ip:5.5.5.5" {
		t.Errorf("limiter keys = %v", limiter.keys)
	}

	failing := newAdminFixture(t, nil)
	failing.agg.recErr = errors.New("aggregator down")

	if _, err := failing.svc.AuthenticateAdmin(context.Background(), "ip", "sess-member", "/admin/audit"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a failing audit write must not change the 403, got %v", err)
	}
}

func TestListAuditForwardsFilterAndMapsEntries(t *testing.T) {
	f := newAdminFixture(t, nil)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	f.agg.list = &agg.ListAuditResponse{
		NextBeforeId: 41,
		Entries: []*agg.AuditEntry{{
			Id: 42, OccurredAt: timestamppb.New(at), ActorKind: "user", ActorHash: "abcd", Action: "admin.denied",
			TargetKind: "route", Target: "/admin/audit", Outcome: "denied", Details: []byte(`{"reason":"not_admin"}`),
		}},
	}

	page, err := f.svc.ListAudit(context.Background(), domain.AuditFilter{Limit: 25, BeforeID: 100, Action: "admin.", Outcome: "denied"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.agg.listIn.GetLimit() != 25 || f.agg.listIn.GetBeforeId() != 100 || f.agg.listIn.GetAction() != "admin." || f.agg.listIn.GetOutcome() != "denied" {
		t.Errorf("request = %v", f.agg.listIn)
	}

	if page.NextBeforeID != 41 || len(page.Entries) != 1 {
		t.Fatalf("page = %+v", page)
	}

	got := page.Entries[0]
	if got.ID != 42 || !got.OccurredAt.Equal(at) || got.ActorHash != "abcd" || got.Action != "admin.denied" || string(got.Details) != `{"reason":"not_admin"}` {
		t.Errorf("entry = %+v", got)
	}
}
