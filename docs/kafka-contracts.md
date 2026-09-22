# Kafka message contracts

Scope: the message shapes published/consumed on the Kafka links in the architecture diagram —

- Aggregator <-> Grade service — **still TBD**, a team decision (needs whoever owns the
  Aggregator), not filled in here.
- Grade service <-> Security wrapper — **still TBD**, same reasoning.
- Grade service <-> LLM service — **still TBD**, same reasoning. Needed for the Documentation
  category (2026-09-22 decision: Documentation is judged by the LLM service, not a deterministic
  heuristic — Grade service ships it repo doc content, e.g. README/CONTRIBUTING/etc., and gets a
  score back). See `backlog/phase-2-grade-service.md`'s Documentation section for what Grade
  service's side needs once this contract exists.
- Grade service <-> Repository service — **defined below**. This exchange is entirely within one
  person's scope (Repository + Grade service), so it didn't need to wait on wider team sign-off —
  see `backlog/phase-3-kafka-integration.md` for the implementation notes.

## `repo.grade.requested`

Producer: currently only a manual trigger (`grade-service --request-org=X --request-repo=Y`);
eventually the Aggregator. Consumer: Repository service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "requested_at": "2026-09-19T23:49:12+03:00"
}
```

## `repo.ready`

Producer: Repository service, once it's finished (successfully or not) handling a
`repo.grade.requested` message. Consumer: Grade service.

```json
{
  "org_slug": "examples",
  "repo_slug": "vulnerable-repo",
  "sourcecraft_uuid": "019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "s3_prefix": "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e",
  "status": "ready",
  "commit_count": 3,
  "pull_count": 2,
  "release_count": 0,
  "ready_at": "2026-09-19T23:49:15+03:00"
}
```

`status` is `"ready"` or `"failed"` — on failure, `error` carries the message and the count fields
are omitted. Grade service logs and skips `"failed"` messages rather than trying to grade them.

Go types: `internal/contracts/repo_grading.go`. Verified end-to-end (2026-09-19): manual trigger ->
Repository service -> `repo.ready` -> Grade service -> printed Activity score, matching the same
result independently verified by hand earlier in phase 2.
