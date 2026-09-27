# Documentation (15%, public)

**Status: implemented** — contract is `repo.docs.grade.requested` → `repo.docs.graded`
(`docs/kafka-contracts.md`). Last updated 2026-09-27.

## How the grade is calculated (step by step)

1. Grade service publishes `repo.docs.grade.requested` for the repo and keeps grading the other
   categories while it waits (up to the LLM timeout, 70s by default).
2. If no reply arrives in time → the whole Documentation section is **no data**.
3. If the reply has a nonempty `error` (e.g. `CONTENT_FILTER`, `LLM_TIMEOUT`), or `grade` is
   `null` → the whole Documentation section is **no data**; subgrades and advice are ignored and
   the error code is logged.
4. Otherwise the LLM's `grade` (0–100) is taken as the category score unchanged; `subgrades` are
   shown as metrics for display only and do not recompute the score.
5. A no-data Documentation section is dropped from the overall grade and the remaining category
   weights are renormalized, the same as any other no-data category.

## What it measures

Documentation quality and best-practice hygiene: README existence and *quality* (not just
presence), LICENSE, local-run instructions, build/test instructions, project structure,
CONTRIBUTING, CODEOWNERS.

## Data source

**Judged by the LLM service, not deterministic heuristics** (decision, 2026-09-22). Grade service
ships repo doc content already present in S3 (README, CONTRIBUTING, CODEOWNERS, detected
build/local-run/test instructions, directory structure) to the LLM service over Kafka, and gets a
score/assessment back. Rationale: TZ §3.1's Documentation criteria are quality judgments ("is the
README actually useful"), not presence checks a deterministic heuristic can make well, and the LLM
service already exists in the architecture for exactly this kind of call.

**Confirmed against the TZ PDF directly (§3.1, 2026-09-22): no external documentation-site
crawling is required or implied.** Every listed criterion is a repo-internal artifact obtainable
from the repo tree already uploaded to S3 by repository-service (`UploadDir`) — nothing new to
fetch, the LLM only ever needs content already in hand.

## Metrics

Not applicable in the usual table form — this category's "metric" is a single LLM judgment call,
not a set of independently-weighted numeric metrics. Exact response shape (a single score? a
per-criterion breakdown the LLM returns that Grade service then combines?) is undecided pending
the Kafka contract.

## No-data vs. real-zero

- Repo below the minimum-content threshold (`00-overview.md` principle 4) → `NoData`.
- The LLM call failing or timing out → `NoData` — never a fabricated score. This covers a missing
  reply and a reply with a nonempty `error` / `grade: null` (2026-09-27).
- A repo the LLM successfully judges as having *no* README at all is a real, harsh score from the
  LLM — not `NoData`. Same "measured harsh vs. unmeasurable" distinction every other category
  draws, just evaluated by the LLM instead of a formula.

## Known limitations

- Original v1-baseline plan (deterministic presence/heuristic checks, no LLM) is superseded, kept
  nowhere but this note for provenance — do not resurrect it without discussing with the user
  first, per `AGENTS.md`'s grading-system rule.

## Open questions

- The actual Grade↔LLM Kafka message contract (`docs/kafka-contracts.md`) — a team decision,
  blocking any implementation here.
- Whether the LLM returns one score or a structured per-criterion breakdown Grade service then
  combines with the usual weighted-formula machinery — undecided, depends on the contract.
