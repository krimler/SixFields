# Status

Resume with:

```sh
make doctor && make test && make test-envtest
```

Last updated 2026-09-11, after a session that took the assembly from nothing to
all seven phases running on a live management cluster.

## Where it stands

All seven phases of PLAN.md are implemented, and Phases 0 to 5 have been verified
against a real kind + Cluster API + CAPD + k0smotron management cluster, not only
against fixtures. `make test` runs in 5s of a 30s budget, `make test-envtest` in
16s of 180s, and `make lint` is clean.

| Phase | State |
|---|---|
| 0 — environment and truth | done; `make dev-up`/`dev-down` from clean, `make api-snapshot` generates the API truth from the pinned modules |
| 1 — the class | done; the six-field Cluster reaches Ready, only topology-owned objects exist |
| 2 — the policy | done; verified live and by 100 cel-go cases with generated coverage |
| 3 — the stream | done; every D2 gate is a test, replay is the UI loop |
| 4 — hosted control plane | done; a k0smotron control plane reaches Ready in 3m07s and reports readiness, not node counts |
| 5 — k0s on machines | done; both placements are one provider family and differ in one class reference |
| 6 — eject, e2e, docs | eject verified live (649 rendered lines re-apply with zero diff); the e2e suite is written but has never been run as a suite |

## What is proven, and how

Four fixtures are recorded from real runs and the rest are declared stand-ins
(`_meta.synthetic`, with a note saying what each stands in for). The recorded ones
are `std-docker-happy` (a full provisioning timeline), `std-docker-ready`,
`hosted-docker-happy`, and `stall-cp-killed`, which was induced by removing a live
control-plane container.

The AI rung is real too: `make doctor-ai` picked `qwen2.5:14b` on this machine
(24 − 5 VM − 5 macOS = 14 GB, measured 26 tok/s warm), and the seven cassettes in
`testdata/cassettes/` were recorded from it. Every one passes the schema and the
grounding check, and they replay inside `make test`, which never calls a model.

## What is still prototype

- **The e2e suite** in `e2e/` is written against the live paths but has never been
  executed end to end. Run it with `make e2e`.
- **The `inmemory` overlay** renders, is tested, and is the intended fast
  substrate, but no cluster has ever been created from it. PLAN.md Phase 3 wants
  `inmem-happy` and the two induced in-memory stalls recorded from it; all three
  are still synthetic.
- **`cluster-bench`**, the **nightly UX probe** and `docs/ux-probe/` are not built.
  `docs/ux-probe/README.md` says what a report must contain.
- **`cluster history` and `cluster rollback`** (PLAN.md D5.6) are not implemented.
- **The `--explain` grounding** is checked against cassettes, not against a live
  model on every run. `make test-llm` with `CLUSTER_AI_RECORD=1` re-records.

## Known constraints, found by running it

Each of these cost real time to find and is now either handled in code or written
down where the next person will hit it:

- A Cluster's placement cannot be changed after creation, and a ClusterClass's
  control-plane kind cannot be changed in place. `hack/dev-up.sh` recreates a class
  whose control plane changed, and refuses while any Cluster uses it.
- Installing the assembly is itself a break-glass write, because `*Template` is a
  managed kind. `dev-up` does it the documented way and the audit annotation
  records every install.
- CAPI defaults every ClusterClass variable onto the Cluster, so a variable the
  policy does not allow breaks an ordinary user's write. The node image is written
  into the machine templates instead of being a variable.
- k0smotron wants the same k0s release spelled two ways, and its `K0sControlPlane`
  reports a k0s version where CAPI's preflight expects a Kubernetes one. Both are
  handled in the class with the citation next to them.
- When a MachineSet cannot create a Machine, CAPI v1.14.2 reports it in no
  condition and emits no event. The `CAPI-WRK-001` runbook says where the reason
  actually is.

## Blocked on a human

1. **The project name and the licence** (QUESTIONS.md Q1). `sixfields` is in the
   module path and the break-glass label domain; no LICENSE is committed, because
   committing one is the decision.
2. **`docs/schema/cluster-spec.v1.json` covers `machineDeployments` only.** A cloud
   overlay backing a pool with `machinePools` needs that file changed, and
   CLAUDE.md makes `docs/schema/` a must-ask.

## Two things PLAN.md gets wrong about the pinned release

Both are recorded in DECISIONS.md with the source that proves it, and both are
handled in code. PLAN.md is left unedited so the original is visible.

1. `spec.topology.class` does not exist at v1beta2; it is
   `spec.topology.classRef.{name,namespace}`.
2. Control-plane replicas cannot be set by a ClusterClass patch, so `size` is
   expanded by `internal/gen` and the policy allows the field only when the two
   agree.
