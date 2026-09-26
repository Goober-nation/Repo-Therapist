#!/usr/bin/env bash
# Creates every CI/CD mock repo listed in docs/ci-mock-repos.json. Runs in parallel; CI runs queue
# on SourceCraft's side, allow ~5 minutes after this exits before every run has finished.
set -euo pipefail
D="$(dirname "$0")"
"$D/setup.sh" "$@" ci-mock-green "Mock repo for CI/CD grading tests: CI always passes" ci 0 0 0 0 0 0 0 &
"$D/setup.sh" "$@" ci-mock-red "Mock repo for CI/CD grading tests: CI always fails" ci 1 1 1 1 1 1 1 1 &
"$D/setup.sh" "$@" ci-mock-flaky "Mock repo for CI/CD grading tests: flaky and degrading CI (older half 75% pass, recent half 25%)" ci 0 0 0 1 0 1 1 1 &
"$D/setup.sh" "$@" ci-mock-dormant "Mock repo for CI/CD grading tests: CI config present, never triggered" dormant &
"$D/setup.sh" "$@" ci-mock-none "Mock repo for CI/CD grading tests: no CI at all" none &
wait
