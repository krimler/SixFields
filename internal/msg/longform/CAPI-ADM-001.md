# CAPI-ADM-001 — the field is managed by the class

What happened: the field you set on the Cluster is one the ClusterClass owns, so
admission rejected the write instead of letting a controller overwrite it later.

Why: the point of the assembly is that six fields are yours and the rest belong to
the class. Errors arrive here, at the object you edited, rather than an hour later
in an object you did not.

Next: set it through the class, or read `docs/eject.md` for the break-glass and for
how to leave the assembly entirely.
