# CAPI-WRK-002, a node is not joining the cluster

What happened: a Machine has infrastructure and bootstrap data but its node never
appeared in the workload cluster.

Why: the join token expired, the node cannot reach the control-plane endpoint, or
the bootstrap process failed after the machine started.

Next: `cluster docs CAPI-WRK-002`.
