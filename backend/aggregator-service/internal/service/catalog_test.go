package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"

	"github.com/google/uuid"
)

// memCatalog keeps the catalog, its staging rows and refreshes in memory, with the database's rules:
// one running refresh, and a commit replaces the catalog only for a refresh still running.
type memCatalog struct {
	mu        sync.Mutex
	catalog   []domain.Repo
	staging   []domain.Repo
	refreshes []domain.CatalogRefresh
	done      chan int64
}

func newMemCatalog(repos ...domain.Repo) *memCatalog {
	return &memCatalog{catalog: repos, done: make(chan int64, 8)}
}

func (m *memCatalog) find(id int64) *domain.CatalogRefresh {
	for i := range m.refreshes {
		if m.refreshes[i].ID == id {
			return &m.refreshes[i]
		}
	}

	return nil
}

func (m *memCatalog) StartRefresh(_ context.Context, trigger, actorHash string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range m.refreshes {
		if r.Status == domain.CatalogRefreshRunning {
			return 0, domain.ErrCatalogRefreshActive
		}
	}

	id := int64(len(m.refreshes) + 1)
	m.refreshes = append(m.refreshes, domain.CatalogRefresh{ID: id, Status: domain.CatalogRefreshRunning, Trigger: trigger, ActorHash: actorHash})
	m.staging = nil

	return id, nil
}

func (m *memCatalog) StagePage(_ context.Context, id int64, repos []domain.Repo, pages, total int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.staging = append(m.staging, repos...)
	r := m.find(id)
	r.Pages, r.Repos = pages, total

	return nil
}

func (m *memCatalog) CommitRefresh(_ context.Context, id int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.find(id)
	if r.Status != domain.CatalogRefreshRunning {
		return 0, domain.ErrCatalogRefreshNotRunning
	}

	seen := map[uuid.UUID]bool{}
	m.catalog = nil

	for _, repo := range m.staging {
		if !seen[repo.SourcecraftUUID] {
			seen[repo.SourcecraftUUID] = true
			m.catalog = append(m.catalog, repo)
		}
	}

	m.staging = nil
	r.Status, r.Repos = domain.CatalogRefreshCompleted, len(m.catalog)
	m.done <- id

	return len(m.catalog), nil
}

func (m *memCatalog) FinishRefresh(_ context.Context, id int64, status, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.find(id)
	if r == nil || r.Status != domain.CatalogRefreshRunning {
		return domain.ErrCatalogRefreshNotRunning
	}

	r.Status, r.Error = status, code
	if status == domain.CatalogRefreshFailed {
		m.done <- id
	}

	return nil
}

func (m *memCatalog) FailOrphanedRefreshes(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.refreshes {
		if m.refreshes[i].Status == domain.CatalogRefreshRunning {
			m.refreshes[i].Status, m.refreshes[i].Error = domain.CatalogRefreshFailed, domain.CatalogErrorInterrupted
		}
	}

	return nil
}

func (m *memCatalog) GetRefresh(_ context.Context, id int64) (domain.CatalogRefresh, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return *m.find(id), nil
}

func (m *memCatalog) Status(context.Context) (domain.CatalogStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := domain.CatalogStatus{Size: len(m.catalog)}
	if n := len(m.refreshes); n > 0 {
		current := m.refreshes[n-1]
		status.Current = &current
	}

	return status, nil
}

func (m *memCatalog) Size(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.catalog), nil
}

func (m *memCatalog) TopRated(_ context.Context, limit int) ([]domain.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repos := append([]domain.Repo(nil), m.catalog...)
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].RatingValue > repos[j].RatingValue })

	if len(repos) > limit {
		repos = repos[:limit]
	}

	return repos, nil
}

// Select honours the language criterion, rating order and the limit; grades are not modelled, so
// OnlyRegrade matches nothing.
func (m *memCatalog) Select(_ context.Context, filter domain.CatalogFilter, limit int) ([]domain.CatalogEntry, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repos := append([]domain.Repo(nil), m.catalog...)
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].RatingValue > repos[j].RatingValue })

	var out []domain.CatalogEntry

	for _, repo := range repos {
		if filter.OnlyRegrade {
			continue
		}

		if len(filter.Languages) > 0 && !containsString(filter.Languages, strings.ToLower(repo.Language.Name)) {
			continue
		}

		out = append(out, domain.CatalogEntry{Repo: repo})
	}

	matched := len(out)
	if len(out) > limit {
		out = out[:limit]
	}

	return out, matched, nil
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}

	return false
}

func (m *memCatalog) refresh(id int64) domain.CatalogRefresh {
	m.mu.Lock()
	defer m.mu.Unlock()

	return *m.find(id)
}

func (m *memCatalog) slugs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []string
	for _, repo := range m.catalog {
		out = append(out, repo.OrgSlug+"/"+repo.RepoSlug)
	}

	return out
}

