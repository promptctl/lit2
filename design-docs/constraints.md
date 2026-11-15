# The constraints, mapped

What the carried documents require, where each requirement is written, what
it implies for lit2, what it buys, and what it costs. The sources are the
event-store charter (`event-store/charter.md`, hard constraints 1–10), the
access-control charter it inherits from (`access-control/charter.md`, hard
constraints 1–2), the heartbeat ban in `work-claims.md`, and the nine
invariants and the structural decisions of `event-store/design.md`.

The charter was written on 2026-08-25 to replace lit v1's Dolt store in
place. Several of its constraints are about that replacement, not about a
tracker; they are marked below. Where `architecture.md` has already departed
from a constraint, that is marked too.

The entries are grouped by what they govern: where the data lives, how it
is written, how it is ordered and derived, how it changes, how it performs,
and access control.

---

## A. Where the data lives

### A1. One repo. Tickets travel with the code repository.
*Access-control charter #1; event-store charter #1.*

- **Implies:** the store is inside the code repo's git common dir; there is
  no second repository to clone, no separate URL, no separate permissions.
- **Buys:** a clone of the code is a clone of the tracker. Permissions,
  backups and discovery are the code's. An agent in a checkout has its
  tickets with no setup.
- **Costs:** the tracker's data growth is the code repo's storage problem on
  the host. Every design choice below about refs and objects is constrained
  by not harming the code repo (A3).

### A2. No server. The remote is a dumb replicated store.
*Access-control charter #2; event-store charter #1.*

- **Implies:** every rule is enforced by every client; nothing at the remote
  validates, merges, orders or rejects. Access control, when it comes, is
  cryptographic (signatures, encryption), never a gate at the host.
- **Buys:** works on any git host, offline, with no deployment and no
  operator. Nothing to run, patch or pay for.
- **Costs:** no coordination before a write, ever. Two offline writers can
  both do a thing, and the design must make that harmless (C2, C3). Anything
  a server would do cheaply (sequence numbers, locks, a single current
  snapshot) has to be replaced by client-side construction.

### A3. lit adds nothing to the code repo's refs or objects, locally; on the remote its data lives under its own refs; no developer git operation changes in behavior, output or speed.
*Event-store charter #10 (owner amendment 2026-10-09); budgets row "change to any git operation the developer runs: none".*

- **Implies:** locally, lit keeps a git object database of its own under the
  common dir (design §store: "never the code repo's"). On the remote, the
  data sits under `refs/lit/*` in the same repository. The developer's
  `git status`, `git log --all`, `git gc`, `git fetch` must be
  indistinguishable from a repo without lit.
- **Buys:** lit cannot pollute the developer's history, break a `git gc`, or
  show up in `git log --all`. A developer who never runs lit never notices
  it.
- **Costs and what it forces:**
  - A second object database to open, write, pack and gc, with its own
    reachability. The workspace file blob is duplicated across the two.
  - The local half is clean. The remote half is where the tension sits:
    `refs/lit/*` are real refs on the shared remote. A developer's
    ordinary clone or fetch does not transfer them (git fetches only the
    refspecs it is asked for, `refs/heads/*` and `refs/tags/*` by default),
    and the objects behind them are not downloaded. But a ref advertisement
    under protocol v0 lists every ref on the remote on every fetch;
    protocol v2 (the default since git 2.26) filters by prefix and does not.
    So "no change in speed" for developers holds on modern clients and
    degrades on old ones, in proportion to the ref count.
  - The remote's object store grows with every lit object pushed and is
    collected only when the host collects. Nothing in the design bounds that
    growth except rewrites (D1) and host gc.
  - Some hosts restrict pushes to non-branch namespaces; the design has not
    verified GitHub, GitLab and plain ssh remotes all accept `refs/lit/*`.
- **Note:** the design's current layout puts a commit per command on each
  writer's ref (B3), so the objects on the remote are the whole command
  history of every writer, forever, and that history is unbounded.
  Constraint A3 does not require it; B3 does.

