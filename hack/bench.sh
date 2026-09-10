#!/usr/bin/env bash
# cluster-bench: one agent binary, one task list, three backends, sequential.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
if [[ "${PROFILE:-dev}" != "bench" ]]; then
  echo "bench: requires PROFILE=bench (the VM must be at 4 GB and nothing else resident)" >&2
  echo "       run: make profile P=bench" >&2
  exit 4
fi
hack/doctor-ai.sh
go test -tags bench -timeout 60m ./internal/bench/... -run TestBench -v
