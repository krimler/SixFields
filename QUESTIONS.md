# Questions

Must-ask items only (CLAUDE.md, "Working without the human"). Each one says what was
done in the meantime so nothing is blocked on an answer.

No questions are open.

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
