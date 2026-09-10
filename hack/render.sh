#!/usr/bin/env bash
# Render every ClusterClass overlay to one file each. The rendered output is the
# reviewable artifact; testdata/golden/class-*.yaml is checked against it.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
# envsubst is the second stage of the pipeline, so the values have to be in the
# environment; a `VAR=... cmd` prefix would only reach kustomize.
export WORKLOAD_NODE_IMAGE WORKLOAD_K8S_VERSION K0S_VERSION
mkdir -p bin/render
for overlay in assembly/clusterclass/overlays/*/; do
  name=$(basename "$overlay")
  kustomize build "$overlay" \
    | envsubst '$WORKLOAD_NODE_IMAGE $WORKLOAD_K8S_VERSION $K0S_VERSION' > "bin/render/${name}.yaml"
  echo "bin/render/${name}.yaml"
done
