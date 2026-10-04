package analytics

import (
	"encoding/json"
	"testing"
	"time"
)

func pr(slug, status, author string, createdDaysAgo int, mergeHours float64, activity bool, reviewers []string, comments ...Comment) PullRequest {
	created := now.AddDate(0, 0, -createdDaysAgo)
	out := PullRequest{
		Slug: slug, Status: status, Author: author, CreatedAt: created,
		UpdatedAt:         created.Add(time.Duration(mergeHours * float64(time.Hour))),
		ActivityAvailable: activity, Comments: comments,
	}
	for _, r := range reviewers {
		var rv Reviewer
		rv.User.Slug = r
		rv.Decision = "approve"
		out.Reviewers = append(out.Reviewers, rv)
	}
	return out
}

func comment(author, typ string, deleted, published bool) Comment {
	var c Comment
	c.Author.Slug = author
	c.Type = typ
	c.IsDeleted = deleted
	c.IsPublished = published
	return c
}

func stats(pulls *PullsExport) Reviews {
	return reviewStats(testConfig(), pulls, now)
}

func TestReviewsCountsUnreviewedAndMedianMergeTime(t *testing.T) {
	got := stats(&PullsExport{Available: true, PullRequests: []PullRequest{
		pr("1", "merged", "alice", 1, 2, true, []string{"bob"}),                               // reviewed by decision
		pr("2", "merged", "alice", 3, 4, true, nil, comment("carol", "default", false, true)), // reviewed by comment
		pr("3", "merged", "alice", 5, 10, true, nil),                                          // no review
		pr("4", "merged", "alice", 7, 6, true, []string{"alice"}),                             // author only: no review
		pr("5", "open", "alice", 9, 0, true, nil),                                             // open: not judged
		pr("6", "discarded", "alice", 11, 0, true, nil),                                       // discarded: not judged
	}})

	if got.Status != StatusOK || got.Sampled != 6 || got.Merged != 4 || got.Unreviewed != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.UnreviewedShare == nil || *got.UnreviewedShare != 0.5 {
		t.Errorf("share = %v, want 0.5", got.UnreviewedShare)
	}
	// Merge hours 2, 4, 10, 6 -> median (4+6)/2 = 5.
	if got.MedianMergeHours == nil || *got.MedianMergeHours != 5 {
		t.Errorf("median = %v, want 5", got.MedianMergeHours)
	}
}

func TestReviewsIgnoresBotsAppsecDeletedAndDraftComments(t *testing.T) {
	got := stats(&PullsExport{Available: true, PullRequests: []PullRequest{
		pr("1", "merged", "alice", 1, 1, true, []string{"dependabot[bot]"}),
		pr("2", "merged", "alice", 3, 1, true, nil, comment("scanner", "appsec", false, true)),
		pr("3", "merged", "alice", 5, 1, true, nil, comment("bob", "default", true, true)),
		pr("4", "merged", "alice", 7, 1, true, nil, comment("bob", "default", false, false)),
		pr("5", "merged", "alice", 9, 1, true, nil, comment("ci-bot", "default", false, true)),
	}})

	if got.Merged != 5 || got.Unreviewed != 5 {
		t.Errorf("got %+v, want all five unreviewed", got)
	}
}

func TestReviewsNoDataReasons(t *testing.T) {
	cases := map[string]struct {
		pulls  *PullsExport
		reason string
	}{
		"missing file":      {nil, ReasonUnavailable},
		"fetch failed":      {&PullsExport{Available: false}, ReasonUnavailable},
		"too few merged":    {&PullsExport{Available: true, PullRequests: []PullRequest{pr("1", "merged", "a", 1, 1, true, nil), pr("2", "merged", "a", 5, 1, true, nil), pr("3", "open", "a", 9, 1, true, nil)}}, ReasonFewMerged},
		"unreadable merged": {&PullsExport{Available: true, PullRequests: []PullRequest{pr("1", "merged", "a", 1, 1, false, nil), pr("2", "merged", "a", 5, 1, false, nil), pr("3", "merged", "a", 9, 1, false, nil)}}, ReasonFewMerged},
		"empty sample":      {&PullsExport{Available: true}, ReasonFewMerged},
	}
	for name, tc := range cases {
		got := stats(tc.pulls)
		if got.Status != StatusNoData || got.Reason != tc.reason || got.UnreviewedShare != nil {
			t.Errorf("%s: got %+v, want no_data/%s", name, got, tc.reason)
		}
	}
}

