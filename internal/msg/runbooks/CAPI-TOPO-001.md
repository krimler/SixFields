# CAPI-TOPO-001 — the topology could not be reconciled

After this page you can tell a real reconcile failure from the normal in-progress
reasons that share this condition, find the exact field of your Cluster that the class
rejected, and reproduce the same error locally before you re-apply.

This is the one stall class that is almost always a mistake in the `Cluster` you wrote.

## What you are seeing

```
Cluster/dev-1 — the topology could not be reconciled (2m30s)
  raw: kubectl get cluster.cluster.x-k8s.io dev-1 -n default -o yaml
  Next: cluster docs CAPI-TOPO-001
```

The stall line names the `Cluster` itself, not a provider object, and usually the
`infrastructure` row of `cluster status` is still `pending` — nothing downstream was ever
created.

## What is actually true

The `Cluster`'s `TopologyReconciled` condition (`cluster.x-k8s.io/v1beta2`). Its reason,
not its status, is the whole message. Exactly one reason means a failure:

- `ReconcileFailed` — the topology controller could not compute or apply the class. The
  message names the field or the variable. This is the one to act on.

Every other False reason means "in progress, waiting on something", and is not a fault:

- `ClusterCreating`, `ClusterUpgrading` — first pass, or a version change in flight.
- `ControlPlaneUpgradePending`, `MachineDeploymentsUpgradePending`,
  `MachinePoolsUpgradePending` — an upgrade is queued behind the control plane.
- `MachineDeploymentsCreatePending`, `MachinePoolsCreatePending` — pools are queued behind
  the control plane being initialized.
- `MachineDeploymentsUpgradeDeferred`, `MachinePoolsUpgradeDeferred` — an annotation on
  the pool is holding its upgrade back on purpose.
- `LifecycleHookBlocking` — an external hook has not answered; the message names it.
- `ClusterClassNotReconciled` — the class itself is not ready yet. The fault is on the
  ClusterClass, not on your Cluster.
- `Paused`, `Deleting` — the cluster is paused or going away.

When the reason is `ClusterClassNotReconciled`, read the `ClusterClass`:
`VariablesReady` False with reason `VariableDiscoveryFailed` means a variable definition
or a patch is invalid, and `RefVersionsUpToDate` False with reason
`RefVersionsNotUpToDate` means it references template versions that no longer exist.

Two things this code does *not* cover. A field the admission policy rejects never reaches
the topology controller — that is a `CAPI-ADM-*` denial at apply time, with the field path
in the message. A version the class accepts but the provider has no image for is
`CAPI-VERSION-001`.

## Check, in order

1. Read the reason, and stop if it is one of the waiting ones.

   ```
   kubectl get cluster dev-1 -n default \
     -o jsonpath='{range .status.conditions[?(@.type=="TopologyReconciled")]}{.status}{"  "}{.reason}{"\n"}{.message}{"\n"}{end}'
   ```

   Good: reason `ClusterCreating` or one of the `*Pending` reasons, with a
   `lastTransitionTime` that moved recently — nothing is wrong, the stall threshold is
   just short. The blocking work is in another phase; run `cluster why dev-1` again.
   Bad: reason `ReconcileFailed`. The message names the field; go to step 2.
   Also bad: reason `ClusterClassNotReconciled` — go to step 4.

2. Reproduce the failure without the cluster.

   ```
   cluster plan -f cluster.yaml
   ```

   Good: it prints the same message the condition carries. Fix the field it names and
   re-apply; that loop is seconds, not minutes.
   Bad: `cluster plan` succeeds against the same file — your applied Cluster differs from
   the file. Compare them: `kubectl get cluster dev-1 -n default -o yaml`.

3. Check the six fields you are allowed to set.

   ```
   kubectl get cluster dev-1 -n default -o jsonpath='{.spec.topology}{"\n"}' | python3 -m json.tool
   kubectl get clusterclass -n default
   ```

   Good: `class` matches a ClusterClass that exists in this namespace, `version` is a
   `vX.Y.Z` string, each `workers.machineDeployments[].class` matches a worker class the
   ClusterClass declares, and every entry in `variables` is one the class exposes.
   Bad: `class: std` with no `std` ClusterClass in the namespace — the assembly is not
   applied here. Run `make render && kubectl apply -f bin/render/docker.yaml`.
   Also bad: a variable name the class does not declare — the message says so; remove it.

4. If the class is the problem, read the class.

   ```
   kubectl get clusterclass std -n default \
     -o jsonpath='{range .status.conditions[*]}{.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}'
   ```

   Good: `VariablesReady True` and `RefVersionsUpToDate True`.
   Bad: `VariablesReady False VariableDiscoveryFailed` — a variable schema or a patch in
   the class is invalid. That is an assembly bug, not a user bug: `make render` and
   `make class-plan` show the blast radius before re-applying.

5. Check what the class actually computes for your input.

   ```
   make class-plan
   clusterctl alpha topology plan -f cluster.yaml -o /tmp/plan
   ```

   Good: the plan lists a control-plane object and one machine deployment per pool.
   Bad: the plan errors with the same text as the condition — the class and your file
   disagree, and the plan output names the patch that failed.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Class name does not exist in the namespace | `ReconcileFailed`, message names the class; `kubectl get clusterclass` is empty | `make render && kubectl apply -f bin/render/docker.yaml` |
| Unknown variable | Message names the variable; the class's `spec.variables` does not list it | Remove it, or use one of the exposed variables |
| Variable value out of range | Message names the variable's schema | Use a value the schema allows (`size: dev\|ha`, `placement: self\|hosted`) |
| Worker class name typo | Message names `workers.machineDeployments[].class` | Match a worker class the ClusterClass declares |
| Class not ready | Reason `ClusterClassNotReconciled`; ClusterClass `VariablesReady` False | Fix the class, then the Cluster reconciles on its own |
| Reading a waiting reason as a failure | Reason is `ClusterCreating` or `*Pending` and it moves every reconcile | Nothing to fix here; the real stall is in another phase |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
cluster render dev-1 > /tmp/rendered.yaml
kubectl get cluster dev-1 -n default -o yaml > /tmp/cluster.yaml
kubectl get clusterclass -n default -o yaml > /tmp/classes.yaml
kubectl logs -n capi-system deployment/capi-controller-manager --since=15m | grep dev-1 > /tmp/topology.log
```

The core controller's log is the one that matters here — the topology controller lives in
`capi-system`, not in a provider. Include `/tmp/cluster.yaml` and `/tmp/classes.yaml`
together: the failure is always a disagreement between those two files.
