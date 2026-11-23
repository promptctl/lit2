# Event store: the design

Answers [charter.md](charter.md); budget figures live in
[budgets.md](budgets.md).

## The shape in one paragraph

Every lit mutation is an immutable, signed-slot, content-addressed **event**
appended to the writing checkout's own git ref inside lit's own object
database under the git common dir. State is a deterministic
**fold** over the union of all writers' events, computed locally, cached
disposably, and identical on every machine that holds the same events. Sync is
`git fetch`/`git push` of per-writer refs against the code repo's ordinary
remote — appending to a ref only you write contends with nothing. Remote bug-filing is the same write path
executed from a machine without a clone: mint events, push them to a fresh
ref.

### Invariant registry

Cite these as `[INV:token]`; each is defined in its section.

| Token | One line |
|---|---|
| `INV:rewrite-is-an-event` | an event changes after it is written only by a rewrite event naming the change; a receiver accepts a published tree that lacks events it held only when rewrite events account for every one (§store) |
| `INV:deterministic-fold` | same event set ⇒ equal derived state on every machine, in any arrival order, from genesis or a snapshot; only a fold-version bump may change it |
| `INV:single-writer-ref` | each ref has exactly one writing checkout; all cross-writer coordination is read-side |
| `INV:preserve-unknown` | an event a reader cannot interpret is carried and surfaced, never discarded |
| `INV:disposable-cache` | derived local state may always be deleted and refolded; nothing behaves as if the cache were truth |
| `INV:opaque-envelope` | layers below the verb layer treat prose payloads as opaque bytes |
| `INV:config-versioned` | every event names the active workspace version its writer ran under (`config-ref`) and the policy version in force |
| `INV:wall-time-is-data` | every event carries a wall-clock time; durations read it, order never does |

## §events — the event model

