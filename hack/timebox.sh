#!/usr/bin/env bash
# Assert a command finishes inside a wall-clock budget. Slowness is a red build.
set -euo pipefail
budget=$1; shift
start=$SECONDS
"$@"
elapsed=$((SECONDS - start))
if (( elapsed > budget )); then
  echo "timebox: '$*' took ${elapsed}s, budget ${budget}s" >&2
  echo "timebox: either make it faster or move the slow test behind a build tag." >&2
  exit 1
fi
echo "timebox: ${elapsed}s / ${budget}s"
