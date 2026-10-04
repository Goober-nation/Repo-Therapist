// Package prdata builds the _meta/pulls.json export the review analytics read
// (docs/grading/analytics.md): the newest few pull requests of the repo with their reviewers and
// comment metadata, fetched through sourcecraft-wrapper-service. Comment text is never exported.
package prdata

import (
	"context"
	"log"
	"sync"

	"repo-therapist/repository-service/internal/scwrapper"
)

const (
	// DefaultSampleLimit is how many of the newest pull requests are sampled (PR_SAMPLE_LIMIT).
	DefaultSampleLimit = 20
	// Workers bounds the parallel per-PR activity fetches: SourceCraft answers in 1.5-5 s per
	// call, so a sequential sample would take over a minute.
	Workers = 4
)

// Fetcher is the part of the wrapper client the export needs.
type Fetcher interface {
	GetPullRequests(ctx context.Context, repoID string, limit int) ([]scwrapper.PullRequest, error)
	GetPullRequestActivity(ctx context.Context, pullRequestID string) (scwrapper.PullRequestActivity, error)
}

// PullRequest is one sampled pull request. ActivityAvailable=false means its reviewers/comments
// could not be read, so it says nothing about reviews (distinct from readable and empty).
type PullRequest struct {
	Slug              string                          `json:"slug"`
	Status            string                          `json:"status"`
	Author            string                          `json:"author"`
	CreatedAt         string                          `json:"created_at"`
	UpdatedAt         string                          `json:"updated_at"`
	ActivityAvailable bool                            `json:"activity_available"`
	Reviewers         []scwrapper.PullRequestReviewer `json:"reviewers"`
	Comments          []scwrapper.PullRequestComment  `json:"comments"`
}

// Export is the _meta/pulls.json shape. Available=false means the sample could not be fetched
// (or is switched off with a limit of 0): PullRequests is then empty and means nothing, which is
// distinct from Available=true with no pull requests (readable, genuinely none).
type Export struct {
	Available    bool          `json:"available"`
	SampleLimit  int           `json:"sample_limit"`
	PullRequests []PullRequest `json:"pull_requests"`
}

// Collect fetches the sample. It never fails: any error only marks the affected part unavailable.
func Collect(ctx context.Context, f Fetcher, repoID string, limit int) Export {
	export := Export{SampleLimit: limit, PullRequests: []PullRequest{}}
	if limit <= 0 {
		return export
	}

	prs, err := f.GetPullRequests(ctx, repoID, limit)
	if err != nil {
		log.Printf("pull requests unavailable for repo %s: %v (review analytics will be no-data)", repoID, err)
		return export
	}
	if len(prs) > limit {
		prs = prs[:limit]
	}

	export.Available = true
	export.PullRequests = make([]PullRequest, len(prs))

	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				export.PullRequests[i] = collectOne(ctx, f, prs[i])
			}
		}()
	}
	for i := range prs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	return export
}

func collectOne(ctx context.Context, f Fetcher, pr scwrapper.PullRequest) PullRequest {
	out := PullRequest{
		Slug:      pr.Slug,
		Status:    pr.Status,
		Author:    pr.Author.Slug,
		CreatedAt: pr.CreatedAt,
		UpdatedAt: pr.UpdatedAt,
		Reviewers: []scwrapper.PullRequestReviewer{},
		Comments:  []scwrapper.PullRequestComment{},
	}

	activity, err := f.GetPullRequestActivity(ctx, pr.ID)
	if err != nil {
		log.Printf("activity of pull request %s unavailable: %v", pr.Slug, err)
		return out
	}

	out.ActivityAvailable = true
	out.Reviewers = activity.Reviewers
	out.Comments = activity.Comments

	return out
}
