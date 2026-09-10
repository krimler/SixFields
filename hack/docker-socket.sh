#!/usr/bin/env bash
# Print the host socket path for the active container runtime. Docker Desktop,
# OrbStack and Colima all differ; kind's extraMount needs the real path.
set -euo pipefail
if [[ -n "${DOCKER_HOST:-}" ]]; then
  echo "${DOCKER_HOST#unix://}"
  exit 0
fi
ctx=$(docker context show 2>/dev/null || echo default)
host=$(docker context inspect "$ctx" 2>/dev/null | jq -r '.[0].Endpoints.docker.Host // empty')
if [[ -n "$host" ]]; then
  echo "${host#unix://}"
  exit 0
fi
echo /var/run/docker.sock
