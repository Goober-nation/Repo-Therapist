package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// batchWrapper answers the two wrapper calls batches make: the repo lookup of the activity check
// and the rating-ordered listing. Unset methods panic through the nil embedded client.
type batchWrapper struct {
	client.CodeHealthClient

	mu       sync.Mutex
	activity map[string]time.Time // slug -> last_updated
	failSlug string
	pages    [][]*client.Repository
	lookups  []string
}

func (b *batchWrapper) GetRepositoryBySlugs(_ context.Context, in *client.GetRepositoryBySlugsRequest, _ ...grpc.CallOption) (*client.GetRepositoryBySlugsResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	slug := in.GetOrgSlug() + "/" + in.GetRepoSlug()
	b.lookups = append(b.lookups, slug)

	if slug == b.failSlug {
		return nil, status.Error(codes.Unavailable, "down")
	}

	last, ok := b.activity[slug]
	if !ok { // repos the test says nothing about are active
		last = time.Now()
	}

	return &client.GetRepositoryBySlugsResponse{
		Id: uuid.NewString(), Slug: in.GetRepoSlug(), Organization: &client.Organization{Slug: in.GetOrgSlug()},
		Visibility: domain.VisibilityPublic, LastActivityAt: last.Format(time.RFC3339Nano),
	}, nil
}

func (b *batchWrapper) ListTopRatedRepos(_ context.Context, in *client.ListTopRatedReposRequest, _ ...grpc.CallOption) (*client.ListTopRatedReposResponse, error) {
	page := 0
	if in.GetPageToken() != "" {
		fmt.Sscan(in.GetPageToken(), &page)
	}

	if page >= len(b.pages) {
		return &client.ListTopRatedReposResponse{}, nil
	}

	resp := &client.ListTopRatedReposResponse{Repositories: b.pages[page]}
	if page+1 < len(b.pages) {
		resp.NextPageToken = fmt.Sprint(page + 1)
	}

	return resp, nil
}

func (w *batchWorld) idOf(slug string) uuid.UUID {
	w.mu.Lock()
	defer w.mu.Unlock()

	for id, repo := range w.repos {
		if repo.OrgSlug+"/"+repo.RepoSlug == slug {
			return id
		}
	}

	return uuid.Nil
}

func (w *batchWorld) setGrade(slug string, at time.Time) {
	id := w.idOf(slug)
	score := int16(70)

	w.mu.Lock()
	w.grades[id] = domain.RepoGrade{SourcecraftUUID: id, Score: &score, GradedAt: &at}
	w.mu.Unlock()
}

