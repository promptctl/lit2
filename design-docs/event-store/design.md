# Event store: the design

Status: draft, first pass. Answers [charter.md](charter.md); budget figures
live in [budgets.md](budgets.md) and prose here cites them without restating
them. Written 2026-08-25. Sections marked **OPEN** are decisions a later
session must close; everything else is a position this draft commits to.

Every system section carries a status line: `destination` (designed, not shipped),
`built (unreleased)` (merged to `master`, carried by no tag yet),
`built (vX.Y.Z)` (describes shipped reality as of that release), or
`superseded → §anchor`. A version appears here only once a tag actually carries
it: naming the release a state is *expected* to ship in makes this a second map
of a number [`CHANGELOG.md`](../../CHANGELOG.md) and the tags already own, and
it is wrong the moment the release is cut as a different version.
`grep 'status: destination'` lists the unbuilt frontier. Flipping a section's
status is part of closing the work that ships it, not a separate act of
documentation.

A section that describes several states — §migration is the only one — carries
its status per state in its own table, and keeps the section-level line at
`destination` until the last of them is built. The frontier grep is the reason:
a section holding four unshipped states must appear in it, and one holding a
shipped state must still say so somewhere a reader will look.

## The shape in one paragraph

Every lit mutation is an immutable, signed-slot, content-addressed **event**
appended to the writing checkout's own git ref inside lit's own object
database under the git common dir. Nothing is ever modified or merged: state is a deterministic
**fold** over the union of all writers' events, computed locally, cached
disposably, and identical on every machine that holds the same events. Sync is
`git fetch`/`git push` of per-writer refs against the code repo's ordinary
remote — appending to a ref only you write contends with nothing, so
concurrency has nothing to manage. Remote bug-filing is the same write path
executed from a machine without a clone: mint events, push them to a fresh
ref. The apparatus the current store needs to manage shared mutable state —
one-writer engines, flock choreography, field-aware reconcile, inline receive
— has nothing to be about and does not exist here.

### Invariant registry

Tokens are stable; cite them from code comments, tickets, and reviews as
`[INV:token]`. Each is defined once, in its section.

| Token | One line |
|---|---|
| `INV:rewrite-is-an-event` | an event changes after it is written only by a rewrite event naming the change; a receiver accepts a published tree that lacks events it held only when rewrite events account for every one (§store) |
| `INV:deterministic-fold` | same event set ⇒ equal derived state on every machine, in any arrival order, from genesis or a snapshot; only a fold-version bump may change it |
| `INV:single-writer-ref` | each ref has exactly one writing checkout; all cross-writer coordination is read-side |
| `INV:preserve-unknown` | an event a reader cannot interpret is carried and surfaced, never discarded |
| `INV:no-foreground-network` | no lit command's foreground path waits on a network operation |
| `INV:disposable-cache` | derived local state may always be deleted and refolded; nothing behaves as if the cache were truth |
| `INV:opaque-envelope` | layers below the verb layer treat prose payloads as opaque bytes |
| `INV:config-versioned` | every event names the active workspace version its writer ran under (`config-ref`) and the policy version in force; both are provenance, the fold reads neither |
| `INV:wall-time-is-data` | every event carries a wall-clock time; durations read it, order never does |

## §events — the event model
status: destination

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
`policy-version` the policy version in force at its fold; both are provenance
(`INV:config-versioned`). `causal-position` is a Lamport
timestamp with the real Lamport advance rule, which is load-bearing and stated
here so no implementation ships the cheap half: a writer mints its next event
at `1 + max(its own last counter, the highest causal-position among all events
it has folded)`. Observing an event therefore always places your next event
after it — an intent minted *because* you saw something supersedes that
something on every machine. A plain per-writer sequence number would not have
this property, and the failure it produces is §fold's no-crime-scene kind: a
deliberate override sorting before the event it overrides, discarded
identically everywhere. Within a command's batched commit the counter still
advances **per event** — a command minting k events occupies k strictly
increasing positions — so the total order never leans on payload iteration
order. Combined with the writer id, then the event id, as tiebreaks, the
position gives the fold a total order without wall clocks; the event id is
needed because two commands of one checkout can fold one frontier and so
mint one `(writer, position)`. Every event is signed by its principal, the
key `lit2 admin init` mints, from the first event of every store; the
signature slot names its crypto suite, so a second suite can coexist with the
first without a schema change.

