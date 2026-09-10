# CLAUDE.md

Guardrails for every session in this repo. Read this and PLAN.md before touching anything.
The original build spec is `docs/build-spec.md`. PLAN.md carries Parts B and D of it,
including every acceptance checklist.

### What this project is

An opinionated assembly of Cluster API (CAPI), not a new API and not a wrapper product.
It ships three things that together make CAPI feel simple without replacing it:

1. **Assembly** — a curated `ClusterClass` (plus provider pins) that encodes one opinion:
   the user's only worker noun is a *pool*, but the class decides what backs it —
   `MachineDeployment` by default (GA, works on bare metal/vSphere/Docker) and
   `MachinePool` only on providers with a native scaling group (ASG/VMSS/MIG); users never
   see the difference. Nodes that join by token via a single static binary (k0s through
   k0smotron's bootstrap provider; kubeadm only as a fallback; Talos deferred to a
   cloud-only spike). Control-plane *placement* as a single user-facing field (`hosted`
   via k0smotron, or `self` via a machine-based control plane) — which may resolve to two
   classes underneath, see Phase 4. Users write exactly one kind: `Cluster`.
2. **Subtraction** — a `ValidatingAdmissionPolicy` (CEL) that restricts what users may
   write to ~6 fields of `Cluster.spec.topology`, rejects direct writes of managed kinds
   with a message pointing at the class, and has a documented break-glass.
3. **Stream** — a CLI (`cluster up | status | why | render`) that watches every CAPI object
   behind a cluster, folds their conditions into four user-facing phases
   (infrastructure → control plane → workers → addons), shows an ETA learned from prior
   runs, and on stall prints exactly one line naming the blocking object, reason, and
   elapsed time. It never hides the raw path.

The thesis: most of what makes CAPI hard is (a) mistakes reported late and in a different
object than the one you edited, and (b) a long opaque wait. Subtraction fixes (a) by moving
errors to admission time. Stream fixes (b). Neither adds a noun or a controller.

### Prior art (read before building; do not reinvent)

- `clusterctl describe cluster --show-conditions all` already renders the condition tree.
  Stream's delta over it is: four phases, ETA from history, stall ranking to one line, and
  a blocking `up`. Reuse its object-discovery approach where possible.
- `clusterctl alpha topology plan` is a dry-run for ClusterClass changes. `cluster render`
  should use or mirror it rather than reimplementing topology computation.
- Giant Swarm ships an opinionated open-source CAPI platform (per-provider Helm charts
  wrapping ClusterClass, `kubectl-gs` plugin). Syself does the same for Hetzner. Study
  their chart values as the closest existing "six fields" surfaces; our differences are
  vendor-neutrality, admission-time subtraction, and the stream.

### Non-goals (do not build these)

- No new user-facing CRDs. If you think you need one, stop and write it in DECISIONS.md.
- No new reconciler/controller in v0. All logic is either declarative (ClusterClass, VAP)
  or client-side (CLI).
- No provider rewrite. Use upstream providers as-is via `clusterctl`.
- No web UI. TTY renderer only, with a `--no-tty` plain mode for CI.
- No secrets handling beyond what `clusterctl` already does.

### Hard rules

1. **Pin everything.** All versions live in `versions.env` (Go, CAPI, CAPD, k0smotron,
   kind, kubectl, clusterctl, envtest k8s version, local model name + quant + hash +
   runtime version). Never float `latest`.
2. **Never invent API details.** Before writing code that touches CAPI types, conditions,
   or provider names: read the CRDs and Go types from the *pinned* release (module cache
   under `$GOMODCACHE/sigs.k8s.io/cluster-api@<pinned>/api/...`, or `kubectl explain`
   against a running `make dev-up` cluster). Write what you found into
   `docs/api-snapshot.md` before using it. Verify provider names with
   `clusterctl config repositories`. Condition type names in particular change between
   CAPI releases (v1beta1 → v1beta2 moved conditions to `metav1.Condition` and renamed
   several); do not trust memory.
