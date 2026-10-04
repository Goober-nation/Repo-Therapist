// Package issuedata builds the _meta/issue-replies.json export the first-reply metric reads
// (docs/grading/issues.md and analytics.md): the newest few issues of the repo with who commented
// and when, fetched through sourcecraft-wrapper-service. Comment text is never exported.
package issuedata

import (
	"context"
	"log"
	"sort"
	"sync"

	"repo-therapist/repository-service/internal/scwrapper"
)

const (
	// DefaultSampleLimit is how many of the newest issues are sampled (ISSUE_REPLY_SAMPLE_LIMIT).
	DefaultSampleLimit = 25
	// Workers bounds the parallel per-issue comment fetches: SourceCraft answers in 1.5-5 s per
	// call, so a sequential sample would take well over a minute.
	Workers = 4
)

// Fetcher is the part of the wrapper client the export needs.
type Fetcher interface {
	GetIssueComments(ctx context.Context, issueID string) ([]scwrapper.IssueComment, error)
}

// Issue is one sampled issue. RepliesAvailable=false means its comments could not be read, so it
// says nothing about replies (distinct from readable and empty).
type Issue struct {
	Slug             string                   `json:"slug"`
	StatusType       string                   `json:"status_type"`
	Author           string                   `json:"author"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
	RepliesAvailable bool                     `json:"replies_available"`
	Comments         []scwrapper.IssueComment `json:"comments"`
}

// Export is the _meta/issue-replies.json shape. Available=false means no sample was taken (switched
// off with a limit of 0): Issues is then empty and means nothing, which is distinct from
// Available=true with no issues (readable, genuinely none).
type Export struct {
	Available   bool    `json:"available"`
	SampleLimit int     `json:"sample_limit"`
	Issues      []Issue `json:"issues"`
}

// Collect samples the newest limit issues by creation time (any status) and reads their comments.
// It never fails: a comment fetch error only marks that issue's replies unavailable.
func Collect(ctx context.Context, f Fetcher, issues []scwrapper.Issue, limit int) Export {
	export := Export{SampleLimit: limit, Issues: []Issue{}}
	if limit <= 0 {
		return export
	}

	export.Available = true

	newest := append([]scwrapper.Issue(nil), issues...)
	// RFC 3339 timestamps from one source sort correctly as text; ties keep a stable order by slug.
	sort.SliceStable(newest, func(i, j int) bool {
		if newest[i].CreatedAt != newest[j].CreatedAt {
			return newest[i].CreatedAt > newest[j].CreatedAt
		}

		return newest[i].Slug < newest[j].Slug
	})

	if len(newest) > limit {
		newest = newest[:limit]
	}

	export.Issues = make([]Issue, len(newest))

	var wg sync.WaitGroup
	jobs := make(chan int)

	for w := 0; w < Workers; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range jobs {
				export.Issues[i] = collectOne(ctx, f, newest[i])
			}
		}()
	}

	for i := range newest {
		jobs <- i
	}

	close(jobs)
	wg.Wait()

	return export
}

func collectOne(ctx context.Context, f Fetcher, issue scwrapper.Issue) Issue {
	out := Issue{
		Slug:       issue.Slug,
		StatusType: issue.StatusType,
		Author:     issue.AuthorSlug,
		CreatedAt:  issue.CreatedAt,
		UpdatedAt:  issue.UpdatedAt,
		Comments:   []scwrapper.IssueComment{},
	}

	comments, err := f.GetIssueComments(ctx, issue.ID)
	if err != nil {
		log.Printf("comments of issue %s unavailable: %v", issue.Slug, err)
		return out
	}

	out.RepliesAvailable = true
	out.Comments = comments

	return out
}
