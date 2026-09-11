# UX probe reports

One file per run, named `<date>.md`. A probe is a coding agent given only
`docs/user-guide.md`, the `cluster` binary and an in-memory management cluster,
asked to create a cluster, find out why a stalled one is stuck, and eject one.

What a report records: the commands issued in order, every dead end, the time to
each answer, and the two thresholds, at most three commands to a correct status,
at most two to the stall reason. A regression against the previous report is an
issue, not a note.

No report exists yet. `PLAN.md` Part D makes one a precondition for calling v0
done, and `STATUS.md` says so.
