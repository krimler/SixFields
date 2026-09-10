# Status

Resume with: `make doctor && make test`

## Done

- Phase 0 scaffolding: repo layout, `versions.env`, `Makefile`, `hack/` scripts.
- `make api-snapshot` writes `docs/api-snapshot.{md,json}` from the pinned CAPI, CAPD and
  k0smotron API packages: 659 constants with file and line, a contract table per provider,
  and the findings that a table cannot express.

## In progress

Phase 0 → Phase 1.

## Blocked

- Recorded fixtures and everything that needs a live management cluster: no container
  runtime is running (see QUESTIONS.md Q1/Q2). Synthetic fixtures stand in and are
  declared as such.
