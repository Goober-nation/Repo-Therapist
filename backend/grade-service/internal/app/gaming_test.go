package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/scoring"
)

// Adversarial scenarios (docs/grading/00-overview.md, "Устойчивость к накрутке"): cheap fake
// activity pushed through the real scoring with the shipped config/grading.yaml. The point is to
// measure how far each trick moves the score of a repo that has nothing real in it, and whether
// the low-confidence flag marks the result. The log table is what the docs quote.

var gamingNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func loadShippedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// fakeCommits returns perWeek one-line commits every week for weeks weeks, authored round-robin
// by the given emails.
func fakeCommits(weeks, perWeek int, emails ...string) []gitlog.Commit {
	return fakeCommitsOfSize(1, weeks, perWeek, emails...)
}

// fakeCommitsOfSize is fakeCommits where every commit changes the given number of lines.
func fakeCommitsOfSize(lines, weeks, perWeek int, emails ...string) []gitlog.Commit {
	var commits []gitlog.Commit
	for w := 0; w < weeks; w++ {
		for i := 0; i < perWeek; i++ {
			n := len(commits)
			commits = append(commits, gitlog.Commit{
				Hash:         fmt.Sprintf("c%d", n),
				AuthorName:   "dev",
				AuthorEmail:  emails[n%len(emails)],
				AuthoredAt:   gamingNow.AddDate(0, 0, -7*w-i),
				Parents:      []string{"p"},
				FilesChanged: []gitlog.FileChange{{Path: "README.md", Added: lines, Deleted: 0}},
			})
		}
	}
	return commits
}

// issueBurst creates n issues in one day, all already completed.
func issueBurst(n int, daysAgo int) []scoring.Issue {
	day := gamingNow.AddDate(0, 0, -daysAgo).Format(time.RFC3339)
	issues := make([]scoring.Issue, n)
	for i := range issues {
		issues[i] = scoring.Issue{CreatedAt: day, UpdatedAt: day, StatusType: "completed"}
	}
	return issues
}

func milestones(n int) []scoring.Milestone {
	out := make([]scoring.Milestone, n)
	for i := range out {
		day := gamingNow.AddDate(0, 0, -10*(i+1)).Format(time.RFC3339)
		out[i] = scoring.Milestone{ID: fmt.Sprintf("m%d", i), CreatedAt: day, Deadline: day}
	}
	return out
}

// delayedIssueBurst is issueBurst adapted to the instant-close rule: every issue stays open for
// closeAfter, and the first n of them are attached to the milestones m0..m(n-1) in turn.
func delayedIssueBurst(n, daysAgo int, closeAfter time.Duration, milestoneCount int) []scoring.Issue {
	day := gamingNow.AddDate(0, 0, -daysAgo)
	issues := make([]scoring.Issue, n)
	for i := range issues {
		issues[i] = scoring.Issue{
			CreatedAt:  day.Format(time.RFC3339),
			UpdatedAt:  day.Add(closeAfter).Format(time.RFC3339),
			StatusType: "completed",
		}
		if i < milestoneCount {
			issues[i].MilestoneID = fmt.Sprintf("m%d", i)
		}
	}
	return issues
}

type scenario struct {
	name   string
	inputs scoring.ActivityInputs
	// files/loc: the size of the (tiny) code base the trick lives in.
	files, loc int
}

type scenarioResult struct {
	name                      string
	activity, issues, overall float64
	activityData, issuesData  bool
	flagged                   bool
	reasons                   []string
}

func runScenario(t *testing.T, cfg *config.Config, s scenario) scenarioResult {
	t.Helper()

	activity := scoring.ComputeCategory("activity", cfg.Categories["activity"], s.inputs, gamingNow)
	issues := scoring.ComputeCategory("issues", cfg.Categories["issues"], s.inputs, gamingNow)
	codeHealth := scoring.ComputeCodeHealth(cfg.Categories["code_health"], scoring.CodeHealthInputs{Available: true, TotalLOC: s.loc, ScannedFiles: s.files})
	// A bare README earns a middling documentation grade; the same in every scenario.
	docs := scoring.CategoryResult{ID: "documentation", Score: 30}

	categories := map[string]scoring.CategoryResult{"activity": activity, "issues": issues, "code_health": codeHealth, "documentation": docs}
	ids := publicCategoryIDs
	weights := map[string]float64{}
	for id, c := range cfg.Categories {
		weights[id] = c.Weight
	}
	overall, noData := scoring.CombineCategories(categories, ids, weights)
	if noData {
		t.Fatalf("%s: no overall score", s.name)
	}

	confidenceInputs := scoring.ConfidenceInputs{CodeAvailable: true, ScannedFiles: s.files, TotalLOC: s.loc}
	if s.inputs.CommitsAvailable {
		confidenceInputs.CommitsAvailable = true
		confidenceInputs.Commits = scoring.FilterCommits(s.inputs.Commits, cfg.Categories["activity"].CommitFilters, gamingNow)
	}
	confidence := scoring.ComputeConfidence(cfg.Confidence, confidenceInputs, categories, ids, gamingNow)

	result := scenarioResult{
		name: s.name, activity: activity.Score, issues: issues.Score, overall: overall,
		activityData: !activity.NoData, issuesData: !issues.NoData, flagged: confidence.LowConfidence,
	}
	for _, r := range confidence.Reasons {
		result.reasons = append(result.reasons, r.Code)
	}
	return result
}

