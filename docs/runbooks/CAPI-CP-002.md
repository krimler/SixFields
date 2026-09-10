# CAPI-CP-002 — a control-plane machine is stuck provisioning

After this page you can say which of the four steps of a Machine's life it is stuck on —
bootstrap data, infrastructure, node registration, or control-plane pods — and read the
one object that owns that step.

## What you are seeing

```
Machine/dev-1-control-plane-7fk2x — a control-plane machine is stuck provisioning (5m03s)
  raw: kubectl get machine.cluster.x-k8s.io dev-1-control-plane-7fk2x -n default -o yaml
  Next: cluster docs CAPI-CP-002
```

The `control plane` row of `cluster status` reads `stalled` with a count like `1/3 nodes`.
If the count is `0/1` or `0/3` and no API server has ever come up, the code is
`CAPI-CP-001`, not this one.

## What is actually true

A `Machine` (`cluster.x-k8s.io/v1beta2`) is a four-step pipeline, and each step has its
own condition. Read them in this order, because each waits on the one before:

1. `BootstrapConfigReady` — False with reason `NotReady` until the bootstrap provider
   writes the data secret. Reason `ObjectDoesNotExist` means the KubeadmConfig is gone.
2. `InfrastructureReady` — False with reason `NotReady` until the infrastructure provider
   creates the machine. Reason `ObjectDoesNotExist` means the DevMachine is gone.
3. `NodeHealthy` / `NodeReady` — reason `NodeDoesNotExist` until the kubelet registers,
   then `NodeNotReady` until the node is ready. If you are here, this is `CAPI-WRK-002`
   territory even for a control-plane machine.
4. `Ready` — the rollup, False with reason `NotReady`.

`status.phase` on the Machine is the same story in one word: `Pending`, `Provisioning`,
`Provisioned`, `Running`.

The `DevMachine` (`infrastructure.cluster.x-k8s.io/v1beta2`) named by the Machine's
`spec.infrastructureRef` says why step 2 is stuck, and its conditions differ by backend.

With `spec.backend.docker`:

- `ContainerProvisioned` — reasons `WaitingForClusterInfrastructureReady`,
  `WaitingForControlPlaneInitialized`, `WaitingForBootstrapData` while it waits on
  something else, `NotProvisioned` when creating the container failed. The container
  runtime's own error is in the message.
- `BootstrapCompleted` — reasons `WaitingForContainer`, `WaitingForCGroups`,
  `WaitingForPreloadedImages` while it waits, `Failed` when `kubeadm` exited non-zero.
- `CGroupsReady` and `PreLoadedImagesReady` — the two steps `BootstrapCompleted` waits on.

With `spec.backend.inMemory` there is no container. Four conditions flip on timers:
`VMProvisioned`, `NodeProvisioned`, `EtcdProvisioned`, `APIServerProvisioned`. Reason
`WaitingForStartupTimeout` means the configured `startupDuration` has not elapsed — the
machine is not broken, it is scheduled. `WaitingForVMProvisioned` and
`WaitingForNodeProvisioned` mean an earlier one has not finished.

The `KubeadmControlPlane` also publishes per-machine conditions once the node exists:
`APIServerPodHealthy`, `ControllerManagerPodHealthy`, `SchedulerPodHealthy`,
`EtcdPodHealthy`, with reasons `Provisioning`, `DoesNotExist`, `Failed`,
`InspectionFailed` and `ConnectionDown`. `ConnectionDown` means the management cluster
cannot reach the workload API server, so those conditions say nothing about the pods.

## Check, in order

