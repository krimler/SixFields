# CAPI-ENV-001 — no container runtime is running

What happened: the dev loop needs a container runtime and none is answering.

Why: kind runs the management cluster in containers, and CAPD reaches the runtime
socket to create workload machines.

Next: start Docker Desktop, OrbStack or Colima, then run `make doctor`.