func gamingScenarios() []scenario {
	// The honest baseline: a README-only repo with one real commit, no issues, no milestones.
	empty := scoring.ActivityInputs{
		Commits: fakeCommitsOfSize(10, 1, 1, "owner@x"), CommitsAvailable: true, IssuesAvailable: true, MilestonesAvailable: true,
	}
	with := func(f func(in *scoring.ActivityInputs)) scoring.ActivityInputs {
		in := empty
		f(&in)
		return in
	}

	return []scenario{
		{name: "empty shell (baseline)", inputs: empty, files: 3, loc: 150},
		{name: "trivial commits: 5/week, 1 account", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Commits = append(in.Commits, fakeCommits(26, 5, "owner@x")...)
		})},
		{name: "sockpuppets: 5/week, 5 made-up emails", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Commits = append(in.Commits, fakeCommits(26, 5, "a@x", "b@x", "c@x", "d@x", "e@x")...)
		})},
		{name: "issue burst: 100 closed in one day", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Issues = issueBurst(100, 5)
		})},
		{name: "milestone spam: 3 empty milestones", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Milestones = milestones(3)
		})},
		{name: "all tricks together", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Commits = append(in.Commits, fakeCommits(26, 5, "a@x", "b@x", "c@x", "d@x", "e@x")...)
			in.Issues = issueBurst(100, 5)
			in.Milestones = milestones(3)
		})},
		{name: "adapted: 4-line commits, 5 emails, issues closed after 1 min, milestones with an issue", files: 3, loc: 150, inputs: with(func(in *scoring.ActivityInputs) {
			in.Commits = append(in.Commits, fakeCommitsOfSize(4, 26, 5, "a@x", "b@x", "c@x", "d@x", "e@x")...)
			in.Issues = delayedIssueBurst(100, 5, time.Minute, 3)
			in.Milestones = milestones(3)
		})},
		{name: "all tricks + padded code (5 files, 200 lines)", files: 5, loc: 200, inputs: with(func(in *scoring.ActivityInputs) {
			in.Commits = append(in.Commits, fakeCommits(26, 5, "a@x", "b@x", "c@x", "d@x", "e@x")...)
			in.Issues = issueBurst(100, 5)
			in.Milestones = milestones(3)
		})},
	}
}

// cell renders a category score, or "нет данных" when the category was excluded from the overall.
func cell(score float64, hasData bool) string {
	if !hasData {
		return "нет данных"
	}
	return fmt.Sprintf("%.0f", score)
}

// TestGamingScenariosTable pins the measured effect of each trick on a repo with nothing real in
// it. These are characterization numbers, not goals: they document how gameable the score is
// today (docs/grading/00-overview.md, "Устойчивость к накрутке"). A hardening change that moves
// them must update this table and that section together.
func TestGamingScenariosTable(t *testing.T) {
	cfg := loadShippedConfig(t)

	want := map[string]struct {
		activity, issues, overall float64 // issues < 0: no data
		flagged                   bool
	}{
		"empty shell (baseline)":                {25, -1, 43, true},
		"trivial commits: 5/week, 1 account":    {25, -1, 43, true},
		"sockpuppets: 5/week, 5 made-up emails": {25, -1, 43, true},
		"issue burst: 100 closed in one day":    {25, -1, 43, true},
		"milestone spam: 3 empty milestones":    {25, -1, 43, true},
		"all tricks together":                   {25, -1, 43, true},
		"adapted: 4-line commits, 5 emails, issues closed after 1 min, milestones with an issue": {100, 100, 84, true},
		"all tricks + padded code (5 files, 200 lines)":                                          {25, -1, 43, true},
	}

	var table strings.Builder
	table.WriteString("\n| Сценарий | Активность | Issues | Итог | Низкая достоверность |\n|---|---|---|---|---|\n")
	for _, s := range gamingScenarios() {
		r := runScenario(t, cfg, s)
		fmt.Fprintf(&table, "| %s | %s | %s | %.0f | %v %v |\n", r.name, cell(r.activity, r.activityData), cell(r.issues, r.issuesData), r.overall, r.flagged, r.reasons)

		w, ok := want[s.name]
		if !ok {
			t.Errorf("no expectation for scenario %q", s.name)
			continue
		}
		if abs(r.activity-w.activity) > 1 || abs(r.overall-w.overall) > 1 || r.flagged != w.flagged || (w.issues < 0) == r.issuesData ||
			(w.issues >= 0 && abs(r.issues-w.issues) > 1) {
			t.Errorf("%s: activity %.0f issues %s overall %.0f flagged %v; want activity %.0f issues %v overall %.0f flagged %v",
				s.name, r.activity, cell(r.issues, r.issuesData), r.overall, r.flagged, w.activity, w.issues, w.overall, w.flagged)
		}
	}
	t.Log(table.String())
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
