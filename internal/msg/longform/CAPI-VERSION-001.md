# CAPI-VERSION-001 — the requested Kubernetes version is not available

What happened: the control plane cannot provision because the version in
`spec.topology.version` has no image the provider can use.

Why: the version does not exist, or it exists upstream but the pinned node image for
it was never built for this architecture.

Next: set `spec.topology.version` to a version the provider publishes. `versions.env`
records the one this assembly is tested against.