func wrapperRepoN(n int, visibility string, rating float64) *client.Repository {
	return &client.Repository{
		Id:           uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprint(n))).String(),
		Slug:         fmt.Sprintf("repo%d", n),
		Organization: &client.Organization{Slug: "org"},
		Visibility:   visibility,
		Rating:       &client.Rating{Value: rating},
		Language:     &client.Language{Name: "Go"},
	}
}

func catalogRepoN(n int, rating float64, language string) domain.Repo {
	return domain.Repo{
		SourcecraftUUID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprint(n))),
		OrgSlug:         "org", RepoSlug: fmt.Sprintf("repo%d", n), Visibility: domain.VisibilityPublic,
		RatingValue: rating, Language: domain.PrimaryLanguage{Name: language},
	}
}

func catalogFixture(stored ...domain.Repo) (*fixture, *memCatalog) {
	f := newFixture("public")
	catalog := newMemCatalog(stored...)
	f.svc.CatalogRepo = catalog

	return f, catalog
}

func waitRefresh(t *testing.T, catalog *memCatalog) int64 {
	t.Helper()

	select {
	case id := <-catalog.done:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh did not finish")

		return 0
	}
}

func TestCatalogRefreshPagesThroughEveryPage(t *testing.T) {
	f, catalog := catalogFixture()
	f.wrapper.publicPages = [][]*client.Repository{
		{wrapperRepoN(1, "public", 1), wrapperRepoN(2, "private", 1)},
		{wrapperRepoN(3, "public", 1), {Id: "not-a-uuid", Slug: "bad", Organization: &client.Organization{Slug: "org"}, Visibility: "public"}},
		{wrapperRepoN(3, "public", 1), wrapperRepoN(4, "public", 1)},
	}

	started, err := f.svc.RefreshCatalog(context.Background(), "abc")
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	if started.Status != domain.CatalogRefreshRunning || started.Trigger != domain.CatalogTriggerAdmin || started.ActorHash != "abc" {
		t.Errorf("started = %+v, want a running admin refresh", started)
	}

	id := waitRefresh(t, catalog)
	got := catalog.refresh(id)

	if got.Status != domain.CatalogRefreshCompleted || got.Pages != 3 || got.Repos != 3 {
		t.Errorf("refresh = %+v, want completed after 3 pages with 3 public repos", got)
	}
	if slugs := catalog.slugs(); strings.Join(slugs, ",") != "org/repo1,org/repo3,org/repo4" {
		t.Errorf("catalog = %v, want the public repos once each", slugs)
	}
	for _, auth := range f.wrapper.authSeen {
		if auth != "Bearer service-pat" {
			t.Errorf("auth = %q, want the service token", auth)
		}
	}
}

func TestCatalogRefreshFailureKeepsTheCatalog(t *testing.T) {
	old := catalogRepoN(99, 1, "Go")

	cases := []struct {
		name  string
		setup func(*fakeWrapper, *AggregatorService)
		code  string
	}{
		{"wrapper error", func(w *fakeWrapper, _ *AggregatorService) {
			w.publicPages = [][]*client.Repository{{wrapperRepoN(1, "public", 1)}, {wrapperRepoN(2, "public", 1)}}
			w.pageErrAt = 2
		}, domain.CatalogErrorWrapper},
		{"page loop", func(w *fakeWrapper, _ *AggregatorService) {
			w.publicPages = [][]*client.Repository{{wrapperRepoN(1, "public", 1)}, {wrapperRepoN(2, "public", 1)}, {wrapperRepoN(3, "public", 1)}}
			w.loopPages = true
		}, domain.CatalogErrorPageLoop},
		{"too many pages", func(w *fakeWrapper, as *AggregatorService) {
			w.publicPages = [][]*client.Repository{{wrapperRepoN(1, "public", 1)}, {wrapperRepoN(2, "public", 1)}, {wrapperRepoN(3, "public", 1)}}
			as.CatalogMaxPages = 2
		}, domain.CatalogErrorTooManyPages},
		{"empty listing", func(w *fakeWrapper, _ *AggregatorService) {
			w.publicPages = [][]*client.Repository{{wrapperRepoN(1, "private", 1)}}
		}, domain.CatalogErrorEmpty},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, catalog := catalogFixture(old)
			tc.setup(f.wrapper, f.svc)

			if _, err := f.svc.RefreshCatalog(context.Background(), ""); err != nil {
				t.Fatalf("RefreshCatalog: %v", err)
			}

			got := catalog.refresh(waitRefresh(t, catalog))
			if got.Status != domain.CatalogRefreshFailed || got.Error != tc.code {
				t.Errorf("refresh = %+v, want failed with %s", got, tc.code)
			}
			if slugs := catalog.slugs(); len(slugs) != 1 || slugs[0] != "org/repo99" {
				t.Errorf("catalog = %v, want the old catalog untouched", slugs)
			}
		})
	}
}

