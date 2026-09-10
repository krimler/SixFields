# CAPI-CP-003 — etcd is not coming up

What happened: the control plane reports etcd as unhealthy or not provisioned, so
the API server cannot serve.

Why: an etcd member failed to start, lost quorum, or on the in-memory backend its
configured startup duration has not elapsed.

Next: `cluster docs CAPI-CP-003`.
