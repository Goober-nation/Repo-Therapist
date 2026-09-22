package scoring

import (
	"sort"
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
	case "abandoned_share":
		return abandonedShare(cfg, events, now)
	case "median_closure_days":
		return medianClosureDays(cfg, events, now)
	case "growth_ratio":
		return growthRatio(cfg, events, now)
	case "share_resolved":
		return shareResolved(cfg, events, now)
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

// windowFilterByUpdated is windowFilter's counterpart keyed off Event.NumericFields
// ["updated_at_unix"] instead of Event.Timestamp (creation time). Needed wherever "did this
// happen recently" means "was it last touched recently," not "was it created recently" — see
// docs/grading/issues.md's growth_ratio/share_resolved redefinition (2026-09-22) for why the two
// aren't interchangeable for issue events. Events without the field (only issues carry it) are
// excluded rather than silently included with an undefined age.
func windowFilterByUpdated(events []Event, now time.Time, windowDays int) []Event {
	cutoff := now.AddDate(0, 0, -windowDays)
	var out []Event
	for _, e := range events {
		updatedUnix, ok := e.NumericFields["updated_at_unix"]
		if !ok {
			continue
		}
		if !time.Unix(int64(updatedUnix), 0).Before(cutoff) {
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

// isResolved reads the fixed backend lifecycle enum (StatusType, populated by
// scoring.issuesToEvents into Event.StringFields["status_type"]) — never the per-repo
// customizable status slug/name, which isn't guaranteed consistent across repos/orgs. See
// docs/grading/issues.md for the enum values (initial/in_progress/paused/completed/cancelled)
// and why status_type was chosen over slug matching.
func isResolved(e Event) bool {
	switch e.StringFields["status_type"] {
	case "completed", "cancelled":
		return true
	default:
		return false
	}
}

// abandonedShare: fraction of *all currently-open* issues (regardless of when they were
// created) that are untouched for at least cfg.StalenessDays. Deliberately NOT windowed by
// creation date (cfg.WindowDays is unused here) — an earlier version was, and that made the
// single worst case (an issue opened years ago and never touched since) invisible, since it
// ages out of a creation-date window. External research (2026-09-22, see
// docs/grading/issues.md) confirmed every mature framework we found (CHAOSS's Issue Age metric,
// GitHub's own actions/stale, probot/stale) measures staleness the same way: over the whole
// open-issue population, keyed off last-activity date, never off creation date.
//
// Below cfg.MinSampleSize currently-open issues → no-data, same small-sample floor as
// Activity's top_contributor_share (maxShare) — a ratio over 1-4 issues swings by tens of
// percentage points on a single event, which is exactly the "secondary signal shouldn't
// dominate the score" failure TZ §13.2 tests for.
func abandonedShare(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	var open []Event
	for _, e := range events {
		if !isResolved(e) {
			open = append(open, e)
		}
	}
	if len(open) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}

	var abandoned int
	for _, e := range open {
		updatedUnix, ok := e.NumericFields["updated_at_unix"]
		if !ok {
			continue
		}
		daysSinceUpdate := now.Sub(time.Unix(int64(updatedUnix), 0)).Hours() / 24
		if daysSinceUpdate >= float64(cfg.StalenessDays) {
			abandoned++
		}
	}
	return aggResult{Value: float64(abandoned) / float64(len(open))}
}

// medianClosureDays: median (updated_at - created_at) over windowed issues that are resolved —
// updated_at is a proxy for close time, not a real close-event timestamp (the wrapper exposes
// no first-comment/close-event timeline separately from updated_at), flagged in
// docs/grading/issues.md. The sample-size floor applies to the *resolved* count, not the
// windowed count — a median over 1-2 closures is barely a median, same reasoning as
// abandonedShare's floor (TZ §13.2, see that function's comment).
func medianClosureDays(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	windowed := windowFilter(events, now, cfg.WindowDays)

	var durations []float64
	for _, e := range windowed {
		if !isResolved(e) {
			continue
		}
		updatedUnix, ok := e.NumericFields["updated_at_unix"]
		if !ok {
			continue
		}
		days := (updatedUnix - float64(e.Timestamp.Unix())) / 86400
		if days < 0 {
			days = 0 // guard against clock skew rather than let bad data go negative
		}
		durations = append(durations, days)
	}
	if len(durations) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	return aggResult{Value: median(durations)}
}

// closedInWindow: resolved issues whose updated_at (a proxy for close time) falls within
// cfg.WindowDays, regardless of when they were created — deliberately decoupled from
// openedInWindow's creation-date filter. An issue opened long ago and finally closed this
// window counts as real closure throughput; the earlier version missed this entirely because it
// only ever looked at issues created within the same window as their closure.
func closedInWindow(cfg config.AggregationConfig, events []Event, now time.Time) []Event {
	var closed []Event
	for _, e := range windowFilterByUpdated(events, now, cfg.WindowDays) {
		if isResolved(e) {
			closed = append(closed, e)
		}
	}
	return closed
}

// growthRatio: issues opened in the window (created_at) divided by issues closed in the window
// (updated_at, resolved), closed floored at 1 so a repo with real opens but zero closes in the
// window doesn't divide by zero — that's a real, valid (harsh) unbounded-growth signal, not
// no-data. Opened and closed are counted as two independent windowed event tallies rather than
// tying closure back to a shared creation cohort — this is the CHAOSS "Issues New"/"Issues
// Closed" pattern (external research, 2026-09-22, see docs/grading/issues.md), and fixes the
// earlier version's blind spot: closing a long-lived backlog issue used to not count as
// "closed" at all if it wasn't also created in the same window.
//
// Below cfg.MinSampleSize total windowed events (opened + closed) → no-data — a ratio built
// from a handful of total events is exactly as fragile regardless of which side dominates it.
func growthRatio(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	opened := windowFilter(events, now, cfg.WindowDays)
	closed := closedInWindow(cfg, events, now)
	if len(opened)+len(closed) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}

	denom := len(closed)
	if denom < 1 {
		denom = 1
	}
	return aggResult{Value: float64(len(opened)) / float64(denom)}
}

// shareResolved: issues closed in the window (updated_at, resolved) as a fraction of the total
// live-plus-recently-resolved backlog — closedInWindow(updated_at) over (currently-open count +
// closedInWindow). "Currently open" and "resolved" are mutually exclusive by construction (an
// issue's current status_type is one or the other), so this needs no per-issue deduplication to
// avoid double-counting, unlike a true set-union would. Reads as "resolution throughput against
// everything that's either still outstanding or was just cleared" — replaces the earlier
// creation-windowed version, which only measured what fraction of a *recent* cohort got
// resolved and ignored resolution of older backlog entirely (external research, 2026-09-22, see
// docs/grading/issues.md).
//
// Below cfg.MinSampleSize (open + closed-in-window) → no-data, same reasoning as growthRatio.
func shareResolved(cfg config.AggregationConfig, events []Event, now time.Time) aggResult {
	var openNow int
	for _, e := range events {
		if !isResolved(e) {
			openNow++
		}
	}
	closed := closedInWindow(cfg, events, now)

	denom := openNow + len(closed)
	if denom < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	return aggResult{Value: float64(len(closed)) / float64(denom)}
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}
