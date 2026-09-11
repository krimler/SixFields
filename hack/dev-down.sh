#!/usr/bin/env bash
# Delete the management cluster and everything it created.
#
# `kind delete cluster` on its own does not do this: CAPD's workload machines are
# containers outside the management cluster, so deleting the management cluster
# orphans them. They keep running, keep their memory, and have no controller left
# to clean them up. This deletes the workload clusters first, through CAPI, so the
# provider removes its own containers, and sweeps up anything left behind.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
KIND_CLUSTER=${KIND_CLUSTER:-sixfields}

if kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  kubectl config use-context "kind-${KIND_CLUSTER}" >/dev/null 2>&1 || true

  clusters=$(kubectl get clusters --all-namespaces \
    -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
  if [[ -n "$clusters" ]]; then
    echo "dev-down: deleting workload clusters so the provider removes its containers"
    while IFS= read -r cluster; do
      [[ -z "$cluster" ]] && continue
      kubectl delete cluster "${cluster##*/}" -n "${cluster%%/*}" --timeout=5m || true
    done <<< "$clusters"
  fi
  kind delete cluster --name "$KIND_CLUSTER"
fi

# Anything the provider did not get to, because the management cluster was
# already gone, or a delete timed out. CAPD labels every container it creates.
orphans=$(docker ps -aq --filter "label=io.x-k8s.kind.cluster" 2>/dev/null || true)
if [[ -n "$orphans" ]]; then
  echo "dev-down: removing $(wc -w <<< "$orphans" | tr -d ' ') orphaned workload container(s)"
  docker rm -f $orphans >/dev/null
fi

echo "dev-down: clean."
