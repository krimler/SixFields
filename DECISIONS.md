# Decisions

Newest first. Each entry: date, decision, alternatives, why, `REVISIT` if a human
should confirm.

## 2026-09-11, The in-memory stall comes from the VM stage, not from etcd

Finding. PLAN.md D1 and D3 both say a deterministic stall is induced by setting one
component's `startupDuration` past `stallAfter`, and name etcd. Setting etcd to 30m does
not stall anything: the cluster reached Ready in 56s with that value in the machine's spec.
The wait is measured from the `NodeProvisioned` condition's transition time
(`test@v1.14.2 infrastructure/docker/internal/controllers/backends/inmemory/inmemorymachine_backend.go`),
and it is skipped once the machine carries the bootstrapped annotation.

Setting the **vm** duration does stall, because its clock starts from the cloud machine's
creation timestamp, which always exists. `inmem-stall-vm` is recorded from a real run of
that, and it replaces the two synthetic in-memory stalls PLAN.md asked for.

Consequence worth keeping: the other three conditions report
`WaitingForVMProvisioned`, so a ranker that ignores that names a symptom. `internal/why`
now ranks a condition below the one its reason names.

## 2026-09-11, The in-memory substrate is its own class

Decision: `std-inmemory` is a separate ClusterClass with its own `Dev*` templates.
Alternatives: one class with two overlays swapping the backend (what the repo had, which
had never been run).
Why: CAPD's in-memory backend simulates a kubeadm control plane, so it cannot use the k0s
control plane `std` now has; and `Dev*Template.spec.template.spec` is immutable, so two
backends need two objects under two names. Installing both under one name fails with
"spec.template.spec field is immutable". The six fields a user writes are unchanged, and
the class name is the only difference in their file.

## 2026-09-11, cluster-bench measures fidelity, because the baseline is the ceiling

PLAN.md D4 says an AI feature that does not beat the no-AI baseline is not shipped. On this
benchmark the baseline cannot be beaten: `why` decides and the model only explains, so the
`noop` backend is correct by construction and is five orders of magnitude faster. Read
literally, `--explain` never ships.

What the benchmark measures instead is fidelity and its cost: routing the analyzer's
finding through a model, does the object and the stall class survive, and what does that
take. Measured on the pinned local model: 6 of 6 tasks, 0 ungrounded, median 13s against
the analyzer's 250ns. `docs/bench.md` states the rule and then states this. `REVISIT` if
`/cluster-fix` ever makes the model decide something, because then the rule applies again.

Two related facts. "Interactions to answer" is not measurable here: `Explainer` is one
request and one answer, so the count is 1 everywhere. And the `anthropic` arm has never
run, because `AI_CREDIT_CAP_USD` is 0 and CLAUDE.md puts spending past that cap on the
must-ask list.

## 2026-09-11, The project is SixFields, under Apache-2.0

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

## 2026-09-11, Both placements bootstrap with k0s; kubeadm is a rendered fallback

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

## 2026-09-11, Three things k0s on CAPD needs, each with its citation

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

## 2026-09-11, k0s bootstrap fetches its binary at machine boot

Finding. The k0s bootstrap runs `curl https://get.k0s.sh | sh` on each machine, so
provisioning depends on a network fetch that can fail, it did, with
`curl: (56) OpenSSL SSL_read: unexpected eof`, and the machine sat at `Provisioned` for
an hour. kubeadm does not have this property: its node image already contains everything.
Taken: nothing, beyond recording it. `cluster why` named the DevMachine and quoted the
curl error on the first try, which is the behaviour this project exists to provide.
`REVISIT` if it recurs often enough to be worth pre-installing k0s in the node image
(`K0sWorkerConfigSpec.preInstalledK0s` exists for that).

## 2026-09-11, Three CAPI immutabilities that shape the dev loop

CAPI refuses, in three places, to change a control plane's kind after the fact: a
Cluster's placement cannot change, a Cluster's class cannot change to one with a
different control-plane kind, and a ClusterClass's own control-plane kind cannot change in
place ("to prevent incompatible changes in the Clusters"). Consequence for the dev loop:
switching a class's bootstrap is a recreate. `hack/dev-up.sh` recreates a class whose
control plane changed and refuses while any Cluster uses it, naming them. Consequence for
a user: `placement` is chosen once, which belongs in the user guide.

