# Eject

Three exits, smallest first: write one field the class manages, turn the policy off, or
leave the assembly and keep plain Cluster API.

None of them touch a running cluster. The policy is a `ValidatingAdmissionPolicy`; it only
sees writes to the management cluster's API server. Removing it changes what you are allowed
to write, not what is already reconciled.

## Write one managed field: break-glass

Break-glass needs two things at once, and each is useless alone:

- the object carries the label `capi-distro.io/break-glass: "true"`
- you are in the group `capi-distro:break-glass`

With only one of them the write is denied and the message says which half you have:

```
spec.clusterNetwork needs both the capi-distro.io/break-glass label and membership of group capi-distro:break-glass. Use break-glass (docs/eject.md).
```

Label the object:

```sh
kubectl label cluster my-cluster capi-distro.io/break-glass=true
```

Get the group. It is an authentication attribute, so it comes from your identity provider,
not from a Kubernetes object. For a kubeconfig backed by a client certificate, the group is
an `O=` in the subject:

```sh
openssl req -new -key you.key -out you.csr \
  -subj "/CN=you/O=capi-distro:break-glass"
```

Submit that CSR, approve it, and use the issued certificate in your kubeconfig. For an OIDC
issuer, add `capi-distro:break-glass` to the claim the API server reads as groups
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
grep -o 'capi-distro-[a-z-]*/break-glass":"[^"]*"' /var/log/kubernetes/audit.log
```

```
capi-distro-cluster-fields/break-glass":"granted user=carol field=spec.clusterNetwork"
capi-distro-managed-kinds/break-glass":"incomplete user=dave kind=KubeadmControlPlane"
```

`granted` is a write that went through. `incomplete` is a denied attempt that had one half.

Take the label off when you are done. It stays on the object otherwise, and it is what makes
the next write to that object skip the check:

```sh
kubectl label cluster my-cluster capi-distro.io/break-glass-
```

Installing or updating the assembly is itself a break-glass write. The ClusterClass
references `*Template` objects, and `*Template` is a managed kind, so applying
`assembly/clusterclass/` needs the label on those objects and the group on you.

## Turn the policy off

Delete the bindings first, then the policies:

```sh
kubectl delete validatingadmissionpolicybinding \
  capi-distro-cluster-fields capi-distro-managed-kinds
kubectl delete validatingadmissionpolicy \
  capi-distro-cluster-fields capi-distro-managed-kinds
```

The binding is what enforces; the policy on its own is inert. Deleting the binding first
means there is never a moment where a binding names a policy that is gone. A binding whose
`policyName` does not resolve does not fail open — the API server treats it as a
misconfiguration and, with `failurePolicy: Fail`, denies the requests it matches. Delete
them in the other order and every `Cluster` write fails until the second command lands.

Nothing else has to change. Both objects are cluster-scoped and hold no state; no Cluster,
MachineDeployment or provider object references them. Controllers keep reconciling
throughout, because they were exempt from the policy anyway.

To check it is gone:

```sh
kubectl get validatingadmissionpolicy,validatingadmissionpolicybinding \
  -l '!kubernetes.io/bootstrapping' | grep capi-distro
```

To put it back:

```sh
kubectl apply -k policy/vap
```

## Leave the assembly: `cluster render`

`cluster render` prints the complete set of Cluster API objects behind one cluster — the
Cluster, the control plane, the MachineDeployments and MachineSets, the Machines, the
bootstrap configs, the infrastructure objects and the templates they came from — as plain
YAML with no ClusterClass and no topology:

```sh
cluster render my-cluster > my-cluster.yaml
```

The output is what the topology controller computed, with `spec.topology` removed and every
generated reference resolved to a concrete object. It re-applies with an empty diff:

```sh
kubectl apply --dry-run=server -f my-cluster.yaml
```

From there you own the objects directly. Delete the ClusterClass and the policy, keep
upstream Cluster API and the providers exactly as `clusterctl` installed them, and edit the
Machines, control plane and templates by hand. Nothing in this repo runs in your cluster:
there is no controller, no CRD and no webhook of ours to remove. What stays is the upstream
Cluster API you would have installed anyway.
