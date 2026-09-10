#!/usr/bin/env bash
# Blast radius of a ClusterClass edit, before apply.
set -euo pipefail
cd "$(dirname "$0")/.."
hack/render.sh >/dev/null
clusterctl alpha topology plan -f "bin/render/${OVERLAY:-docker}.yaml" "$@"