1. Find which step the Machine is on.

   ```
   kubectl get machine.cluster.x-k8s.io dev-1-control-plane-7fk2x -n default \
     -o jsonpath='{.status.phase}{"\n"}{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `BootstrapConfigReady True` and `InfrastructureReady False NotReady` with a
   `lastTransitionTime` inside the last minute — the infrastructure provider is working.
   Bad: `BootstrapConfigReady False NotReady` — nothing downstream will move; go to step 4.
   Also bad: `NodeHealthy False NodeDoesNotExist` with infrastructure ready — the machine
   booted and never joined; read `cluster docs CAPI-WRK-002`.

2. Read the DevMachine.

   ```
   kubectl get devmachine.infrastructure.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}{end}'
   ```

   Good, docker backend: `ContainerProvisioned True Provisioned` and `BootstrapCompleted
   False WaitingForCGroups` — it is mid-boot.
   Good, inMemory backend: `VMProvisioned False WaitingForStartupTimeout` — a deliberate
   delay, not a fault. Compare it against the template's `startupDuration` (step 5).
   Bad: `ContainerProvisioned False NotProvisioned` — the message is the runtime's error
   (image not found, no space, port in use).
   Also bad: `BootstrapCompleted False Failed` — `kubeadm` failed inside the machine; its
   output is in the message and in the container's log (step 3).

3. Docker backend only: read the machine's container.

   ```
   docker ps -a --filter name=dev-1-control-plane
   docker logs --tail=100 <container>
   ```

   Good: the container is `Up` and the log ends with kubelet lines.
   Bad: the container is `Exited`, or the log ends in a `kubeadm init`/`kubeadm join`
   error. That error is the fault; nothing in CAPI will say more.

4. If bootstrap is the blocked step, read the KubeadmConfig.

   ```
   kubectl get kubeadmconfig.bootstrap.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{range .status.conditions[*]}{.type}={.status}/{.reason} {end}{"\n"}{end}'
   ```

   Good: `DataSecretAvailable=True/Available`.
   Bad: `DataSecretAvailable=False/NotAvailable` — the bootstrap provider log has the
   reason; a control-plane join also needs `CertificatesAvailable=True/Available` on the
   KubeadmControlPlane.

5. inMemory backend only: confirm the delay is configured, not real.

   ```
   kubectl get devmachinetemplate.infrastructure.cluster.x-k8s.io -n default \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.spec.template.spec.backend.inMemory}{"\n"}{end}'
   ```

   Each component has its own `provisioning.startupDuration` under `vm`, `node`, `etcd`
   and `apiServer`.
   Good: those durations are seconds and the elapsed time in the stall line is shorter
   than their sum.
   Bad: one component's `startupDuration` is longer than the stall threshold
   (`--stall-after`, default 3m). That is an induced stall; it is the fixture doing its job.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Node image missing or wrong architecture | DevMachine `ContainerProvisioned` False, reason `NotProvisioned`, image pull error in the message | `cluster docs CAPI-VERSION-001` |
| `kubeadm` failed inside the machine | DevMachine `BootstrapCompleted` False, reason `Failed`; the container log ends in a kubeadm error | Fix what kubeadm names, delete the Machine, let the control plane recreate it |
| Bootstrap data never produced | Machine `BootstrapConfigReady` False, reason `NotReady` | Read the bootstrap provider log; see `cluster docs CAPI-CP-001` step 4 |
| Host out of disk or memory | Container `Exited` immediately; `docker` reports no space | Free space, `make profile P=dev` to resize the VM, retry |
| Deliberate inMemory delay | DevMachine reason `WaitingForStartupTimeout` | Nothing is wrong; raise `--stall-after` or lower `startupDuration` |
| Management cluster cannot reach the workload API | KubeadmControlPlane per-machine conditions with reason `ConnectionDown` | Check the load balancer container, then `cluster docs CAPI-WRK-002` |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get machine,devmachine,kubeadmconfig,kubeadmcontrolplane -n default \
  -l cluster.x-k8s.io/cluster-name=dev-1 -o yaml > /tmp/objects.yaml
```

On the docker backend add `docker ps -a --filter name=dev-1` and
`docker logs <container> > /tmp/container.log`.
`make record-fixture NAME=stall-cp-machine CLUSTER_NAME=dev-1` records the state.
