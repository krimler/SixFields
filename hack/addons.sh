#!/usr/bin/env bash
# Install the assembly's add-ons as a ClusterResourceSet, so every cluster the
# class creates gets a CNI without the user knowing there is one. Without it the
# nodes join and stay NotReady, and the cluster never reaches Ready — which is
# how the missing add-on was found.
#
# The manifest is pinned by digest. A tag can be moved; a digest cannot.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env

mkdir -p bin/addons
manifest="bin/addons/${CNI_NAME}-${CNI_VERSION}.yaml"

if [[ ! -f "$manifest" ]]; then
  curl -sSL -o "$manifest" "$CNI_MANIFEST_URL"
fi
actual=$(shasum -a 256 "$manifest" | awk '{print $1}')
if [[ "$actual" != "$CNI_SHA256" ]]; then
  echo "addons: $CNI_MANIFEST_URL has digest $actual, versions.env pins $CNI_SHA256" >&2
  echo "addons: either the release was re-tagged or the download is corrupt. Do not proceed." >&2
  rm -f "$manifest"
  exit 1
fi

# The ConfigMap holds the manifest; the ClusterResourceSet applies it to every
# cluster the class labels. ApplyOnce is the default and is what we want: the CNI
# is installed once and then owned by whoever operates the workload cluster.
# Server-side apply: a CNI manifest is larger than the 256KB limit on the
# last-applied-configuration annotation that client-side apply writes.
kubectl create configmap "${CNI_NAME}-${CNI_VERSION}" \
  --from-file="${CNI_NAME}.yaml=${manifest}" \
  --dry-run=client -o yaml | kubectl apply --server-side --force-conflicts -f -

kubectl apply -f - <<YAML
apiVersion: addons.cluster.x-k8s.io/v1beta2
kind: ClusterResourceSet
metadata:
  name: ${CNI_NAME}
  labels:
    sixfields.io/break-glass: "true"
spec:
  strategy: ApplyOnce
  # CAPI labels every Cluster built from a ClusterClass with
  # topology.cluster.x-k8s.io/owned (verified on a live object; the name is in
  # docs/api-snapshot.md). Selecting on it matches exactly the clusters this
  # assembly creates and no hand-made one, and costs the user no field. An empty
  # selector would have been simpler but CAPI rejects it.
  clusterSelector:
    matchExpressions:
    - key: topology.cluster.x-k8s.io/owned
      operator: Exists
  resources:
  - name: ${CNI_NAME}-${CNI_VERSION}
    kind: ConfigMap
YAML
