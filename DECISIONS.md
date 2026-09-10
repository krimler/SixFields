# Decisions

Newest first. Each entry: date, decision, alternatives, why, `REVISIT` if a human
should confirm.

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