## 2026-09-11, The assembly ships a CNI

Decision: `hack/addons.sh` installs a `ClusterResourceSet` with a digest-pinned Calico
manifest, selected by the `topology.cluster.x-k8s.io/owned` label CAPI puts on every
Cluster built from a class.
Alternatives: leave the CNI to the user (then no cluster from this class ever reaches
Ready, which is what happened); a `sixfields.io/cni` label the user sets (a seventh
field); an empty selector (CAPI rejects it).
Why: an opinionated assembly that produces a cluster whose nodes never become Ready is not
an assembly. It also makes the fourth phase report something real instead of "none".

## 2026-09-11, The local model is `qwen2.5:14b`, picked by measurement

`make doctor-ai` on the 24 GB reference machine: 24 − 5 (container VM, dev profile) − 5
(macOS reserve) = 14 GB for the model, so the heaviest dense Qwen that fits with headroom.
It measured 6.6 tok/s on a cold load and refused the model, then 26 tok/s warm and
accepted it, the 8 tok/s floor exists because a slow `--explain` is worse than the runbook
alone. Pinned in `versions.env` with its quantisation, digest and runtime. The seven
cassettes in `testdata/cassettes/` are recorded from it and every one passes the grounding
check, so no answer that named an object the analyzer never saw has ever reached a user.

## 2026-09-10, The next action comes from the message registry, not from the renderer

The renderer used to print `cluster docs <code>` under every stall. It now prints
`msg.Get(code).NextAction`, so the action a user is given is the same one
`cluster explain` gives and the same one the runbook's own example shows. One stall class
(`CAPI-VERSION-001`) sends the user to a field to change rather than to a runbook, and
that difference is now visible everywhere instead of only in the registry.
`TestUX_RunbookExamplesMatchTheRealStallBlock` fails if a runbook's opening example drifts
from what the renderer emits.

## 2026-09-10, The runbook is its own rung; `cluster why` does not print it

`cluster why` prints the stall line, the raw line and the next action. It prints the
runbook only under `--explain`, where the runbook is the thing the model's three lines sit
under and the thing that stands alone when the model is slow or absent. Printing a
two-page runbook unasked would bury the one line the command exists to show.

## 2026-09-10, gocritic's hugeParam and rangeValCopy are off, with a reason

The pure core takes values and returns values on purpose: a caller can never be surprised
by a mutation, and a fixture can be folded twice with the same result. Those two checks
trade that property for a copying win this workload, tens of objects per snapshot, does
not need. `revive`'s `exported` rule is off for the same kind of reason: its failure mode
is a comment that restates the declaration, which CLAUDE.md's no-slop rule forbids. Every
other rule the plan names is on and the tree passes with zero issues.

## 2026-09-10, Runbooks and skills live in two places, and a test keeps them identical

`docs/runbooks/` is where a person reads them; `internal/msg/runbooks/` is what
`go:embed` can reach, so `cluster docs` works offline. `make sync-embeds` copies, and
`TestUX_EmbeddedRunbooksMatchDocs` fails on drift. The same applies to `skills/` and
`cmd/cluster/skills/`. Alternatives rejected: a symlink (go:embed does not follow them)
and moving the canonical copy inside `internal/` (a reader would not find it).

## 2026-09-10, envtest uses minimal CRDs, not the real Cluster API ones

`testdata/crds/minimal-capi.yaml` declares the group, version and names of the kinds the
envtest layer needs, with `x-kubernetes-preserve-unknown-fields`. That is enough for what
this layer checks, that the admission policy compiles under the API server's own type
checker and denies, and that the watcher's discovery finds the right resources. Vendoring
the real CRDs would add megabytes to the repo and test the CRDs rather than this code;
`make e2e` exercises the real ones against a live install.

## 2026-09-10, `--redact-ips` is one-way

Names are replaced reversibly, so a user reads their own vocabulary in the answer.
Addresses are replaced with a placeholder that cannot be reversed, so a model can never
repeat one back. Redaction defaults off (an address is often the whole answer) and
anonymisation defaults on only for the `anthropic` backend, because with `local` nothing
leaves the machine.

## 2026-09-10, `CLUSTER_AI` defaults to `explain`

