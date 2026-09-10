#!/usr/bin/env bash
# Resize the container VM for a profile. Resizing restarts the runtime, so this
# waits for it and re-runs dev-up, which is idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."
p=${1:-}
case "$p" in
  dev)   gb=5  ;;
  e2e)   gb=10 ;;
  bench) gb=4  ;;
  *) echo "usage: make profile P=dev|e2e|bench" >&2; exit 2 ;;
esac

ctx=$(docker context show 2>/dev/null || echo default)
case "$ctx" in
  colima)        colima stop && colima start --memory "$gb" ;;
  orbstack)      echo "profile: set memory to ${gb} GB in OrbStack → Settings → System, then re-run" >&2; exit 4 ;;
  desktop-linux) echo "profile: set memory to ${gb} GB in Docker Desktop → Settings → Resources, then re-run" >&2; exit 4 ;;
  *)             echo "profile: unknown docker context '$ctx'; set the VM to ${gb} GB by hand" >&2; exit 4 ;;
esac

until docker info >/dev/null 2>&1; do sleep 2; done
PROFILE="$p" hack/doctor.sh
PROFILE="$p" hack/dev-up.sh
