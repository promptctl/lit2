# Event store: the charter

This document records the directive for the event-sourced store lit is
built on. It is the question; [design.md](design.md) is the answer and
[budgets.md](budgets.md) is the numbers.

## Why this exists

Two pressures point at the same architecture.

**Concurrency.** When the unit of truth is mutable shared state, every
concurrent writer contends for the same thing, and the cost is the machinery
around that contention: write engines, locks, field-aware reconcile. Making
writes immutable appends and deriving state deterministically removes that
contention by construction rather than managing it by protocol.

**Access control.** The access-control charter
([../access-control/charter.md](../access-control/charter.md)) demands signed
mutations verified by every client over a dumb replicated remote. Most of the
strain in access control — re-signing replayed commits, ciphertext merge
fingerprints, tier-scoped reconcile — is friction between RBAC and
*reconcile*, and the event store removes reconcile.

## Hard constraints

1. **One repo, no server, git-native.** The goal is stated in
   [../requirements.md](../requirements.md). Ticket data lives in lit's own
   git object database under the code repo's git common dir and syncs as git
   refs to the same remote the code uses.
2. **Every command under one second.** The goal is stated in
   [../requirements.md](../requirements.md); the budgets, and the 10x scale
   they are measured at, are in [budgets.md](budgets.md).
3. **Schema primitives.** The event schema carries a signature slot and
   principal reference on every event; prose payloads as opaque envelopes;
   on every event, the blob id of the workspace version its writer ran under
   and the policy version in force; a signed, versioned policy
   as events in the store; and the workspace model as data a committed file declares and an event applies,
   enforced by generic code at the write, not hand-coded rules. These
   are primitives for the access-control layers and the user-defined
   hierarchy feature.
4. **lit leaves the code repository's git alone.** Locally, lit keeps its
   data in its own storage under the git common dir and adds nothing to the
   code repo's refs or object database. On the remote, lit's data lives under
   its own refs and nowhere else. No git operation the developer runs changes
   in behavior, output, or speed.
5. **Built fresh.** lit2 does not migrate lit v1's store.

## Calibration

120% of the primitives, 80% of the features: the primitive layer (event shape,
the applied workspace version and the policy events, signature slots, fold
engine) carries headroom beyond every
identified consumer; the feature layer covers the main body of uses and leaves
the tail unbuilt. For future lit features, do not predict them — document the
constraints this design imposes on them, so evolution is burdened knowingly or
not at all.
