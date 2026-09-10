# Status

Resume with:

```sh
make doctor && make test && make test-envtest
```

Last updated 2026-09-10.

## Done

Phases 0 to 3 and most of Part D. Every claim below is backed by a test that runs
in `make test` (5s, budget 30s) or `make test-envtest` (16s, budget 180s), and
`make lint` is clean.

- **Phase 0 — environment and truth.** `versions.env` pins everything, including
  the module versions and the time budgets. `make api-snapshot` regenerates
  `docs/api-snapshot.{md,json}` by parsing the pinned CAPI, CAPD and k0smotron API
  packages out of the module cache: 659 constants with file and line, a contract
  table per provider, and the findings a table cannot hold. `make doctor` checks
  tools, architecture, the container runtime socket, the memory profile and the
  envtest assets, and every failure names its fix.
- **Phase 1 — the class.** `ClusterClass std` with docker and inmemory overlays,
  rendered by `make render` and checked in as goldens. `assembly/` tests the
  rendered output rather than the kustomize inputs: the exposed variables, their
  schemas, the absence of deprecated kinds, and that the node image comes from
  `versions.env`.
- **Phase 2 — the policy.** `policy/vap/` holds the two policies and their
  bindings. `policy/tests` compiles that YAML with cel-go and evaluates it:
  100 cases covering every allowed field, every denied path, both halves of
  break-glass, the audit annotation, and each exempt controller. A coverage test
  reads the denied-key lists out of the policy, so adding a denial without adding
  a case fails the build. `make test-envtest` applies the same YAML to a real API
  server and asserts it admits the six-field Cluster and rejects a hand-written
  KubeadmControlPlane.
- **Phase 3 — the stream.** `internal/{snapshot,fold,why,eta,gen,render}` are pure
  and import no Kubernetes client. `internal/watch` assembles the same envelope
  from a live cluster, so a replayed fixture exercises the production path.
  `cluster up | status | why | render | plan | new | explain | docs | kubeconfig |
  doctor` are implemented; `cluster status --replay <dir> --speed N` is the UI
  development loop and needs no cluster at all.
- **Part D.** Every user-facing string is a typed entry in `internal/msg`, and the
  lints walk that registry: next-action on every message, the stall-line contract,
  the admission-message contract, the jargon deny-list generated from the API
  snapshot, golden TTY frames at 80 and 120 columns, colour independence, the
  render budget, stall-detection latency, the exit-code contract, and the
  `--json` schema. `internal/explain` is the one model rung: four backends behind
  one interface, output validated against `docs/schema/explain.v1.json`, a
  grounding check that fails on an invented name or number, and reversible
  anonymisation with one-way IP redaction.

## In progress

`make dev-up` has just run for the first time (a container runtime became
available on 2026-09-10). Until its output is reviewed, treat the following as
unverified against a live cluster:

- The six-field Cluster reaching Ready (Phase 1 acceptance).
- The e2e suite in `e2e/` — written, never executed.
- Recorded fixtures. Every fixture under `testdata/fixtures/` is still synthetic
  and declares it in `_meta.synthetic`.

The next command is:

```sh
make build
./bin/cluster up dev-1 -f examples/dev-1.yaml --no-tty
make record-fixture NAME=std-docker-happy CLUSTER_NAME=dev-1
```

Re-recording a fixture changes files under `testdata/fixtures/` and the goldens
under `testdata/golden/`, and no assertion — that is what the envelope indirection
buys.

## Not started

- **Phase 4 — hosted control plane.** `placement: hosted` folds correctly and has
  fixtures, but k0smotron is not installed by `hack/clusterctl-init.sh` unless
  `WITH_K0SMOTRON=true`, and no hosted cluster has been created.
- **Phase 5 — k0s on machines.** See the contract risk in DECISIONS.md before
  starting: k0smotron v1.10.9 is a v1beta1-contract provider.
- **Phase 6 — the AI usability probe, `cluster-bench`, and cassettes recorded from
  a real local model.** `make doctor-ai` is written but has never been run: no
  model endpoint is configured, so `CLUSTER_AI_MODEL` in `versions.env` is empty.

## Blocked, and on what

Nothing is blocked on a decision. Two things are blocked on a human action:

1. **The project name and the licence** (QUESTIONS.md Q1). `capi-distro` is used
   throughout, including in the break-glass label domain; no LICENSE file is
   committed, because committing one is the decision.
2. **`docs/schema/cluster-spec.v1.json` covers `machineDeployments` only.** A cloud
   overlay that backs a pool with `machinePools` needs that file changed, and
   CLAUDE.md makes `docs/schema/` a must-ask.

## Two things PLAN.md gets wrong about the pinned release

Both are recorded in DECISIONS.md with the source that proves it, and both are
already handled in the code:

1. `spec.topology.class` does not exist at v1beta2; it is
   `spec.topology.classRef.{name,namespace}`.
2. Control-plane replicas cannot be set by a ClusterClass patch. `size` is
   expanded by `internal/gen` into `spec.topology.controlPlane.replicas`, and the
   policy allows that field only when it agrees with `size`.

PLAN.md is left unedited so the human can see the original and confirm the fix.
