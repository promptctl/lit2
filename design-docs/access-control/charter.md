# Access control: the charter

This document records the directive for designing lit's access-control
system. It is the question; [design.md](design.md) is the answer.

## Why this exists

The goals access control serves, real role-based access control and tickets
private from repo readers, are stated in
[../requirements.md](../requirements.md), as are one repo, no server, and
enforcement in clients.

## Hard constraints

1. **Transparent to the user.** Using lit requires no knowledge of
   cryptography. Key handling, signing, and verification happen without the
   user studying the system.
2. **Rough edges resolve toward lit, not away from the RBAC system.** Where
   access control would need an overly complicated implementation or data
   model, prefer redesigning lit's data model over weakening the
   access-control architecture.
3. **Prefer established patterns.** Reference existing systems wherever
   possible, for the crypto and for the role vocabulary.

## Calibration

The 120/80 calibration in the
[event-store charter](../event-store/charter.md) applies here.
