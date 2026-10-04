package scoring

import (
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
)

func issueAt(created time.Time, closedAfter time.Duration, status, milestone string) Issue {
	return Issue{
		CreatedAt:   created.Format(time.RFC3339Nano),
		UpdatedAt:   created.Add(closedAfter).Format(time.RFC3339Nano),
		StatusType:  status,
		MilestoneID: milestone,
	}
}

func TestFilterIssuesDropsOnlyInstantlyClosedIssues(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	issues := []Issue{
		issueAt(now, 0, "completed", ""),                                // opened and closed in the same moment
		issueAt(now, 60*time.Second, "cancelled", ""),                   // exactly at the limit
		issueAt(now, 61*time.Second, "completed", ""),                   // just over it
		issueAt(now, 0, "initial", ""),                                  // still open: updated_at == created_at is normal
		issueAt(now, 0, "in_progress", ""),                              // open as well
		issueAt(now, 3*24*time.Hour, "completed", ""),                   // an honest closure
		{CreatedAt: "garbage", UpdatedAt: "x", StatusType: "completed"}, // unparseable: kept
	}

	got := FilterIssues(issues, config.IssueFilters{InstantCloseSeconds: 60})
	if len(got) != 5 {
		t.Fatalf("kept %d issues, want 5: %+v", len(got), got)
	}
	if got[0].StatusType != "completed" || got[0].UpdatedAt == got[0].CreatedAt {
		t.Errorf("first kept issue = %+v, want the 61-second closure", got[0])
	}

	if off := FilterIssues(issues, config.IssueFilters{}); len(off) != len(issues) {
		t.Errorf("filter off kept %d of %d", len(off), len(issues))
	}
}

func TestFilterMilestonesNeedAnAttachedIssue(t *testing.T) {
	milestones := []Milestone{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}, {ID: ""}}
	issues := []Issue{{MilestoneID: "m1"}, {MilestoneID: "m1"}, {MilestoneID: "m3"}, {}}
	on := config.IssueFilters{MilestonesNeedIssues: true}

	got := FilterMilestones(milestones, issues, true, on)
	ids := []string{}
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if len(ids) != 3 || ids[0] != "m1" || ids[1] != "m3" || ids[2] != "" {
		t.Errorf("kept %v, want m1, m3 and the legacy id-less milestone", ids)
	}

	if got := FilterMilestones(milestones, nil, true, on); len(got) != 1 {
		t.Errorf("no issues at all kept %d milestones, want only the id-less one", len(got))
	}

	// Without the issue list there is nothing to judge by: a defect of the data, not of the repo.
	if got := FilterMilestones(milestones, nil, false, on); len(got) != 4 {
		t.Errorf("issues unavailable kept %d milestones, want all 4", len(got))
	}
	if got := FilterMilestones(milestones, issues, true, config.IssueFilters{}); len(got) != 4 {
		t.Errorf("filter off kept %d milestones, want all 4", len(got))
	}
}

// An instantly closed issue is dropped before milestones are checked, so it cannot make an
// otherwise empty milestone look real.
func TestComputeCategoryFiltersMilestonesAfterInstantIssues(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cat := config.CategoryConfig{
		Status:       config.StatusCalculated,
		IssueFilters: config.IssueFilters{InstantCloseSeconds: 60, MilestonesNeedIssues: true},
		Metrics: []config.MetricConfig{{
			ID: "milestones_per_year", Status: config.StatusCalculated, Weight: 1, Source: "milestones.json",
			Aggregation: config.AggregationConfig{Type: "rate_per_year", WindowDays: 365},
			Formula:     config.FormulaConfig{Type: "linear_clamp", Direction: "+", Critical: 0, Healthy: 3},
		}},
	}
	day := now.AddDate(0, 0, -10).Format(time.RFC3339)
	inputs := ActivityInputs{
		IssuesAvailable: true, MilestonesAvailable: true,
		Milestones: []Milestone{{ID: "m1", CreatedAt: day, Deadline: day}},
		Issues:     []Issue{issueAt(now.AddDate(0, 0, -10), 0, "completed", "m1")},
	}

	if got := ComputeCategory("activity", cat, inputs, now); got.Metrics[0].RawValue != 0 {
		t.Errorf("milestone vouched for by an instantly closed issue counted: raw = %v", got.Metrics[0].RawValue)
	}

	inputs.Issues = []Issue{issueAt(now.AddDate(0, 0, -10), 48*time.Hour, "completed", "m1")}
	if got := ComputeCategory("activity", cat, inputs, now); got.Metrics[0].RawValue == 0 {
		t.Error("milestone with a real attached issue was dropped")
	}
}
