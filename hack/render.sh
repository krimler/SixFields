#!/usr/bin/env bash
# Render every ClusterClass overlay to one file each. The rendered output is the
# reviewable artifact; testdata/golden/class-*.yaml is checked against it.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
mkdir -p bin/render
for overlay in assembly/clusterclass/overlays/*/; do
  name=$(basename "$overlay")
  WORKLOAD_NODE_IMAGE="$WORKLOAD_NODE_IMAGE" \
  WORKLOAD_K8S_VERSION="$WORKLOAD_K8S_VERSION" \
  K0S_VERSION="$K0S_VERSION" \
    kustomize build "$overlay" | envsubst > "bin/render/${name}.yaml"
  echo "bin/render/${name}.yaml"
done
