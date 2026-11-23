# lit2: the requirements

What lit2 must do and must not do, from what lit is for: an issue tracker
that lives in the code repository and is worked mostly by agents.

## 1. What lit must do

The operations of an issue tracker, as `architecture.md` "Functionality,
composed" lists them: file and groom, pick and claim, move work through a
configurable process with requirements and guidance, review and send back,
close with a reason, order and sequence, link and block, comment and label,
search and edit in bulk, undo, delete and purge, configure the process,
export the whole store and import one. Role-based access control and the
workflow features are part of this. Under role-based access control a
contributor can file tickets but cannot delete an epic or a ticket it did
not create, and a ticket can be kept private from people who can read the
repository.

Work in the backlog is groomed before an agent pulls it, and what groomed
means is part of the configurable process. The process is built from
workflows made of handlers, one mechanism for every use case, and a project
or a person can override any part of a shipped workflow without breaking the
features built on it.

Work is grouped in containers so that a process can be scoped, and
sequenced so that an order can be set. Which containers and sequences exist
and what they are called is design. Configuration is per project; finer
scopes are design.

All of it works from several worktrees of one clone and from several
machines, offline, with sync through the code's own remote and no server.
Ticket data lives in the code's own repository, not a separate one. The
remote stays a dumb replicated store, and all enforcement is client-side:
signatures verified by every client, encryption for visibility.

Agents are the primary user. Output is deterministic. Its default form is
human-readable text, because that is what a language model reads;
machine-readable forms are secondary.

## 2. What must be true of the data

The same backlog on every machine that holds the same data. Nothing lost
silently under concurrent writes; a contradiction is shown, not resolved by
luck. Deleting and purging are possible and reach every replica that syncs. History is
readable. An old binary never corrupts a newer store.

## 3. Performance of lit

Performance is a priority, and the reason v2 exists. The aim is every
command fast enough to call in a loop, sub-second as the reflex; cost that
tracks active work, not all history ever written, or history compacted so
that it does; and background work negligible to low in its impact on the
machine.

## 4. Zero to negligible impact on the user's git

Clone, fetch, push, status, log and gc are unaffected or affected
negligibly. The repository grows minimally because of lit, and never
without bound. Nothing lit keeps appears in normal git tooling.

## 5. A complexity budget

Fewer mechanisms than v1, not a different set of the same size. No locks,
no write engines. Reconciliation is
allowed, because it is typically trivial in time and in complexity:
decidable cases are decided automatically, undecidable cases are not. Every
mechanism is justified by an operation in "What lit must do". Anything intricate is a cost
to be removed, not a feature.

## 6. Operational

One static binary, permissively licensed, working against GitHub, GitLab
and a plain ssh remote, with the git credentials the user already has.
Every dependency is permissively licensed; prior art under a non-permissive
license is reimplemented from its behavior, never copied.
