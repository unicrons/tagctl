#!/usr/bin/env bash
# Checks commit subjects (and an optional PR title) against Conventional Commits.
# Usage: check-commits.sh <base-ref> [pr-title]
set -euo pipefail

pattern='^(feat|fix|docs|test|refactor|chore|ci|deps|perf)\([a-z0-9._/-]+\)!?: [^ ].*$'
max=60
failed=0

check() {
  local subject="$1" label="$2"
  if [[ ! "$subject" =~ $pattern ]]; then
    echo "::error::$label does not follow type(scope): summary -> \"$subject\""
    failed=1
  elif (( ${#subject} > max )); then
    echo "::error::$label exceeds $max characters (${#subject}) -> \"$subject\""
    failed=1
  fi
}

while IFS= read -r line; do
  check "${line#* }" "commit ${line%% *}"
done < <(git log --no-merges --format='%h %s' "$1..HEAD")

if [[ -n "${2:-}" ]]; then
  check "$2" "PR title"
fi

exit $failed
