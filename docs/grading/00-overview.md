# Repo Health Score — grading overview (v1)

Team-shared methodology for computing the 6-category Repo Health Score. Supersedes the original
draft. Written against
the case TZ (`yandex_docs/8. Yandex Cloud.pdf`, §3.1, §3.2, §13.2), whose hard requirements shaped
every design choice below: the score must be **reproducible, explainable, fact-grounded, and must
distinguish "bad" from "no data"** — judges explicitly test *behavior* on control scenarios, not
agreement with a reference number.

Per-category detail (what it measures, **a simple-worded step-by-step walkthrough of how the
grade is calculated**, data source, metrics, no-data rules, limitations, open questions) lives
one file per category in this directory, all following the same template — `activity.md`,
`security.md`, `issues.md`, `code-health.md`, `ci-cd.md`, `documentation.md`. This file holds
only what's cross-cutting, so it isn't duplicated six times.

**Per `AGENTS.md`: any change to the grading system has to be discussed with the user and
reflected here before/alongside the code change — not committed as a drive-by — and every
category file must include the plain-language walkthrough section, kept in sync with the code.**

## Category weights

Started from the TZ's example weights (§3.2); re-weighted 2026-09-27 (see below):

| Category | Weight | Public? | Status |
|---|---|---|---|
| Activity | 20% (was 15%) | yes | **implemented** |
| Issues | 20% (was 15%) | yes | **implemented** |
| Security | 20% | no | **implemented** |
| Documentation | 15% | yes | **implemented** (LLM service) |
| CI/CD | 15% | no | **implemented** |
| Code health / tech debt | 10% (was 20%) | yes | **implemented** |

**Public score** = Activity + Documentation + Issues + Code health (65%, renormalized to /100):
Activity and Issues ≈31% each, Documentation ≈23%, Code health ≈15%.
**Private score** = all six, already summing to 100% directly.

**Re-weighting (2026-09-27).** A batch of 11 real public repos showed Code health scoring 100 for
8 of them: with fewer than 5 TODO/FIXME markers three of its four metrics are "no data", so marker
density alone — zero markers → perfect — decided the category, which then carried up to 40% of a
no-LLM public score. Code health was halved to 10% and the freed 10% split between Activity (the
category that actually separated those repos) and Issues. Step by step:

1. Each category is scored 0-100 exactly as before; nothing inside a category changed.
2. The overall score is the weighted average of the categories that have data, with the new
   weights: Activity 20, Issues 20, Security 20, Documentation 15, CI/CD 15, Code health 10.
3. Categories without data are dropped and the rest re-weighted, as always — so in the public view
   Code health counts ≈15% instead of ≈31%.

Security and CI/CD are private-only, not a TZ requirement — but the TZ explicitly permits excluding
a category "if the reason and impact on score are explained" (§3.2), so this is a documented,
justified exclusion, not a silent one. The two categories' reasons differ — see each one's own
file: Security is genuinely platform-access-gated; CI/CD's private-only status started as a product
decision, and its current data source (`cicd/runs`) is also owner-token-only.

## Public vs. private grading and recommendations (2026-09-26)

Each grading job is **either** public **or** private, never both:

- The request carries a user token → **private**: all six categories, graded with the user's
  token.
- No token → **public**: only the four public categories, graded with the service's own token.
  Security and CI/CD are not computed at all (no AppSec call, no CI/CD read).
- A non-public repo requested without a token can't be graded and the request is dropped.

The result (`grading.results`) says which one it is in `visibility` and fills only that side;
`missing_data` lists only that view's categories, so a public result never reports Security or
CI/CD as missing.

**Recommendations**: every graded category's recommendations (one per metric scoring below
healthy, see `config/recommendations.yaml`) go into one pool, and **N are picked at random**
(all of them if there are N or fewer). N is `GRADE_ADVICE_LIMIT` (default `0` = send all).
Each text starts with a severity label (`[critical]`, `[high]`, `[medium]`, `[low]`); it is
display-only — selection is not ordered by it.

