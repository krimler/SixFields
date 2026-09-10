#!/usr/bin/env bash
# Print the KUBEBUILDER_ASSETS path for the pinned Kubernetes version, fetching it once.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
export GOBIN="${PWD}/bin"
if ! command -v "${GOBIN}/setup-envtest" >/dev/null; then
  go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VERSION}" >&2
fi
"${GOBIN}/setup-envtest" use "${ENVTEST_K8S_VERSION}" --os darwin --arch "$(uname -m | sed 's/x86_64/amd64/')" -p path --bin-dir "${HOME}/.cache/envtest"
