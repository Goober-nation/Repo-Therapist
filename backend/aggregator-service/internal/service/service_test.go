package service

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	testRepoID = uuid.MustParse("019e82c2-2ef4-7bf7-b41e-82ccee32cec4")
	testNow    = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
)

type fakeRepos struct {
	lockFree  bool
	acquired  int
	released  []uuid.UUID
	created   []domain.Repo
	updated   []domain.Repo
	existing  map[uuid.UUID]bool
	createErr error
}

func (f *fakeRepos) CreateRepository(ctx context.Context, repo domain.Repo) (uuid.UUID, error) {
	if f.createErr != nil {
		return uuid.Nil, f.createErr
	}
	f.created = append(f.created, repo)
	f.existing[repo.SourcecraftUUID] = true
	return repo.SourcecraftUUID, nil
}

func (f *fakeRepos) UpdateRepository(ctx context.Context, repo domain.Repo) (uuid.UUID, error) {
	if !f.existing[repo.SourcecraftUUID] {
		return uuid.Nil, domain.ErrRepositoryNotFound
	}
	f.updated = append(f.updated, repo)
	return repo.SourcecraftUUID, nil
}

func (f *fakeRepos) DeleteRepository(ctx context.Context, repoID uuid.UUID) error { return nil }
func (f *fakeRepos) GetRepositoryByID(ctx context.Context, repoID uuid.UUID) (domain.Repo, error) {
	return domain.Repo{}, domain.ErrRepositoryNotFound
}
func (f *fakeRepos) GetRepositoryBySlugs(ctx context.Context, orgSlug, repoSlug string) (domain.Repo, error) {
	return domain.Repo{}, domain.ErrRepositoryNotFound
}
func (f *fakeRepos) GetRepositoryLock(ctx context.Context, id uuid.UUID) (domain.RepoLock, error) {
	return domain.RepoLock{}, nil
}

func (f *fakeRepos) AcquireRepositoryLock(ctx context.Context, id uuid.UUID) (bool, error) {
	if !f.lockFree {
		return false, nil
	}
	f.lockFree = false
	f.acquired++
	return true, nil
}

func (f *fakeRepos) ReleaseRepositoryLock(ctx context.Context, id uuid.UUID) error {
	f.released = append(f.released, id)
	f.lockFree = true
	return nil
}

type fakeGrades struct {
	public, private map[uuid.UUID]domain.RepoGrade
	// repos is the repo metadata TopGradedPublicRepos joins with public.
	repos        []domain.Repo
	upsertErr    error
	savedPublic  []domain.RepoGrade
	savedPrivate []domain.RepoGrade

	confidence       map[uuid.UUID]domain.GradeConfidence
	confidenceDelete []uuid.UUID
	confidenceErr    error

	analyticsPublic, analyticsPrivate map[uuid.UUID]domain.RepoAnalytics
	analyticsDeleted                  []string
	analyticsErr                      error

	history    []domain.RepoGrade
	historyErr error

	facts        map[uuid.UUID]domain.RepoFacts
	factsDeleted []uuid.UUID
	factsErr     error
	hasFactsErr  error
}

func (f *fakeGrades) UpsertPublicFacts(ctx context.Context, repoID uuid.UUID, facts domain.RepoFacts, gradedAt *time.Time) error {
	if f.factsErr != nil {
		return f.factsErr
	}
	if f.facts == nil {
		f.facts = map[uuid.UUID]domain.RepoFacts{}
	}
	f.facts[repoID] = facts
	return nil
}

func (f *fakeGrades) HasPublicFacts(ctx context.Context, repoID uuid.UUID) (bool, error) {
	if f.hasFactsErr != nil {
		return false, f.hasFactsErr
	}
	_, ok := f.facts[repoID]
	return ok, nil
}

func (f *fakeGrades) DeletePublicFacts(ctx context.Context, repoID uuid.UUID) error {
	f.factsDeleted = append(f.factsDeleted, repoID)
	return f.factsErr
}

func (f *fakeGrades) AppendPublicHistory(ctx context.Context, grade domain.RepoGrade) error {
	if f.historyErr != nil {
		return f.historyErr
	}
	f.history = append(f.history, grade)
	return nil
}

func (f *fakeGrades) putAnalytics(public bool, repoID uuid.UUID, analytics json.RawMessage, gradedAt *time.Time) error {
	if f.analyticsErr != nil {
		return f.analyticsErr
	}
	target := &f.analyticsPrivate
	if public {
		target = &f.analyticsPublic
	}
	if *target == nil {
		*target = map[uuid.UUID]domain.RepoAnalytics{}
	}
	(*target)[repoID] = domain.RepoAnalytics{SourcecraftUUID: repoID, Analytics: analytics, GradedAt: gradedAt}
	return nil
}

func (f *fakeGrades) UpsertPublicAnalytics(ctx context.Context, repoID uuid.UUID, analytics json.RawMessage, gradedAt *time.Time) error {
	return f.putAnalytics(true, repoID, analytics, gradedAt)
}

func (f *fakeGrades) UpsertPrivateAnalytics(ctx context.Context, repoID uuid.UUID, analytics json.RawMessage, gradedAt *time.Time) error {
	return f.putAnalytics(false, repoID, analytics, gradedAt)
}

func (f *fakeGrades) DeletePublicAnalytics(ctx context.Context, repoID uuid.UUID) error {
	f.analyticsDeleted = append(f.analyticsDeleted, "public")
	return f.analyticsErr
}

func (f *fakeGrades) DeletePrivateAnalytics(ctx context.Context, repoID uuid.UUID) error {
	f.analyticsDeleted = append(f.analyticsDeleted, "private")
	return f.analyticsErr
}

func (f *fakeGrades) GetPublicAnalytics(ctx context.Context, repoID uuid.UUID) (domain.RepoAnalytics, error) {
	if a, ok := f.analyticsPublic[repoID]; ok {
		return a, nil
	}
	return domain.RepoAnalytics{}, domain.ErrGradeNotFound
}

func (f *fakeGrades) GetPrivateAnalytics(ctx context.Context, repoID uuid.UUID) (domain.RepoAnalytics, error) {
	if a, ok := f.analyticsPrivate[repoID]; ok {
		return a, nil
	}
	return domain.RepoAnalytics{}, domain.ErrGradeNotFound
}

func (f *fakeGrades) UpsertPublicConfidence(ctx context.Context, repoID uuid.UUID, confidence domain.GradeConfidence, gradedAt *time.Time) error {
	if f.confidenceErr != nil {
		return f.confidenceErr
	}
	if f.confidence == nil {
		f.confidence = map[uuid.UUID]domain.GradeConfidence{}
	}
	f.confidence[repoID] = confidence
	return nil
}

