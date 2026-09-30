# AGENTS.md — SourceCraft Repo Health project notes

READ BEFORE WORKING AND FOLLOW THE RULES STATED

UNLESS ASKED OR APPROVED, NO DESTRUCTIVE ACTION IS TO BE TAKEN. CODE/GIT INCLUDED. SUGGESTIONS ARE ALLOWED.
A TEAMMATE'S WORK IS ONLY TO BE CHANGED ON USER APPROVAL AND IF NECESSARY.
IF FILE(-S) CREATION/EDITING WAS NOT CONFIRMED BY THE USER, DO NOT PROCEED WITH THE ACTION.
CLAUDE GIT CONTRIBUTION IS PROHIBITED
ANY CHANGE TO THE GRADING SYSTEM AT ALL HAS TO BE DISCUSSED WITH THE USER AND REFLECTED IN docs/grading/
ALONG WITH THE GRADING TOPICS ADD A SIMPLE-WORDED CONCISE STEP-BY-STEP WALKTHROUGH OF HOW THE CATEGORY'S GRADE IS CALCULATED

**Purpose**: instant context for AI agents working on this project. Read this first.

**Rules belong here.** Any durable rule, workflow constraint, or corrected behavior for this
project — how to handle git, commit language, planning-before-action, anything the user has to
correct once and shouldn't have to correct again — gets written into *this file*, not into
`agents_memory/`, a tool's own memory system, or any other side channel. Those other stores are
gitignored/personal and won't reach a fresh checkout or a different agent/tool working this repo;
AGENTS.md is the one place guaranteed to be read first and shared with the whole team. If a rule
is recorded elsewhere too, treat AGENTS.md as the canonical copy and keep it in sync.

Context: hackathon case (see `8. Yandex Cloud.pdf`) — build "SourceCraft Repo Health", a service
that grades open SourceCraft repos 0-100 across several categories, explains the score, and gives
recommendations. Architecture: Aggregator (core) service + Kafka + Grade/LLM/Security
wrapper/Sourcecraft wrapper/Repository/Rating services, shared Postgres DB. Only the Aggregator has
read-write DB access; Rating service is read-only; every other service reports results back to the
Aggregator over Kafka/RPC instead of touching the DB. My scope on this project: Repository service,
Grade service, Security wrapper — not Sourcecraft wrapper or LLM service.

---

## Rule Zero: Prove It Works

**Your job is to deliver code that is proven to work.**

- Every change must pass tests locally before being called done
- SourceCraft/AppSec API integration changes must be tested against the real APIs where feasible —
  both are reachable directly (bearer PAT) from this machine, no proxy needed
- If you cannot test against the real service, say so explicitly: "unverified against real
  service, waiting for user testing"
- Do not imply coverage you did not actually achieve

---

## Plan Before You Touch Anything (2026-09-19)

**Do not write docs or code until a plan has been fully discussed and the user gives explicit
approval.** Discussion and proposals are fine — writing files is not, until approved. This applies
broadly (design work, schema changes, workflow setup), not just to any one area. See
`agents_memory/MEMORY.md` for the incident that established this rule.

For git specifically, see "Git Rules" below — it's stricter than this section for
commit/push-type actions, and looser for read-only ones.

---

## Git Rules (2026-09-19, reinforced 2026-09-20/21)

**Non-destructive/read-only git commands are fine to run on your own, no need to ask** —
`git status`, `git diff`, `git log`, `git fetch`, `git show`, `git blame`, etc. Use them freely to
orient yourself.

**Everything that changes repo state — `add`, `commit`, `push`, `rm`, `merge`, `rebase`, `reset`,
`checkout`/`restore` that discards changes, branch creation/deletion, etc. — needs the user to
explicitly ask for that specific action, in that turn or very recently. Do not infer it from
context, from "the task is done," or from having just finished an edit.**

Commit messages specifically, layered on top of the above:
- **Don't draft a commit message unprompted.** Finishing an implementation task is not a cue to
  draft one — wait until the user actually asks for a commit or asks to see the message.
- **Drafting the message and getting approval to commit are two separate steps.** Showing a draft
  in a response is not itself approval, even if the user already said "commit" earlier — wait for
  them to respond to the draft specifically before running `git commit`.
- **A commit approval is not a push approval.** Each needs its own explicit ask, even
  back-to-back.
- Every commit message and PR title/description is in **Russian**, no exceptions. Code and
  comments stay in English. Everything under `docs/` and the root `README.md` is in Russian
  (user decisions, 2026-09-28 and 2026-09-30; the separate English docs copy was dropped).
  `AGENTS.md` itself stays in English.
