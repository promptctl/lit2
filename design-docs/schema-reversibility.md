# Schema reversibility: an old reader loses nothing, and only a release raises a remote

Status: accepted design, 2026-11-02. Written tense-independent — it reads as a
specification before implementation and as a record of the architecture after.
If behavior and this document disagree and no superseding design exists, the
divergence is a bug in one of them.

## The two rules

1. **Nothing a user wrote is lost to a binary that cannot read it.** Events are
   immortal and every shape ever shipped stays readable, through the chain of
   pure upcasters the reader holds ([event-store/design.md](event-store/design.md)
   §events). An event whose type or schema version a reader cannot interpret is
   carried byte for byte, counted and named on `/integrity` with the version
   gap, and never dropped or guessed at (`INV:preserve-unknown`); mutations on
   its subject are refused, so the old reader cannot race what it cannot see.
   A newer binary opening the same store reads everything, because the store
   itself never stepped anywhere: the fold is a function of the events, and
   there is no schema on disk to step down.
2. **A dev build never raises a remote's event schema.** A binary built from a
   working tree mints no event at a schema version no published release
   carries, whatever the command or flag, so a remote never holds events only
   a dev build can read. Raising the schema a remote holds is something only a
   published release does, and `min-reader-version` in the workspace file
   ([architecture.md](architecture.md) §3.15) is how a workspace then gates
   writers too old to read it.

The rules support each other. Rule 1 means a raise strands nobody: an older
binary keeps working on what it can read and says what it cannot. Rule 2 means
the raise itself arrives only with a release, so the fleet is never half-blind
to a shape that no release explains.