func TestCatalogRefreshOneAtATimeAndCancel(t *testing.T) {
	f, catalog := catalogFixture(catalogRepoN(99, 1, "Go"))
	f.wrapper.publicPages = [][]*client.Repository{{wrapperRepoN(1, "public", 1)}}
	f.wrapper.pageGate = make(chan struct{}) // holds the first page until the refresh is cancelled

	started, err := f.svc.RefreshCatalog(context.Background(), "")
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}

	if _, err := f.svc.RefreshCatalog(context.Background(), ""); !errors.Is(err, domain.ErrCatalogRefreshActive) {
		t.Errorf("second refresh err = %v, want ErrCatalogRefreshActive", err)
	}

	cancelled, err := f.svc.CancelCatalogRefresh(context.Background(), "")
	if err != nil {
		t.Fatalf("CancelCatalogRefresh: %v", err)
	}
	if cancelled.ID != started.ID || cancelled.Status != domain.CatalogRefreshCancelled {
		t.Errorf("cancelled = %+v, want refresh %d cancelled", cancelled, started.ID)
	}

	// The worker lets go of the runner once the page call returns.
	deadline := time.Now().Add(5 * time.Second)
	for f.svc.catalogBusy() {
		if time.Now().After(deadline) {
			t.Fatal("the cancelled refresh did not stop")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if slugs := catalog.slugs(); len(slugs) != 1 || slugs[0] != "org/repo99" {
		t.Errorf("catalog = %v, want the old catalog untouched", slugs)
	}
	if _, err := f.svc.CancelCatalogRefresh(context.Background(), ""); !errors.Is(err, domain.ErrCatalogRefreshNotRunning) {
		t.Errorf("cancel with nothing running err = %v, want ErrCatalogRefreshNotRunning", err)
	}
}

func TestStartCatalogRefreshesOnlyAnEmptyCatalog(t *testing.T) {
	f, catalog := catalogFixture(catalogRepoN(99, 1, "Go"))
	f.svc.StartCatalog(context.Background())
	if len(catalog.refreshes) != 0 {
		t.Errorf("refreshes = %+v, want none for a filled catalog", catalog.refreshes)
	}

	f, catalog = catalogFixture()
	f.wrapper.publicPages = [][]*client.Repository{{wrapperRepoN(1, "public", 1)}}
	f.svc.StartCatalog(context.Background())

	got := catalog.refresh(waitRefresh(t, catalog))
	if got.Trigger != domain.CatalogTriggerStartup || got.Status != domain.CatalogRefreshCompleted {
		t.Errorf("refresh = %+v, want a completed startup refresh", got)
	}
}

func TestBatchSelectionReadsTheCatalog(t *testing.T) {
	f, _ := catalogFixture(catalogRepoN(1, 1, "Go"), catalogRepoN(2, 5, "Python"), catalogRepoN(3, 3, "Go"), catalogRepoN(4, 4, "Go"))
	f.svc.BatchRepo = newMemBatches()

	_, sel, err := f.svc.selectForBatch(context.Background(), domain.BatchFilter{Limit: 2, Languages: []string{"Go"}})
	if err != nil {
		t.Fatalf("selectForBatch: %v", err)
	}

	var slugs []string
	for _, repo := range sel.Repos {
		slugs = append(slugs, repo.RepoSlug)
	}

	if strings.Join(slugs, ",") != "repo4,repo3" || sel.Matched != 3 || len(sel.Fetched) != 2 {
		t.Errorf("selection = %v of %d, fetched=%d, want the top 2 of 3 Go repos by rating", slugs, sel.Matched, len(sel.Fetched))
	}

	_, sel, err = f.svc.selectForBatch(context.Background(), domain.BatchFilter{Limit: 100})
	if err != nil || len(sel.Repos) != 4 || sel.Matched != 4 {
		t.Errorf("no criteria: %d of %d, %v, want every catalog repo", len(sel.Repos), sel.Matched, err)
	}
	if len(f.wrapper.authSeen) != 0 {
		t.Errorf("wrapper calls = %d, want none while the catalog is filled", len(f.wrapper.authSeen))
	}
}

// One catalog repo that cannot be saved is left out of the batch instead of refusing it.
func TestBatchLeavesOutUnsaveableRepo(t *testing.T) {
	bad := catalogRepoN(2, 5, "Go")
	f, _ := catalogFixture(catalogRepoN(1, 1, "Go"), bad, catalogRepoN(3, 3, "Go"))
	f.svc.BatchRepo = newMemBatches()
	f.repos.createErrFor = map[uuid.UUID]error{bad.SourcecraftUUID: errors.New("broken row")}

	batch, err := f.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{Limit: 3})
	if err != nil {
		t.Fatalf("StartBatch: %v", err)
	}
	if batch.Total != 2 {
		t.Errorf("total = %d, want the 2 repos that could be saved", batch.Total)
	}
	_, _ = f.svc.ControlBatch(context.Background(), "abc", batch.ID, domain.BatchActionCancel)
}
