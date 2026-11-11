# lit2: the requirements

What lit2 must do and must not do, from what lit is for: an issue tracker
that lives in the code repository and is worked mostly by agents. The plan
is judged against this document. The constraints the carried charters and
designs impose are mapped in `constraints.md`; a constraint there that no
requirement here needs is a choice, not a requirement.

## 1. What lit must do

The operations of an issue tracker, as `architecture.md` §4 lists them:
file and groom, pick and claim, move work through a configurable process
with requirements and guidance, review and send back, close with a reason,
order and sequence, link and block, comment and label, search and edit in
bulk, undo, delete and purge, configure the process. Role-based access
control and the workflow features are part of this. Several rows of that
table are specializations of other rows and are not requirements of their
own.

Work is grouped and ordered in containers so that a process and an order
can be scoped. Which containers exist and what they are called is design.
Configuration is per project; finer scopes are design.

All of it works from several worktrees of one clone and from several
machines, offline, with sync through the code's own remote and no server.

Agents are the primary user. Output is deterministic. Its default form is
human-readable text, because that is what a language model reads;
machine-readable forms are secondary.

## 2. What must be true of the data

The same backlog on every machine that holds the same data. Nothing lost
silently under concurrent writes; a contradiction is shown, not resolved by
luck. Deleting and purging are possible and reach every replica. History is
readable. An old binary never corrupts a newer store.

## 3. Performance of lit

This is the reason v2 exists. Every command is fast enough to call in a
loop, sub-second as the reflex. Cost tracks active work, not all history
ever written, or history is compacted so that it does. Background work is
negligible to low in its impact on the machine.

## 4. Zero to negligible impact on the user's git

Clone, fetch, push, status, log and gc are unaffected or affected
negligibly. The repository grows minimally because of lit, and never
without bound. Nothing lit keeps appears in normal git tooling. The working
tree holds only configuration a person put there on purpose, never raw
data.

## 5. A complexity budget

Fewer mechanisms than v1, not a different set of the same size. No locks,
no write engines, no network on a foreground path. Reconciliation is
allowed, because it is typically trivial in time and in complexity:
decidable cases are decided automatically, undecidable cases are not. Every
mechanism is justified by an operation in §1. Anything intricate is a cost
to be removed, not a feature.

## 6. Operational

One static binary, permissively licensed, working against GitHub, GitLab
and a plain ssh remote, with the git credentials the user already has.
