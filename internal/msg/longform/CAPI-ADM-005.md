# CAPI-ADM-005 — the version is not a Kubernetes version

What happened: `spec.topology.version` is not a version string. The commonest cause
is an unsubstituted placeholder — a file meant to be run through `envsubst` that was
applied directly.

Why: the field is checked before the cluster is created, because a bad version does
not fail until a machine tries to pull an image that does not exist, minutes later
and in a different object.

Next: set `spec.topology.version` to something like `v1.34.11`. `versions.env`
records the version this assembly is tested against.
