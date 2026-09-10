# CAPI-TOPO-001 — the topology could not be reconciled

What happened: the Cluster's `TopologyReconciled` condition is False, so the class
never produced the objects the cluster needs.

Why: the topology names a class, version or variable the class does not accept.
This is the one stall class that is almost always a mistake in the Cluster you wrote,
not in the infrastructure.

Next: `cluster plan -f <your file>` reproduces the same error before you apply, and
`cluster docs CAPI-TOPO-001` has the runbook.
