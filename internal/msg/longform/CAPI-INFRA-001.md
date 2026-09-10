# CAPI-INFRA-001 — infrastructure is not ready

What happened: the Cluster's `InfrastructureReady` condition is still False, so the
infrastructure provider has not finished creating the network, load balancer and
whatever else the cluster needs before a control plane can start.

Why: the infrastructure object (DevCluster, AWSCluster, ...) reports the real reason.
Everything else is waiting on it, which is why this is the only line worth reading.

Next: `cluster docs CAPI-INFRA-001` for the runbook, or read the object with the
`raw:` command printed under the stall line.
