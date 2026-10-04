// Package issuereplies reads the _meta/issue-replies.json export written by repository-service
// (the newest few issues with who commented and when) and turns it into first-reply times. The
// Issues score metric and the analytics block both use it, so they cannot disagree. Comment text
// is never exported.
package issuereplies

import (
	"strings"
	"time"
)

// Comment is who commented on an issue and when.
type Comment struct {
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

// Issue is one sampled issue. RepliesAvailable false means its comments could not be read: it
// says nothing about replies and is left out of every figure.
type Issue struct {
	Slug             string    `json:"slug"`
	StatusType       string    `json:"status_type"`
	Author           string    `json:"author"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	RepliesAvailable bool      `json:"replies_available"`
	Comments         []Comment `json:"comments"`
}

// Export is the _meta/issue-replies.json shape. Available false means no sample was taken.
type Export struct {
	Available   bool    `json:"available"`
	SampleLimit int     `json:"sample_limit"`
	Issues      []Issue `json:"issues"`
}

// Sample is one issue's wait for a first reply. Hours is the time to the first reply by someone
// other than the author or a bot; an issue without such a reply counts as the time it waited
// (until it was closed, or until now if it is still open), never more than the cap.
type Sample struct {
	Hours   float64
	Replied bool
}

func closed(i Issue) bool { return i.StatusType == "completed" || i.StatusType == "cancelled" }

// IsBot reports a bot account by the same name rule as the commit filters.
func IsBot(slug string) bool {
	slug = strings.ToLower(slug)
	for _, suffix := range []string{"[bot]", "-bot", "_bot"} {
		if strings.HasSuffix(slug, suffix) {
			return true
		}
	}

	return false
}

// FilterInstantClose drops closed issues whose last update came within instantCloseSeconds of
// their creation: an issue opened and closed in one moment is a tracker entry made for the count,
// the same rule as the Issues category's own filter. The input is not modified.
func (e Export) FilterInstantClose(instantCloseSeconds int) Export {
	if instantCloseSeconds <= 0 {
		return e
	}

	limit := time.Duration(instantCloseSeconds) * time.Second
	kept := make([]Issue, 0, len(e.Issues))

	for _, i := range e.Issues {
		if closed(i) && !i.CreatedAt.IsZero() && !i.UpdatedAt.IsZero() && i.UpdatedAt.Sub(i.CreatedAt) <= limit {
			continue
		}

		kept = append(kept, i)
	}

	e.Issues = kept

	return e
}

// Samples computes the wait of every issue whose comments could be read. capDays bounds a wait
// (30 means a month is the longest an unanswered issue can count for).
func Samples(issues []Issue, now time.Time, capDays int) []Sample {
	limit := float64(capDays) * 24
	out := make([]Sample, 0, len(issues))

	for _, i := range issues {
		if !i.RepliesAvailable || i.CreatedAt.IsZero() {
			continue
		}

		var first time.Time

		for _, c := range i.Comments {
			if c.Author == "" || c.Author == i.Author || IsBot(c.Author) || c.CreatedAt.Before(i.CreatedAt) {
				continue
			}

			if first.IsZero() || c.CreatedAt.Before(first) {
				first = c.CreatedAt
			}
		}

		sample := Sample{Replied: !first.IsZero()}

		if sample.Replied {
			sample.Hours = first.Sub(i.CreatedAt).Hours()
		} else {
			end := now
			if closed(i) && !i.UpdatedAt.IsZero() {
				end = i.UpdatedAt
			}

			sample.Hours = end.Sub(i.CreatedAt).Hours()
		}

		if sample.Hours < 0 {
			sample.Hours = 0
		}

		if limit > 0 && sample.Hours > limit {
			sample.Hours = limit
		}

		out = append(out, sample)
	}

	return out
}

// DistinctAuthors counts the different people (not bots) who opened the issues. A repository whose
// issues all come from one or two people is a team tracking its own work, where "nobody replied"
// is not neglect, so the first-reply metric needs a minimum number of different reporters.
func DistinctAuthors(issues []Issue) int {
	seen := map[string]struct{}{}

	for _, i := range issues {
		if i.RepliesAvailable && i.Author != "" && !IsBot(i.Author) {
			seen[i.Author] = struct{}{}
		}
	}

	return len(seen)
}

// Imported reports a bulk-imported tracker: at least minIssues issues and importShare or more of
// them created on one calendar day (UTC).
func Imported(issues []Issue, importShare float64, minIssues int) bool {
	count := 0
	perDay := map[string]int{}

	for _, i := range issues {
		if !i.RepliesAvailable || i.CreatedAt.IsZero() {
			continue
		}

		count++
		perDay[i.CreatedAt.UTC().Format("2006-01-02")]++
	}

	if count == 0 || count < minIssues || importShare <= 0 {
		return false
	}

	most := 0

	for _, n := range perDay {
		if n > most {
			most = n
		}
	}

	return float64(most)/float64(count) >= importShare
}