### A4. Per-writer refs: each checkout appends to exactly one ref, and no checkout writes another's.
*Design §store, `INV:single-writer-ref`; refs `refs/lit/log/<checkout-id>` and `refs/lit/inbox/*`.*

- **Implies:** the unit of sync is the writer, not the workspace. The
  remote holds one ref per live writing checkout plus one per unswept
  remote filing. Reading is a union over all refs. Dead checkouts' refs are
  pruned by the sweep (D3).
- **Buys:** a push that can never conflict, because the remote is only ever
  asked to fast-forward a ref one writer owns. No retry loop, no merge, no
  lock, no "diverged" state. The design's rejected-alternatives list names
  the alternative, one shared ref with a compare-and-swap retry, and
  rejects it for serializing all writers and putting the remote's
  availability back on the write path.
- **Costs:** the ref count on the remote is the number of checkouts that
  have ever written and not been swept, plus inbox refs. Worktrees multiply
  it. The budgets file asserts "refs present = live checkouts + unswept
  inbox refs", which depends on the sweep (D3) working and on inbox refs
  being swept promptly. The ref-advertisement cost in A3 scales with this
  number. Adoption of a workspace means fetching every one of them.

---

## B. How data is written

### B1. Writes are appends; every mutation is a content-addressed event.
*Charter #2; design §events.*

- **Implies:** nothing is edited in place. A correction is a new event.
  Every event has a hash that is its identity.
- **Buys:** no write ever contends with another write (together with A4);
  history is the audit log for free; undo is another event; replication is
  copying objects, which git does natively; content-addressing makes
  duplicates and tampering detectable.
- **Costs:** the event set only grows, so reading is a fold over everything
  (C1) and the fold's cost grows with history unless cached (E2) and
  compacted (D1). Removing anything is a rewrite (D1), never a delete.

### B2. The event is `(type, schema-version, subject, payload, attribution, config-ref, causal-position, recorded-at, signature-slot)`, with the signature over the header and a salted hash of the payload, not the payload.
*Design §events; charter #6 birth requirements.*

- **Implies:** the shape is fixed at the first shipped event. The signature
  survives a payload rewrite (D1), so provenance outlives erasure.
- **Buys:** access control is a policy over this shape, never a schema
  change (F1). A rewrite
  never invalidates anyone's signature. `config-ref` lets two machines
  straddling a configuration change fold identically (C4).
- **Costs:** every event carries fields nothing reads yet. The salt and
  header hashing are machinery the first release does not exercise. The
  attribution triple is fixed by the access-control design before any
  access control exists.

### B3. A command batches all its events into one commit; the commit's first parent is the writer's previous head; the ref is fast-forward-only and linear in first-parent history.
*Design §store.*

- **Implies:** one commit, one tree and the event blobs per command, chained
  forever on the writer's ref. The chain is what the remote holds (A3, A4).
  A refold from genesis walks the chain.
- **Buys:** atomicity per command (one ref update); an incremental fold can
  take "events since my last head" as "commits since my last head"; the
  receiver's rewrite check (D1) can compare chains.
- **Costs:** this is the unbounded growth. Tens of thousands of commits and
  trees per writer over a project's life, all on the remote, all walked on a
  refold, all fetched at adoption. The sweep anchor (D3) adds merge parents
  to the chain. None of what B3 buys needs the chain: atomicity needs one
  ref update, not ancestry; "events since" is a set difference; the rewrite
  check is a set comparison. This is a design decision, not a charter
  constraint, and it fails requirement 4 (`requirements.md`). **Departure
  made:** `architecture.md` §3.15 replaces it with a local append-only log
  per writer, sealed into a few segment blobs at sync, published as one
  commit per writer whose ref is replaced on push. Nothing on the remote
  grows with commands and the number of events never reaches git.

### B4. No network on any foreground path.
*Charter #3 last sentence; design §sync, `INV:no-foreground-network`.*

- **Implies:** a command reads and writes local objects only; push and
  fetch happen in a detached background process on a debounce.
- **Buys:** every command's latency is local and bounded; offline is the
  normal case, not a mode; a dead remote costs one advisory line.
