# Issues (15%, public)

**Status: planned — not started.** Last updated 2026-09-22. See `backlog/phase-2-grade-service.md`
("Next task", item 1) for the concrete implementation task.

## What it measures

How well the project triages and closes work items: abandonment, responsiveness, throughput
growth, resolution rate. Window: last 6 months.

## Data source

S3 `_meta/issues.json` — already written by repository-service, already carries `status` and
`updated_at` alongside `created_at`. Grade service's local `Issue` struct
(`internal/scoring/activity.go`) currently only decodes `created_at` — extending it to also decode
`status`/`updated_at` is the only new data-plumbing this category needs; no new
repository-service/wrapper work.

## Metrics

Planned, not yet in `config/grading.yaml`. Event-stream shaped (same engine Activity uses —
`ComputeCategory`/`eventsForSource`), not a hand-written category like Security.

| Metric | Direction | Critical | Healthy | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| % abandoned (open, untouched 30d) | − | TBD | TBD | TBD | issues.json | new |
| Median closure time | − | TBD | TBD | TBD | issues.json | new |
| Growth rate (opened/closed, floored at 1) | − | TBD | TBD | TBD | issues.json | `rate_per_week`-style |
| Share resolved | + | TBD | TBD | TBD | issues.json | new |

## No-data vs. real-zero

- **0 issues total** → every metric above is statistically meaningless (same reasoning as
  Activity's `top_contributor_share` `min_sample_size` floor) → `NoData` for all of them, not a
  real 0%/100%.
- A repo *with* issues where, say, 0% are abandoned is a real, good, measured 0% — not `NoData`.

## Known limitations

- **Median closure time is a proxy**, not a real "time to close" — computed as `updated_at -
  created_at` on closed issues, because the wrapper exposes no first-comment/close-event
  timestamp separately from `updated_at`. Flag this limitation wherever the metric is surfaced to
  a user, not just here.
- "Median response time" (time to first response) from the original TZ wishlist (§3.1) has **no
  data source at all** through the current wrapper — dropped from the metric list above rather
  than faked. Revisit if sourcecraft-wrapper-service ever exposes issue comments/timeline.

## Open questions

- Exact critical/healthy thresholds — not chosen yet, needs real-repo calibration same as every
  other provisional threshold in this project.
- Whether "growth rate" should be a ratio (opened/closed) or two separate rate metrics — TZ's own
  wording is ambiguous here, needs a decision before implementation.
