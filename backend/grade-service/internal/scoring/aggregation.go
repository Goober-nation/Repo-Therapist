package scoring

import (
	"time"

	"repo-therapist/grade-service/internal/config"
)

// Event is the uniform shape every source (commits, pulls, releases, ...) converts into, so
// aggregation logic doesn't need to know which source it came from. Only the fields a given
// aggregation actually reads need to be populated.
type Event struct {
	Timestamp     time.Time
	NumericFields map[string]float64
	StringFields  map[string]string
}

// aggResult separates "we computed a real value" from "we could not compute one" — the second
// case is what feeds combine.go's renormalization. Important distinction (see comments below):
// a genuinely-measured zero (e.g. 0 commits in the window, source data fetched fine) is a real,
// valid, possibly-harsh value — NOT the same as "no data." TZ §11 rule 5 says absence of data
// must not be auto-treated as bad; it does not say a real zero shouldn't be scored as bad.
type aggResult struct {
	Value  float64
	NoData bool
}

func aggregate(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	switch cfg.Type {
	case "rate_per_week":
		return ratePerWindow(cfg, events, now, 7)
	case "rate_per_year":
		return ratePerWindow(cfg, events, now, 365)
	case "days_since_last_event":
		return daysSinceLastEvent(events)
	case "unique_count":
		return uniqueCount(cfg, events, now)
	case "max_share":
		return maxShare(cfg, events, now)
	default:
		return aggResult{NoData: true}
	}
}

func windowFilter(events []Event, now time.Time, windowDays int) []Event {
	if windowDays <= 0 {
		return events
	}
	cutoff := now.AddDate(0, 0, -windowDays)
	var out []Event
	for _, e := range events {
		if !e.Timestamp.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

// ratePerWindow always returns a real value, never no-data: a window with zero qualifying events
// is a valid rate of 0 (the repo really did have no activity), not a data-availability problem —
// as long as the source itself fetched successfully, which the caller already guarantees before
// building events at all.
func ratePerWindow(cfg config.AggregationConfig, events []Event, now time.Time, periodDays float64) aggResult {
	windowed := windowFilter(events, now, cfg.WindowDays)
	periods := float64(cfg.WindowDays) / periodDays
	if periods <= 0 {
		return aggResult{NoData: true}
	}

	var total float64
	if cfg.Field == "" {
		total = float64(len(windowed))
	} else {
		for _, e := range windowed {
			total += e.NumericFields[cfg.Field]
		}
	}
	return aggResult{Value: total / periods}
}

// daysSinceLastEvent needs at least one event ever to be defined. An empty event list is
// genuinely ambiguous (brand-new repo vs. repo older than our lookback window with no recent
// activity) — treated as no-data rather than guessing, a deliberate simplification flagged in
// backlog/phase-2-grade-service.md.
func daysSinceLastEvent(events []Event) aggResult {
	if len(events) == 0 {
		return aggResult{NoData: true}
	}
	latest := events[0].Timestamp
	for _, e := range events[1:] {
		if e.Timestamp.After(latest) {
			latest = e.Timestamp
		}
	}
	return aggResult{Value: time.Since(latest).Hours() / 24}
}

func uniqueCount(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	windowed := windowFilter(events, now, cfg.WindowDays)
	seen := make(map[string]struct{})
	for _, e := range windowed {
		if v, ok := e.StringFields[cfg.Field]; ok {
			seen[v] = struct{}{}
		}
	}
	return aggResult{Value: float64(len(seen))}
}

// maxShare: the top field-value's share of all events in the window. Below min_sample_size,
// the metric is statistically meaningless (e.g. 1 contributor = 100% by construction for any
// solo/new repo) — deliberately no-data, not a real measurement, per docs/grading/activity.md's
// Activity table note.
func maxShare(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	windowed := windowFilter(events, now, cfg.WindowDays)
	if len(windowed) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}

	counts := make(map[string]int)
	for _, e := range windowed {
		if v, ok := e.StringFields[cfg.Field]; ok {
			counts[v]++
		}
	}
	// Note: a single distinct value with count == len(windowed) (one person made every commit)
	// scores as real, deliberate 100% concentration here — that's genuine bus-factor signal, not
	// a data artifact. Only the raw event-count floor (min_sample_size, config-driven) excludes
	// this metric; a separate "min distinct contributors" gate was considered and rejected —
	// would hide real bus-factor risk on mature-but-solo projects, not just protect new ones.

	var max int
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	return aggResult{Value: float64(max) / float64(len(windowed))}
}
