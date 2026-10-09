package service

import (
	"context"
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

// batchWrapper answers the wrapper call batches make: the repo lookup of the activity check. Unset
// methods panic through the nil embedded client.
type batchWrapper struct {
	client.CodeHealthClient

	mu       sync.Mutex
	activity map[string]time.Time // slug -> last_updated
	failSlug string
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

	batch, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{Limit: 10, Force: true})
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

func TestCatalogBatchSavesNewReposBeforeStarting(t *testing.T) {
	w, _, _ := newBatchWorld(t)
	saved := &savingRepos{worldRepos: worldRepos{w}}
	w.svc.ReposRepo = saved

	fresh := catalogRepoN(1, 5, "Go")
	w.svc.CatalogRepo = newMemCatalog(fresh)

	if _, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{Limit: 1}); err != nil {
		t.Fatal(err)
	}

	saved.mu.Lock()
	defer saved.mu.Unlock()

	if len(saved.created) != 1 || saved.created[0].SourcecraftUUID != fresh.SourcecraftUUID {
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
