package analytics

import (
	"sort"
	"strings"
	"time"

	"repo-therapist/grade-service/internal/config"
)

// No-data reasons of the reviews block.
const (
	ReasonUnavailable = "unavailable"
	ReasonFewMerged   = "few_merged"
	ReasonImported    = "imported"
)

// PullsExport is the _meta/pulls.json shape written by repository-service: the newest few pull
// requests with reviewer decisions and comment metadata (never comment text).
type PullsExport struct {
	Available    bool          `json:"available"`
	SampleLimit  int           `json:"sample_limit"`
	PullRequests []PullRequest `json:"pull_requests"`
}

type PullRequest struct {
	Slug              string     `json:"slug"`
	Status            string     `json:"status"`
	Author            string     `json:"author"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ActivityAvailable bool       `json:"activity_available"`
	Reviewers         []Reviewer `json:"reviewers"`
	Comments          []Comment  `json:"comments"`
}

type Reviewer struct {
	User struct {
		Slug string `json:"slug"`
	} `json:"user"`
	Decision string `json:"decision"`
}

type Comment struct {
	Author struct {
		Slug string `json:"slug"`
	} `json:"author"`
	Type        string `json:"type"`
	IsDeleted   bool   `json:"is_deleted"`
	IsPublished bool   `json:"is_published"`
}

// Reviews describes how the sampled pull requests were reviewed. Informational only.
type Reviews struct {
	Status string `json:"status"`
	// Reason says why Status is no_data: unavailable, few_merged or imported.
	Reason string `json:"reason,omitempty"`
	// Sampled is how many pull requests were in the sample; Merged how many of them were merged
	// with readable reviewers and comments, and so judged; Unreviewed how many of those had no
	// review by anyone but the author.
	Sampled    int `json:"sampled"`
	Merged     int `json:"merged"`
	Unreviewed int `json:"unreviewed"`
	// UnreviewedShare is Unreviewed/Merged; nil unless Status is ok.
	UnreviewedShare *float64 `json:"unreviewed_share"`
	// MedianMergeHours is the median of updated_at - created_at over merged pull requests. It is
	// approximate: SourceCraft has no merge time, and updated_at moves on later activity.
	MedianMergeHours *float64 `json:"median_merge_hours"`
}

// reviewStats judges a sample. cfg.MinMergedPRs and cfg.ImportShare come from config.
func reviewStats(cfg config.AnalyticsConfig, pulls *PullsExport) Reviews {
	reviews := Reviews{Status: StatusNoData}
	if pulls == nil || !pulls.Available {
		reviews.Reason = ReasonUnavailable
		return reviews
	}

	reviews.Sampled = len(pulls.PullRequests)

	if importedSample(pulls.PullRequests, cfg.MinMergedPRs, cfg.ImportShare) {
		reviews.Reason = ReasonImported
		return reviews
	}

	var mergeHours []float64
	for _, pr := range pulls.PullRequests {
		if pr.Status != "merged" {
			continue
		}

		if hours := pr.UpdatedAt.Sub(pr.CreatedAt).Hours(); !pr.CreatedAt.IsZero() && hours >= 0 {
			mergeHours = append(mergeHours, hours)
		}

		if !pr.ActivityAvailable {
			continue
		}

		reviews.Merged++
		if !hasReview(pr) {
			reviews.Unreviewed++
		}
	}

	if reviews.Merged < cfg.MinMergedPRs {
		reviews.Reason = ReasonFewMerged
		return reviews
	}

	reviews.Status = StatusOK
	share := round2(float64(reviews.Unreviewed) / float64(reviews.Merged))
	reviews.UnreviewedShare = &share

	if len(mergeHours) > 0 {
		median := round2(medianOf(mergeHours))
		reviews.MedianMergeHours = &median
	}

	return reviews
}

// hasReview: someone other than the author, and not a bot, either left a reviewer decision or a
// published, non-deleted, non-AppSec comment. The author's own comments and bots do not count.
func hasReview(pr PullRequest) bool {
	for _, r := range pr.Reviewers {
		if counts(r.User.Slug, pr.Author) {
			return true
		}
	}

	for _, c := range pr.Comments {
		if c.Type == "appsec" || c.IsDeleted || !c.IsPublished {
			continue
		}

		if counts(c.Author.Slug, pr.Author) {
			return true
		}
	}

	return false
}

func counts(slug, author string) bool {
	return slug != "" && slug != author && !isBot(slug)
}

func isBot(slug string) bool {
	slug = strings.ToLower(slug)
	for _, suffix := range []string{"[bot]", "-bot", "_bot"} {
		if strings.HasSuffix(slug, suffix) {
			return true
		}
	}

	return false
}

// importedSample reports a bulk-imported history: at least minPRs pull requests and importShare
// or more of them created on one calendar day (UTC).
func importedSample(prs []PullRequest, minPRs int, importShare float64) bool {
	if len(prs) == 0 || len(prs) < minPRs || importShare <= 0 {
		return false
	}

	perDay := map[string]int{}
	for _, pr := range prs {
		perDay[pr.CreatedAt.UTC().Format("2006-01-02")]++
	}

	most := 0
	for _, n := range perDay {
		if n > most {
			most = n
		}
	}

	return float64(most)/float64(len(prs)) >= importShare
}

func medianOf(values []float64) float64 {
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}

	return (values[mid-1] + values[mid]) / 2
}
