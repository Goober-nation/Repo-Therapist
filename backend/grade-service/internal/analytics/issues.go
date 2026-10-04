package analytics

import (
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/issuereplies"
)

// No-data reasons of the issue replies block, besides ReasonUnavailable and ReasonImported.
const (
	ReasonFewIssues  = "few_issues"
	ReasonFewAuthors = "few_authors"
)

// IssueReplies describes how quickly issues get a first reply, over the newest sampled issues (the
// same sample, rules and numbers as the Issues category's first-reply metric, but shown even when
// that metric is not applicable to the score). Informational only.
type IssueReplies struct {
	Status string `json:"status"`
	// Reason says why Status is no_data: unavailable, few_issues, few_authors or imported.
	Reason string `json:"reason,omitempty"`
	// Sampled is how many issues with readable comments were judged; Replied how many got a reply
	// by someone other than the author or a bot; Unanswered the rest.
	Sampled    int `json:"sampled"`
	Replied    int `json:"replied"`
	Unanswered int `json:"unanswered"`
	// RepliedShare is Replied/Sampled; nil unless Status is ok.
	RepliedShare *float64 `json:"replied_share"`
	// MedianFirstReplyHours is the median wait: an unanswered issue counts as the time it waited
	// (until it was closed, or until now), at most CapDays.
	MedianFirstReplyHours *float64 `json:"median_first_reply_hours"`
	CapDays               int      `json:"cap_days"`
}

// issueReplyStats judges the sample. The export must already be filtered (instant-close issues out).
func issueReplyStats(cfg config.AnalyticsConfig, export *issuereplies.Export, now time.Time) IssueReplies {
	result := IssueReplies{Status: StatusNoData, CapDays: cfg.IssueCapDays}
	if export == nil || !export.Available {
		result.Reason = ReasonUnavailable
		return result
	}

	if issuereplies.DistinctAuthors(export.Issues) < cfg.MinIssueAuthors {
		result.Reason = ReasonFewAuthors
		return result
	}

	if issuereplies.Imported(export.Issues, cfg.ImportShare, cfg.MinIssues) {
		result.Reason = ReasonImported
		return result
	}

	samples := issuereplies.Samples(export.Issues, now, cfg.IssueCapDays)
	result.Sampled = len(samples)

	if len(samples) < cfg.MinIssues {
		result.Reason = ReasonFewIssues
		return result
	}

	hours := make([]float64, 0, len(samples))

	for _, s := range samples {
		hours = append(hours, s.Hours)

		if s.Replied {
			result.Replied++
		}
	}

	result.Unanswered = result.Sampled - result.Replied
	share := round2(float64(result.Replied) / float64(result.Sampled))
	median := round2(medianOf(hours))
	result.Status, result.RepliedShare, result.MedianFirstReplyHours = StatusOK, &share, &median

	return result
}