An event is a small structured record: `(type, schema-version, subject,
payload, attribution, config-ref, policy-version, causal-position,
recorded-at, signature-slot)`. The signature covers a **header** — every field
but the payload, plus a salted hash committing to the payload — and never the
payload itself or any other event. So a rewrite that replaces or drops a
payload leaves the original author's signature valid: it still proves who made
which change where, and a verifier reports the payload as rewritten, pointing
at the rewrite event and its own signature. The salt travels with the payload
and is erased with it, so a low-entropy value cannot be recovered by guessing
its hash. Attribution is the `(checkout, principal, agent)` triple: the
checkout id, the fingerprint of the key that signed the event, and the
optional opaque agent label the invoking harness supplies. `config-ref` is the
git blob id of the active workspace version the writer ran under, and
`policy-version` the policy version in force at its fold
(`INV:config-versioned`). `causal-position` is a Lamport
timestamp with the real Lamport advance rule: a writer mints its next event
at `1 + max(its own last counter, the highest causal-position among all events
it has folded)`. Observing an event therefore always places your next event
after it — an intent minted *because* you saw something supersedes that
something on every machine. A plain per-writer sequence number would not have
this property: a deliberate override could sort before the event it
overrides. Within a command's batched commit the counter still
advances **per event** — a command minting k events occupies k strictly
increasing positions — so the total order never leans on payload iteration
order. Combined with the writer id, then the event id, as tiebreaks, the
position gives the fold a total order without wall clocks; the event id is
needed because two commands of one checkout can fold one frontier and so
mint one `(writer, position)`. Every event is signed, from the first event
of every store, by its principal's key or a key the principal delegated to
the clone (the access-control design's Principals); the signature slot names its crypto suite, so a second suite can coexist with the
first without a schema change.

`recorded-at` is the writer's clock reading when it minted the event, stored
as read. It is data, not order (`INV:wall-time-is-data`). Durations read it:
how long a ticket has been in progress, how long ago it was last touched,
whether a claim has outlived `claim_ttl`. Which
event superseded which is the total order's question alone, a claim's latest
establishing event and a ticket's column among them. When a question asks
*when*, such as a claim's last refresh or a ticket's last touch, its answer is
the largest `recorded-at` among the events that qualify. The total order
cannot answer it, because between concurrent events it is a tiebreak, not a
sequence in time: a writer that worked offline through many events can rank a
three-day-old touch after one made today.

A writer's `recorded-at` is its own clock reading, never a time it folded from
another writer carried forward. If it were, one fast clock would hand its
future to every writer that folded its events, and every claim they refreshed
would read as fresh until real time caught up.

Skew still reaches every duration and can make one negative. Claim expiry
is a duration that decides who holds a lane, measured from the holder's newest
event against `claim_ttl`. A writer whose clock runs slow by more than
`claim_ttl` publishes events a reader already counts as expired, so a live lane
looks free; a writer whose clock runs fast keeps a dead lane held for as long
as its clock is ahead. §fold says how a reader
surfaces it.

Events carry **intent, not derived state** — the fold owns derivation, and an
event that embeds a derived value forks truth into two homes:

- WRONG: `{"type":"rank.set","issue":"x1","position":4}` — position 4 of
  *what list*? Two concurrent events like it cannot both be true.
- RIGHT: `{"type":"rank.place","issue":"x1","above":"y2"}` — an intent that
  stays meaningful whatever else happened concurrently.

**Identity.** Ids are minted with no coordination, so they must be
collision-proof by construction: an id, an item's or an element's such as a
comment's, is 128 random bits minted at its create and carried in the create
event. The fold reads it from there, so where the event lands in a log is no
part of identity, two offline checkouts cannot mint one id, and a create
replayed into another log keeps its id. An id is rendered by its **segment**,
a short form fixed at mint: the shortest prefix of the id that no id this
writer has folded shares, as wide as the namespace's fill needs, so a later
mint goes wider rather than making an earlier segment ambiguous. Two writers
offline can fix one segment for two items; after sync that segment resolves to
neither, the reader is shown both, and each item keeps its id. A child renders
as `<parent>.<segment>` from its parent link, which is not part of the id;
the project prefix and topic in the rendering, and how a rendering parses,
are [../architecture.md](../architecture.md) "Event and intent". A rendered form is display; the id is what agents store. The id
*policy* (opacity, format, what stays visible) is in the access-control
design's What the model had to give.

**Schema versioning.** Every shipped event shape stays readable while any
store may hold its version. Readers hold a chain of pure upcasters (v1→v2→…) applied at
fold time; a schema bump changes no bytes on disk. A binary built from a
working tree mints no event at a schema version no published release carries,
so a remote never holds events only a dev build
can read: raising the schema a remote holds is a release's act, and
`min-reader-version` below is how a workspace then gates writers too old to
read it. An upcaster stays while any store may hold its version, because
deleting it deletes that store's ability to read its own history.

**Unknown events** (`INV:preserve-unknown`). A reader meeting an event type or
version it cannot interpret carries it byte-for-byte and surfaces one warning
naming the version gap. It never drops, never guesses, and never treats
failure-to-read as absence:

- WRONG: skip the unknown event and fold the rest — the fold now silently
  disagrees with every up-to-date machine, and the divergence has no bad data
  anywhere to find.
- RIGHT: fold what is readable, report "3 events from a newer lit (min reader
  vX) not applied," and refuse mutations that would race the unread events'
  issues.

`min-reader-version` in the workspace file lets a workspace gate too-old
writers entirely.

**Codec.** The codec is invisible above the event package boundary, and its
framing must let the fold meet the refold budget in [budgets.md](budgets.md).

## §store — git as the substrate

