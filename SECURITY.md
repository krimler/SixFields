# Security

## Reporting a problem

Report a security problem privately. Open a GitHub security advisory on
[krimler/SixFields](https://github.com/krimler/SixFields/security/advisories/new),
which is visible only to the maintainers, or email yavan@outlook.com.

Do not open a public issue for a security problem.

Tell us what you found, how to reproduce it, and what an attacker gets. You will
get an acknowledgement within three working days.

## What counts

SixFields runs on your laptop and in your management cluster. The parts worth
reporting:

- A way for an ordinary user to write a field or a kind the admission policy is
  meant to refuse, without break-glass.
- A way to use break-glass with only one of its two halves.
- Anything that leaves the machine when `CLUSTER_AI` is `off`, or that reaches a
  model with a name, an address or a secret that `--anonymize` should have
  replaced. `docs/ai.md` states what is sent and when.
- A command that prints a credential, or writes one to a file readable by others.

## What does not count

- The break-glass path working as documented. It is meant to let a named group
  bypass the policy, and every use is recorded in the audit log.
- Anything requiring cluster-admin on the management cluster. Someone with that
  can delete the policy.
- Versions of Cluster API, Kubernetes or a provider other than the ones pinned in
  `versions.env`.

## Supported versions

This project has made no release. The supported version is the current commit on
the main branch.
