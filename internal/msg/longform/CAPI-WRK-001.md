# CAPI-WRK-001 — worker machines are not becoming ready

What happened: the pool has fewer ready replicas than it wants and none has changed
state for a while.

Why: the machines exist but their nodes are not registering, or the pool cannot
create machines at all (quota, template, bootstrap secret).

Next: `cluster docs CAPI-WRK-001`.
