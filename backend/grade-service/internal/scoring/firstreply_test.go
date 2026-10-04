package scoring

import (
	"fmt"
	"math"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/issuereplies"
)

var replyNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func replyAgg() config.AggregationConfig {
	return config.AggregationConfig{Type: "median_first_reply_hours", CapDays: 30, MinIssueAuthors: 3, MinSampleSize: 5, ImportShare: 0.8}
}

// repliedIssue: opened by author daysAgo days ago and answered by "maintainer" hoursToReply later
// (negative: never answered, still open).
func repliedIssue(author string, daysAgo int, hoursToReply float64) issuereplies.Issue {
	created := replyNow.AddDate(0, 0, -daysAgo)
	i := issuereplies.Issue{Slug: fmt.Sprint(daysAgo, author), Author: author, StatusType: "initial", CreatedAt: created, UpdatedAt: created, RepliesAvailable: true}
	if hoursToReply >= 0 {
		i.Comments = []issuereplies.Comment{{Author: "maintainer", CreatedAt: created.Add(time.Duration(hoursToReply * float64(time.Hour)))}}
	}

	return i
}

func export(issues ...issuereplies.Issue) issuereplies.Export {
	return issuereplies.Export{Available: true, SampleLimit: 25, Issues: issues}
}

func TestMedianFirstReplyHoursOverAMixedCommunity(t *testing.T) {
	got := medianFirstReplyHours(replyAgg(), export(
		repliedIssue("u1", 3, 2), repliedIssue("u2", 6, 10), repliedIssue("u3", 9, 30), repliedIssue("u4", 12, 100), repliedIssue("u5", 15, -1),
	), replyNow)

	// Waits: 2, 10, 30, 100 and the 15 days (360 h) the last one is still waiting -> median 30.
	if got.NoData || got.Value != 30 {
		t.Errorf("got %+v, want a median of 30 hours", got)
	}
}

func TestMedianFirstReplyHoursCountsNeglectAsAWaitNotAsNoData(t *testing.T) {
	got := medianFirstReplyHours(replyAgg(), export(
		repliedIssue("u1", 40, -1), repliedIssue("u2", 50, -1), repliedIssue("u3", 60, -1), repliedIssue("u4", 70, -1), repliedIssue("u5", 80, -1),
	), replyNow)

	if got.NoData || got.Value != 720 {
		t.Errorf("got %+v, want the 30-day cap (720 h): five reporters nobody answered is neglect, not missing data", got)
	}
}

func TestMedianFirstReplyHoursNotApplicable(t *testing.T) {
	cases := map[string]issuereplies.Export{
		"sample unavailable": {Available: false},
		"few authors (a team tracking its own work)": export(
			repliedIssue("u1", 3, -1), repliedIssue("u2", 6, -1), repliedIssue("u1", 9, -1), repliedIssue("u2", 12, -1), repliedIssue("u1", 15, -1), repliedIssue("u2", 18, -1),
		),
		"too few issues": export(repliedIssue("u1", 3, 2), repliedIssue("u2", 6, 2), repliedIssue("u3", 9, 2), repliedIssue("u4", 12, 2)),
	}

	imported := export()
	for i := 0; i < 6; i++ {
		issue := repliedIssue(fmt.Sprint("u", i), 20, 5) // all created the same day
		issue.Slug = fmt.Sprint("s", i)
		imported.Issues = append(imported.Issues, issue)
	}
	cases["imported tracker"] = imported

	for name, ex := range cases {
		if got := medianFirstReplyHours(replyAgg(), ex, replyNow); !got.NoData {
			t.Errorf("%s: got %+v, want no data (never a bad score)", name, got)
		}
	}
}

func TestFirstReplyMetricInTheIssuesCategoryScoresAndRenormalizes(t *testing.T) {
	cfg := config.CategoryConfig{
		Status: config.StatusCalculated,
		Metrics: []config.MetricConfig{
			{ID: "median_first_reply_hours", Status: config.StatusCalculated, Weight: 0.5, Source: "issue-replies.json", Aggregation: replyAgg(),
				Formula: config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 336, Healthy: 24}},
			{ID: "abandoned_share", Status: config.StatusCalculated, Weight: 0.5, Source: "issues.json",
				Aggregation: config.AggregationConfig{Type: "abandoned_share", StalenessDays: 60, MinSampleSize: 1},
				Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 0.5, Healthy: 0.05}},
		},
	}

	inputs := ActivityInputs{
		IssuesAvailable: true,
		Issues:          []Issue{{CreatedAt: replyNow.AddDate(0, 0, -5).Format(time.RFC3339Nano), UpdatedAt: replyNow.Format(time.RFC3339Nano), StatusType: "initial"}},
		IssueReplies: export(
			repliedIssue("u1", 3, 24), repliedIssue("u2", 6, 24), repliedIssue("u3", 9, 24), repliedIssue("u4", 12, 24), repliedIssue("u5", 15, 24),
		),
	}

	withReplies := ComputeCategory("issues", cfg, inputs, replyNow)
	byID := map[string]MetricResult{}
	for _, m := range withReplies.Metrics {
		byID[m.ID] = m
	}

	reply := byID["median_first_reply_hours"]
	if reply.NoData || reply.RawValue != 24 || reply.Score != 1 {
		t.Errorf("first-reply metric = %+v, want raw 24 h and a perfect score", reply)
	}

	// A sample the metric does not apply to drops out and the other metric carries the whole category.
	inputs.IssueReplies = issuereplies.Export{Available: false}
	without := ComputeCategory("issues", cfg, inputs, replyNow)
	if math.Abs(without.Score-byID["abandoned_share"].Score*100) > 0.001 {
		t.Errorf("without the sample the score is %.2f, want the other metric's %.2f alone", without.Score, byID["abandoned_share"].Score*100)
	}
}

func TestFirstReplySampleIsFilteredLikeTheIssues(t *testing.T) {
	cfg := config.CategoryConfig{
		Status:       config.StatusCalculated,
		IssueFilters: config.IssueFilters{InstantCloseSeconds: 30},
		Metrics: []config.MetricConfig{{ID: "median_first_reply_hours", Status: config.StatusCalculated, Weight: 1, Source: "issue-replies.json", Aggregation: replyAgg(),
			Formula: config.FormulaConfig{Type: "linear_clamp", Direction: "-", Critical: 336, Healthy: 24}}},
	}

	// Five real issues and one tracker entry closed in a second: the entry must not count.
	instant := repliedIssue("u6", 7, -1)
	instant.Slug, instant.StatusType, instant.UpdatedAt = "instant", "completed", instant.CreatedAt.Add(time.Second)

	ex := export(repliedIssue("u1", 3, 24), repliedIssue("u2", 6, 24), repliedIssue("u3", 9, 24), repliedIssue("u4", 12, 24), repliedIssue("u5", 15, 24), instant)
	got := ComputeCategory("issues", cfg, ActivityInputs{IssueReplies: ex}, replyNow)

	if got.NoData || got.Metrics[0].RawValue != 24 {
		t.Errorf("got %+v, want a median of 24 h with the instant-close entry left out", got.Metrics[0])
	}
}