func TestReviewsImportGuard(t *testing.T) {
	sameDay := func(n int) []PullRequest {
		var prs []PullRequest
		for i := 0; i < n; i++ {
			p := pr("x", "merged", "a", 2, 1, true, nil)
			p.CreatedAt = p.CreatedAt.Add(time.Duration(i) * time.Minute)
			prs = append(prs, p)
		}
		return prs
	}

	// 10 PRs created on one day: imported history, no data.
	if got := stats(&PullsExport{Available: true, PullRequests: sameDay(10)}); got.Status != StatusNoData || got.Reason != ReasonImported {
		t.Errorf("got %+v, want no_data/imported", got)
	}

	// 8 of 10 on one day (exactly 80%) is still the guard; 7 of 10 is not.
	spread := func(sameDayCount int) []PullRequest {
		prs := sameDay(sameDayCount)
		for i := sameDayCount; i < 10; i++ {
			prs = append(prs, pr("y", "merged", "a", 10+i*3, 1, true, nil))
		}
		return prs
	}
	if got := stats(&PullsExport{Available: true, PullRequests: spread(8)}); got.Reason != ReasonImported {
		t.Errorf("80%% same day: got %+v, want imported", got)
	}
	if got := stats(&PullsExport{Available: true, PullRequests: spread(7)}); got.Status != StatusOK {
		t.Errorf("70%% same day: got %+v, want ok", got)
	}

	// Below the minimum sample the guard does not apply (two PRs on one day is not an import).
	if got := stats(&PullsExport{Available: true, PullRequests: sameDay(2)}); got.Reason == ReasonImported {
		t.Errorf("2 PRs: got %+v, guard must not apply", got)
	}
}