func (f *fakeGrades) DeletePublicConfidence(ctx context.Context, repoID uuid.UUID) error {
	f.confidenceDelete = append(f.confidenceDelete, repoID)
	return f.confidenceErr
}

func (f *fakeGrades) UpsertPublicGrade(ctx context.Context, grade domain.RepoGrade) error {
	f.savedPublic = append(f.savedPublic, grade)
	return f.upsertErr
}

func (f *fakeGrades) UpsertPrivateGrade(ctx context.Context, grade domain.RepoGrade) error {
	f.savedPrivate = append(f.savedPrivate, grade)
	return f.upsertErr
}

func (f *fakeGrades) TopGradedPublicRepos(ctx context.Context, limit int) ([]domain.Repo, error) {
	var out []domain.Repo
	for _, r := range f.repos {
		if _, ok := f.public[r.SourcecraftUUID]; ok && r.Visibility == domain.VisibilityPublic {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RatingValue > out[j].RatingValue })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeGrades) GetPublicGradeByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoGrade, error) {
	if g, ok := f.public[repoID]; ok {
		return g, nil
	}
	return domain.RepoGrade{}, domain.ErrGradeNotFound
}

func (f *fakeGrades) GetPrivateGradeByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoGrade, error) {
	if g, ok := f.private[repoID]; ok {
		return g, nil
	}
	return domain.RepoGrade{}, domain.ErrGradeNotFound
}

type fakeLLM struct {
	public, private map[uuid.UUID]domain.RepoLLMResponsePublic
}

func (f *fakeLLM) UpsertPublicResponse(ctx context.Context, response domain.LLMRecommendationsReceivedEvent) error {
	return nil
}
func (f *fakeLLM) UpsertPrivateResponse(ctx context.Context, response domain.LLMRecommendationsReceivedEvent) error {
	return nil
}
func (f *fakeLLM) DeletePublicResponse(ctx context.Context, repoID uuid.UUID) error  { return nil }
func (f *fakeLLM) DeletePrivateResponse(ctx context.Context, repoID uuid.UUID) error { return nil }

func (f *fakeLLM) GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoLLMResponsePublic, error) {
	if r, ok := f.public[repoID]; ok {
		return r, nil
	}
	return domain.RepoLLMResponsePublic{}, domain.ErrLLMResponseNotFound
}

func (f *fakeLLM) GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoLLMResponsePrivate, error) {
	if r, ok := f.private[repoID]; ok {
		return domain.RepoLLMResponsePrivate(r), nil
	}
	return domain.RepoLLMResponsePrivate{}, domain.ErrLLMResponseNotFound
}

// fakeWrapper answers like sourcecraft-wrapper-service. myRepos is keyed by token: a token not in
// the map sees no repos. Every call's outgoing authorization value is recorded.
type fakeWrapper struct {
	repo        *client.GetRepositoryBySlugsResponse
	repoErr     error
	myRepos     map[string][]*client.Repository
	publicRepos []*client.Repository
	authSeen    []string

	myOrgs       *client.GetMyOrgsResponse
	orgRepos     *client.GetOrgReposResponse
	orgReposErr  error
	orgReposSeen []*client.GetOrgReposRequest

	// publicPages are the catalog listing's pages; page i+1's token is "p<i+1>". pageErrAt fails the
	// page with that index (1-based) when set; loopPages makes the last page point back to page 2.
	publicPages [][]*client.Repository
	pageErrAt   int
	loopPages   bool
	pageGate    chan struct{}
}

func (f *fakeWrapper) record(ctx context.Context) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	auth := ""
	if values := md.Get("authorization"); len(values) > 0 {
		auth = values[0]
	}
	f.authSeen = append(f.authSeen, auth)
	return auth
}

func (f *fakeWrapper) GetRepositoryBySlugs(ctx context.Context, in *client.GetRepositoryBySlugsRequest, opts ...grpc.CallOption) (*client.GetRepositoryBySlugsResponse, error) {
	f.record(ctx)
	return f.repo, f.repoErr
}

func (f *fakeWrapper) ListAllPublicRepos(ctx context.Context, in *client.ListAllPublicReposRequest, opts ...grpc.CallOption) (*client.ListAllPublicReposResponse, error) {
	f.record(ctx)
	return &client.ListAllPublicReposResponse{Repos: f.publicRepos}, nil
}

func (f *fakeWrapper) ListPublicReposPage(ctx context.Context, in *client.ListPublicReposPageRequest, opts ...grpc.CallOption) (*client.ListPublicReposPageResponse, error) {
	f.record(ctx)

	if f.pageGate != nil {
		select {
		case <-f.pageGate:
		case <-ctx.Done():
			return nil, status.Error(codes.Canceled, "cancelled")
		}
	}

	page := 1
	if in.GetPageToken() != "" {
		fmt.Sscanf(in.GetPageToken(), "p%d", &page)
	}

	if page == f.pageErrAt {
		return nil, status.Error(codes.Unavailable, "down")
	}

	resp := &client.ListPublicReposPageResponse{}
	if page <= len(f.publicPages) {
		resp.Repositories = f.publicPages[page-1]
	}

	switch {
	case page < len(f.publicPages):
		resp.NextPageToken = fmt.Sprintf("p%d", page+1)
	case f.loopPages:
		resp.NextPageToken = "p2"
	}

	return resp, nil
}

func (f *fakeWrapper) GetMyRepos(ctx context.Context, in *client.GetMyReposRequest, opts ...grpc.CallOption) (*client.GetMyReposResponse, error) {
	auth := f.record(ctx)
	repos, ok := f.myRepos[auth]
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	return &client.GetMyReposResponse{Repositories: repos}, nil
}