`off | explain | all`, defaulting to `explain`: the one AI rung that has earned its place
is the one a user asks for by name. `make test` never calls a model, the default gate
runs `go test ./...`, and every AI path is covered by the `noop` and `cassette` backends.
`docs/ai.md` states what leaves the machine per backend.

## 2026-09-10, PLAN.md is wrong: `spec.topology.class` is `classRef` at v1beta2

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

## 2026-09-10, `size` is expanded by the generator, not by a class patch

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

## 2026-09-10, Golden files update by environment variable as well as by flag

`-update` is registered by `internal/golden`, so `go test ./... -update` fails in packages
that do not import it. `make golden` sets `UPDATE_GOLDEN=1` instead, which works across the
whole tree; the flag still works inside a single package.

## 2026-09-10, Only the frontier phase can stall

A phase is called stalled only when it is the earliest phase that is not done. Workers
sitting at 0/2 while the control plane is stuck are waiting, not stuck, and naming them
would point the user at the wrong object. The same rule suppresses the ETA on phases behind
the frontier: their own history says nothing about how long the thing in front of them will
take.

## 2026-09-10, Pin CAPI v1.14.2, contract v1beta2

Decision: `CAPI_VERSION=v1.14.2`, `CAPD_VERSION=v1.14.2`, `CLUSTERCTL_VERSION=v1.14.2`.
Alternatives: v1.13.6 (previous minor, same contract), v1.11.x (the floor PLAN.md names).
Why: v1.14.0 shipped 2026-07-30 and v1.14.2 is its second patch, so the line has had six
weeks of fixes; `metadata.yaml` declares contract `v1beta2` for 1.11 through 1.14, so
nothing in PLAN.md changes by taking the newest.

## 2026-09-10, API types are read, not linked

Decision: the CLI module does not import `sigs.k8s.io/cluster-api`. `hack/tools` is a
separate, stdlib-only module whose `apisnapshot` command parses the pinned API packages
out of the module cache and writes `docs/api-snapshot.{md,json}`.
Alternatives: import the CAPI types into the CLI and use the Go constants directly.
Why: CLAUDE.md requires the pure core to import no Kubernetes client, and the CAPI api
module declares `go 1.26.0`, which would force the whole project's toolchain. Reading the
source keeps the dependency at zero and still makes "never invent API details" checkable:
`docs/api-snapshot.json` is the allow-list the jargon lint and the fold table are
generated from.

## 2026-09-10, MachinePool folds from replica counters, not conditions

Decision: `internal/fold` reads MachinePool `status.{replicas,readyReplicas,availableReplicas}`
and treats its `status.conditions` as best-effort.
Alternatives: fold MachinePool the same way as MachineDeployment, from condition types.
Why: CAPI v1.14.2 ships **no** v1beta2 condition constants for MachinePool, the block is
commented out with "not yet implemented"
(`core/v1beta2/machinepool_types.go:31`, recorded in `docs/api-snapshot.md`). PLAN.md
Phase 3 says "per MachinePool ready-replicas vs desired", which happens to be right; this
entry records *why* it has to be that way. `REVISIT` when CAPI implements them.

## 2026-09-10, k0smotron is a v1beta1-contract provider (risk for Phases 4-5)

Finding, not a decision: k0smotron v1.10.9 declares contract `v1beta1` for every release
series (`metadata.yaml`), while the pinned CAPI implements `v1beta2`. CAPI v1.14 still
accepts v1beta1-contract providers, but only "temporarily ... until v1beta1 is EOL"
(`sigs.k8s.io/cluster-api/internal/contract/version.go:49`), targeted at v1.16.
Consequence: Phases 4 and 5 work on the pinned release; they will break on a CAPI bump
past v1beta1 removal. Taken: keep kubeadm as the `self` control plane until Phase 5, keep
the fold layer contract-agnostic (it reads `status.conditions` and falls back to
`status.deprecated.v1beta1.conditions`). `REVISIT` before bumping CAPI past v1.15.

## 2026-09-10, Provider names verified against clusterctl

`clusterctl config repositories` on the pinned clusterctl v1.14.2 gives: core
`cluster-api`, bootstrap/control-plane `kubeadm`, infrastructure `docker`, and
`k0sproject-k0smotron` for bootstrap, control-plane and infrastructure. There is no
separate in-memory provider: the in-memory backend is part of CAPD, selected per object
with `spec.backend.inMemory`. `hack/clusterctl-init.sh` uses exactly these names.

