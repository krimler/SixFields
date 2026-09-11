# Decisions

Newest first. Each entry: date, decision, alternatives, why, `REVISIT` if a human
should confirm.

## 2026-09-11 — The project is SixFields, under Apache-2.0

Decision: the name is **SixFields**, after the surface it gives a user, and the licence is
**Apache-2.0** (`LICENSE`).
Alternatives: `capi-distro` (the working name, which describes the category rather than the
idea); LGPL-3.0.
Why the licence: Apache-2.0 is what every CNCF project uses and what the CNCF IP Policy
expects, and it is what Cluster API, k0smotron, Kubernetes and every provider in this repo
already use, so there is nothing to reconcile. LGPL-3.0 is avoided across this ecosystem by
anyone who links or redistributes, and would block adoption and CNCF donation.

The rename reached the Go module path, the break-glass label domain
(`sixfields.io/break-glass`), the break-glass group (`sixfields:break-glass`), the policy
object names, and the kind cluster name.

Two things deliberately did not change. The CLI binary stays `cluster`, because
`cluster up dev-1` reads as the thing you are doing and `sixfields up dev-1` does not; the
project and the command are allowed different names. The error codes keep their `CAPI-`
prefix, because they classify Cluster API stalls and that is what a reader is looking at.
`REVISIT` either if the human disagrees; both are a one-line change.

## 2026-09-11 — Both placements bootstrap with k0s; kubeadm is a rendered fallback

Decision: `std` uses `K0sControlPlaneTemplate` and `K0sWorkerConfigTemplate`,
`std-hosted` uses `K0smotronControlPlaneTemplate` and the same worker bootstrap. The two
classes differ in exactly one reference, which is PLAN.md Phase 5's acceptance.
Alternatives: keep kubeadm for `self` (two provider families, two sets of condition types
to fold, and the fold table doubles); make k0s a third class (three classes to install and
explain).
Why: one bootstrap mechanism means one set of conditions in `internal/fold`, one runbook
vocabulary, and a worker that joins the same way whatever runs its control plane.
`std-kubeadm` is still rendered and tested so a reader who needs kubeadm finds a working
class rather than reconstructing one; `make dev-up` does not install it.

## 2026-09-11 — Three things k0s on CAPD needs, each with its citation

Found by running it, not by reading:

1. `--enable-worker=true` on the control plane. A k0s controller is not a Kubernetes node
   unless it also runs a worker, so the control-plane Machine never gets a `nodeRef`,
   never becomes Ready, and CAPI's MachineSet preflight holds every worker for ever. The
   control-plane taint still keeps ordinary workloads off it.
2. `machineset.cluster.x-k8s.io/skip-preflight-checks: ControlPlaneIsStable` on the worker
   class. `K0sControlPlane` reports `status.version` as the k0s release (`v1.34.11+k0s.0`)
   while the MachineSet carries the Kubernetes version (`v1.34.11`); CAPI compares them as
   strings and concludes an upgrade is permanently pending. Only that one check is
   skipped. `REVISIT` when k0smotron reports a Kubernetes version.
3. The k0s version must carry the same Kubernetes version the assembly installs
   everywhere else. A control plane on one minor and a topology version on another leaves
   workers unable to fetch the `worker-config-default-<minor>` ConfigMap their k0s expects:
   they reach the API server, authenticate, and exit.

## 2026-09-11 — k0s bootstrap fetches its binary at machine boot

Finding. The k0s bootstrap runs `curl https://get.k0s.sh | sh` on each machine, so
provisioning depends on a network fetch that can fail — it did, with
`curl: (56) OpenSSL SSL_read: unexpected eof`, and the machine sat at `Provisioned` for
an hour. kubeadm does not have this property: its node image already contains everything.
Taken: nothing, beyond recording it. `cluster why` named the DevMachine and quoted the
curl error on the first try, which is the behaviour this project exists to provide.
`REVISIT` if it recurs often enough to be worth pre-installing k0s in the node image
(`K0sWorkerConfigSpec.preInstalledK0s` exists for that).

