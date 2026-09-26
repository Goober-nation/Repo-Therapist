#!/usr/bin/env bash
# Creates CI/CD mock repos under timoxa102928. Each push to main = one CI run; the
# `outcome` file (0/1) decides pass/fail, `sleep_s` sets rough duration.
# Usage: setup.sh [--force] <slug> <description> <mode> [outcomes...]
# Needs SOURCECRAFT_PAT (owner of the org) in the environment. Idempotent by default: a repo that
# already has content is left alone; --force re-pushes (history rewritten, NEW runs appended —
# old runs stay, so expected scores in docs/ci-mock-repos.json may no longer hold).
# The repos currently in use were created 2026-09-26 with the invocations in create-all.sh.
#   mode: ci       -> config triggers on main, one push per outcome
#         dormant  -> config triggers only on branch "never", runs never happen
#         none     -> no .sourcecraft/ci.yaml at all
set -euo pipefail
FORCE=0; [ "${1:-}" = --force ] && { FORCE=1; shift; }
ORG=${MOCK_ORG:-timoxa102928}
SLUG=$1; DESC=$2; MODE=$3; shift 3
API=https://api.sourcecraft.tech
REMOTE="https://oauth:${SOURCECRAFT_PAT}@git.sourcecraft.dev/${ORG}/${SLUG}.git"
W="$(mktemp -d)/$SLUG"

code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $SOURCECRAFT_PAT" "$API/repos/$ORG/$SLUG")
if [ "$code" = 404 ]; then
  curl -sf -X POST -H "Authorization: Bearer $SOURCECRAFT_PAT" -H "Content-Type: application/json" \
    "$API/orgs/$ORG/repos" -d "{\"slug\":\"$SLUG\",\"description\":\"$DESC\",\"visibility\":\"public\"}" >/dev/null
  echo "created $SLUG"
else
  empty=$(curl -sf -H "Authorization: Bearer $SOURCECRAFT_PAT" "$API/repos/$ORG/$SLUG" | sed -n 's/.*"is_empty":\([a-z]*\).*/\1/p')
  if [ "$empty" != true ] && [ "$FORCE" = 0 ]; then
    echo "$SLUG exists and has content, skipping (use --force to re-push)"; exit 0
  fi
  echo "$SLUG exists ($code), pushing"
fi

rm -rf "$W"; mkdir -p "$W"; cd "$W"
git init -q -b main; git config user.name mock-bot; git config user.email mock-bot@example.invalid
echo "# $SLUG" > README.md
echo "$DESC" >> README.md

branch=main; [ "$MODE" = dormant ] && branch=never
if [ "$MODE" != none ]; then
  mkdir -p .sourcecraft
  cat > .sourcecraft/ci.yaml <<EOF
on:
  push:
    - workflows: [build]
      filter:
        branches: [$branch]
workflows:
  build:
    tasks:
      - name: test
        cubes:
          - name: run
            script:
              - sleep \$(cat sleep_s)
              - exit \$(cat outcome)
EOF
fi

push() { git push -q -f "$REMOTE" main 2>&1 | sed "s/${SOURCECRAFT_PAT}/***/g" || true; }

if [ "$MODE" != ci ]; then
  echo 0 > outcome; echo 1 > sleep_s
  git add -A; git commit -qm "init"; push; echo "$SLUG: pushed ($MODE)"; exit 0
fi

i=0
for o in "$@"; do
  i=$((i+1))
  echo "$o" > outcome; echo $((i % 3 + 2)) > sleep_s
  git add -A; git commit -qm "run $i: outcome=$o"; push
  echo "$SLUG: push $i outcome=$o"
  sleep 20   # let the previous run register before the next push
done
