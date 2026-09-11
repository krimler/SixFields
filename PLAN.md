# PLAN.md

Each phase lists: goal, work, tests, acceptance. Time boxes are guidance, not deadlines.

### Phase 0 — Environment and truth (½ day)

**Goal:** a repeatable dev loop and a written snapshot of the real API surface.

Work:
- `versions.env`, `Makefile` targets above, `hack/kind-config.yaml` (Docker socket mount),
  `hack/clusterctl-init.sh` reading `versions.env`.
- `make dev-up` brings up kind + CAPI core + CAPD + feature gates. Verify with
  `kubectl get pods -A` and `clusterctl describe cluster` on a throwaway cluster from the
  upstream CAPD quickstart (not our class yet).
- Write `docs/api-snapshot.md`: for each kind we will read (Cluster, KubeadmControlPlane,
  MachinePool, MachineDeployment, MachineSet, Machine, DevCluster, DevMachinePool,
  DevMachine, KubeadmConfig, ClusterResourceSetBinding), list the condition types and
  phase enums actually present in the pinned release, with the file/line they came from.
  Pin CAPI ≥ v1.11 (v1beta2 API); note that v1beta1 API and contract support is being
  removed (targeted at v1.16), so conditions live in `status.conditions` as
  `metav1.Condition` and the old ones under `status.deprecated.v1beta1`.
- **Contract check per provider.** For every provider we will install (CAPD, k0smotron,
  k0smotron bootstrap + both control-plane kinds, later Kamaji/AWS), record in `docs/api-snapshot.md`
  which CAPI contract version it implements (`v1beta1` or `v1beta2`) from its
  `metadata.yaml` / release notes. If k0smotron does not implement the v1beta2 contract
  or the `Dev*` machine kinds for the pinned CAPI, flag it as a blocking risk for Phases
  4–5 before doing any of that work, and propose either pinning a CAPI line it supports
  or keeping kubeadm as the `self` control plane.
- Record fixture `quickstart-docker` at t+0, t+60s, t+180s, Ready.

Tests: none beyond `make lint`.

Acceptance:
- [ ] `make dev-up && make dev-down` works twice in a row from clean.
- [ ] `docs/api-snapshot.md` exists and cites the pinned source.
- [ ] `testdata/fixtures/quickstart-docker/` has ≥ 4 envelopes.

### Phase 1 — Assembly v0: the class (1 day)

**Goal:** one `ClusterClass` on CAPD where a user-facing `Cluster` needs only these fields:

```yaml
apiVersion: cluster.x-k8s.io/<pinned>
kind: Cluster
metadata: {name: dev-1, namespace: default}
spec:
  topology:
    class: std
    version: v1.31.x          # pinned
    workers:
      machineDeployments:      # "pool" in the generator; MD here because CAPD has no scaling group
      - name: default
        class: default
        replicas: 2
```

Work:
- `assembly/clusterclass/base/`: `ClusterClass std` with `controlPlane` (kubeadm, replicas
  defaulted to 1 via variable default; 3 for `size: ha`), `workers.machineDeployments[default]`
  backed by `DevMachineTemplate` (docker backend), variables: `size` (`dev|ha`, default `dev`),
  `placement` (`self|hosted`, default `self`; `hosted` is wired in Phase 4),
  `nodeImage` (defaulted from `versions.env` via kustomize). A second overlay for a
  cloud provider may switch the pool to `workers.machinePools`; the generator's
  `pools:` input maps to whichever the overlay uses.
- Patches: set control-plane replicas from `size`; set machine templates from `nodeImage`.
- `make render` prints the fully rendered class; check it into `testdata/golden/class-docker.yaml`.

Tests (`assembly/` has a small Go test package):
- Golden: rendered overlay == golden file (`-update` to regenerate, diff reviewed).
- Schema: every variable's `openAPIV3Schema` rejects an invalid value and accepts the
  default (validate with the same CEL/openapi validator envtest uses, or via envtest
  `dry-run` create of the Cluster).
- Envtest: apply CRDs from pinned CAPI + the class; create the 6-field Cluster; assert the
  topology controller is *not* needed to pass server-side validation (i.e. the object is
  admitted). Reconciliation itself is e2e.

Acceptance:
- [ ] The 6-field Cluster above reaches `Ready` on `make dev-up` within 10 minutes.
- [ ] `kubectl get cluster,kcp,md,machines` shows only objects labeled
      `topology.cluster.x-k8s.io/owned` besides the Cluster itself (verify the label
      name against the pinned release first; it is from memory).
- [ ] Fixtures recorded: `std-docker-happy/` at ≥ 5 points including Ready.

### Phase 2 — Subtraction: the policy (1 day)

**Goal:** users can only write the allowed fields; everything else fails at admission with
a message that names the class and the break-glass.

Allowed on `Cluster` (create/update by non-exempt identities):
`metadata.{name,namespace,labels,annotations}`, `spec.topology.class`,
`spec.topology.version`, `spec.topology.workers.machinePools[].{name,class,replicas}`,
`spec.topology.variables[]` **only** for names in an allow-list (`size`, `placement`).
Everything else under `spec` (e.g. `clusterNetwork`, `controlPlaneRef`,
`infrastructureRef`, `spec.topology.controlPlane.replicas`, unknown variables) → deny.

