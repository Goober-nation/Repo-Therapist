package scoring

import (
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/gitlog"
)

// Issue is the subset of sourcecraft-wrapper-service's Issue data Activity's issue-rate metric
// and Issues' metrics need — matches the shape repository-service writes to _meta/issues.json.
// StatusType is the fixed backend lifecycle enum (initial/in_progress/paused/completed/
// cancelled), not the per-repo customizable Status slug — see
// backend/repository-service/internal/scwrapper/client.go's Issue struct for why.
type Issue struct {
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	StatusType string `json:"status_type"`
}

// Milestone is the subset of sourcecraft-wrapper-service's Milestone data Activity's
// milestone-cadence metrics need — matches the shape repository-service writes to
// _meta/milestones.json.
type Milestone struct {
	Deadline  string `json:"deadline"`
	CreatedAt string `json:"created_at"`
}

// ActivityInputs bundles the three S3 _meta sources Activity draws from, plus whether each one
// was actually fetched successfully. That last part matters: a source that failed to fetch means
// every metric sourced from it is NoData; a source that fetched fine but is empty (e.g. a repo
// with genuinely zero milestones) means its rate-type metrics compute a real, valid zero instead —
// see aggregation.go's ratePerWindow comment for why that distinction isn't optional.
type ActivityInputs struct {
	Commits             []gitlog.Commit
	CommitsAvailable    bool
	Issues              []Issue
	IssuesAvailable     bool
	Milestones          []Milestone
	MilestonesAvailable bool
}

// ComputeCategory runs every calculated metric in a category config against the given inputs and
// combines them. Currently only Activity's sources (commits/issues/milestones) are wired up via
// eventsForSource — categories whose sources aren't implemented yet report those metrics NoData,
// which is the correct behavior (not an error) until they're built.
func ComputeCategory(categoryID string, cfg config.CategoryConfig, inputs ActivityInputs, now time.Time) CategoryResult {
	if cfg.Status != config.StatusCalculated {
		return CategoryResult{ID: categoryID, NoData: true}
	}

	results := make([]MetricResult, 0, len(cfg.Metrics))
	for _, m := range cfg.Metrics {
		results = append(results, computeMetric(m, inputs, now))
	}

	score, noData := combineWeighted(results)
	return CategoryResult{ID: categoryID, Score: score, NoData: noData, Metrics: results}
}

func computeMetric(m config.MetricConfig, inputs ActivityInputs, now time.Time) MetricResult {
	base := MetricResult{ID: m.ID, Weight: m.Weight}
	if m.Status != config.StatusCalculated {
		base.NoData = true
		return base
	}

	events, available := eventsForSource(m.Source, inputs)
	if !available {
		base.NoData = true
		return base
	}

	agg := aggregate(m.Aggregation, events, now)
	if agg.NoData {
		base.NoData = true
		return base
	}

	base.RawValue = agg.Value
	base.Score = applyFormula(m.Formula, agg.Value)
	return base
}

func eventsForSource(source string, inputs ActivityInputs) ([]Event, bool) {
	switch source {
	case "commits.json":
		if !inputs.CommitsAvailable {
			return nil, false
		}
		return commitsToEvents(inputs.Commits), true
	case "issues.json":
		if !inputs.IssuesAvailable {
			return nil, false
		}
		return issuesToEvents(inputs.Issues), true
	case "milestones.json":
		if !inputs.MilestonesAvailable {
			return nil, false
		}
		return milestonesToEvents(inputs.Milestones), true
	default:
		return nil, false
	}
}

func commitsToEvents(commits []gitlog.Commit) []Event {
	events := make([]Event, 0, len(commits))
	for _, c := range commits {
		var netLines float64
		for _, fc := range c.FilesChanged {
			if fc.Binary {
				continue
			}
			netLines += float64(fc.Added - fc.Deleted)
		}
		events = append(events, Event{
			Timestamp:     c.AuthoredAt,
			NumericFields: map[string]float64{"net_lines": netLines},
			StringFields:  map[string]string{"author_email": c.AuthorEmail},
		})
	}
	return events
}

func issuesToEvents(issues []Issue) []Event {
	events := make([]Event, 0, len(issues))
	for _, i := range issues {
		ts, err := time.Parse(time.RFC3339Nano, i.CreatedAt)
		if err != nil {
			continue // skip unparseable timestamps rather than fail the whole metric
		}
		e := Event{Timestamp: ts, StringFields: map[string]string{"status_type": i.StatusType}}
		if updatedAt, err := time.Parse(time.RFC3339Nano, i.UpdatedAt); err == nil {
			e.NumericFields = map[string]float64{"updated_at_unix": float64(updatedAt.Unix())}
		}
		events = append(events, e)
	}
	return events
}

// isUnsetDeadline reports whether raw is empty or the platform's sentinel for "no deadline set"
// — SourceCraft's API returns Go's zero-value time ("0001-01-01T00:00:00Z") rather than omitting
// the field or nulling it, confirmed live (2026-09-22) against yapb/yapb's real milestones, all
// of which carry this exact value despite having real, distinct created_at/updated_at dates. A
// naive raw=="" check misses this and lets a "no deadline" milestone parse as a real ~2000-year-
// old deadline, producing nonsense like days_since_last_milestone > 100000.
func isUnsetDeadline(raw string) bool {
	if raw == "" {
		return true
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && ts.Year() <= 1
}

func milestonesToEvents(milestones []Milestone) []Event {
	events := make([]Event, 0, len(milestones))
	for _, m := range milestones {
		raw := m.Deadline
		if isUnsetDeadline(raw) {
			raw = m.CreatedAt
		}
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		events = append(events, Event{Timestamp: ts})
	}
	return events
}
