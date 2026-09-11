# CAPI-ADM-002, the kind is managed by the class

What happened: you tried to create or update a kind the class generates
(KubeadmControlPlane, MachineDeployment, Machine, a *Template, ...).

Why: those objects are derived from the Cluster. Editing them directly is undone at
the next reconcile, so the policy stops the write and you keep your work.

Next: change the Cluster instead, or read `docs/eject.md`.
