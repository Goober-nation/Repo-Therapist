package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
)

// DefaultCatalogMaxPages bounds one catalog refresh at 100 repos a page (200 000 repos); a listing
// that is still going past it fails the refresh rather than storing a cut list.
const DefaultCatalogMaxPages = 2000

// catalogRunner is the process-wide state of the catalog refresh: at most one runs at a time (the
// database enforces it too), and cancel stops the running one.
type catalogRunner struct {
	mu      sync.Mutex
	running bool
	id      int64
	cancel  context.CancelFunc
}

func (as *AggregatorService) catalogMaxPages() int {
	if pages := as.liveCatalogMaxPages(); pages > 0 {
		return pages
	}

	return DefaultCatalogMaxPages
}

func (as *AggregatorService) requireCatalog() error {
	if as.CatalogRepo == nil {
		return errors.New("the public repo catalog is not configured")
	}

	return nil
}

// catalogInUse reports whether the stored catalog should replace live SourceCraft listings: it is
// configured and holds at least one repo. Before the first successful refresh the old live paths run.
func (as *AggregatorService) catalogInUse(ctx context.Context) bool {
	if as.CatalogRepo == nil {
		return false
	}

	size, err := as.CatalogRepo.Size(ctx)
	if err != nil {
		log.Printf("public repo catalog size: %v", err)

		return false
	}

	return size > 0
}

// StartCatalog prepares the catalog at startup: refreshes left running by a previous process are
// failed, and an empty catalog gets its first refresh.
func (as *AggregatorService) StartCatalog(ctx context.Context) {
	if as.CatalogRepo == nil {
		return
	}

	as.catalogCtx = ctx

	if err := as.CatalogRepo.FailOrphanedRefreshes(ctx); err != nil {
		log.Printf("public repo catalog: %v", err)
	}

	size, err := as.CatalogRepo.Size(ctx)
	if err != nil {
		log.Printf("public repo catalog: %v", err)

		return
	}

	if size > 0 {
		log.Printf("public repo catalog holds %d repositories", size)

		return
	}

	if _, err := as.startCatalogRefresh(ctx, domain.CatalogTriggerStartup, ""); err != nil {
		log.Printf("public repo catalog: first refresh: %v", err)
	}
}

// RefreshCatalog starts a refresh of the public repo catalog on an admin's request.
// ErrCatalogRefreshActive while one is running. The catalog in use stays until the new one is complete.
func (as *AggregatorService) RefreshCatalog(ctx context.Context, actorHash string) (domain.CatalogRefresh, error) {
	if err := as.requireCatalog(); err != nil {
		return domain.CatalogRefresh{}, err
	}

	return as.startCatalogRefresh(ctx, domain.CatalogTriggerAdmin, actorHash)
}

func (as *AggregatorService) startCatalogRefresh(ctx context.Context, trigger, actorHash string) (domain.CatalogRefresh, error) {
	if as.Token == "" {
		return domain.CatalogRefresh{}, fmt.Errorf("%w: SERVICE_TOKEN is not set", domain.ErrUnauthenticated)
	}

	as.catalog.mu.Lock()
	defer as.catalog.mu.Unlock()

	if as.catalog.running {
		return domain.CatalogRefresh{}, domain.ErrCatalogRefreshActive
	}

	id, err := as.CatalogRepo.StartRefresh(ctx, trigger, actorHash)
	if err != nil {
		return domain.CatalogRefresh{}, err
	}

	base := as.catalogCtx
	if base == nil {
		base = context.Background()
	}

	runCtx, cancel := context.WithCancel(base)
	as.catalog.running, as.catalog.id, as.catalog.cancel = true, id, cancel

	if trigger == domain.CatalogTriggerAdmin {
		as.auditCatalog(ctx, actorHash, domain.AuditActorAdmin, "catalog.refresh.start", domain.AuditOutcomeOK, id, nil)
	}

	go as.runCatalogRefresh(runCtx, id)

	return as.CatalogRepo.GetRefresh(ctx, id)
}

func (as *AggregatorService) catalogBusy() bool {
	as.catalog.mu.Lock()
	defer as.catalog.mu.Unlock()

	return as.catalog.running
}

// CancelCatalogRefresh stops the running refresh; the catalog in use is untouched.
// ErrCatalogRefreshNotRunning when none runs.
func (as *AggregatorService) CancelCatalogRefresh(ctx context.Context, actorHash string) (domain.CatalogRefresh, error) {
	if err := as.requireCatalog(); err != nil {
		return domain.CatalogRefresh{}, err
	}

	as.catalog.mu.Lock()
	running, id, cancel := as.catalog.running, as.catalog.id, as.catalog.cancel
	as.catalog.mu.Unlock()

	if !running {
		return domain.CatalogRefresh{}, domain.ErrCatalogRefreshNotRunning
	}

	// Marked first, so a commit racing the cancel finds the refresh no longer running.
	if err := as.CatalogRepo.FinishRefresh(ctx, id, domain.CatalogRefreshCancelled, ""); err != nil {
		return domain.CatalogRefresh{}, err
	}

	cancel()

	as.auditCatalog(ctx, actorHash, domain.AuditActorAdmin, "catalog.refresh.cancel", domain.AuditOutcomeOK, id, nil)

	return as.CatalogRepo.GetRefresh(ctx, id)
}

