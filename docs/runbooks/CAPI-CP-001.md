# CAPI-CP-001 — control plane is not initializing

After this page you can tell apart the three reasons no API server has come up —
no machine, no certificates, no bootstrap — and read the object that knows which.

## What you are seeing

```
KubeadmControlPlane/dev-1 — control plane is not initializing (3m41s)
  raw: kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default -o yaml
  Next: cluster docs CAPI-CP-001
```

The `infrastructure` row of `cluster status` reads `done`; `control plane` reads
`stalled` with `0/1 nodes` or `0/3 nodes`. This code means the control plane has not
reached its *first* API server. Once one exists and a later machine is stuck, the code
is `CAPI-CP-002` instead.

## What is actually true

Two conditions on the `Cluster` (`cluster.x-k8s.io/v1beta2`):

- `ControlPlaneInitialized` — False with reason `NotInitialized` until the control-plane
  provider reports its first API server. It never goes back to False afterwards.
- `ControlPlaneAvailable` — False with reason `NotAvailable`, or reason
  `ObjectDoesNotExist` when `spec.controlPlaneRef` points at nothing.

The `KubeadmControlPlane` (`controlplane.cluster.x-k8s.io/v1beta2`) named by
`spec.controlPlaneRef` says why:

- `Initialized` — False with reason `NotInitialized`. The mirror of the Cluster's.
- `CertificatesAvailable` — False with reason `NotAvailable` or `InternalError`. Nothing
  can boot until the cluster CA secrets exist.
- `ScalingUp` — True with reason `ScalingUp` while it creates the first Machine, or
  reason `WaitingForReplicasSet` when `spec.replicas` has not been set by the topology.
- `MachinesReady` — reason `NoReplicas` when no Machine exists yet, `NotReady` once one
  does.

With `placement: hosted` the control-plane object is a `K0smotronControlPlane`
(`controlplane.cluster.x-k8s.io`, k0smotron v1.10.9) and there are no control-plane
Machines at all. It publishes `Available` and `Paused` only, so its pods in the
management cluster are the next thing to read.

## Check, in order

1. Name the blocking object and check `spec.replicas` is set.

   ```
   cluster why dev-1
   kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default \
     -o jsonpath='{.spec.replicas}{"\n"}{.status.replicas}{"\n"}'
   ```

   Good: `1` and `1` (or `3` and `1` while scaling up).
   Bad: the first line is empty — the topology never patched replicas in, and the
   KubeadmControlPlane's `ScalingUp` reason is `WaitingForReplicasSet`. That is
   `cluster docs CAPI-TOPO-001`.

2. Read the control plane's own conditions.

   ```
   kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `Initialized  False  NotInitialized` and `CertificatesAvailable  True  Available`
   — certificates exist, it is waiting on the machine. Continue to step 3.
   Bad: `CertificatesAvailable  False` — no machine will ever boot. The message names the
   secret; check it exists with
   `kubectl get secret -n default -l cluster.x-k8s.io/cluster-name=dev-1`.

3. Find the first control-plane Machine.

   ```
   kubectl get machines -n default -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o custom-columns=NAME:.metadata.name,PHASE:.status.phase,NODE:.status.nodeRef.name
   ```

   Good: one Machine in phase `Provisioning` or `Provisioned` less than two minutes old.
   Bad: no Machines at all — the control plane cannot create one; the reason is on its
   `ScalingUp` condition and in the control-plane provider's log (step 5).
   Also bad: a Machine in phase `Provisioning` for longer than the stall threshold — that
   is `cluster docs CAPI-CP-002`.

4. Read the bootstrap object for that Machine.

   ```
   kubectl get kubeadmconfig.bootstrap.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"\n"}{end}{end}'
   ```

   Good: `DataSecretAvailable  True  Available` — the cloud-init the machine needs exists.
   Bad: `DataSecretAvailable  False  NotAvailable` — the bootstrap provider has not
   produced it; its log is the next stop.

5. Confirm the control-plane and bootstrap providers are alive.

   ```
   kubectl get deployments -A -l cluster.x-k8s.io/provider
   kubectl logs -n <namespace> deployment/<name> --since=15m | grep dev-1
   ```

   Good: reconcile lines mentioning `dev-1` in the last minute.
   Bad: no control-plane provider deployment, or a pod in `CrashLoopBackOff`. Run
   `make dev-up`, or read the crashing pod's log.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Topology never set replicas | KubeadmControlPlane `ScalingUp` reason `WaitingForReplicasSet`, `spec.replicas` empty | `cluster docs CAPI-TOPO-001` |
| Certificates never generated | `CertificatesAvailable` False; the referenced secret is missing | Delete the half-written secret named in the message and let the control plane recreate it |
| Bootstrap data never produced | KubeadmConfig `DataSecretAvailable` False, reason `NotAvailable` | Read the bootstrap provider log; a rejected kubeadm config is the usual cause |
| Node image for the version does not exist | The first Machine never leaves `Provisioning`, infra object message is an image pull error | `cluster docs CAPI-VERSION-001` |
| Control-plane provider not installed | No deployment carries `cluster.x-k8s.io/provider` for `controlplane` | `make dev-up` |
| Hosted placement, pods not scheduled | Control-plane kind is `K0smotronControlPlane`, `Available` False, no Machines exist | Read the k0smotron pods in the management cluster: `kubectl get pods -n default -l cluster.x-k8s.io/cluster-name=dev-1` |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get cluster,kubeadmcontrolplane,machine,kubeadmconfig,devmachine -n default -o yaml > /tmp/objects.yaml
kubectl logs -n <namespace> deployment/<control-plane-provider> --since=30m > /tmp/cp-provider.log
```

`make record-fixture NAME=stall-cp-init CLUSTER_NAME=dev-1` records the same state as a
fixture.
