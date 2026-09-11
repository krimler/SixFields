# User guide

After this page you can create a cluster, read what the tool shows you while it
comes up, work out what is wrong when it stops, and leave without taking anything
with you.

## Two nouns

A **cluster** and a **pool**. You write one kind, `Cluster`, and inside it you name
pools of workers. What backs a pool — a MachineDeployment, or a MachinePool where
the provider has a scaling group — is the class's decision, not yours.

## Six fields

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: dev-1
  namespace: default
spec:
  topology:
    classRef:
      name: std
    version: v1.34.11
    workers:
      machineDeployments:
      - name: default
        class: default
        replicas: 2
```

That is the whole surface, plus two variables:

| variable | values | what it does |
|---|---|---|
| `size` | `dev` (default), `ha` | one control-plane node, or three |
| `placement` | `self` (default), `hosted` | control plane on machines, or as pods in the management cluster |

`size: ha` also needs `spec.topology.controlPlane.replicas: 3`. A ClusterClass
patch cannot set control-plane replicas at this Cluster API release, so the two
have to agree; `cluster new --size ha` writes both, and admission rejects a pair
that disagrees.

Write the file by hand, or:

```sh
cluster new dev-1 --version v1.34.11 --pool default=2 > cluster.yaml
cluster plan -f cluster.yaml
cluster up dev-1 -f cluster.yaml
```

`cluster plan` runs the admission rules locally. A field the cluster would reject
is rejected here first, with the same message.

## What you'll see during the wait

Four phases, in the order they finish: infrastructure, control plane, workers,
add-ons. Each row shows a state, one detail worth reading, and either how long it
took or how much longer it usually takes.

```
Cluster/dev-1  class std · v1.34.11 · self
infrastructure  ████████████ done     ready                                  1m30s
control plane   ██████░░░░░░ running  0/1 nodes                        ~1m10s left
workers         ░░░░░░░░░░░░ pending  waiting
addons          ████████████ done     none

safe to Ctrl-C; `cluster status dev-1` resumes
```

The estimate is p50 over your last twenty runs of that phase on this machine,
partitioned by provider, class and placement. Under three runs it says `no history
yet`. Three runs is too few to estimate from.

`--no-tty` prints one line per state change instead, for logs and CI. `--json`
prints a versioned document (`docs/schema/status.v1.json`); `--json --verbose`
includes every object the view was computed from.

## What a stall looks like

A phase is stalled when nothing contributing to it has changed for three minutes
(`--stall-after` changes that). Only the phase the cluster is actually waiting on
can stall — workers sitting at 0/2 behind a stuck control plane are waiting, not
stuck.

```
DevMachine/dev-1-cp-abcde — etcd is not coming up (6m32s)
raw: kubectl get devmachine.infrastructure.cluster.x-k8s.io dev-1-cp-abcde -n default -o yaml
typical: p50 1m15s · p95 2m00s
next: cluster docs CAPI-CP-003
```

One object is named: the most specific one that is failing, most recently. The
`typical:` line appears once three runs of that phase are in the history; before
that there is nothing to compare against and the line is left out.

Why that object and not another:

```sh
cluster why dev-1 --explain-ranking
```

## Five rungs

1. The stall line.
2. `cluster why dev-1 --verbose` — the full condition text, and the ranking.
3. The `raw:` line — the kubectl command behind what you were shown.
4. `cluster docs CAPI-CP-003` — the runbook for that stall class.
5. `cluster why dev-1 --explain` — a model explains the analyzer's finding.
   Optional, and it never decides anything: see `docs/ai.md`.

`cluster explain CAPI-CP-003` prints the short form of any code without a cluster.

## Getting a kubeconfig

```sh
cluster kubeconfig dev-1 > dev-1.kubeconfig
KUBECONFIG=dev-1.kubeconfig kubectl get nodes
```

On Docker Desktop the kubeconfig Cluster API writes points at an address only
reachable from inside the container network. `cluster kubeconfig` rewrites it to
the published port, so this works without editing anything.

## How to eject

```sh
cluster render dev-1 > dev-1-objects.yaml
kubectl diff -f dev-1-objects.yaml && echo "no diff"
```

`kubectl diff` exits 0 when the file matches what is running. Use `diff` here and
not `apply --dry-run=server`: most of these objects are kinds the policy manages,
so an apply is denied while the policy is switched on. `docs/eject.md` shows the
order to switch it off in.

That file is every Cluster API object behind the cluster. `docs/eject.md` is how to
remove the admission policy without touching a running cluster.

## Exit codes

| code | meaning |
|---|---|
| 0 | ready |
| 2 | stalled, or still running when the wait ended |
| 3 | rejected at admission |
| 4 | environment: no cluster, no permission, no runtime |

`cluster up` is safe to re-run. `--timeout` bounds the wait.
