#!/usr/bin/env bash
# One command: starts infra + services, grades every repo in docs/candidate-repos.json with
# passes_full_criteria=true through the full Kafka flow, prints each grading.results message, then
# stops the services. Runs sequentially: grading-request reads the next result on the topic, so
# parallel requests would mix up results. Repos without a result are retried once at the end.
#
# Only infra containers are started. A running go-services container (which bundles older builds
# of the same services and would consume the batch's messages) is stopped for the batch and
# started again on exit.
#
# Needs .env (copy .env.example, fill SOURCECRAFT_PAT). Each request waits up to 15m by default;
# extra args go to grading-request and override it (e.g. --wait=20m). Service logs:
# tmp/grade-candidates-logs/.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/tmp/bin"
LOGS="$ROOT/tmp/grade-candidates-logs"
mkdir -p "$BIN" "$LOGS"

[ -f "$ROOT/.env" ] || { echo "missing .env (cp .env.example .env, fill SOURCECRAFT_PAT)"; exit 1; }
set -a; source "$ROOT/.env"; set +a
export SOURCECRAFT_URL="${SOURCECRAFT_URL:-https://api.sourcecraft.tech}"

GO_SERVICES_WAS_RUNNING=false
if [ -n "$(cd "$ROOT" && docker compose ps -q --status running go-services 2>/dev/null)" ]; then
  GO_SERVICES_WAS_RUNNING=true
  echo "stopping go-services for the batch (started again on exit)"
  (cd "$ROOT" && docker compose stop go-services) || exit 1
fi

# Binaries run directly (not go run) so kill reaches the actual process.
PIDS=()
cleanup() {
  [ "${#PIDS[@]}" -gt 0 ] && kill "${PIDS[@]}" 2>/dev/null
  wait 2>/dev/null
  if [ "$GO_SERVICES_WAS_RUNNING" = true ]; then
    echo "starting go-services again"
    (cd "$ROOT" && docker compose start go-services)
  fi
}
trap cleanup EXIT
trap 'echo; echo interrupted; exit 130' INT TERM

(cd "$ROOT" && docker compose up -d postgres kafka minio redis migrate) || exit 1

echo "building..."
(cd "$ROOT/backend/sourcecraft-wrapper-service" && go build -o "$BIN/sc-wrapper" ./cmd) &&
(cd "$ROOT/backend/repository-service" && go build -o "$BIN/repository-service" ./cmd/repository-service) &&
(cd "$ROOT/backend/grade-service" && go build -o "$BIN/grade-service" ./cmd/grade-service &&
  go build -o "$BIN/grading-request" ./cmd/grading-request) || exit 1

"$BIN/sc-wrapper" >"$LOGS/sc-wrapper.log" 2>&1 & PIDS+=($!)
"$BIN/repository-service" --listen >"$LOGS/repository-service.log" 2>&1 & PIDS+=($!)
"$BIN/grade-service" --listen >"$LOGS/grade-service.log" 2>&1 & PIDS+=($!)
sleep 5
for p in "${PIDS[@]}"; do
  kill -0 "$p" 2>/dev/null || { echo "a service exited on startup, see $LOGS"; exit 1; }
done

REPOS=()
while read -r line; do REPOS+=("$line"); done < <(jq -r '.repos[] | select(.passes_full_criteria) | "\(.org) \(.repo)"' "$ROOT/docs/candidate-repos.json")

# run_pass label repos...: grades each "org repo" entry; failed ones end up in FAILED.
FAILED=()
run_pass() {
  local label="$1"; shift
  FAILED=()
  for entry in "$@"; do
    read -r org repo <<<"$entry"
    # --wait=15m comes first so a caller's --wait in EXTRA_ARGS overrides it.
    echo "=== $org/$repo ($label) ==="
    "$BIN/grading-request" --org="$org" --repo="$repo" --wait=15m ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"} </dev/null
    rc=$?
    [ "$rc" -ge 128 ] && exit "$rc"
    if [ "$rc" -ne 0 ]; then
      echo "!!! $org/$repo: no result (see $LOGS)"
      FAILED+=("$entry")
    fi
    echo
  done
}

EXTRA_ARGS=("$@")
run_pass "first pass" "${REPOS[@]}"
FIRST_FAILED=("${FAILED[@]+"${FAILED[@]}"}")
STILL_FAILED=()
if [ "${#FIRST_FAILED[@]}" -gt 0 ]; then
  echo "retrying ${#FIRST_FAILED[@]} repo(s) without a result"
  run_pass "retry" "${FIRST_FAILED[@]}"
  STILL_FAILED=("${FAILED[@]+"${FAILED[@]}"}")
fi

echo "=== summary ==="
echo "graded: $(( ${#REPOS[@]} - ${#STILL_FAILED[@]} )) of ${#REPOS[@]} (recovered on retry: $(( ${#FIRST_FAILED[@]} - ${#STILL_FAILED[@]} )))"
for entry in "${STILL_FAILED[@]+"${STILL_FAILED[@]}"}"; do echo "still failed: ${entry/ //}"; done
