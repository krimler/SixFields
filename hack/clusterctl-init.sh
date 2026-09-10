#!/usr/bin/env bash
# clusterctl init with every provider pinned from versions.env. Idempotent:
# clusterctl skips providers that are already at the requested version.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env

export CLUSTER_TOPOLOGY=true
export EXP_CLUSTER_RESOURCE_SET=true

args=(--core "cluster-api:${CAPI_VERSION}"
      --bootstrap "kubeadm:${CAPI_VERSION}"
      --control-plane "kubeadm:${CAPI_VERSION}"
      --infrastructure "docker:${CAPD_VERSION}")

# k0smotron ships bootstrap, control-plane and infrastructure providers under one
# name. Phases 4-5 need it; Phases 0-3 do not, so it is opt-in.
if [[ "${WITH_K0SMOTRON:-false}" == "true" ]]; then
  args+=(--bootstrap "k0sproject-k0smotron:${K0SMOTRON_VERSION}"
         --control-plane "k0sproject-k0smotron:${K0SMOTRON_VERSION}")
fi

clusterctl init "${args[@]}"