`recorded-at` is the writer's clock reading when it minted the event, stored
as read. It is data, not order (`INV:wall-time-is-data`). Durations read it:
how long a ticket has been in progress, how long ago it was last touched,
whether a claim has outlived `claim_ttl`. Order never reads it. Which
event superseded which is the total order's question alone: every ordering the
current store takes from stamps, a claim's latest establishing event and a
ticket's phase among them, comes from the total order once the fold owns it.
When a question asks *when*, such as a claim's last refresh or a ticket's last
touch, its answer is the largest `recorded-at` among the events that qualify.
The total order cannot answer it, because between concurrent events it is a
tiebreak, not a sequence in time: a writer that worked offline through many
events can rank a three-day-old touch after one made today. Backfilled events
keep the times the current store stamped, which sit past their writer's clock
wherever it carried a lane's history forward; while such a time runs ahead of
real time, its events read as fresh and its writer draws the advisory below.

The current store stamps each write just past its lane's history, because
there stamp order is the order. The total order removes that reason, so a
writer does not carry folded times forward. If it did, one fast clock would
hand its future to every writer that folded its events, and every claim they
refreshed would read as fresh until real time caught up.

Skew still reaches every duration and can make one negative. Claim expiry
is a duration that decides who holds a lane, measured from the holder's newest
event against `claim_ttl`. A writer whose clock runs slow by more than
`claim_ttl` publishes events a reader already counts as expired, so a live lane
looks free; a writer whose clock runs fast keeps a dead lane held for as long
as its clock is ahead. Skew cannot reorder events. §fold says how a reader
surfaces it.

Events carry **intent, not derived state** — the fold owns derivation, and an
event that embeds a derived value forks truth into two homes:

- WRONG: `{"type":"rank.set","issue":"x1","position":4}` — position 4 of
  *what list*? The value is a fold output smuggled into an input; two
  concurrent events like it cannot both be true, and the fold is forced to
  arbitrate nonsense.
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
as `<parent>.<segment>` from its parent link, and the project prefix is a
label the active workspace version supplies at render; neither is part of the
id. Nothing numbers children: a rendered form is display, the id is what
agents store. This section is the minting mechanism's **home** for every id;
the access-control design's "required lit redesigns" §1 owns the id *policy*
(opacity, format, what stays visible) and defers the mechanism here.

**Schema versioning.** Events are immortal, so every shape ever shipped stays
readable forever. Readers hold a chain of pure upcasters (v1→v2→…) applied at
fold time; a schema bump changes no bytes on disk. Know what an
upcaster is before writing one: a frozen historical artifact, kept and tested
forever — the same class of object as `schema_reconcile.go`, whose deletion
once bricked every pre-v1 workspace (the P000 incident). The temptation will
arrive as hygiene: *"this upcaster handles a version nothing produces anymore
— dead code."* It is not dead; its producers are merely finished. Somewhere a
store still holds their events, and deleting the upcaster deletes that store's
ability to read its own history.

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
writers entirely, so a fleet is never half-blind for long.

**Codec — OPEN.** The framing must let the fold meet the refold budget in
[budgets.md](budgets.md) (a compact self-describing binary encoding is the
lean; the harness decides). The codec choice is invisible above the event
package boundary.

