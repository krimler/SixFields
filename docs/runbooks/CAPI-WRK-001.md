# CAPI-WRK-001, worker machines are not becoming ready

After this page you can say whether the pool failed to *create* machines or created them
and they failed to *become ready*, and read the right object for each, they are different
faults with different fixes.

## What you are seeing

```
MachineDeployment/dev-1-default, worker machines are not becoming ready (4m55s)
raw: kubectl get machinedeployment.cluster.x-k8s.io dev-1-default -n default -o yaml
next: cluster docs CAPI-WRK-001
```

The `workers` row of `cluster status` reads `stalled` with a count like `1/3 nodes`. The
`control plane` row reads `done`: this code is only emitted after the API server is up.

A pool is one user-facing noun. Underneath, the class backs it with a `MachineDeployment`
(the default, and what CAPD uses) or a `MachinePool` on providers with a native scaling
group. The stall line names whichever one exists.

## What is actually true

The `Cluster` (`cluster.x-k8s.io/v1beta2`) holds two rollups:

- `WorkersAvailable`, False with reason `NotAvailable`; reason `NoWorkers` means the
  topology declares no pool at all, which is not a stall.
- `WorkerMachinesReady`, False with reason `NotReady`; reason `NoReplicas` means the
  pool exists but wants zero machines.

The `MachineDeployment` (`cluster.x-k8s.io/v1beta2`) says which half of the problem it is:

- `ScalingUp` True with reason `ScalingUp`, it is creating machines. Reason
  `WaitingForReplicasSet` means `spec.replicas` was never set, so it will create none.
- `Available` False with reason `WaitingForReplicasSet` or `WaitingForAvailableReplicasSet`
 , machines exist but not enough are available yet.
- `MachinesReady` False with reason `NotReady`, the machines exist and are not ready.
  This is the case where you stop reading the pool and read a Machine.

Counting is the fastest split: `status.replicas` versus `spec.replicas` on the pool. If
`status.replicas` is short, machine *creation* is blocked (a `MachineSet` and its
`ScalingUp` condition say why). If it matches and `status.readyReplicas` is short, machine
*readiness* is blocked, and one Machine's own conditions are the answer.

A `MachinePool` publishes no conditions in CAPI v1.14.2, the constants are declared but
not implemented. For that kind, the replica counters (`status.replicas`,
`status.readyReplicas`, `status.availableReplicas`) are the only signal, and the
`DevMachinePool` behind it carries `Ready` and `ReplicasReady`.

## Check, in order

1. Split creation from readiness.

   ```
   kubectl get machinedeployment.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o custom-columns=NAME:.metadata.name,WANT:.spec.replicas,HAVE:.status.replicas,READY:.status.readyReplicas,AVAIL:.status.availableReplicas
   ```

   Good: `WANT 3  HAVE 3  READY 2`, machines exist, one is not ready. Go to step 3.
   Bad: `WANT 3  HAVE 1`, creation is blocked. Go to step 2.
   Also bad: `WANT` empty, the topology never set replicas; read
   `cluster docs CAPI-TOPO-001`.

2. Creation blocked: read the MachineSet under the pool.

   ```
   kubectl get machineset.cluster.x-k8s.io -n default \
     -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}{end}'
   ```

   Good: `ScalingUp True ScalingUp` with a fresh `lastTransitionTime`, it is working.
   Bad: `ScalingUp True ScalingUp: Scaling up from 0 to 2 replicas`, unchanged for
   minutes, with `HAVE 0`. **The message does not say why.** On CAPI v1.14.2 a failure
   to create the Machine, a template that will not clone, a bootstrap config a webhook
   rejects, appears in no condition and emits no event. Go to step 2b.

2b. Creation blocked with no reason on any object: read the controller log.

   ```
   kubectl logs -n capi-system deployment/capi-controller-manager --tail=200 \
     | grep -i "failed to sync replicas" | tail -3
   ```

   This is the only place the reason exists. A real one, from a run of this assembly:

   ```
   failed to clone bootstrap configuration from K0sWorkerConfigTemplate
   default/hosted-1-default-dtsmv while creating a Machine: ... admission webhook
   "validate-k0sworkerconfig-v1beta1.k0smotron.io" denied the request:
   spec.version: Invalid value: "v1.36.4-k0s.0": k0s specific versions must be
   specified using the '+k0s' suffix
   ```

   Read the object it names. Everything after `denied the request:` is the fix, and it
   is almost always a field in the class, and not anything on the cluster. Correct it
   in `assembly/`, re-run `make dev-up`, and the MachineSet retries by itself.

3. Readiness blocked: find the machine that is not ready.

   ```
   kubectl get machines -n default -l cluster.x-k8s.io/cluster-name=dev-1 \
     -o custom-columns=NAME:.metadata.name,PHASE:.status.phase,NODE:.status.nodeRef.name,VERSION:.spec.version
   ```

   Good: every worker is `Running` with a node name.
   Bad: one is `Provisioning`, read `cluster docs CAPI-CP-002`; the machine pipeline is
   identical for workers.
   Also bad: one is `Provisioned` with no node name, the machine booted and never joined:
   read `cluster docs CAPI-WRK-002`.

4. Read that machine's conditions.

   ```
   kubectl get machine.cluster.x-k8s.io <name> -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `BootstrapConfigReady True`, `InfrastructureReady True`, `NodeHealthy False
   NodeNotReady` on a node that appeared seconds ago, the kubelet is still starting.
   Bad: `NodeHealthy False NodeDoesNotExist` for minutes, `cluster docs CAPI-WRK-002`.

5. Rule out a health check deleting machines faster than they come up.

   ```
   kubectl get machinehealthcheck -n default -o wide
   kubectl get machines -n default -l cluster.x-k8s.io/cluster-name=dev-1 \
     --sort-by=.metadata.creationTimestamp
   ```

   Good: no MachineHealthCheck, or the machine ages are increasing steadily.
   Bad: every worker is under two minutes old on a cluster that is ten minutes old, the
   pool is in a create/remediate loop. The MachineHealthCheck's `RemediationAllowed`
   condition with reason `TooManyUnhealthy` confirms it.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Replicas never set by the class | Pool `spec.replicas` empty, `ScalingUp` reason `WaitingForReplicasSet` | `cluster docs CAPI-TOPO-001` |
| Machine template missing | MachineSet cannot create; message names a template that does not resolve | Re-apply the assembly: `make render && kubectl apply -f bin/render/docker.yaml` |
| Host cannot run more machines | Machines stuck in `Provisioning`; the runtime reports no space or no memory | `make profile P=dev` to resize the VM, or lower the pool's replicas |
| Node image wrong for the version | Machines never leave `Provisioning`, infra message is an image pull error | `cluster docs CAPI-VERSION-001` |
| Nodes register but never go Ready | Machine `NodeHealthy` reason `NodeNotReady`; the workload cluster has no CNI | `cluster docs CAPI-ADDON-001` |
| Remediation loop | Every worker is newer than the stall; MachineHealthCheck reason `TooManyUnhealthy` | Delete or widen the MachineHealthCheck, then fix the underlying readiness fault |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
clusterctl describe cluster dev-1 -n default --show-conditions all > /tmp/tree.txt
kubectl get machinedeployment,machineset,machine,devmachine,machinehealthcheck -n default \
  -l cluster.x-k8s.io/cluster-name=dev-1 -o yaml > /tmp/objects.yaml
```

State which half you landed in, creation or readiness, and the `WANT/HAVE/READY` numbers
from step 1. `make record-fixture NAME=stall-workers CLUSTER_NAME=dev-1` records the state.
