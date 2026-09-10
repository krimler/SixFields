# CAPI-VERSION-001 — the requested Kubernetes version is not available

After this page you can prove in one command whether the node image for
`spec.topology.version` exists for your architecture, and pick a version that does
without waiting for another ten-minute timeout.

## What you are seeing

```
KubeadmControlPlane/dev-1 — the requested Kubernetes version is not available (5m40s)
raw: kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default -o yaml
next: set spec.topology.version to a version the provider publishes
```

The `control plane` row of `cluster status` reads `stalled` at `0/1 nodes`, and it has
been there since the first machine was created. If workers are affected instead, the same
fault shows on the `workers` row with the same next action.

## What is actually true

There is no version-specific condition anywhere in CAPI v1.14.2 or CAPD v1.14.2. This
code is a ranking result, not a condition: `internal/why` reports it when a machine's
infrastructure never provisions and the provider's message is about the image. Do not go
looking for a `VersionAvailable` condition; it does not exist.

What you can read:

- The `Cluster`'s `ControlPlaneInitialized` is False with reason `NotInitialized`, and
  `ControlPlaneAvailable` is False with reason `NotAvailable`. Neither says why.
- The `Machine`'s `InfrastructureReady` is False with reason `NotReady`.
- The `DevMachine`'s `ContainerProvisioned` is False with reason `NotProvisioned`, and
  **its message carries the container runtime's image error**. That message is the only
  direct evidence, so it is the first thing to read.
- If the version string is malformed or the class's schema rejects it, the failure never
  reaches a machine: the `Cluster`'s `TopologyReconciled` is False with reason
  `ReconcileFailed` instead, which is `CAPI-TOPO-001`.

Two versions must agree. `spec.topology.version` on the Cluster is what kubeadm installs;
the image in `spec.backend.docker.customImage` on the DevMachineTemplate is what the
container boots. The class patches the second from its `nodeImage` variable, whose default
comes from `WORKLOAD_NODE_IMAGE` in `versions.env`. A version bump that does not move the
image produces exactly this stall.

## Check, in order

1. Read the image error.

   ```
   kubectl get devmachine.infrastructure.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}{end}'
   ```

   Good: `ContainerProvisioned False NotProvisioned` with a message naming an image and a
   pull failure. That confirms the code; go to step 2.
   Bad: `ContainerProvisioned False WaitingForBootstrapData` — the image is not the
   problem, bootstrap is: `cluster docs CAPI-CP-001`.

2. Compare the three versions that must agree.

   ```
   kubectl get cluster dev-1 -n default -o jsonpath='{.spec.topology.version}{"\n"}'
   kubectl get devmachinetemplate.infrastructure.cluster.x-k8s.io -n default \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.spec.template.spec.backend.docker.customImage}{"\n"}{end}'
   grep -E "WORKLOAD_K8S_VERSION|WORKLOAD_NODE_IMAGE" versions.env
   ```

   Good: the topology version, the tag in `customImage`, and `WORKLOAD_K8S_VERSION` are
   the same `vX.Y.Z`.
   Bad: the topology version is newer than the image tag — that is the fault. Either set
   the version back, or bump `WORKLOAD_NODE_IMAGE` in `versions.env` and re-apply the
   assembly.

3. Prove the image exists for this machine's architecture.

   ```
   docker pull kindest/node:v1.34.11
   docker image inspect kindest/node:v1.34.11 --format '{{.Os}}/{{.Architecture}}'
   uname -m
   ```

   Good: the pull succeeds and the architecture matches your host (`arm64` on Apple
   Silicon, `amd64` on Intel).
   Bad: `manifest unknown` — that tag was never published; pick another patch release.
   Also bad: the image is `amd64` on an `arm64` host — the machine boots and dies. kind
   publishes per-architecture digests; use the digest pinned in `versions.env`, not a
   floating tag.

4. Check the version is one this assembly is tested against.

   ```
   grep -E "MGMT_K8S_VERSION|WORKLOAD_K8S_VERSION|MGMT_NODE_IMAGE|WORKLOAD_NODE_IMAGE" versions.env
   ```

   Good: your `spec.topology.version` equals `WORKLOAD_K8S_VERSION`.
   Bad: it does not. Nothing pins it to succeed. Use `WORKLOAD_K8S_VERSION`, or change
   `versions.env` deliberately and re-run `make dev-up`.

5. Confirm the fix before applying it.

   ```
   cluster plan -f cluster.yaml
   ```

   Good: the plan renders with the version you chose and the class accepts it.
   Bad: the plan errors on the version's format — the class's schema rejects the string
   (a missing `v` prefix is the usual cause): `cluster docs CAPI-TOPO-001`.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Version bumped, node image not | Topology version newer than the `customImage` tag | Set both, or set the version back to `WORKLOAD_K8S_VERSION` |
| Patch release never published | `docker pull` returns `manifest unknown` | Choose a published patch release of the same minor |
| Wrong architecture image | Image inspects as `amd64` on an `arm64` host; containers exit at once | Use the digest pinned in `versions.env` |
| Floating tag resolved to something else | `customImage` has no `@sha256:` digest | Pin the digest; nothing in this repo floats |
| Malformed version string | Cluster `TopologyReconciled` reason `ReconcileFailed`, message names the version | `cluster docs CAPI-TOPO-001` |
| Registry unreachable | Pull error is a timeout or auth failure, not `manifest unknown` | Fix the network or the registry credentials; the version is fine |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
kubectl get cluster dev-1 -n default -o jsonpath='{.spec.topology}{"\n"}' > /tmp/topology.json
kubectl get devmachine,devmachinetemplate,machine -n default \
  -l cluster.x-k8s.io/cluster-name=dev-1 -o yaml > /tmp/objects.yaml
grep -E "K8S_VERSION|NODE_IMAGE" versions.env > /tmp/versions.txt
docker image inspect <image> --format '{{.Os}}/{{.Architecture}}' > /tmp/image.txt
```

Include `uname -m` and the exact `customImage` string: without both, nobody can tell a
missing tag from a wrong architecture.