// CatalogStatus is what the admin panel shows about the catalog.
func (as *AggregatorService) CatalogStatus(ctx context.Context) (domain.CatalogStatus, error) {
	if err := as.requireCatalog(); err != nil {
		return domain.CatalogStatus{}, err
	}

	return as.CatalogRepo.Status(ctx)
}

// runCatalogRefresh pages through every repo SourceCraft lists into the staging table and, when the
// listing ended normally, replaces the catalog with it. Any failure leaves the catalog as it was.
func (as *AggregatorService) runCatalogRefresh(ctx context.Context, id int64) {
	defer func() {
		as.catalog.mu.Lock()
		as.catalog.cancel()
		as.catalog.running, as.catalog.id, as.catalog.cancel = false, 0, nil
		as.catalog.mu.Unlock()
	}()

	code, err := as.fetchCatalog(ctx, id)
	if ctx.Err() != nil {
		// Cancelled by an admin (already recorded) or by shutdown (failed as interrupted on the next start).
		log.Printf("public repo catalog refresh %d stopped", id)

		return
	}

	if err == nil {
		var size int
		if size, err = as.CatalogRepo.CommitRefresh(ctx, id); err == nil {
			log.Printf("public repo catalog refresh %d completed: %d repositories", id, size)
			as.auditCatalog(ctx, "", domain.AuditActorSystem, "catalog.refresh.complete", domain.AuditOutcomeOK, id, map[string]any{"repos": size})

			return
		}

		if errors.Is(err, domain.ErrCatalogRefreshNotRunning) {
			return
		}

		code = domain.CatalogErrorStore
	}

	log.Printf("public repo catalog refresh %d failed (%s): %v", id, code, err)

	if err := as.CatalogRepo.FinishRefresh(ctx, id, domain.CatalogRefreshFailed, code); err != nil &&
		!errors.Is(err, domain.ErrCatalogRefreshNotRunning) {
		log.Printf("public repo catalog refresh %d: %v", id, err)
	}

	as.auditCatalog(ctx, "", domain.AuditActorSystem, "catalog.refresh.fail", domain.AuditOutcomeError, id, map[string]any{"reason": code})
}

// fetchCatalog stages every page; on failure it returns the fixed error code with the cause.
func (as *AggregatorService) fetchCatalog(ctx context.Context, id int64) (string, error) {
	var (
		pageToken string
		total     int
		seen      = map[string]bool{}
	)

	for page := 1; ; page++ {
		if page > as.catalogMaxPages() {
			return domain.CatalogErrorTooManyPages, fmt.Errorf("listing goes past %d pages", as.catalogMaxPages())
		}

		cont, cancel := context.WithTimeout(withToken(ctx, as.Token), as.rpcTimeout())
		response, err := as.clientRPC.ListPublicReposPage(cont, &client.ListPublicReposPageRequest{PageToken: pageToken})
		cancel()

		if err != nil {
			return domain.CatalogErrorWrapper, wrapperError("list public repositories page", err)
		}

		repos := make([]domain.Repo, 0, len(response.GetRepositories()))
		for _, pbRepo := range response.GetRepositories() {
			repo, err := repoFromWrapper(pbRepo)
			if err != nil || repo.Visibility != domain.VisibilityPublic {
				continue
			}

			repos = append(repos, repo)
		}

		total += len(repos)

		if err := as.CatalogRepo.StagePage(ctx, id, repos, page, total); err != nil {
			return domain.CatalogErrorStore, err
		}

		next := response.GetNextPageToken()
		if next == "" {
			break
		}

		// A token seen before would page forever.
		if seen[next] {
			return domain.CatalogErrorPageLoop, fmt.Errorf("page token repeats at page %d", page)
		}

		seen[next], pageToken = true, next
	}

	// An empty listing is a platform or token problem, not a platform without repos: keep the old catalog.
	if total == 0 {
		return domain.CatalogErrorEmpty, errors.New("listing returned no public repositories")
	}

	return "", nil
}

// auditCatalog records a catalog action; best effort, like auditBatch.
func (as *AggregatorService) auditCatalog(ctx context.Context, actorHash, actorKind, action, outcome string, id int64, details map[string]any) {
	if as.AuditRepo == nil {
		return
	}

	var raw json.RawMessage

	if details != nil {
		raw, _ = json.Marshal(details)
	}

	err := as.RecordAudit(ctx, domain.AuditEvent{
		ActorKind:  actorKind,
		ActorHash:  actorHash,
		Action:     action,
		TargetKind: domain.AuditTargetCatalog,
		Target:     fmt.Sprintf("%d", id),
		Outcome:    outcome,
		Details:    raw,
	})
	if err != nil {
		log.Printf("audit %s for catalog refresh %d: %v", action, id, err)
	}
}
