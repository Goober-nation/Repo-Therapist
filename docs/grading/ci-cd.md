# CI/CD (15%, private — product decision, not a platform block)

**Status: planned — not started.** Last updated 2026-09-22. See `backlog/phase-2-grade-service.md`
("Next task", item 3) for the concrete implementation task.

## What it measures

Whether CI exists at all, how often it passes, whether it's getting more or less stable over time.

## Data source

**Re-verified live against the real API, 2026-09-22 — all three claims below confirmed exactly:**

| Endpoint | Result |
|---|---|
| `GET /repos/.../cicd/runs` (direct listing, repo not ours) | `403 AccessDenied`, permission `src.repositories.listCIFlux` |
| `GET /repos/.../cicd/runs` (repo we own) | `200` — ownership unlocks it, but not relied on (viewer-tier path below works regardless of ownership) |
| `GET /pulls/{slug}/merge-checks` | `200`, viewer-readable — reproduced the exact `security-pipeline1`/`security-pipeline2` example on `examples/appsec-analysis` PR 1 |
| Direct workflow-content fetch (`getCIWorkflowContent`) | `403 AccessDenied` — still blocked even for a real run slug found via `merge-checks` |

**Needs upstream work in two other services**, not just Grade service:
1. sourcecraft-wrapper-service: new RPC iterating a repo's PRs (`/pulls`, already viewer-readable)
   and calling `/pulls/{slug}/merge-checks` on each, returning aggregated `ci_workflows`.
2. repository-service: calls that new RPC, exports `_meta/ci-runs.json`.
3. Grade service: scores success rate + stability trend (recent-half vs. older-half success rate)
   from that export.

Full test matrix: `agents_memory/topics/sourcecraft-api.md`'s "CI/CD signal" section.

## Metrics

Planned, not yet in `config/grading.yaml`.

| Metric | Direction | Critical | Healthy | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Has CI at all (hard gate) | gate | — | — | — | ci-runs.json | `gate` (see no-data section) |
| Success rate | + | TBD | TBD | TBD | ci-runs.json | new |
| Stability trend (recent-half vs. older-half) | + | TBD | TBD | TBD | ci-runs.json | new |
| Duration | — | n/a | n/a | n/a | — | **no data source at any PAT tier** |

## No-data vs. real-zero

**The one deliberate exception to "never fabricate 0" in this whole policy:**

- **0 PRs ever** → `NoData`, not 0. Through this data source, a push-only-to-main pipeline looks
  identical to no pipeline at all — genuinely no observable signal, not evidence of no CI.
- **PRs exist but none show any `ci_workflows`** → real hard gate, **category score = 0**. This is
  the first category needing `combine.go`'s "gate zeroes the whole category" side effect —
  `formula.go`'s `gate` formula type has existed in the registry since Activity was built but
  nothing has used it yet.
- **Duration** is no-data regardless of repo or PAT tier — it simply isn't in the `merge-checks`
  payload (only `created_at`, `status`, `run_slug`, `workflow_slug`). This revises the original
  "owner-tier only" assumption: it's unavailable through this data source at any tier, not just
  gated by access.

## Known limitations

- Only sees CI runs triggered *by PRs* — a push-only-to-main pipeline is invisible to this data
  source (folded into the "0 PRs → NoData" case above, not a separate bug).
- One API call per PR for `merge-checks` — needs sampling/pagination for repos with many PRs, not
  designed yet.

## Open questions

**Access-control decision, explicitly parked, not blocking implementation:** the data is
technically fetchable on any public repo at viewer tier — private-only is a product choice (don't
expose operational CI detail to someone who doesn't control the repo), not a platform limitation.
The real rule this implies — grade/expose CI/CD only when the caller's own PAT shows an
ownership/control relationship to the repo being analyzed, not just "repo is private" — isn't
designed yet. Until it exists, Grade service will compute CI/CD unconditionally for any repo
(test/dev behavior only, explicitly not the intended production gating, per user instruction
2026-09-22). Likely belongs at the Aggregator layer, not something Grade service enforces alone.
