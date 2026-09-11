# Eject

Three exits, smallest first: write one field the class manages, turn the policy off, or
leave the assembly and keep plain Cluster API.

None of them touch a running cluster. The policy is a `ValidatingAdmissionPolicy`; it only
sees writes to the management cluster's API server. Removing it changes what you are allowed
to write, not what is already reconciled.

## Write one managed field: break-glass

Break-glass needs two things at once, and each is useless alone:

- the object carries the label `sixfields.io/break-glass: "true"`
- you are in the group `sixfields:break-glass`

With only one of them the write is denied and the message says which half you have:

```
spec.clusterNetwork needs both the sixfields.io/break-glass label and membership of group sixfields:break-glass. Use break-glass (docs/eject.md).
```

Label the object:

```sh
kubectl label cluster my-cluster sixfields.io/break-glass=true
```

Get the group. It is an authentication attribute, so it comes from your identity provider,
not from a Kubernetes object. For a kubeconfig backed by a client certificate, the group is
an `O=` in the subject:

```sh
openssl req -new -key you.key -out you.csr \
  -subj "/CN=you/O=sixfields:break-glass"
```

Submit that CSR, approve it, and use the issued certificate in your kubeconfig. For an OIDC
issuer, add `sixfields:break-glass` to the claim the API server reads as groups
(`--oidc-groups-claim`). Check what you actually have:

```sh
kubectl auth whoami
```

Now the write goes through:

```sh
kubectl edit cluster my-cluster
```

Every break-glass write is recorded. The policy adds an audit annotation to the request's
audit event, so both uses and half-uses are greppable:

```sh
grep -o '"sixfields-[a-z-]*/break-glass":"[^"]*"' /var/log/kubernetes/audit.log
```

```
"sixfields-cluster-fields/break-glass":"granted user=carol field=spec.clusterNetwork"
"sixfields-managed-kinds/break-glass":"incomplete user=dave kind=KubeadmControlPlane"
```

`granted` is a write that went through. `incomplete` is a denied attempt that had one half.

Take the label off when you are done. While it is there, anyone in the break-glass group can
write any managed field on that object:

```sh
kubectl label cluster my-cluster sixfields.io/break-glass-
```

Installing or updating the assembly is itself a break-glass write. The ClusterClass
references `*Template` objects, and `*Template` is a managed kind, so applying
`assembly/clusterclass/` needs the label on those objects and the group on you.

## Turn the policy off

Delete the bindings first, then the policies:

```sh
kubectl delete validatingadmissionpolicybinding \
  sixfields-cluster-fields sixfields-managed-kinds
kubectl delete validatingadmissionpolicy \
  sixfields-cluster-fields sixfields-managed-kinds
```

The binding enforces; a policy with no binding does nothing. In this order enforcement stops
in one step. The other order leaves a binding pointing at a policy that is gone: a v1.34 API
server ignores that binding, so writes do go through, but it starts enforcing again the
moment anything re-creates the policy, a GitOps reconcile, or a re-run of
`kubectl apply -k policy/vap`.

Nothing else has to change. Both objects are cluster-scoped and hold no state; no Cluster,
MachineDeployment or provider object references them. Controllers keep reconciling
throughout, because they were exempt from the policy anyway.

To check it is gone:

```sh
kubectl get validatingadmissionpolicy,validatingadmissionpolicybinding | grep sixfields
```

To put it back:

```sh
kubectl apply -k policy/vap
```

## Leave the assembly: `cluster render`

`cluster render` prints the complete set of Cluster API objects behind one cluster, the
Cluster, the control plane, the MachineDeployments and MachineSets, the Machines, the
bootstrap configs, the infrastructure objects and the templates they came from, as plain
YAML with no ClusterClass and no topology:

```sh
cluster render my-cluster > my-cluster.yaml
```

The output is every object the topology controller produced, as it stands, with every
generated reference resolved to a concrete object. The Cluster keeps its `spec.topology`,
which is what a later `cluster render` reads.

Check it against what is running:

```sh
kubectl diff -f my-cluster.yaml && echo "no diff"
```

`kubectl diff` exits 0 when the file matches. Applying the file is a different matter while
the policy is on: most of these objects are managed kinds, so the writes are denied. Remove
the bindings first, as above, and then the file applies.

From there you own the objects directly. Delete the ClusterClass and the policy, keep
upstream Cluster API and the providers exactly as `clusterctl` installed them, and edit the
Machines, control plane and templates by hand. Nothing in this repo runs in your cluster:
there is no controller, no CRD and no webhook of ours to remove. What stays is the upstream
Cluster API you would have installed anyway.