## 2026-09-10, Repo layout follows CLAUDE.md, with the build spec kept

The original build spec was handed over as a file named `prompt`; it is now
`docs/build-spec.md`, unedited. CLAUDE.md is Part A of it and PLAN.md is Parts B and D,
so the guardrails and the plan are where CLAUDE.md says they are.

## 2026-09-12, thirteen defects found by a control study, and what was done about each

A two-arm control study (`paper/study/`) ran the three pre-registered first-use
tasks against this tool and against `clusterctl` alone. Thirteen defects came out
of it, counted as distinct repairs, each carrying its own regression test that
fails if the repair is reverted. Twelve were properties of the system as measured;
the thirteenth was introduced by two of the repairs and caught by re-measuring.
Nine are below and four in the entry that follows. The numbers are before and
after, measured on the same live cluster.

**1. A misspelt ClusterClass name was refused by nothing.** The API server
accepts `classRef.name: std-inmemroy` with a warning, stores the Cluster, and
creates nothing; the policy passed it, and `cluster plan` printed "no field is
rejected". An admission policy cannot fix this: it sees only the object being
written and cannot read a second object to learn whether the class exists. So
`cluster up` looks the class up before applying (`cmd/cluster/class.go`), fails
with `CAPI-ENV-003` and suggests the nearest installed name. `cluster plan` stays
offline and now says which check it did not do. Alternative considered: a
`paramKind` on the policy naming the installed classes, rejected because it would
have to be regenerated whenever a class is installed.

**2. `cluster why` took 5.4s against 84ms for `clusterctl describe`.** Not the
model: `CLUSTER_AI=off` made no difference and the process sat at 2% CPU. It was
client-go's own rate limiter, 5 queries a second by default, against a snapshot
that lists about forty CAPI resource types. `internal/watch` now opts out
(`config.QPS = -1`), as kubectl does, and leaves the limiting to the API server's
priority and fairness. 5,450ms to 81ms. The lists were also made concurrent,
which on its own changed nothing and is kept because it costs nothing and the
sum-of-latencies shape would return the moment a provider adds resource types.
`Client.Throttled` exists so an envtest asserts the setting. 5,450 ms to 65 ms;
readings of 81 ms and 66 ms appear in earlier runs of the same build, and
`paper/study/data/stream-output.csv` carries the current one.

**3. `DevMachine` was not a managed kind.** `kubectl patch devmachine` succeeded
for an ordinary user, so "users write exactly one kind" was not true of the
infrastructure machine object. The test that asserted this was correct behaviour
gave the reason "denying these would break the class itself", and that reason is
wrong: a DevMachine is created by the MachineSet controller and reconciled by
CAPD's, and both service accounts are exempt. Added `DevMachine`,
`DockerMachine`, `DevMachinePool`, `DockerMachinePool`, `K0sWorkerConfig` and
`K0sControllerConfig`. Verified live: the write is refused and a cluster still
reaches Ready in 48s. The list now lives in `gen.ManagedKinds` as well as in the
CEL, and `TestPolicy_ManagedKindListMatchesTheGenerator` fails if they drift.

**4. `cluster render` omitted what the rendered file depends on.** It emitted 18
objects for a cluster whose control arm counterparts captured 32 to 34. The
rendered Cluster keeps `spec.topology`, so it points at a ClusterClass the file
did not contain: the escape hatch depended on the thing it exists to escape.
`Reachable` now follows `spec.topology.classRef` to the class and the class's
`templateRef`s to its templates. 18 to 24 objects, and `kubectl diff` still exits
0 clean.

Secrets are still not printed, and that is deliberate: CLAUDE.md's non-goals rule
out secrets handling beyond what clusterctl does, and writing a cluster's
certificate authority into a file as a side effect of `render` is not something
this command should do quietly. The defect was the silence, not the omission, so
render now names on stderr exactly which Secrets it skipped and gives the command
that fetches them. `REVISIT`: if `cluster render` is ever meant to produce a file
that recreates a cluster elsewhere rather than one that describes the cluster you
have, this decision has to be reopened, and `clusterctl move` is the prior art.

