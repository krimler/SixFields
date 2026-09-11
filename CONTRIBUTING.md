# Contributing

Thank you for looking. This page is what you need to make a change that lands.

## The one rule

The tests are the specification. A behaviour that matters has a test, and a change
that fixes a bug starts with a test that fails because of the bug.

## Getting set up

```sh
make bootstrap
make doctor
make test
```

`make doctor` tells you what your machine is missing and how to fix it. `make test`
runs in under 30 seconds and needs no cluster.

## The loop

```sh
make test          # pure logic, no cluster, under 30 seconds
make test-envtest  # against a real Kubernetes API server, under 3 minutes
make e2e           # builds real clusters, about 3 minutes
make lint          # golangci-lint, yamllint, kubeconform
```

Use the fastest layer that can express your check. A pure test beats an API-server
test, which beats a test that builds a cluster.

You can work on the display with no cluster at all. SixFields replays recorded
runs:

```sh
make replay F=inmem-stall-vm
```

## Before you open a change

- `make test` and `make lint` both pass.
- New behaviour has a test whose name says the behaviour, so
  `TestUX_StallLineNamesTheRightObject` and not `TestFold3`.
- Golden files are reviewed as a diff. Regenerate with `make golden` and read what
  changed.
- Anything you decided that this repo does not already answer goes in
  `DECISIONS.md`: the date, the decision, what else you considered, and why.

## Things that will bounce

**A condition name written from memory.** Every Cluster API condition this code
names has to appear in `docs/api-snapshot.md`, which is generated from the pinned
release. Run `make api-snapshot` and use what is there.

**A comment that restates the code.** Comments explain why, or cite the upstream
file and line the behaviour depends on.

**An em dash.** There is a lint, over every file this project wrote.

**A contrast that carries nothing**, so "not just X but Y" or "more than just".
Also linted. Naming an alternative you rejected is different, and it belongs in a
comment or in `DECISIONS.md`. It does not belong in something a user reads,
because the user never considered the alternative.

**A test that is skipped.** A skip needs a linked issue and an expiry date in the
reason.

**A new CRD or a new controller.** SixFields is a blueprint, an admission rule and
a command. If you think you need a controller, write it up in `DECISIONS.md`
first.

## Where things live

`CLAUDE.md` is the full set of rules. `PLAN.md` is the plan and its acceptance
checklists. `STATUS.md` says where the work is now. The README has a map of the
directories.

## Licence

By contributing you agree that your work is licensed under Apache-2.0, the same as
the rest of the project.
