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

# k0smotron is required, not optional: the std class bootstraps with k0s and the
# std-hosted class runs its control plane as k0smotron pods. Set
# WITH_K0SMOTRON=false to install only the kubeadm providers, which is enough for
# the std-kubeadm fallback and the std-inmemory substrate.
if [[ "${WITH_K0SMOTRON:-true}" == "true" ]]; then
  args+=(--bootstrap "k0sproject-k0smotron:${K0SMOTRON_VERSION}"
         --control-plane "k0sproject-k0smotron:${K0SMOTRON_VERSION}")
fi

clusterctl init "${args[@]}"
