# Security (20%, private)

**Status: implemented** (2026-09-22). Last updated 2026-09-22.

## What it measures

Real AppSec SourceCraft findings only, per TZ §3.1's explicit warning against faking security
data. Open critical/high/medium/low defect-group counts, days since the latest scan.

## How it's calculated, in plain words

1. Grade-service asks security-wrapper-service, "what does AppSec know about this repo?",
   handing over the user's own SourceCraft login token so the wrapper can ask on their behalf.
2. If the repo has never been scanned, the whole category is "no data" — we never guess or
   pretend it's a 0.
3. If it has been scanned, we get back: how many open critical/high/medium/low security findings,
   and how many days since the last scan.
4. Each of those five numbers is placed on a line between "healthy" (e.g. 0 open criticals) and
   "critical" (e.g. 5+ open criticals) — closer to healthy scores near 1, closer to critical
   scores near 0.
5. The five scores are combined into one 0-100 category score by weighted average (open critical
   count matters most).

## Data source

gRPC call to security-wrapper-service's `GetAppsecResult(git_repo)` — Grade service dials it
directly (`backend/grade-service/internal/appsecwrapper`), forwarding the caller's SourceCraft PAT
(`SOURCECRAFT_PAT` env) as `authorization` metadata. The wrapper itself calls the real AppSec API
(`https://appsec.sourcecraft.tech`) — latest scan lookup, then per-severity **OPEN** defect-group
counts via `/v1/defect-groups` filtered by `severity`+`status=OPEN` (never the raw `int32`
status/severity fields — see `agents_memory/topics/appsec-api.md`'s int32 landmine writeup).

## Metrics

Snapshot input (one gRPC call, already aggregated) — doesn't go through the
source/aggregation-config engine the event-stream categories use; weights/thresholds are
hardcoded in `internal/scoring/security.go`, only the category's own weight/status is
config-driven (`config/grading.yaml`).

| Metric | Direction | Critical | Healthy | Weight | Source | Aggregation |
|---|---|---|---|---|---|---|
| Open critical count | − | 5 | 0 | 0.35 | AppSec gRPC | n/a — snapshot |
| Open high count | − | 15 | 0 | 0.25 | AppSec gRPC | n/a — snapshot |
| Open medium count | − | 40 | 0 | 0.15 | AppSec gRPC | n/a — snapshot |
| Open low count | − | 80 | 0 | 0.10 | AppSec gRPC | n/a — snapshot |
| Days since last scan | − | 90 | 0 | 0.15 | AppSec gRPC | n/a — snapshot |

All five thresholds are first-guess placeholders, not yet validated against a real population of
repos (same caveat Activity's `provisional: true` flags carry).

## No-data vs. real-zero

- `HasScan=false` (repo has never been scanned) → **whole category** is `NoData` — never a
  fabricated 0. This is the dominant no-data case for Security, not a per-metric edge case.
- A scan that ran and found 0 open critical/high/medium/low is a real, good, measured score — not
  `NoData`.
- Any wrapper/dial/gRPC-call failure (network, timeout, wrapper down) → logged, degrades to
  `NoData` — a transport failure must never be reported the same as "clean scan."

## Known limitations

- **Access gate is public/private, not ownership** (confirmed by organizers 2026-09-21): public
  repos get AppSec with no extra setup; private repos need an opt-in "Security Add-on" connected
  for that specific repo — owning/adminning the repo does **not** unlock it on its own (verified
  live: our own private repo 403'd everywhere on `appsec.sourcecraft.tech` until flipped public).
  Practical effect: the TZ's "analyze my own repo" flow will legitimately hit "Нет данных" for
  Security on a private repo without the add-on connected — expected platform behavior, not a
  bug. See `agents_memory/topics/appsec-api.md` for the full test matrix.
- Report bytes (SARIF, for human-readable defect summaries) are parsed in-memory and discarded,
  never persisted to S3 — matches the "parse then discard" DB design decision.

## Open questions

- Whether to surface "connect the Security Add-on" as an explicit UI call-to-action for the
  private-repo no-data case, vs. a silent no-data card — product decision, not yet made.
- End-to-end verified (2026-09-22) against a real repo
  (`timoxa102928/appsec-private-test`) via both direct `grpcurl` and the full grade-service client
  path — not just compiled.
