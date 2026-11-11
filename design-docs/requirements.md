# lit2: the requirements

What lit2 must do and must not do, stated from what lit is for. The plan
is judged against this document. The constraints the carried charters and
designs impose are mapped separately in `constraints.md`; where one of those
is not needed to meet a requirement here, it is a choice, not a requirement.

## 1. What lit must do

The operations in `architecture.md` §4, in general: file and groom, pick
and claim, move work through a configurable process with requirements and
guidance, review and send back, close with a reason, order and sequence,
link and block, comment and label, search and edit in bulk, undo, delete
and purge, configure the process. Role-based access control and the
workflow features are required. Some rows of that table are
specializations derivable from other behavior rather than requirements in
their own right.

Not required: arbitrary configuration per epic. The epic itself is not a
requirement.

All of it from several worktrees of one clone and from several machines,
offline, with sync through the code's own remote and no server.

Agents are the primary user. Output is deterministic. It is human-readable
text by default, because that is what a language model reads;
machine-readable output is a secondary concern.

## 2. What must be true of the data

The same backlog on every machine that holds the same data. Nothing lost
silently under concurrent writes; a contradiction is shown, not resolved by
luck. Deleting and purging are possible and reach every replica. History is
readable. An old binary never corrupts a newer store.

## 3. Performance of lit

This is the reason v2 exists. Every command is fast enough to call in a
loop, sub-second as the reflex. Cost tracks active work, not all history
ever written, or history is compacted so that it does. Background work is
broadly negligible to low impact.

## 4. Zero to negligible impact on the user's git

Clone, fetch, push, status, log and gc: zero to negligible impact. Minimal
growth of the repository because of lit, and never without bound. Nothing
lit keeps shows up in normal git tooling. The working tree holds only
purposeful user configuration, never raw data.

## 5. A complexity budget

Fewer mechanisms than v1, not a different set of the same size. No locks,
no write engines, no inline network. Reconciliation is allowed, since it is
typically trivial in cost, in time and in complexity: decidable cases are
decided automatically, undecidable cases are not. Every mechanism is
justified by an operation in §1. Anything intricate is a cost to be
removed, not a feature.

## 6. Operational

One static binary, permissively licensed, working against GitHub, GitLab
and a plain ssh remote, with the git credentials the user already has.
