package scoring

import (
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/shared/gitlog"
	"repo-therapist/shared/sourcecraft"
)

// ActivityInputs bundles the three S3 _meta sources Activity draws from, plus whether each one
// was actually fetched successfully. That last part matters: a source that failed to fetch means
// every metric sourced from it is NoData; a source that fetched fine but is empty (e.g. a repo
// with genuinely zero releases) means its rate-type metrics compute a real, valid zero instead —
// see aggregation.go's ratePerWindow comment for why that distinction isn't optional.
type ActivityInputs struct {
	Commits           []gitlog.Commit
	CommitsAvailable  bool
	Pulls             []sourcecraft.PullRequest
	PullsAvailable    bool
	Releases          []sourcecraft.Release
	ReleasesAvailable bool
}

// ComputeCategory runs every calculated metric in a category config against the given inputs and
// combines them. Currently only Activity's sources (commits/pulls/releases) are wired up via
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
	case "pulls.json":
		if !inputs.PullsAvailable {
			return nil, false
		}
		return pullsToEvents(inputs.Pulls), true
	case "releases.json":
		if !inputs.ReleasesAvailable {
			return nil, false
		}
		return releasesToEvents(inputs.Releases), true
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

func pullsToEvents(pulls []sourcecraft.PullRequest) []Event {
	events := make([]Event, 0, len(pulls))
	for _, p := range pulls {
		ts, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
		if err != nil {
			continue // skip unparseable timestamps rather than fail the whole metric
		}
		events = append(events, Event{Timestamp: ts})
	}
	return events
}

func releasesToEvents(releases []sourcecraft.Release) []Event {
	events := make([]Event, 0, len(releases))
	for _, r := range releases {
		raw := r.ReleasedAt
		if raw == "" {
			raw = r.CreatedAt
		}
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		events = append(events, Event{Timestamp: ts})
	}
	return events
}