## §store — git as the substrate
status: destination

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
worth compacting — the tool is a **rewrite** (charter #7). A rewrite is an event
appended to the rewriter's ref that names the events it drops, the payloads it
replaces, and the ranges it squashes. The fold applies it on every replica the
moment it arrives; each writer then re-seals its segments without the
rewritten material and replaces its ref, and a receiver accepts the new tree
only when the events it lacks are the ones rewrite events name. Surviving
events keep their bytes and their signatures (§events). Purge, below, is the
rewrite that drops one ticket's events; compaction is the rewrite that
squashes, keeping and dropping by address within windows bounded by an item's
create and its transitions, so the state an item was in when it moved stays as
the enforcer judged it. Nothing else removes anything.

**Purge** removes one ticket the way a git history rewrite removes content,
in three parts.

- *The record.* A purge is an event like any other mutation, appended to the
  purging checkout's own ref: it names the ticket by its issue id and birth
  (`created_at`) and carries nothing else of the ticket. Nothing un-purges, and the purge event is what reaches every
  other store.
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
  replaced. This is the rewrite mechanism of §store applied to one ticket, and
  it is checkable the same way: a receiver accepts the new tree only when
  every event it lacks belongs to an id a purge event names. Surviving events
  keep their bytes and their per-event signatures; only the segments around
  them change. A checkout writes only its own ref (`INV:single-writer-ref`),
  so the purge completes as each writer next syncs, and the fold hides the
  events everywhere in the meantime. Once nothing reaches the objects, gc
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
anchor holds and more, which passes the liveness test **by construction**.
So a false-positive death costs nothing durable: an idle falsely-swept
checkout stays swept (harmless — its events are anchored) until its next
write, whose sync durably restores the ref. Two survivors racing to sweep the
same corpse is harmless: sweep events are idempotent facts the fold
deduplicates, and each CAS delete has at most one winner — the loser no-ops.
lit may not change the behavior, output, or speed of any git operation the
developer runs (charter #10); the budgets file carries the check.

**Remote filing** is the ordinary write path from a storeless machine: mint
events, seal them into one segment, publish a commit holding it to a fresh
uniquely-named ref under `refs/lit/inbox/`. Any clone's receive folds inbox
refs in and anchors them. No special case: an inbox ref is a writer ref with
one segment and no checkout behind it.

## §fold — one function owns derivation
status: destination

`fold(events) → state`. Events are ordered by `(causal-position, writer-id, event-id)` —
a total order every machine computes identically — and applied by pure
per-type rules. Two properties are the whole contract
(`INV:deterministic-fold`):

1. **Total**: every event in the readable set is applied or explicitly
   surfaced (`INV:preserve-unknown`); there is no quiet skip.
2. **Deterministic**: no clock reads, no map-iteration order, no filesystem
   or environment input. Same events, same state, in any arrival order, from
   genesis or from a snapshot.

The stakes are worth stating plainly: a nondeterministic fold is the one bug
in this design with no crime scene. Two machines hold identical events,
compute different backlogs, and every individual read looks healthy — the
divergence surfaces weeks later as two agents confidently working two
realities. Determinism is enforced, not hoped: it is tested as equality
between independent folds of one event set, through the state's own equality
and never through a pinned serialization, and the **fold version** — an
explicit constant — bumps with any rule change, invalidating snapshots
(§cache) so no machine mixes two folds' outputs.

**Time enters at the edges.** The fold reads each event's `recorded-at` as
input, like any payload field, and never reads the clock. A question whose
answer depends on the present, such as whether a ticket has gone untouched for
72 hours or whether a claim's window has lapsed, is answered after the fold: a
read-time evaluation takes the folded state and the reader's `now` as
arguments, computes the answer, and writes nothing. The fold keeps each
writer's largest `recorded-at`, and the same evaluation compares those to
`now`. A writer ahead of `now` by more than a tenth of the reader's freshness
window T is surfaced as an advisory naming that writer and the reader's own checkout, because a
fast writer and a slow reader look identical from here: on a slow machine
every active writer appears ahead, and the advisory is the one place that
machine learns its own clock is behind. The fold's output stays cacheable
because nothing in it depends on when it was computed.

**The fold reads no configuration.** The active workspace version
(§security-and-config) defines what a valid write is: levels, fields, columns
and their requirements. One enforcer judges every write at write time, against
the writer's own current fold and the active version it ran under, and refuses
there; the event records that version as `config-ref` (`INV:config-versioned`).
The fold applies every verified event as written and settles the one class of
contradiction only concurrent offline machines can produce (two
individually-legal, jointly-contradictory structures) by the deterministic
rule for that structure, holding the contradiction as a value on the thing it
concerns plus a surfaced advisory. Judging state against the model is the
read-time evaluation's job, under the active version the reader ran under:
that is where a column or field the version no longer declares is reported.

**Concurrent prose rewrites** — title, description, agent prompt changed to
different text on both sides — remain the one class no rule settles. The fold
holds both events, renders the field as conflicted with both texts
addressable, and the resolution is simply another event from whoever judges
the merge (under access control, a keyholder). Nothing blocks; the store never
has a "diverged" state, only a field with two live candidates.

## §rank — order as intents
status: destination

Rank is the most contended object in the system — one shared total order the
whole fleet reorders constantly — and the hottest fold path (see budgets).
Rank is on lanes and on nothing else, and rank events are **relative
intents**: `place lane X above lane Y`, `place lane X at top/bottom`. The fold
applies intents in the global total order,
and supersession is scoped honestly to what the order can promise. For
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
`INV:deterministic-fold` on exactly the anchor-overlap case. An intent whose
anchor no longer exists when it arrives places the subject at the bottom and
is named on `/integrity`; a new lane ranks last.

An intent survives interleaving in a way a position never can: "above Y" stays
above Y however the list shuffled around them, where a stored position 4 is a
lie the moment anyone else moves. (This is §events' WRONG/RIGHT pair operating
at scale — rank is why that rule exists.) The budget in budgets.md is the
constraint the rank fold must meet from a snapshot as from genesis.

## §cache — scaffolding, never structure
status: destination

Reads come from a local materialized state under the common dir, keyed by
`(fold-version, ref heads, log lengths)`, rebuilt incrementally by folding
only events newer than its snapshot: the snapshot remembers, per writer, the
segment and offset it has consumed. It is **scaffolding, never structure**
(`INV:disposable-cache`): any process may delete it at any moment and the only
cost is one refold. Correctness never depends on it; every command's staleness
check reads the writer refs' heads (cheap, local) and folds the delta.

This is where concurrency tries to reenter the design. Two worktree processes
will race on the cache file, and the temptation will be reflexive: *"I'll add
a small flock around the cache write."* Stop — that lock is the first cell of
the disease this design exists to cure. The cache needs no winner: writes are
atomic-rename, a loser's work is discarded, a corrupt or torn cache fails its
checksum and is deleted and refolded. Losing a race costs a refold; adding a
lock costs the design.

"Premature optimization" does not apply to snapshots, and the objection should
be met on its own turf: the proverb correctly bans speculative machinery for
unmeasured problems. The refold ceiling in budgets.md is not speculative — it
descends directly from the charter's hard constraint 3, and a fold that only
meets it warm is a fold that misses it on every fold-version bump. Snapshots
are load-bearing from the first shipped version.

## §sync — refs out, refs in, nothing in front
status: destination

**The foreground never waits on a wire** (`INV:no-foreground-network`). Push
mirrors and receive fetches run in background processes; a command reads and
writes local objects only. The temptation is always modest: *"one quick fetch
inline here — it's fast and the data's fresher."* That exact reasoning, made
policy, is how `lit backlog` came to overshoot the per-command directive by an
order of magnitude under the current store (budgets.md's pre-#414 baseline
row; fixed only by PR #414 after the fact). The current architecture had an
excuse: embedded Dolt permits one read-write engine, so background sync would
break foreground commands, and inline was the least-bad seat for the fetch.
This design removes the excuse — appends and folds cannot conflict with a
background fetch — so nothing earns a seat on the foreground path again.

**Out:** a write schedules a detached `lit2 admin sync`, which seals this
checkout's log into segments and publishes its one commit, replacing its ref.
Replacing a ref only you write cannot conflict (`INV:single-writer-ref`); a
receiver verifies the published segments before accepting them (§store,
§security-and-config).

**In:** the same background sync fetches peers' refs, verifies them, and a
read starts one when the last fetch is older than the debounce. Arrival needs
no merge — new events simply exist locally, and the next command's staleness
check folds them (`INV:deterministic-fold` makes arrival order irrelevant).
There is no "diverged" state to report: the read-time evaluation compares
local refs to remote-tracking refs and reads the last push outcome, and says
so in one advisory line on read and write.

**Adoption** (first clone of an existing workspace): fetch `refs/lit/log/*`
**and** `refs/lit/inbox/*` — inbox events fold exactly as an ordinary receive
would fold them, so a workspace-founding adopter sees remotely-filed bugs
with no established-clone prerequisite — verify every writer's segments from
the enrollment forward (§security-and-config), fold. The adopting checkout
adopts the enrollment it finds and pushes a signed membership request. Two
workspaces independently initialized against one remote are two enrollments
meeting: a contradiction the fold reports, and the later one in total order
yields. Identity cannot collide (§events: ids are random bits), so what the
union of two writers' events surfaces is *semantic* duplication — two issues
describing one bug — held as a fold advisory for an agent to settle by closing
one. Adoption verifies the full history: every receiver runs the
verification rule over a writer's published segments before accepting them,
and the verification-cost budget in budgets.md is what that must meet.

## §security-and-config — the slots the future stands on
status: destination

This section binds the charter's birth requirements to their mechanism; the
access-control design ([../access-control/design.md](../access-control/design.md))
remains the normative home for the layers themselves.

- **Signature slot.** Every event carries one (§events), signed by its
  principal from the first event of every store, and every verifier checks it
  as a fold rule and at receive. The replayed-commit re-signing problem the
  access-control draft holds OPEN dissolves here: a rewrite re-seals
  segments, never events, and each signature covers its event's header alone,
  so every surviving signature is original forever.
- **Envelopes.** Prose payload fields are opaque byte strings end to end
  (`INV:opaque-envelope`), encrypted under the workspace's default tier from
  the first event; a project that wants its backlog world-readable sets the
  default tier to plaintext. The fold, cache, codec, and sync never inspect
  prose, so tiers change what the verb layer does, not what the store does.
- **Policy as events.** The policy — the member list with roles and wrapped
  tier keys, the role definitions, and a monotone version — lives in the
  store as events, signed like any events. Threshold operations are
  **quorum-by-events**, not multi-signature events: `policy.propose` carries
  the new document, each co-signer independently appends a `policy.approve`
  citing its hash, and a version activates at the causal position where the
  approvals the previous version requires are all present. A change that
  touches the owner set or widens any role's capabilities requires the owner
  threshold, one while there is one owner and a majority once there are more,
  with the owner being removed excluded from the quorum that removes them;
  every other change needs one `member.admin` or `policy.admin` signature,
  and the verifier decides which case applies by diffing the granted surface.
  Per-writer refs make this the only natural shape — co-signers cannot share
  a commit — and it is why one signature slot per event remains sufficient as
  a birth requirement. The first policy is the enrollment, the first event of
  a workspace, signed by its founding owner; verification seeds there.
- **The workspace model.** Levels, fields, columns and their requirements,
  labels, link types, `claim_ttl` and `min-reader-version` are
  `.lit/workspace.toml`, a committed file in the code repository as the
  intent; the version in force is the last one applied into the log by `lit2
  config commit`, an event carrying the version's bytes and its blob id and
  naming the version it replaces. `lit2 admin init` applies the default
  profile as version one. Every event names the active version its writer ran
  under (`config-ref`) and the policy version in force (`INV:config-versioned`),
  as provenance: the enforcer judged the write under them at write time, and
  the fold reads neither.
- **Revocation.** For each event in total order the fold checks the signature
  against its principal's key and the intent against the policy in force at
  the event's causal position; an event that fails is carried, counted on
  `/integrity` with its writer, and never applied, and an event whose cited
  policy version is lower than its writer's previous one fails the same way,
  so a position cannot be backdated past a revocation. The residual — a
  revoked principal that stays offline from its revocation can mint events
  whose positions sort before it — is applied as any offline work made before
  the news arrived is, and reported with its `recorded-at` so an owner can
  judge and rewrite. The rule is deterministic: nothing in it is keyed to what
  a receiver happens to know.
- **Erasure has two reaches.** A rewrite (charter #7, §store) removes
  content from every replica that syncs. Copies that never sync again — a
  stale clone, a host that has not collected garbage, a backup — are reached
  only if the content was ciphertext whose key can be shredded, which keeps
  the read layer's encrypted-by-default posture load-bearing. The access-control design owns the key machinery.

YAGNI deserves its answer here, because every slot above will attract it:
*"nothing verifies signatures yet — ship without the slot."* YAGNI is right
about features: unused surface is carrying cost, and the 80% feature line in
the charter exists for exactly that reason. These are not features; they are
the primitive layer's headroom (the 120% side of the same rule), and their
absence is not a smaller system but a fleet-wide schema migration standing
between the system and its own roadmap. The slots ship inert; the features
wait their turn.

## §migration — five states, four gates
status: destination

Decisions of 2026-10-28 are recorded in this section: the read-source rule,
event-only facts, the shortened S1 proof period, retirement by no-op, and the
freeze on Dolt-shaped additions.

The store advances through five architectural states. Each gate is a
condition, not a date; a state whose gate hasn't passed is where the system
rests. The strangler-fig discipline: the Dolt store is the oracle until the
gates prove the fold against it, on real fleet data, at 10x.

| State | The system is | Gate to advance | Status |
|---|---|---|---|
| S0 seam | CLI depends on a storage interface; Dolt implements it, and a second engine proves it is one | interface carved; behavior unchanged | built (v0.9.0) |
| S1 shadow | every mutation dual-writes; differential oracle diffs fold vs Dolt after each command | oracle diff empty over the proof period below; budgets pass at 10x | destination |
| S2 read-flip | reads serve from the fold behind a flag; Dolt still authoritative for writes | flag default-on with no regressions; oracle still clean; budgets re-green at 10x | destination |
| S3 write-flip | events are truth; sync is git refs; Dolt shadows as rollback | rollback window expires quiet; budgets re-green at 10x | destination |
| S4 exit | Dolt, the vendored driver, reconcile, engine-serialization, mirror-flock machinery are **deleted**; the oracle retires with Dolt | deletions merged; budgets green as CI regression checks; docs' statuses flipped | destination |

This table's Status column is load-bearing beyond this document: README.md and
docs/architecture.md carry user-facing claims that Dolt is lit's storage —
true today, falsified at S3 — and `internal/docsclaims` registers each one
against this table. Flipping a state's status fails that gate test until those
docs are rewritten, so the doc-update duty rides the same flip that closes the
shipping work.

S0 shipped as four things, and the shape matters to every state after it.
`internal/storage` is the contract, stated in the model's vocabulary with no
engine in any signature. Beside it sit seven **capabilities** — sync,
reconcile, checkpoints, repair, schema migration, import, and raw test access —
each an interface an engine may decline, asked for by a discovery call that
returns the interface or a typed refusal naming the engine. The split rule is
that two operations share a capability only when no engine could plausibly
offer one without the other, which is why reconcile is not part of sync: this
design's §sync says arrival needs no merge, so the event store will offer sync
and decline reconcile, and it must be able to say so rather than stub a
reconciliation it did not perform. What each state after S0 attaches to is
therefore specific: S1's dual-write is a decorator installed at the engine
factory, the one place a store is opened; the oracle diffs through the
contract's whole-state export, which is core rather than a capability precisely
so that two engines are comparable; and S4 deletes the capabilities the event
store never offered.

The **conformance suite** is what makes the contract a boundary rather than a
description of Dolt. An interface pins shapes, and shapes are not behavior; the
suite is a set of statements about behavior, parameterized over "give me a
fresh engine," and an engine that satisfies the interface but fails the suite
does not implement the contract. Both engines run it. Its second job starts at
S1: the suite is where a behavior the two engines disagree about gets settled
in writing, before the oracle meets the disagreement and reports it as
divergence.

Two such disagreements were settled at S0, and both are settled AGAINST the
better answer on purpose, because an engine that is right where the other is
arbitrary still reads as divergence. A listing's order ends in the issue id as
its last key, always, so no tie is left to an engine's incidental row order.
History is ordered by creation time and then by event id — not a happens-before
claim, since ids are random and a coarse clock ties routinely, but a rule both
engines can obey identically. Ordering that genuinely carries happens-before
arrives with §events' Lamport positions, which supersedes this rule for both
engines at once rather than refining it.

S1 is also where history backfills: the existing backlog replays into events
via export, with a provenance marker so pre-migration history is attributed
honestly rather than re-authored. A ticket purged before the backfill has no
rows in the export to replay, and the export's purge records backfill as purge
events, so a store that still held the ticket when it backfilled drops it as
the record arrives, the same as after the switch (§store).

Gate scope, stated once: the budgets gate binds from S1 onward — at S0 no
event store exists to measure, so S0's gate is behavioral identity alone.

The oracle's comparison ends at the write flip: once S3 flips writes, Dolt
receives none, so there is nothing left to diff — S2's gate is deliberately
the last to carry an oracle clause, and S3→S4 is judged on rollback-quiet
plus budgets alone.

The dangerous state is the comfortable one. S1–S2 *work* — everything green,
both stores humming — and the temptation will be to live there: *"the shadow's
been clean for weeks; flipping is risk with no feature."* A dual-truth store
is not a conservative resting point; it is two systems' carrying cost plus a
divergence risk neither system has alone, and every week in it teaches new
code to straddle both. The gates are the pace: pass a gate, advance; fail a
gate, fix what failed. Parking is the one move the charter forbids, and S4's
deletions are part of the campaign's definition of done, not a someday.

**One authoritative read source at every moment: Dolt until the read flip,
the fold after.** There are never two read paths live at once. Serving
event-only reads from the fold during S1 while Dolt serves everything else is
rejected: it is the straddling the previous paragraph warns against, arriving
early and by choice. The priority is getting to S2 quickly, so S1's proof
period is shortened to what we would trust behind a flag with Dolt still
owning writes and the oracle still running. The proof is shortened, not
skipped.

**Event-only facts, from S2 onward.** Event-only data shapes are allowed, with
no requirement to backport them to Dolt. Once reads come from the fold, any new
fact is an event kind plus a fold rule: no Dolt table, no migration. The config
stream (§security-and-config) already works this way; it is the rule, not an
exception. Because the fold must be readable first, this starts at S2, not S1.
Schema-bearing features wait for S2.

**Nothing Dolt-shaped is added before the flip, and nothing slated for
redesign is grown.** plan_reviews is the first case: findings, reasons and the
owner override are events, not columns.

**Features retire by no-op, never by dropping the table.** A retired command
refuses and names its replacement; it never exits zero having done nothing. One
code-side registry marks retired collections. Sync and reconcile keep carrying
them so older lits don't break; reads, the oracle and the backfill ignore
them. Purge still clears them. Retired history ends at S4 unless a feature
explicitly maps its old rows into events.

## Rejected alternatives

Recorded so they are not re-proposed from scratch; each was weighed in the
2026-08-25 design conversation.

- **CAS retry loop on one shared spine** (single data branch, fetch-rebase-
  push): safe but serializes all writers through retry contention, keeps a
  merge/replay step alive, and re-imports the remote's availability into the
  write path. Per-writer refs need no winner at all.
- **In-place remote mutation of the Dolt store**: requires partial chunk
  fetch the git transport can't promise, and a full lit+Dolt peer wherever a
  bug is filed. The inbox ref subsumes the use case.
- **Existence-hiding visibility** (per-ticket secrecy of *presence*): breaks
  rank, claims, and epic derivation wholesale; the access-control design's
  visible-skeleton rule is adopted unchanged.
- **Compaction by truncation**, dropping old events with no rewrite event
  naming them: a replica could not tell it from corruption (charter #7), and
  the survivors' signatures would prove nothing about what went; compaction is
  a rewrite that squashes (§store), and snapshots accelerate, they never
  replace.
- **Heartbeat/liveness signals for writer refs**: banned mechanism-wide by
  standing owner directive; ref hygiene keys off the dead-checkout evidence
  the claims design already derives.

## OPEN questions

1. Event codec (§events) — decided by the harness against the refold budget.
2. Snapshot sharing: purely local, or a shared snapshot ref to accelerate
   adoption — and what trust that implies, given that adoption verifies the
   full history (§sync).
3. Chunked read flipping (§migration), for whoever pulls the read-flip work
   to investigate. Not decided. The idea: S0 shipped `internal/storage` as a
   contract and the read flip routes reads through it to the fold; instead of
   one flag that flips every read, group the contract's read methods into four
   or five coarse chunks (roughly: issues and fields; labels, relations and
   parents; comments and history; rank; config), each with its own flag. A
   chunk flips when its reads route to the fold; the oracle keeps comparing
   every chunk against Dolt until S3; Dolt still owns writes, so a bad flip is
   a display bug with a one-flag rollback. It would let simple facts like
   labels and comments flip early while rank stays on Dolt until proven, and
   event-only features would wait for their own chunk rather than the whole
   fold. It costs two read sources live during the chunked period, the
   straddling §migration warns against; it only works if chunks are coarse and
   the period short, and a command spanning two chunks (`lit show` printing
   labels from one and history from another) forces those chunks to flip
   together. Unverified: whether the methods in
   `internal/storage/contract.go` partition cleanly, or whether reads cross
   boundaries (a listing filtered by label and ordered by rank, say). If they
   don't partition, it's one flip.