## 2026-09-11 — Three CAPI immutabilities that shape the dev loop

CAPI refuses, in three places, to change a control plane's kind after the fact: a
Cluster's placement cannot change, a Cluster's class cannot change to one with a
different control-plane kind, and a ClusterClass's own control-plane kind cannot change in
place ("to prevent incompatible changes in the Clusters"). Consequence for the dev loop:
switching a class's bootstrap is a recreate. `hack/dev-up.sh` recreates a class whose
control plane changed and refuses while any Cluster uses it, naming them. Consequence for
a user: `placement` is chosen once, which belongs in the user guide.

## 2026-09-11 — The assembly ships a CNI

Decision: `hack/addons.sh` installs a `ClusterResourceSet` with a digest-pinned Calico
manifest, selected by the `topology.cluster.x-k8s.io/owned` label CAPI puts on every
Cluster built from a class.
Alternatives: leave the CNI to the user (then no cluster from this class ever reaches
Ready, which is what happened); a `sixfields.io/cni` label the user sets (a seventh
field); an empty selector (CAPI rejects it).
Why: an opinionated assembly that produces a cluster whose nodes never become Ready is not
an assembly. It also makes the fourth phase report something real instead of "none".

## 2026-09-11 — The local model is `qwen2.5:14b`, picked by measurement

`make doctor-ai` on the 24 GB reference machine: 24 − 5 (container VM, dev profile) − 5
(macOS reserve) = 14 GB for the model, so the heaviest dense Qwen that fits with headroom.
It measured 6.6 tok/s on a cold load and refused the model, then 26 tok/s warm and
accepted it — the 8 tok/s floor exists because a slow `--explain` is worse than the runbook
alone. Pinned in `versions.env` with its quantisation, digest and runtime. The seven
cassettes in `testdata/cassettes/` are recorded from it and every one passes the grounding
check, so no answer that named an object the analyzer never saw has ever reached a user.

## 2026-09-10 — The next action comes from the message registry, not from the renderer

The renderer used to print `cluster docs <code>` under every stall. It now prints
`msg.Get(code).NextAction`, so the action a user is given is the same one
`cluster explain` gives and the same one the runbook's own example shows. One stall class
(`CAPI-VERSION-001`) sends the user to a field to change rather than to a runbook, and
that difference is now visible everywhere instead of only in the registry.
`TestUX_RunbookExamplesMatchTheRealStallBlock` fails if a runbook's opening example drifts
from what the renderer emits.

## 2026-09-10 — The runbook is its own rung; `cluster why` does not print it

`cluster why` prints the stall line, the raw line and the next action. It prints the
runbook only under `--explain`, where the runbook is the thing the model's three lines sit
under and the thing that stands alone when the model is slow or absent. Printing a
two-page runbook unasked would bury the one line the command exists to show.

## 2026-09-10 — gocritic's hugeParam and rangeValCopy are off, with a reason

The pure core takes values and returns values on purpose: a caller can never be surprised
by a mutation, and a fixture can be folded twice with the same result. Those two checks
trade that property for a copying win this workload — tens of objects per snapshot — does
not need. `revive`'s `exported` rule is off for the same kind of reason: its failure mode
is a comment that restates the declaration, which CLAUDE.md's no-slop rule forbids. Every
other rule the plan names is on and the tree passes with zero issues.

## 2026-09-10 — Runbooks and skills live in two places, and a test keeps them identical

`docs/runbooks/` is where a person reads them; `internal/msg/runbooks/` is what
`go:embed` can reach, so `cluster docs` works offline. `make sync-embeds` copies, and
`TestUX_EmbeddedRunbooksMatchDocs` fails on drift. The same applies to `skills/` and
`cmd/cluster/skills/`. Alternatives rejected: a symlink (go:embed does not follow them)
and moving the canonical copy inside `internal/` (a reader would not find it).

