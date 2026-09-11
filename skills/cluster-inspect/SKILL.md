---
name: cluster-inspect
description: Read-only investigation of a Cluster API cluster built with sixfields. Use when someone asks why a cluster is not coming up, what phase it is in, what is blocking it, or what objects a cluster produced. Runs `cluster status --json`, `cluster why --json` and `cluster render`, and grounds every claim in that output.
---

# cluster-inspect

Answer questions about one cluster from its own status. Never write, never apply,
never delete: this skill is read-only by construction, and `cluster-fix` is a
separate skill so that the two cannot be confused.

## Do this

1. `cluster status <name> --json`, the four phases, the estimates, and the stall
   if there is one. Everything you say must come from this document.
2. If a phase is `stalled`, `cluster why <name> --json --explain-ranking`. The
   `stall.object` field is the one object worth naming. `stall.candidates` says
   what else was failing and why it ranked lower.
3. `cluster docs <stall.code>`, the runbook for that stall class. Follow its
   "Check, in order" section. Do not invent checks.
4. Only if the runbook asks for it: `cluster render <name>` for the full object
   set, or the `raw:` command the stall line printed.

## Report like this

- **Phase**: which of the four phases is not done, and its detail verbatim.
- **Blocking object**: `stall.object`, as `Kind/name`.
- **Why**: `stall.message`, quoted. Do not paraphrase a provider message.
- **Elapsed**: `stall.since_ns` as a duration, and how that compares to
  `estimates[<phase>]`.
- **Next**: the runbook step that applies, and the `raw:` command.

## Rules

- Every object name, reason and number in your report must appear in the JSON you
  read. If it does not, you inferred it, say so, or drop it.
- Do not name CAPI condition types in the summary. They are in
  `stall.condition_type` and on the `raw:` line; that is where they belong.
- If `stall` is absent and no phase is stalled, the cluster is waiting normally.
  Say what it is waiting for and what the estimate is. Do not go looking for a
  problem.
- If a command fails, report the failure. Do not substitute a guess.
