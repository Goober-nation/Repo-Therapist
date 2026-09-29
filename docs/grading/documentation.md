# Documentation (15%, public)

**Status: implemented** — contract is `repo.docs.grade.requested` → `repo.docs.graded`
(`docs/kafka-contracts.md`). Last updated 2026-09-29.

## How the grade is calculated (step by step)

1. Grade service publishes `repo.docs.grade.requested` for the repo and keeps grading the other
   categories while it waits (up to the LLM timeout, 70s by default).
2. The LLM service reads the repo from S3 and asks the model to grade it. In the default
   `LLM_DOCS_MODE=auto`, if anything on the model path fails — the call itself (provider down,
   rate limit, 60s timeout, content filter, malformed answer) or turning the model's answer into
   a result — the service grades the same repo **algorithmically** instead (section presence only,
   quality not judged) and replies with that grade: every section found in the docs earns 80% of
   its points (quality unknown), a missing one earns 0, required files score
   as usual, and consistency is not graded. The service adds a notice that this simplified method
   was used; the grade service may reorder or limit the recommendations. `LLM_DOCS_MODE=algorithmic`
   always uses this method; `LLM_DOCS_MODE=llm` never falls back and reports the failure instead.
   (2026-09-29)
3. If no reply arrives in time → the whole Documentation section is **no data**.
4. If the reply has a nonempty `error` or `grade` is `null` → the whole Documentation section is
   **no data**; subgrades and advice are ignored and the error code is logged. In `auto` mode this
   only happens when the repo itself couldn't be read (e.g. `S3_PREFIX_NOT_FOUND`), the request
   was invalid, or the algorithmic grade also failed.
5. Otherwise the reply's `grade` (0–100) is taken as the category score unchanged; `subgrades` are
   shown as metrics for display only and do not recompute the score.
6. A no-data Documentation section is dropped from the overall grade and the remaining category
   weights are renormalized, the same as any other no-data category.

## What it measures

Documentation quality and best-practice hygiene: README existence and *quality* (not just
presence), LICENSE, local-run instructions, build/test instructions, project structure,
CONTRIBUTING, CODEOWNERS.

## Data source

**Primary path: LLM judgment, with deterministic fallback.** Grade service ships repo documentation
already present in S3 (README, CONTRIBUTING, CODEOWNERS, detected build/local-run/test instructions,
directory structure) to the LLM service over Kafka and gets a score and assessment back. The model
judges quality; the fallback only checks whether sections are present. Rationale: TZ §3.1's
Documentation criteria include quality judgments ("is the README actually useful"), which presence
checks cannot make well, and the LLM service already exists in the architecture for this call.

**Confirmed against the TZ PDF directly (§3.1, 2026-09-22): no external documentation-site
crawling is required or implied.** Every listed criterion is a repo-internal artifact obtainable
from the repo tree already uploaded to S3 by repository-service (`UploadDir`) — nothing new to
fetch, the LLM only ever needs content already in hand.

## Metrics

Not applicable in the usual table form — the LLM path returns one category judgment rather than a
set of independently weighted numeric metrics; the fallback checks section presence. The reply
carries one `grade` (0–100), used as the category score, plus `subgrades` and `advice`
(`repo.docs.graded` in `docs/kafka-contracts.md`).

## No-data vs. real-zero

- There is no minimum-content gate: `00-overview.md` principle 4 is parked, so a near-empty repo
  is still graded (see the known limitation there).
- If the model path fails or times out in `auto` mode, the service uses the algorithmic grade: a
  real, simplified score, with the fallback noted in advice. `NoData` is returned for a missing
  reply or a reply with a nonempty `error` / `grade: null`; in `auto` mode, this means the repo
  could not be read, the request was invalid, or the algorithmic fallback also failed.
- A repo the LLM successfully judges as having *no* README at all is a real, harsh score from the
  LLM — not `NoData`. Same "measured harsh vs. unmeasurable" distinction every other category
  draws, just evaluated by the LLM instead of a formula.

## Known limitations

- Original v1-baseline plan (deterministic presence/heuristic checks, no LLM) is superseded, kept
  nowhere but this note for provenance — do not resurrect it without discussing with the user
  first, per `AGENTS.md`'s grading-system rule.

## Open questions

- None about the contract: it is fixed in `docs/kafka-contracts.md` (`repo.docs.grade.requested`
  → `repo.docs.graded`), and the LLM returns a single `grade` taken as the category score.
