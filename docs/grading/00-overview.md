# Repo Health Score — grading overview (v1)

Team-shared methodology for computing the 6-category Repo Health Score. Supersedes the original
draft (`tmp/source-docs/grading-policy-original.md`, kept for provenance only). Written against
the case TZ (`yandex_docs/8. Yandex Cloud.pdf`, §3.1, §3.2, §13.2), whose hard requirements shaped
every design choice below: the score must be **reproducible, explainable, fact-grounded, and must
distinguish "bad" from "no data"** — judges explicitly test *behavior* on control scenarios, not
agreement with a reference number.

Per-category detail (what it measures, data source, metrics, no-data rules, limitations, open
questions) lives one file per category in this directory, all following the same template —
`activity.md`, `security.md`, `issues.md`, `code-health.md`, `ci-cd.md`, `documentation.md`. This
file holds only what's cross-cutting, so it isn't duplicated six times.

**Per `AGENTS.md`: any change to the grading system has to be discussed with the user and
reflected here before/alongside the code change — not committed as a drive-by.**

## Category weights

Kept from the TZ's example weights (§3.2) — they sum cleanly and nothing argues for deviating yet:

| Category | Weight | Public? | Status |
|---|---|---|---|
| Code health / tech debt | 20% | yes | planned |
| Security | 20% | no | **implemented** |
| Activity | 15% | yes | **implemented** |
| Documentation | 15% | yes | blocked (Grade↔LLM Kafka contract) |
| Issues | 15% | yes | planned |
| CI/CD | 15% | no | planned |

**Public score** = Activity + Documentation + Issues + Code health (65%, renormalized to /100).
**Private score** = all six, already summing to 100% directly.

Security and CI/CD are private-only, not a TZ requirement — but the TZ explicitly permits excluding
a category "if the reason and impact on score are explained" (§3.2), so this is a documented,
justified exclusion, not a silent one. The two categories' reasons differ — see each one's own
file: Security is genuinely platform-access-gated; CI/CD's private-only status is a product
decision, not a technical block (its data is viewer-readable on public repos).

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