## 2026-09-10 — envtest uses minimal CRDs, not the real Cluster API ones

`testdata/crds/minimal-capi.yaml` declares the group, version and names of the kinds the
envtest layer needs, with `x-kubernetes-preserve-unknown-fields`. That is enough for what
this layer checks — that the admission policy compiles under the API server's own type
checker and denies, and that the watcher's discovery finds the right resources. Vendoring
the real CRDs would add megabytes to the repo and test the CRDs rather than this code;
`make e2e` exercises the real ones against a live install.

## 2026-09-10 — `--redact-ips` is one-way

Names are replaced reversibly, so a user reads their own vocabulary in the answer.
Addresses are replaced with a placeholder that cannot be reversed, so a model can never
repeat one back. Redaction defaults off (an address is often the whole answer) and
anonymisation defaults on only for the `anthropic` backend, because with `local` nothing
leaves the machine.

## 2026-09-10 — `CLUSTER_AI` defaults to `explain`

`off | explain | all`, defaulting to `explain`: the one AI rung that has earned its place
is the one a user asks for by name. `make test` never calls a model — the default gate
runs `go test ./...`, and every AI path is covered by the `noop` and `cassette` backends.
`docs/ai.md` states what leaves the machine per backend.

## 2026-09-10 — PLAN.md is wrong: `spec.topology.class` is `classRef` at v1beta2

Finding. PLAN.md Phase 1's example Cluster and Phase 2's allow-list both name
`spec.topology.class`. That field does not exist in the pinned release: v1beta2 spells it
`spec.topology.classRef.{name,namespace}` (`api@v1.14.2 core/v1beta2/cluster_types.go`,
`Topology.ClassRef ClusterClassRef`), and upstream's own CAPD examples at this tag use
`classRef:`.
Taken: `internal/gen` writes and reads `classRef`, `internal/fold` reads
`spec.topology.classRef.name`, the fixtures use it, and the admission policy allows
`classRef.{name,namespace}`. Writing the v1beta1 spelling is now a denial with its own
negative test (`TestGen_DeniedFieldsAreRejected/v1beta1 class spelling`), because a user
copying an older example is the likeliest way to hit it.
Proposed PLAN.md fix: replace `class: std` with `classRef: {name: std}` in the Phase 1
snippet and in the Phase 2 allow-list. Left unedited so the human sees the original.

## 2026-09-10 — `size` is expanded by the generator, not by a class patch

Finding, then a decision. PLAN.md Phase 1 says "Patches: set control-plane replicas from
`size`". That cannot work at v1.14.2 and would fail *silently*: `KubeadmControlPlaneTemplate`
has no `replicas` field, and the topology patch engine lists the control plane's
`spec.replicas` in `PreserveFields`, so a patched value is discarded without an error
(`core/reconcilers/topology/cluster/patches/engine.go`). The only input is
`Cluster.spec.topology.controlPlane.replicas`.
Alternatives: drop `size` (no way to ask for HA); expose `controlPlane.replicas` as a
seventh user field (breaks the six-field thesis).
Taken: `size` stays the user-facing variable, `gen.ControlPlaneReplicas` maps it
(`dev`->1, `ha`->3), and the generator writes `spec.topology.controlPlane.replicas`. The
admission policy therefore cannot simply deny that field: it allows it only when it agrees
with `size`, and denies every other field under `spec.topology.controlPlane`. One function
holds the mapping, so the CLI and the policy cannot drift.
`REVISIT` if CAPI ever lets a class patch reach control-plane replicas.

## 2026-09-10 — Golden files update by environment variable as well as by flag

`-update` is registered by `internal/golden`, so `go test ./... -update` fails in packages
that do not import it. `make golden` sets `UPDATE_GOLDEN=1` instead, which works across the
whole tree; the flag still works inside a single package.

## 2026-09-10 — Only the frontier phase can stall