Managed kinds (`KubeadmControlPlane`, `K0sControlPlane`, `MachineDeployment`,
`MachineSet`, `Machine`, `MachinePool`, `*Template`, `DockerCluster`, `KubeadmConfig*`):
deny CREATE/UPDATE by non-exempt identities. Determine empirically in Phase 2 which of
these two exemption mechanisms holds on a real run (record who creates what from the
audit of a `std-docker-happy` run), then implement the one that does, and document it:
  (a) identity: `request.userInfo.username` in an allow-list of controller service
      accounts (CAPI core, CAPD, k0smotron, kubeadm providers), and/or
  (b) ownership label: objects carrying `topology.cluster.x-k8s.io/owned` are exempt.
Default expectation: (a) is required, (b) is belt-and-braces. Do not assume; verify.

Break-glass: label `sixfields.io/break-glass: "true"` on the object **and** the
requesting identity in group `sixfields:break-glass`. Both required. Every use is
logged by the binding's audit annotation.

Work:
- `policy/vap/cluster-fields.yaml` (VAP + Binding, `validationActions: [Deny, Audit]`).
- `policy/vap/managed-kinds.yaml`.
- `messageExpression` returns a one-line message of the form:
  `"<field> is managed by ClusterClass 'std'. Set it via the class or use break-glass (docs/eject.md)."`
- `docs/eject.md`: how to remove the policy without touching clusters.

Tests (`policy/tests`, pure CEL via cel-go with the k8s library, < 5s):
- Table: one positive case per allowed field; one negative per denied field; unknown
  variable name denied; each managed kind denied for a human, allowed for each exempt
  identity; break-glass with label only → deny; group only → deny; both → allow.
- Message assertions: every deny message contains the field path and the word `break-glass`.
- Envtest smoke (in `make test-envtest`): the same policy applied to a real API server
  (≥ 1.30) admits the Phase-1 Cluster and rejects one hand-written `KubeadmControlPlane`.

Acceptance:
- [ ] `make test` runs the CEL table in < 5s with 100% of denied fields covered
      (a generated list in `policy/tests/coverage_test.go` fails if a denied path has no case).
- [ ] On `make dev-up`, a normal kubeconfig user cannot create a `KubeadmControlPlane`;
      the error text names the class and the break-glass doc.
- [ ] The Phase-1 happy path still reaches Ready with the policy installed (controllers
      are correctly exempt).

### Phase 3 — Stream: the CLI core (2 days)

**Goal:** `cluster up NAME -f cluster.yaml` applies, then blocks and streams four phases
with an ETA, and on stall prints one blocking line. `cluster status NAME` is the same view
without applying. `cluster why NAME` prints only the stall line and the raw command.

Folding model (`internal/fold`), as a pure function over the fixture envelope:
- Phase `infrastructure`: from the Cluster's infrastructure-ready condition and the infra
  cluster object's ready condition.
- Phase `control-plane`: from control-plane-initialized / control-plane-ready on the
  Cluster, plus the control plane object's available/ready conditions and its
  ready-replicas vs desired. Reports `k/n nodes`.
- Phase `workers`: per MachinePool ready-replicas vs desired; aggregate.
- Phase `addons`: ClusterResourceSetBinding applied state (Helm add-on provider later).
- Each phase → `{state: pending|running|done|stalled, detail: string, elapsed: duration}`.
- `stalled` = phase `running` with no condition transition on any contributing object for
  longer than `stallAfter` (default 3m, flag).
- Use only condition types listed in `docs/api-snapshot.md`. Support both v1beta1-style
  and v1beta2-style conditions behind one interface; test both with fixtures.

Why (`internal/why`), pure:
- Candidate set: every contributing object's `False` conditions.
- Rank: (1) most specific object first (Machine > MachineSet/MachinePool > control plane >
  Cluster), (2) most recent `lastTransitionTime`, (3) severity if present.
- Output: `{object: kind/name, reason, message, since: duration, raw: "kubectl get <kind> <name> -o yaml"}`.
- Message must be one line ≤ 120 chars; longer upstream messages are truncated at a word
  boundary with `…`, full text available under `--verbose`.

ETA (`internal/eta`), pure:
- History store interface with a file implementation (`~/.cluster/history.json`) keyed
  by `(provider, class, placement, phase)`; store durations of completed phases.
- Estimate: p50 and p95 over last N (default 20). With < 3 samples print `no history yet`.
- Never show an ETA for a stalled phase; show `typical p50 · p95` under the stall line.

Watch (`internal/watch`):
- Dynamic client; list+watch the Cluster and every object reachable by owner references
  and refs (`controlPlaneRef`, `infrastructureRef`, MachinePool infra refs). Build the
  same envelope the fixtures use, so `fold`/`why` are identical in tests and in production.
- Snapshot cadence: on every event, debounced to ≤ 2 renders/s.

Render (`internal/render`):
- TTY: four rows, progress blocks, right-aligned detail; stall block below; raw line last.
- `--no-tty`: one line per state change, machine-greppable, used in e2e.
- `--json`: emit the envelope + folded phases; used by tests and by anything else that
  wants to build on this.

