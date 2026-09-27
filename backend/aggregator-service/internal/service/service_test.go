package service

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	upsertErr       error
	savedPublic     []domain.RepoGrade
	savedPrivate    []domain.RepoGrade
}

func (f *fakeGrades) UpsertPublicGrade(ctx context.Context, grade domain.RepoGrade) error {
	f.savedPublic = append(f.savedPublic, grade)
	return f.upsertErr
}

func (f *fakeGrades) UpsertPrivateGrade(ctx context.Context, grade domain.RepoGrade) error {
	f.savedPrivate = append(f.savedPrivate, grade)
	return f.upsertErr
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

func (f *fakeWrapper) GetMyRepos(ctx context.Context, in *client.GetMyReposRequest, opts ...grpc.CallOption) (*client.GetMyReposResponse, error) {
	auth := f.record(ctx)
	repos, ok := f.myRepos[auth]
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	return &client.GetMyReposResponse{Repositories: repos}, nil
}

type fakeGradingPublisher struct {
	events []domain.GradingRequestedEvent
}

func (f *fakeGradingPublisher) Publish(ctx context.Context, event domain.GradingRequestedEvent) error {
	f.events = append(f.events, event)
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
	}
	f.svc = NewAggregatorService(f.repos, f.grades, f.llm, f.wrapper, f.grading, f.llmPub, time.Second, "service-pat")
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
	if ev := f.grading.events[0]; ev.Visibility != "public" || ev.Token != "" || ev.OrgSlug != "org" || ev.RepoSlug != "repo" {
		t.Errorf("event = %+v, want public request without token", ev)
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

func TestGetGradingOwnerGetsPrivateGradingWithToken(t *testing.T) {
	f := newFixture("private")

	_, err := f.svc.GetGrading(userCtx("owner-pat"), "org", "repo")
	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}

	if len(f.grading.events) != 1 {
		t.Fatalf("published %d grading requests, want 1", len(f.grading.events))
	}
	if ev := f.grading.events[0]; ev.Visibility != "private" || ev.Token != "owner-pat" {
		t.Errorf("event = %+v, want private request carrying the owner's token", ev)
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
		"unknown visibility": {visibility: "secret"},
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
		Advice:         json.RawMessage(`[{"category_id":"activity","metric_id":"m","text":"Делайте коммиты чаще."},{"category_id":"x","metric_id":"y","text":"  "}]`),
		GradedAt:       gradedAt(testNow),
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
	if !reflect.DeepEqual(got.Advice, []string{"Делайте коммиты чаще."}) {
		t.Errorf("advice = %#v, want the recommendation texts only", got.Advice)
	}
	if got.MissingData == nil {
		t.Errorf("missing_data should be an empty list, not null")
	}

	// The wire form carries null for no-data and strings for advice.
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
	evaluated := testNow.Add(time.Minute)
	f.llm.public[testRepoID] = domain.RepoLLMResponsePublic{SourcecraftUUID: testRepoID, RawResponse: json.RawMessage(`{"summary":"ok"}`), EvaluatedAt: &evaluated}

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

func TestRegradeContinuesAfterFailingRepo(t *testing.T) {
	f := newFixture("public")
	f.wrapper.publicRepos = []*client.Repository{
		{Id: "not-a-uuid", Slug: "broken", Organization: &client.Organization{Slug: "org"}, Visibility: "public"},
		{Id: testRepoID.String(), Slug: "repo", Organization: &client.Organization{Slug: "org"}, Visibility: "public"},
	}

	err := f.svc.regradePublicRepos(context.Background())
	if err == nil {
		t.Errorf("expected the broken repo to be reported")
	}
	if len(f.grading.events) != 1 || f.grading.events[0].RepoSlug != "repo" {
		t.Errorf("events = %+v, want the valid repo still requested", f.grading.events)
	}
	if !reflect.DeepEqual(f.wrapper.authSeen, []string{"Bearer service-pat"}) {
		t.Errorf("auth = %v, want the service token", f.wrapper.authSeen)
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

func TestRegradeLimitTakesTopRated(t *testing.T) {
	f := newFixture("public")
	f.svc.RegradeLimit = 1
	low := uuid.New()
	f.wrapper.publicRepos = []*client.Repository{
		{Id: low.String(), Slug: "low", Organization: &client.Organization{Slug: "org"}, Visibility: "public", Rating: &client.Rating{Value: 1}},
		{Id: testRepoID.String(), Slug: "repo", Organization: &client.Organization{Slug: "org"}, Visibility: "public", Rating: &client.Rating{Value: 9}},
	}

	if err := f.svc.regradePublicRepos(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.grading.events) != 1 || f.grading.events[0].RepoSlug != "repo" {
		t.Errorf("events = %+v, want only the top-rated repo", f.grading.events)
	}
}
