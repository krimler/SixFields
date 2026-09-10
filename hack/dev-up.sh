#!/usr/bin/env bash
# kind management cluster + CAPI providers + the assembly. Idempotent: re-running
# is a no-op when everything is already in place, so e2e can reuse one cluster.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
KIND_CLUSTER=${KIND_CLUSTER:-capi-distro}

if ! docker info >/dev/null 2>&1; then
  echo "dev-up: no container runtime. Start Docker Desktop, OrbStack or Colima, then re-run." >&2
  exit 4
fi

if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  DOCKER_SOCKET=$(hack/docker-socket.sh) \
  MGMT_NODE_IMAGE="$MGMT_NODE_IMAGE" \
  envsubst < hack/kind-config.yaml | kind create cluster --name "$KIND_CLUSTER" --config -
fi
kubectl config use-context "kind-${KIND_CLUSTER}"

hack/clusterctl-init.sh
kubectl wait --for=condition=Available --timeout=5m -n capi-system deployment/capi-controller-manager

hack/render.sh
kubectl apply -f "bin/render/${OVERLAY:-docker}.yaml"

if [[ "${WITH_POLICY:-true}" == "true" ]]; then
  kubectl apply -f policy/vap/
fi

echo "dev-up: ready. Next: cluster up dev-1 -f examples/dev-1.yaml"