func (f *fakeWrapper) GetMyOrgs(ctx context.Context, in *client.GetMyOrgsRequest, opts ...grpc.CallOption) (*client.GetMyOrgsResponse, error) {
	f.record(ctx)
	if f.myOrgs == nil {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	return f.myOrgs, nil
}

func (f *fakeWrapper) GetOrgRepos(ctx context.Context, in *client.GetOrgReposRequest, opts ...grpc.CallOption) (*client.GetOrgReposResponse, error) {
	f.record(ctx)
	f.orgReposSeen = append(f.orgReposSeen, in)
	if f.orgReposErr != nil {
		return nil, f.orgReposErr
	}
	return f.orgRepos, nil
}

type fakeGradingPublisher struct {
	events []domain.GradingRequestedEvent
}

func (f *fakeGradingPublisher) Publish(ctx context.Context, event domain.GradingRequestedEvent) error {
	f.events = append(f.events, event)
	return nil
}

type storedToken struct {
	token string
	ttl   time.Duration
}

type fakeTokenStore struct {
	tokens  map[string]storedToken
	deleted []string
}

func (f *fakeTokenStore) PutToken(ctx context.Context, jobID, token string, ttl time.Duration) error {
	f.tokens[jobID] = storedToken{token: token, ttl: ttl}
	return nil
}

func (f *fakeTokenStore) DeleteToken(ctx context.Context, jobID string) error {
	delete(f.tokens, jobID)
	f.deleted = append(f.deleted, jobID)
	return nil
}

type fakeLLMPublisher struct {
	events []domain.LLMRecommendationsRequestedEvent
}

func (f *fakeLLMPublisher) Publish(ctx context.Context, event domain.LLMRecommendationsRequestedEvent) error {
	f.events = append(f.events, event)
	return nil
}

type fixture struct {
	svc     *AggregatorService
	repos   *fakeRepos
	grades  *fakeGrades
	llm     *fakeLLM
	wrapper *fakeWrapper
	grading *fakeGradingPublisher
	llmPub  *fakeLLMPublisher
	tokens  *fakeTokenStore
}

func newFixture(visibility string) *fixture {
	f := &fixture{
		repos:  &fakeRepos{lockFree: true, existing: map[uuid.UUID]bool{}},
		grades: &fakeGrades{public: map[uuid.UUID]domain.RepoGrade{}, private: map[uuid.UUID]domain.RepoGrade{}},
		llm:    &fakeLLM{public: map[uuid.UUID]domain.RepoLLMResponsePublic{}, private: map[uuid.UUID]domain.RepoLLMResponsePublic{}},
		wrapper: &fakeWrapper{
			repo: &client.GetRepositoryBySlugsResponse{
				Id:             testRepoID.String(),
				Slug:           "repo",
				Organization:   &client.Organization{Slug: "org"},
				Visibility:     visibility,
				LastActivityAt: "2026-09-20T12:00:00Z",
				Language:       &client.Language{Name: "Go", Color: "#00ADD8"},
				Rating:         &client.Rating{Value: 4.5},
				WebUrl:         "https://sourcecraft.dev/org/repo",
			},
			myRepos: map[string][]*client.Repository{
				"Bearer owner-pat": {{Slug: "repo", Organization: &client.Organization{Slug: "org"}}},
				"Bearer other-pat": {},
			},
		},
		grading: &fakeGradingPublisher{},
		llmPub:  &fakeLLMPublisher{},
		tokens:  &fakeTokenStore{tokens: map[string]storedToken{}},
	}
	f.svc = NewAggregatorService(f.repos, f.grades, f.llm, f.wrapper, f.grading, f.llmPub, time.Second, "service-pat")
	f.svc.TokenStore = f.tokens
	f.svc.TokenTTL = 20 * time.Minute
	f.svc.now = func() time.Time { return testNow }
	return f
}

func userCtx(token string) context.Context {
	if token == "" {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func gradedAt(t time.Time) *time.Time { return &t }

func TestGetGradingPublicRepoWithoutGradeRequestsPublicGrading(t *testing.T) {
	f := newFixture("public")

	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}

	// The repo is stored under its real SourceCraft UUID, with the wrapper's metadata.
	if len(f.repos.created) != 1 || f.repos.created[0].SourcecraftUUID != testRepoID {
		t.Fatalf("created = %+v, want one repo with id %s", f.repos.created, testRepoID)
	}
	if got := f.repos.created[0]; got.Language.Name != "Go" || got.RatingValue != 4.5 || got.WebURL == "" {
		t.Errorf("stored metadata = %+v", got)
	}

	if len(f.grading.events) != 1 {
		t.Fatalf("published %d grading requests, want 1", len(f.grading.events))
	}
	if ev := f.grading.events[0]; ev.Visibility != "public" || ev.JobID == "" || ev.OrgSlug != "org" || ev.RepoSlug != "repo" {
		t.Errorf("event = %+v, want public request with a job id", ev)
	}
	if len(f.tokens.tokens) != 0 {
		t.Errorf("public grading stored a token: %d keys", len(f.tokens.tokens))
	}

	// Anonymous lookup goes through the service token; no private-access check is attempted.
	if !reflect.DeepEqual(f.wrapper.authSeen, []string{"Bearer service-pat"}) {
		t.Errorf("wrapper auth = %v, want only the service token lookup", f.wrapper.authSeen)
	}
}

func TestGetGradingSecondCallWhileLockedStaysPending(t *testing.T) {
	f := newFixture("public")

	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")

	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}
	if len(f.grading.events) != 1 {
		t.Errorf("published %d grading requests, want 1 (lock held)", len(f.grading.events))
	}
	// Second call updates the existing row instead of trying to insert it again.
	if len(f.repos.created) != 1 || len(f.repos.updated) != 1 {
		t.Errorf("created=%d updated=%d, want 1 and 1", len(f.repos.created), len(f.repos.updated))
	}
}

func TestGetGradingReturnsCurrentGrade(t *testing.T) {
	f := newFixture("public")
	score := int16(80)
	f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}

	grade, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grade.Score == nil || *grade.Score != 80 {
		t.Errorf("grade = %+v", grade)
	}
	if len(f.grading.events) != 0 {
		t.Errorf("current grade should not trigger grading")
	}
}

func TestGetGradingRegradesWhenAnalyticsMissing(t *testing.T) {
	f := newFixture("public")
	f.svc.RegradeMissingAnalytics = true
	score := int16(80)
	f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}

	// Up-to-date grade, but no analytics row yet: regrade even though the repo is unchanged.
	if _, err := f.svc.GetGrading(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}
	if len(f.grading.events) != 1 {
		t.Fatalf("published %d grading requests, want 1", len(f.grading.events))
	}
}

