# Manual testing

No automated test suite yet — this is how to exercise
everything by hand, matching what's actually been verified so far.

## Setup

```bash
cp .env.example .env   # fill in SOURCECRAFT_PAT with a real token
docker compose up -d   # Postgres + MinIO + Kafka
set -a && source .env && set +a
```

## 1. Repository service alone (one-shot)

Clones a repo, uploads its tree + git history + issues/milestones to S3. Needs
sourcecraft-wrapper-service running too (`go run ./backend/sourcecraft-wrapper-service/cmd`,
`SOURCECRAFT_URL=https://api.sourcecraft.tech`) — repository-service talks to it via
`SC_WRAPPER_ADDR` (defaults to `localhost:8083`).

```bash
go run ./backend/repository-service/cmd/repository-service --org=examples --repo=vulnerable-repo
```

Check what landed in MinIO (console at `http://localhost:9001`, login from `.env`), or via `mc`:

```bash
docker run --rm --network <compose-project>_default --entrypoint sh quay.io/minio/mc -c "
  mc alias set local http://minio:9000 \$S3_ACCESS_KEY \$S3_SECRET_KEY
  mc ls --recursive local/repo-content/repos/<sourcecraft_uuid>
"
```

(`<compose-project>` is the docker-compose project name — usually the directory name; check with
`docker compose ps` if unsure.)

## 2. Grade service alone (one-shot)

Reads a repo's S3 exports (from step 1) and computes the Activity score.

```bash
go run ./backend/grade-service/cmd/grade-service --repo-id=<sourcecraft_uuid>
```

Prints the full score breakdown as JSON — each metric's raw value, score, weight, and whether it
was no-data.

## 3. Full async flow via Kafka

Start both services in listen mode (separate terminals, or backgrounded):

```bash
go run ./backend/repository-service/cmd/repository-service --listen
go run ./backend/grade-service/cmd/grade-service --listen
```

Then trigger a grade from a third terminal. This publishes `grading.requests` exactly like the
Aggregator does and prints the `grading.results` message for that repo:

```bash
go run ./backend/grade-service/cmd/grading-request --org=examples --repo=vulnerable-repo
```

`grading.results` carries only the repo's UUID, so `grading-request` first waits for the
`repo.ready` message for this org/repo (which has the UUID), then for the result with that UUID
and visibility. Messages for other requests are skipped, so a late result from an earlier request
is never printed as this one's. It exits non-zero right away if the clone failed (`repo.ready`
status `failed`) or the result has an `error`; otherwise it gives up after `--wait` (default 10m,
covering the default clone limit).

Watch the two listener terminals: Repository service logs the clone/upload and publishes
`repo.ready`; Grade service logs the grading and publishes the result.

**Note**: if testing `--listen` processes via `go run ... &` in a shell script, `kill <pid>` won't
stop the actual compiled binary (`go run`'s signal handling doesn't propagate to its child) — build
first and run the binary directly, or use `pkill -f <binary-name>`, if you need to script
start/stop.

## 4. Batch: all candidate repos

`scripts/grade-candidates.sh` runs step 3 for every repo in `docs/candidate-repos.json` with
`passes_full_criteria=true`, one at a time:

1. If the dockerized `go-services` is running, it's stopped (it bundles older builds of the same
   services and would take the batch's messages) and started again when the script exits, including
   on Ctrl-C.
2. Only the infra containers are started (postgres, kafka, minio, migrate); the three services are
   built from the current code and run locally.
3. Each repo is requested with `--wait=15m`; arguments to the script go to `grading-request` and
   override that (e.g. `--wait=20m`).
4. Repos that got no result are retried once at the end, then a summary prints how many were
   graded, how many recovered on retry, and which still failed.

Documentation needs the LLM service; for a batch without it: `LLM_ENABLED=false scripts/grade-candidates.sh`.

**Time limits** (stability over speed, 2026-09-27): repository-service gives each repo
`CLONE_TIMEOUT` (default 10m) for clone + export + upload, grade-service `GRADE_TIMEOUT`
(default 4m) for grading. Each consumer handles one message at a time, so a repo that uses its full
limit holds up the ones queued behind it. Transient SourceCraft/AppSec errors are retried inside
the wrappers (`HTTP_RETRY_ATTEMPTS`, `HTTP_RETRY_WAIT_SECONDS`).

## Build/lint checks

Each backend service is its own Go module (linked via the root `go.work`), so run these per module:

```bash
for d in backend/shared backend/grade-service backend/repository-service backend/security-wrapper-service; do
  (cd "$d" && go build ./... && go vet ./...)
done
gofmt -l backend         # should print nothing
```

## Manual-tagged tests

One test needs a live MinIO with data already uploaded (not run by default `go test ./...`):

```bash
(cd backend/shared && go test -tags=manual ./storage/ -run TestBatchReadManual -v)
```

## CI/CD mock repos

Five public repos under `timoxa102928` with known CI histories — see `docs/ci-mock-repos.json`
for IDs and expected scores. Needs a PAT that owns that org (`cicd/runs` is owner-only).

Offline fixture tests (part of plain `go test ./...`):

```bash
(cd backend/grade-service && go test ./internal/scoring/ -run CICD -v)
```

Live check against the real API (scoring only, reads runs straight from REST):

```bash
(cd backend/grade-service && go test -tags live ./internal/scoring/ -run LiveMockRepos -v)
```

Full pipeline for one repo (sourcecraft-wrapper-service must be running):

```bash
go run ./backend/repository-service/cmd/repository-service --org=timoxa102928 --repo=ci-mock-flaky
go run ./backend/grade-service/cmd/grade-service --repo-id=01a0dd5d-d258-7af2-9b1b-b2346d6d29b6 | jq .categories.ci_cd
```

Recreating the repos: `scripts/mock-repos/create-all.sh` (skips repos that already have content;
`--force` re-pushes and appends new runs, which changes expected scores). Newly pushed runs queue
on SourceCraft for a few minutes — the live test fails fast while any run is still
`prepared`/`processing`.
