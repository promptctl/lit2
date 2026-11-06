# lit v2: architecture

Status: draft, 2026-11-05. The one strategy for building `lit2`, drawn from
the design documents in this directory. It names the substrate, the
primitives built on it, and how the functionality an issue tracker needs is
composed from those primitives without special cases. [scenarios.md](scenarios.md)
walks the common situations through it and records where it strained.

Precedence: [project-intent.md](project-intent.md), then the two charters
([event-store/charter.md](event-store/charter.md),
[access-control/charter.md](access-control/charter.md)), then this document,
then the design it cites for a concept. The contract described here is
expected to move as the software is used; what should not move is the
primitive set and the rule that functionality is composed from it.

## 1. What lit2 is

lit2 keeps a repository's backlog as a tree of nodes inside the repository's
own git object database, synced through the remote the code already uses,
with no server. An agent works it with four verbs on paths: `get`, `set`,
`add`, `rm`. Every write is one or more immutable events appended to the
writing checkout's own ref; the state everyone reads is a deterministic fold
of every checkout's events, so two machines holding the same events agree on
the backlog with nothing to merge. The workspace's model, its levels, fields,
columns, transition requirements, link types, lanes, and the guidance it
speaks to an agent, is data in the same store, edited with the same four
verbs. The engine knows a small fixed set of things and the content of none
of them.

## 2. The substrate and what it can do natively

Every primitive in §3 is chosen because the substrate does it for free. Where
a wanted behavior is not on this list, the design composes it from things that
are, rather than adding a mechanism beside the substrate.