Tests:
- `fold`: for every fixture directory, every envelope has a golden `expected.json`.
  Include at least these scenarios (record them in Phase 3): `std-docker-happy`,
  `stall-bad-version` (unreachable Kubernetes version → control plane never ready),
  `stall-cp-killed` (docker container of a control-plane machine removed), `stall-bad-variable`
  (topology reconcile reports an error), `scale-up` (replicas 2→4 mid-run).
- **Deterministic stalls via `inMemory`:** add an `std-inmemory` overlay and record
  `inmem-happy` plus `inmem-stall-etcd` (etcd `startupDuration` set to 10× `stallAfter`)
  and `inmem-stall-node` (node startup delayed). These run in `make test-envtest`
  without Docker and are the primary fixtures for `why` ranking and ETA math; the
  Docker-backed ones stay as e2e confirmation.
- `why`: for each stall fixture, the golden ranks the intended object first. Add a
  "two stalls at once" hand-made variant to lock ranking order.
- `eta`: table tests for p50/p95, insufficient history, and key partitioning.
- `render --no-tty`: golden text per fixture sequence.
- `watch`: envtest — create objects with owner refs, assert the envelope contains exactly
  the reachable set and nothing else.

Acceptance:
- [ ] `cluster up dev-1 -f cluster.yaml` on `make dev-up` blocks until Ready and exits 0;
      `--no-tty` output matches the golden sequence modulo timestamps.
- [ ] Induce `stall-bad-version`; within `stallAfter + 10s` the stall line names the
      control plane object and the version reason; `cluster why` prints the same line.
- [ ] `make test` still < 30s.

### Phase 4 — Placement: hosted control plane (1 day)

**Goal:** `placement: hosted` yields a k0smotron control plane; `self` unchanged.

Work:
- Extend `hack/clusterctl-init.sh` to install k0smotron providers (verify names via
  `clusterctl config repositories`).
- Upstream `ClusterClass` has exactly one `controlPlane` definition per class (a vendor
  fork exists that adds multiple control-plane classes, which tells you upstream lacks
  it). So default to **two classes** (`std` and `std-hosted`) sharing overlays, and have
  the generator map `placement` to the class name. Only try a single-class `enabledIf`
  patch if you can show the control-plane `ref` kind can be switched by a patch on the
  pinned release; record the finding either way.
- `fold`: control-plane phase handles the k0smotron control-plane kind's conditions
  (add them to `docs/api-snapshot.md` first).

Tests:
- Golden render for both placements. Fixtures `hosted-docker-happy` + one hosted stall.
- Policy: `placement` accepted values only `self|hosted`; anything else denied.

Acceptance:
- [ ] Same 6-field Cluster + `placement: hosted` reaches Ready on CAPD.
- [ ] `cluster status` shows `control plane · hosted` and reports pod readiness rather than
      node counts for that phase.

### Phase 5 — Nodes that join: k0s on machines (1–2 days)

**Goal:** `placement: self` uses k0smotron's machine-based control plane (`K0sControlPlane`)
and worker bootstrap (`K0sWorkerConfigTemplate`), so both placements share one provider
family, one bootstrap mechanism (single static k0s binary, token join), and one set of
condition types. kubeadm remains only as a documented fallback. This phase runs entirely on
CAPD on a Mac — k0smotron documents `K0sControlPlane` against a Docker machine template.

Why not Talos here: Sidero has stopped active development of the Talos CAPI providers
(community support only), their API is still `v1alpha3` on the v1beta1 contract that CAPI is
dropping, and CAPD has no supported path to deliver a Talos machine config into a container.
Talos stays a valid *production* target via a cloud/bare-metal infra provider; see Deferred.

Work:
- Switch the `self` class's control plane ref to `K0sControlPlaneTemplate` and worker
  bootstrap to `K0sWorkerConfigTemplate`; verify k0smotron's compatibility with the pinned
  CAPI contract and the `Dev*` machine kinds in Phase 0's contract check (its docs still
  show `DockerMachineTemplate`).
- `assembly/images/`: pin the k0s version used by the class (one line in `versions.env`).
- `fold`/`why`: add `K0sControlPlane` / `K0sWorkerConfig` condition types after
  snapshotting; the hosted (`K0smotronControlPlane`) types from Phase 4 likely overlap.

Tests: golden render; fixtures `k0s-self-docker-happy` + `k0s-self-stall-bad-version`; a
`bootstrap` variable is not exposed to users (operator opinion, set in the overlay).

Acceptance:
- [ ] k0s-on-machines cluster reaches Ready on CAPD; Machines show the k0s bootstrap kind.
- [ ] Stall on a bad k0s version yields a one-line reason naming the Machine.
- [ ] `placement: self` and `placement: hosted` differ in exactly one class reference in
      `make render` output.

### Phase 6 — Eject, e2e, docs (1 day)

Work:
- `cluster render NAME` prints the complete set of CAPI objects for the cluster
  (the escape hatch). Test: `kubectl apply --dry-run=server` of the output produces an
  empty diff against live objects.
