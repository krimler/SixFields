# CAPI-ENV-003, there is no such ClusterClass

What happened: `spec.topology.classRef.name` names a ClusterClass that is not
installed in the Cluster's namespace. A misspelt name is the commonest cause.

Why: the API server accepts a Cluster whose class does not exist. It attaches a
warning to the response and stores the object, and then nothing happens: no
machines, no phase moves, and the only sign of trouble is a condition saying the
topology has not been fully reconciled. The class name is also the one field the
admission policy cannot check, because a policy sees the object being written and
cannot read a second object to find out whether the class exists. So `cluster up`
looks instead, before it applies.

Next: `kubectl get clusterclass -n <namespace>` lists the names that work. If the
class you meant is genuinely missing, `make dev-up` installs the assembly.
