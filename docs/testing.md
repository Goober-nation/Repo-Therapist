# Manual testing

No automated test suite yet (tracked as an open item in `backlog/`) — this is how to exercise
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

Then trigger a grade from a third terminal:

```bash
go run ./backend/grade-service/cmd/grade-service --request-org=examples --request-repo=vulnerable-repo
```

Watch the two listener terminals: Repository service logs the clone/upload and publishes
`repo.ready`; Grade service logs the grading and prints the same JSON result as step 2.

**Note**: if testing `--listen` processes via `go run ... &` in a shell script, `kill <pid>` won't
stop the actual compiled binary (`go run`'s signal handling doesn't propagate to its child) — build
first and run the binary directly, or use `pkill -f <binary-name>`, if you need to script
start/stop.

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
