# CAPI-CP-001, control plane is not initializing

What happened: infrastructure is ready but the control plane has not reported
`Initialized`, so no API server has come up yet.

Why: the control-plane object is waiting on its first machine, on certificates, or
on a bootstrap step that failed. Its own conditions say which.

Next: `cluster docs CAPI-CP-001`.