Following owner references out of a ClusterClass had to be stopped at the same
time. CAPI adds an owner reference to every template a class has ever named and
never removes it: on the live cluster `std-control-plane-machine` was owned by
both `std` and `std-inmemory`, and expanding those references pulled the docker
class's templates into an in-memory cluster's render. A class's children are the
templates in its spec.

**5. `cluster plan` named the wrong ClusterClass and read managed kinds as
Clusters.** It printed `is managed by ClusterClass 'std'` for a `std-inmemory`
cluster, because `gen.FromObject` emitted the denials for the fields beside
`topology` before it had read `classRef.name`. The server-side policy interpolated
the real name all along, so this was the client disagreeing with the thing it
claims to reproduce character for character. Reading a `MachineDeployment` field
by field was the same bug in another form: it produced "spec.clusterName is
managed by ClusterClass 'std'", naming a field that is legitimate on that kind and
a class the object does not carry. A non-Cluster managed kind now returns
`KindManaged`, which is the wording the policy uses and names no class.

**6. `cluster render --help` still promised the check the documents had
dropped.** README, the user guide and `docs/eject.md` were corrected to
`kubectl diff` after the first-use study; the command's own help still offered
`cluster render NAME | kubectl apply --dry-run=server -f -`, which the policy
denies for 13 of 18 objects. It is the copy nearest to being run. The help now
gives the diff and says why the dry-run apply is the wrong check.

**7. Grounding did not read the line the reader runs.** Judging six local-model
answers by hand found two that named a real object under a kind it does not have,
`kubectl get kubeadmcontrolplane hosted-1-cp` for a K0smotronControlPlane and the
same for a Cluster's name. Both return NotFound, and both scored as correct,
because `Explanation.Grounded` checked the three lines and not `next_command`.
It now checks that a `kubectl get|describe` target names an object the model was
given, under a kind that object has. The local backend's `UNGROUNDED` count went
from 0 to 2 on the same six tasks, which is the check working rather than the
model getting worse. One name can belong to two kinds, because CAPI names an
infrastructure machine after its Machine, and the check accepts either.

**8. `cluster render` left a dangling ClusterResourceSetBinding.** Re-running the
eject task after fix 4 was what found it: all three subjects independently
reported that the file carries the binding recording that Calico was applied and
not the `ClusterResourceSet` that defines it, so the CNI would not be reinstalled
from the file. `Reachable` now follows `spec.bindings[].clusterResourceSetName`,
which is the only link, since the set carries no cluster label and does not own
the binding. The ConfigMap the set applies is in the core group, which this tool
does not read, and is named on stderr beside the Secrets.

**9. Owner references must not be followed out of a shared definition.** Both
render fixes leaked on their first attempt and both were caught by rendering a
live cluster rather than by a test. A ClusterClass owns every template it has ever
named, so an in-memory cluster's file gained the docker class's templates; a
ClusterResourceSet owns every binding it has applied, so following it took a
24-object render to 37, the extra thirteen being other clusters' bindings.
`sharedDefinitions` names the two kinds a cluster refers to rather than owns, and
the owner-reference pass stops at them. `REVISIT`: this is a list of two, and a
third shared kind would have to be added by hand. A rule derived from the
contract, rather than a list, would be worth more.

### What this cost, and what it says about the method

Nine defects at this point, of which the first-use study had already found four and
fixed them. Four of the five new ones were invisible to the fixture-backed suite for the same
reason as the eleven in the paper: the fixtures encode the same understanding as
the code. Two were found only by a control arm, which is the argument for running
one; two more were found only by re-measuring after a fix, which is the argument
for re-measuring. The latency defect was found by timing a command that the test
suite only ever asserts the output of.

## 2026-09-12, three gaps the control study left open

**Item 1, the two refusals that were client-side only.**

`04-version-no-v` was not a defect. Cluster API's mutating webhook prepends a missing `v`
before anything else sees the object, with the comment "Tolerate version strings without a
v prefix: prepend it if it's not there" (cluster-api@v1.14.2
core/webhooks/admission/cluster.go:92), and validating admission runs after mutating
admission, so the policy is handed `v1.34.11` whatever the user typed. The study recorded a
tolerated input as an uncaught mistake. The case is reclassified `tolerated`, and
`internal/gen` now prefixes before matching, because refusing `1.34.11` made `cluster plan`
stricter than the server and broke the one promise it makes.

