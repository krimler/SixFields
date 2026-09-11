# SixFields

SixFields builds a Kubernetes cluster from six lines of text, and shows you four
progress bars while it works.

This page assumes you have never done this before. Every word that matters is
explained the first time it appears, and there are links at the bottom if you want
to go deeper.

## Before you start

This is young software. It was built over a few days, it has been run on one
machine, and it has never carried anything real. The tests are thorough and the
whole thing is honest about what it has and has not done. Use it to learn, to try
ideas, and to build clusters you can throw away. Do not put anything you care
about on it yet. `STATUS.md` lists what is finished and what is still a sketch.

You need:

- a **Mac with an Apple Silicon chip** (M1 or later). Linux is not supported yet,
  because the setup script uses Homebrew.
- **Docker Desktop**, **OrbStack** or **Colima**, running. Any of the three works.
- about **8 GB of free memory** and **20 GB of disk**.
- an internet connection for the first run, which downloads about a gigabyte.

Everything else is installed for you.

## The words you need

**Kubernetes** is a program that runs other programs across a group of computers.
You give it a list of what you want running, and it keeps that true. People say
"k8s" for short, because there are eight letters between the k and the s.

A **cluster** is one group of computers running Kubernetes together. Making one by
hand is fiddly, and that is the problem this tool solves.

A **node** is one computer in the cluster. Some nodes do the thinking and some do
the work. The thinking ones are the **control plane**. The working ones are
**workers**, and your programs run there.

**YAML** is a file format for writing down settings. It uses indentation the way
an outline does. You will write about fifteen lines of it and never touch it
again.

**Docker** runs a program in a **container**, which is a sealed box with its own
filesystem. SixFields uses containers to pretend to be computers, so your laptop
can host a whole cluster.

**Cluster API** is the official Kubernetes project for building clusters. It is
powerful and it is hard to read. SixFields keeps its power and hides the parts you
do not need yet.

## Why this exists

Cluster API works. The trouble is what it feels like to use.

You write a file describing a cluster. Kubernetes accepts it. Eight minutes later
nothing has happened, and the reason is sitting on an object you have never heard
of, in a field called something like `InfrastructureReady`, in a form that assumes
you already know the answer. The mistake was in the file you wrote. The complaint
arrives somewhere else, much later.

That is two separate problems.

The first is that errors arrive late and in the wrong place. SixFields fixes it by
checking your file the moment you write it. A field you are not allowed to set is
refused immediately, by name, with the reason. Nothing is silently ignored and
nothing fails eight minutes later.

The second is that the wait tells you nothing. A real cluster takes minutes, and
plain Cluster API gives you a word like `Provisioning` and no way to tell slow
from stuck. SixFields shows four progress bars, an estimate from your own past
runs, and when nothing has moved for a while, one line naming the object that is
blocking.

SixFields adds no new Kubernetes objects for you to learn and runs no extra
software in your cluster. It is a blueprint, a rule about who may write what, and
a program that watches. You can walk away from all three and keep your clusters.

## The one strange idea

To build a cluster, SixFields uses a cluster.

The first one is a factory. You hand the factory a short order form, and it builds
you the cluster you asked for. Kubernetes people call the factory the **management
cluster**.

The factory lives on your laptop inside Docker. You build it once and keep it.

## First run

```sh
make bootstrap
make dev-up
cluster up dev-1 -f examples/dev-1.yaml
```

Three commands. Here is what each one does.

`make bootstrap` installs the tools. It uses **Homebrew**, the package installer
for macOS, and it pins every version so you get the ones the tests ran with. Two
of the tools are worth knowing by name: **kubectl** is how you talk to a
Kubernetes cluster, and **kind** builds a small cluster inside Docker.

`make dev-up` builds the factory. It starts the small cluster, installs Cluster
API into it, and loads the SixFields blueprint. The first run takes a few minutes
because it downloads an **image**, which is a frozen copy of a filesystem that a
container starts from. This one is about a gigabyte.

`cluster up dev-1 -f examples/dev-1.yaml` hands over the order form and waits.
When it finishes you have a working cluster called `dev-1`.

If something goes wrong, run `make doctor`. It checks your machine and tells you
what to fix.

## A faster way to look around

The cluster above is real. Its nodes are containers, it downloads images, and it
takes several minutes.

There is a second kind of cluster that the factory only pretends to build. It uses
no containers and it finishes in about a minute. Everything on this page works the
same way on it, so it is a good place to try things.

```sh
cluster up fast-1 -f examples/fast-1.yaml
```

One line in the file is different. You will see which in a moment.

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

The first four lines are bookkeeping. `apiVersion` and `kind` tell Kubernetes what
sort of object this is. `namespace` is a folder name for keeping objects apart;
`default` is the one that already exists.