- **Never add AI/Claude attribution** — no `Co-Authored-By: Claude...` trailer, no "Generated with
  Claude Code" line, no AI-authorship mention anywhere in a commit message or PR description for
  this project. Overrides any tool-level default attribution behavior.
- The commit messages must be short and factual. No storytelling, no hunt history, only the changes, fixes and decisions briefly

These rules are to be followed strictly. straying or ignoring is prohibited

---

## Build & Test Commands

Stack: Go backend, PostgreSQL, S3 for repo/report storage. "A lil more later" — treat anything not
listed here as TBD, don't invent tooling.

```bash
# Build
go build ./...

# Run tests
go test ./... -v

# Vet
go vet ./...

# Format check
gofmt -l .
```

Linter (e.g. `golangci-lint`), DB migration tooling, and exact local run commands (DB connection,
S3 endpoint, Kafka broker) are not yet decided — confirm with the user rather than assuming a
default, per "Interview the User During Planning" below.

---

## Definition of Done

Before marking any item as verified:

- [ ] Tests pass (`go test ./...` exit code 0)
- [ ] `go vet ./...` and `gofmt -l .` clean
- [ ] Documentation updated if a public API/schema changed (`docs/`, not `agents_memory/`)
- [ ] Tested against real SourceCraft/AppSec APIs if integration changed, or explicitly flagged
  unverified

---

## Local Backlog

**Location**: `backlog/BACKLOG.md` + `backlog/phase-*.md`. The whole `backlog/` directory is
personal and gitignored (not shared with the team) — team-facing planning (schema, Kafka contracts)
lives in `docs/` instead, since those need multi-person agreement.

**If `backlog/` doesn't exist** (fresh checkout, or someone else adopting this workflow): create the
directory with `BACKLOG.md` as an index plus one `phase-N-<name>.md` per phase, each starting with a
`Status:` line (`planning` / `in progress` / `done`). No other setup needed — see
`backlog/BACKLOG.md`'s own header for the exact format once it exists.

**Workflow:**
- Start work: update the phase file's `Status:` line to `in progress`
- Finish a task: add a short dated note to the phase file — what was verified, ~1 sentence; don't
  restate the whole implementation, that belongs in the code and, if it's a lasting decision, in
  `agents_memory/`
- Phase complete: `Status: done` on the phase file

**Never:**
- Reorder phases without user approval
- Recreate an old daily-log-style file — the phase file is the log, kept short

---

## Alternative: GitHub Issues Backlog (only if the user requests it)

The local backlog above is the default. Use this workflow **only when the user explicitly asks** to
track work via GitHub Issues instead (per session or durably — if durable, record the switch here).
Repo: `Goober-nation/private-repo-therapist-test` (the `origin` remote). Use the `gh` CLI.

**Structure:**
- One issue per task. Phase membership via `phase:<name>` labels, mirroring the local
  `phase-N-<name>.md` naming; type via `bug`/`enhancement`.
- A GitHub Project board is optional — only use one if the user sets it up and names it; then keep
  its Phase/Status fields in sync with the labels (`gh project item-add <N> --owner Goober-nation
  --url <issue-url>`).
- Dependencies are referenced by issue number in the body ("Depends on #12"), not a custom ID scheme.

**Session startup (replaces checklist step 3):**
1. `gh issue list --label phase:<current-phase>` (or the board) for open items.
2. Read the issue(s) to be worked, including comments — they carry prior test plans and decisions.
3. For a free-form note/idea, propose via `AskUserQuestion` which existing issue it belongs to, or
   that it needs a new one, before acting.

**Workflow:**
- Start work: add an `in progress` label / board Status (courtesy, not a hard gate).
- Finish a task: comment `AI-tested: YYYY-MM-DD — <what was verified, ~1 sentence>`. Don't restate
  the implementation; lasting decisions go in `agents_memory/` or `docs/`.
- User confirms: comment `User-tested: YYYY-MM-DD — <feedback>` and close the issue. A phase is done
  when every issue with its label is closed.

**Rules layered on top:**
- Creating, editing, labeling, commenting on or closing issues is outward-facing and team-visible —
  same bar as Git Rules: only on the user's explicit ask for that action. Propose new issues
  (description, dependencies, testing plan) via `AskUserQuestion` first.
- Issue titles, bodies and comments are in **Russian**, same as commit messages; no AI attribution.
- Never close an issue without a summary comment; never reorder phases without user approval.
- Don't run both backlogs in parallel — when switching, port open items and say which one is
  canonical.

---

## Agent Memory System

