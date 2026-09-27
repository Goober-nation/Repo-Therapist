# Activity (20%, public)

**Status: implemented** (2026-09-19). Last updated 2026-09-22.

## What it measures

How alive the project actually is right now: commit cadence, recency, contributor spread,
issue/milestone cadence, and bus-factor concentration. Purely behavioral signal — never
popularity (design principle 5 in `00-overview.md`).

## How it's calculated, in plain words

1. Repository-service clones the repo and asks SourceCraft for its issues and milestones, saving
   each as one entry per event (commit, issue, milestone) with a timestamp, in S3.
2. Before counting anything, grade-service drops commits that don't reflect real work — each rule
   toggled in `config/grading.yaml`'s `activity.commit_filters` (defaults in brackets):
   - authored in the future — a wrong clock or a forged date [on];
   - empty — a non-merge commit that changed no files [on];
   - by a bot — author name or email name part ends in `[bot]`, `-bot` or `_bot` [on];
   - merges — commits with two or more parents [off: turning it on shifts every repo's counts].
3. Grade-service turns the remaining commits, issues and milestones into seven numbers: how often commits happen, how long since the
   last one, how many different people are committing, how often issues/milestones show up, how
   long since the last milestone, and how concentrated commits are in one person.
4. Each number gets placed on a line between a "healthy" value and a "critical" value — close to
   healthy scores near 1, close to critical scores near 0.
5. If a metric's underlying data couldn't be fetched at all, or (for top-contributor share) there
   were too few commits to compute a meaningful share, that metric is dropped — "no data," not a
   fabricated 0.
6. The seven scores are combined into one 0-100 category score by weighted average, stretching
   remaining weights to fill any dropped metric's gap.

## Data source

S3 `_meta/commits.json`, `_meta/issues.json`, `_meta/milestones.json` — written by
repository-service (`backend/repository-service/internal/service/clone.go`) from a real git clone
(commits) and sourcecraft-wrapper-service RPCs (issues, milestones). Each export is a full
per-event list with timestamps, never a pre-aggregated count (see `00-overview.md`'s time-decay
note for why that matters).

### Commit filters (2026-09-27)

`commits.json` carries each commit's `parents` (from `git log %P`): `[]` for a root commit, two or
more for a merge. `git log --numstat` lists no files for merge commits, so parents are what tells a
merge apart from an empty commit. Exports written before this field existed have no `parents`;
for those the empty-commit filter keeps every commit rather than risk dropping merges. Filtering
happens once, before any commits.json-sourced metric (`ComputeCategory` → `FilterCommits`).

## Metrics

| Metric | Direction | Critical | Healthy* | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Commits/week | + | 0.5 | 5 | 0.25 | commits.json | `rate_per_week` |
| Days since last activity | − | 40 | 3 | 0.20 | commits.json | `days_since_last_event` |
| Unique contributors | + | 1 | 5 | 0.15 | commits.json | `unique_count(author_email)` |
| Issue rate/week | + | 0.1 | 2 | 0.15 | issues.json | `rate_per_week` |
| Milestones/year | + | 0 | 3 | 0.05 | milestones.json | `rate_per_year` |
| Days since last milestone | − | 180 | 30 | 0.10 | milestones.json | `days_since_last_event` |
| Top-contributor share | − | 1.0 | 0.5 | 0.10 | commits.json | `max_share(author_email)` |

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
- SourceCraft's API returns Go's zero-value time (`"0001-01-01T00:00:00Z"`) for a milestone with
  no deadline set, rather than omitting the field or returning null. `milestonesToEvents` treats
  this sentinel (or an empty string) as unset and falls back to `created_at`
  (`isUnsetDeadline` in `internal/scoring/activity.go`) — without it, `days_since_last_milestone`
  silently computes from a ~2025-year-old fake deadline instead of the real date.

## Open questions

- Threshold validation against real repos — tracked in `backlog/phase-2-grade-service.md`.
- No automated unit tests yet for `internal/scoring` — same gap for every category.

## Design history (compressed)

- **Releases dropped, 2026-09-25.** `_meta/releases.json` was fetched but never found populated
  on any real repo tested — releases appear to be a largely-unused SourceCraft feature distinct
  from git tags, and it was never wired into any metric. repository-service no longer fetches it.
- **`net_lines_per_week` dropped, 2026-09-25.** Net lines changed per week conflated too many
  unrelated real events (a steady contributor pace, a single big refactor, a vendored-dependency
  bump, a revert) under one number, and its healthy band had no activity floor — a repo with
  **zero commits in the window** scored identically to one with healthy, balanced activity (both
  clamp to score 1 inside `[-2, 10]`). Caught reviewing `yapb/yapb`'s real result: a fully dormant
  repo got this metric's maximum score purely from having nothing happen. At 0.05 weight it wasn't
  pulling enough independent signal to justify fixing over dropping — `commits_per_week` and
  `days_since_last_activity` (0.20 weight each) already correctly punish zero/low activity, so the
  category lost no real coverage by removing it.
- **Its 0.05 weight moved to `commits_per_week` (0.20 -> 0.25), 2026-09-26.** Dropping the metric
  left the category's weights summing to 0.95, which the config validator rejects at startup.
- **`issue_rate_per_week`/`milestones_per_year` recalibrated, 2026-09-25.** Real data across 20
  diverse repos (`agents_memory/topics/threshold-calibration-data.md`) — including large orgs like
  gravity-ui/datalens/postgres forks — found median and p75 exactly 0 for both metrics, max 0.117
  and 4 respectively. The old thresholds (`critical: 0.2/1`, `healthy: 2/6`) meant essentially no
  real repo ever cleared critical, zero discrimination. Not evidence 20 real repos are badly run —
  reads as SourceCraft's issue/milestone features being lightly adopted platform-wide. New:
  `issue_rate_per_week` critical `0.02`/healthy `0.15`; `milestones_per_year` critical `0`/healthy
  `3` — both still `provisional: true`, now at least anchored to the real observed range instead
  of an arbitrary guess nothing could reach.
- **`issue_rate_per_week` recalibrated again, 2026-09-28** (was critical 0.02 / healthy 0.15). The
  09-25 population was mostly repos that don't use issues, so the bounds sat so low that any repo
  that does maxed out: 27 of 29 issue-using SourceCraft repos (11 test repos + 18 random from
  `docs/candidate-repos.json`) scored a perfect 1. Their rates: min 0, p10 0.34, median 1.0,
  p75 2.7, max 14.9 per week. New bounds 0.1 / 2: 9 full, 18 in between, 2 at 0 (no new issues in
  the window). Step by step: count issues opened in the last 180 days, divide by the weeks in that
  window; 2+ per week scores 1, 0.1 or less scores 0, straight line in between (e.g. 1.05/week →
  0.5). Still `provisional: true`.