- **Costs:** a background process to spawn, deduplicate and observe; "is my
  view current" is always "as of the last fetch", and the design must
  surface that honestly (staleness advisories). The first clone's download is
  the one exception the charter allows.

### B5. No subprocesses; git objects and refs are read and written in process through a permissively licensed library.
*Charter #4.*

- **Implies:** no `git` binary. In Go that means go-git or a cgo binding to a
  permissive library.
- **Buys:** a single static binary; no dependence on the host's git version
  or configuration; no shell-out latency; no copyleft in the dependency tree.
- **Costs:** the library must do transport and authentication itself: ssh
  agents, https credential helpers, host tokens, the user's `.gitconfig`
  and `.ssh/config`. The `git` binary does all of this for free and
  go-git does some of it. This is the largest practical risk in the
  constraint set and is not addressed in the carried docs. It also means
  pack-file performance, shallow and partial fetch, and gc are whatever the
  library implements. **Departure made:** `architecture.md` decision G
  runs the user's git for push and fetch in the background process and
  keeps go-git for objects and refs.

---

## C. How state is ordered and derived

### C1. State is a deterministic pure function of the event set.
*Charter #2; design §fold, `INV:deterministic-fold`.*

- **Implies:** no clock, no map-iteration order, no filesystem input in the
  fold. A fold-version constant bumps with any rule change and invalidates
  caches.
- **Buys:** every machine with the same events shows the same backlog, which
  is the whole promise; arrival order is irrelevant, so sync needs no merge;
  the cache is disposable (E2) because it can always be recomputed.
- **Costs:** a nondeterministic fold is, in the design's words, "the one bug
  with no crime scene". Every derivation must be sorted into fold (pure) or
  tick (clocked), and some wanted behaviors split in two. Every rule change
  is a fold-version bump and a fleet-wide refold.
- **Departure already made:** the design pins `fold(corpus)` byte-for-byte
  with golden fixtures. `architecture.md` §3.3 tests determinism as state
  equality between independent folds instead.

### C2. Total order is `(causal-position, writer-id, event-id)`, with the real Lamport advance rule: next = 1 + max(own last, highest seen).
*Design §events.*

- **Implies:** every writer must fold before it mints, so that an intent made
  in view of a state sorts after that state. Order never reads a clock
  (`INV:wall-time-is-data`).
- **Buys:** "the later decision stands" for everything a writer did knowingly,
  on every machine; a total order with no coordination; no clock skew in
  ordering.
- **Costs:** genuinely concurrent events have no "later", only a tiebreak by
  writer id, and the design promises convergence there, not recency. A
  writer offline for three days sorts its old work after today's. Durations
  and "when" questions must come from `recorded-at` after the fold, and
  clock skew reaches every one of them.

### C3. Events carry intent, never derived state; rank is relative intents; cross-machine contradictions resolve at the fold by a deterministic rule plus an advisory, never by blocking; concurrent prose edits coexist until someone resolves them.
*Charter #5; design §events, §rank, §fold.*

- **Implies:** `place x above y`, not `x is at 4`; a transition carries
  from and to; two offline writers' contradictory structures both survive
  as values the next reader sees. The fold cannot be partitioned by subject,
  because rank anchors overlap.
- **Buys:** no write is ever refused for something another machine did;
  conflicts are data an agent can see and settle; local enforcement stays
  exactly as strict as a single machine's.
- **Costs:** every event type needs a stated rule for its concurrent case,
  and some of those rules are unfinished (design OPEN 2: rank under
  concurrent reparenting). Readers must be shown contradictions and handle
  them. The fold is sequential over the whole set.

### C4. Every event names the configuration version it was authored under, and validation judges it against that version.
*Charter #6; design `INV:config-versioned`.*

- **Implies:** the configuration is versioned and every version stays
  resolvable forever. The validation engine is generic over configuration,
  not hand-coded rules.
- **Buys:** two machines straddling a process change fold identically; the
  process can change under running work without a migration (scenarios 7
  and 8); the model is data.