### How a result is put together, step by step

1. The request arrives. Token present → private, otherwise public.
2. The repo is cloned and its data exported (with the user's token for private jobs).
3. The categories for that visibility are scored: public = Activity, Documentation, Issues,
   Code health; private = those plus Security and CI/CD.
4. The overall score is the weighted average of those categories; categories with no data are
   dropped and the rest re-weighted. In the private view, open Security findings then cap it:
   open critical → at most 59, open high → at most 79 (see `security.md`).
5. Every category's recommendations are collected into one list, and `GRADE_ADVICE_LIMIT`
   (default 0 = all) are drawn at random.
6. The result is sent with `visibility` set and only that side (score, category scores,
   recommendations) filled; `missing_data` names that view's categories that had no data.

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
   **Parked as of 2026-09-25**: still aspirational, not implemented as a hard gate anywhere —
   explicit decision (during Code health's implementation) that every repo should be gradeable
   regardless of size, since a hard gate blocks grading small-but-real repos entirely. The gaming
   concern this principle names is real and unaddressed; the intended replacement is a future,
   separate "repo grade trust/confidence factor" (weights the score's reliability rather than
   refusing to compute one) — not designed yet. Code health's per-metric floors (`min_sample_size`) are a
   narrower, already-implemented version of the same underlying concern, scoped to individual
   metrics rather than the whole repo.
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

## Implementation methodology

The working method every implemented category so far has followed, kept here so it repeats for
Code health/CI/CD/Documentation rather than being reinvented per category:

1. **Before writing code: read the existing state, then propose a plan and get it confirmed.**
   Read the TZ section the category maps to and the relevant platform-API research already
   recorded — never start from a blank assumption
   about what data is available. Lay out the concrete plan (data source, metric list, no-data
   rule per metric) and get it confirmed before writing implementation code, not after.
2. **Implementation is checked against this doc's own principles, not just "does it compile."**
   Every metric gets an explicit no-data-vs-real-zero rule (principle 2/4) before it's considered
   done — a metric with no stated floor is treated as unfinished, not just unpolished.
3. **Testing has two independent layers, both required — neither substitutes for the other:**
   - *Synthetic, targeted cases* — small hand-constructed inputs built to exercise one specific
     behavior (a sample-size floor tripping, a specific edge case a fix is meant to catch) and
     checked against a hand-computed expected result. Fast, precise, good for proving a specific
     fix does what it claims.
   - *Live, end-to-end runs against real repos* — the actual services (repository-service,
     grade-service, any wrapper involved) run against real SourceCraft data, not mocks. Chosen to
     cover a range of shapes (near-empty, small, large; healthy and neglected) rather than one
     convenient example.
4. **Real-repo results get independently cross-checked, not just trusted.** At least one
   real-repo result is recomputed by hand from a fresh, separate fetch of the same raw data
   (outside the codebase being tested — e.g. a standalone script hitting the platform API
   directly) and compared number-for-number against the code's own output. A result is not
   "verified" until it matches an independently-derived number, not just until the pipeline runs
   without erroring.
5. **When a design choice can't be settled from the TZ or first principles alone, look outward
   before guessing.** For choices with real prior art (what counts as a "stale" issue, how a
   metric should be windowed, what threshold values are defensible) a research pass over
   external, real-world sources (established frameworks, tooling defaults, relevant literature)
   is run and its findings are what change the design — not a from-scratch guess presented as if
   it were informed. Every threshold adopted this way is still marked `provisional: true`: an
   externally-sourced starting point, not a substitute for calibration against this project's own
   repo population.
6. **Self-review is part of the process, not a one-time gate.** Coming back to already-shipped
   work and asking "is this actually defensible" — not just "does it run" — is expected to
   surface real problems (a missing sample-size floor, a windowing choice that quietly hides the
   exact cases a metric exists to catch). When it does, the fix, its reasoning, and what
   triggered it are recorded in the category's own file (this directory), not just fixed silently.

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