| capability | what it gives | what it cannot give |
|---|---|---|
| append-only, content-addressed events | an immutable record; a history that is the audit log; undo as another event | in-place change: a correction is a new event, a removal is a rewrite |
| one ref per writing checkout, fast-forward only | writes that never contend; push and fetch that never merge; no locks anywhere | coordination before a write: two offline writers can both do a thing |
| a deterministic fold with a total order `(causal-position, writer)` | the same state on every machine from the same events; concurrent contradictions settled identically everywhere | reading a clock, so any "how long ago" question is answered after the fold |
| read-time evaluation with `now` | labels, expiry, staleness, held, computed on every read, never stored, so they settle as facts change | a stored reaction to a condition, which would outlive the condition |
| a versioned config stream in the same log | the model as data, folded like everything else, visible to every reader of the store | a model that depends on files outside the store |
| rewrites as events | purge, squash, compaction, erasure, converging on every replica | reaching copies that never sync again (charter #7) |
| a git remote as a dumb replicated store | sync through the channel the code already uses; remote filing from a storeless machine | enforcement by the remote; a server of any kind |
| a signature slot and opaque prose envelopes on every event | access control later without a schema change | hiding that an item exists (the visible skeleton) |

Two consequences shape everything above the substrate. Because the fold has
no clock, every derivation is sorted into one of two homes: the **fold**
(a pure function of events) or **tick** (a pure function of folded state,
config and `now`). And because no write is coordinated, every write is an
**intent** that stays meaningful whatever happened concurrently: `place x
above y`, not `x is at position 4`.

## 3. The primitives

Each primitive is one type, one job, and a stated set of things the engine
reads from it. The content is the workspace's. The engine ships no column
name, no level name, no label, no guidance text; those are the default
profile, data a workspace may rewrite `[LAW:types-are-the-program]`.

### 3.1 Node and path

The workspace is a tree. Every node has a path: an item id, then `/` and a
field, then a sub-id, as deep as the tree goes. `.` joins a child id to its
parent; `/` descends into a field. A path parses once into a typed address
(node kind, level, field, scalar or list, settable or derived) and every verb
takes the address `[LAW:parse-dont-validate]`. The **field table** is the
schema of the surface: one row per field with its type, whether it is
settable or derived, and what `set` accepts. If a field is not in the table it
does not exist, and `--help` is generated from the same table as the parser.

Selectors `(status=open labels=bug)` and globs `rin.*` expand to ids before
anything runs; a write over many paths is one write and lands whole or not at
all. The selector grammar is the same atom set a requirement's `test` uses
(§3.6), so a reader learns it once, and an atom's path may read through
`parent`, so `(stage=terminal parent/stage=terminal)` names the last ticket
of an epic. `set` and `add` both take `path value` pairs; a list value on
`add` appends each element.

### 3.2 Event and intent

An event is `(type, schema-version, subject, payload, attribution, config-ref,
causal-position, recorded-at, signature-slot)` as the event-store design
defines it. The verb layer compiles every write into intent events; the four
verbs are the interface, the events are the memory, and the two are not the
same shape:

| on the command line | events minted |
|---|---|
| `set x/title "…"` | `field.set {item, field, value}` |
| `set x/status review` | `transition {item, from, to}` |
| `add x/labels ready` / `rm x/labels/ready` | `label.add` / `label.rm` |
| `set x/labels '[a, b]'` | one `label.rm` per element removed, one `label.add` per element added |
| `add x/comments "…"` | `comment.add {item, id, body}` |
| `add <epic>/lanes/datetime '[dee, b64]'` | `lane.create`, then `lane.place {lane, item, after}` per member |
| `set <epic>/lanes/datetime '[b64, dee, fw9]'` | one `lane.place` per member whose predecessor changed |
| `set <lane>/rank 2`, `'{above: y}'`, `'{below: y}'`, `top`, `bottom` | `rank.place {subject, frame, above \| below \| top \| bottom}` |
| `set /columns/impl/entry '[…]'` | `config.set {path, value}` |
| `rm x` | `item.delete {item}`, a reversible tombstone |
| `rm x --purge` | a rewrite naming the item |

Ids are opaque: the project prefix from the config stream, then a hash of
`(writer, causal-position)`, children as `<parent>.<hash>`. Two offline
writers cannot collide, and a short id resolves by unique suffix.

### 3.3 Fold and tick

`fold(events) → state`: items, fields, comments, labels, links, lanes and
their order, frames and their order, lane holders, the folded config, and
every unresolved concurrent contradiction as a value on the thing it
concerns. `tick(state, config, now) → view`: computed labels, `held`, claim
expiry, staleness, stage, rollup. Every read is `tick(fold(events), now)`;
every write is evaluated against the same. Neither function performs I/O.

Determinism is a behavior: two folds of one event set, in any arrival order,
from genesis or from a snapshot, yield equal state. It is tested as that
equality, through the state's own equality and never through a pinned
serialization `[LAW:behavior-not-structure]`.

### 3.4 Item, level, frame

An **item** is a node at one of exactly three **levels**; roles are fixed in
code, names are configuration (`ticket`, `epic`, `initiative` by default).
Containment is exact: a level-1 item's parent is a level-2 item, a level-2's
is a level-3. An item created without a parent lands in a synthetic container
at the level above (`unfiled` at 2, `unassigned` at 3, two keys in the default
profile), so every item has exactly one place and nothing in the engine
handles a parentless case.

A **frame** is the node whose children are ranked against each other: the
root ranks initiatives, an initiative ranks epics, an epic ranks its lanes, a
lane orders its tickets. Rank is a position in a frame, folded from
`rank.place` intents, and the frame is always the item's container, so a rank
intent names a frame that exists and reparenting an item moves it to the
bottom of its new frame, echoed. An intent whose anchor is no longer in the
subject's frame when it arrives places the subject at the bottom of its frame
and is named on `/integrity`. The backlog is the depth-first walk of the
tree in frame order; `/lanes` at the root is that walk's lanes.

Only a level-1 item has a stored column. A level-2 or level-3 item derives
`stage` from its children (`terminal` when all are, `initial` when all are or
it is empty, else `active`) and `rollup`, the count of children per column.
Finishing or reopening a container happens only through its children.

### 3.5 Column and stage

A **column** is a node in the column graph, `/columns/<name>`. A ticket's
`status` holds the name of the column it is in; the newest `transition` in its
history is the one stored fact about its position. The engine reads two flags,
`initial` and `terminal`, and a `next` list. Everything the engine does with a
column it does through **stage**: `initial`, `active` (neither flag),
`terminal`. Start is any move out of `initial`; done is any move into a
`terminal`. Reachability follows from `next` alone: a column some `next` names
is entered only along those edges; a column no `next` names is entered from
everywhere, which is how parking, reopening and closing without finishing
work with no edge list.

A resolved column graph is checked on every write that changes it: one
`initial`, at least one `terminal`, every `next` target present, no column
removed while a ticket in the frame holds it. Two concurrent writers can
still produce a ticket in a column its frame no longer declares; the fold
keeps the transition, tick reports the column as undeclared with stage
`active`, `/next` passes the lane over naming the reason, `/integrity` lists
it, and the universal edges always offer a way out. Any node may carry `columns`;
a ticket resolves its graph by nearest ancestor, per column name, so an epic
adds an `audit` column by declaring it and pointing `plan/next` at it.

### 3.6 Requirement

A **requirement** is three values on a column's `entry` or `exit` list: a
`test` (one selector atom over one path of the ticket: `p`, `!p`, `p=v`,
`p!=v`, `p~re`), a `scope` (`self` or `ancestors`), and a `strength` (`block`
or `advise`, no default). One pure function evaluates them, at exactly one
place, the transition write: the source's `exit` list on a forward edge, the
destination's `entry` list on every arrival, all of them, so a refusal names
everything unmet at once `[LAW:single-enforcer]`. `block` refuses and writes
nothing; `advise` prints and proceeds. The same evaluation, run as a dry run
of each forward edge, is the derived field `held`.

Anything a workspace wants to require is a fact something wrote (a field, a
label, a verdict) or a derived field the engine exposes (`blockers`,
`plan/verdict`, `verdict`), read by a requirement. The test grammar does not
grow.

### 3.7 Lane

A **lane** is an ordered list node in an epic, `<epic>/lanes/<name>`; its
order is sequence, and a member's `blocked_by` derives from the member before
it. A ticket is in exactly one lane in its own epic; a ticket in no declared
lane is its own one-member lane, so parallel costs nothing and serial is one
`add`. A sequence that crosses epics is `blocks` edges. The lane is also the
unit a checkout holds (§3.8). Its head is its first member whose stage is not
`terminal`. A lane is identified by its name in its frame; `lane.create` is
idempotent on that pair, so two offline writers who create the same lane fold
to one lane whose members are placed in total order, with an advisory.

### 3.8 Claim

A **claim** is derived, never written. A lane is held by the checkout that
made its latest establishing transition, out of `initial` or into a
`terminal`, on any member; the fold computes the holder set in total order,
`claim` is the last, and `contested` is the rest. Release is a write,
`rm <lane>/claim`, which the fold reads as ending the hold until the next
establishing transition; parking a member into `initial` and finishing the
lane's last member end it too. Expiry is tick's: `claim_ttl`, resolved by
nearest ancestor, measured from the holder's newest event on any member; an
expired claim is no claim and prints as none. A ticket's `claim` reads its
lane's, so finishing one member hands the checkout the next with no second
action and no event on the next ticket.

### 3.9 Link

A **link** is a typed edge between items. The engine knows two kinds and no
more: `dependency`, directed and acyclic, read by `/next` and `blocked_by`;
`relates`, a reference with no scheduling meaning. A workspace declares link
types under `/links` with a kind and the `[from, to]` level pairs they may
join, adds one with a new name, and cannot add a kind.

### 3.10 Rule

A **rule** is `{where: <selector>, label: <name>}` in `/rules`. Tick evaluates
every rule on every read and the matching tickets carry the label, computed,
never stored, so it is gone the read after its condition stops holding and
needs no merge. The default profile ships one: `blocked` where a blocker's
stage is not terminal.

### 3.11 Moment and handler

A **moment** is one of six things the engine observes: `enter <column>`,
`leave <column>`, `in <column>` (a read printed the ticket whole), `refused
<column>`, `wrote <glob>`, `read <glob>`. A **handler** binds `on` a list of
moments, `where` a selector, and holds a `body` of prose; a **workflow** is a
named set of handlers a project adopts or declines as one thing. A handler
prints text after the data it belongs to and produces nothing else. Handlers
resolve per field from the ticket's node up through its ancestors, then the
user layer, then the shipped layer, so the smallest override is one field.
Bare `lit2` is `get /`, and the shipped `orientation` handler on `read /` is
the entry point the managed block in `AGENTS.md` names.

### 3.12 Rewrite

A **rewrite** is an event that names the events it drops, the payloads it
replaces, or the ranges it squashes. The fold applies it on arrival; each
writer then rebuilds its own ref without the rewritten material, and a
receiver accepts a non-fast-forward move only when rewrite events account for
every difference. Purge is the rewrite that drops one item's events; the fold
also drops events on that item that arrive after the purge from writers who
had not seen it, and counts them on `/integrity` with their writer, so a lost
comment is named to the one who wrote it. Compaction is the rewrite that
squashes. Nothing else removes anything.

### 3.13 Identity

Every event's attribution is `(checkout, principal, agent)`. The checkout id
is the 128-bit token in the checkout's private git dir, the thing claims and
writer refs key on. The principal is the key that signed the event; until the
access-control write layer ships it is a key the checkout mints at init, so
the slot is real from the first event. The agent label is an optional opaque
string the invoking harness supplies, shown beside comments and history. No
usernames, hostnames or paths enter the store. `get /me` prints the three.

### 3.14 The workspace document

The configuration is nodes: `/levels`, `/columns`, `/links`, `/rules`,
`/workflows`, `/lanes` on epics, `claim_ttl` on any node. A write to one is
`config.set {path, value}`, an event like any other, and the configuration
document is a fold output parsed once into one typed schema value that every
command carries. Two concurrent edits to different keys both land; two to one
key fold like a scalar; a union that references something undeclared keeps
the value, shows it as undeclared, and counts it on `/integrity`. The schema
parse refuses locally what the designs say it refuses; the fold's tolerance
exists for the one case the local check cannot see. A store with no config
events reads as the shipped default profile. The user layer of workflows is
the one configuration outside the store, because it spans repositories.

## 4. Functionality, composed

What an issue tracker must do, and which primitives do it. The test for each
row is that no primitive learned a new case.

| the tracker must | composed from |
|---|---|
| file work, alone or in bulk, with defaults | `add <parent>/children '{…}'` or a list of them; the level's template fills absent fields and echoes each as a derivation |
| keep work out of the queue until it is ready | the initial column's `exit` list: a description present, `labels=ready` on the ticket or an ancestor, no blocker; `held` reports it, `/next` reads `held` |
| say what to work on | `/next`: the heads of the lanes this checkout holds, in frame order; a held head whose own `held` is non-empty is printed with its reasons and passed; then the first lane head in frame order that is not held elsewhere and whose `held` is empty, labeled as outside the commitment; every lane passed over is named with its reason |
| commit a checkout to a unit of work | the claim on the lane, derived from the start transition; `/next` serves the held lane first; leaving is `rm <lane>/claim` |
| sequence work | a lane; reorder with one `set` on the list |
| make work wait on other work | a `blocks` link; `blockers` and `blocked_by` derive from links and lane position together |
| interrupt for an urgency | `priority: urgent` on a ticket; `/next` serves an urgent head ranked above the held lane |
| move work through a process | transitions along `next`; requirements on the edges; stage gives start and done |
| review a plan before code | `plan` is a versioned text node; `add x/plan/verdicts '{pass of:<digest> laws:[…]}'` names the digest the reviewer read, `of` required, refused if the plan has moved since; `impl/entry` requires `plan/verdict=pass` |
| send work back from review | `add x/verdicts '{fail findings:[…]}'`; the ticket stays in `review`; round derives from the count of fails; `merge/entry` requires `verdict=pass` |
| close without finishing, with a reason | terminal columns `dup`, `superseded`, `obsolete`, `wontfix`, each with an `entry` requirement for `of` or `reason` |
| park, reopen | a transition into `initial`, reachable from everywhere; the claim ends |
| finish a container | derived: an epic's stage is terminal when every child's is |
| tell an agent what to do now | the handler bound `in <column>` prints after the ticket on `get x` and on `/next` |
| nudge without blocking | a requirement with `strength = advise`; a handler |
| customize the process per project or per epic | `columns`, `workflows`, `claim_ttl` on any node; nearest ancestor wins per column, per handler field |
| add a field, a level name, a link type | `/levels/<n>/fields`, `/levels/<n>/name`, `/links/<name>`; no code |
| see why a ticket is where it is | `get x/history`; `get x/held`; a rule's label names the rule |
| undo | `set` back from the echo, or `get x/body/versions/N | set x/body -`; every version is in the history |
| find things | selectors over any field, at any depth, across every frame's dialect of a column name |
| edit many things at once, safely | selectors and globs expand once; `--dry-run` prints the expansion and the diff; a refusal on one path writes nothing |
| work from several worktrees of one clone | one store under the common dir; each worktree its own checkout, ref and claims |
| work from several machines, offline | per-writer refs; background push and fetch; the fold settles concurrent edits; conflicts are values; `/integrity` lists advisories |
| know the view is stale or the push failed | tick compares local refs to remote-tracking refs and reads the last push outcome; one advisory line on read and write |
| file a bug with no clone | mint events, push to `refs/lit/inbox/<id>`; any clone's receive folds and anchors them |
| read another repository's backlog | `--at <dir>` opens the store, folds, parses the same config, ticks; writes nothing |
| erase content | `rm x --purge`, a rewrite; every replica converges; `gc` reclaims |
| keep the store small and fast | the cache snapshot; compaction as a rewrite; budgets as gates |
| survive a binary that is too old | `INV:preserve-unknown`: unknown events carried and counted; mutations on their subjects refused; `min-reader-version` in the config stream |
| restrict who may do what, later | the principal on every event; capabilities as `(verb, path glob, binding)` over the same paths; the signature slot verified at receive |
| hide content from some readers, later | prose fields are envelopes from the first event; structure stays visible |

Three functions the source documents asked about and this table answers
without a new mechanism: "ready to pull" is the initial column's exit list;
"rework" is a verdict inside `review`; "orphaned" is the selector
`(stage=active updated<-6h)`.

## 5. One command, start to finish

Every verb runs this pipeline on every call; the verb and the paths are
values `[LAW:dataflow-not-control-flow]`. Steps 2 through 5 are pure functions
of (events, clock, argv, writer); the rest is the hearth
`[LAW:effects-at-boundaries]`.

1. **Open.** Read every writer ref head; load the cache snapshot keyed by
   `(fold-version, ref heads)`. A miss or a torn snapshot is one refold; no
   lock.
2. **Fold** the events newer than the snapshot.
3. **Parse** the folded configuration into the schema value.
4. **Address.** Parse each path into a typed address; expand selectors and
   globs once.
5. **Evaluate.** `get` computes its answer with tick and the clock. A write
   computes a description: the intents it would mint, the requirement
   verdicts, the derivations, the echo. Refusal of any path refuses the whole.
6. **Perform.** Append the intents as one commit to this checkout's ref, or
   decline because `--dry-run` was given. Update the snapshot by atomic rename.
7. **Speak.** Print the echo, then the handlers' bodies for the moments
   raised, after the data; under `--json`, one object keyed by path.
8. **Record** the command event: paths, expansion, verdicts, events, handlers
   fired and suppressed, cache hit or refold, timings, `dry_run`, `written`.
   One record per invocation, local, never synced `[LAW:nothing-unseen]`.
9. **Sync.** A write schedules a detached `lit2 admin sync` to push this
   checkout's ref; a read starts the same when the last fetch is older than
   the debounce. Nothing in the foreground waits on the network.

## 6. The parts, downhill

`[LAW:one-way-deps]` Each layer depends only on the layers below it.

```
 cli        argv → typed command; rendering; exit codes; --json
 guidance   moments → resolved handlers → text after the data
 verbs      get/set/add/rm on typed addresses; requirements; verb → intents; echo
 address    path grammar; selectors; globs; the field table
 tick       clocked derivations: labels, held, expiry, staleness, stage, rollup
 config     the folded document → one typed schema value
 fold       events → state; validation engine; rank; holders; conflicts
 events     record, codec, upcasters, ids, signature slot
 store      objects and refs under the common dir; per-writer and inbox refs;
            rewrites; snapshot; the background sync process
 git        an in-process, permissively licensed git library
```

Beside the stack, passed through by every layer: the command event, and the
composition root, the one place a store is opened and the clock, the writer
identity and the agent label are read. Tests build the same stack over an
in-memory object store, a fixed clock and a fixed identity.

## 7. Decisions for Brandon

Each has my recommendation first. Everything not listed is decided above.

**A. Lanes live in their epic and rank lives in the frame (§3.4, §3.7), or
lanes are cross-cutting in one global order ([path-addressed-cli.md](path-addressed-cli.md)).**
Recommended: in the epic. It makes the epic the unit of commitment and the
"all of one epic before the next" ordering structural, gives every rank
intent a frame that exists, and makes `/next` one rule. Cost: a sequence
across epics is `blocks` edges or a level-2 item, not a lane.

**B. Language and git library.** Recommended: Go, with go-git (Apache-2.0)
in process. v1 is Go, every design assumes it. The first budget run measures
go-git's object walk on the depth corpus; gitoxide behind cgo is the fallback.

**C. `topic`.** Recommended: not a field; ids are opaque with the project
prefix, and an area of focus is a label.

**D. `archived`.** Recommended: not a field; a ticket out of the flow is in a
terminal column, one that should not exist is deleted (tombstone) or purged
(rewrite).

**E. First release.** Recommended: everything in §3 and §4 except the two
rows marked "later", with the signature slot and envelopes present and inert.

**F. Synthetic container names.** Recommended: `unfiled` and `unassigned`,
two keys in the default profile.

## 8. Constraints on later work

- A new fact is an event kind plus a fold rule and, if it has a clock, a tick
  rule. Never a table, never a second store, never a file beside the store.
- A new capability is a verb on a path. Policy will name capabilities as
  `(verb, path glob, binding)`; what cannot be spelled as a path write cannot
  be authorized.
- Every prose field is an envelope below the verb layer from its first event.
- Everything computed across items reads structural fields only. A
  requirement that reads prose by content is evaluated by its writer, who
  holds the key; a reader without it sees that requirement's verdict as
  `unknown (tier)`, a third value of the verdict type, never a guess.
- The fold reads no clock; tick reads one. A derivation that wants both is
  two derivations.
- No network on a foreground path; no lock on the cache.
- A handler produces text and nothing else.

## 9. Testing

Tests assert what an agent or a person can rely on after a command, in the
vocabulary of §3, never the shape of the code or the bytes it printed
`[LAW:behavior-not-structure]`.

- Layers from events through verbs are pure and are tested in process over
  an in-memory object store, a fixed clock and a fixed identity: given these
  events and this command, this is refused for this reason, or these intents
  are minted and this state results.
- Determinism is tested as agreement between independent folds.
- The scenarios in [scenarios.md](scenarios.md) are the acceptance contract:
  each ends in what the agent sees and can do next.
- The budgets in [event-store/budgets.md](event-store/budgets.md) run in the
  harness and gate releases.
- One end-to-end smoke per release: a real repository, a real remote, two
  clones, the background sync.

## 10. Build order

Each step leaves every layer below it in its final shape; step 2 is the
walking skeleton.

1. **Skeleton.** The layout of §6, the composition root, the command event,
   the in-memory store, the harness, `lit2 --help` from the field table.
   (ticket lit-bootstrap-1f9.438)
2. **Events and fold, first facts.** Items, fields, comments, labels, links;
   ids; the snapshot; `add /children`, `get`, `set`, `add`, `rm`; real objects
   under the common dir; `lit2 admin init`.
3. **Config and columns.** Config events and the schema parse; levels and
   synthetic containers; columns, stage, rollup; transitions with the one
   enforcer; the default profile.
4. **Lanes, rank, claims, next.** Frames and rank intents; lanes; holders and
   expiry; `/lanes`, `/next`, `held`, rules.
5. **Guidance.** Workflows, handlers, the three layers, moments, the managed
   block, `--dry-run` printing guidance.
6. **Sync.** Refs on the remote, the background process, inbox refs,
   staleness, `/integrity`.
7. **Rewrites.** Purge, squash, sweep, the receiver's rule; the budgets
   harness and the depth corpus.
8. **Selectors in full, `--json`, `--at`.**

## Appendix: where this departs from a source document

One line each, so a reader of the older document knows the position moved.

- event-store design §events: no fold-derived child ordinals; ids are the
  opaque form only.
- event-store design §fold: determinism is asserted as state equality, not
  pinned bytes.
- event-store design §security-and-config, workspace-configuration.md: the
  configuration is folded from per-path `config.set` events; the whole
  document is a rendering, not an event payload.
- path-addressed-cli.md: lanes live in their epic; rank is per frame; `rank`
  takes relative forms that are the events' own shape.
- work-claims.md: the routing steps collapse into the frame walk; the holder
  set replaces the single claim scalar; everything else stands.
- columns-and-phases.md, transition-requirements.md: a verdict carries `of`,
  the digest it judged.
- custom-fields.md, workspace-schema: `topic` and `type` are not fields;
  `archived` is not a field (decisions C, D).
- schema-reversibility.md: the two rules survive as `INV:preserve-unknown`
  and the release-only raise of event schema versions; the SQL mechanism
  does not.
- MINIMAL-LIT-SKETCH: lanes, claims and priority are kept, as the later
  designs require; its eleven guarantees are all rows of §4.
