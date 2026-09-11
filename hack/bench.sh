#!/usr/bin/env bash
# cluster-bench: one task list, one backend at a time, scored against the fixtures.
# BACKENDS picks which: noop,cassette (the default) need no model and no network.
set -euo pipefail
cd "$(dirname "$0")/.."
# Exported, not just sourced: the local backend reads CLUSTER_AI_URL and
# CLUSTER_AI_MODEL from the environment of the test binary.
set -a
source versions.env
set +a

BACKENDS=${BACKENDS:-noop,cassette}

if [[ "${PROFILE:-dev}" != "bench" ]]; then
  echo "bench: the bench profile only. It runs alone — 4 GB of VM, one resident model," >&2
  echo "       nothing else (PLAN.md D1). Two steps:" >&2
  echo "         make profile P=bench      # resize the container VM and wait for it" >&2
  echo "         PROFILE=bench make bench  # or: export PROFILE=bench" >&2
  exit 4
fi

# Only a model-backed backend needs the memory math. The default two answer from
# the analyzer and from recorded cassettes, which is why they are the default.
if [[ "$BACKENDS" == *local* || "$BACKENDS" == *anthropic* ]]; then
  hack/doctor-ai.sh
fi

# -count 1 because a cached result prints no report, and -v because `go test`
# shows a passing package's stdout only in verbose mode.
go test -tags bench -timeout 60m -count 1 -v ./internal/bench/ -run TestBench -backends "$BACKENDS"
