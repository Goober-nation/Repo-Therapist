# Repo Health Score — grading policy (v1)

Team-shared methodology for computing the 6-category Repo Health Score. Supersedes the original
draft (`tmp/source-docs/grading-policy-original.md`, kept for provenance only — this doc is the
one to implement against). Written against the case TZ (`yandex_docs/8. Yandex Cloud.pdf`, §3.1,
§3.2, §13.2), whose hard requirements shaped every design choice below: the score must be
**reproducible, explainable, fact-grounded, and must distinguish "bad" from "no data"** — judges
explicitly test *behavior* on control scenarios, not agreement with a reference number.

## Category weights

Kept from the TZ's example weights (§3.2) — they sum cleanly and nothing argues for deviating yet:

| Category | Weight | Public? |
|---|---|---|
| Code health / tech debt | 20% | yes |
| Security | 20% | no |
| Activity | 15% | yes |
| Documentation | 15% | yes |
| Issues | 15% | yes |
| CI/CD | 15% | no |

**Public score** = Activity + Documentation + Issues + Code health (65%, renormalized to /100).
**Private score** = all six, already summing to 100% directly.
Security and CI/CD are private-only per our own platform-access research
(`agents_memory/topics/sourcecraft-api.md`, `appsec-api.md` — both are role-gated, unreachable for
a public crawl we don't own), not a TZ requirement — but the TZ explicitly permits excluding a
category "if the reason and impact on score are explained" (§3.2), so this is a documented,
justified exclusion, not a silent one.

## Design principles

1. **One scoring formula, everywhere** — replaces the original draft's mix of a log-shaped formula
   (whose sign convention was ambiguous/backwards for most of its own metrics) and a two-branch
   linear formula:
   ```
   score = clamp((x - critical) / (healthy - critical), 0, 1)
   ```
   Works for "higher is better" and "higher is worse" alike, purely by which of `critical`/`healthy`
   is numerically larger — no separate sign branches, and trivially explainable in the UI ("your
   value sits X% of the way from critical to healthy").
2. **Missing/disabled metrics renormalize weights, never auto-zero.** A metric that's `not_calculated`
   (deliberately turned off) and a metric that's `calculated` but has no data for this particular repo
   are treated identically at combine-time: dropped, and the remaining metrics' weights renormalized
   to sum to 1 within the category. If every metric in a category is unavailable, the category itself
   reports "Нет данных" and is dropped one level up, with the same renormalization applied to
   categories. This directly satisfies TZ §11 rule 5 and is a judged criterion (§13.2).
3. **The clamped score is lossy by design — the raw value never is.** Two repos with wildly different
   severity (2 vs 40 open critical vulnerabilities) both clamp to score 0, which is correct for
   *aggregation* (an unbounded contribution would let one metric dominate — exactly what TZ §13.2's
   "устойчивость методики" criterion checks against) but wrong for *recommendations*, which need
   that severity difference. Every formula/aggregation output is `{raw_value, score}`; recommendation
   generation reads `raw_value`, never the clamped score.
4. **Empty/trivial repos must not accidentally score well.** A near-empty repo trivially clears most
   "badness" thresholds (zero TODOs, zero abandoned issues, zero everything) and could look
   artificially healthy. Repos below a minimum content threshold (or `is_empty=true`) short-circuit
   to "Недостаточно данных" rather than a score. Not something either source doc called out — a real
   gaming vector caught during design, not after.
5. **Popularity is never a scoring input**, anywhere — matches TZ §11 rule 9 ("Repo Health Score
   должен иметь большее значение для рейтинга, чем популярность проекта") directly.
6. **Metric config is layered — source, aggregation, formula — each independently swappable.**
   `source` says which S3 `_meta/*.json` file feeds the metric; `aggregation` says how raw
   timestamped events become one number; `formula` says how that number maps to a 0–1 score. This
   is what makes `config/grading.yaml` extensible without rewrites — see "Future: time-decay" below
   for why this specific split matters, not just modularity for its own sake.
7. **`status: calculated` / `not_calculated`** is a per-metric *and* per-category config-time toggle,
   kept deliberately separate from runtime "no data for this repo" — the two are different facts
   (we chose not to compute this, vs. we tried and this repo had nothing to compute) that the UI
   must be able to tell apart, even though they're combined identically for scoring (principle 2).

## Future: time-decay (not implemented now, deliberately schema-ready)

A flat time window (e.g. "commits in the last 180 days") has a hard cliff: a commit on day 179
counts fully, day 181 counts zero — the score can jump purely because time passed, not because the
repo changed. Exponential decay (weighting each event by `exp(-age/half_life)`) removes that
artifact. This is a real improvement, not implemented in v1 because it needs real half-life values
to validate against, which we don't have yet — shipping an untuned decay curve is worse than a
documented flat window.

What v1 does to avoid this becoming tech debt: every `_meta/*.json` export
(`commits.json`, `issues.json`, `milestones.json`) stores full per-event timestamps, never a
pre-aggregated count — so switching an `aggregation.type` from `rate_per_week` to a future
`exp_decay_rate` is a change inside one Go function, not a data re-export. The trap this avoids is
building any future category's export as a pre-bucketed aggregate instead of an event list.

## Activity (15%, implemented)

| Metric | Direction | Critical | Healthy* | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Commits/week | + | 0.5 | 5 | 0.20 | commits.json | `rate_per_week` |
| Days since last activity | − | 40 | 3 | 0.20 | commits.json | `days_since_last_event` |
| Unique contributors | + | 1 | 5 | 0.15 | commits.json | `unique_count(author_email)` |
| Issue rate/week | + | 0.2 | 2 | 0.15 | issues.json | `rate_per_week` |
| Milestones/year | + | 1 | 6 | 0.05 | milestones.json | `rate_per_year` |
| Days since last milestone | − | 180 | 30 | 0.10 | milestones.json | `days_since_last_event` |
| Top-contributor share | − | 1.0 | 0.5 | 0.10 | commits.json | `max_share(author_email)` — no-data if <2 commits in window; a genuine 100% (one person, many commits) still scores as real bus-factor risk, not excluded |
| Net lines/week (band) | healthy [-2,10], critical [-30,50] | — | — | 0.05 | commits.json | `rate_per_week(net_lines)` |

*= marked `provisional: true` in config — first-guess placeholders, not yet validated against real
repos. "MR rate/week" resolves a contradiction in the original draft (prose said per-month, its own
table said per-week) — picking per-week, matching the table.

## Documentation (15%, method defined; implementation pending)

LLM service owns the category's versioned 0–100 method. Ordinary code performs deterministic file
checks; model output is validated for format only. The model awards integer points for semantic documentation
quality. LLM service combines both parts and returns a final category score that Grade service does
not recalculate. The current Kafka completed payload (v2) contains only `grade`, eight section
`subcategories`, and a flat `facts` list. Internal checks and numeric per-fact penalties are
not sent; recommendations are generated later on Aggregator request.

The method assigns 20 points to deterministic README, LICENSE, CONTRIBUTING, and CODEOWNERS checks,
and 80 points to purpose, local-run, build, testing, usage, navigation, and consistency checks.
Missing and inapplicable checks follow the global renormalization rule in design principle 2.
Complete weights, codes, normalization, evidence rules, and control cases are defined in
[`documentation-grading.md`](documentation-grading.md).

## Issues (15%, not yet implemented)

Window: last 6 months. Needs a new `_meta/issues.json` export (not yet built). Metrics: % abandoned
(open, untouched 30d), median response time, median closure time, growth rate (opened/closed,
floored at 1), share resolved.

## Code health / tech debt (20%, not yet implemented)

Snapshot metrics over current TODO/FIXME markers: mean/median age, density per kLOC, oldest marker,
worst-file concentration (defined explicitly as % of all markers found in the single worst file —
the original draft left this undefined). Needs per-line blame data, not just commit-level history —
find marker lines via text search, then `git blame` only those specific lines (cheap, not a
full-repo blame) into a new `_meta/todo-markers.json` export.

## CI/CD (15%, not yet implemented)

Hard gate: no CI detected → category score = 0. Then: success rate, stability trend (recent-half vs
older-half success rate), duration (owner-tier only, no-data on the public crawl — known platform
limitation, see `agents_memory/topics/sourcecraft-api.md`).

## Security (20%, not yet implemented)

Real AppSec SourceCraft data only, per TZ §3.1's explicit warning against faking it. Open
critical/high/medium/low counts, days since last scan. No scan ever run → whole category is "Нет
данных," never a fabricated 0.