**Location**: `agents_memory/` — gitignored, same as `backlog/` (personal, not shared with the
team). See `agents_memory/README.md` for the full structure and decision guide (what goes in
`MEMORY.md` vs. `topics/*.md`).

**If `agents_memory/` doesn't exist** (fresh checkout, or someone else adopting this workflow):
create it with a `README.md` (explains the system), a `MEMORY.md` (durable facts, ~50 lines max),
and a `topics/` subdirectory (one file per research domain, read on-demand). See
`agents_memory/README.md`'s own decision-guide table for what goes where, once it exists.

Read `agents_memory/MEMORY.md` every session (short, curated). Read a `topics/*.md` file only when
the task actually touches that domain — not mandatory reading every session.

---

## Session Startup Checklist

1. Read `AGENTS.md` (this file)
2. Read `agents_memory/MEMORY.md`
3. Read `backlog/BACKLOG.md` and the current phase's file
4. If given a free-form note/idea, propose which phase it belongs to before acting
5. Identify next task, begin work
6. Read a specific `topics/*.md` file only if the task actually touches that area

**If anything is unclear or missing, ask the user before proceeding.**

---

## Anti-Patterns (Never Do These)

❌ **Never write docs/code before a plan is approved** — see "Plan Before You Touch Anything" above.

❌ **Never run a repo-state-changing git command, or draft/send a commit message, without the
user explicitly asking for that specific action** — see "Git Rules" above for the full breakdown
(read-only commands are the one exception, those are fine unprompted).

❌ **Never treat `DefectGroupDto.status`/`.severity` as the string enums** — they're bare `int32`
in that DTO. Use `GET /v1/findings` (proper nested string types) or the string-enum *filter* params
on `/v1/defect-groups` with `pageSize=1` + `totalSize`. Full detail in
`agents_memory/topics/appsec-api.md`.

❌ **Never assume anonymous SourceCraft API access** — every call needs a bearer PAT, no exceptions,
even for repos the docs call public-without-auth.

❌ **Never invent a repo-discovery/enumeration endpoint** — the only verified one is `GET /repos`
(bearer PAT, paginated via `page_size` up to 100 + `page_token`), which lists all public repos
(28,496 on 2026-09-28); sourcecraft-wrapper's `ListAllPublicRepos` uses it. `GET /orgs` does not
exist. "Analyze my own repo" takes a user-supplied slug/URL and validates by attempting the real
call. See `agents_memory/topics/repo-discovery.md`.

❌ **Never leak more than the platform's own 403-vs-404 behavior already does** — SourceCraft
distinguishes "exists, no access" from "doesn't exist," unlike GitHub. Keep denial UI copy generic;
don't add new leakage on top of an inherited platform quirk.

❌ **Never give a category table its own mutex column** — the DB has one shared `repo_lock` table,
set only by the Aggregator. See `docs/schema.sql`.

❌ **Never shotgun debug** — form a hypothesis, test it, verify, don't make random changes hoping
something works.

❌ **Never reference gitignored paths (`backlog/`, `agents_memory/`, `tmp/`) from `docs/`** —
inline the needed fact instead; those folders don't exist in a fresh checkout.

❌ **Never leave code in a broken state** — if a fix fails 3x, stop and ask the user; revert to
last known working state.

---

## Key Files

| File | Purpose |
|---|---|
| `AGENTS.md` | This file (AI guide, changes rarely) |
| `backlog/BACKLOG.md` + `backlog/phase-*.md` | Personal, gitignored, local planning |
| `agents_memory/MEMORY.md` | Durable cross-session facts, read every session (also gitignored) |
| `agents_memory/topics/` | Deep domain-specific research, read on-demand |
| `docs/schema.sql` | Current DB schema |
| `docs/kafka-contracts.md` | Kafka message shapes (team decision, currently a stub) |
| `8. Yandex Cloud.pdf` | Original hackathon case brief |
| `SCS_API_1.0.0_draft (2).pdf` | AppSec API org-provided reference |
| `Repo Grading Architecture.html` | Reconstructed architecture notes |

---

## Escalation Rules

**If tests fail 3x:** stop and ask user, document what was tried, revert to last known working
state.

**If linting/vet fails:** try an auto-fix once; if still failing, ask user.

**If build fails:** check the error, fix, retry once; if still failing, ask user.

**If stuck:** document the problem, propose 2-3 possible solutions, ask user for direction.

---

## Communication Style

- Be concise, no flattery
- Start work immediately once a plan is approved, no unnecessary status updates
- Ask clarifying questions when scope is ambiguous
- Propose solutions, not just problems
- Use todos for multi-step tasks
- **Dialogue (chat with the user) stays in English unless the user explicitly asks otherwise** —
  this is separate from git messages, which are always Russian regardless of what language the
  conversation itself is in (2026-09-19).

