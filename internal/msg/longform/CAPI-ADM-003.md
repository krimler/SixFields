# CAPI-ADM-003 — the variable is not one the class exposes

What happened: `spec.topology.variables` names a variable that is not in the
allow-list.

Why: the class exposes exactly the variables it can honour. An unknown name would be
silently ignored by the topology controller, which is worse than a rejection.

Next: `cluster render --class` lists the variables the class accepts. `docs/eject.md`
has the break-glass.