- `e2e/`: happy path × {self, hosted} × {kubeadm, k0s}; one stall; break-glass path.
- `docs/user-guide.md`: two nouns, six fields, what you'll see during the wait, what a
  stall looks like, how to eject.

Acceptance (global — these are the tests that decide whether the thesis held):
- [ ] **Six-field test**: a new user creates a working cluster writing only allowed fields.
- [ ] **Early-error test**: 100% of denied fields fail at admission, never at reconcile.
- [ ] **Twelve-minute test**: during provisioning the user always sees phase, detail, and
      either an ETA or a stall reason; never a bare `Provisioning`.
- [ ] **Eject test**: `cluster render` output re-applies with zero diff.
- [ ] **No-slop pass done**: a dedicated commit after all checklists were green that
      removed more lines than it added, with `DECISIONS.md` listing what went and why.
- [ ] **Untouched test**: `git diff` against upstream CAPI/CAPD/k0smotron manifests
      is empty — we changed nothing upstream.

### Deliberately deferred

Fleet-level sequencing, Helm add-on provider, AWS overlay, ConfigMap-backed ETA history
shared across users, kubectl plugin that hides managed kinds from `get`, Karpenter-style
workload-driven pools. Each gets a DECISIONS.md entry when picked up.

**Talos spike (cloud-only, not on Mac).** An immutable-OS `self` placement using the
community-maintained Talos CAPI providers against a real infra provider (Hetzner, AWS,
Proxmox). Preconditions: the providers implement the pinned CAPI contract, and a stall
fixture can be recorded from that environment. Do not start it until Phase 6 is done.


---

## Part D — Dev loop on macOS, ergonomics as test criteria, AI assists

### D1. macOS is the primary dev machine

- **Tools via one command.** `Brewfile` + `mise.toml` (or `.tool-versions`) pin go, kind,
  kubectl, clusterctl, kubeconform, yamllint, golangci-lint, gotestsum, and
  `setup-envtest`. `make bootstrap` installs everything; `make doctor` verifies it.
