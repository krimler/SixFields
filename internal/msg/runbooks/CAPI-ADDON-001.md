# CAPI-ADDON-001 — add-ons were not applied

After this page you can find which resource of which add-on set failed, say whether it
failed to be *read* in the management cluster or *applied* in the workload cluster, and
recognise the case where the cluster is up but has no CNI.

## What you are seeing

```
ClusterResourceSetBinding/dev-1 — add-ons were not applied (3m18s)
  raw: kubectl get clusterresourcesetbinding.addons.cluster.x-k8s.io dev-1 -n default -o yaml
  Next: cluster docs CAPI-ADDON-001
```

The `addons` row of `cluster status` reads `stalled` with a count like `1/3 applied`.
Rows above it read `done`. A cluster in this state has an API server that answers and
nodes that never go Ready, because the CNI is usually one of the unapplied resources.

## What is actually true

Two objects, and they answer different questions.

The `ClusterResourceSet` (`addons.cluster.x-k8s.io/v1beta2`) is the definition, and its
`ResourcesApplied` condition says whether the set as a whole went in:

- reason `NotApplied` — not applied yet, or applying failed.
- reason `WrongSecretType` — a referenced Secret is not of type
  `addons.cluster.x-k8s.io/resource-set`, so CAPI refuses to read it.
- reason `InternalError` — the controller errored; its log has the detail.

The pinned release also keeps the older reasons under
`status.deprecated.v1beta1.conditions` on the same object, and they are more specific
when present: `RetrievingResourceFailed` (the ConfigMap or Secret does not exist),
`RemoteClusterClientFailed` (could not connect to the workload cluster), `ApplyFailed`
(the workload API server rejected the manifest), `ClusterMatchFailed` (the label selector
did not match).

The `ClusterResourceSetBinding` is the per-cluster record and the only place with
per-resource state. Under `spec.bindings[].resources[]`, each entry has `applied`, `hash`
and `lastAppliedTime`. `cluster status` counts exactly these: `2/3 applied` means one
entry has `applied: false`.

The distinction that matters: `RetrievingResourceFailed` and `WrongSecretType` are faults
in the *management* cluster (a missing or mistyped ConfigMap/Secret), while `ApplyFailed`
and `RemoteClusterClientFailed` are faults against the *workload* cluster. Only the second
pair needs the workload kubeconfig to diagnose.

## Check, in order

1. Find the resource that did not apply.

   ```
   kubectl get clusterresourcesetbinding.addons.cluster.x-k8s.io dev-1 -n default \
     -o jsonpath='{range .spec.bindings[*]}{.clusterResourceSetName}{"\n"}{range .resources[*]}  {.kind}/{.name}  applied={.applied}  {.lastAppliedTime}{"\n"}{end}{end}'
   ```

   Good: every entry `applied=true`.
   Bad: one entry `applied=false` — note its kind and name; the rest of this page is
   about that one.
   Also bad: no binding object at all — nothing ever matched this cluster; go to step 3.

2. Read the set's condition, including the deprecated block.

   ```
   kubectl get clusterresourceset.addons.cluster.x-k8s.io -n default \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .status.conditions[*]}  {.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}{range .status.deprecated.v1beta1.conditions[*]}  v1beta1 {.type}{"  "}{.status}{"  "}{.reason}{"  "}{.message}{"\n"}{end}{end}'
   ```

   Good: `ResourcesApplied True Applied`.
   Bad: reason `WrongSecretType` — the Secret's `type` is wrong; step 4 fixes it.
   Also bad: v1beta1 reason `ApplyFailed` — the message is the workload API server's own
   rejection, and it is the answer.

3. Check the set actually selects this cluster.

   ```
   kubectl get clusterresourceset.addons.cluster.x-k8s.io -n default \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.spec.clusterSelector}{"  strategy="}{.spec.strategy}{"\n"}{end}'
   kubectl get cluster dev-1 -n default --show-labels
   ```

   Good: the selector's labels are a subset of the Cluster's labels.
   Bad: no overlap — the set never matched. Add the label to the Cluster; labels are one
   of the fields the admission policy leaves you.

4. Check the referenced ConfigMaps and Secrets exist and are the right type.

   ```
   kubectl get clusterresourceset.addons.cluster.x-k8s.io -n default \
     -o jsonpath='{range .items[*]}{range .spec.resources[*]}{.kind}/{.name}{"\n"}{end}{end}'
   kubectl get configmap,secret -n default
   kubectl get secret <name> -n default -o jsonpath='{.type}{"\n"}'
   ```

   Good: every referenced object exists, and each Secret's type is
   `addons.cluster.x-k8s.io/resource-set`.
   Bad: a referenced object is missing — that is `RetrievingResourceFailed`; create it.
   Also bad: a Secret of type `Opaque` — recreate it with the required type; the type
   cannot be patched.

5. Confirm the resource can be applied to the workload cluster by hand.

   ```
   clusterctl get kubeconfig dev-1 -n default > /tmp/dev-1.kubeconfig
   kubectl --kubeconfig /tmp/dev-1.kubeconfig get nodes
   kubectl get configmap <name> -n default -o jsonpath='{.data}' | head -c 2000
   ```

   Good: `get nodes` answers and the ConfigMap's data is valid YAML for the CNI.
   Bad: `get nodes` times out — that is `RemoteClusterClientFailed`, and the fault is the
   control-plane endpoint, not the add-on: `cluster docs CAPI-WRK-002` step 3.

## Common causes

| cause | how you recognise it | fix |
|---|---|---|
| Referenced ConfigMap or Secret missing | v1beta1 reason `RetrievingResourceFailed`; `kubectl get` finds nothing | Create it in the Cluster's namespace |
| Secret has the wrong type | Reason `WrongSecretType` | Recreate the Secret with type `addons.cluster.x-k8s.io/resource-set` |
| Selector does not match the Cluster | No ClusterResourceSetBinding exists, or v1beta1 reason `ClusterMatchFailed` | Add the selector's labels to the Cluster |
| Manifest rejected by the workload API | v1beta1 reason `ApplyFailed`; the message is the API server's error | Fix the manifest in the ConfigMap; `ApplyOnce` sets do not retry a changed resource |
| Workload cluster unreachable | v1beta1 reason `RemoteClusterClientFailed`; `get nodes` times out with the workload kubeconfig | `cluster docs CAPI-WRK-002` step 3 |
| Feature gate off | No ClusterResourceSet objects reconcile at all | The gate is set by `hack/clusterctl-init.sh` (`EXP_CLUSTER_RESOURCE_SET=true`); re-run `make dev-up` |
| No CNI, so nodes never go Ready | Nodes exist, `Ready` False, the unapplied resource is the CNI manifest | Fix the add-on first; the worker stall clears by itself |

## If none of that explains it

```
cluster status dev-1 --json > /tmp/status.json
cluster why dev-1 --verbose > /tmp/why.txt
kubectl get clusterresourceset,clusterresourcesetbinding -n default -o yaml > /tmp/addons.yaml
kubectl get configmap,secret -n default > /tmp/refs.txt
kubectl logs -n capi-system deployment/capi-controller-manager --since=15m | grep -i resourceset > /tmp/crs.log
```

Do not attach the Secrets themselves — their contents are what add-on sets carry, and they
are credentials as often as manifests. `kubectl get secret <name> -n default -o jsonpath='{.type}'`
is enough for anyone helping you.
