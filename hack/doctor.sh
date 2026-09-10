#!/usr/bin/env bash
# Verify the machine can run the dev loop. Every failure names the fix.
set -uo pipefail
cd "$(dirname "$0")/.."
source versions.env

fail=0
ok()   { printf "  \033[32mok\033[0m    %s\n" "$1"; }
warn() { printf "  \033[33mwarn\033[0m  %s\n" "$1"; }
bad()  { printf "  \033[31mfail\033[0m  %s\n" "$1"; fail=1; }

echo "tools"
check_version() { # name  actual  want
  if [[ -z "${2:-}" ]]; then bad "$1 is not installed — run: make bootstrap"; return; fi
  if [[ "$2" == *"${3#v}"* ]]; then ok "$1 $2"; else warn "$1 $2 (versions.env pins $3)"; fi
}
check_version go         "$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')" "$GO_VERSION"
check_version kind       "$(kind version 2>/dev/null | awk '{print $2}')"               "$KIND_VERSION"
check_version kubectl    "$(kubectl version --client -o json 2>/dev/null | jq -r .clientVersion.gitVersion)" "$KUBECTL_VERSION"
check_version clusterctl "$(clusterctl version -o short 2>/dev/null)"                   "$CLUSTERCTL_VERSION"
for t in kustomize golangci-lint yamllint kubeconform jq; do
  command -v "$t" >/dev/null && ok "$t" || bad "$t is not installed — run: make bootstrap"
done

echo "architecture"
arch=$(uname -m)
if [[ "$arch" == "arm64" ]]; then
  ok "darwin/arm64"
  for img in "$MGMT_NODE_IMAGE" "$WORKLOAD_NODE_IMAGE"; do
    if command -v docker >/dev/null && docker manifest inspect "$img" >/dev/null 2>&1; then
      if docker manifest inspect "$img" 2>/dev/null | jq -e '.manifests[]?|select(.platform.architecture=="arm64")' >/dev/null; then
        ok "$img has an arm64 manifest"
      else
        bad "$img has no arm64 manifest — it will fail with 'exec format error'"
      fi
    else
      warn "could not inspect $img (registry unreachable or docker down)"
    fi
  done
else
  ok "$arch"
fi

echo "container runtime"
ctx=$(docker context show 2>/dev/null || echo "")
sock="${DOCKER_HOST:-}"
if [[ -z "$sock" && -n "$ctx" ]]; then
  sock=$(docker context inspect "$ctx" 2>/dev/null | jq -r '.[0].Endpoints.docker.Host // empty')
fi
sock=${sock#unix://}
if docker info >/dev/null 2>&1; then
  ok "runtime up (context '$ctx', socket $sock)"
  # kind mounts this path into the management cluster so CAPD can reach the runtime.
  [[ -S "$sock" ]] && ok "socket exists for the kind extraMount" || bad "socket $sock is not a socket"
else
  bad "no container runtime — start Docker Desktop, OrbStack or Colima"
fi

echo "memory profile ${PROFILE:-dev}"
total_gb=$(( $(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1024 / 1024 / 1024 ))
case "${PROFILE:-dev}" in
  dev)   vm=5;  model=9 ;;
  e2e)   vm=10; model=0 ;;
  bench) vm=4;  model=15 ;;
  *)     bad "unknown PROFILE '${PROFILE:-}' (dev|e2e|bench)"; vm=0; model=0 ;;
esac
reserve=5
need=$((vm + model + reserve))
if (( total_gb == 0 )); then
  warn "cannot read physical memory"
elif (( need <= total_gb )); then
  ok "${total_gb} GB physical; profile needs ${vm} GB VM + ${model} GB model + ${reserve} GB macOS = ${need} GB"
else
  bad "${total_gb} GB physical but profile needs ${need} GB — run: make profile P=dev"
fi
if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
  vm_actual=$(( $(docker info --format '{{.MemTotal}}' 2>/dev/null || echo 0) / 1024 / 1024 / 1024 ))
  if (( vm_actual > 0 )) && (( vm_actual != vm )); then
    warn "container VM has ${vm_actual} GB, profile ${PROFILE:-dev} wants ${vm} GB — run: make profile P=${PROFILE:-dev}"
  fi
fi

echo "envtest"
if assets=$(hack/envtest-assets.sh 2>/dev/null); then ok "assets at $assets"; else warn "envtest assets missing — run: make envtest"; fi

exit $fail
