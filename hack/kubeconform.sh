#!/usr/bin/env bash
# Validate the assembly and policy against the pinned CRD schemas.
set -euo pipefail
cd "$(dirname "$0")/.."
source versions.env
hack/render.sh >/dev/null
kubeconform -strict -ignore-missing-schemas \
  -kubernetes-version "${MGMT_K8S_VERSION#v}" \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  bin/render/*.yaml policy/vap/*.yaml
