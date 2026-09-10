# capi-distro

Create a Cluster API cluster by writing six fields, and watch it come up as four
phases instead of a condition tree. When it stops, one line names the object that
is blocking and the kubectl command that shows you why.

This is an assembly of upstream Cluster API, not a fork and not a wrapper product.
It adds no CRD and no controller: a curated `ClusterClass`, a
`ValidatingAdmissionPolicy` that keeps users to the six fields, and a CLI.

## First run

```sh
make bootstrap
make dev-up
cluster up dev-1 -f examples/dev-1.yaml
```

`make bootstrap` installs the pinned tools; `make dev-up` brings up a kind
management cluster with Cluster API, CAPD and the assembly; `cluster up` applies
the Cluster and blocks until it is ready. It is safe to Ctrl-C — `cluster status
dev-1` picks the same view back up.

## What you write

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

Two nouns: a cluster, and a pool. Everything else — the control plane, the machine
templates, the bootstrap configs — belongs to the class. Writing one of them is
rejected at admission with a message naming the field, not silently overwritten an
hour later.

`size: dev|ha` and `placement: self|hosted` are the only variables. `cluster new`
writes the file for you:

```sh
cluster new dev-1 --version v1.34.11 --pool default=2 > cluster.yaml
```

## What you see

```
Cluster/dev-1  class std · v1.34.11 · self
infrastructure  ████████████ done     ready                                  1m30s
control plane   ██████░░░░░░ running  0/1 nodes                        ~1m10s left
workers         ░░░░░░░░░░░░ pending  waiting
addons          ████████████ done     none

safe to Ctrl-C; `cluster status dev-1` resumes
```

and when nothing is moving:

```
DevMachine/dev-1-cp-abcde — etcd is not coming up (6m32s)
raw: kubectl get devmachine.infrastructure.cluster.x-k8s.io dev-1-cp-abcde -n default -o yaml
typical: p50 1m15s · p95 2m00s
next: cluster docs CAPI-CP-003
```

The estimate comes from your own previous runs on this machine. It is never shown
for a stalled phase, because it would be a claim about progress that is not
happening.

## When something is wrong

Five rungs, in order. Most people never leave the first.

1. The stall line above.
2. `cluster why dev-1 --verbose` — the full condition text and why this object
   ranked first.
3. The `raw:` line — the kubectl command behind what you were shown.
4. `cluster docs CAPI-CP-003` — the runbook for that stall class.
5. `cluster why dev-1 --explain` — a model explains the analyzer's output. Opt-in,
   off the critical path, and it never invents an object name. See `docs/ai.md`.

`cluster plan -f cluster.yaml` runs the admission rules locally, so a rejection
arrives before you submit rather than after.

## Leaving

`cluster render dev-1` prints every CAPI object behind the cluster; it re-applies
with no diff. `docs/eject.md` is how to remove the policy without touching a
running cluster. The escape hatch is a feature, not an oversight.

## Exit codes

`0` ready · `2` stalled or still running when the wait ended · `3` rejected at
admission · `4` environment.

## Working on this

`CLAUDE.md` is the guardrails, `PLAN.md` the phased plan, `STATUS.md` where the
work is now. `docs/api-snapshot.md` is generated from the pinned Cluster API
release and is the only place a condition name may come from.

```sh
make test          # pure core, under 30s
make test-envtest  # against a real API server
make replay F=inmem-stall-etcd   # iterate on the renderer with no cluster at all
```