Six fields are yours:

| Field | Meaning |
|---|---|
| `name` | what to call your cluster |
| `classRef.name` | which blueprint to use. `std` is the one that ships |
| `version` | which version of Kubernetes to install |
| `name` (under workers) | a name for this group of worker nodes |
| `class` | which kind of worker node. `default` is the one that ships |
| `replicas` | how many worker nodes you want |

`replicas` means copies. Two replicas means two worker nodes.

Two more fields are optional:

- `size` is `dev` for one control-plane node or `ha` for three. `ha` is short for
  high availability, which means the cluster survives losing one of them.
- `placement` is `self` to run the control plane on its own nodes, or `hosted` to
  run it inside the factory as **pods**. A pod is the smallest thing Kubernetes
  runs: one or more containers that live and die together.

`classRef.name` is the line that changes for the faster cluster. It reads
`std-inmemory` there. To see every blueprint your factory has:

```sh
kubectl get clusterclass
```

You can write the file by hand. You can also have SixFields write it:

```sh
cluster new dev-1 --version v1.34.11 --pool default=2 > cluster.yaml
```

Everything else about the cluster comes from the blueprint. You do not choose the
networking, the images, or how the control plane starts. Those are decided once,
in `assembly/`, and every cluster gets the same answers.

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

**infrastructure** is the networking your cluster needs before anything starts.
Part of it is a **load balancer**, a single address that forwards to whichever
control-plane node is healthy.

**control plane** is the thinking part. **workers** are the nodes that run your
programs.

**addons** are the extras the blueprint installs for you. The important one is the
**network plugin**, the piece that lets pods on different nodes talk to each
other. Kubernetes does not include one, and nodes stay unhealthy until something
provides it. The blueprint installs one so you never meet this problem.

The time on the right is how long that step usually takes on your machine.
SixFields remembers your last twenty runs. Before three runs it says `no history
yet`, because three runs is too few to guess from.

Press Ctrl-C whenever you like. The cluster keeps building. Run `cluster status
dev-1` to watch again.

## When it gets stuck

Building a cluster can stall. A node fails to start, an image will not download, a
piece never reports healthy. When that happens you get one line:

```
DevMachine/dev-1-cp-abcde: etcd is not coming up (6m32s)
raw: kubectl get devmachine.infrastructure.cluster.x-k8s.io dev-1-cp-abcde -n default -o yaml
typical: p50 1m15s · p95 2m00s
next: cluster docs CAPI-CP-003
```

Read it top to bottom:

- `DevMachine/dev-1-cp-abcde` is the object to look at. There are about twenty
  objects behind a cluster and most of them are complaining. This is the one that
  matters.
- `etcd is not coming up` is what is wrong, in English. **etcd** is the database
  where Kubernetes keeps everything it knows. Nothing works without it.
- `(6m32s)` is how long it has been stuck.
- `raw:` is a command you can paste. It prints everything Kubernetes knows about
  that object. Nothing is hidden from you.
- `typical:` is how long this step usually takes, so you can tell "slow" from
  "stuck". **p50** is the middle of your past runs: half were faster. **p95** is
  the slow end: only one run in twenty took longer. The line appears once you have
  built three clusters.
- `next:` is what to do.

There are five levels of detail. Most people stop at the first.

1. The line above.
2. `cluster why dev-1 --verbose` shows the full text and why this object was
   picked over the others.
3. The `raw:` command shows the object itself.
4. `cluster docs CAPI-CP-003` prints a **runbook**: a checklist for that exact
   problem, with the commands to run and what a good and a bad answer look like.
5. `cluster why dev-1 --explain` asks a language model to explain the runbook's
   findings in three lines. This one is optional and needs a model running on your
   machine. `docs/ai.md` says what it sends and where.

## When your file is wrong

Typing a field the blueprint owns gets you an error straight away:

```
spec.clusterNetwork is managed by ClusterClass 'std'. Set it via the class or use break-glass (docs/eject.md).
```

A **ClusterClass** is what Kubernetes calls the blueprint. You get this message the
moment you apply the file. To check before you apply:

```sh
cluster plan -f cluster.yaml
```

The message is the same either way.

## Getting into your new cluster

A **kubeconfig** is a small file holding an address and a password, and `kubectl`
reads it to know which cluster you mean.

```sh
cluster kubeconfig dev-1 > dev-1.kubeconfig
KUBECONFIG=dev-1.kubeconfig kubectl get nodes
```

The kubeconfig Cluster API writes points at an address that only works from inside
Docker. `cluster kubeconfig` fixes the address for you.

## Leaving

Every object SixFields made is a normal Cluster API object. You can take them and
go:

```sh
cluster render dev-1 > dev-1-objects.yaml
kubectl diff -f dev-1-objects.yaml && echo "no diff"
```