3. **Pure core.** Packages `internal/fold`, `internal/eta`, `internal/why`, `internal/gen`
   import no Kubernetes client. They take plain Go structs / JSON fixtures and return
   values. All Kubernetes I/O lives in `internal/watch` and `cmd/`.
4. **Fixtures are recorded, not written.** Status snapshots in `testdata/fixtures/` come
   from real `make dev-up` runs via `make record-fixture`. Hand-edit a fixture only to
   create a minimal variant, and say so in its `_meta.note` field.
5. **Every denied field has a negative test. Every allowed field has a positive test.
   Break-glass has one test per policy.** No exceptions.
6. **A phase is done when its acceptance checklist passes and `make test` is green.**
   Do not start the next phase before that. Commit at least once per phase with the
   phase name in the message.
7. **Record decisions, don't wait for them.** Any choice not covered by this document
   goes into `DECISIONS.md` as: date, decision, alternatives considered, why, and a
   `REVISIT` tag if you'd want the human to confirm. For items under "Open decisions",
   take the bold default and proceed. Only the "must-ask" list in "Working without the
   human" stops work.
8. **The escape hatch is a feature.** `cluster render` (full CAPI YAML for a cluster) and
   the `raw:` line in every stall message are not optional polish; they are how the
   abstraction stays honest.

### How to work: increments, test first

The unit of work is one failing test made green, not one feature made complete.

1. **Red, green, commit.** Write the test (or golden, or fixture assertion) first; run it and
   see it fail for the right reason; write the least code that passes; run `make test`;
   commit. A commit that adds more than ~200 lines of non-test code without a test that
   needed them is a smell — split it.
2. **Vertical slices.** Each phase is delivered as thin end-to-end slices (one fixture →
   one fold rule → one render line), not as layers (all of `fold`, then all of `render`).
   The first slice of Phase 3 is: one recorded fixture, `fold` returns one phase, `--no-tty`
   prints one line, golden checked in. Everything after is widening.
3. **Tests define done.** The acceptance checklists in PLAN.md are tests or scripts, not
   prose. If an acceptance item can't be expressed as something that runs, rewrite the item.
4. **The tree is always green.** Never commit with `make test` failing. If a change breaks
   an unrelated test, fix or revert before moving on; do not skip, mark flaky, or widen a
   tolerance to make it pass. Skips need a linked issue and an expiry date in the skip
   reason.
5. **Change one thing per commit.** Refactors and behavior changes are separate commits.
   Fixture re-records are their own commit with the `_meta.note` explaining why.
6. **Time-box investigations.** If a test can't be made green after three distinct
   approaches, write a minimal repro into `docs/blocked/<slug>.md` (what, expected, actual,
   attempts) and move to the next independent slice. Don't burn hours in one hole.
7. **Prove the negative.** Every bug fix starts with a test that reproduces the bug and
   fails; the fix makes it pass. No bug fix without a regression test.
8. **Cheap loop first.** Pure-package tests (`make watch`) before envtest before Docker e2e.
   Only escalate to a slower layer when the faster one can't express the check.

### Working without the human

Assume the human reads this repo a few times a week, not a few times an hour. Design the
work so their absence costs nothing and their return is efficient.

- **Default, record, proceed.** When a decision is needed, take the most reversible
  reasonable option, record it in `DECISIONS.md` with `REVISIT` if it deserves review, and
  keep going. Reversible beats optimal.
- **Must-ask list (the only things that stop work):** spending beyond the API credit cap in
  `versions.env`; anything touching a real cloud account or non-kind cluster; deleting or
  rewriting recorded fixtures wholesale; changing the user-facing schema in
  `docs/schema/`; adding a dependency with a non-Apache/MIT/BSD license; choosing the
  project name or license. For these, write the question in `QUESTIONS.md` with your
  recommended answer, commit at a safe checkpoint, and continue on work that doesn't
  depend on it.
