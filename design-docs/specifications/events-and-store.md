# Events and the store

Carries out `strategy/storage-and-sync.md`.

Every lit mutation is an immutable, signed, content-addressed **event**
appended to the writing checkout's own log in lit's own git object database
under the git common dir. State is a deterministic fold over the union of
all writers' events (`specifications/fold.md`). Sync is `git fetch` and
`git push` of per-writer refs against the code repository's ordinary remote.

## The invariant registry

Code and documents cite these as `[INV:token]`. Each is defined in the
section named.

| Token | One line |
|---|---|
| `INV:rewrite-is-an-event` | an event changes after it is written only by a rewrite event naming the change; a receiver accepts a published tree that lacks events it held only when rewrite events account for every one ("Rewrite") |
| `INV:deterministic-fold` | same event set ⇒ equal derived state on every machine, in any arrival order, from genesis or a snapshot; only a fold-version bump may change it (`specifications/fold.md`) |
| `INV:single-writer-ref` | each ref has exactly one writing checkout; all cross-writer coordination is read-side ("Log, segments and refs") |
| `INV:preserve-unknown` | an event a reader cannot interpret is carried and surfaced, never discarded ("Unknown events") |
| `INV:disposable-cache` | derived local state may always be deleted and refolded; nothing behaves as if the cache were truth ("Cache") |
| `INV:opaque-envelope` | layers below the verb layer treat prose payloads as opaque bytes ("Event model") |
| `INV:config-versioned` | every event names the active workspace version its writer ran under (`config-ref`) and the policy version in force ("Event model") |
| `INV:wall-time-is-data` | every event carries a wall-clock time; durations read it, order never does ("Wall time") |

## Event model

An event is `(type, schema-version, subject, payload, attribution,
config-ref, policy-version, causal-position, recorded-at, signature-slot)`.

- **Signature.** The signature covers a **header**: every field but the
  payload, plus a salted hash committing to the payload. It never covers the
  payload itself or any other event. A rewrite that replaces or drops a
  payload therefore leaves the author's signature valid: it still proves who
  made which change where, and a verifier reports the payload as rewritten,
  pointing at the rewrite event and its own signature. The salt travels with
  the payload and is erased with it, so a low-entropy value cannot be
  recovered by guessing its hash. Every event is signed, from the first event
  of every store, by its principal's key or a key the principal delegated to
  the clone (`specifications/access.md`, "Principals and keys"). The
  signature slot names its crypto suite.
- **Attribution** is `(checkout, principal, agent)`
  (`specifications/access.md`, "Identity").
- **`config-ref`** is the git blob id, in the code repository's object
  format, of the active workspace version the writer ran under
  (`specifications/workspace-file.md`, "Applying a version").
  **`policy-version`** is the policy version in force at the writer's fold
  (`INV:config-versioned`).
- **`causal-position`** is a Lamport timestamp with the Lamport advance
  rule: a writer mints its next event at `1 + max(its own last counter, the
  highest causal-position among all events it has folded)`. Observing an
  event always places your next event after it, so an intent minted because
  you saw something supersedes that something on every machine. A plain
  per-writer sequence number was rejected because a deliberate override
  could sort before the event it overrides. Within a command's batch the
  counter advances per event: a command minting k events occupies k strictly
  increasing positions, so the total order never leans on payload iteration
  order.
- **Total order** is `(causal-position, writer-id, event-id)`, computed
  identically on every machine and needing no wall clock. The event id is
  needed because two commands of one checkout can fold one frontier and so
  mint one `(writer, position)`.
- **Envelopes.** Prose payload fields are opaque byte strings end to end
  (`INV:opaque-envelope`). The fold, cache, codec and sync never inspect
  prose, so tiers change what the verb layer does, not what the store does
  (`specifications/access.md`, "Tiers and envelopes").

## Wall time

`recorded-at` is the writer's clock reading when it minted the event, stored
as read. It is data, not order (`INV:wall-time-is-data`). Durations read it:
how long a ticket has been in progress, how long since it was last touched,
whether a claim has outlived `claim_ttl`. Supersession is the total order's
question and *when* is `recorded-at`'s (`specifications/fold.md`, "Order"): a
writer that worked offline through many events can rank a three-day-old
touch after one made today.

