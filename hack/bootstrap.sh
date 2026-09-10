#!/usr/bin/env bash
# Install every pinned tool. Re-runnable; installs nothing that is already correct.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env

if ! command -v brew >/dev/null; then
  echo "bootstrap: Homebrew is required: https://brew.sh" >&2
  exit 4
fi

brew bundle --file=Brewfile

go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VERSION}"
go install "gotest.tools/gotestsum@${GOTESTSUM_VERSION}"

echo "bootstrap: done. Next: make doctor"
