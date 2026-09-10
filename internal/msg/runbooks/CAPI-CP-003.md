# CAPI-CP-003 — etcd is not coming up

After this page you can tell an etcd member that failed from an etcd member CAPI simply
cannot see, decide whether quorum still exists, and know which of those two you must not
fix by deleting a machine.

## What you are seeing

```
KubeadmControlPlane/dev-1 — etcd is not coming up (6m20s)
raw: kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default -o yaml
next: cluster docs CAPI-CP-003
```

The `control plane` row of `cluster status` reads `stalled`. With `size: ha` the count is
often `2/3 nodes`: two members are healthy and the third is holding the cluster back.

## What is actually true

The `KubeadmControlPlane` (`controlplane.cluster.x-k8s.io/v1beta2`) owns etcd health, in
two layers.

Cluster-wide, `EtcdClusterHealthy`:

- reason `NotHealthy` — at least one member is bad, and the message names it.
- reason `HealthUnknown` — CAPI has not been able to check.
- reason `ConnectionDown` — the management cluster cannot reach the workload API server,
  so nothing was checked. This is a networking fault, not an etcd fault.
- reason `InspectionFailed` — the check itself errored.

Per machine, `EtcdMemberHealthy` and `EtcdPodHealthy`, with reasons `NotHealthy`,
`InspectionFailed`, `ConnectionDown`, and `Deleting` (for `EtcdMemberHealthy`) or
`Provisioning`, `DoesNotExist`, `Failed` (for `EtcdPodHealthy`). `EtcdPodHealthy` with
reason `Provisioning` on a machine that is a minute old is normal.

The distinction that matters: `NotHealthy` means CAPI looked and the member is bad;
`ConnectionDown` and `InspectionFailed` mean CAPI did not get an answer. Deleting a
machine on the strength of `ConnectionDown` can destroy quorum on a cluster that was fine.

On the CAPD `inMemory` backend etcd is faked. The `DevMachine`
(`infrastructure.cluster.x-k8s.io/v1beta2`) carries `EtcdProvisioned` with reasons
`WaitingForVMProvisioned`, `WaitingForNodeProvisioned`, `WaitingForStartupTimeout` and
`Provisioned`. `WaitingForStartupTimeout` is a configured delay, not a failure — that is
how `inmem-stall-etcd` is induced.

## Check, in order

1. Read both etcd layers at once.

   ```
   kubectl get kubeadmcontrolplane.controlplane.cluster.x-k8s.io dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `EtcdClusterHealthy True Healthy`, and the stall is somewhere else — re-read
   `cluster why dev-1`.
   Bad: `EtcdClusterHealthy False NotHealthy` with a message naming one machine. Go to
   step 2 with that name.
   Also bad: reason `ConnectionDown` — stop reading etcd and go to step 4.

2. Read the per-machine conditions.

   ```
   kubectl get machines -n default -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"\n"}{end}{end}'
   ```

   Good: exactly one machine with `EtcdMemberHealthy False NotHealthy` and the others
   True — quorum survives while you fix it.
   Bad: two of three False — quorum is already lost, and no controller will recover it;
   go to step 5 before deleting anything.

3. inMemory backend only: check whether the delay is configured.

   ```
   kubectl get devmachine.infrastructure.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{range .status.conditions[*]}{.type}={.reason} {end}{"\n"}{end}'
   ```

   Good: `EtcdProvisioned=WaitingForStartupTimeout` and the elapsed time is shorter than
   the template's `spec.template.spec.backend.inMemory.etcd.provisioning.startupDuration`
   — it is a timer, wait it out.
   Bad: `EtcdProvisioned=WaitingForVMProvisioned` — etcd is not the blocked step at all;
   the machine is. Read `cluster docs CAPI-CP-002`.

4. Confirm the management cluster can reach the workload API server.

   ```
   kubectl get cluster dev-1 -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"\n"}{end}' | grep RemoteConnectionProbe
   clusterctl get kubeconfig dev-1 -n default > /tmp/dev-1.kubeconfig
   kubectl --kubeconfig /tmp/dev-1.kubeconfig get nodes
   ```

   Good: `RemoteConnectionProbe True ProbeSucceeded` and `get nodes` returns.
   Bad: `ProbeFailed`, or `get nodes` times out — the etcd conditions are meaningless
   until this works. On the docker backend the load-balancer container is the usual cause:
   `docker ps -a --filter name=dev-1`.

5. Docker backend only: ask etcd itself.

   ```
   docker exec <control-plane-container> etcdctl \
     --endpoints=https://127.0.0.1:2379 \
     --cacert=/etc/kubernetes/pki/etcd/ca.crt \
     --cert=/etc/kubernetes/pki/etcd/server.crt \
     --key=/etc/kubernetes/pki/etcd/server.key \
     endpoint status --cluster -w table
   ```

   Good: every member answers and one is the leader.
   Bad: a member is missing or errors. Its container log
   (`docker logs <container> 2>&1 | grep -i etcd`) has the reason.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Member still starting | `EtcdPodHealthy` reason `Provisioning` on a machine a minute old | Wait; the stall clears on its own |
| Configured startup delay (inMemory) | DevMachine `EtcdProvisioned` reason `WaitingForStartupTimeout` | Nothing is wrong; this is the induced-stall fixture |
| Workload API unreachable | `EtcdClusterHealthy` reason `ConnectionDown`, Cluster `RemoteConnectionProbe` reason `ProbeFailed` | Fix the load balancer or the network; do not delete machines |
| One member's data corrupt or disk full | `EtcdMemberHealthy` False, reason `NotHealthy`, member errors in the container log | Delete that one Machine while the other two are healthy; the control plane replaces it |
| Quorum lost | Two or more members `NotHealthy` on a three-member control plane | Restore from a backup or recreate the cluster; a scale-up cannot regain quorum |
| Even replica count | `spec.replicas` is 2 or 4 on the KubeadmControlPlane | Set `size: dev` (1) or `size: ha` (3); etcd needs an odd count |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get kubeadmcontrolplane,machine,devmachine -n default \
  -l cluster.x-k8s.io/cluster-name=dev-1 -o yaml > /tmp/objects.yaml
```

On the docker backend add the `etcdctl endpoint status` output above and
`docker logs <control-plane-container> 2>&1 | tail -200 > /tmp/etcd.log`. Say in the
report whether quorum was intact when you captured it — that decides what anyone can
safely suggest.
