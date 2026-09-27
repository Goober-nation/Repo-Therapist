# Code health / tech debt (10%, public)

**Status: implemented.** Last updated 2026-09-25.

## What it measures

Accumulated technical debt as marked in-code: how many TODO/FIXME markers exist, how old they
are, how concentrated they are. Snapshot metrics over current repo state, not an event rate.

## How it's calculated, in plain words

1. Repository-service walks the cloned tree (skipping `.git`, `vendor/`, `node_modules/`, and
   binary files), finds every line containing `TODO` or `FIXME`, and for each one runs `git
   blame` on just that file to find which commit last touched the line and how old that commit
   is. It also counts the total non-blank lines across every scanned text file (`total_loc`).
2. That list of markers (file, line, type, age in days) plus `total_loc` is saved to S3 as
   `_meta/todo-markers.json`.
3. Grade-service computes four numbers from that list: the median marker age, markers per 1000
   lines of code, the share of markers older than 293 days, and the single worst file's share of
   all markers.
4. Each number is placed on a line between a "healthy" value and a "critical" value — close to
   healthy scores near 1, close to critical scores near 0.
5. The four scores combine into one 0-100 category score by weighted average. A metric that's
   "no data" (see below) is dropped and the others' weights stretch to fill the gap — never
   treated as a 0.
6. The category counts 10% of the overall score (was 20% until 2026-09-27). Most repos have too
   few markers for more than the density metric, so "few TODOs" alone gave a perfect category
   score that weighed heavily on the total; see `00-overview.md` for the re-weighting.

## Data source

S3 `_meta/todo-markers.json`, written by `internal/todomarkers` (repository-service), read by
`internal/scoring/codehealth.go` (grade-service). Not routed through the generic
`ComputeCategory`/`eventsForSource` engine Activity/Issues share — these are snapshot statistics
over a marker list, not per-event rates to window (same reasoning `security.go` documents for
Security). Metric weights/thresholds still live in `config/grading.yaml` (unlike Security), since
each one maps cleanly onto the existing `linear_clamp` formula config.

## Metrics

All thresholds `provisional: true` — first-guess values, not calibrated against a repo population.

| Metric | Direction | Critical | Healthy | Weight | `min_sample_size` population |
|---|---|---|---|---|---|
| Median marker age (days) | − | 293 | 183 | 0.25 | **uncapped** marker count — see below |
| Density per kLOC (markers / 1000 LOC) | − | 3 | 0.3 | 0.30 | none — see below |
| Share of markers stale (≥293d, uncapped) | − | 0.5 | 0.05 | 0.25 | **uncapped** marker count — see below |
| Worst-file concentration (max-file markers ÷ total markers) | − | 0.6 | 0.15 | 0.20 | all markers (age-independent) |

`Worst-file concentration` is defined as **% of all markers found in the single worst file** —
the original source draft left this undefined; this is the resolved definition.

**"Oldest marker age" was replaced with "share of markers stale ≥ a threshold"** — see Design
history.

**Median marker age's critical (293d) and healthy (183d), and `share_stale_markers`' per-marker
staleness cutoff (293d, same value), are flat but empirically calibrated** — p90/p25 of the real
pooled age distribution across 192 markers from 6 repos (median 242d, p25 183d, p75 278d,
p90 293d — see Design history). A per-repo commit-cadence-relative version of these two metrics
was tried and reverted: real data showed TODO age doesn't scale proportionally with commit cadence
either, and using the ratio would have penalized the fastest-moving, most actively-maintained
repos hardest for the same or better absolute age than slower ones — see Design history for the
full analysis.

## No-data vs. real-zero

- `density_per_kloc` is the only metric with **no** `min_sample_size` floor: 0 markers over a
  real, non-empty `total_loc` is a real, good (0) density, not `NoData` — a trivial/near-empty
  repo doesn't get to look artificially clean by having nothing to mark. It only goes `NoData`
  when `total_loc == 0` itself (e.g. a repo of nothing but binaries/vendor) — genuinely nothing
  to divide by, distinct from a small-sample problem.
- `worst_file_concentration` is undefined with 0 markers (nothing to concentrate) — below
  `min_sample_size: 5` markers, `NoData`, same pattern as Activity's `top_contributor_share`.
- `median_marker_age` and `share_stale_markers` are computed **only over markers with a
  confidently known age** — a marker whose git-blame walk hit the shallow-clone boundary
  (`Marker.AgeCapped: true`, see Known limitations) has an *unknown* age, not a measured old one,
  and including it would silently score "we don't know" as "this is bad." Below
  `min_sample_size: 5` **uncapped** markers (not raw marker count) → `NoData` for these two
  metrics specifically. A repo with 50 markers, all capped, has 0 usable data points here, not
  50 — see Design history for the real-repo testing that found this needed fixing.
- Whole-category `NoData` when `todo-markers.json` itself couldn't be fetched (`Available: false`
  in `CodeHealthInputs`) — distinct from a fetch that succeeded and found a genuinely low-content
  repo, which is not gated (see Design history — no minimum-content/repo-size gate exists for
  this category, or anywhere, by explicit decision).

## Known limitations