- **`STATUS.md` is the handoff.** Keep it current at every commit: what's done (with
  commit hashes), what's in progress, what's blocked and on what, and the exact command
  to resume. A human returning after a week should orient from this file alone in under
  five minutes.
- **`QUESTIONS.md` batches, never blocks.** One question per entry: context, options,
  recommendation, what you did in the meantime. When the human answers, the entry moves to
  `DECISIONS.md`.
- **Always resumable.** Work in small commits so partial progress is usable. Never leave
  the tree in a state where `make dev-up && make test` doesn't run. If you must stop
  mid-slice, stash or branch — never commit a broken state to main.
- **No idle waiting.** If everything left depends on a must-ask item, use the time on the
  deferred list in PLAN.md, on fixture coverage, or on the no-slop pass below — and say so
  in `STATUS.md`.
- **Report tersely.** Chat output is for what the human needs to decide or verify, not a
  narration of what you did. The record is in git, `STATUS.md`, and `DECISIONS.md`.

### No slop: the standard while building, and the final pass

Slop is anything that exists because it was easy to generate, not because a reader or a
test needed it. It is judged by the reader's cost, not the writer's effort.

Ongoing standard:

- **Code.** No comments that restate the code; comments explain *why* or cite the CAPI
  source/line they depend on. No speculative abstractions, interfaces with one
  implementation, options nobody sets, or "for future extensibility". No defensive
  error-wrapping that adds no information. No dead code, no `TODO: implement`. Names
  are the ones a CAPI maintainer would use, from `docs/api-snapshot.md`. `golangci-lint`
  runs `unused`, `unparam`, `revive`, `gocritic`, `errorlint`, `misspell`; the config is
  part of the repo and not loosened to pass.
- **Tests.** No tests that assert what the compiler already guarantees, no
  duplicated table rows that exercise the same branch, no mocking of pure packages. A
  test's name says the behavior, not the function (`TestUX_StallLineNamesObject`, not
  `TestFold3`).
- **Docs.** Task-first: every page starts with what the reader can do after reading it.
  No "Introduction/Overview" sections, no restating the previous page, no marketing tone,
  no emoji, no hedging ("may", "might", "it is recommended to consider"). Every code block
  is executed by the README test. If a sentence can be deleted without the reader losing a
  capability, delete it.
- **Commits and chat.** Commit subject ≤ 72 chars stating the change; body only if the
  *why* isn't obvious. No "Summary of changes" bullets in chat that duplicate the diff.

Final pass (its own commit, after every acceptance checklist is green):

1. Re-read every file in the diff since Phase 0 as a stranger. Delete anything the tests
   don't need and a reader wouldn't miss. Target: the final pass removes lines, never adds.
2. Collapse abstractions that ended up with one caller. Inline helpers used once.
3. Regenerate docs from verified behavior: user-guide examples are copied from passing
   README tests, error-code long forms from the `internal/msg` registry, the condition
   table from `docs/api-snapshot.md`. Docs that can't be traced to a test or a snapshot are
   rewritten or removed.
4. Run the message-quality lint and the jargon lint over docs as well as code.
5. Check the four handoff files (`STATUS.md`, `DECISIONS.md`, `QUESTIONS.md`,
   `docs/blocked/`) are current, then trim `STATUS.md` to the resume command and the
   open items.
6. Record in `DECISIONS.md` what was removed and why; the deletions are the review.

### Repo layout

