# SixFields

SixFields makes a Kubernetes cluster from six lines of YAML, and shows you four
progress bars while it builds.

It is built on [Cluster API](https://cluster-api.sigs.k8s.io/), the Kubernetes
project for creating clusters. Cluster API is powerful and hard to read. SixFields
keeps all of its power and hides the parts you do not need yet.

## What you are about to do

You will install some tools, start a small Kubernetes cluster on your laptop, and
then use that cluster to build another cluster. The second one is the one you
asked for. The first one exists only to build it.

That sounds odd the first time. Kubernetes people call the first cluster the
**management cluster**. Think of it as a factory. You hand the factory a short
order form, and it builds you a cluster.

## First run

```sh
make bootstrap
make dev-up
cluster up dev-1 -f examples/dev-1.yaml
```

Three commands. Here is what each one does.

`make bootstrap` installs the tools: Go, kind, kubectl, and a few others. It uses
Homebrew, and it pins every version so you get the same ones the tests ran with.

`make dev-up` builds the factory. It starts a small Kubernetes cluster inside
Docker, installs Cluster API into it, and loads the SixFields blueprint. This
takes a few minutes the first time because it downloads a large image.

`cluster up dev-1 -f examples/dev-1.yaml` places the order and waits. When it
finishes you have a working Kubernetes cluster called `dev-1`.

If something goes wrong, run `make doctor`. It checks your machine and tells you
what to fix.

## The order form

This is `examples/dev-1.yaml`. It is the whole thing.

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

Six fields matter:

| Field | Meaning |
|---|---|
| `name` | what to call your cluster |
| `classRef.name` | which blueprint to use. `std` is the one that ships |
| `version` | which Kubernetes version to install |
| `name` (under workers) | a name for this group of worker machines |
| `class` | which kind of worker machine. `default` is the one that ships |
| `replicas` | how many worker machines you want |

Two more fields are optional. `size` is `dev` for one control-plane node or `ha`
for three. `placement` is `self` to run the control plane on its own machines, or
`hosted` to run it as pods inside the factory.

You can write the file by hand. You can also have SixFields write it:

```sh
cluster new dev-1 --version v1.34.11 --pool default=2 > cluster.yaml
```

Everything else about the cluster comes from the blueprint. You do not choose the
network plugin, the machine images, or how the control plane starts. Those are
decided once, in `assembly/`, and every cluster gets the same answers.

## What you see while it builds

```
Cluster/dev-1  class std · v1.34.11 · self
infrastructure  ████████████ done     ready                                  1m30s
control plane   ██████░░░░░░ running  0/1 nodes                        ~1m10s left
workers         ░░░░░░░░░░░░ pending  waiting
addons          ████████████ done     none

safe to Ctrl-C; `cluster status dev-1` resumes
```

Four rows, always the same four, in the order they finish.

**infrastructure** is the network and the load balancer. **control plane** is the
brain of your cluster. **workers** are the machines that run your programs.
**addons** are the extras the blueprint installs for you, like the network plugin.

The time on the right is how long that step usually takes on your machine.
SixFields remembers your last twenty runs and uses the middle value. Before three
runs it says `no history yet`, because three runs is too few to guess from.

Press Ctrl-C whenever you like. The cluster keeps building. Run
`cluster status dev-1` to watch again.

## When it gets stuck

Building a cluster can stall. A machine fails to start, an image will not
download, a component never reports healthy. When that happens you get one line:

```
DevMachine/dev-1-cp-abcde — etcd is not coming up (6m32s)
raw: kubectl get devmachine.infrastructure.cluster.x-k8s.io dev-1-cp-abcde -n default -o yaml
typical: p50 1m15s · p95 2m00s
next: cluster docs CAPI-CP-003
```

Read it top to bottom:

- `DevMachine/dev-1-cp-abcde` is the object to look at. There are usually twenty
  objects behind a cluster and most of them are complaining. This is the one that
  matters.
- `etcd is not coming up` is what is wrong, in English.
- `(6m32s)` is how long it has been stuck.
- `raw:` is a command you can paste. It prints everything Kubernetes knows about
  that object. Nothing is hidden from you.
- `typical:` is how long this step usually takes, so you can tell "slow" from
  "stuck".
- `next:` is what to do.

There are five levels of detail, and most people stop at the first.

1. The line above.
2. `cluster why dev-1 --verbose` shows the full text and why this object was
   picked over the others.
3. The `raw:` command shows the raw object.
4. `cluster docs CAPI-CP-003` prints a runbook: what to check, in order, with the
   commands and what a good and a bad answer look like.
5. `cluster why dev-1 --explain` asks a language model to explain the runbook's
   findings in three lines. This one is optional and needs a model on your
   machine. See `docs/ai.md` for what it sends and where.

## When your file is wrong

Typing a field the blueprint owns gets you an error straight away:

```
spec.clusterNetwork is managed by ClusterClass 'std'. Set it via the class or use break-glass (docs/eject.md).
```

You get this the moment you apply the file. Check before you apply:

```sh
cluster plan -f cluster.yaml
```

The message is the same one either way.

## Getting into your new cluster

```sh
cluster kubeconfig dev-1 > dev-1.kubeconfig
KUBECONFIG=dev-1.kubeconfig kubectl get nodes
```

The file Cluster API writes points at an address that only works from inside
Docker. `cluster kubeconfig` fixes the address for you.

## Leaving

Every object SixFields made is a normal Cluster API object. You can take them and
go:

```sh
cluster render dev-1 > dev-1-objects.yaml
```

That file is your whole cluster. Applying it again changes nothing, which is how
you know it is complete. `docs/eject.md` shows how to turn off the six-field rule
and keep your clusters running.

## Cleaning up

```sh
make dev-down
```

This deletes the factory and every cluster it built, including the containers.

## Exit codes

Useful in scripts.

| Code | Meaning |
|---|---|
| 0 | ready |
| 2 | stalled, or still building when the wait ran out |
| 3 | your file was rejected |
| 4 | something on your machine is wrong; run `make doctor` |

## Working on SixFields

`CLAUDE.md` holds the rules. `PLAN.md` holds the plan. `STATUS.md` says where the
work is right now.

```sh
make test          # pure logic, under 30 seconds
make test-envtest  # against a real Kubernetes API server
make e2e           # against real clusters
```

You can work on the display without any cluster at all. SixFields records real
runs and replays them:

```sh
make replay F=inmem-stall-etcd
```

`docs/api-snapshot.md` is generated from the exact Cluster API version this repo
pins. Every condition name in the code has to appear there, and a test fails if
one does not.

## Licence

Apache-2.0. See `LICENSE`.
