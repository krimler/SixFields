# Status

Resume with:

```sh
make doctor && make test && make test-envtest
```

Last updated 2026-09-12, after a control study and the nine fixes it produced.

## Where it stands

All seven phases of PLAN.md are implemented and verified against a live kind +
Cluster API + CAPD + k0smotron management cluster. `make test` runs in 3s of a
30s budget, `make test-envtest` in 26s of 180s, `make lint` is clean.

| Phase | State |
|---|---|
| 0, environment and truth | done |
| 1, the class | done |
| 2, the policy | done; the managed-kind list now covers the infrastructure machine kinds |
| 3, the stream | done; `cluster why` answers in 65ms, was 5,450ms |
| 4, hosted control plane | done |
| 5, k0s on machines | done |
| 6, eject, e2e, docs | done; `cluster render` emits the class and its templates, and names what it skips |

## The control study

`paper/study/` holds the whole thing: `findings.md` is the writeup,
`measure.sh` re-runs every measurement, `data/*.csv` are the numbers,
`build.sh` redraws the figures. It ran the three pre-registered first-use tasks
against this tool and against `clusterctl` alone, five blinded trials per task per
arm, with commands counted by logging shims rather than self-reported.

Headline: 1 command against a median of 7 to create a cluster, 2 against 3 to
diagnose a stall, and 3 lines of output to read against 113. The control arm won
the eject task outright until that was fixed, and still gives a more specific
diagnosis than `cluster why` does.

It found thirteen defects, counted as distinct repairs, each carrying a regression
test that fails if the repair is reverted. Twelve were properties of the system as
measured; the thirteenth was introduced by two of the repairs and caught by
re-measuring. DECISIONS.md 2026-09-12 has an entry per defect with before and after.
The three that matter to the claims:

1. An ordinary user could `kubectl patch devmachine`, so the managed surface was
   smaller than described.
2. `cluster why` spent 5.4s of every invocation inside client-go's own rate
   limiter. Nothing to do with the model, which is never called without
   `--explain`.
3. `cluster render` omitted the ClusterClass its own output refers to, so the
   escape hatch depended on the thing it exists to escape.

## What is still prototype

- **`cluster history` and `cluster rollback`** (PLAN.md D5.6) are not implemented.
- **The `anthropic` backend** has never run. `AI_CREDIT_CAP_USD` is 0.
- **The UX probe and the control study run by hand.** Nothing schedules either.
- **`inmem-happy` is still synthetic.**
- **The model writes raw condition names into prose, and nothing stops it.**
  `docs/schema/explain.v1.json` says the three lines carry no condition type names;
  the local model writes `WaitingForStartupTimeout` into them, and no check
  enforces the schema's sentence. The jargon lint that keeps those identifiers out
  of `internal/msg` does not run over model output. Recorded in
  `paper/study/data/model-judge.csv`, `after-fixes` row `inmem-stall-vm`, and in
  DECISIONS.md 2026-09-12 under the tension it creates with the four-phase display.
- **The model no longer writes the command under its lines.** The analyser supplies
  it, so it cannot name an object the analyser did not rank. What the model still
  decides is the prose.

## Known constraints, found by running it

- A Cluster's placement cannot change after creation, and a ClusterClass's
  control-plane kind cannot change in place.
- Installing the assembly is itself a break-glass write.
- CAPI defaults every ClusterClass variable onto the Cluster.
- k0smotron wants the same k0s release spelled two ways.
- A MachineSet that cannot create a Machine reports it in no condition and emits
  no event.
- CAPI never removes an owner reference from a template a ClusterClass has
  stopped naming, and one ClusterResourceSet owns every cluster's binding. Both
  make ownership the wrong edge to follow out of a shared object.
- The API server accepts a Cluster naming a ClusterClass that does not exist, with
  a warning rather than an error. No admission policy can catch it; the client
  looks the name up instead.

## Blocked on a human

1. **The project name and the licence** (QUESTIONS.md Q1).
2. **`docs/schema/cluster-spec.v1.json` covers `machineDeployments` only.**