A writer's `recorded-at` is its own clock reading, never a time it folded
from another writer carried forward. Otherwise one fast clock would hand its
future to every writer that folded its events, and every claim they
refreshed would read as fresh until real time caught up.

Skew still reaches every duration and can make one negative. A writer whose
clock runs slow by more than `claim_ttl` publishes events a reader already
counts as expired, so a live lane looks free; a writer whose clock runs fast
keeps a dead lane held for as long as its clock is ahead. How a reader
surfaces skew is `specifications/fold.md`.

## Intent

Events carry **intent, not derived state**. The fold owns derivation, and an
event that embeds a derived value forks truth into two homes:

- WRONG: `{"type":"rank.set","issue":"x1","position":4}`: position 4 of
  what list? Two concurrent events like it cannot both be true.
- RIGHT: `{"type":"rank.place","issue":"x1","above":"y2"}`: an intent that
  stays meaningful whatever else happened concurrently.

The verb layer compiles every write into intent events. The verbs are the
interface and the events are the memory, and the two are not the same shape:

| on the command line | events minted |
|---|---|
| `set x/title "…"` | `field.set {item, field, value, over}`, `over` the candidates the writer saw and replaces |
| `set x/status review` | `transition {item, from, to, over}`, `over` the status writes the writer saw |
| `add x/labels ready` / `rm x/labels/ready` | `member.add {item, field, member}` / `member.rm {item, field, member, observed}`, `observed` the adds the writer saw |
| `set x/labels '[a, b]'` | one `member.rm` per element removed, one `member.add` per element added |
| `add x/comments "…"` | `comment.add {item, id, body}` |
| `rm x/comments/<id>`, which is `set x/comments/<id>/deleted true` | `field.set {element, deleted, true}` about the comment, a node with fields as an item is; `get x/comments` still lists it, marked |
| `add <node>/lanes/datetime '[dee, b64]'` | `lane.create`, then `lane.place {lane, item, after}` per member |
| `set <node>/lanes/datetime '[b64, dee, fw9]'` | one `lane.place` per member whose predecessor changed |
| `add x/checklist '["…", "…"]'` / `set x/checklist/<id>/state done` | `checklist.add {item, id, text}` / `checklist.set {item, id, state, reason}` |
| `set <lane>/rank '{above: y}'`, `'{below: y}'`, `top`, `bottom` | `rank.place {lane, above \| below \| top \| bottom}` |
| `set /columns/impl/entry '[…]'` | no event: an edit to `.lit/workspace.toml` in the working tree, echoed as such |
| `lit2 config commit` | an apply carrying the version's bytes (`specifications/workspace-file.md`, "Applying a version") |
| `rm x`, which is `set x/deleted true` | `field.set {item, deleted, true}`, a reversible tombstone; `set x/deleted false` restores |
| `add <lane>/claim` / `rm <lane>/claim` | `claim.take {lane}` / `claim.release {lane}` |
| any write with `--reason "…"` | the reason on every event the write mints, an enveloped prose field |
| `rm x --purge` | a rewrite naming the item ("Purge") |

## Ids

Ids are minted with no coordination, so they are collision-proof by
construction: an id, an item's or an element's such as a comment's, is 128
random bits minted at its create and carried in the create event. The fold
reads it from there, so where the event lands in a log is no part of
identity, two offline checkouts cannot mint one id, and a create replayed
into another log keeps its id. A purged id is never reused: a create with
that id never arrives again, because the purge drops it.

**Segment.** An id is rendered by its segment, a short form fixed at mint:
the shortest prefix of the id that no id this writer has folded shares, as
wide as the namespace's fill needs, so a later mint goes wider rather than
making an earlier segment ambiguous. Two offline writers can fix one segment
for two items; after sync that segment resolves to neither, the reader is
shown both, and each item keeps its id. A child renders as
`<parent>.<segment>` from its parent link, which is not part of the id.