A phase is called stalled only when it is the earliest phase that is not done. Workers
sitting at 0/2 while the control plane is stuck are waiting, not stuck, and naming them
would point the user at the wrong object. The same rule suppresses the ETA on phases behind
the frontier: their own history says nothing about how long the thing in front of them will
take.

## 2026-09-10 — Pin CAPI v1.14.2, contract v1beta2

Decision: `CAPI_VERSION=v1.14.2`, `CAPD_VERSION=v1.14.2`, `CLUSTERCTL_VERSION=v1.14.2`.
Alternatives: v1.13.6 (previous minor, same contract), v1.11.x (the floor PLAN.md names).
Why: v1.14.0 shipped 2026-07-30 and v1.14.2 is its second patch, so the line has had six
weeks of fixes; `metadata.yaml` declares contract `v1beta2` for 1.11 through 1.14, so
nothing in PLAN.md changes by taking the newest.

## 2026-09-10 — API types are read, not linked

Decision: the CLI module does not import `sigs.k8s.io/cluster-api`. `hack/tools` is a
separate, stdlib-only module whose `apisnapshot` command parses the pinned API packages
out of the module cache and writes `docs/api-snapshot.{md,json}`.
Alternatives: import the CAPI types into the CLI and use the Go constants directly.
Why: CLAUDE.md requires the pure core to import no Kubernetes client, and the CAPI api
module declares `go 1.26.0`, which would force the whole project's toolchain. Reading the
source keeps the dependency at zero and still makes "never invent API details" checkable:
`docs/api-snapshot.json` is the allow-list the jargon lint and the fold table are
generated from.

## 2026-09-10 — MachinePool folds from replica counters, not conditions

Decision: `internal/fold` reads MachinePool `status.{replicas,readyReplicas,availableReplicas}`
and treats its `status.conditions` as best-effort.
Alternatives: fold MachinePool the same way as MachineDeployment, from condition types.
Why: CAPI v1.14.2 ships **no** v1beta2 condition constants for MachinePool — the block is
commented out with "not yet implemented"
(`core/v1beta2/machinepool_types.go:31`, recorded in `docs/api-snapshot.md`). PLAN.md
Phase 3 says "per MachinePool ready-replicas vs desired", which happens to be right; this
entry records *why* it has to be that way. `REVISIT` when CAPI implements them.

## 2026-09-10 — k0smotron is a v1beta1-contract provider (risk for Phases 4-5)

Finding, not a decision: k0smotron v1.10.9 declares contract `v1beta1` for every release
series (`metadata.yaml`), while the pinned CAPI implements `v1beta2`. CAPI v1.14 still
accepts v1beta1-contract providers, but only "temporarily ... until v1beta1 is EOL"
(`sigs.k8s.io/cluster-api/internal/contract/version.go:49`), targeted at v1.16.
Consequence: Phases 4 and 5 work on the pinned release; they will break on a CAPI bump
past v1beta1 removal. Taken: keep kubeadm as the `self` control plane until Phase 5, keep
the fold layer contract-agnostic (it reads `status.conditions` and falls back to
`status.deprecated.v1beta1.conditions`). `REVISIT` before bumping CAPI past v1.15.

## 2026-09-10 — Provider names verified against clusterctl

`clusterctl config repositories` on the pinned clusterctl v1.14.2 gives: core
`cluster-api`, bootstrap/control-plane `kubeadm`, infrastructure `docker`, and
`k0sproject-k0smotron` for bootstrap, control-plane and infrastructure. There is no
separate in-memory provider: the in-memory backend is part of CAPD, selected per object
with `spec.backend.inMemory`. `hack/clusterctl-init.sh` uses exactly these names.

## 2026-09-10 — Repo layout follows CLAUDE.md, with the build spec kept

The original build spec was handed over as a file named `prompt`; it is now
`docs/build-spec.md`, unedited. CLAUDE.md is Part A of it and PLAN.md is Parts B and D,
so the guardrails and the plan are where CLAUDE.md says they are.