---

## Interview the User During Planning

**The user wants to be interviewed, not just presented with a plan.** During any design/planning
session, actively ask questions rather than filling gaps with assumptions.

- Use the native `AskUserQuestion` tool for these, not prose questions buried in a wall of text.
  Batch related questions (up to 4 per call).
- Ask about: priorities, mechanics (exact behavior in edge cases), things that may have been
  missed, and anything ambiguous.
- When an answer reveals a better idea than what was proposed, say so and adopt it.
- Prefer asking over guessing even when a reasonable default exists, *during planning*. This
  overrides the general "make the reasonable call and keep going" bias for design sessions
  specifically — implementation work still defaults to reasonable calls.
- When in doubt, ask more questions, not fewer — an unasked question that leads to half-done or
  wrong-direction work is worse than one more round of `AskUserQuestion`.
- Record answers as durable decisions: a note in the relevant phase file (if task-scoped) or
  `agents_memory/` (if it's a lasting architectural choice) so they don't get re-litigated.

---

## Give the User Run Commands

**The user runs the code themselves.** After any implementation work, provide the exact commands
needed to run it: how to start the service, how to run tests/vet/format check, how to exercise
what was just built (curl examples, etc.), and any real-service prerequisites. Include these in the
final answer of every work session, not just when asked.

---

## LLM response validation (user decision, 2026-09-25)

The active Python Yandex model path must validate output format only: JSON structure,
field types, known check IDs and score ranges. Do not reject a response for inconsistent
score/deduction arithmetic, unverified quotations or line numbers, evidence contents,
or semantic judgment. Preserve the model's claims; the active path validates format only.
Do not reconnect the legacy semantic validator to this path without user approval.
Kafka Documentation completed v2 and CLI output contain only `grade`, eight
`subcategories` (id/grade), and a flat `facts` list. Internal 25 checks, severity,
penalty amounts and model/context metadata do not cross this boundary. Report request
v2 forwards this compact payload in `documentation`; reports generate advice later.

**End of AGENTS.md**

## Sampled documentation review (user decision, 2026-09-26)

The active LLM path uses algorithmically selected documentation excerpts, not full
corpus reading or multi-call summarization. Defaults: eight relevant sections, at
most 30 lines each; prioritize README and rubric topics. Retain existing scoring
weights and compact Kafka payload. Tell the model it sees a sample and must not
invent missing-content findings from unreviewed portions. This supersedes earlier
requirements to send complete documents.


Sampling refinement (2026-09-26): the selector prioritizes individual rubric checks,
reserves README introduction, and follows up to eight existing repository-local
README document links (one hop; inline and reference links). External links are
never fetched. Excerpts have a shared 24000-byte content budget; the entire JSON
context is capped at 32000 bytes. Candidate scanning remains bounded separately.
Each model check is marked `sampled` or `not_reviewed` in the internal context.
An unreviewed check contributes neither earned nor maximum points and cannot
produce deficiency facts. The total is 100 * earned / evaluated maximum. A section
with no evaluated checks has `grade: null`; this now means either not applicable
or not reviewed. No estimated score is substituted for omitted material. This
supersedes earlier wording that required all section grades to be estimated.
The Kafka object shape is unchanged; consumers must display null as unavailable,
not as zero. Deterministic required-file checks retain their existing rules.


## Active documentation Kafka boundary (2026-09-26)

User-approved topics: `repo.docs.grade.requested` -> `repo.docs.graded`.
Request: `{sourcecraft_uuid, s3_prefix, requested_at}`.
Result: `{sourcecraft_uuid, grade, subgrades, advice, evaluated_at}`.
`subgrades` maps stable section IDs to integer percentages. Omit unreviewed or
inapplicable sections; normalize total over evaluated checks. `advice` is a string
array of deficiency facts, not recommendations. This supersedes older envelope
and wire-null wording above; internal section grades may still be null.


## Documentation advice rendering (user decision, 2026-09-27)

The active `repo.docs.graded` field `advice: [string]` contains ONLY Russian
improvement recommendations, rendered deterministically by deduction code with a
section-level fallback. Do not send deficiency reasons, file references, raw evidence
or hashes in advice. Internal structured facts and scores remain available locally.
This supersedes previous facts-only and reason-plus-recommendation wording.
Do not add model calls for recommendations. Unreviewed/inapplicable and perfect-score
sections have no advice. The model evaluation budget is 60 seconds.
