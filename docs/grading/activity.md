# Activity (15%, public)

**Status: implemented** (2026-09-19). Last updated 2026-09-19.

## What it measures

How alive the project actually is right now: commit cadence, recency, contributor spread,
issue/milestone cadence, and bus-factor concentration. Purely behavioral signal — never
popularity (design principle 5 in `00-overview.md`).

## Data source

S3 `_meta/commits.json`, `_meta/issues.json`, `_meta/milestones.json` — written by
repository-service (`backend/repository-service/internal/service/clone.go`) from a real git clone
(commits) and sourcecraft-wrapper-service RPCs (issues, milestones). Each export is a full
per-event list with timestamps, never a pre-aggregated count (see `00-overview.md`'s time-decay
note for why that matters).

## Metrics

| Metric | Direction | Critical | Healthy* | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Commits/week | + | 0.5 | 5 | 0.20 | commits.json | `rate_per_week` |
| Days since last activity | − | 40 | 3 | 0.20 | commits.json | `days_since_last_event` |
| Unique contributors | + | 1 | 5 | 0.15 | commits.json | `unique_count(author_email)` |
| Issue rate/week | + | 0.2 | 2 | 0.15 | issues.json | `rate_per_week` |
| Milestones/year | + | 1 | 6 | 0.05 | milestones.json | `rate_per_year` |
| Days since last milestone | − | 180 | 30 | 0.10 | milestones.json | `days_since_last_event` |
| Top-contributor share | − | 1.0 | 0.5 | 0.10 | commits.json | `max_share(author_email)` |
| Net lines/week (band) | healthy [-2,10], critical [-30,50] | — | — | 0.05 | commits.json | `rate_per_week(net_lines)` |

*= marked `provisional: true` in `config/grading.yaml` — first-guess placeholders, not yet
validated against real repos.

## No-data vs. real-zero

- A source (`commits.json`/`issues.json`/`milestones.json`) that failed to fetch → every metric
  sourced from it is `NoData` for every one of its metrics.
- A source that fetched fine but is genuinely empty (e.g. a repo with zero milestones) → its
  rate-type metrics compute a real, valid zero — not `NoData`.
- **Top-contributor share** has its own floor: fewer than 2 commits in the window is
  statistically meaningless (a "share" of 1 commit is undefined, not just harsh) → `NoData`. A
  genuine 100% concentration with ≥2 commits (one person, many commits) still scores as real
  bus-factor risk — explicitly *not* excluded, rejected as a stricter rule during design.

## Known limitations

- All `healthy`/`critical` thresholds are provisional — first guesses, not validated against a
  real population of repos yet.
- "MR rate/week" in the original source draft had a prose/table contradiction (per-month vs.
  per-week) — resolved to per-week, matching the table.
- Releases (`_meta/releases.json`) were fetched but never found populated on any real repo tested
  — releases appear to be a largely-unused SourceCraft feature distinct from git tags; not
  currently wired into any metric.

## Open questions

- Threshold validation against real repos — tracked in `backlog/phase-2-grade-service.md`.
- No automated unit tests yet for `internal/scoring` — same gap for every category.
