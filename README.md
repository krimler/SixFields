<div align="center">

# SixFields

Create a Kubernetes cluster from a file of six fields, and watch it being built.

[![Status](https://img.shields.io/badge/status-experimental-orange)](STATUS.md)
[![Setup](https://img.shields.io/badge/setup-macOS%20today%2C%20Linux%20untested-lightgrey)](#3-what-you-need-on-your-machine)
[![Cluster API](https://img.shields.io/badge/Cluster%20API-v1beta2-326ce5)](https://cluster-api.sigs.k8s.io/)
[![Licence](https://img.shields.io/badge/licence-Apache--2.0-blue)](LICENSE)
<!-- When CI exists, add:
[![CI](https://github.com/krimler/sixfields/actions/workflows/ci.yml/badge.svg)](https://github.com/krimler/sixfields/actions/workflows/ci.yml)
-->

</div>

SixFields is a small tool built on top of Cluster API. You describe a cluster in about fifteen lines of YAML, of which six are yours to decide. SixFields checks the file the moment you submit it, builds the cluster, and shows you four progress bars while it works. If the build gets stuck, it tells you which one thing is stuck and what to do about it.

This README is written as a tutorial. It assumes you have not used Kubernetes before. If you have, you can skip Section 2 and skim the rest. Part II at the end is a reference for the commands and the code.

> [!WARNING]
> SixFields is new. It was written over a few days, it has been run on one machine, and it has never carried a real workload. Use it to learn and to experiment with clusters you can throw away. Do not put anything you care about on it yet. `STATUS.md` lists what is finished and what is not.

## Contents

**Part I: Tutorial**

1. [What SixFields does](#1-what-sixfields-does)
2. [A few terms you need to know](#2-a-few-terms-you-need-to-know)
3. [What you need on your machine](#3-what-you-need-on-your-machine)
4. [Setting up](#4-setting-up)
5. [Creating your first cluster](#5-creating-your-first-cluster)
6. [The cluster file](#6-the-cluster-file)
7. [A faster cluster for practising](#7-a-faster-cluster-for-practising)
8. [When a cluster gets stuck](#8-when-a-cluster-gets-stuck)
9. [When there is a mistake in your file](#9-when-there-is-a-mistake-in-your-file)
10. [Using your cluster](#10-using-your-cluster)
11. [Deleting clusters](#11-deleting-clusters)
12. [Leaving SixFields](#12-leaving-sixfields)
13. [Exit codes](#13-exit-codes)
14. [Summary](#14-summary)
15. [Exercises](#15-exercises)

**Part II: Reference**

16. [Command reference](#16-command-reference)
17. [How the code is organised](#17-how-the-code-is-organised)
18. [Working on SixFields](#18-working-on-sixfields)
19. [Contributing](#19-contributing)
20. [Related projects](#20-related-projects)
21. [Further reading](#21-further-reading)
22. [Licence](#22-licence)

## 1. What SixFields does

Cluster API is the official Kubernetes project for creating clusters. It works, but it is hard to use for two reasons, and SixFields exists to remove those two reasons.

The first reason is that Cluster API reports mistakes late, and in the wrong place. Suppose you write a file describing a cluster and submit it. Kubernetes accepts the file without complaint. Nothing visible happens for several minutes. Then a message appears, not on the object you created but on some other object you have never heard of, in a field with a name like `InfrastructureReady`, worded as if you already knew what it meant. The mistake was in your file all along, but you find out about it much later and somewhere else.

SixFields checks your file the moment you submit it. If you have set a field you are not allowed to set, the file is refused immediately, and the message names the field and gives the reason. Nothing is silently ignored, and nothing fails eight minutes later because of something you typed.

The second reason is that Cluster API tells you very little while a cluster is being built. A real cluster takes several minutes to create. During that time plain Cluster API shows you one word, such as `Provisioning`. It does not tell you how far along the build is, how much longer it will take, or whether it has stopped making progress, so you cannot tell the difference between slow and stuck.

SixFields shows four progress bars, one for each stage of the build, with an estimate of the time remaining based on your own earlier runs. If nothing has moved for a while, it prints one line naming the object that is holding things up.

SixFields does this without adding any new kind of Kubernetes object, and without running any extra software inside your cluster. It consists of a template for clusters, a rule about which fields you may set, and a command that watches. You can stop using all three at any time and your clusters will carry on working.

## 2. A few terms you need to know

Before we create a cluster, it is necessary to understand a few terms. Each is explained the first time it appears, and Section 21 has links if you would like to read more. If you already work with Kubernetes and Cluster API, skip to Section 3.

**Kubernetes** is a program whose job is to run other programs on a group of computers. You give Kubernetes a list of what you want running, and it starts those programs, watches them, and restarts them if they stop. The name is often shortened to k8s, because there are eight letters between the k and the s.

A group of computers that Kubernetes manages together is called a **cluster**. Each computer in the cluster is a **node**. Nodes are of two kinds. **Control plane** nodes take the decisions: which program runs on which node, and what to do when something fails. **Worker** nodes are where your programs run. A small cluster might have one control plane node and two worker nodes.

The smallest thing that Kubernetes runs is a **pod**, which is one or more containers that are started and stopped together. A **container** is a program running inside its own sealed filesystem, isolated from the rest of the machine. The frozen copy of a filesystem that a container starts from is called an **image**. **Docker** is the software that runs containers on your laptop. SixFields uses containers to stand in for computers, which is how a whole cluster can run on one laptop.

**YAML** is a file format for writing down settings. It uses indentation to show structure, the way an outline does. In this tutorial you will write about fifteen lines of it.

**Cluster API** is the official Kubernetes project for creating and managing clusters. It is powerful, but the files it expects you to write are long, and the messages it gives back are difficult to read. SixFields is built on Cluster API and hides the parts you do not need yet.

Within Cluster API, a **ClusterClass** is a template for a cluster. It decides everything that should be the same for every cluster, such as the networking and the images, so that an individual cluster file only has to say what is different: the name, the version, and how many workers.

Finally, Cluster API has one idea that surprises most people the first time they meet it. To build a cluster, it uses another cluster. The cluster that does the building is called the **management cluster**, and it is where the Cluster API software runs. The clusters it builds for you are called **workload clusters**. In SixFields, the management cluster is a small cluster that runs on your laptop inside Docker. You create it once and keep it. Running it on a laptop is a convenience for development; in real use a management cluster lives on a server, and nothing in SixFields ties it to a laptop.

## 3. What you need on your machine

SixFields itself runs wherever Go and `kubectl` run. The command-line tool is pure Go with no C dependencies, so it builds for Linux as easily as for macOS. The ClusterClass, the policy, and the add-on are Kubernetes objects, so they run wherever the management cluster runs. And the clusters themselves are Linux already: kind's nodes are Linux containers, the machines that Cluster API's Docker provider creates are Linux containers, and k0s is a Linux binary.

What is macOS-only today is the setup: the scripts in `hack/` behind `make bootstrap` and `make doctor`, which install the tools and check your machine. They have been run on one Apple Silicon Mac and nowhere else, so Linux is untested rather than unsupported. Section 18 lists exactly which scripts are involved and what it would take to change them.

For the setup as it stands, you need:

- A Mac. The setup scripts use Homebrew, and they have been run only on Apple Silicon (M1 or later).
- Docker Desktop, OrbStack, or Colima, installed and running. Any of the three will do; the setup scripts know all three.
- About 8 GB of free memory and 20 GB of free disk space.
- An internet connection for the first run, which downloads about one gigabyte.

Everything else is installed for you in the next section.

If you are on Linux, `make bootstrap` and `make doctor` will not work, but nothing else is in the way. Install kubectl, kind, and the other tools yourself at the versions recorded in `versions.env`, and build the command with `go build ./cmd/cluster`. Because the tool is pure Go, cross-compiling from a Mac with `GOOS=linux GOARCH=amd64` also works. Bear in mind that nobody has run the rest of the setup on Linux yet, so expect small problems in the scripts rather than in the tool.

## 4. Setting up

Setting up takes three commands. Run them from the root of this repository.

```sh
make bootstrap
make build
make dev-up
```

Let us see what each one does.

`make bootstrap` installs the tools that SixFields needs. It uses **Homebrew**, the package installer for macOS. Homebrew cannot pin a version per tool, so it installs the current one, and `make doctor` then compares what you have against the versions recorded in `versions.env` and warns you about any that differ. This is the macOS path; Section 3 says what to do on Linux. Two of these tools are worth knowing by name. **kubectl** is the command you use to talk to any Kubernetes cluster. **kind** creates a small Kubernetes cluster inside Docker, and the management cluster is made with it.

`make build` compiles SixFields itself into `bin/cluster`. Nothing installs it into a system directory, so for the rest of this tutorial put it on your path:

```sh
export PATH="$PWD/bin:$PATH"
```

That lasts for the current terminal session. If you open a new terminal, run it again, or write `./bin/cluster` wherever this tutorial writes `cluster`.

`make dev-up` creates the management cluster. It does three things in turn: it starts a kind cluster, it installs Cluster API into that cluster, and it loads the SixFields ClusterClass and the rule about fields. The first time you run it, it downloads an image of about one gigabyte, so expect it to take a few minutes.

If any of the three fails, run `make doctor`. It examines your machine and tells you what is missing or wrong.

## 5. Creating your first cluster

With the management cluster running, create a workload cluster with one command:

```sh
cluster up dev-1 -f examples/dev-1.yaml
```

Here `dev-1` is the name of the cluster and `examples/dev-1.yaml` is the file that describes it. We will look inside that file in the next section. The command sends the file to the management cluster and then waits while the cluster is built, showing a display like this:

```
Cluster/dev-1  class std · v1.34.11 · self
infrastructure  ████████████ done     ready                                  1m30s
control plane   ██████░░░░░░ running  0/1 nodes                        ~1m10s left
workers         ░░░░░░░░░░░░ pending  waiting
addons          ████████████ done     none

safe to Ctrl-C; `cluster status dev-1` resumes
```

Let us understand this display.

The first line names the cluster, and shows the ClusterClass it was built from (`std`), the Kubernetes version (`v1.34.11`), and where its control plane runs (`self`, meaning on nodes of its own).

The next four lines are the four stages of the build. They are always the same four, in the order in which they usually finish.

- **infrastructure** is the networking the cluster needs before any node can start. Part of it is a **load balancer**, which is a single address that forwards traffic to whichever control plane node is healthy.
- **control plane** is the nodes that take the decisions, as we saw in Section 2.
- **workers** are the nodes on which your programs will run.
- **addons** are the extras that the ClusterClass installs for you. The most important of these is the **network plugin**, which is the piece that lets pods on different nodes talk to each other. Kubernetes does not include one, and nodes stay unhealthy until something provides it. The ClusterClass installs one, so you will not run into this problem.

Each stage line shows a progress bar, a status word (`done`, `running`, or `pending`), a short description of the current state, and a time on the right. The time is based on your own earlier runs. SixFields remembers the timings of your last twenty runs and uses them to estimate how long each stage takes on your machine. Until you have done three runs it shows `no history yet`, because fewer than three is too few to estimate from.

The last line tells you that it is safe to press Ctrl-C. The build does not stop when you do; it carries on in the management cluster. To watch it again, run `cluster status dev-1`.

When all four stages show `done`, the command exits and you have a working cluster called `dev-1`. Section 10 shows how to connect to it.

## 6. The cluster file

Now let us look at the file we just used. Here is the object in `examples/dev-1.yaml`. The file itself opens with a few comment lines, which are left out here:

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

Let us go through it.

The first four lines are bookkeeping that every Kubernetes object has. `apiVersion` and `kind` together say what sort of object this is: a `Cluster`, as defined by Cluster API. `name` is the name of the cluster. `namespace` is like a folder name; Kubernetes uses namespaces to keep objects apart, and `default` is the one that exists already.

Everything under `spec` describes what you want. `topology` means that this cluster is built from a ClusterClass, and `classRef` says which one: `std`, the class that ships with SixFields. `version` is the version of Kubernetes to install. `workers` describes the worker nodes, in one or more groups called machine deployments. This file has one group, named `default`, of the `default` kind of worker, with two replicas. Replicas means copies, so two replicas means two worker nodes.

Of all these lines, six are yours to decide. The rest are either bookkeeping or fixed by the ClusterClass.

| Field | What it means |
|---|---|
| `name` | what to call the cluster |
| `classRef.name` | which ClusterClass to use; `std` is the one that ships |
| `version` | which version of Kubernetes to install |
| `name` (under `machineDeployments`) | a name for this group of worker nodes |
| `class` | which kind of worker node; `default` is the one that ships |
| `replicas` | how many worker nodes you want |

Two more fields are optional:

- `size` may be `dev`, which gives you one control plane node, or `ha`, which gives you three. `ha` stands for high availability; a cluster with three control plane nodes keeps working if one of them fails.
- `placement` may be `self`, which runs the control plane on its own nodes, or `hosted`, which runs it as pods inside the management cluster.

There is one wrinkle with `size`. Setting it to `ha` does not on its own give you three control plane nodes; you must also write the number in the file:

```yaml
  topology:
    controlPlane:
      replicas: 3
    variables:
    - name: size
      value: ha
```

The two have to agree, and the file is refused if they do not. The reason is a limitation of Cluster API: a ClusterClass cannot set the number of control plane nodes, so the number has to be written on the cluster. `cluster new --size ha` writes both for you, and `examples/ha-hosted.yaml` shows the whole file.

Everything else about the cluster, such as the networking, the images, and how the control plane starts, comes from the ClusterClass. Those decisions are made once, in the `assembly/` directory, and every cluster gets the same ones. To see the ClusterClasses your management cluster has, run:

```sh
kubectl get clusterclass
```

You need not write the file by hand. The `cluster new` command writes it for you from the six fields:

```sh
cluster new dev-1 --version v1.34.11 --pool default=2 > cluster.yaml
```

Here `--pool default=2` means a worker group named `default` with two replicas.

## 7. A faster cluster for practising

The cluster we created in Section 5 is a real one. Its nodes are containers, it downloads images, and it takes several minutes to build. That is slower than you want when you are learning.

SixFields ships a second ClusterClass, called `std-inmemory`, for practising. A cluster of this class is only simulated by the management cluster: no containers are created, and the build finishes in about a minute. Everything in this tutorial works the same way on it, so it is a good place to try things out.

```sh
cluster up fast-1 -f examples/fast-1.yaml
```

If you compare `examples/fast-1.yaml` with `examples/dev-1.yaml`, you will find that the only setting that differs is `classRef.name`, which reads `std-inmemory`. The cluster has a different name as well, and the comment at the top of the file is different.

## 8. When a cluster gets stuck

A build does not always finish. Sometimes a node fails to start, an image cannot be downloaded, or a component never reports that it is healthy. Cluster API keeps waiting, and the progress bar stops moving.

When this happens, SixFields prints a short block like the one below. You can also ask for it at any time with `cluster why dev-1`.

```
DevMachine/dev-1-cp-abcde: etcd is not coming up (6m32s)
raw: kubectl get devmachine.infrastructure.cluster.x-k8s.io dev-1-cp-abcde -n default -o yaml
typical: p50 1m15s · p95 2m00s
next: cluster docs CAPI-CP-003
```

Let us read it line by line.

The first line names one object, `DevMachine/dev-1-cp-abcde`, and says what is wrong with it in plain words: etcd is not coming up. **etcd** is the database in which Kubernetes keeps everything it knows, and nothing works until it is running. The time in brackets, `6m32s`, is how long this has been stuck. Note that there are about twenty objects behind every cluster, and when something goes wrong several of them may be reporting problems at once. SixFields picks the one that is the cause and shows only that.

The `raw:` line is a command you can copy and paste. It prints everything Kubernetes knows about that object. The block above is only a summary; nothing is hidden, and this command shows the whole object.

The `typical:` line tells you how long this stage usually takes on your machine, so that you can tell slow from stuck. `p50` is the middle value of your earlier runs: half of them were faster. `p95` is the slow end: only one run in twenty took longer. This line appears once you have built three clusters.

The `next:` line tells you what to do next.

There are five levels of detail in all, each giving more than the last. Most of the time the first is enough.

1. The block above.
2. `cluster why dev-1 --verbose` shows the full text of the problem, and explains why this object was chosen over the others.
3. The `raw:` command shows the object itself, exactly as Kubernetes stores it.
4. `cluster docs CAPI-CP-003` prints a **runbook** for this exact problem: a checklist with the commands to run, and what a good answer and a bad answer look like.
5. `cluster why dev-1 --explain` asks a language model to explain the runbook's findings in three lines. This is optional, and it needs a model running on your own machine. `docs/ai.md` says exactly what is sent and where.

## 9. When there is a mistake in your file

In Section 6 we saw that only six fields are yours. Suppose you set one that belongs to the ClusterClass, say `spec.clusterNetwork`. The file is refused the moment you submit it, with this message:

```
spec.clusterNetwork is managed by ClusterClass 'std'. Set it via the class or use break-glass (docs/eject.md).
```

The message tells you three things: which field is the problem, who owns it (the ClusterClass named `std`), and what to do if you really need to change it. Changing it means either editing the ClusterClass, or switching the six-field rule off, which Section 12 discusses.

You need not submit a file to find out whether it is acceptable. The `plan` command checks a file without building anything:

```sh
cluster plan -f cluster.yaml
```

It prints the same message that `cluster up` would.

## 10. Using your cluster

To use a cluster you need its **kubeconfig**, a small file that holds the cluster's address and a credential. `kubectl` reads this file to know which cluster you mean. Get it, and use it, like this:

```sh
cluster kubeconfig dev-1 > dev-1.kubeconfig
KUBECONFIG=dev-1.kubeconfig kubectl get nodes
```

The first command writes the kubeconfig to a file. The second sets the `KUBECONFIG` environment variable for one command and asks the cluster to list its nodes. For `dev-1` you should see one control plane node and two worker nodes.

There is one thing to know here. The kubeconfig that Cluster API itself writes contains an address that only works from inside Docker. `cluster kubeconfig` replaces it with an address that works from your machine. If you ever fetch the kubeconfig some other way and `kubectl` cannot connect, this is the reason.

## 11. Deleting clusters

SixFields has no delete command of its own, because Cluster API already has one. A cluster is deleted by deleting its `Cluster` object from the management cluster, and Cluster API then removes everything that belongs to it:

```sh
kubectl delete cluster dev-1
```

To remove everything at once, including the management cluster itself, run:

```sh
make dev-down
```

This deletes the management cluster, every workload cluster it made, and all of their containers.

## 12. Leaving SixFields

Every object that SixFields creates is an ordinary Cluster API object. Nothing about your cluster depends on SixFields staying installed, and you can take the objects with you:

```sh
cluster render dev-1 > dev-1-objects.yaml
kubectl diff -f dev-1-objects.yaml && echo "no diff"
```

The first command writes out every object behind the cluster. The second compares that file with what is running; `kubectl diff` exits with 0 when they match, so `no diff` is printed. That file is your whole cluster.

Note, however, that most of these objects are owned by the ClusterClass, so applying the file yourself needs the six-field rule switched off first. `docs/eject.md` shows how to do this without stopping any cluster.

## 13. Exit codes

An exit code is the number a command leaves behind when it finishes, so that a script can tell what happened. Zero always means success. The `cluster` command uses these:

| Code | Meaning |
|---|---|
| 0 | ready |
| 2 | stalled, or still building when the wait ran out |
| 3 | your file was rejected |
| 4 | something on your machine is wrong; run `make doctor` |

## 14. Summary

In this tutorial we have seen that:

- SixFields is built on Cluster API and adds nothing that runs inside your cluster.
- A management cluster, created once with `make dev-up`, builds workload clusters for you.
- A cluster is described by a YAML file in which six fields are yours: the name, the ClusterClass, the Kubernetes version, and the name, kind, and number of worker nodes.
- `cluster up` submits the file and shows four progress bars: infrastructure, control plane, workers, and addons.
- Setting a field you do not own is refused immediately, with the field named and the reason given. `cluster plan` checks a file without building anything.
- When a build gets stuck, `cluster why` names the one object that is the cause, and `cluster docs` prints a runbook for it.
- `cluster kubeconfig` gives you a kubeconfig that works from your machine.
- Clusters of the `std-inmemory` class build in about a minute and are the fastest way to practise.

## 15. Exercises

1. Create an in-memory cluster with three worker nodes instead of two, using `cluster new` to write the file.
2. Add a `spec.clusterNetwork` section to a cluster file and check it with `cluster plan`. Read the message carefully.
3. Start a build, press Ctrl-C halfway through, and pick the display up again with `cluster status`.
4. Run `cluster render` on a finished cluster and count how many objects are behind it.
5. Run `make replay F=inmem-stall-vm`, which replays a recorded run of a build that stalls, and read the block that `why` produces.

---

The rest of this README is reference material.

## 16. Command reference

| Command | What it does |
|---|---|
| `cluster up NAME -f FILE` | submit the file and wait, showing the four progress bars |
| `cluster status NAME` | show the progress bars for a cluster that already exists |
| `cluster why NAME` | name the one object that is blocking |
| `cluster plan -f FILE` | check a file without submitting it |
| `cluster new NAME` | write a file from the six fields |
| `cluster render NAME` | print every object behind a cluster |
| `cluster kubeconfig NAME` | print a kubeconfig that works from your machine |
| `cluster docs CODE` | print the runbook for a problem |
| `cluster explain CODE` | print the short version of a problem |
| `cluster doctor` | check that this machine can create a cluster |
| `cluster skills install` | install the read-only agent skills |
| `cluster version` | print the version of the binary |

Every command accepts `--help`. `status` and `why` accept `--json` when a script is reading the output.

## 17. How the code is organised

```
assembly/     the ClusterClass and the templates it points at
policy/       the rule about which fields you may set, and its tests
cmd/cluster/  the command you type
internal/     the packages the command is made of
docs/         everything you are meant to read
testdata/     recorded runs, and the expected output for each
e2e/          tests that build real clusters
hack/         the scripts behind the make targets (macOS-only today)
```

`internal/` is worth a closer look, because it is organised around one rule: the packages that make decisions do not talk to the network. You give them a snapshot of a cluster, and they give you back an answer. Only the `watch` package talks to Kubernetes.

| Package | What it does |
|---|---|
| `snapshot` | one moment in a cluster's life, as plain data |
| `fold` | turns a snapshot into the four progress bars |
| `why` | picks the one object to name when a build stalls |
| `eta` | keeps the timings of past runs and estimates the next |
| `gen` | turns the six fields into a `Cluster` file |
| `msg` | every sentence the tool can print, in one place |
| `render` | draws the progress bars, the plain text, and the JSON |
| `watch` | reads a live cluster and builds a snapshot |
| `explain` | the optional language model step |

Five more packages under `internal/` exist for the tests: `fixture` writes and reads recorded runs, `golden` compares output against checked-in files, `envtest` starts a real Kubernetes API server, `bench` scores the language model backends, and `style` holds the writing rules for this repository.

Because everything above `watch` works on a snapshot, a recorded run can be replayed through exactly the code that a live run uses. This is why you can develop the display with no cluster at all, and why the tests are fast.

## 18. Working on SixFields

`CLAUDE.md` holds the rules for working in this repository. `PLAN.md` holds the plan. `STATUS.md` says where the work is right now.

There are three kinds of tests:

```sh
make test          # pure logic, under 30 seconds
make test-envtest  # against a real Kubernetes API server
make e2e           # against real clusters
```

You can work on the display without any cluster. SixFields records real runs and replays them:

```sh
make replay F=inmem-stall-vm
```

`docs/api-snapshot.md` is generated from the exact Cluster API version this repository pins. Every condition name used in the code has to appear there, and a test fails if one does not.

### Portability

The tool is not tied to macOS; the setup scripts are. Five things in `hack/` assume a Mac:

1. `make bootstrap` installs tools with Homebrew from a Brewfile.
2. `hack/doctor.sh` reads memory with `sysctl -n hw.memsize`.
3. `hack/doctor-ai.sh` does the same.
4. `hack/envtest-assets.sh` has `--os darwin` hardcoded.
5. `hack/profile.sh` branches over Docker Desktop, OrbStack, and Colima, which are the three macOS Docker runtimes.

Making the setup portable means a package-install path for apt or dnf, a memory probe that works on both systems, removing the hardcoded `darwin`, and one CI run on Linux to prove it. That is about half a day of work, all of it in `hack/`. Until it is done, Linux is untested rather than unsupported.

## 19. Contributing

Read `CONTRIBUTING.md`. In short: the tests are the specification, the tree stays green, and every bug fix begins with a test that fails because of the bug.

To report a security problem privately, follow `SECURITY.md`.

## 20. Related projects

SixFields is not the first attempt at this.

[clusterctl](https://cluster-api.sigs.k8s.io/clusterctl/overview) is the official Cluster API command. It installs providers and it can describe a cluster as a tree of conditions. SixFields uses it, and folds that tree into four progress bars.

[Giant Swarm](https://www.giantswarm.io/) and [Syself](https://syself.com/) both ship opinionated Cluster API platforms, each wrapping a ClusterClass in Helm charts for their own customers. Their field surfaces are the closest thing to the six fields here.

SixFields differs in three ways: it refuses extra fields when you submit the file, it turns the wait into something you can read, and it is vendor neutral, with nothing running inside your cluster.

## 21. Further reading

All of these are free and written for beginners.

- [Kubernetes basics](https://kubernetes.io/docs/tutorials/kubernetes-basics/), an interactive walkthrough from the Kubernetes project.
- [What a pod is](https://kubernetes.io/docs/concepts/workloads/pods/) and [what a node is](https://kubernetes.io/docs/concepts/architecture/nodes/).
- [YAML in ten minutes](https://learnxinyminutes.com/docs/yaml/).
- [Docker's getting started guide](https://docs.docker.com/get-started/).
- [The Cluster API book](https://cluster-api.sigs.k8s.io/), the project SixFields is built on.
- [kind](https://kind.sigs.k8s.io/), which runs the management cluster.
- [kubectl commands](https://kubernetes.io/docs/reference/kubectl/), the ones you will use most.
- [etcd](https://etcd.io/) and [k0s](https://k0sproject.io/), two pieces the ClusterClass uses.

## 22. Licence

[Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0). See `LICENSE`.
