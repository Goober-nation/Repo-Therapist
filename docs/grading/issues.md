# Issues (20%, public)

**Status: implemented.** Last updated 2026-09-25.

## What it measures

How well the project triages and closes work items: abandonment across the whole open-issue
backlog (not just recent issues), closure throughput, resolution rate. Time-to-first-response is
a decided non-goal, not a pending gap — see Known limitations. Median closure time is the
closure-related signal this category tracks instead; it doesn't stand in for first-response,
it's a separate question the metric set was scoped to answer from the start.

## How it's calculated, in plain words

1. Repository-service asks SourceCraft for the repo's issues and saves them as one entry per
   issue (created date, last-touched date, status) in S3.
2. **Abandoned share**: of every issue that's currently open — however old — what fraction
   haven't been touched in 60+ days.
3. **Growth ratio** and **share resolved**: count, independently, how many issues were *opened*
   in the last 6 months and how many were *closed* in the last 6 months (regardless of when the
   closed ones were originally opened). Growth ratio is opened÷closed; share resolved is
   closed÷(closed + everything still currently open).
4. **Median closure time**: of issues created in the last 6 months that got closed, the middle
   value of how long that took.
5. Below 5 relevant issues for a given number, it's "not enough data" instead of a guess — the
   relevant population differs per metric, see Metrics table.
6. Each scored number is placed on a line between a "healthy" value and a "critical" value —
   close to healthy scores near 1, close to critical scores near 0.
7. The four scores combine into one 0-100 category score by weighted average. A "no data" metric
   is dropped and the others' weights stretch to fill the gap — never treated as a 0.

**Median closure time, worked through (thresholds from 2026-09-28):** a median of 2 days or less
scores 1 (full), 14 days or more scores 0, and in between the score falls in a straight line —
e.g. 8 days → (14 − 8) / (14 − 2) = 0.5.

## Data source

S3 `_meta/issues.json` (same export Activity reads). `resolved` is judged by `status_type`
(`completed`/`cancelled`) — the fixed 5-value SourceCraft backend lifecycle enum
(`initial|in_progress|paused|completed|cancelled`, confirmed from the live Swagger spec) — never
the per-repo customizable `status` slug/name, which isn't safe to match across repos/orgs.

## Metrics

Event-stream shaped, same engine Activity uses (`ComputeCategory`/`eventsForSource`), config in
`config/grading.yaml` under `categories.issues`. All thresholds `provisional: true` — first-guess
values, not calibrated against a repo population.

| Metric | Direction | Critical | Healthy | Weight | `min_sample_size` population |
|---|---|---|---|---|---|
| Abandoned share (all open issues, untouched 60d) | − | 0.5 | 0.05 | 0.30 | currently-open issue count |
| Median closure days (created-and-closed cohort) | − | 14 | 2 | 0.25 | resolved-in-cohort count |
| Growth ratio (opened-in-window ÷ closed-in-window, floored at 1) | − | 5 | 1 | 0.20 | opened + closed in window |
| Share resolved (closed-in-window ÷ (open-now + closed-in-window)) | + | 0.3 | 0.9 | 0.25 | open-now + closed-in-window |

## No-data vs. real-zero

- Below `min_sample_size: 5` in a metric's own population (see table) → `NoData` for that
  metric — a ratio/median over a handful of events swings too far on one issue's fate to be
  defensible (TZ §13.2, устойчивость методики). Same pattern as Activity's `top_contributor_share`.
- A repo with enough issues where, say, 0% are abandoned is a real, good, measured 0% — not
  `NoData`.
- `growth_ratio`'s "floored at 1" (denominator only) is separate from the sample-size floor: a
  repo with real opens and zero closes gets a real harsh ratio, not a fabricated no-data.
- `share_resolved`'s denominator needs no dedup — "currently open" and "currently resolved" are
  mutually exclusive by construction.

## Known limitations

- **Median closure time is a proxy** (`updated_at - created_at`), not a real close-event
  timestamp — the wrapper exposes no first-comment/close-event timeline separate from
  `updated_at`.
- **Time to first response is out of scope, decided 2026-09-25** — `updated_at` conflates first
  response, later discussion, relabeling, and closure into one timestamp, so it can't be used as
  a proxy, and no other data source exists. Not planned; would need
  sourcecraft-wrapper-service to expose a real issue comment/event timeline before this is
  revisited at all.
- `paused` (`status_type`) counts as unresolved/open everywhere here, same as
  `initial`/`in_progress`.
- Issues carry their own visibility flag independent of the repo's — a public repo can contain
  private issues a viewer-tier PAT never sees, undercounting these metrics for repos that use
  that feature (`agents_memory/topics/sourcecraft-api.md`).
- `growth_ratio`/`share_resolved` are a simplification: most real dashboards (CHAOSS, GitHub
  Pulse, OSSInsight) track backlog size as a time series rather than a windowed ratio, since a
  ratio can't distinguish "small and volatile" from "enormous and stable." Defensible for a
  single 0-100 sub-score, not the field's primary pattern.

## Open questions

- Threshold calibration against a real repo population — done for `median_closure_days` only
  (2026-09-28, below); the other three are still first-guess values.
- A supplementary raw backlog-size trend (open-issue count, window start vs. end) — the most
  consistent finding from research was that the field prefers this over a bare ratio; not built.

## Design history (compressed)

**`median_closure_days` recalibrated, 2026-09-28** (was critical 60 / healthy 21). All 21 real
SourceCraft repos with enough resolved issues (11 test repos + 10 drawn at random from
`docs/candidate-repos.json`) scored a perfect 1 — SourceCraft team projects close issues in
0.01-13.9 days (median 1.24, p90 8.9), far below the ~30-day open-source average the old bounds
were based on. New bounds 14 / 2: 12 repos full, 9 graded in between, none at 0. Strict
percentiles (p90 / p25 = 9d / 0.5d) were rejected: they would call a 9-day median "critical" and
most reward same-day closes, some of which look like tracker bookkeeping rather than work.
Still `provisional: true` — n is small and SourceCraft-specific. Raw data:
`agents_memory/topics/threshold-calibration-data.md`.

Originally windowed every metric by issue *creation* date. Two rounds of correction, both
2026-09-22:
1. **No sample-size floor** — `dqdkfa/libmdbx` with 3 windowed issues produced a confident
   68.9/100. Added `min_sample_size: 5`.
2. **Creation-date windowing hid real abandonment** — an issue opened 2 years ago and never
   touched since aged out of any creation-window and was invisible to `abandoned_share`. A
   ~20-source research pass (CHAOSS, GitHub's `actions/stale`/`probot/stale`, OpenSSF Scorecard,
   MSR literature) confirmed every mature framework measures staleness over the whole open-issue
   population by last-activity date, never by creation date — redesigned `abandoned_share`
   accordingly, decoupled `growth_ratio`/`share_resolved`'s opened/closed counts, loosened
   `median_closure_days`' healthy bound (7d→21d) against the ~30d empirical real-world average.
   `median_closure_days` itself was left creation-windowed — that specific pattern (CHAOSS's
   cohort-based Issue Resolution Duration) is the field's standard for closure-time specifically.
   Confirmed on real data: `dqdkfa/libmdbx` has 7 currently-open issues stale 143-2197 days, 6 of
   which were created outside the old 180-day window — exactly the case the redesign fixes.
`libmdbx`'s `abandoned_share`, `growth_ratio`, and `share_resolved` were independently
recomputed from a fresh, separate API fetch (not through this codebase) and matched exactly.