- **Costs:** every cited version must travel with the events, so every
  version a writer ever ran under, committed or not, is kept forever. The
  revocation seam the design leaves OPEN (#6) follows
  from citing versions: a revoked writer can keep citing the version that
  allowed it.
- **Departure already made:** the charter and design say a "versioned,
  signed configuration stream" of events. `architecture.md` §3.15 makes it
  a committed file, `.lit/workspace.toml`, versioned by git; the model in
  force is the active version a person commits, or the shipped default
  before any is committed. **Departure made:** decision H. The fold reads no
  configuration; `config-ref` stays on the event as provenance, the git blob
  id of the active version its writer ran under; the one enforcer judges a
  write under that version at the write, and tick applies the reader's
  active version at the read. Nothing travels with the events but the id,
  and the stream and its quorum-by-events signing are gone.

### C5. Ids derive from `(writer, causal-position, content)`; they are opaque; sequential numbering is presentation only.
*Design §events "Issue identity".*

- **Buys:** no id collision without coordination; ids never reused, so a
  purge is final.
- **Costs:** no human-friendly sequential ids as identity; the carried design
  derives display ordinals that can renumber when a late sibling syncs in.
  `architecture.md` drops the ordinals entirely.

### C6. Unknown events are carried and surfaced, never dropped; schema upcasters are kept forever.
*Design §events, `INV:preserve-unknown`.*

- **Buys:** an old binary never silently diverges from a new one; every
  shape ever shipped stays readable; a workspace is never bricked by an
  upgrade.
- **Costs:** every historical upcaster is permanent code; an old reader must
  refuse writes to subjects it cannot fully read; `min-reader-version`
  is needed to stop a fleet being half-blind.

---

## D. How history changes

### D1. Rewriting history is first-class: drop, replace or squash by a rewrite event; every replica converges; an append-only audit trail is an opt-in policy layered on top.
*Charter #7 (owner amendments 2026-10-09 and 2026-11-03); design §store.*

- **Implies:** purge, erasure and compaction are one mechanism. A receiver
  accepts a non-fast-forward ref move only when rewrite events account for
  every difference. Each writer rebuilds its own ref without the rewritten
  material when it next runs.
- **Buys:** content can actually leave; stores can be compacted; one
  mechanism instead of three; the strict audit guarantee can be added by
  policy to a store that rewrites, where the reverse is impossible.
- **Costs:** the receiver's verification of non-fast-forward moves is real
  work and is on the critical path of every fetch; rewrites reach only
  replicas that sync, so true erasure of stale copies needs encryption (F2);
  the remote keeps unreachable objects until its host collects.

### D2. The cache is scaffolding: derived local state keyed by `(fold-version, ref heads)`, deletable at any moment, never locked, atomic-rename only.
*Charter #2 last sentence; design §cache, `INV:disposable-cache`.*

- **Buys:** no shared mutable state anywhere but the event log; no lock
  anywhere; a torn or stale cache costs one refold.
- **Costs:** the refold must meet the budget (E1) from a cold start, which
  drives the codec and snapshot design; two worktrees racing on
  the cache waste a fold.

### D3. Dead checkouts' refs are swept: a live checkout appends a sweep event with the dead head as a second parent, then deletes the dead ref locally and remotely by compare-and-swap.
*Design §store "Ref hygiene".*

- **Buys:** the ref count on the remote stays "live checkouts + unswept
  inbox refs" (A4) without any heartbeat (D4); a resurrected ref is
  distinguishable from a stale one by its position relative to the anchor.
- **Costs:** the most intricate mechanism in the design: merge parents on
  otherwise linear chains, a liveness proof that only the dead checkout's
  own machine can make instantly, remote ref deletion by CAS, and a rule for
  a swept checkout that wakes up. It exists entirely to bound A4's ref count
  under B3's chains. With one tree per writer (`architecture.md` §3.15)
  the anchor is one tree, not a chain, and most of the intricacy goes.

### D4. No heartbeats, no liveness probes, anywhere.
*Charter #8; `work-claims.md`.*

- **Buys:** no background chatter, no writes that carry no intent, no
  "last seen" state to replicate.
- **Costs:** a dead holder's claim ends only by age (`claim_ttl`) or by its
  own machine's local prune; a remote reader cannot tell dead from quiet
  before the window lapses.

---

## E. How it performs

### E1. Every command under one second at ten times current fleet scale, on both axes, with the figures in budgets.md.
*Charter #3; budgets.md command and storage tables.*

- **Implies:** warm cache under 300 ms, cold under 1 s, full refold from
  genesis under 1 s, staleness check under 20 ms, background work bounded
  per occasion and machine-wide.
- **Buys:** a tool an agent can call in a loop; the design decisions that
  follow (the snapshot, codec chosen by measurement, no inline fetch) are
  forced rather than argued. The application is built first, every read a
  replay; the snapshot and the gates come after it, measured against it.
- **Costs:** a harness and a corpus that do not yet exist and must be built
  before the first release can be gated; the in-process library (B5) must
  meet the walk budgets, which is unmeasured.

### E2. The migration is a strangler fig with Dolt as the oracle.
*Charter #9.*

- **Does not apply to lit2.** It governs replacing v1's store in place.
  lit2 is a clean implementation with no Dolt and no oracle. The design's
  §migration, OPEN #7, and every mention of S0–S3 gates are v1 material.

---

## F. Access control

### F1. Birth requirements: a signature slot and principal on every event; prose payloads envelope-capable; a versioned signed configuration; validation as a generic engine.
*Charter #6; design §security-and-config.*

- **Buys:** signed, verified history and tiered visibility with no server,
  which `requirements.md` §1 makes part of the first release.
- **Costs:** the event shape, the fold and the receive boundary carry
  signing, verification and envelopes from the first event; key material
  must be available to detached processes without a prompt.
- **Departure made:** nothing here is inert or later. `architecture.md`
  §3.16–3.19 put the write layer and the encrypted default tier in the
  first release. The signed configuration *stream* became a committed file
  (C4); the policy stays in the store as events.

### F2. Layers below the CLI boundary treat prose as opaque bytes.
*Design `INV:opaque-envelope`.*

- **Buys:** encryption touches only the CLI boundary; the fold, cache,
  codec and sync never see prose.
- **Costs:** nothing computed across items may read prose. Requirements and
  selectors on prose by content are evaluated only by the writer; a reader
  without the key sees `unknown`. Search over bodies is a CLI-boundary
  feature, never a store feature.

### F3. Every event carries a wall-clock time; durations read it, order never does.
*Design `INV:wall-time-is-data`.*

- **Buys:** "how long ago" questions have an answer; clock skew cannot
  reorder history.
- **Costs:** skew reaches every duration; a fast writer keeps a dead lane
  held, a slow one looks expired; the design adds an advisory for a writer
  ahead of the reader's clock.

---

## What is load-bearing and what is not

Removing any of these collapses something the others stand on:

- A2 (no server) with B1 and C1: the whole reason the fold exists.
- C1 with C2: without a total order the fold is not deterministic.
- C3: without intents, concurrent writes cannot converge.
- D2: with a locked or trusted cache, A2's "no locks" is gone.

These are choices the charter made that lit2 could make differently
without touching the above:

- **B3, one commit per command chained forever.** Replaced in
  `architecture.md` §3.15; see B3 above.
- **A4, per-writer refs on the code's remote.** Kept. The design's
  rejected-alternatives list rules out one shared ref for the workspace,
  because two writers must then agree on it. What each ref points at is
  now one commit over that writer's segments, replaced on push.
- **B5, no subprocess.** Buys a static binary; costs owning transport and
  authentication, which v1 never had to because it runs the git binary for
  push and fetch. `architecture.md` decision G takes the git binary for
  transport only, in the background process; the charter is amended.
- **D4, no heartbeats.** A standing ban with its reasons in
  `work-claims.md`; costs only the dead-holder window.
- **E2, the strangler fig.** Not applicable; drop from lit2's constraint
  set.
- **F1's signed configuration stream.** Already replaced in
  `architecture.md` by a committed file; `config-ref` survives as
  provenance and nothing in the fold reads it (decision H).
- **C1's byte-identical golden fixtures.** Already replaced in
  `architecture.md` by state equality.
