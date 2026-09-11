# CAPI-ADM-004, break-glass needs both the label and the group

What happened: the object carries the break-glass label but the requester is not in
the break-glass group, or the other way round.

Why: one of the two alone is easy to do by accident. Both together is a deliberate
act, and it is recorded in the audit annotation.

Next: `docs/eject.md` documents both halves and what gets logged.
