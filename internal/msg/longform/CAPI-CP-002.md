# CAPI-CP-002 — a control-plane machine is stuck provisioning

What happened: a Machine belonging to the control plane has not become Ready. The
infrastructure for it exists or is being created, but the node never registered.

Why: usually the bootstrap data never ran, the image is wrong for the architecture,
or the container/VM died on start.

Next: `cluster docs CAPI-CP-002`.
