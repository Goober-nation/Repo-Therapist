package scoring

import (
	"time"

	"repo-therapist/grade-service/internal/config"
)

// isClosed reports whether an issue's lifecycle status is terminal; status_type, not the
// repo-customizable status slug, is what means "closed" across repos.
func isClosed(i Issue) bool {
	return i.StatusType == "completed" || i.StatusType == "cancelled"
}

// FilterIssues drops closed issues whose last update came within InstantCloseSeconds of their
// creation (docs/grading/00-overview.md): an issue opened and closed in one moment is a tracker
// entry made for the count, and would otherwise score a perfect closure time. Issues with
// unparseable timestamps are kept. The input slice is not modified.
func FilterIssues(issues []Issue, f config.IssueFilters) []Issue {
	if f.InstantCloseSeconds <= 0 {
		return issues
	}

	limit := time.Duration(f.InstantCloseSeconds) * time.Second
	kept := make([]Issue, 0, len(issues))
	for _, i := range issues {
		if isClosed(i) {
			created, errCreated := time.Parse(time.RFC3339Nano, i.CreatedAt)
			updated, errUpdated := time.Parse(time.RFC3339Nano, i.UpdatedAt)
			if errCreated == nil && errUpdated == nil && updated.Sub(created) <= limit {
				continue
			}
		}
		kept = append(kept, i)
	}

	return kept
}

// FilterMilestones keeps only milestones that an issue is attached to, when MilestonesNeedIssues
// is on. issues must already be filtered, so an instantly closed issue cannot vouch for a
// milestone. It cannot judge without the issue list (issuesAvailable false), and a milestone
// without an id (legacy export) is kept: neither case is a defect of the repo. The input slice is
// not modified.
func FilterMilestones(milestones []Milestone, issues []Issue, issuesAvailable bool, f config.IssueFilters) []Milestone {
	if !f.MilestonesNeedIssues || !issuesAvailable {
		return milestones
	}

	attached := make(map[string]struct{}, len(issues))
	for _, i := range issues {
		if i.MilestoneID != "" {
			attached[i.MilestoneID] = struct{}{}
		}
	}

	kept := make([]Milestone, 0, len(milestones))
	for _, m := range milestones {
		if _, ok := attached[m.ID]; ok || m.ID == "" {
			kept = append(kept, m)
		}
	}

	return kept
}
