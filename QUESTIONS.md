# Questions

Must-ask items only (CLAUDE.md, "Working without the human"). Each one says what was
done in the meantime so nothing is blocked on an answer.

### Q3, drop `next_command` from `docs/schema/explain.v1.json`

**Context.** The model used to be asked for the command printed under its three lines.
Judged by hand over six diagnosis tasks it read the wrong object in three of them: a real
name under a kind it does not have, which returns NotFound, and a lookup by a Cluster's
name that no control plane carries. The analyser has already ranked one object and already
written the command that reads it, so there was nothing for a model to decide.

**What was done in the meantime.** The prompt no longer asks for a command, and every
backend is wrapped so the analyser's command replaces whatever the model returned. Judged
again, all six tasks now read the right object and the grounding check flags none. The
schema file is untouched, and `next_command` is still `required` in it, but the requirement
is now satisfied by the analyser rather than by the model: the wrapper fills the field
before validation runs, so a backend that emits one has it discarded and a backend that
emits none validates anyway.

**The question.** `docs/schema/` is on the must-ask list, so the field stays until you say
otherwise. It now has no effect on anything: removing it in a v2 would stop models
generating a string that is thrown away, and keeping it leaves the schema describing a
field the code overwrites.

**Recommendation.** Cut it in `explain.v2.json`, keeping v1 readable for the recorded
cassettes, which were recorded against v1 and still replay. The alternative, editing v1 in
place, would invalidate every cassette in `testdata/cassettes/`.

## Answered

### Q1, Project name and licence (answered 2026-09-11)

The project is **SixFields**. The licence is **Apache-2.0**, in `LICENSE`.

Apache-2.0 is what every CNCF project uses and what the CNCF IP Policy expects, and it is
what Cluster API, k0smotron, Kubernetes and every provider here already use, so there is no
mixing to reason about. LGPL-3.0 was the alternative; it is avoided across the cloud-native
ecosystem by anyone who links or redistributes, and CNCF would not accept it.

Moved to DECISIONS.md.

### Q2, Recorded fixtures need a container runtime (answered 2026-09-10)

A runtime became available and four fixtures are now recorded from real runs:
`std-docker-happy` (six points to Ready), `std-docker-ready`, `hosted-docker-happy`, and
`stall-cp-killed`, induced by removing a live control-plane container. The rest are
declared stand-ins and `TestFixtures_SyntheticAreDeclared` enforces the declaration.
