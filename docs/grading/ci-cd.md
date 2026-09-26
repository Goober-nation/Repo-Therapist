# CI/CD (15%, private only)

**Status: implemented 2026-09-26.** Verified against 5 real mock repos (`docs/ci-mock-repos.json`)
both through the scoring code directly and through the full repository-service → S3 →
grade-service pipeline.

## What it measures

Whether the repo has CI at all, how often its runs pass, whether that's getting better or worse,
and how long a run takes.

## How it's calculated, in plain words

1. Repository-service checks the cloned repo for a CI config file (`.sourcecraft/ci.yaml`, the only
   location SourceCraft's CI/CD reference documents).
2. It asks sourcecraft-wrapper for the repo's CI runs using the caller's token. It keeps runs
   created in the last 180 days, newest first, at most 25. If the runs can't be read (the token
   doesn't own the repo), it records that instead.
3. Both results are saved to S3 as `_meta/ci-runs.json`.
4. Grade-service decides whether there is anything to grade:
   - runs couldn't be read → **no data** for the whole category;
   - no config file **and** zero runs → **score 0** (hard gate: the repo has no CI);
   - config file but zero runs → **no data** (CI is set up but was never seen running).
5. Otherwise it keeps only finished runs (passed or failed) and computes three numbers: the share
   that passed; the pass share of the newer half minus the older half; the median run time in
   minutes.
6. Each number is placed on a line between a "healthy" and a "critical" value — close to healthy
   scores near 1, close to critical near 0.
7. The three scores combine into one 0-100 score by weighted average (50% / 30% / 20%). A metric
   without enough runs is "no data", dropped, and the others' weights stretch to fill the gap.
8. That score is multiplied by 0.4 + 0.6 × (success-rate score). A pipeline that never passes keeps
   at most 40% of its score (so at most 20), one that passes ≥90% of the time keeps all of it. If
   success rate is "no data", nothing is multiplied.

## Data source

`_meta/ci-runs.json`, written by repository-service (`internal/cidata`):

```json
{
  "config_files": [".sourcecraft/ci.yaml"],
  "runs_available": true,
  "runs": [{"slug": "8", "status": "failed", "event_type": "push",
            "created_at": "...", "started_at": "...", "finished_at": "..."}]
}
```

- Runs come from the existing wrapper RPC `GetCiCDRuns` → `GET /repos/id:{id}/cicd/runs`. That
  endpoint only answers for a token with owner access (403 `listCIFlux` otherwise, re-verified
  2026-09-26 on `imbok/uims-portal`). The wrapper currently reports that 403 as a generic gRPC
  `Internal`, so "denied" and "fetch failed" look the same — both become `runs_available: false`,
  which is correct for grading either way.
- Includes push-, PR- and schedule-triggered runs alike.
- SourceCraft reports a queued run (`prepared`) with `started_at`/`finished_at` of
  `1970-01-01T00:00:00Z`; repository-service blanks those so they can't be read as real times.

## Metrics

All thresholds are provisional (first guesses; the mock repos only exercise the extremes).

| Metric | Direction | Critical | Healthy | Weight | Min runs | Aggregation |
|---|---|---|---|---|---|---|
| `has_ci` | gate | — | — | 0 (gate only) | — | `ci_has_ci`: config file or any run exists |
| `ci_success_rate` | + | 0.5 | 0.9 | 0.5 | 3 | `ci_success_rate` |
| `ci_stability_trend` | + | -0.3 | 0 | 0.3 | 6 | `ci_stability_trend`: newer-half minus older-half pass share, -1..1 |
| `ci_median_duration_min` | − | 30 | 5 | 0.2 | 3 | `ci_median_duration_min` |

Run status handling (`Run.Status` enum from the Swagger spec):
- pass: `success`
- fail: `failed`, `timeout`
- ignored: `canceled`, `skipped`, `awaiting_approval`, `rejected` (say nothing about whether the
  code passed), `created`, `prepared`, `processing` (not finished)

**Success multiplier** (`ciSuccessMultiplier` in `cicd.go`, floor `0.4`): final = weighted ×
(0.4 + 0.6·s), s = `ci_success_rate`'s 0-1 score. Chosen over raising success rate's weight, which
would have made trend and duration nearly irrelevant: with the multiplier they still move the score
proportionally at every success level.

With an odd number of finished runs, the middle one is left out of the trend so both halves are
equal size. Duration only counts runs that have both timestamps.

## No-data vs. real-zero

- **Token doesn't own the repo** → whole category no-data (decided 2026-09-26). Private grade is
  renormalized over the other private categories.
- **No config and zero readable runs** → real 0 via the `has_ci` gate. This is the one case the
  gate formula's "zero the whole category" side effect is applied (in `ComputeCICD`).
- **Config but zero runs** → no-data, not 0.
- **Runs exist but no config** (config deleted or moved) → scored normally; `has_ci` passes.
- Too few finished runs for every metric's floor → no-data.

## Mock repos and tests

- Fixture tests: `backend/grade-service/internal/scoring/testdata/ci/*.json` +
  `cicd_test.go` — 14 scenarios, pinned to the real `config/grading.yaml` thresholds.
- Real repos: `docs/ci-mock-repos.json` (green 100, red 20, flaky 8, dormant no-data, none 0),
  created by `scripts/mock-repos/create-all.sh`, checked by `cicd_live_test.go`
  (`-tags live`). See `docs/testing.md`.

## Known limitations

- Only the wrapper's first page of runs is read (it doesn't pass a page size or follow page
  tokens). The page held all 8 runs of every mock repo; if SourceCraft's default page is smaller
  than 25, busy repos get fewer runs than the cap.
- The 180-day window is by `created_at`, applied in repository-service, so the export reflects
  export time, not grading time.
- Only `.sourcecraft/ci.yaml` counts as a config file.

## Open questions

- How to grade "config but never ran" and "no CI at all" — raised for the team,
  `backlog/team-sync-2026-09-26.md` item 5. Current behavior: no-data and 0 respectively.
- The ownership check is implicit (403 → no-data). An explicit "caller controls this repo" rule at
  the Aggregator is still not designed.

## Design history (compressed)

- 2026-09-22 plan: viewer-tier `GET /pulls/{slug}/merge-checks` per PR (the only CI signal a
  non-owner token can read), with "0 PRs → no-data" and no duration available. Dropped
  2026-09-26: CI/CD is private-only anyway, `cicd/runs` with an owner token gives full run history
  including durations, and the wrapper already had the RPC. PR coverage was dropped with it (the
  wrapper has no PR listing), replaced by median duration.
- 2026-09-26: an always-red but stable, fast pipeline scored 50 (trend and duration perfect).
  Fixed with the success multiplier (red → 20, flaky → 8); moderate pipelines drop too (80% pass:
  76 → 65), accepted as the cost of keeping trend/duration meaningful.
- 2026-09-26: the clone-side config check was added so a repo with no PRs/runs can still be told
  apart ("has CI, never ran" vs. "no CI").
