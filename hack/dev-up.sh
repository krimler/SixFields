#!/usr/bin/env bash
# kind management cluster + CAPI providers + the assembly. Idempotent: re-running
# is a no-op when everything is already in place, so e2e can reuse one cluster.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
KIND_CLUSTER=${KIND_CLUSTER:-sixfields}

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

# Every provider registers admitting webhooks, and applying the class calls all of
# them. Waiting only for the core manager is not enough: the first `make dev-up`
# on a clean machine failed with "connection refused" from the CAPD and kubeadm
# webhook services because their pods were still starting.
kubectl wait --for=condition=Available --timeout=5m --all-namespaces \
  --selector cluster.x-k8s.io/provider deployment

# A deployment is Available before its Service has endpoints. Wait for the
# endpoints too, or the apply below races the webhook it is about to call.
for service in $(kubectl get service --all-namespaces \
      --selector cluster.x-k8s.io/provider \
      -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}'); do
  namespace=${service%%/*}
  name=${service##*/}
  for _ in $(seq 1 60); do
    addresses=$(kubectl get endpointslice -n "$namespace" \
      -l "kubernetes.io/service-name=$name" -o jsonpath='{.items[*].endpoints[*].addresses[*]}' 2>/dev/null || true)
    [[ -n "$addresses" ]] && break
    sleep 2
  done
  [[ -n "${addresses:-}" ]] || echo "dev-up: $service still has no endpoints; the apply may fail" >&2
done

hack/render.sh

# The assembly is made of kinds the policy manages, so applying it is a
# break-glass write: the objects carry the label (see the base kustomization) and
# this supplies the group. Both halves are required, and the binding records the
# use in the audit log — which is the point. RBAC still comes from the caller, so
# system:masters is impersonated alongside.
for overlay in ${OVERLAYS:-docker hosted inmemory}; do
  [[ -f "bin/render/${overlay}.yaml" ]] || continue
  # The hosted class needs k0smotron; skip it rather than fail when it is absent.
  if [[ "$overlay" == "hosted" ]] && ! kubectl get crd k0smotroncontrolplanetemplates.controlplane.cluster.x-k8s.io >/dev/null 2>&1; then
    echo "dev-up: skipping the hosted class (k0smotron is not installed; WITH_K0SMOTRON=true installs it)"
    continue
  fi
  # A ClusterClass's control-plane kind cannot be changed in place: CAPI forbids
  # it "to prevent incompatible changes in the Clusters", the same rule that stops
  # a Cluster changing placement. Switching a class's bootstrap is therefore a
  # recreate, and that is only safe while no Cluster uses it.
  for class in $(awk '/^  name: /{name=$2} /^kind: ClusterClass$/{want=1} want && /^  name: /{print $2; want=0}' \
        "bin/render/${overlay}.yaml" | sort -u); do
    current=$(kubectl get clusterclass "$class" \
      -o jsonpath='{.spec.controlPlane.templateRef.kind}' 2>/dev/null || true)
    wanted=$(awk -v c="$class" '
      /^kind: ClusterClass$/{inclass=1} inclass && $0 ~ "^  name: "c"$"{found=1}
      found && /^      kind: /{print $2; exit}' "bin/render/${overlay}.yaml")
    [[ -z "$current" || -z "$wanted" || "$current" == "$wanted" ]] && continue

    users=$(kubectl get clusters --all-namespaces \
      -o jsonpath="{range .items[?(@.spec.topology.classRef.name==\"$class\")]}{.metadata.name} {end}" 2>/dev/null || true)
    if [[ -n "${users// /}" ]]; then
      echo "dev-up: ClusterClass $class changes control plane from $current to $wanted," >&2
      echo "        but these clusters use it: ${users}" >&2
      echo "        delete them first, or apply the new class under a different name." >&2
      exit 1
    fi
    echo "dev-up: recreating ClusterClass $class ($current -> $wanted); nothing uses it"
    kubectl delete clusterclass "$class"
  done

  kubectl apply -f "bin/render/${overlay}.yaml" \
    --as "${BREAK_GLASS_USER:-sixfields-installer}" \
    --as-group sixfields:break-glass \
    --as-group system:masters
done

hack/addons.sh

if [[ "${WITH_POLICY:-true}" == "true" ]]; then
  kubectl apply -f policy/vap/
fi

echo "dev-up: ready. Next: cluster up dev-1 -f examples/dev-1.yaml"
