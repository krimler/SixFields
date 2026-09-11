# CAPI-ADDON-001, add-ons were not applied

What happened: a ClusterResourceSet matched the cluster but its resources are not
applied, so the cluster is up without its CNI or other add-ons.

Why: the referenced ConfigMap or Secret is missing, or applying it failed against
the workload cluster.

Next: `cluster docs CAPI-ADDON-001`.
