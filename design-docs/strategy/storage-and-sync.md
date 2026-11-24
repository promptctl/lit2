# Storage and sync

How lit keeps a backlog that many checkouts write at once, offline, through
the remote the code already uses.

## Diagnosis

When the unit of truth is mutable shared state, every concurrent writer
contends for the same thing, and the cost is the machinery around that
contention: write engines, locks, field-aware reconcile. lit's writers are
worktrees of one clone, clones on several machines and several people, often
offline, with no server to arbitrate. The remote is a plain git host that can
only store and replicate refs and objects. Signed, verified writes (see
`strategy/authority-and-visibility.md`) add a second pressure: most of the
friction between access control and a tracker is friction with reconcile,
such as re-signing replayed commits and merging ciphertext.

The store also lives beside the user's code. Anything that touches the code
repository's refs, objects or remote transfers is felt by every developer on
every git command, and a store that grows with every command grows the remote
without bound.

## Guiding policy

Remove contention by construction rather than manage it by protocol: every
write is an immutable event appended to a log only its writer touches, and
state is derived from the union of all writers' events (`strategy/derivation.md`).
Build each behavior from what git already does natively, and compose a
wanted behavior from those capabilities before adding a mechanism beside
them. Keep everything lit stores out of the code repository's own refs and
objects.

## Actions

**One writer per ref.** Each writing checkout appends to its own log and
publishes to one ref that no other checkout writes. Writes never contend,
push and fetch have nothing to merge, and nothing needs a lock.
Serves: Complexity, Operations.
Spec: `specifications/events-and-store.md`.

**The remote grows with writers, not with events.** A writer's log is sealed
into a few segment blobs under one commit that replaces its ref on each
publish, so the number of events never reaches git as commits or refs. Dead
checkouts' refs are swept so the ref namespace stays at live writers, without
any checkout ever publishing to another's ref.
Serves: Impact on git.
Spec: `specifications/events-and-store.md`.

**A store of its own.** lit keeps its objects in its own object database
under the git common dir, so every worktree of a clone sees one store and the
code repository's object database and refs are untouched. On the remote lit's
data lives under its own ref namespace, which a developer's clone and fetch
never ask for.
Serves: Impact on git, Operations.
Spec: `specifications/events-and-store.md`; the checks are in
`specifications/budgets.md`.

**git writes; go-git reads.** Two writers of one repository is a bug class,
so the user's own `git` binary is the one writer of lit's repository and owns
its formats, locks and fsync. It is also the one reader of the code
repository, never writing it, so lit finds and reads there what git finds and
reads. go-git reads lit's repository in process, because a process per read
grows with history.
Serves: Data, Performance, Impact on git.
Spec: `specifications/events-and-store.md`, "Sync";
`specifications/code-layout.md`, "Module and tooling".

**The remote is a dumb replicated store.** Sync is push and fetch of writer
refs, run by the user's own git so their credentials, ssh configuration and
URL rewriting apply unchanged. Arrival needs no merge: new events simply
exist, and the next fold includes them. Every receiver verifies a
publication before folding it, so the remote is trusted for nothing.
Serves: Operational, Operations.
Spec: `specifications/events-and-store.md`, "Sync".

**Filing without a clone is the same write.** A machine with no store mints
events and publishes them to a fresh inbox ref; any clone's receive verifies,
folds and anchors them. Remote filing is the ordinary write path, not a
second one.
Serves: Operations.
Spec: `specifications/events-and-store.md`.

**Corrections are appends; history changes only through signed rewrite
events.** A wrong status or a mistaken close is fixed by the correcting
event, as a ledger corrects an entry. When history itself must change
(content that must go, noise worth squashing, history worth compacting, a
dead writer's ref) the tool is a rewrite event that names what it drops,
replaces or squashes. A replica can then tell a rewrite from corruption, and
surviving events keep their original signatures. Purge, compaction, sweep and
destroy are all rewrites, and a rewrite reaches every replica that syncs.
Serves: Data, Impact on git.
Spec: `specifications/events-and-store.md`, "Rewrite".

## Headroom in the primitives

The primitive layer carries 120% of what its identified consumers need and
the feature layer covers 80% of the uses: the event shape, the applied
workspace version, the policy events, the signature slot and the prose
envelope carry headroom beyond every current consumer, while the feature
layer covers the main body of uses and leaves the tail unbuilt. Future
features are not predicted; the constraints this design imposes on them are
written down, so evolution is burdened knowingly or not at all.

YAGNI will be raised against every slot: *nothing reads this slot yet, so
ship without it.* YAGNI is right about features: unused surface is
carrying cost, and the 80% feature line exists for exactly that reason. The
slots are not features; they are the primitive layer's headroom, and their
absence is not a smaller system but a fleet-wide schema migration standing
between the system and its own roadmap. A slot whose capability is not built
yet sits unused; the feature waits its turn.

Serves: Complexity.