- **Shallow-clone blame cap**: repository-service clones with `--shallow-since=<GIT_HISTORY_DAYS
  days ago>`. A marker whose true origin predates that window blames to the shallow clone's
  grafted boundary commit, not its real origin — `AgeDays` is set to exactly
  `GIT_HISTORY_DAYS` and `AgeCapped: true` in that case, i.e. "at least this old," not a precise
  age. Detected via `.git/shallow`'s boundary-commit list, not a date heuristic.
  **In practice this hits the large majority of markers on any repo with real, mature history**
  (postgres 98.5% capped, divkit 97.9%, diktat 100% — measured 2026-09-25 against real repos,
  see Design history) — only a repo whose TODO-touching commits all happen to fall inside the
  1-year window escapes it. `median_marker_age`/`share_stale_markers` exclude capped markers
  entirely rather than trust the fabricated age (see No-data vs. real-zero); the practical
  consequence is that these two metrics go `NoData` far more often on real repos than the
  `min_sample_size` floor alone would suggest — a repo can have hundreds of TODOs and still have
  under 5 with a confidently known age.
- **No language-aware parsing** — a `TODO`/`FIXME` match is a plain word-boundary text search, no
  distinction between a marker in a real comment vs. a string literal. Not planned for v1.
- **Marker list is fixed** (`TODO`, `FIXME`) as a plain `[]string` in
  `internal/todomarkers.Markers` — extending it (e.g. adding `XXX`/`HACK`) is a one-line change,
  not a design decision made now.
- `AgeDays` is computed once, at repository-service's scan time, not re-derived at grading time —
  a re-grade without a fresh clone would score against a stale snapshot. Not a concern in
  practice since grading always follows a fresh clone (same lifecycle as `commits.json`), but
  flagged since it differs from Activity's live-computed-`now` pattern.

## Open questions

- Threshold calibration against a real repo population — not done, same gap every provisional
  threshold has (no external research pass was run for this category, unlike Issues).
- Marker keyword set (`TODO`/`FIXME` only) could later add `XXX`/`HACK` — deliberately deferred,
  not designed now.

## Design history (compressed)

- **No minimum-content/repo-size gate.** A design draft proposed short-circuiting small repos to
  "insufficient data" (extending 00-overview.md's aspirational principle 4 into a real
  cross-cutting gate, reusing `total_loc` as the signal). Rejected outright: every repo should be
  gradeable regardless of size — parked instead as a future "repo grade trust factor" concept
  (separate mechanism, not built now).
- **"Oldest marker age" replaced with "share of markers stale ≥ a threshold" (180d originally,
  293d after later recalibration below).** A raw max is an order
  statistic: with more markers sampled from the same underlying age distribution, the max skews
  older purely from sample size — this would systematically penalize large repos vs. small ones
  for no real quality difference (flagged directly: "a big repo will always statistically do
  worse than a small one with todos"). `density_per_kloc` already normalizes for size by
  construction and needed no change; the fix specific to the age-extremity metric was swapping it
  for a ratio (share of markers past a staleness threshold) in the same statistical family as
  Issues' `abandoned_share`, normalized by construction and not sensitive to marker count.
- A `todos/file_count` ratio (as an alternative or additional normalization) was considered and
  declined — `density/kLOC` already normalizes for size, and `worst_file_concentration` is
  already a share, not a raw count, so no further per-file normalization was judged necessary.
- **Real-repo testing (2026-09-25), three rounds, same day:**
  1. *Capped markers were polluting the age metrics.* Tested 12 real repos (5-17,651 files); on
     most, 60-100% of markers hit the shallow-clone age cap, so `median_marker_age`/
     `share_stale_markers` were scoring a fabricated 365d age as if real, clustering nearly every
     repo at ~50/100 regardless of actual quality. **Fix**: exclude capped markers entirely
     (`uncappedMarkers`), `NoData` below `min_sample_size` uncapped rather than a fabricated
     number.
  2. *Tried making the thresholds relative to each repo's own commit cadence* (reasoning: a flat
     day-count judges a hyper-active repo and a rarely-touched one the same way). Rejected on
     re-verification — every active real repo had a sub-2-day commit gap, so the mechanism
     floored at the same `critical: 90d` for all of them, differentiating nothing.
  3. *Checked the premise itself before retuning it* — computed age÷cadence directly. It varied
     64x across repos and ran backwards: the fastest-moving, best-maintained repos (`userver`,
     `divkit`) would be penalized hardest for the same or better absolute TODO age than a
     slow-moving one. Reverted entirely, no cadence dependence anywhere in Code health.

  **Landed on**: flat `critical: 293d` / `healthy: 183d` for `median_marker_age`, and the same
  293d as `share_stale_markers`' per-marker cutoff — taken from the real pooled age
  distribution's p90/p25 (192 markers, 6 repos), not a guess. Re-verified against all 12 repos:
  real partial credit and a 58.3-100 spread, no clustering, no backwards incentive.

- **`density_per_kloc` recalibrated, 2026-09-25.** A follow-up 20-repo run (5 to 17,651 files)
  found the max observed density across
  32 repos tested total (both batches) was 2.01/kLOC — the old `critical: 20/healthy: 1` meant
  virtually every real repo scored ~1.0 here regardless of actual density, no discrimination.
  New: `critical: 3` / `healthy: 0.3`, anchored to the real observed range instead of an arbitrary
  guess nothing could approach. Still `provisional: true`.