**Rendering.** An item renders as `<prefix>-<topic>-<segments>`, for example
`lit-bootstrap-1f9.zc7`: the prefix is the label the active workspace version
supplies, and the topic is the item's own `topic` field
(`specifications/work-items.md`). Neither is part of the id, so a reader can
tell otherwise arbitrary segments apart, and changing either rewrites
nothing. A reference in prose is stored as `@<segments>` and rendered the
same way, so prose follows a rename too. Parsing accepts the segments bare,
or under any prefix a version in this store's log has ever declared and any
topic the item has ever carried. It refuses a prefix none has declared,
naming it as another project's, and a topic the item never had, naming the
item's current rendering. A rendered form is display; the id is what agents
store.

## Schema versions

Every shipped event shape stays readable while any store may hold its
version. Readers hold a chain of pure upcasters (v1→v2→…) applied at fold
time; a schema bump changes no bytes on disk. An upcaster stays while any
store may hold its version, because deleting it deletes that store's ability
to read its own history.

A binary built from a working tree mints no event at a schema version no
published release carries, so a remote never holds events only a dev build
can read. Raising the schema a remote holds is a release's act, and
`min-reader-version` in the workspace file is how a workspace then gates
binaries too old to read it.

## Unknown events

A reader meeting an event type or version it cannot interpret
(`INV:preserve-unknown`) carries it byte for byte and surfaces one warning
naming the version gap. It never drops, never guesses, and never treats
failure to read as absence:

- WRONG: skip the unknown event and fold the rest. The fold now silently
  disagrees with every up-to-date machine, and the divergence has no bad
  data anywhere to find.
- RIGHT: fold what is readable, report `3 events from a newer lit2 (min
  reader vX) not applied`, and refuse mutations that would race the unread
  events' subjects.

`/next` lists any lane with a member carrying unread events as unreadable
and skips it. `set` on such an item is refused naming the version. Reads of
other items proceed. Once the workspace's `min-reader-version` exceeds the
binary's own, every command refuses, naming the version and the remedy.

## Codec

The codec is invisible above the `events` unit, and its framing lets the
fold meet the full-refold budget (`specifications/budgets.md`).

## Log, segments and refs

On disk, per clone, the store's root is `<git-common-dir>/lit/store/`: lit's
own git object database, never the code repository's, so all worktrees of a
clone see one store. Key material sits beside it and outside it
(`specifications/access.md`, "Identity").

**Checkout id.** Each checkout's id is a full-entropy 128-bit token minted
once into `<git-dir>/lit/checkout/id`, 32 lowercase hex digits and a
newline, which git deletes with the worktree. The short forms surfaces
render are display aliases, never identity. Ref names and the fold's
writer-id tiebreak lean on its uniqueness, and at full entropy it holds even
between never-connected workspaces that later meet on one remote.

**Log.** A command appends its events to the writer's own append-only log
under the common dir, one record per event, and touches nothing else in the
store. A crash mid-append loses at most the record being written. A command
reads every writer ref head and every open log under the common dir, its own
and its sibling worktrees', so a sibling's write is visible to the next
command without a sync.

**Segments and the ref.** Sync seals the log into segment blobs of bounded
size and publishes one commit per writer, whose tree holds the writer's
segments, to `refs/lit/log/<checkout-id>`. No checkout ever writes another's
ref (`INV:single-writer-ref`). The ref is replaced on each push, never
advanced, because only its writer updates it. The events are the history
and the ref has none, so the remote holds, per live writer, one commit, one
tree and a few segment blobs, and the number of events never reaches git.
A writer's log is the source of its ref: its next sync re-publishes the full
tree over a ref that is missing or vandalized on the remote.

Rejected: one commit per command on a chained ref, which grows git with
every mutation; and a merged state file on one branch.

## The code repository's git

lit adds nothing to the code repository's refs or object database, and on
the remote its data lives under `refs/lit/` and nowhere else. A developer's
clone and fetch ask for `refs/heads/*` and `refs/tags/*`, so lit's refs and
the objects behind them are never transferred. Under protocol v2 the
advertisement is filtered by the prefixes asked for, so lit's refs are not
listed either; a protocol v0 client is sent every ref on every fetch and pays
in proportion to the ref count, which the sweep ("Ref sweep") keeps at live
checkouts plus unswept inbox refs. `specifications/budgets.md` carries the
check.