```
.
├── CLAUDE.md
├── PLAN.md
├── DECISIONS.md                 # decisions taken, with REVISIT tags
├── STATUS.md                    # handoff: done / in progress / blocked / resume command
├── QUESTIONS.md                 # batched must-ask items with recommendations
├── versions.env
├── Makefile
├── assembly/
│   ├── clusterclass/            # ClusterClass YAML + kustomize overlays per provider
│   │   ├── base/
│   │   └── overlays/{docker,aws}/
│   ├── providers/               # clusterctl init configs per profile
│   └── images/                  # pinned node/k0s image references
├── policy/
│   ├── vap/                     # ValidatingAdmissionPolicy + Binding YAML
│   └── tests/                   # CEL table tests (Go)
├── cmd/cluster/                 # CLI entrypoint (cobra)
├── internal/
│   ├── fold/                    # conditions -> phases (pure)
│   ├── why/                     # stall-reason ranking (pure)
│   ├── eta/                     # history + estimates (pure)
│   ├── gen/                     # 6-field spec -> Cluster topology YAML (pure)
│   ├── watch/                   # dynamic client, informers, snapshot assembly
│   └── render/                  # TTY + plain renderers
├── testdata/
│   ├── fixtures/                # recorded snapshots: <scenario>/<t+NNs>.json
│   └── golden/                  # expected outputs
├── hack/                        # kind + clusterctl scripts
├── e2e/                         # CAPD end-to-end (build tag: e2e)
└── docs/
    ├── api-snapshot.md          # what you actually found in the pinned CRDs
    ├── eject.md                 # how to leave: render, un-apply policy, keep clusters
    └── user-guide.md
```

### Make targets (must exist by end of Phase 0)

```
make dev-up          kind mgmt cluster + clusterctl init (CAPD, feature gates) + apply assembly
make dev-down        delete it
make test            go test ./... (unit, pure core, policy CEL tests)   < 30s
make test-envtest    envtest-backed API tests                            < 3m
make e2e             CAPD end-to-end, build tag e2e                       < 25m
make record-fixture  NAME=<scenario> dumps all CAPI objects for CLUSTER=<name> into testdata
make lint            golangci-lint + yamllint + kubeconform on assembly/ and policy/
make render          renders the ClusterClass overlays to a single file for inspection
make profile P=...   resize container VM for dev|e2e|bench, restart runtime, re-check
make doctor-ai       memory math + local Qwen model recommendation + tokens/s check
make bench           cluster-bench, sequential, per backend (no-AI, local, anthropic)
```

### Tech decisions (defaults — see Open decisions)

- Go (current stable), cobra for CLI, controller-runtime for client + envtest,
  `github.com/google/cel-go` with `k8s.io/apiserver/pkg/cel/library` for policy tests.
- Management cluster for dev: kind with the Docker socket mounted; infra provider CAPD.
  **Use the `Dev*` kinds (`DevCluster`, `DevMachine`, `DevMachineTemplate`,
  `DevMachinePool`) with `spec.backend.docker`.** The `Docker*` kinds are deprecated and
  scheduled for removal; do not reference them.
- **Fast test substrate:** the same `Dev*` kinds have an `inMemory` backend that fakes
  VM/node/apiserver/etcd provisioning with configurable `startupDuration` and
  `startupJitter`. Use it for stream/ETA/stall tests: it needs no Docker, runs in
  seconds, and lets you *induce* a stall deterministically by setting one component's
  startup duration past `stallAfter`. Docker backend stays for e2e only.
- `clusterctl init` with `CLUSTER_TOPOLOGY=true` (MachinePool is enabled by default
  since CAPI v1.7; still beta, and only used where the provider has a scaling group).
- Control plane `placement: self` → kubeadm control plane (v0) → k0s on machines via
  k0smotron `K0sControlPlane` (Phase 5).
  `placement: hosted` → k0smotron.
- Kubernetes ≥ 1.30 on the management cluster (VAP is GA there).
- Plain `testing` + `testify/require`. Table-driven tests. Golden files with `-update` flag.

### Fixture format

One JSON envelope per snapshot:

```json
{
  "_meta": {"scenario": "happy-path-docker", "t_plus_s": 240, "capi": "v1.x.y", "note": ""},
  "objects": [ /* full JSON of Cluster, control plane, MachinePools, Machines,
                  infra objects, bootstrap configs, ClusterResourceSetBindings */ ]
}
```

Tests load an envelope, run `fold.Fold(objects)` / `why.Rank(objects)`, and compare to the
golden `expected.json` next to it (`phases[]`, `stall`, `raw`).

