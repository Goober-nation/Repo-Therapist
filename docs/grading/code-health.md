# Code health / tech debt (20%, public)

**Status: planned — not started.** Last updated 2026-09-22. See `backlog/phase-2-grade-service.md`
("Next task", item 2) for the concrete implementation task.

## What it measures

Accumulated technical debt as marked in-code: how many TODO/FIXME markers exist, how old they are,
how concentrated they are. Snapshot metrics over current repo state, not an event rate.

## Data source

**Needs one new repository-service export: `_meta/todo-markers.json`.** Text-search the cloned
tree for TODO/FIXME markers, then `git blame` only those specific lines (cheap — not a full-repo
blame) to get each marker's age. Written before the clone's temp dir is discarded, same lifecycle
as `commits.json` (`backend/repository-service/internal/service/clone.go`). Grade service then
reads it like any other `_meta/*.json` export.

## Metrics

Planned, not yet in `config/grading.yaml`.

| Metric | Direction | Critical | Healthy | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Mean/median marker age | − | TBD | TBD | TBD | todo-markers.json | new |
| Density per kLOC | − | TBD | TBD | TBD | todo-markers.json | new |
| Oldest marker age | − | TBD | TBD | TBD | todo-markers.json | new |
| Worst-file concentration | − | TBD | TBD | TBD | todo-markers.json | new |

`Worst-file concentration` is defined explicitly as **% of all markers found in the single worst
file** — the original source draft left this undefined; this is the resolved definition.

## No-data vs. real-zero

- Gated at the **repo level**, not per-metric: the existing empty-repo/minimum-content-threshold
  check (`00-overview.md` principle 4) applies here directly. A repo that passes the size gate and
  genuinely has 0 TODO/FIXME markers is a real, good score — not `NoData` (a trivial/near-empty
  repo doesn't get to look artificially clean by having nothing to mark, which is exactly why the
  floor lives at the repo-size gate and not at the marker count).
- Within a repo that passed the gate: `density/kLOC` is well-defined at 0 markers (0/kLOC is a
  real number) as long as kLOC > 0. `Oldest marker age` and `worst-file concentration` are
  undefined with 0 markers (nothing to take an age or concentration of) → those two specific
  metrics go `NoData` even when the repo itself passed the size gate — same per-metric-floor
  pattern as Activity's `top_contributor_share`.

## Known limitations

- Needs per-line blame data, not just commit-level history that already exists — this is genuinely
  new repository-service work, not a re-read of existing exports (unlike Issues).
- TODO/FIXME text search is a simple grep-style match — no attempt at language-aware parsing
  (e.g. distinguishing a TODO in a comment from one in a string literal) planned for v1.

## Open questions

- Exact critical/healthy thresholds — not chosen yet.
- Marker detection scope: which file extensions/dirs to scan (e.g. skip `vendor/`, `node_modules/`
  the same way `.git` is already skipped by `UploadDir`) — not decided yet.
