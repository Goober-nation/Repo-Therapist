#!/usr/bin/env bash
# One command: starts infra + services, grades every repo in docs/candidate-repos.json with
# passes_full_criteria=true through the full Kafka flow, prints each grading.results message, then
# stops the services. Runs sequentially: grading-request reads the next result on the topic, so
# parallel requests would mix up results.
#
# Needs .env (copy .env.example, fill SOURCECRAFT_PAT). Extra args (e.g. --wait=5m) go to
# grading-request. Service logs: tmp/grade-candidates-logs/.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/tmp/bin"
LOGS="$ROOT/tmp/grade-candidates-logs"
mkdir -p "$BIN" "$LOGS"

[ -f "$ROOT/.env" ] || { echo "missing .env (cp .env.example .env, fill SOURCECRAFT_PAT)"; exit 1; }
set -a; source "$ROOT/.env"; set +a
export SOURCECRAFT_URL="${SOURCECRAFT_URL:-https://api.sourcecraft.tech}"

(cd "$ROOT" && docker compose up -d) || exit 1

echo "building..."
(cd "$ROOT/backend/sourcecraft-wrapper-service" && go build -o "$BIN/sc-wrapper" ./cmd) &&
(cd "$ROOT/backend/repository-service" && go build -o "$BIN/repository-service" ./cmd/repository-service) &&
(cd "$ROOT/backend/grade-service" && go build -o "$BIN/grade-service" ./cmd/grade-service &&
  go build -o "$BIN/grading-request" ./cmd/grading-request) || exit 1

# Binaries run directly (not go run) so kill reaches the actual process.
PIDS=()
cleanup() { kill "${PIDS[@]}" 2>/dev/null; wait 2>/dev/null; }
trap cleanup EXIT
trap 'echo; echo interrupted; exit 130' INT TERM
"$BIN/sc-wrapper" >"$LOGS/sc-wrapper.log" 2>&1 & PIDS+=($!)
"$BIN/repository-service" --listen >"$LOGS/repository-service.log" 2>&1 & PIDS+=($!)
"$BIN/grade-service" --listen >"$LOGS/grade-service.log" 2>&1 & PIDS+=($!)
sleep 5
for p in "${PIDS[@]}"; do
  kill -0 "$p" 2>/dev/null || { echo "a service exited on startup, see $LOGS"; exit 1; }
done

# Process substitution (not a pipe) keeps the loop in this shell, so Ctrl-C stops everything.
while read -r org repo; do
  echo "=== $org/$repo ==="
  "$BIN/grading-request" --org="$org" --repo="$repo" "$@" </dev/null
  rc=$?
  [ "$rc" -ge 128 ] && exit "$rc"
  [ "$rc" -ne 0 ] && echo "!!! $org/$repo: no result (see $LOGS)"
  echo
done < <(jq -r '.repos[] | select(.passes_full_criteria) | "\(.org) \(.repo)"' "$ROOT/docs/candidate-repos.json")