## Rewrite

A **rewrite** is an event appended to the rewriter's log that names the
events it drops, the payloads it replaces, and the ranges it squashes; a
mangled event is one more thing a rewrite replaces or drops. The fold
applies it on every replica the moment it arrives. Each writer then re-seals
its segments without the rewritten material and replaces its ref, and a
receiver accepts the new tree only when the events it lacks are the ones
rewrite events name (`INV:rewrite-is-an-event`). A rewrite re-seals
segments, never events, so surviving events keep their bytes and their
original signatures.

**Compaction** is the rewrite that squashes, keeping and dropping by address
within windows bounded by an item's create and its transitions, so the state
an item was in when it moved stays as it was judged at the write. Rejected:
compaction by truncation, dropping old events with no rewrite event naming
them, because a replica could not tell it from corruption and the survivors'
signatures would prove nothing about what went; and a `gc` that edits
history in place. Snapshots accelerate; they never replace events.

**Erasure reaches every replica that syncs.** Copies that never sync again
(a stale clone, a host that has not collected garbage, a backup) keep what
they had, destroyed content keys included (`specifications/access.md`,
"Keys, grants, rotation and erasure").

### Purge

Purge, `rm x --purge`, removes one ticket the way a git history rewrite
removes content, in three parts.

- **The record.** A purge is an event appended to the purging checkout's own
  log. It names the ticket by its id and birth (`created_at`) and carries
  nothing else of the ticket. Nothing un-purges, and the purge event is what
  reaches other stores.
- **The fold.** The fold drops every event whose subject is a purged id,
  wherever it sits and whenever it was minted: before the purge, or after it
  by a writer that had not yet received it. The ticket is gone from derived
  state on every machine the purge has reached, the moment it arrives,
  whatever that machine had done to it. Another ticket's events stay: an
  edge that names the purged id folds to no edge, as an edge to an id that
  never existed does, and a mention in text stays text. Events whose subject
  is purged and which arrived after the purge are counted, and `/integrity`
  lists them with their writer, so a writer whose comment was dropped
  learns of it on its next command.
- **The rewrite.** Each checkout re-seals its own segments without the
  purged ticket's events, including those in the dead writers' and inbox
  trees it anchors, and pushes the new commit as a compare-and-swap against
  the one it replaced. A checkout writes only its own ref, so the purge
  completes as each writer next syncs, and the fold hides the events in the
  meantime. Once nothing reaches the objects, gc removes them: locally at
  the next gc, and on the remote only when its host collects garbage, as
  after any git force-push.

## Ref sweep

Writer refs are per checkout, and checkouts die. The store prunes refs whose
checkout is proven dead by the liveness evidence the claims derive
(`specifications/lanes-and-claims.md`); pruning trims the namespace, never
the events. Rejected: heartbeat signals for writer refs.

The sweep needs no shared ref and no exception to `INV:single-writer-ref`.
The sweeping checkout appends a sweep event to its own log and anchors the
dead writer's one tree from its own published commit, so the dead writer's
segments stay reachable from a live ref. The dead ref is then deleted,
locally and on the remote by a ref-deletion push, with a compare-and-swap
against the anchored commit. "Write" in the invariant's sense means
publishing to a ref; deleting a proven-dead, already-anchored ref is
namespace maintenance, not authorship.

The anchor tells stale from alive. A reappearing ref that carries no event
the anchored tree lacks is the old corpse and is deleted again; a ref that
carries events past the anchor proves its writer lives, and the receive
keeps it and folds the new events. A checkout whose own ref is absent, swept
locally under a shared common dir or deleted on the remote, keeps its log,
so its next sync publishes a tree holding everything the anchor holds and
more, which passes the liveness test. A false-positive death therefore costs
nothing durable: an idle falsely swept checkout stays swept, its events
anchored, until its next write, whose sync restores the ref. Two survivors
racing to sweep one corpse is harmless: sweep events are idempotent facts
the fold deduplicates, and each compare-and-swap delete has at most one
winner.

## Inbox refs