func TestComputeIncludesReviewsAndVersion(t *testing.T) {
	result := Compute(testConfig(), Inputs{Pulls: &PullsExport{Available: true, PullRequests: []PullRequest{
		pr("1", "merged", "a", 1, 2, true, []string{"b"}), pr("2", "merged", "a", 4, 2, true, nil), pr("3", "merged", "a", 8, 2, true, nil),
	}}}, now)

	if result.Version != 4 || result.Reviews.Status != StatusOK || result.Reviews.Unreviewed != 2 {
		t.Fatalf("result = %+v", result)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Reviews struct {
			Status          string   `json:"status"`
			UnreviewedShare *float64 `json:"unreviewed_share"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(raw, &back); err != nil || back.Reviews.Status != "ok" || back.Reviews.UnreviewedShare == nil {
		t.Errorf("json = %s (err %v)", raw, err)
	}

	// Without pulls the rest of the analytics is unaffected and reviews is no_data.
	if no := Compute(testConfig(), Inputs{}, now); no.Reviews.Status != StatusNoData || no.Reviews.Reason != ReasonUnavailable {
		t.Errorf("no pulls: %+v", no.Reviews)
	}
}

// prReviewed builds a pull request created createdDaysAgo days ago whose first review, by "bob",
// came reviewAfterHours later; a negative value means it has no review.
func prReviewed(slug, status string, createdDaysAgo int, reviewAfterHours float64, activity bool) PullRequest {
	out := pr(slug, status, "alice", createdDaysAgo, 5, activity, nil)
	if reviewAfterHours >= 0 {
		var rv Reviewer
		rv.User.Slug = "bob"
		rv.Decision = "approve"
		rv.CreatedAt = out.CreatedAt.Add(time.Duration(reviewAfterHours * float64(time.Hour)))
		out.Reviewers = []Reviewer{rv}
	}
	return out
}

func TestBottlenecksMedianFirstReviewAndWaitingPullRequests(t *testing.T) {
	got := stats(&PullsExport{Available: true, PullRequests: []PullRequest{
		prReviewed("a", "merged", 20, 2, true),
		prReviewed("b", "merged", 18, 6, true),
		prReviewed("c", "merged", 16, 10, true),
		prReviewed("d", "open", 9, 26, true),   // open but already reviewed
		prReviewed("e", "open", 10, -1, true),  // open, no review for 10 days: waiting
		prReviewed("f", "open", 2, -1, true),   // open, no review but only 2 days: not waiting yet
		prReviewed("g", "draft", 30, -1, true), // drafts are not waiting for review
		prReviewed("h", "discarded", 25, -1, true),
		prReviewed("i", "merged", 12, 3, false), // reviewers and comments unreadable: not judged
	}}).Bottlenecks

	if got.WaitingDays != 7 {
		t.Errorf("waiting days = %d, want the configured 7 echoed", got.WaitingDays)
	}
	if got.Status != StatusOK || got.Reviewed != 4 || got.Open != 3 || got.Waiting != 1 {
		t.Fatalf("got %+v, want ok, 4 reviewed, 3 open, 1 waiting", got)
	}
	// Hours 2, 6, 10, 26 -> median (6+10)/2 = 8.
	if got.MedianFirstReviewHours == nil || *got.MedianFirstReviewHours != 8 {
		t.Errorf("median = %v, want 8", got.MedianFirstReviewHours)
	}
}

func TestBottlenecksNeedEnoughReviewedPullRequestsForAMedian(t *testing.T) {
	// Two reviewed pull requests and nothing open: no median and nothing else to report.
	got := stats(&PullsExport{Available: true, PullRequests: []PullRequest{
		prReviewed("a", "merged", 20, 2, true),
		prReviewed("b", "merged", 18, 6, true),
		prReviewed("c", "merged", 16, -1, true),
	}}).Bottlenecks
	if got.Status != StatusNoData || got.Reason != ReasonFewReviewed || got.MedianFirstReviewHours != nil || got.Reviewed != 2 {
		t.Errorf("got %+v, want no_data few_reviewed with no median", got)
	}

	// The same plus a waiting open pull request: still worth showing, just without the median.
	got = stats(&PullsExport{Available: true, PullRequests: []PullRequest{
		prReviewed("a", "merged", 20, 2, true),
		prReviewed("b", "merged", 18, 6, true),
		prReviewed("c", "open", 16, -1, true),
	}}).Bottlenecks
	if got.Status != StatusOK || got.MedianFirstReviewHours != nil || got.Open != 1 || got.Waiting != 1 {
		t.Errorf("got %+v, want ok with no median, 1 open, 1 waiting", got)
	}
}

func TestBottlenecksUnavailableAndImportedAreNoData(t *testing.T) {
	if got := stats(nil).Bottlenecks; got.Status != StatusNoData || got.Reason != ReasonUnavailable {
		t.Errorf("no export: got %+v", got)
	}
	if got := stats(&PullsExport{Available: false}).Bottlenecks; got.Reason != ReasonUnavailable {
		t.Errorf("export unavailable: got %+v", got)
	}

	same := func(n int) []PullRequest {
		var out []PullRequest
		for i := 0; i < n; i++ {
			out = append(out, prReviewed(string(rune('a'+i)), "open", 30, -1, true)) // all created the same day
		}
		return out
	}
	got := stats(&PullsExport{Available: true, PullRequests: same(5)}).Bottlenecks
	if got.Status != StatusNoData || got.Reason != ReasonImported || got.Waiting != 0 {
		t.Errorf("imported history: got %+v, want no_data imported and no waiting count", got)
	}
}

func TestBottlenecksIgnoreSelfBotAndUntimedReviews(t *testing.T) {
	selfReviewed := prReviewed("a", "open", 10, -1, true)
	var own Reviewer
	own.User.Slug = "alice" // the author approving their own pull request
	own.CreatedAt = selfReviewed.CreatedAt.Add(time.Hour)
	selfReviewed.Reviewers = []Reviewer{own}

	botReviewed := prReviewed("b", "open", 11, -1, true)
	botReviewed.Comments = []Comment{func() Comment {
		c := comment("ci-bot", "default", false, true)
		c.CreatedAt = botReviewed.CreatedAt.Add(time.Hour)
		return c
	}()}

	untimed := prReviewed("c", "open", 12, -1, true)
	var noTime Reviewer
	noTime.User.Slug = "bob" // a real review whose time is unknown
	untimed.Reviewers = []Reviewer{noTime}

	beforeCreation := prReviewed("d", "merged", 13, -5, true) // a review dated before the pull request

	got := stats(&PullsExport{Available: true, PullRequests: []PullRequest{selfReviewed, botReviewed, untimed, beforeCreation}}).Bottlenecks

	if got.Reviewed != 0 {
		t.Errorf("reviewed = %d, want 0: self, bot, untimed and pre-creation reviews give no first-review time", got.Reviewed)
	}
	if got.Open != 3 || got.Waiting != 2 {
		t.Errorf("open = %d waiting = %d, want 3 open and 2 waiting (self and bot do not count; the untimed review still does)", got.Open, got.Waiting)
	}
}