func TestBatchSkipsReposWithoutNewActivity(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/quiet", "org/busy", "org/new", "org/flaky")
	gradedAt := time.Now().Add(-48 * time.Hour)

	for _, slug := range []string{"org/quiet", "org/busy", "org/flaky"} {
		w.setGrade(slug, gradedAt)
	}

	wrapper := &batchWrapper{activity: map[string]time.Time{
		"org/quiet": gradedAt.Add(-time.Hour), // last activity before the grade
		"org/busy":  gradedAt.Add(time.Hour),
	}, failSlug: "org/flaky"}
	w.svc.clientRPC = wrapper

	batch, err := w.svc.StartBatch(context.Background(), "abc", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	done := batchStatus(t, w.svc, batch.ID)
	if done.OK != 3 || done.Skipped != 1 {
		t.Fatalf("batch = %+v, want 3 graded (busy, never graded, failed check) and 1 skipped", done)
	}

	w.mu.Lock()
	requested := strings.Join(w.requested, ",")
	w.mu.Unlock()

	if strings.Contains(requested, "org/quiet") {
		t.Fatalf("quiet repo was graded: %s", requested)
	}

	items, err := w.svc.BatchRepo.ListBatchItems(context.Background(), batch.ID, domain.BatchItemSkipped, 10)
	if err != nil || len(items) != 1 || items[0].Error != domain.BatchSkipNoActivity {
		t.Fatalf("skipped items = %+v, %v", items, err)
	}

	// A repo never graded needs no lookup at all.
	for _, slug := range wrapper.lookups {
		if slug == "org/new" {
			t.Fatal("activity check looked up a repo that was never graded")
		}
	}
}

func TestForcedBatchRegradesWithoutActivityCheck(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/quiet")
	gradedAt := time.Now().Add(-time.Hour)
	w.setGrade("org/quiet", gradedAt)

	wrapper := &batchWrapper{activity: map[string]time.Time{"org/quiet": gradedAt.Add(-time.Hour)}}
	w.svc.clientRPC = wrapper

	batch, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{All: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	if done := batchStatus(t, w.svc, batch.ID); done.OK != 1 || done.Skipped != 0 {
		t.Fatalf("batch = %+v", done)
	}

	if len(wrapper.lookups) != 0 {
		t.Fatalf("forced batch checked activity: %v", wrapper.lookups)
	}
}

func pbRepo(id uuid.UUID, slug, visibility, language string, lastActivity time.Time) *client.Repository {
	org, name, _ := strings.Cut(slug, "/")

	return &client.Repository{
		Id: id.String(), Slug: name, Organization: &client.Organization{Slug: org}, Visibility: visibility,
		Language: &client.Language{Name: language}, LastActivityAt: lastActivity.Format(time.RFC3339Nano),
	}
}

func TestTopRatedSelectionKeepsRatingOrderAndCombinesCriteria(t *testing.T) {
	w, batches, _ := newBatchWorld(t)
	now := time.Now()

	ids := map[string]uuid.UUID{}
	for _, slug := range []string{"a/go1", "b/private", "c/py", "d/go-low", "e/go-new", "f/go2", "g/go3"} {
		ids[slug] = uuid.New()
	}

	scores := map[uuid.UUID]int{ids["a/go1"]: 90, ids["d/go-low"]: 20, ids["f/go2"]: 75, ids["g/go3"]: 80}
	gradedAt := now.Add(-24 * time.Hour)
	batches.gradeInfo = func(id uuid.UUID) (domain.BatchGrade, bool) {
		score, ok := scores[id]

		return domain.BatchGrade{Score: &score, GradedAt: &gradedAt}, ok
	}

	w.svc.clientRPC = &batchWrapper{pages: [][]*client.Repository{
		{
			pbRepo(ids["a/go1"], "a/go1", "public", "Go", now),          // active since grade
			pbRepo(ids["b/private"], "b/private", "private", "Go", now), // not public
			pbRepo(ids["c/py"], "c/py", "public", "Python", now),        // other language
			pbRepo(ids["d/go-low"], "d/go-low", "public", "Go", now),    // score too low
		},
		{
			pbRepo(ids["e/go-new"], "e/go-new", "public", "go", now),                // never graded: no score
			pbRepo(ids["f/go2"], "f/go2", "public", "Go", gradedAt.Add(-time.Hour)), // inactive
			pbRepo(ids["g/go3"], "g/go3", "public", "Go", now),                      // beyond top 2
		},
	}}

	top, minScore := 2, 50
	filter := domain.BatchFilter{TopRated: &top, Languages: []string{"Go"}, MinScore: &minScore}

	preview, err := w.svc.PreviewBatch(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(preview.Sample, ",") != "a/go1,f/go2" || preview.Inactive != 1 || preview.Capped {
		t.Fatalf("preview = %+v", preview)
	}

	filter.Force = true

	forced, err := w.svc.PreviewBatch(context.Background(), filter)
	if err != nil || forced.Inactive != 0 {
		t.Fatalf("forced preview = %+v, %v", forced, err)
	}

	// Without a score bound the never-graded repo qualifies in its rating position.
	noScore := domain.BatchFilter{TopRated: &top, Languages: []string{"go"}}

	preview, err = w.svc.PreviewBatch(context.Background(), noScore)
	if err != nil || strings.Join(preview.Sample, ",") != "a/go1,d/go-low" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
}

func TestTopRatedBatchSavesNewReposBeforeStarting(t *testing.T) {
	w, _, _ := newBatchWorld(t)
	saved := &savingRepos{worldRepos: worldRepos{w}}
	w.svc.ReposRepo = saved

	id := uuid.New()
	w.svc.clientRPC = &batchWrapper{pages: [][]*client.Repository{{pbRepo(id, "new/repo", "public", "Go", time.Now())}}}

	top := 1

	if _, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{TopRated: &top}); err != nil {
		t.Fatal(err)
	}

	saved.mu.Lock()
	defer saved.mu.Unlock()

	if len(saved.created) != 1 || saved.created[0].SourcecraftUUID != id {
		t.Fatalf("created = %+v", saved.created)
	}
}

type savingRepos struct {
	worldRepos
	mu      sync.Mutex
	created []domain.Repo
}

func (s *savingRepos) UpdateRepository(context.Context, domain.Repo) (uuid.UUID, error) {
	return uuid.Nil, domain.ErrRepositoryNotFound
}

func (s *savingRepos) CreateRepository(_ context.Context, repo domain.Repo) (uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.created = append(s.created, repo)

	return repo.SourcecraftUUID, nil
}

func TestTopRatedValidation(t *testing.T) {
	zero, tooMany, ok := 0, domain.MaxBatchTopRated+1, 10

	for name, filter := range map[string]domain.BatchFilter{
		"zero":                        {TopRated: &zero},
		"too many":                    {TopRated: &tooMany},
		"with all":                    {TopRated: &ok, All: true},
		"force alone is no criterion": {Force: true},
	} {
		if err := filter.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if err := (domain.BatchFilter{TopRated: &ok, Force: true}).Validate(); err != nil {
		t.Fatal(err)
	}
}
