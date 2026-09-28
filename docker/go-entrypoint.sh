#!/usr/bin/env bash
# Starts each Go service with its own env overrides; exits when any of them exits.
set -uo pipefail

GO_SERVICES="${GO_SERVICES:-aggregator-service sourcecraft-wrapper-service security-wrapper-service repository-service grade-service rating-service gateway-service}"

start() {
	case "$1" in
	gateway-service) gateway-service & ;;
	rating-service) GRPC_ADDRESS=:8086 rating-service & ;;
	sourcecraft-wrapper-service) APP_PORT=8082 sourcecraft-wrapper-service & ;;
	security-wrapper-service) APP_PORT=8085 security-wrapper-service & ;;
	aggregator-service) GRPC_ADDRESS=:8083 aggregator-service & ;;
	repository-service) repository-service --listen & ;;
	grade-service) grade-service --listen & ;;
	*) echo "unknown service: $1" >&2; exit 2 ;;
	esac
	echo "started $1 (pid $!)"
}

for s in $GO_SERVICES; do start "$s"; done

trap 'kill 0' TERM INT
wait -n
code=$?
echo "a service exited with code $code, stopping container" >&2
kill 0
exit "$code"
