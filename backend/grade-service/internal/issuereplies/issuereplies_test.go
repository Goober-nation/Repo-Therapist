package issuereplies

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func at(daysAgo float64) time.Time {
	return now.Add(-time.Duration(daysAgo * 24 * float64(time.Hour)))
}

func issue(author, status string, createdDaysAgo float64, comments ...Comment) Issue {
	created := at(createdDaysAgo)

	return Issue{Slug: "x", Author: author, StatusType: status, CreatedAt: created, UpdatedAt: created.Add(2 * time.Hour), RepliesAvailable: true, Comments: comments}
}

func comment(author string, createdDaysAgo float64) Comment {
	return Comment{Author: author, CreatedAt: at(createdDaysAgo)}
}

func TestSamplesUseTheFirstReplyByAnotherHuman(t *testing.T) {
	got := Samples([]Issue{
		issue("alice", "initial", 10, comment("alice", 9.9), comment("ci-bot", 9.8), comment("bob", 9), comment("carol", 5)),
	}, now, 30)

	// Alice's own comment and the bot do not count: Bob answered a day after creation.
	if len(got) != 1 || !got[0].Replied || got[0].Hours != 24 {
		t.Errorf("got %+v, want one replied sample of 24 hours", got)
	}
}

func TestSamplesCountAnUnansweredIssueAsItsWaitCappedAndClosedOnesUntilClosing(t *testing.T) {
	open := issue("alice", "initial", 5)
	closedFast := issue("alice", "completed", 5) // updated 2 hours after creation
	ancient := issue("alice", "initial", 200)

	got := Samples([]Issue{open, closedFast, ancient}, now, 30)

	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Replied || got[0].Hours != 120 {
		t.Errorf("open and unanswered: %+v, want its 5 days of waiting (120 h)", got[0])
	}
	if got[1].Replied || got[1].Hours != 2 {
		t.Errorf("closed without a reply: %+v, want the 2 hours until it was closed, not neglect", got[1])
	}
	if got[2].Hours != 720 {
		t.Errorf("a very old unanswered issue: %+v, want the 30-day cap (720 h)", got[2])
	}
}

func TestSamplesIgnoreUnreadableIssuesAndCommentsBeforeCreation(t *testing.T) {
	unreadable := issue("alice", "initial", 5)
	unreadable.RepliesAvailable = false

	early := issue("alice", "initial", 5, comment("bob", 6)) // a comment dated before the issue: clock skew
	got := Samples([]Issue{unreadable, early}, now, 30)

	if len(got) != 1 || got[0].Replied {
		t.Errorf("got %+v, want only the readable issue, with the pre-creation comment ignored", got)
	}
}

func TestDistinctAuthorsSkipBotsAndUnreadableIssues(t *testing.T) {
	unreadable := issue("dave", "initial", 1)
	unreadable.RepliesAvailable = false

	issues := []Issue{issue("alice", "initial", 1), issue("alice", "initial", 2), issue("bob", "initial", 3), issue("ci-bot", "initial", 4), issue("", "initial", 5), unreadable}
	if got := DistinctAuthors(issues); got != 2 {
		t.Errorf("distinct authors = %d, want 2 (alice and bob)", got)
	}
}

func TestImportedNeedsEnoughIssuesAndASingleDayShare(t *testing.T) {
	sameDay := func(n int) []Issue {
		var out []Issue
		for i := 0; i < n; i++ {
			out = append(out, issue("a", "initial", 10))
		}

		return out
	}

	if !Imported(sameDay(5), 0.8, 5) {
		t.Error("5 of 5 on one day with a minimum of 5 is an import")
	}
	if Imported(sameDay(4), 0.8, 5) {
		t.Error("below the minimum sample the guard does not apply")
	}

	spread := sameDay(3)
	for i := 0; i < 7; i++ {
		spread = append(spread, issue("a", "initial", float64(20+i*3)))
	}
	if Imported(spread, 0.8, 5) {
		t.Error("3 of 10 on one day is not an import")
	}
	if Imported(sameDay(5), 0, 5) {
		t.Error("a zero share switches the guard off")
	}
}

func TestFilterInstantCloseDropsOnlyClosedIssuesClosedInstantly(t *testing.T) {
	instant := issue("a", "completed", 5)
	instant.UpdatedAt = instant.CreatedAt.Add(10 * time.Second)
	slow := issue("a", "completed", 5) // closed after 2 hours
	openInstant := issue("a", "initial", 5)
	openInstant.UpdatedAt = openInstant.CreatedAt.Add(10 * time.Second) // still open: kept

	export := Export{Available: true, Issues: []Issue{instant, slow, openInstant}}
	got := export.FilterInstantClose(30)

	if len(got.Issues) != 2 || len(export.Issues) != 3 {
		t.Errorf("kept %d of %d, want 2 of 3 and the input untouched", len(got.Issues), len(export.Issues))
	}
	if same := export.FilterInstantClose(0); len(same.Issues) != 3 {
		t.Error("a zero threshold must keep everything")
	}
}

func TestIsBot(t *testing.T) {
	for slug, want := range map[string]bool{"dependabot[bot]": true, "ci-bot": true, "x_bot": true, "Robot": false, "bob": false} {
		if IsBot(slug) != want {
			t.Errorf("IsBot(%q) = %v, want %v", slug, !want, want)
		}
	}
}