- **Container runtime.** Support Docker Desktop, OrbStack, and Colima. `make doctor`
  detects which one via `docker context` / `DOCKER_HOST`, and resolves the socket path
  for the kind `extraMounts` entry (Docker Desktop: `/var/run/docker.sock`; OrbStack and
  Colima use per-user sockets — detect, don't hardcode). VM sizing is profile-based
  (see "Memory profiles" below); `make doctor` warns when the active profile's VM size
  doesn't match the target and prints the runtime's command to change it.
- **Apple Silicon.** `make doctor` checks `uname -m` and that every pinned image
  (kindest/node, k0s, CAPD controller) has an arm64 manifest. Fail early with the
  image name, not later with a cryptic `exec format error`.
- **The CAPD-on-Mac kubeconfig quirk.** Workload-cluster kubeconfigs from CAPD point at
  the load-balancer container's internal address. On Docker Desktop that's unreachable
  from the host; the server must be rewritten to `127.0.0.1` and the LB's published
  port. `cluster kubeconfig NAME` does this automatically and tests cover it
  (`TestKubeconfig_RewritesForDockerDesktop`). This is exactly the kind of paper cut the
  project exists to remove; it should never be a README footnote.
- **Memory profiles (reference machine: 24 GB unified memory).** Three profiles,
  selected by `PROFILE=dev|e2e|bench` on make targets; `make doctor` shows the budget:

  | Profile | Container VM | Model loaded | What runs | Budget (of 24 GB) |
  |---|---|---|---|---|
  | `dev` (default) | 5 GB | ~14B dense, Q4 (~9 GB) | kind + inMemory workload clusters, `make test`, `make test-envtest`, `--explain` | macOS ~5 · VM 5 · model 9 · headroom ~5 |
  | `e2e` | 10 GB | none | kind + CAPD docker workload cluster (1 CP + 2 workers) | macOS ~5 · VM 10 · headroom ~9 |
  | `bench` | 4 GB | MoE ~35B-A3B, Q4 (mmap; ~14–18 GB resident) | kind + inMemory + `cluster-bench`, build-time generation | macOS ~5 · VM 4 · model ≤ 15 · no other work |

  Rules that follow: the model is never loaded in `e2e`; `bench` runs alone (doctor
  refuses to start it if the VM is above 4 GB or another model process is resident);
  `dev` uses the inMemory backend exclusively so the VM stays small. Switching profiles
  means resizing the container VM, which restarts it — `make profile P=e2e` does the
  resize, waits for Docker, and re-checks `make dev-up` idempotently. Daily work stays
  in `dev`; `e2e` is a deliberate, occasional switch (or CI).
- **Prefer Docker-free loops.** The `Dev*` `inMemory` backend runs entirely inside the
  kind management cluster's API server model — no workload containers. Most integration
  tests (`make test-envtest`) should use it. Docker-backed workload clusters are for
  `make e2e` only, and `make e2e` reuses one management cluster across runs (idempotent
  `dev-up`, per-run namespaces, `trap` cleanup) so a rerun costs minutes, not a rebuild.
- **envtest on darwin/arm64.** `setup-envtest use -p path --os darwin --arch arm64`;
  cache under `~/.cache/envtest`; `make doctor` checks the binary matches the pinned
  Kubernetes version.
- **What does not run on a Mac (and the plan doesn't need it to).** Talos-in-CAPI
  locally (see Phase 5); any VM-backed infra provider that needs nested virtualization
  (KubeVirt/CAPK) — Apple Silicon Docker runtimes don't expose it; real cloud overlays
  need cloud credentials but are *driven* from the Mac. Everything else in this document —
  kind, CAPD `Dev*` docker and inMemory backends, k0smotron hosted and machine-based
  control planes, VAP on kind's API server, envtest, cel-go tests, pty-based render tests,
  replay, cluster-bench, cassettes, skills — runs natively on Apple Silicon.
- **Time budgets are tests.** `make test` fails if it exceeds 30 s wall-clock on the
  reference machine profile; `make test-envtest` 3 min. Budgets live in `Makefile` and
  are asserted by `hack/timebox.sh`, so slowness is a red build, not a vibe.

### D2. Ergonomics as unit/integration test criteria

The rule: every user-facing string is defined in one package, `internal/msg`, as a typed
template. That single design choice is what makes UX testable — the lint walks a registry
instead of grepping code.

Deterministic gates (run in `make test`):

1. **Time to first feedback.** With a fake clock and replayed fixture, `cluster up` emits
   its first line within 1 s of start and never goes more than 5 s without output while
   running. (`TestUX_FirstFeedbackUnder1s`, `TestUX_NoSilentGaps`)
2. **Stall line contract.** ≤ 120 chars; contains `kind/name`, a reason, an elapsed time;
   is followed by exactly one `raw:` line with a copy-pasteable kubectl command.
   (`TestUX_StallLineContract`, runs over every stall fixture)
3. **Admission message contract.** Every deny message contains the offending field path,
   the class name, and the word `break-glass`, and is ≤ 2 sentences.
   (`TestUX_AdmissionMessageContract`, generated over every denied path)
4. **Next-action rule.** Every error message ends with something the user can do
   (a command, a doc path, or a field to change). Enforced by a required `NextAction`
   field on the error type — the compiler is the lint.
5. **Jargon lint.** Raw condition type names (`InfrastructureReady`, `MachinesSpecUpToDate`,
   …) may appear only on `raw:` lines and under `--verbose`. Deny-list generated from
   `docs/api-snapshot.md`, so it updates itself when CAPI does.
6. **Golden UX snapshots.** For every fixture sequence: `--no-tty` transcript golden;
   TTY rendering golden at 80 and 120 columns via a pseudo-terminal; `NO_COLOR=1`
   golden. Plus the color-independence test: strip ANSI from the color rendering and
   assert it carries the same tokens as the `NO_COLOR` rendering — no meaning by color
   alone.
7. **Render budget.** Replay at 50× with a fake clock and count renders: ≤ 2/s, and no
   two consecutive frames identical (no flicker, no busy loop).
8. **Stall detection latency.** For each induced-stall fixture, the stall line appears
   within `stallAfter + 10 s` of the last transition.
9. **`--json` schema stability.** Output validated against a versioned JSON schema in
   `docs/schema/status.v1.json`; changing it requires bumping the version and a golden.
10. **Help and README are executable.** Every command's `--help` includes an `Examples:`
    block (test parses cobra tree). README code blocks are extracted and run in
    `make test-envtest` against the inMemory overlay; the documented first-run path must
    be ≤ 4 commands (`TestUX_FirstRunCommandCount`).
11. **Exit-code contract.** `0` ready, `2` stalled (still running when `--timeout` hit),
    `3` admission rejected, `4` environment (doctor) failure. Golden per scenario.

Integration gates (run in `make test-envtest` on inMemory):

- A new user script: apply the 6-field Cluster, run `cluster up`, kill it, run
  `cluster status`, run `cluster why`; assert the transcripts are consistent (same phase
  states in all three views at the same instant).
- Stall then recovery: extend etcd startup, see the stall line, shorten it (edit the
  DevMachine), assert the stall line clears and phases resume without restart.

### D3. Further dev ergonomics

- `cluster fixture record|replay|diff` (hidden dev subcommands). **Replay is the big one:**
  `cluster status --replay testdata/fixtures/inmem-stall-etcd --speed 20` lets you iterate
  on rendering against a real timeline with no cluster at all. UI work should happen here.
- `make watch` (`gotestsum --watch`) for the pure packages; sub-second loop.
- `make replay F=<fixture>` shortcut; `make class-plan` runs `clusterctl alpha topology plan`
  against the rendered class so ClusterClass edits show their blast radius before apply.
- Pre-commit: yamllint, kubeconform with the pinned CAPI CRD schemas, CEL compile check
  for every policy expression, `internal/msg` lint, `go vet`.
- `cluster why --explain-ranking` prints why each candidate lost, for debugging the
  ranker without reading code.
- Dev runs feed the ETA history automatically, so after a week of local work the ETA is
  already calibrated for the Mac profile.
- Devcontainer as a fallback for Linux CI parity; not the primary path.

### D4. AI-assisted ergonomics (opt-in, never on the critical path)

Patterns borrowed from projects that already do this (see DECISIONS.md for sources):

- **Ship skills, not a chatbot (Anyscale Agent Skills).** The primary AI deliverable is
  a `skills/` folder installed by the CLI for Claude Code / Cursor: `/cluster-inspect`
  (read-only: runs `cluster status --json`, `cluster why --json`, `cluster render`,
  grounds against `docs/api-snapshot.md` and the pinned CAPI source, returns a structured
  report) and `/cluster-fix` (proposes a change, shows the diff, applies only on
  confirmation). Inspect and fix are separate skills on purpose.
- **Deterministic analyzer first, model second (k8sgpt).** `why` is the analyzer; the
  model only ever explains the analyzer's output. A `noop` backend echoes the input so
  every AI code path is testable without a model.
- **Reversible anonymization (k8sgpt).** `--anonymize` replaces object names/namespaces
  with keys before anything leaves the machine and re-substitutes them in the answer.
  Test: round-trip identity on every fixture.
- **Runbooks beat models (HolmesGPT).** `runbooks/<stall-class>.md` — one per stall
  class the ranker can emit (control plane not initializing, machine stuck provisioning,
  topology reconcile failed, infra not ready, node not joining). Humans read them, `why`
  links them, agents use them as tools. This is the highest-leverage artifact in D4 and
  should be written before any model integration.
- **Read-only by design, RBAC-respecting (HolmesGPT).** Inspect paths use the caller's
  kubeconfig and never write. Fix paths are a separate binary entry point.
- **Big outputs get summarized by a cheap model before the main one (HolmesGPT).** For
  `--verbose` envelopes over a size threshold, a summarizer step; otherwise raw.
- **Task benchmark verified against a live cluster (kubectl-ai's k8s-bench, KubeBench).**
  `cluster-bench` runs an agent binary against the inMemory management cluster on a task
  list (create, diagnose stall X, scale, eject), scores pass/fail by checking the cluster
  state — not the transcript — and reports per model. Time to root cause and number of
  interactions are recorded per task. **Runner:** adapt k8s-bench's Go runner with CAPI
  tasks; do not depend on agent-breakage (arXiv 2605.23058) — its injector targets
  workload faults in a single k3d cluster and needs Node + Postgres/pgvector — but
  adopt its methodology: agent-off control arm, framework-error vs reasoning-error
  labels on every failed run, pre-registered thresholds and sample size, and a README
  that states reproduction time and cost. Revisit contributing a CAPI-lifecycle injector
  to agent-breakage only if `/cluster-fix` becomes autonomous.
- **Every AI number needs a no-AI baseline.** Each benchmark task runs with skills off
  (plain CLI) and on; the report shows both. An AI feature that doesn't beat the baseline
  on time-to-answer or interactions is not shipped.
- **Test split (HolmesGPT).** `go test ./...` is `not-llm`; `make test-llm` runs
  cassette-replayed and live evals separately, never in the default gate.

Principles: AI features are additive; `make test` never calls a model; CI uses recorded
responses (cassettes); anything sent to a model is redacted (kubeconfig contents, tokens,
secrets, optionally IPs); every AI output is grounded in the fixture envelope and that
grounding is checked deterministically.

1. **`cluster why --explain`.** Sends the ranked candidates, their raw messages, and the
   phase table to a model; returns a 3-line plain-language explanation plus one suggested
   next command. Tests: cassette-based unit tests for shape; a **grounding test** that
   every object name, reason, and number in the explanation appears in the envelope
   (deterministic, fails the build); a nightly LLM-as-judge rubric for helpfulness that
   is advisory only. Cache by envelope hash.
2. **AI usability probe (nightly).** Run a coding agent as a *first-time user* in a
   sandbox with only `docs/user-guide.md`, the CLI binary, and an inMemory management
   cluster. Tasks: create a cluster; find out why the stalled one is stuck; eject one.
   Record commands issued, dead ends, and time; write `docs/ux-probe/<date>.md`.
   Thresholds: ≤ 3 commands to first correct status, ≤ 2 to the stall reason. A
   regression against baseline opens an issue automatically. This is the closest thing
   to a usability study that runs every night.
3. **Message-quality grader.** For every string in `internal/msg`, a rubric grader
   (names the object? says what to do? no internal jargon? under budget?) posts a CI
   comment with suggestions. The deterministic lint in D2 remains the gate; the grader
   only advises.
4. **Fixture mutation suggestions.** From the recorded stall fixtures, the agent proposes
   minimal hand-made variants that would distinguish ranking rules (two stalls at once,
   stale vs fresh transition times). Proposals land as PRs with `_meta.note` set;
   recorded fixtures stay the source of truth.
5. **`cluster new --from "3 ha nodes, hosted control plane, gpu pool of 2"`.** Natural
   language → generator input. The output must validate against the generator's JSON
   schema and pass the admission policy locally before it's shown; it is never applied
   without printing the YAML and asking. Tests: cassette + schema validation; a golden
   set of 20 phrasings with expected structured output.
6. **Drift assistant on CAPI bumps.** When `versions.env` changes, a job regenerates
   `docs/api-snapshot.md`, diffs condition types, and asks the agent to propose updates
   to the fold table and jargon deny-list as a PR. Golden fixture tests gate the merge.
7. **Agent-native repo.** CLAUDE.md (Part A) plus hooks: run `make test` after edits to
   `internal/`, run the policy table after edits to `policy/`, and a `/record` helper
   that wraps `cluster fixture record`. Keep the agent inside the same guardrails as a
   human contributor — nothing in D4 bypasses D2.

Cost and safety knobs: `CLUSTER_AI=off|explain|all` env var, per-day token cap,
`--redact-ips`, and a `docs/ai.md` that lists exactly what leaves the machine.

#### D4.1 Default model backend: the heaviest local Qwen that fits

Every AI path in this project talks to one `Explainer` interface with four backends:
`noop` (echo), `cassette` (recorded), `local` (OpenAI-compatible HTTP endpoint on the Mac),
and `anthropic` (API). **`local` is the default**, and the model is the heaviest
open-weight Qwen that fits the machine's unified memory with headroom for the dev loop.

- **Runtime.** Serve through an OpenAI-compatible endpoint. Prefer MLX (`mlx_lm.server`
  or LM Studio's MLX engine) for throughput on Apple Silicon; Ollama is acceptable but
  check the pinned Qwen generation actually loads there (some Qwen releases ship as
  GGUF + separate vision projector files that Ollama couldn't load at the time of writing;
  llama.cpp-compatible backends could). `CLUSTER_AI_URL` defaults to the runtime's local
  port; `CLUSTER_AI_MODEL` is pinned in `versions.env` with quantization and the file
  hash, plus runtime version. Temperature 0 and a fixed seed where the runtime supports it.
- **Choosing the model.** `make doctor-ai` reads unified memory, subtracts the active
  profile's VM size and a 5 GB macOS reserve, lists the Qwen models the runtime can
  serve, and recommends the heaviest that fits — dense over MoE at equal fit for
  explanation quality, MoE (`*-A3B`-style) when the dense option would swap. It then
  runs a 200-token prompt and reports tokens/s; under 8 tok/s it recommends one size
  down, because a slow `--explain` is worse than the runbook alone. Do **not** hardcode a
  model name in code; the generation moves every few months. Record the pick in
  `DECISIONS.md` with the memory math.

  **For the 24 GB reference machine** (verify names at setup; generations move):
  - `dev` / `--explain`: the current ~14B dense Qwen at Q4_K_M (~9 GB). This is the
    pinned default. A 9B at Q4 (~6.6 GB) is the fallback if tokens/s or headroom is poor.
  - `bench` / build-time generation: the current ~35B-A3B MoE at Q4. Only ~3B parameters
    are active per token, so it is fast, and with mmap the inactive experts can stay
    on disk — but it needs the `bench` profile (VM at 4 GB, nothing else running).
  - Not viable here: 27–32B dense (Q4 is ~17–20 GB and wants the whole machine), and
    anything larger. Don't try; the doctor will refuse.
  - Escape hatch: `CLUSTER_AI_URL` can point at a bigger model on another machine on
    the LAN (a second Mac, or a GPU box) with no code change; that's the way to get
    a heavier model for `bench` without giving up the dev loop on this one.

  Rough guide for other machines (verify at setup): 32–48 GB → the current 27–32B
  dense at Q4; 64–128 GB → 32B dense at Q8, or a mid-size MoE for speed;
  192–512 GB → the largest current Qwen MoE at Q4.
- **Thinking mode.** Off (or capped to a small budget) for `--explain`, where latency is
  the user's cost; on for build-time error-code long forms and for `cluster-bench`, where
  quality is the point and nobody is waiting at a prompt.
- **Structured output.** Ask for JSON via the endpoint's `response_format` (or grammar
  where available); validate against the schema in `docs/schema/explain.v1.json`. On
  invalid output, fall back to rung 4 (the runbook) silently and log it — never show the
  user a half-parsed answer.
- **Latency contract.** `--explain` prints the runbook immediately, then streams the
  model's three lines under it; hard timeout 60 s, after which the runbook stands alone.
  Test with a fake backend that stalls.
- **Memory contention.** Never start or warm the model automatically during `make e2e`
  or `make dev-up`; `make bench` requires `PROFILE=bench`, runs model jobs sequentially,
  and checks free memory first. `make doctor-ai` warns when the pinned model plus the
  active profile's VM plus the macOS reserve exceeds physical memory, and it treats
  swapping as a failure, not a slowdown: a dense model that pages is unusable.
- **Privacy.** With `local`, nothing leaves the machine, so `--anonymize` defaults off;
  it stays available and defaults on for `anthropic`. `docs/ai.md` says which is active.
- **Evals.** `cluster-bench` reports per backend: no-AI baseline, local Qwen, and (if a
  key is present) the Anthropic model, on the same tasks. Cassettes checked into the
  repo are recorded from the local model so CI is deterministic and free.
- **Build-time generation.** Error-code long forms and runbook first drafts are
  generated with the local model, then human-reviewed before they ship as static text.

Note on the coding agent itself: this document is harness-agnostic. Claude Code runs on
Anthropic models; if the agent working this repo runs on a local Qwen through a
local-model-capable harness instead, the rules that matter most are unchanged and
matter more — smaller slices, verify against pinned source, never invent API details.

### D5. End-user ergonomics (the person running `cluster up`)

Non-AI fundamentals first; AI rungs sit on top and are never the first thing a user sees.

1. **The ladder.** Every problem has the same rungs in order: one-line stall/deny message →
   `--verbose` (full condition text) → `raw:` kubectl line → `cluster docs <stall-class>`
   (runbook printed in-terminal) → `--explain`. Users with no API key get four rungs.
2. **`cluster plan`.** Runs the admission CEL locally and renders the topology diff before
   sending anything; wraps `clusterctl alpha topology plan` for class changes. Errors move
   from admission time to before-submit.
3. **Error codes.** Every stall class and denial has a stable code (`CAPI-CP-003`);
   `cluster explain <code>` prints the long form. Long forms are generated at build time,
   human-reviewed, and shipped as static text in the binary (AI at build time,
   deterministic at runtime, no key needed).
4. **Wait reassurance.** Stream prints "safe to Ctrl-C; `cluster status NAME` resumes";
   `--notify desktop|slack` on ready/stall; a GitHub Action posts the stream summary and
   stall line to the PR that changed the cluster file.
5. **`did you mean`.** Levenshtein against allowed fields, values, and commands in every
   deny and usage error.
6. **Undo.** `cluster history` and `cluster rollback` over ClusterClass rebases.
7. **Doctor for users.** kubeconfig reachability, RBAC for `Cluster` create, provider
   credentials, per-namespace quota ("you can create 2 more clusters here").
8. **Idempotent and scriptable.** Re-running `cluster up` is safe; `--wait --timeout`;
   documented exit codes; `--json --jq`.
9. **AI rungs, in value order.** (a) `--explain` grounded in the runbook for the emitted
   code, `--anonymize` default on; (b) `cluster skills install` so the user's own agent
   drives `status --json` / `why --json` via the inspect skill; (c) `cluster new --from
   "<sentence>"` → plan preview → confirm; (d) plain-language rewrite of raw provider
   errors, grounded to the original text and shown alongside it.
10. **Measurement.** Per release, with AI-off baseline: time to first cluster, time to
    root cause on the scenario library, interactions to root cause. Pre-registered
    thresholds, sample size sized to scoring noise, scenarios from recorded snapshots
    (see Odmark et al., arXiv 2605.23058, and Cloud-OpsBench's snapshot paradigm).
    An AI rung that doesn't beat the rung below it on these numbers is not a default.

Tests: each rung has a golden per stall class; `plan` denial messages must equal admission
messages byte-for-byte for the same input; `did you mean` has a table test over one-edit
typos of every allowed field; error-code long forms are lint-checked to contain what
happened, why, and a next action.

---

## Part D acceptance additions

- [ ] `make bootstrap && make doctor` on a fresh Apple Silicon Mac with OrbStack or
      Docker Desktop passes and prints resolved socket path, arch check, memory check.
- [ ] `cluster kubeconfig` output works from the host on Docker Desktop without edits.
- [ ] All D2 gates exist as tests and run inside the 30 s / 3 min budgets.
- [ ] `--explain` grounding test passes on every stall fixture with cassettes checked in,
      recorded from the pinned local Qwen model.
- [ ] `make doctor-ai` on the dev Mac recommends a model, the endpoint answers a
      one-token prompt, and the pick is recorded in `DECISIONS.md` with the memory math.
- [ ] One UX-probe report exists in `docs/ux-probe/` from a real run before v0 is called done.

## Open decisions (defaults in bold)

1. Language: **Go**. (Python would make the CLI faster to write and the policy tests slower
   to trust; Go keeps us in the ecosystem's toolchain.)
2. Hosted control plane provider: **k0smotron** (lightweight, works on kind/CAPD) vs Kamaji
   (needs a datastore; better if you already run Kamaji).
3. Bootstrap order: **kubeadm in v0 for the dev loop, k0s (k0smotron) in Phase 5**.
   Talos is a deferred, cloud-only spike (see Deliberately deferred).
4. ETA history location: **local file** in v0; ConfigMap in the management cluster later.
5. Managed-kind exemption: **service-account allow-list, verified empirically in Phase 2**,
   with the ownership label as a second check.
6. Expose `bootstrap` as a user variable: **no** (operator opinion, lives in the overlay).
7. Project name and license: unset.
8. Container runtime on Mac: **support all three, test on OrbStack and Docker Desktop**;
   Colima best-effort.
9. AI features in v0: **runbooks, the `/cluster-inspect` skill, `--explain` with
   `--anonymize`, and `cluster-bench` with a no-AI baseline**; `/cluster-fix`, the
   nightly probe, and the rest deferred.
10. Model backend for the AI features: **`local` — the heaviest Qwen that fits the Mac,
    served over an OpenAI-compatible endpoint, pinned in `versions.env`**; `anthropic`
    optional via env var; one `Explainer` interface with `noop` and `cassette`
    implementations for tests (see D4.1). **On the 24 GB reference machine: ~14B dense
    Q4 for `dev`/`--explain`, ~35B-A3B MoE Q4 for `bench` only.**
11. Reference machine: **24 GB unified memory, Apple Silicon**. Profiles in D1 are sized
    for it; a bigger machine loosens them, never the reverse.
