package analytics

import (
	"fmt"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/issuereplies"
)

func replyIssue(author string, daysAgo int, hoursToReply float64) issuereplies.Issue {
	created := now.AddDate(0, 0, -daysAgo)
	i := issuereplies.Issue{Slug: fmt.Sprint(daysAgo, author), Author: author, StatusType: "initial", CreatedAt: created, UpdatedAt: created, RepliesAvailable: true}
	if hoursToReply >= 0 {
		i.Comments = []issuereplies.Comment{{Author: "maintainer", CreatedAt: created.Add(time.Duration(hoursToReply * float64(time.Hour)))}}
	}

	return i
}

func repliesExport(issues ...issuereplies.Issue) *issuereplies.Export {
	return &issuereplies.Export{Available: true, SampleLimit: 25, Issues: issues}
}

func TestIssueRepliesMedianRepliedShareAndUnanswered(t *testing.T) {
	got := issueReplyStats(testConfig(), repliesExport(
		replyIssue("u1", 3, 2), replyIssue("u2", 6, 10), replyIssue("u3", 9, 30), replyIssue("u4", 12, -1), replyIssue("u5", 15, -1),
	), now)

	if got.Status != StatusOK || got.Sampled != 5 || got.Replied != 3 || got.Unanswered != 2 || got.CapDays != 30 {
		t.Fatalf("got %+v", got)
	}
	if got.RepliedShare == nil || *got.RepliedShare != 0.6 {
		t.Errorf("replied share = %v, want 0.6", got.RepliedShare)
	}
	// Waits 2, 10, 30 and the two unanswered ones at 288 h and 360 h -> median 30.
	if got.MedianFirstReplyHours == nil || *got.MedianFirstReplyHours != 30 {
		t.Errorf("median = %v, want 30", got.MedianFirstReplyHours)
	}
}

func TestIssueRepliesNoDataReasons(t *testing.T) {
	if got := issueReplyStats(testConfig(), nil, now); got.Status != StatusNoData || got.Reason != ReasonUnavailable || got.MedianFirstReplyHours != nil {
		t.Errorf("no export: %+v", got)
	}
	if got := issueReplyStats(testConfig(), &issuereplies.Export{Available: false}, now); got.Reason != ReasonUnavailable {
		t.Errorf("sample off: %+v", got)
	}

	solo := repliesExport(replyIssue("u1", 3, -1), replyIssue("u2", 6, -1), replyIssue("u1", 9, -1), replyIssue("u2", 12, -1), replyIssue("u1", 15, -1))
	if got := issueReplyStats(testConfig(), solo, now); got.Reason != ReasonFewAuthors {
		t.Errorf("two reporters: %+v, want few_authors", got)
	}

	few := repliesExport(replyIssue("u1", 3, 1), replyIssue("u2", 6, 1), replyIssue("u3", 9, 1))
	if got := issueReplyStats(testConfig(), few, now); got.Reason != ReasonFewIssues || got.Sampled != 3 {
		t.Errorf("three issues: %+v, want few_issues with the sample size reported", got)
	}

	var same []issuereplies.Issue
	for i := 0; i < 6; i++ {
		same = append(same, replyIssue(fmt.Sprint("u", i), 20, 3))
	}
	if got := issueReplyStats(testConfig(), repliesExport(same...), now); got.Reason != ReasonImported {
		t.Errorf("same-day tracker: %+v, want imported", got)
	}
}

func TestComputeIncludesIssueRepliesAndTheyNeverChangeTheRest(t *testing.T) {
	result := Compute(testConfig(), Inputs{IssueReplies: repliesExport(
		replyIssue("u1", 3, 2), replyIssue("u2", 6, 10), replyIssue("u3", 9, 30), replyIssue("u4", 12, 40), replyIssue("u5", 15, 50),
	)}, now)

	if result.IssueReplies.Status != StatusOK || result.Version != 4 {
		t.Fatalf("result = %+v", result)
	}
	if result.BusFactor.Status != StatusNoData || result.Reviews.Status != StatusNoData {
		t.Errorf("the other blocks changed: %+v", result)
	}
}