That file is your whole cluster, and `kubectl diff` exits 0 to prove it matches
what is running. Most of those objects are ones the blueprint owns, so actually
applying the file needs the six-field rule switched off first. `docs/eject.md`
shows how, without stopping any cluster.

## Cleaning up

```sh
make dev-down
```

This deletes the factory and every cluster it built, including the containers.

## Exit codes

An **exit code** is the number a command leaves behind so a script can tell what
happened. Zero always means success.

| Code | Meaning |
|---|---|
| 0 | ready |
| 2 | stalled, or still building when the wait ran out |
| 3 | your file was rejected |
| 4 | something on your machine is wrong; run `make doctor` |

## What else is out there

SixFields is not the first attempt at this.

[clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview) is the official
Cluster API command. It installs providers and it can describe a cluster as a tree
of conditions. SixFields uses it, and folds that tree into four rows.

[Giant Swarm](https://www.giantswarm.io/) and [Syself](https://syself.com/) both
ship opinionated Cluster API platforms, each wrapping a ClusterClass in Helm
charts for their own customers. Their field surfaces are the closest thing to the
six fields here.

What SixFields does differently is refuse the extra fields at write time, and turn
the wait into something you can read. It is also vendor neutral and it adds
nothing that runs inside your cluster.

## Where to learn more

Every one of these is free and written for beginners.

- [Kubernetes basics](https://kubernetes.io/docs/tutorials/kubernetes-basics/), an
  interactive walkthrough from the Kubernetes project.
- [What a pod is](https://kubernetes.io/docs/concepts/workloads/pods/) and
  [what a node is](https://kubernetes.io/docs/concepts/architecture/nodes/).
- [YAML in ten minutes](https://learnxinyminutes.com/docs/yaml/).
- [Docker's own getting started guide](https://docs.docker.com/get-started/).
- [The Cluster API book](https://cluster-api.sigs.k8s.io/), the project SixFields
  is built on.
- [kind](https://kind.sigs.k8s.io/), which runs the factory.
- [kubectl commands](https://kubernetes.io/docs/reference/kubectl/), the ones you
  will use most.
- [etcd](https://etcd.io/) and [k0s](https://k0sproject.io/), two pieces the
  blueprint uses.

## Every command

| Command | What it does |
|---|---|
| `cluster up NAME -f FILE` | apply the file and wait, showing the four rows |
| `cluster status NAME` | show the four rows for a cluster that already exists |
| `cluster why NAME` | name the one object that is blocking |
| `cluster plan -f FILE` | check a file without applying it |
| `cluster new NAME` | write a file from the six fields |
| `cluster render NAME` | print every object behind a cluster |
| `cluster kubeconfig NAME` | print a kubeconfig that works from your machine |
| `cluster docs CODE` | print the runbook for a problem |
| `cluster explain CODE` | print the short version of a problem |
| `cluster doctor` | check that you can create a cluster here |
| `cluster skills install` | install the read-only agent skills |

Every one takes `--help`. Add `--json` to `status` and `why` when a script is
reading the output.

## How the code is organized

```
assembly/     the blueprint: the ClusterClass and the templates it points at
policy/       the rule about who may write what, and its tests
cmd/cluster/  the command you type
internal/     the parts the command is made of
docs/         everything you are meant to read
testdata/     recorded runs, and the expected output for each
e2e/          tests that build real clusters
hack/         the scripts behind the make targets
```

`internal/` is worth a closer look, because it is split on one idea. The pieces
that think are pure: you hand them a snapshot of a cluster and they hand back an
answer, with no network in between.

| Package | What it does |
|---|---|
| `snapshot` | one moment in a cluster's life, as plain data |
| `fold` | turns that into the four rows you see |
| `why` | picks the one object to name when things stall |
| `eta` | keeps the timings of your past runs and predicts the next |
| `gen` | turns the six fields into a Cluster file |
| `msg` | every sentence the tool can say, in one place |
| `render` | draws the rows, the plain text, and the JSON |
| `watch` | reads a live cluster and builds a snapshot |
| `explain` | the optional language model rung |

Only `watch` talks to Kubernetes. Everything above it works on a snapshot, so a
recorded run replays through exactly the code a live run uses. That is why you can
develop the display with no cluster at all, and why the tests are fast.

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
make replay F=inmem-stall-vm
```

`docs/api-snapshot.md` is generated from the exact Cluster API version this repo
pins. Every condition name in the code has to appear there, and a test fails if
one does not.

## Helping

Read `CONTRIBUTING.md`. The short version: the tests are the specification, the
tree stays green, and every change that fixes a bug starts with a test that fails
because of it.

Report a security problem privately. `SECURITY.md` says how.

## Licence

[Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0). See `LICENSE`.
