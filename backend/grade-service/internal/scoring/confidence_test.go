package scoring

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

var confidenceNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func testConfidenceConfig() config.ConfidenceConfig {
	return config.ConfidenceConfig{
		Enabled: true, WindowDays: 180,
		MinFiles: 5, MinLOC: 200, MinCommits: 5, MinCommitsSingleAuthor: 20, MinCategoriesWithData: 2,
		Reasons: map[string]string{
			config.ReasonLowCode: "code", config.ReasonLowHistory: "history", config.ReasonLowCoverage: "coverage",
		},
	}
}

// commitsBy returns n commits, one per day going back from confidenceNow, authored round-robin by
// the given emails.
func commitsBy(n int, emails ...string) []gitlog.Commit {
	commits := make([]gitlog.Commit, n)
	for i := range commits {
		commits[i] = gitlog.Commit{
			Hash:        fmt.Sprintf("h%d", i),
			AuthorEmail: emails[i%len(emails)],
			AuthoredAt:  confidenceNow.AddDate(0, 0, -i-1),
		}
	}
	return commits
}

func healthyInputs() ConfidenceInputs {
	return ConfidenceInputs{
		CodeAvailable: true, ScannedFiles: 40, TotalLOC: 5000,
		CommitsAvailable: true, Commits: commitsBy(60, "a@x", "b@x", "c@x"),
	}
}

func fourCategories(noData ...string) (map[string]CategoryResult, []string) {
	ids := []string{"activity", "documentation", "issues", "code_health"}
	cats := map[string]CategoryResult{}
	for _, id := range ids {
		cats[id] = CategoryResult{ID: id, Score: 70}
	}
	for _, id := range noData {
		cats[id] = CategoryResult{ID: id, NoData: true}
	}
	return cats, ids
}

func reasonCodes(c *Confidence) []string {
	codes := []string{}
	for _, r := range c.Reasons {
		codes = append(codes, r.Code)
	}
	return codes
}

func TestConfidenceHealthyRepoIsNotFlagged(t *testing.T) {
	cats, ids := fourCategories()
	got := ComputeConfidence(testConfidenceConfig(), healthyInputs(), cats, ids, confidenceNow)
	if got == nil || got.LowConfidence || len(got.Reasons) != 0 || got.Reasons == nil {
		t.Fatalf("got %+v, want not flagged with an empty (non-nil) reasons list", got)
	}
}

func TestConfidenceDisabledReturnsNil(t *testing.T) {
	cfg := testConfidenceConfig()
	cfg.Enabled = false
	cats, ids := fourCategories()
	if got := ComputeConfidence(cfg, healthyInputs(), cats, ids, confidenceNow); got != nil {
		t.Errorf("got %+v, want nil when disabled", got)
	}
}

func TestConfidenceLittleCodeBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		files, loc  int
		wantFlagged bool
	}{
		{"exactly the floors", 5, 200, false},
		{"one file short", 4, 5000, true},
		{"one line short", 40, 199, true},
		{"empty repo", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := healthyInputs()
			in.ScannedFiles, in.TotalLOC = tc.files, tc.loc
			cats, ids := fourCategories()
			got := ComputeConfidence(testConfidenceConfig(), in, cats, ids, confidenceNow)
			if got.LowConfidence != tc.wantFlagged || (tc.wantFlagged && strings.Join(reasonCodes(got), ",") != config.ReasonLowCode) {
				t.Errorf("got %+v, want flagged=%v with only LOW_CODE", got, tc.wantFlagged)
			}
		})
	}
}

func TestConfidenceLittleHistory(t *testing.T) {
	cases := []struct {
		name        string
		commits     []gitlog.Commit
		wantFlagged bool
	}{
		{"exactly the commit floor, several authors", commitsBy(5, "a@x", "b@x"), false},
		{"one commit short", commitsBy(4, "a@x", "b@x"), true},
		{"solo author with a healthy history is fine", commitsBy(20, "a@x"), false},
		{"solo author just below the higher bar", commitsBy(19, "a@x"), true},
		{"author casing is one author", commitsBy(10, "A@x", "a@X"), true},
		{"no commits", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := healthyInputs()
			in.Commits = tc.commits
			cats, ids := fourCategories()
			got := ComputeConfidence(testConfidenceConfig(), in, cats, ids, confidenceNow)
			if got.LowConfidence != tc.wantFlagged || (tc.wantFlagged && strings.Join(reasonCodes(got), ",") != config.ReasonLowHistory) {
				t.Errorf("got %+v, want flagged=%v with only LOW_HISTORY", got, tc.wantFlagged)
			}
		})
	}
}

func TestConfidenceOnlyCountsCommitsInTheWindow(t *testing.T) {
	in := healthyInputs()
	old := commitsBy(60, "a@x", "b@x")
	for i := range old {
		old[i].AuthoredAt = confidenceNow.AddDate(0, 0, -200-i) // all outside the 180-day window
	}
	in.Commits = old
	cats, ids := fourCategories()
	if got := ComputeConfidence(testConfidenceConfig(), in, cats, ids, confidenceNow); !got.LowConfidence {
		t.Errorf("got %+v, want flagged: every commit is older than the window", got)
	}
}

func TestConfidenceLittleCoverage(t *testing.T) {
	cfg := testConfidenceConfig()

	cats, ids := fourCategories("documentation", "issues")
	got := ComputeConfidence(cfg, healthyInputs(), cats, ids, confidenceNow)
	if got.LowConfidence {
		t.Errorf("two categories with data: got %+v, want not flagged", got)
	}

	cats, ids = fourCategories("documentation", "issues", "code_health")
	got = ComputeConfidence(cfg, healthyInputs(), cats, ids, confidenceNow)
	if strings.Join(reasonCodes(got), ",") != config.ReasonLowCoverage {
		t.Errorf("one category with data: got %+v, want only LOW_COVERAGE", got)
	}

	// A category that was never computed counts as no data too.
	delete(cats, "activity")
	if got = ComputeConfidence(cfg, healthyInputs(), cats, ids, confidenceNow); strings.Join(reasonCodes(got), ",") != config.ReasonLowCoverage {
		t.Errorf("no categories with data: got %+v, want LOW_COVERAGE", got)
	}
}

// An unreadable export skips its signal: no data is not a defect (TZ §11.5).
func TestConfidenceUnavailableSourcesSkipTheirSignals(t *testing.T) {
	in := ConfidenceInputs{} // nothing readable
	cats, ids := fourCategories()
	got := ComputeConfidence(testConfidenceConfig(), in, cats, ids, confidenceNow)
	if got.LowConfidence {
		t.Errorf("got %+v, want not flagged: neither code nor commits were readable and coverage is fine", got)
	}
}

func TestConfidenceAllSignalsKeepStableOrderAndTexts(t *testing.T) {
	in := ConfidenceInputs{CodeAvailable: true, CommitsAvailable: true}
	cats, ids := fourCategories("documentation", "issues", "code_health")
	got := ComputeConfidence(testConfidenceConfig(), in, cats, ids, confidenceNow)

	want := []ConfidenceReason{
		{Code: config.ReasonLowCode, Text: "code"},
		{Code: config.ReasonLowHistory, Text: "history"},
		{Code: config.ReasonLowCoverage, Text: "coverage"},
	}
	if !got.LowConfidence || len(got.Reasons) != 3 {
		t.Fatalf("got %+v", got)
	}
	for i, r := range want {
		if got.Reasons[i] != r {
			t.Errorf("reason %d = %+v, want %+v", i, got.Reasons[i], r)
		}
	}
}