Events live in lit's own git object database under
`$(git rev-parse --git-common-dir)`, never the code repo's, so all worktrees
of a clone see one store. Each checkout publishes exactly one ref —
`refs/lit/log/<checkout-id>` — and no checkout ever writes another's ref
(`INV:single-writer-ref`). The checkout-id is a **full-entropy token** (128
bits, minted once into the checkout's private git dir); the short forms
surfaces render are display aliases, never identity. Everything downstream
leans on this uniqueness — ref names, the fold's writer-id tiebreak — and at
full entropy it holds even between never-connected workspaces that later meet
on one remote. A command appends its events to the writer's own append-only
**log** under the common dir, one record per event, and touches nothing else
in the store; a crash mid-append loses at most the record being written. Sync
seals the log into **segment** blobs of bounded size and publishes one commit
per writer whose tree holds the writer's segments; the ref is replaced on each
push, never advanced, because only its writer updates it. The events are the
history and the ref has none, so the remote holds, per live writer, one
commit, one tree and a few segment blobs, and the number of events never
reaches git.

Most corrections are appends: a wrong status or a mistaken close is fixed by
the correcting event, as a ledger corrects an entry. When history itself should
change — content that must go, a mangled event, noise worth squashing, history
worth compacting — the tool is a **rewrite**. A rewrite is an event
appended to the rewriter's ref that names the events it drops, the payloads it
replaces, and the ranges it squashes. The fold applies it on every replica the
moment it arrives; each writer then re-seals its segments without the
rewritten material and replaces its ref, and a receiver accepts the new tree
only when the events it lacks are the ones rewrite events name. Surviving
events keep their bytes and their signatures (§events). Purge, below, is the
rewrite that drops one ticket's events; compaction is the rewrite that
squashes, keeping and dropping by address within windows bounded by an item's
create and its transitions, so the state an item was in when it moved stays as
it was judged at the write. History changes only through signed rewrite
events.

**Purge** removes one ticket the way a git history rewrite removes content,
in three parts.

- *The record.* A purge is an event like any other mutation, appended to the
  purging checkout's own ref: it names the ticket by its issue id and birth
  (`created_at`) and carries nothing else of the ticket. Nothing un-purges, and the purge event is what reaches
  other stores.
- *The fold.* The fold drops every event whose subject is a purged id,
  wherever it sits and whenever it was minted — before the purge, or after it
  by a writer that had not yet received it. The ticket is gone from derived
  state on every machine the purge event has reached, the moment it arrives,
  whatever that machine had done to it. Another ticket's events stay: an edge
  that names the purged id folds to no edge, as an edge to an id that never
  existed does, and a mention in text stays text.
- *The rewrite.* Each checkout re-seals its own segments without the purged
  ticket's events — including the dead writers' and inbox trees it anchors —
  and pushes the new commit as a compare-and-swap against the one it
  replaced. This is the rewrite mechanism above, applied to one ticket. A
  checkout writes only its own ref (`INV:single-writer-ref`), so the purge
  completes as each writer next syncs, and the fold hides the events in the
  meantime. Once nothing reaches the objects, gc
  removes them: locally at the next gc, and on the remote only when its host
  collects garbage, as after any git force-push.

The purged id is never reused: ids are random bits (§events), and a create
with the purged id never arrives again because the purge drops it.

**Ref hygiene.** Writer refs are per-checkout, and checkouts die; the store
prunes refs whose checkout is proven dead — pruning trims the *namespace*,
never the events. The sweep needs no shared ref and no exception to
`INV:single-writer-ref`: the sweeping checkout appends a sweep event to **its
own** log and anchors the dead writer's one tree from its own published
commit, so the dead writer's segments stay reachable from a live ref; the
dead ref is then deleted — locally *and* on the remote, via a ref-deletion
push — with a compare-and-swap against the anchored commit. "Write," in the
invariant's sense, means publishing to a ref, and no checkout ever does that
to another's; deleting a proven-dead, already-anchored ref is namespace
maintenance, not authorship. Fetch cannot resurrect a *stale* swept ref, and
the anchor is what discriminates stale from alive: a reappearing ref that
carries no event the anchored tree lacks is the old corpse and is re-deleted;
a ref that carries events **past** the anchor proves its writer lives, and
the receive keeps it and folds the new events. A checkout whose own ref is
absent — swept locally under a shared common dir, or deleted on the remote —
keeps its log, so its next sync publishes a tree holding everything the
anchor holds and more, which passes the liveness test.
So a false-positive death costs nothing durable: an idle falsely-swept
checkout stays swept (harmless — its events are anchored) until its next
write, whose sync durably restores the ref. Two survivors racing to sweep the
same corpse is harmless: sweep events are idempotent facts the fold
deduplicates, and each CAS delete has at most one winner — the loser no-ops.
The charter's constraint that lit leaves the code repository's git alone
applies here, and the budgets file carries the check. A developer's
clone and fetch ask for `refs/heads/*` and `refs/tags/*`, so lit's refs and
the objects behind them are never transferred; under protocol v2 the
advertisement is filtered by the prefixes asked for, so they are not listed
either, while a protocol v0 client is sent every ref on every fetch and pays in
proportion to the ref count, which the sweep keeps at live checkouts plus
unswept inbox refs.

**Remote filing** is the ordinary write path from a storeless machine: mint
events, seal them into one segment, publish a commit holding it to a fresh
uniquely-named ref under `refs/lit/inbox/`. Any clone's receive folds inbox
refs in and anchors them. An inbox ref is a writer ref with one segment and
no checkout behind it.

## §fold — one function owns derivation

`fold(events) → state`. Events are ordered by `(causal-position, writer-id, event-id)` —
a total order every machine computes identically — and applied by pure
per-type rules. Two properties make the contract
(`INV:deterministic-fold`):

1. **Total**: every event in the readable set is applied or explicitly
   surfaced (`INV:preserve-unknown`); there is no quiet skip.
2. **Deterministic**: no clock reads, no map-iteration order, no filesystem
   or environment input. Same events, same state, in any arrival order, from
   genesis or from a snapshot.

Determinism is tested as equality
between independent folds of one event set, through the state's own equality
and never through a pinned serialization, and the **fold version** — an
explicit constant — bumps with any rule change, invalidating snapshots
(§cache) so no machine mixes two folds' outputs.

**Time enters at the edges.** The fold reads each event's `recorded-at` as
input, like any payload field. A question whose
answer depends on the present, such as whether a ticket has gone untouched for
72 hours or whether a claim's window has lapsed, is answered after the fold: a
read-time evaluation takes the folded state and the reader's `now` as
arguments, computes the answer, and writes nothing. The fold keeps each
writer's largest `recorded-at`, and the same evaluation compares those to
`now`. A writer ahead of `now` by more than a tenth of the reader's freshness
window T is surfaced as an advisory naming that writer and the reader's own checkout, because a
fast writer and a slow reader look identical from here: on a slow machine
every active writer appears ahead, and the advisory is how that
machine learns its own clock is behind. The fold's output stays cacheable
because nothing in it depends on when it was computed.

**Writes are judged at the write.** The active workspace version
(§security-and-config) defines what a valid write is: levels, fields, columns
and their requirements. The requirement gate in the verbs judges every write
at write time, against the writer's own current fold and the active version it
ran under, and refuses there; the event records that version as `config-ref`
(`INV:config-versioned`).
The fold applies every verified event as written and settles the one class of
contradiction only concurrent offline machines can produce (two
individually-legal, jointly-contradictory structures) by the deterministic
rule for that structure, holding the contradiction as a value on the thing it
concerns plus a surfaced advisory.

**Concurrent prose rewrites** — title, description, agent prompt changed to
different text on both sides — are not settled by a rule. The fold
holds both events, renders the field as conflicted with both texts
addressable, and the resolution is simply another event from whoever judges
the merge (under access control, a keyholder).

## §rank — order as intents

Rank is the most contended object in the system — one shared total order the
whole fleet reorders constantly — and the hottest fold path (see budgets).
Rank is on lanes, and rank events are **relative
intents**: `place lane X above lane Y`, `place lane X at top/bottom`;
[../path-addressed-cli.md](../path-addressed-cli.md) states the rank rules. The fold
applies intents in the global total order,
and supersession is scoped to what the order can promise. For
**causally ordered** intents — the writer had folded the earlier intent when
it minted the later one — the later strictly supersedes, guaranteed by the
Lamport advance rule in §events; that is the "the later decision stands" a
human expects, and it covers every intent made in view of the state it
changes. For **genuinely concurrent** same-issue intents there is no "later":
the shared total order settles them as deterministic arbitration, and the
design claims convergence there, not recency. Concurrent intents **commute
only when their subject-and-anchor sets are disjoint**: `place X above Y`
concurrent with `place Y above X` has different subjects but contradictory
content, and it too resolves by the total order — for this reason the fold
may not be partitioned or parallelized by subject, which would break
`INV:deterministic-fold` on exactly the anchor-overlap case.

## §cache — scaffolding, never structure

Reads come from a local materialized state under the common dir, keyed by
`(fold-version, ref heads, log lengths)`, rebuilt incrementally by folding
only events newer than its snapshot: the snapshot remembers, per writer, the
segment and offset it has consumed. It is **scaffolding, never structure**
(`INV:disposable-cache`): any process may delete it at any moment and the only
cost is one refold. Correctness never depends on it; every command's staleness
check reads the writer refs' heads (cheap, local) and folds the delta.

Two worktree processes may race on the cache file, and the cache needs no
lock: a loser's work is discarded, and a corrupt or torn cache fails its
checksum and is deleted and refolded.

## §sync — refs out, refs in

**Out:** a write schedules a detached `lit2 admin sync`, which seals this
checkout's log into segments and publishes its one commit, replacing its ref.
Replacing a ref only you write cannot conflict (`INV:single-writer-ref`); a
receiver verifies the published segments before accepting them (§store,
§security-and-config).

**In:** the same background sync fetches peers' refs, verifies them, and a
read starts one when the last fetch is older than the debounce. Arrival needs
no merge — new events simply exist locally, and the next command's staleness
check folds them (`INV:deterministic-fold` makes arrival order irrelevant).
The read-time evaluation compares local refs to remote-tracking refs and
reads the last push outcome, and reports it in one advisory line on read and
write.

**Adoption** (first clone of an existing workspace): fetch `refs/lit/log/*`
**and** `refs/lit/inbox/*` — inbox events fold exactly as an ordinary receive
would fold them, so a workspace-founding adopter sees remotely-filed bugs
with no established-clone prerequisite — verify every writer's segments from
the enrollment forward (§security-and-config), fold; the access-control
design's Enforcement says which enrollment that is. Ids are random bits
(§events), so what the
union of two writers' events surfaces is *semantic* duplication — two issues
describing one bug — held as a fold advisory for an agent to settle by closing
one. Adoption verifies the full history: every receiver runs the
verification rule over a writer's published segments before accepting them,
and the verification-cost budget in budgets.md is what that must meet.

## §security-and-config — the slots the future stands on

The access-control design ([../access-control/design.md](../access-control/design.md))
is the home for the layers themselves.

- **Signature slot.** Every event carries one (§events), signed from the
  first event of every store, and every verifier checks it as a fold rule
  and at receive. A rewrite re-seals segments, never events, and each
  signature covers its event's header alone, so every surviving signature
  stays original.
- **Envelopes.** Prose payload fields are opaque byte strings end to end
  (`INV:opaque-envelope`). The fold, cache, codec, and sync never inspect
  prose, so tiers change what the verb layer does, not what the store does.
- **Policy as events.** The policy — the member list with roles and wrapped
  tier keys, the role definitions, and a monotone version — lives in the
  store as events, signed like any events; the access-control design
  specifies how a version activates.
- **The workspace model.** The version in force is the last one applied
  into the log by `lit2 config commit`, an event carrying the version's bytes
  (the workspace-schema design). Every event names the active version its
  writer ran under (`config-ref`) and the policy version in force
  (`INV:config-versioned`). `lit2 admin init` writes the signed enrollment
  first and then applies the default profile as version one, because every
  event must be signed by an enrolled principal.
- **Revocation.** For each event in total order the fold checks the
  signature and the capability against the policy in force at the event's
  causal position, as the access-control design's Enforcement specifies.
- **Erasure has two reaches.** A rewrite (§store) removes
  content from every replica that syncs. Copies that never sync again — a
  stale clone, a host that has not collected garbage, a backup — are reached
  only if the content was ciphertext whose key can be shredded. The access-control design owns the key machinery.

YAGNI deserves its answer here, because every slot above will attract it:
*"nothing verifies signatures yet — ship without the slot."* YAGNI is right
about features: unused surface is carrying cost, and the 80% feature line in
the charter exists for exactly that reason. These are not features; they are
the primitive layer's headroom (the 120% side of the same rule), and their
absence is not a smaller system but a fleet-wide schema migration standing
between the system and its own roadmap. The slots ship inert; the features
wait their turn.

## Rejected alternatives

- **CAS retry loop on one shared spine** (single data branch, fetch-rebase-
  push): safe but serializes all writers through retry contention, keeps a
  merge/replay step alive, and re-imports the remote's availability into the
  write path.
- **Mutating the remote's store in place to file from a storeless machine**:
  requires a partial fetch the git transport can't promise, and a full lit
  peer wherever a bug is filed. The inbox ref subsumes the use case.
- **Compaction by truncation**, dropping old events with no rewrite event
  naming them: a replica could not tell it from corruption (§store), and
  the survivors' signatures would prove nothing about what went; compaction is
  a rewrite that squashes (§store), and snapshots accelerate, they never
  replace.
- **Heartbeat signals for writer refs**: ref sweeps rely on the dead-checkout
  evidence the claims design already derives.
