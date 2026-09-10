# Node images

Change which image a cluster boots, and know which file to edit for which cluster.

## What is pinned, and where

Every image reference lives in `versions.env`. Nothing in `assembly/` names a tag.

| variable | boots | reaches the cluster through |
|---|---|---|
| `MGMT_NODE_IMAGE` | the kind management cluster | `hack/kind-config.yaml`, read by `hack/dev-up.sh` |
| `WORKLOAD_NODE_IMAGE` | control-plane and worker machines of a workload cluster | the `nodeImage` variable of ClusterClass `std`, applied to `DevMachineTemplate.spec.template.spec.backend.docker.customImage` by the `nodeImage` patch |

Both are `kindest/node:<version>@sha256:<digest>`. The digest is the pin; the tag
is there so a human can read it.

`WORKLOAD_NODE_IMAGE` reaches the class as the literal `${WORKLOAD_NODE_IMAGE}`
default of the `nodeImage` variable in
`assembly/clusterclass/base/clusterclass.yaml`. `hack/render.sh` pipes
`kustomize build` through `envsubst`, so the substitution happens after kustomize
and only in the rendered output.

The `inmemory` overlay boots nothing: its machines have no image
(`InMemoryMachineBackendSpec` in
`sigs.k8s.io/cluster-api/test@v1.14.2 infrastructure/docker/api/v1beta2/devmachine_types.go:357`).
It carries the `nodeImage` variable anyway so both overlays accept the same
Cluster.

## Change one

1. Find the digest for the tag you want. The kind release notes list them, or:

   ```
   docker pull kindest/node:v1.34.11
   docker inspect --format='{{index .RepoDigests 0}}' kindest/node:v1.34.11
   ```

2. Edit `versions.env`. `WORKLOAD_NODE_IMAGE` and `WORKLOAD_K8S_VERSION` must name
   the same Kubernetes version: the image is what the machine boots, the version
   is what `Cluster.spec.topology.version` asks for, and kubeadm compares them.

3. Re-render and refresh the goldens:

   ```
   make render
   cp bin/render/docker.yaml testdata/golden/class-docker.yaml
   cp bin/render/inmemory.yaml testdata/golden/class-inmemory.yaml
   ```

4. `make lint` (kubeconform runs against the rendered output) and `make test`.

Changing `MGMT_NODE_IMAGE` needs `make dev-down && make dev-up`; kind does not
re-image a running cluster.