func TestGetGradingRegradesWhenAnalyticsVersionIsOld(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"version 1", `{"version":1}`, true},
		{"no version", `{"bus_factor":{}}`, true},
		{"not json", `oops`, true},
		{"version 2", `{"version":2}`, true},
		{"version 3", `{"version":3}`, true},
		{"version 4", `{"version":4}`, false},
		{"newer", `{"version":5}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture("public")
			f.svc.RegradeMissingAnalytics = true
			score := int16(80)
			f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}
			_ = f.grades.putAnalytics(true, testRepoID, json.RawMessage(tc.body), gradedAt(testNow))
			f.grades.facts = map[uuid.UUID]domain.RepoFacts{testRepoID: {}}

			_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
			if got := errors.Is(err, domain.GradingPending); got != tc.want {
				t.Errorf("regrade = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}

func TestGetGradingKeepsGradeWhenAnalyticsPresentOrCheckOff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		check   bool
		present bool
	}{{"present", true, true}, {"check off", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture("public")
			f.svc.RegradeMissingAnalytics = tc.check
			score := int16(80)
			f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}
			if tc.present {
				_ = f.grades.putAnalytics(true, testRepoID, json.RawMessage(`{"version":4}`), gradedAt(testNow))
				f.grades.facts = map[uuid.UUID]domain.RepoFacts{testRepoID: {}}
			}

			if _, err := f.svc.GetGrading(userCtx(""), "org", "repo"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(f.grading.events) != 0 {
				t.Errorf("published %d grading requests, want 0", len(f.grading.events))
			}
		})
	}
}

// A public grade stored before the ranking filters existed has no facts; opening it regrades it so
// the filters can see the repository. A facts row whose members are all unknown still counts as present.
func TestGetGradingRegradesAPublicGradeWithoutFacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facts    map[uuid.UUID]domain.RepoFacts
		lookup   error
		wantPend bool
	}{
		{"no facts row", nil, nil, true},
		{"facts row with every member unknown", map[uuid.UUID]domain.RepoFacts{testRepoID: {}}, nil, false},
		{"a failed lookup never starts a grading", nil, errors.New("db down"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture("public")
			f.svc.RegradeMissingAnalytics = true
			score := int16(80)
			f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}
			_ = f.grades.putAnalytics(true, testRepoID, json.RawMessage(`{"version":4}`), gradedAt(testNow))
			f.grades.facts, f.grades.hasFactsErr = tc.facts, tc.lookup

			_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
			if got := errors.Is(err, domain.GradingPending); got != tc.wantPend {
				t.Errorf("regrade = %v (err %v), want %v", got, err, tc.wantPend)
			}
		})
	}

	// The owner's private view has no facts at all and must never regrade because of them.
	f := newFixture("private")
	f.svc.RegradeMissingAnalytics = true
	score := int16(80)
	f.grades.private = map[uuid.UUID]domain.RepoGrade{testRepoID: {SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}}
	_ = f.grades.putAnalytics(false, testRepoID, json.RawMessage(`{"version":4}`), gradedAt(testNow))

	if _, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo"); err != nil {
		t.Errorf("a private grade without facts must not regrade: %v", err)
	}
}

func TestGetGradingOwnerGetsPrivateGradingWithToken(t *testing.T) {
	f := newFixture("private")

	_, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo")
	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}

	if len(f.grading.events) != 1 {
		t.Fatalf("published %d grading requests, want 1", len(f.grading.events))
	}
	ev := f.grading.events[0]
	if ev.Visibility != "private" || ev.JobID == "" {
		t.Errorf("event = %+v, want private request with a job id", ev)
	}
	if got, ok := f.tokens.tokens[ev.JobID]; !ok || got.token != "owner-pat" || got.ttl != 20*time.Minute {
		t.Errorf("token store = %+v, want owner token under the event's job id with the lock TTL", f.tokens.tokens)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "owner-pat") || strings.Contains(string(payload), `"token"`) {
		t.Errorf("grading.requests payload carries the token: %s", payload)
	}

	// The access check used the caller's token, never the service token.
	for _, auth := range f.wrapper.authSeen {
		if auth == "Bearer service-pat" {
			t.Errorf("service token used while the caller had a token: %v", f.wrapper.authSeen)
		}
	}
}

func TestGetGradingPrivateRepoWithoutAccessIsRefused(t *testing.T) {
	for name, token := range map[string]string{"anonymous": "", "other user": "other-pat"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture("private")

			_, err := f.svc.GetGrading(userCtx(token), "org", "repo")
			if !errors.Is(err, domain.ErrNoAccess) {
				t.Fatalf("err = %v, want ErrNoAccess", err)
			}
			if f.repos.acquired != 0 || len(f.grading.events) != 0 {
				t.Errorf("no lock or grading request expected, got acquired=%d events=%d", f.repos.acquired, len(f.grading.events))
			}
		})
	}
}

func TestGetGradingPublicRepoOwnerSeesPrivateGrade(t *testing.T) {
	f := newFixture("public")
	publicScore, privateScore := int16(70), int16(60)
	f.grades.public[testRepoID] = domain.RepoGrade{Score: &publicScore, GradedAt: gradedAt(testNow)}
	f.grades.private[testRepoID] = domain.RepoGrade{Score: &privateScore, GradedAt: gradedAt(testNow)}

	owner, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo")
	if err != nil || *owner.Score != 60 {
		t.Errorf("owner grade = %+v, %v; want private 60", owner, err)
	}

	anon, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if err != nil || *anon.Score != 70 {
		t.Errorf("anonymous grade = %+v, %v; want public 70", anon, err)
	}
}

func TestGetGradingWrapperErrorsMapToDomain(t *testing.T) {
	cases := map[codes.Code]error{
		codes.NotFound:         domain.ErrRepositoryNotFound,
		codes.PermissionDenied: domain.ErrNoAccess,
		codes.Unauthenticated:  domain.ErrUnauthenticated,
		codes.DeadlineExceeded: domain.ErrTimeout,
		codes.Internal:         domain.ErrExternalAPI,
	}
	for code, want := range cases {
		t.Run(code.String(), func(t *testing.T) {
			f := newFixture("public")
			f.wrapper.repoErr = status.Error(code, "x")

			_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
			if !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestSaveGradingResultNormalizesAndReleasesLock(t *testing.T) {
	f := newFixture("public")
	f.repos.lockFree = false

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID}, "public", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(f.grades.savedPublic) != 1 {
		t.Fatalf("saved %d public grades, want 1", len(f.grades.savedPublic))
	}
	saved := f.grades.savedPublic[0]
	if saved.MissingData == nil || len(saved.MissingData) != 0 {
		t.Errorf("missing_data = %#v, want empty non-nil slice (column is NOT NULL)", saved.MissingData)
	}
	if string(saved.CategoryScores) != "{}" || string(saved.Advice) != "[]" {
		t.Errorf("category_scores=%s advice=%s, want {} and []", saved.CategoryScores, saved.Advice)
	}
	if len(f.repos.released) != 1 {
		t.Errorf("lock released %d times, want 1", len(f.repos.released))
	}
}

func TestSaveGradingResultReleasesLockOnEveryFailure(t *testing.T) {
	cases := map[string]struct {
		visibility, code string
		upsertErr        error
	}{
		"grading error":      {visibility: "public", code: "GRADING_FAILED"},
		"save error":         {visibility: "private", upsertErr: errors.New("db down")},
		"unknown grade mode": {visibility: "secret"},
		// internal is a repo visibility, never a grade mode.
		"internal grade mode": {visibility: "internal"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture("public")
			f.grades.upsertErr = tc.upsertErr

			err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID}, tc.visibility, tc.code)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if len(f.repos.released) != 1 || f.repos.released[0] != testRepoID {
				t.Errorf("released = %v, want the repo's lock released once", f.repos.released)
			}
		})
	}
}

func TestLLMGradeFromRepoGrade(t *testing.T) {
	score := int16(64)
	grade := domain.RepoGrade{
		Score:          &score,
		CategoryScores: json.RawMessage(`{"activity":{"id":"activity","no_data":false,"score":90.5,"metrics":[]},"issues":{"id":"issues","no_data":true,"score":0}}`),
		Advice: json.RawMessage(`[{"category_id":"activity","metric_id":"m","priority":"critical","text":"[critical] Делайте коммиты чаще."},` +
			`{"category_id":"x","metric_id":"y","priority":"low","text":"  "},` +
			`{"category_id":"issues","metric_id":"old","text":"[high] Старый формат."},` +
			`{"category_id":"documentation","metric_id":"z","text":"Без приоритета."}]`),
		GradedAt: gradedAt(testNow),
	}

	got, err := llmGradeFromRepoGrade(grade)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.CategoryScores["activity"] == nil || *got.CategoryScores["activity"] != 90.5 {
		t.Errorf("activity = %v, want 90.5", got.CategoryScores["activity"])
	}
	if value, ok := got.CategoryScores["issues"]; !ok || value != nil {
		t.Errorf("issues = %v (present=%v), want explicit null for no-data", value, ok)
	}
	wantAdvice := []domain.LLMAdvice{
		{CategoryID: "activity", Priority: "critical", Text: "Делайте коммиты чаще."},
		{CategoryID: "issues", Priority: "high", Text: "Старый формат."},
		{CategoryID: "documentation", Priority: "medium", Text: "Без приоритета."},
	}
	if !reflect.DeepEqual(got.Advice, wantAdvice) {
		t.Errorf("advice = %#v, want %#v", got.Advice, wantAdvice)
	}
	if got.MissingData == nil {
		t.Errorf("missing_data should be an empty list, not null")
	}

	// The wire form carries null for no-data and {category_id, priority, text} objects for advice.
	raw, _ := json.Marshal(got)
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if wire["category_scores"].(map[string]any)["issues"] != nil {
		t.Errorf("wire issues = %v, want null", wire["category_scores"])
	}
}

func TestRequestLLMRecommendationsReturnsCurrentResponse(t *testing.T) {
	f := newFixture("public")
	f.grades.public[testRepoID] = domain.RepoGrade{GradedAt: gradedAt(testNow)}
	// evaluated_at earlier than graded_at (clock skew) must not matter: only graded_at counts.
	evaluated := testNow.Add(-time.Minute)
	f.llm.public[testRepoID] = domain.RepoLLMResponsePublic{SourcecraftUUID: testRepoID, RawResponse: json.RawMessage(`{"summary":"ok"}`), EvaluatedAt: &evaluated, GradedAt: gradedAt(testNow)}

	resp, err := f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.RawResponse) != `{"summary":"ok"}` {
		t.Errorf("response = %s", resp.RawResponse)
	}
	if len(f.llmPub.events) != 0 {
		t.Errorf("a current response must not trigger a new LLM request")
	}
}

func TestRequestLLMRecommendationsIgnoresResponseForOtherGrade(t *testing.T) {
	cases := map[string]*time.Time{
		// The repo was regraded while the LLM worked on the old grade; the late reply is
		// newer than the grade by evaluated_at but describes the old one.
		"older grade":  gradedAt(testNow.Add(-time.Hour)),
		"no graded_at": nil,
	}
	for name, responseGradedAt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture("public")
			f.grades.public[testRepoID] = domain.RepoGrade{GradedAt: gradedAt(testNow)}
			evaluated := testNow.Add(time.Minute)
			f.llm.public[testRepoID] = domain.RepoLLMResponsePublic{SourcecraftUUID: testRepoID, RawResponse: json.RawMessage(`{"summary":"old"}`), EvaluatedAt: &evaluated, GradedAt: responseGradedAt}

			if _, err := f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
				t.Fatalf("err = %v, want GradingPending", err)
			}
			if len(f.llmPub.events) != 1 {
				t.Fatalf("published %d LLM requests, want 1", len(f.llmPub.events))
			}
			if got := f.llmPub.events[0].Grade.GradedAt; got == nil || !got.Equal(testNow) {
				t.Errorf("request grade.graded_at = %v, want %v", got, testNow)
			}
		})
	}
}

func TestLLMReceivedEventKeepsGradedAt(t *testing.T) {
	var event domain.LLMRecommendationsReceivedEvent
	payload := `{"sourcecraft_uuid":"` + testRepoID.String() + `","raw_response":{},"model_used":"m","evaluated_at":"2026-10-02T10:00:00Z","graded_at":"2026-10-02T09:00:00.123456Z","visibility":"public","error":""}`
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := time.Date(2026, 10, 2, 9, 0, 0, 123456000, time.UTC)
	if event.GradedAt == nil || !event.GradedAt.Equal(want) {
		t.Errorf("graded_at = %v, want %v", event.GradedAt, want)
	}
}

func TestRequestLLMRecommendationsPublishesOnceWhilePending(t *testing.T) {
	f := newFixture("public")
	f.grades.public[testRepoID] = domain.RepoGrade{GradedAt: gradedAt(testNow)}

	for i := 0; i < 3; i++ {
		if _, err := f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
			t.Fatalf("call %d: err = %v, want GradingPending", i, err)
		}
	}

	if len(f.llmPub.events) != 1 {
		t.Fatalf("published %d LLM requests, want 1", len(f.llmPub.events))
	}
	if ev := f.llmPub.events[0]; ev.Visibility != "public" || ev.SourcecraftUUID != testRepoID {
		t.Errorf("event = %+v", ev)
	}

	// After the TTL a new request may be published.
	f.svc.now = func() time.Time { return testNow.Add(f.svc.LLMRequestTTL + time.Second) }
	_, _ = f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	if len(f.llmPub.events) != 2 {
		t.Errorf("published %d LLM requests after TTL, want 2", len(f.llmPub.events))
	}
}

func TestRequestLLMRecommendationsReportsFailureOnceThenRetries(t *testing.T) {
	f := newFixture("public")
	f.grades.public[testRepoID] = domain.RepoGrade{GradedAt: gradedAt(testNow)}

	_, _ = f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	_ = f.svc.SaveLLMRecommendations(context.Background(), domain.LLMRecommendationsReceivedEvent{SourcecraftUUID: testRepoID, Visibility: "public", Error: "NO_DATA"})

	_, err := f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.ErrRecommendationsFailed) {
		t.Fatalf("err = %v, want ErrRecommendationsFailed", err)
	}

	_, err = f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.GradingPending) || len(f.llmPub.events) != 2 {
		t.Errorf("err = %v, events = %d; want a fresh request after the failure was reported", err, len(f.llmPub.events))
	}
}

func TestRequestLLMRecommendationsWithoutGradeRequestsGrading(t *testing.T) {
	f := newFixture("public")

	_, err := f.svc.RequestLLMRecommendations(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}
	if len(f.grading.events) != 1 || len(f.llmPub.events) != 0 {
		t.Errorf("grading=%d llm=%d, want a grading request and no LLM request", len(f.grading.events), len(f.llmPub.events))
	}
}

func TestGetMyReposRequiresUserToken(t *testing.T) {
	f := newFixture("public")

	if _, err := f.svc.GetMyRepos(userCtx("")); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
	if len(f.wrapper.authSeen) != 0 {
		t.Errorf("wrapper must not be called with the service token for my repos")
	}
}

func TestRegradeSkipsWithoutServiceToken(t *testing.T) {
	f := newFixture("public")
	f.svc.Token = ""

	if err := f.svc.regradePublicRepos(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(f.wrapper.authSeen) != 0 {
		t.Errorf("wrapper should not be called without a service token")
	}
}

func TestGetGradingJobIDsAreUniquePerRequest(t *testing.T) {
	f := newFixture("public")
	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	f.repos.lockFree = true
	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	if len(f.grading.events) != 2 || f.grading.events[0].JobID == f.grading.events[1].JobID {
		t.Fatalf("events = %+v, want two requests with distinct job ids", f.grading.events)
	}
}

type failingGradingPublisher struct{}

func (failingGradingPublisher) Publish(ctx context.Context, event domain.GradingRequestedEvent) error {
	return errors.New("kafka down")
}

func TestGetGradingPublishFailureDeletesStoredToken(t *testing.T) {
	f := newFixture("private")
	f.svc.gradingPublisher = failingGradingPublisher{}

	_, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo")
	if !errors.Is(err, domain.ErrGradingFailed) {
		t.Fatalf("err = %v, want ErrGradingFailed", err)
	}
	if len(f.tokens.tokens) != 0 || len(f.tokens.deleted) != 1 {
		t.Errorf("tokens = %+v deleted = %v, want the stored token removed", f.tokens.tokens, f.tokens.deleted)
	}
}

func TestGetGradingPrivateWithoutTokenStoreFails(t *testing.T) {
	f := newFixture("private")
	f.svc.TokenStore = nil

	_, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo")
	if !errors.Is(err, domain.ErrGradingFailed) {
		t.Fatalf("err = %v, want ErrGradingFailed", err)
	}
	if len(f.grading.events) != 0 {
		t.Errorf("published %d requests without a stored token", len(f.grading.events))
	}
	if err != nil && strings.Contains(err.Error(), "owner-pat") {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestSaveGradingResultStoresPublicConfidence(t *testing.T) {
	f := newFixture("public")
	confidence := &domain.GradeConfidence{LowConfidence: true, Reasons: []byte(`[{"code":"LOW_CODE","text":"x"}]`)}

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Confidence: confidence}, "public", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := f.grades.confidence[testRepoID]
	if !ok || !got.LowConfidence || string(got.Reasons) != string(confidence.Reasons) {
		t.Errorf("stored confidence = %+v (found %v)", got, ok)
	}
	if len(f.grades.confidenceDelete) != 0 {
		t.Error("a present flag must not be deleted")
	}
}

func TestSaveGradingResultDropsStaleConfidenceWhenNoneSent(t *testing.T) {
	f := newFixture("public")

	if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID}, "public", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(f.grades.confidenceDelete) != 1 || len(f.grades.confidence) != 0 {
		t.Errorf("deleted=%v stored=%v, want the stale row deleted and nothing stored", f.grades.confidenceDelete, f.grades.confidence)
	}
}

func TestSaveGradingResultKeepsConfidenceOutOfPrivateGrades(t *testing.T) {
	f := newFixture("private")
	confidence := &domain.GradeConfidence{LowConfidence: true}

	if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Confidence: confidence}, "private", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(f.grades.confidence) != 0 || len(f.grades.confidenceDelete) != 0 || len(f.grades.savedPrivate) != 1 {
		t.Errorf("stored=%v deleted=%v private=%d: the flag belongs to public grades only", f.grades.confidence, f.grades.confidenceDelete, len(f.grades.savedPrivate))
	}
}

func TestSaveGradingResultReportsConfidenceFailureAndStillReleasesLock(t *testing.T) {
	f := newFixture("public")
	f.grades.confidenceErr = errors.New("db down")
	f.repos.lockFree = false

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Confidence: &domain.GradeConfidence{}}, "public", "")
	if !errors.Is(err, domain.ErrGradingFailed) {
		t.Fatalf("err = %v, want ErrGradingFailed", err)
	}
	if len(f.grades.savedPublic) != 1 || len(f.repos.released) != 1 {
		t.Errorf("grade saved %d times, lock released %d times: the grade must stay and the lock must be released", len(f.grades.savedPublic), len(f.repos.released))
	}
}

func TestSaveGradingResultStoresAnalyticsPerMode(t *testing.T) {
	analytics := json.RawMessage(`{"version":1}`)
	for _, mode := range []string{"public", "private"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(mode)
			if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Analytics: analytics, GradedAt: gradedAt(testNow)}, mode, ""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			stored, other := f.grades.analyticsPublic, f.grades.analyticsPrivate
			if mode == "private" {
				stored, other = other, stored
			}
			if got := stored[testRepoID]; string(got.Analytics) != `{"version":1}` || got.GradedAt == nil {
				t.Errorf("%s analytics = %+v", mode, got)
			}
			if len(other) != 0 {
				t.Errorf("the %s grade also wrote to the other mode's table: %v", mode, other)
			}
		})
	}
}

func TestSaveGradingResultDropsStaleAnalyticsWhenNoneSent(t *testing.T) {
	for _, none := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("")} {
		f := newFixture("public")
		if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Analytics: none}, "public", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.grades.analyticsDeleted) != 1 || f.grades.analyticsDeleted[0] != "public" || len(f.grades.analyticsPublic) != 0 {
			t.Errorf("analytics %q: deleted=%v stored=%v, want the stale public row deleted", none, f.grades.analyticsDeleted, f.grades.analyticsPublic)
		}
	}
}

// Analytics are advisory: failing to store them must neither fail the grading nor lose the grade.
func TestSaveGradingResultSurvivesAnalyticsFailure(t *testing.T) {
	f := newFixture("public")
	f.grades.analyticsErr = errors.New("db down")
	f.repos.lockFree = false

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Analytics: json.RawMessage(`{"version":1}`)}, "public", "")
	if err != nil {
		t.Fatalf("err = %v, want nil: the grade is saved and analytics are advisory", err)
	}
	if len(f.grades.savedPublic) != 1 || len(f.repos.released) != 1 {
		t.Errorf("grade saved %d times, lock released %d times", len(f.grades.savedPublic), len(f.repos.released))
	}
}

func TestSaveGradingResultAppendsPublicHistoryOnly(t *testing.T) {
	score := int16(77)
	grade := domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, CategoryScores: json.RawMessage(`{"activity":{"score":80}}`), GradedAt: gradedAt(testNow)}

	pub := newFixture("public")
	if err := pub.svc.SaveGradingResult(context.Background(), grade, "public", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pub.grades.history) != 1 || *pub.grades.history[0].Score != 77 || pub.grades.history[0].GradedAt == nil {
		t.Errorf("public history = %+v, want one point with the score and graded_at", pub.grades.history)
	}

	priv := newFixture("private")
	if err := priv.svc.SaveGradingResult(context.Background(), grade, "private", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(priv.grades.history) != 0 {
		t.Errorf("a private grade wrote %d history points, want none", len(priv.grades.history))
	}

	failed := newFixture("public")
	if err := failed.svc.SaveGradingResult(context.Background(), grade, "public", "REPO_FETCH_FAILED"); err == nil {
		t.Fatal("a failed grading should return an error")
	}
	if len(failed.grades.history) != 0 {
		t.Errorf("a failed grading wrote %d history points, want none", len(failed.grades.history))
	}
}

// History is advisory: failing to append must neither fail the grading nor lose the grade.
func TestSaveGradingResultSurvivesHistoryFailure(t *testing.T) {
	f := newFixture("public")
	f.grades.historyErr = errors.New("db down")
	score := int16(50)

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}, "public", "")
	if err != nil {
		t.Fatalf("err = %v, want nil: the grade is saved and the history is advisory", err)
	}
	if len(f.grades.savedPublic) != 1 || len(f.repos.released) != 1 {
		t.Errorf("grade saved %d times, lock released %d times", len(f.grades.savedPublic), len(f.repos.released))
	}
}

func TestSaveGradingResultStoresFactsForPublicGradesOnly(t *testing.T) {
	facts := json.RawMessage(`{"files":14,"loc":2600,"has_ci":true,"has_security_policy":null}`)
	grade := domain.RepoGrade{SourcecraftUUID: testRepoID, Facts: facts, GradedAt: gradedAt(testNow)}

	pub := newFixture("public")
	if err := pub.svc.SaveGradingResult(context.Background(), grade, "public", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := pub.grades.facts[testRepoID]
	if !ok || got.Files == nil || *got.Files != 14 || got.LOC == nil || *got.LOC != 2600 || got.HasCI == nil || !*got.HasCI {
		t.Fatalf("stored facts = %+v, want 14 files, 2600 lines and a CI config", got)
	}
	if got.HasSecurityPolicy != nil {
		t.Errorf("an unknown fact must stay unknown (nil), got %v", *got.HasSecurityPolicy)
	}

	priv := newFixture("private")
	if err := priv.svc.SaveGradingResult(context.Background(), grade, "private", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(priv.grades.facts) != 0 || len(priv.grades.factsDeleted) != 0 {
		t.Errorf("a private grade touched the public facts: %v / %v", priv.grades.facts, priv.grades.factsDeleted)
	}
}

func TestSaveGradingResultDropsStaleFactsWhenNoneOrUnreadableSent(t *testing.T) {
	for _, none := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(""), json.RawMessage(`{"files":"many"}`), json.RawMessage(`oops`)} {
		f := newFixture("public")
		if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Facts: none}, "public", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.grades.factsDeleted) != 1 || len(f.grades.facts) != 0 {
			t.Errorf("facts %q: deleted=%v stored=%v, want the stale row deleted and nothing stored", none, f.grades.factsDeleted, f.grades.facts)
		}
	}
}

// Facts are advisory filters: failing to store them must neither fail the grading nor lose the grade.
func TestSaveGradingResultSurvivesFactsFailure(t *testing.T) {
	f := newFixture("public")
	f.grades.factsErr = errors.New("db down")
	f.repos.lockFree = false

	err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Facts: json.RawMessage(`{"files":1}`)}, "public", "")
	if err != nil {
		t.Fatalf("err = %v, want nil: the grade is saved and the facts are advisory", err)
	}
	if len(f.grades.savedPublic) != 1 || len(f.repos.released) != 1 {
		t.Errorf("grade saved %d times, lock released %d times", len(f.grades.savedPublic), len(f.repos.released))
	}
}

func TestGetAnalyticsPublicViewNeverStartsAGrading(t *testing.T) {
	f := newFixture("public")
	f.grades.analyticsPublic = map[uuid.UUID]domain.RepoAnalytics{testRepoID: {SourcecraftUUID: testRepoID, Analytics: json.RawMessage(`{"version":1}`), GradedAt: gradedAt(testNow)}}

	got, err := f.svc.GetAnalytics(userCtx(""), "org", "repo")
	if err != nil || string(got.Analytics) != `{"version":1}` {
		t.Fatalf("got %+v err %v", got, err)
	}
	if len(f.grading.events) != 0 || !f.repos.lockFree {
		t.Error("reading analytics must never start a grading or take the lock")
	}
}

func TestGetAnalyticsWithoutRowIsEmptyNotAnError(t *testing.T) {
	f := newFixture("public")

	got, err := f.svc.GetAnalytics(userCtx(""), "org", "repo")
	if err != nil || got.Analytics != nil || got.GradedAt != nil {
		t.Errorf("got %+v err %v, want empty analytics and no error", got, err)
	}
}

func TestGetAnalyticsOwnerSeesPrivateOthersSeePublic(t *testing.T) {
	f := newFixture("public")
	f.grades.analyticsPublic = map[uuid.UUID]domain.RepoAnalytics{testRepoID: {Analytics: json.RawMessage(`{"who":"public"}`)}}
	f.grades.analyticsPrivate = map[uuid.UUID]domain.RepoAnalytics{testRepoID: {Analytics: json.RawMessage(`{"who":"private"}`)}}

	owner, err := f.svc.GetAnalytics(userCtx("owner-pat"), "org", "repo")
	if err != nil || string(owner.Analytics) != `{"who":"private"}` {
		t.Errorf("owner got %s err %v, want the private analytics", owner.Analytics, err)
	}

	for _, token := range []string{"", "other-pat"} {
		got, err := f.svc.GetAnalytics(userCtx(token), "org", "repo")
		if err != nil || string(got.Analytics) != `{"who":"public"}` {
			t.Errorf("token %q got %s err %v, want the public analytics", token, got.Analytics, err)
		}
	}
}

func TestGetAnalyticsPrivateRepoWithoutAccessIsRefused(t *testing.T) {
	f := newFixture("private")
	f.grades.analyticsPrivate = map[uuid.UUID]domain.RepoAnalytics{testRepoID: {Analytics: json.RawMessage(`{"secret":true}`)}}

	for _, token := range []string{"", "other-pat"} {
		got, err := f.svc.GetAnalytics(userCtx(token), "org", "repo")
		if !errors.Is(err, domain.ErrNoAccess) || got.Analytics != nil {
			t.Errorf("token %q: got %s err %v, want ErrNoAccess and nothing leaked", token, got.Analytics, err)
		}
	}
}

func TestRegradeStatusOffUntilStarted(t *testing.T) {
	f := newFixture("public")

	if status := f.svc.GetRegradeStatus(context.Background()); status.Enabled || status.NextRunAt != nil || status.Running {
		t.Errorf("status = %+v, want off before the schedule starts", status)
	}

	f.svc.StartPublicGrading(context.Background(), 0, false)
	if f.svc.GetRegradeStatus(context.Background()).Enabled {
		t.Errorf("a zero interval must leave the schedule off")
	}

	f.svc.Token = ""
	f.svc.StartPublicGrading(context.Background(), time.Hour, false)
	if f.svc.GetRegradeStatus(context.Background()).Enabled {
		t.Errorf("no service token must leave the schedule off")
	}
}

func TestRegradeStatusReportsScheduleAndProgress(t *testing.T) {
	f := newFixture("public")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f.svc.StartPublicGrading(ctx, time.Hour, false)

	status := f.svc.GetRegradeStatus(ctx)
	if !status.Enabled || status.Interval != time.Hour || status.NextRunAt == nil || status.Running {
		t.Fatalf("status = %+v, want an idle schedule every hour", status)
	}

	if until := status.NextRunAt.Sub(f.svc.currentTime()); until != time.Hour {
		t.Errorf("next run in %v, want one interval from the clock", until)
	}

	f.svc.runPublicRegrade(ctx)

	status = f.svc.GetRegradeStatus(ctx)
	if status.Running || status.StartedAt == nil || status.LastFinishedAt == nil || status.Total != status.Done {
		t.Errorf("status = %+v, want a finished run with every repo counted", status)
	}
}

// The scheduled run only refreshes existing grades: candidates are the top RegradeLimit graded public
// repos by SourceCraft rating; a higher-rated repo without a grade is never picked.
func TestScheduledRegradeSelectsTopRatedGradedRepos(t *testing.T) {
	f := newFixture("public")
	f.svc.RegradeLimit = 2

	repo := func(slug string, rating float64, graded bool) domain.Repo {
		r := domain.Repo{SourcecraftUUID: uuid.New(), OrgSlug: "org", RepoSlug: slug, Visibility: domain.VisibilityPublic, RatingValue: rating}
		if graded {
			f.grades.public[r.SourcecraftUUID] = domain.RepoGrade{SourcecraftUUID: r.SourcecraftUUID, GradedAt: gradedAt(time.Now())}
		}
		return r
	}
	f.grades.repos = []domain.Repo{repo("low", 1, true), repo("ungraded", 10, false), repo("top", 9, true), repo("mid", 5, true)}

	repos, _, err := f.svc.publicReposToRegrade(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var slugs []string
	for _, r := range repos {
		slugs = append(slugs, r.RepoSlug)
	}
	if strings.Join(slugs, ",") != "top,mid" {
		t.Errorf("candidates = %v, want the top 2 graded repos", slugs)
	}
	if len(f.wrapper.authSeen) != 0 {
		t.Errorf("selection called the wrapper %d times, want none", len(f.wrapper.authSeen))
	}
}

// A graded repo is regraded only after new SourceCraft activity: outdated analytics or facts do not
// count, and an unanswered activity check skips the repo instead of grading it.
func TestScheduledRegradeOnlyOnFreshActivity(t *testing.T) {
	// The fixture's wrapper reports last activity on 2026-09-20 12:00.
	cases := []struct {
		name     string
		gradedAt time.Time
		lookup   error
		graded   bool
	}{
		{"activity since the grade", time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), nil, true},
		{"no activity since the grade", time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), nil, false},
		{"unanswered check", time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), errors.New("down"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture("public")
			// No analytics or facts rows: the old run regraded such a grade even without activity.
			f.svc.RegradeMissingAnalytics = true
			f.wrapper.repoErr = tc.lookup
			f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, GradedAt: gradedAt(tc.gradedAt)}
			f.grades.repos = []domain.Repo{{SourcecraftUUID: testRepoID, OrgSlug: "org", RepoSlug: "repo", Visibility: domain.VisibilityPublic}}

			if err := f.svc.regradePublicRepos(context.Background()); err != nil {
				t.Fatalf("regrade: %v", err)
			}
			if got := len(f.grading.events) == 1; got != tc.graded {
				t.Errorf("graded = %v, want %v", got, tc.graded)
			}
		})
	}
}

func TestScheduledRegradeNeverGradesWithoutGrade(t *testing.T) {
	f := newFixture("public")
	if err := f.svc.regradeRepo(context.Background(), domain.Repo{SourcecraftUUID: testRepoID, OrgSlug: "org", RepoSlug: "repo", Visibility: domain.VisibilityPublic}); err != nil {
		t.Fatal(err)
	}
	if len(f.grading.events) != 0 || len(f.wrapper.authSeen) != 0 {
		t.Errorf("events = %d lookups = %d, want nothing for an ungraded repo", len(f.grading.events), len(f.wrapper.authSeen))
	}
}
