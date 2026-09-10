# CAPI-ENV-002 — no such cluster

What happened: no Cluster of that name exists in the namespace.

Why: either the name is wrong, or it is in another namespace, or `cluster up` never
got as far as creating it.

Next: `kubectl get clusters -A`.