Remote filing is the ordinary write path from a machine without a store:
mint events, seal them into one segment, and publish a commit holding it to
a fresh, uniquely named ref under `refs/lit/inbox/`. An inbox ref is a
writer ref with one segment and no checkout behind it. Any clone's receive
verifies inbox refs, folds them in and anchors them.

Rejected: mutating the remote's store in place from a storeless machine,
which needs a partial fetch the git transport cannot promise and a full lit
peer wherever a bug is filed.

## Sync

**Out.** A write schedules a detached `lit2 admin sync`, which seals this
checkout's log into segments and publishes its one commit, replacing its
ref. Replacing a ref only you write cannot conflict
(`INV:single-writer-ref`).

**In.** The same background sync fetches peers' refs and verifies each
writer's published segments before accepting them
(`specifications/access.md`, "Verification"). A read starts one when the
last fetch is older than the debounce. Arrival needs no merge: new events
simply exist locally, and the next command's staleness check folds them.
Evaluate compares local refs to remote-tracking refs and reads the last push
outcome, and reports either in one advisory line on read and on write.

**Transport.** The background sync process runs the user's own `git` binary
for push and fetch, so the user's credential helpers, `~/.ssh/config` and
`insteadOf` apply. go-git, in process, handles lit's objects and refs and
does no network I/O.

**Adoption**, the first clone of an existing workspace, fetches
`refs/lit/log/*` and `refs/lit/inbox/*`. Inbox events fold exactly as an
ordinary receive folds them, so a workspace-founding adopter sees remotely
filed bugs with no established-clone prerequisite. Adoption verifies every
writer's segments from the enrollment forward (`specifications/access.md`,
"Enrollment and joining") and folds. Ids are random bits, so what the union
of two writers' events surfaces is semantic duplication, two issues
describing one bug, held as a fold advisory for an agent to settle by
closing one. The first-adopt verification budget is
`specifications/budgets.md`.

Rejected: a compare-and-swap retry loop on one shared spine (a single data
branch, fetch-rebase-push). It is safe, but it serializes all writers
through retry contention, keeps a merge and replay step alive, and imports
the remote's availability into the write path.

## Rank

Rank is on lanes, and rank events are relative intents: place lane X above
or below lane Y, or at the top or bottom ("Intent"). The fold applies them
in the total order, and supersession is scoped to what the order can
promise:

- **Causally ordered intents.** When the writer had folded the earlier
  intent when it minted the later one, the later strictly supersedes,
  guaranteed by the Lamport advance rule ("Event model"). This covers every
  intent made in view of the state it changes.
- **Concurrent intents** on one subject have no "later": the total order
  settles them as deterministic arbitration, which promises convergence, not
  recency.
- Concurrent intents commute only when their subject-and-anchor sets are
  disjoint; overlapping ones resolve by the total order, which is why the
  fold is never partitioned by subject (`specifications/fold.md`, "Order").
- An intent whose anchor no longer exists when it arrives places the subject
  at the bottom of the order, and `/integrity` names the intent.

The snapshot holds the folded lane order, so a new `rank.place` applies to a
known order.

## Cache

Reads come from a local materialized state under the common dir, keyed by
`(fold-version, ref heads, log lengths)` and rebuilt incrementally by
folding only events newer than its snapshot: the snapshot remembers, per
writer, the segment and offset it has consumed. It is scaffolding, never
structure (`INV:disposable-cache`): any process may delete it at any moment,
and the only cost is one refold. Correctness never depends on it. Every
command's staleness check reads the writer ref heads, which is cheap and
local, and folds the delta.

The cache needs no lock. A command rewrites the snapshot by atomic rename;
when two worktree processes race on it, a loser's work is discarded, and a
corrupt or torn snapshot fails its checksum and is deleted and refolded.

The fold version (`specifications/fold.md`) is part of the key, so a release
that bumps it misses every store's snapshot once. The first command in each
store refolds from genesis inside the cold budget and rewrites the snapshot.
Nothing runs at install time; the cost is paid per store, on first use,
once.

## Export and import

`lit2 admin export` writes every event as one JSON document naming its
schema version. `lit2 admin import` replays such a document into a store as
this checkout's writes, each carrying its source attribution and time as
provenance. A create carries its id, so every item keeps its id.