What is a real mistake is a version no prefix can rescue: `latest` becomes `vlatest`,
`${KUBERNETES_VERSION}` survives unsubstituted, `v1.34` has no patch. The policy now denies
those, with the same text `internal/msg` produces, and `15-version-placeholder` is the case.
The message quotes the version after mutation, so a user who typed `latest` is told about
`vlatest`; that is inherent to validating-after-mutating and is left as it is, because the
alternative is a message that does not match the stored object.

`02-class-typo` stays client-side, and this is a decision rather than an omission.

A `ValidatingAdmissionPolicy` cannot ask whether a named object exists. `paramRef` takes a
fixed name or a label selector, and with a selector "multiple params are found, they are all
evaluated with the policy expressions and the results are ANDed together"
(k8s.io/api@v0.37.0 admissionregistration/v1/types.go:582), so selecting every ClusterClass
expresses "every class has this name", not "some class does". The only expressible form is a
single ConfigMap listing installed class names.

That was rejected. Cluster API polls for two seconds waiting for the class to appear and be
reconciled before it downgrades to a warning
(core/webhooks/admission/cluster.go:989), which is deliberate tolerance of a Cluster and its
ClusterClass being applied in either order, as one kustomize output or one GitOps sync does.
A static list refuses exactly that case, and refuses it with a message that is wrong: the
class is there, the list has not caught up. Trading a benign warning for a false refusal
makes the failure worse, not earlier. `cluster up` looks the name up instead, which is
allowed to be wrong in the harmless direction because it runs before the write.

The result is 12 of 13 refused at admission and one advised by the client. `REVISIT` if
Cluster API ever makes a missing class an error, or if VAP gains a lookup.

**Item 2, the command under the model's lines.**

`AnalyserCommand` wraps every backend and replaces whatever command it returned with
`req.Stall.Raw`, the command the analyser had already written for the object it ranked. The
prompt no longer asks for one. Both construction sites wrap, and a test asserts each backend
the CLI can pick comes back wrapped, so this is a property of the type rather than of the
prompt.

Judged by hand over the same six tasks: the command read the right object in 3 of 6 before
and 6 of 6 after. The third line helped in 4 of 6 before and 6 of 6 after, because the line
that told a reader to raise the `startupDuration` that caused the stall is gone; the prompt
now states the schema's own contract for that line, that it says what becomes true once the
object is unblocked rather than what to do.

Two things this cost. Naming the object in line 1 had to be asked for explicitly: taking the
command away lost it on one task, reproducibly, and the benchmark fell to 5 of 6 until the
prompt said so. And the model still writes raw condition names into prose, which the schema
forbids and nothing enforces; that predates this change and is recorded below.

The rung is kept rather than removed. It stays behind `--explain`, costs nothing unused, and
two of six answers say something the analyser cannot: which variable is undefined, and which
image failed to pull. What it must not do is decide, and after this change it cannot.

**Item 3, where a reader learns why.** No code change; `data/rungs.csv` is the measurement.
The study measured rung 1. `cluster why --verbose` carries
`VMProvisioned=False reason=WaitingForStartupTimeout`, and the runbook explains that the
duration has not elapsed and the machine is scheduled rather than broken. Both are one
command from the stall line. Rung 1 omits the reason because it is a raw condition
identifier and the jargon lint keeps those out of user-facing prose, which is the rule that
stops the one-liner growing back into the condition tree.

`REVISIT`: rung 1's `next:` names the runbook and never mentions `--verbose`, so a reader
following the tool's own signposting goes from three lines to a 154-line runbook without
being shown the 23-line answer in between. Naming both would cost one line. Left alone
because the brief for this work asked for the question to be stated rather than closed.

**A tension between the two, worth naming rather than fixing.** Rung 1 omits the condition
reason because the jargon lint keeps raw identifiers out of user-facing prose, and that lint
is the argument for the four-phase display existing at all. Rung 5, the model, writes those
identifiers freely: `WaitingForStartupTimeout` appears in its prose, the schema for that
output forbids condition type names, and nothing checks it. So the rule that shapes the
bottom of the ladder is unenforced at the top. Either the lint should run over model output
as it runs over `internal/msg`, or rung 5 should be described as a different kind of text
with a different rule. It is recorded as an open item in STATUS.md rather than decided here,
because deciding it means choosing what the model rung is for.
