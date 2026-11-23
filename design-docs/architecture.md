# lit v2: architecture

The strategy for building `lit2`, drawn from the design documents in this
directory. It names the substrate, the primitives built on it, and how the
functionality an issue tracker needs is composed from those primitives.
[scenarios.md](scenarios.md) walks the common situations through it.

## 1. What lit2 is

lit2 keeps a repository's backlog as a tree of nodes in a git object database
of its own under the repository's git common dir, synced through the remote
the code already uses, with no server. An agent works it with verbs on paths
([path-addressed-cli.md](path-addressed-cli.md) "Verbs"). Every write is one
or more immutable events appended to the writing checkout's own ref; the
state everyone reads is a deterministic fold of every checkout's events, so
two machines holding the same events agree on the backlog with nothing to
merge. The workspace's model, its levels, fields, columns, transition
requirements, link types, and the guidance it speaks to an agent, is a file
in the repository, applied into the same store as a version (§3.15); lanes
and everything else a tracker holds are events in it, written with the same
verbs. The engine knows the primitives below and the content of none of
them.

## 2. The substrate and what it can do natively

Each primitive in §3 is built on what the substrate does natively. Where a
wanted behavior is not on this list, the design prefers to compose it from
things that are rather than add a mechanism beside the substrate.

| capability | what it gives |
|---|---|
| append-only, content-addressed events | an immutable record; a history that is the audit log; undo as another event |
| one ref per writing checkout, replaced only by its writer | writes that do not contend; push and fetch with nothing to merge; no locks; nothing on the remote grows with commands |
| a deterministic fold with a total order `(causal-position, writer, event id)` | the same state on every machine from the same events; concurrent contradictions settled identically everywhere |
| read-time evaluation with `now` | expiry, staleness, held, computed on every read, so they settle as facts change |
| committed configuration files, versioned by git | the model as data a person edits, reviews in a pull request and diffs; every version addressable by blob hash in the code repository's history |
| rewrites as events | purge, squash, compaction, erasure, converging on every replica that syncs |
| a git remote as a dumb replicated store | sync through the channel the code already uses; remote filing from a storeless machine |
| a signature on every event and opaque prose envelopes | every verifier enforces who may write what, with no server; content hidden from readers without the key; policy as events in the same store |

Two consequences shape everything above the substrate. Because the fold has
no clock, every derivation is sorted into one of two homes: the **fold**
(a pure function of events) or **evaluate** (a pure function of folded state,
config and `now`). Only facts are stored: stage, `held`, a lane's holder, a
verdict, `/next` and every other derived value are computed on read from
facts, the configuration and `now`, and a read writes no event. And because
no write is coordinated, every write is an **intent** that stays meaningful
whatever happened concurrently: `place x above y`, not `x is at position 4`.

## 3. The primitives

Each primitive is one type, one job, and a stated set of things the engine
reads from it. The content is the workspace's. The engine ships no column
name, no level name, no label; those are the default profile, data a workspace may rewrite `[LAW:types-are-the-program]`.

### 3.1 Node and path

The workspace is a tree. Every node has a path: an item id, then `/` and a
field, then a sub-id, as deep as the tree goes. A path parses once into a typed address
(node kind, level, field, scalar or list, settable or derived) and every verb
takes the address `[LAW:parse-dont-validate]`. The **field table** is the
schema of the surface, one row per field
([workspace-schema/design.md](workspace-schema/design.md) "Fields"), and
`--help` is generated from the same table as the parser.

Selectors `(status=open labels=bug)` and globs `rin.*` expand to ids before
anything runs. The selector grammar is the same atom set a requirement's `test` uses
(§3.6), so a reader learns it once. The path grammar, selectors, globs,
output order and `@` references in prose are
[path-addressed-cli.md](path-addressed-cli.md) "Paths", "Verbs", "Everything
else is a path" and "Bulk".

### 3.2 Event and intent

An event is `(type, schema-version, subject, payload, attribution, config-ref,
policy-version, causal-position, recorded-at, signature-slot)` as the
event-store design defines it ([event-store/design.md](event-store/design.md)
"§events — the event model"). The verb layer compiles every write into intent events; the
verbs are the interface, the events are the memory, and the two are not the
same shape:

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
| `rm x`, which is `set x/deleted true` | `field.set {item, deleted, true}`, a reversible tombstone; `set x/deleted false` restores |
| `add <lane>/claim` / `rm <lane>/claim` | `claim.take {lane}` / `claim.release {lane}` |
| any write with `--reason "…"` | the reason on every event the write mints, an enveloped prose field |
| `rm x --purge` | a rewrite naming the item |

Ids, their segments and the total order are the event-store design's
("§events — the event model"). The project prefix and the item's
`topic` (§3.4) are not part of the id: the rendering is
`<prefix>-<topic>-<segments>`, `lit-bootstrap-1f9.zc7`, the prefix a label
the active version (§3.15) supplies and the topic the item's own field, so a
reader can tell otherwise arbitrary segments apart and changing either
rewrites nothing; a reference in prose is stored as `@<segments>` and
rendered the same way, so prose follows a rename too. Parsing accepts the segments bare, or under any prefix a
version in this store's log has ever declared and any topic the item has ever
carried, and refuses a prefix none has, naming it as another project's, or a
topic the item never had, naming its current rendering.

### 3.3 Fold and evaluate

`fold(events) → state`: items, fields, comments, labels, links,
lanes, their order and their rank, checklists, lane holders, the revisions
and reviews of each reviewable (§4), the applied workspace versions (§3.15),
and every
unresolved concurrent contradiction as a value on the thing it concerns.
`evaluate(state, config, now) → view`: `held`, claim
expiry, staleness, stage, rollup, the labels declared with `where` (§3.11). Every read is `evaluate(fold(events), config, now)`;
every write is judged against the same. Neither function performs I/O.
The fold's order and determinism are the event-store design's ("§fold — one
function owns derivation").

### 3.4 Work item, level, tree

A **work item** is a node of the tree at a **level**. The workspace file
declares the levels, numbered from 1 at the bottom, each with
a name. Code knows level numbers and roles, not names. An item at level 1 is a **leaf**
and an item above level 1 is a **node**, whose `stage` and `rollup` derive
from its children ([columns-and-phases.md](columns-and-phases.md)). A one-level workspace is a flat
list of leaves under the root. Containment, the synthetic homes that hold
parentless items, and moves are
[workspace-schema/design.md](workspace-schema/design.md) "Hierarchy".

Beside its title and prose an item carries built-in fields
([workspace-schema/design.md](workspace-schema/design.md) "Fields"). One is
`topic`, settable, one short word that groups items across the tree: the create
supplies it or lit2 derives one from the title and echoes the derivation, and
`(topic=cli)` selects on it; a child's default is its parent's topic, a
top-level item's is derived from its title, and the topic renders in the id
(§3.2).

The tree groups, scopes and rolls up; order is the lane's (§3.7).

### 3.5 Column and stage

A **column** is a node in the column graph, `/columns/<name>`. A ticket's
`status` holds the name of the column it is in. The engine reads two flags,
`initial` and `terminal`, the `next` lists, and the entry and exit lists, and
most of what it does with a column it does through **stage**: `initial`,
`active` (neither flag), `terminal`. Transitions, start and done,
reachability, a column a ticket's ancestors no longer declare, and resolution
by nearest ancestor are [columns-and-phases.md](columns-and-phases.md); the
checks a column graph meets when a version is applied are
[workspace-schema/design.md](workspace-schema/design.md) "Validating the
schema".

### 3.6 Requirement

A **requirement** is one entry of a column's `entry` or `exit` list: a `test`
and a `severity`. A test is one selector atom over one path of the ticket, or
the name of a **check**, a named list of tests. One pure function evaluates
requirements at the transition write: `error` refuses and writes nothing;
`warning` proceeds. The same evaluation, run as a dry run of each forward
edge, is the derived field `held`. The atoms, checks, scopes and severities
are [transition-requirements.md](transition-requirements.md).

Anything a workspace wants to require is a fact something wrote (a field, a
label, a review) or a derived field the engine exposes (`blockers`,
`plan/verdict`, `pr/verdict`), read by a requirement.

### 3.7 Lane

A **lane** is an ordered list node, `<node>/lanes/<name>` on any node
including the root, whose members are leaves from anywhere in the tree; its
order is sequence. A leaf in no declared lane is its own one-member lane.
Lanes are ranked against each other in one order, folded from `rank.place`
intents. The lane is also the unit a checkout holds (§3.8), and its head is
its first member whose stage is not `terminal`. Membership, rank and lane
creation are [path-addressed-cli.md](path-addressed-cli.md) "Lanes"; what
`/next` serves is [work-claims.md](work-claims.md) "Routing".

### 3.8 Claim

A **claim** is a checkout's hold on a lane. The fold computes the holder
from claim events, and evaluate applies `claim_ttl`. A ticket's `claim`
reads its lane's. What establishes a claim, what ends it, and contested
holders are [work-claims.md](work-claims.md).

### 3.9 Checklist

A leaf carries a **checklist**, `x/checklist`: items with a minted id, a
text, and one of three states, `pending`, `done`, `abandoned` with an
optional reason. Checklist items are not work items: they have no level,
lane, claim, link or column. `done` and `abandoned` are final, and an item
is removed only by a rewrite, so a finished leaf shows six months later what
was done and what was abandoned and why. `add x/checklist '[…]'` mints items;
`set x/checklist/<id>/state done` moves one; `set x/checklist '[…]'`
replaces the list and the verb layer diffs it: items whose text it knows
keep their id and state, new text is added, vanished text is abandoned,
which is what lets a harness hook push an agent's whole TODO list on every
change with no agent effort. The fold merges per item and a final state
wins over `pending`. No event per check is folded into history beyond the
item events themselves; the leaf's transitions carry the counts. A
column may require `checklist/pending = 0` on entry, so finishing a leaf with
pending items is refused and names them.
Item text is prose and an envelope (§3.19); the `set` diff matches on the
commitment.

### 3.10 Link

A **link** is a typed edge between items. The engine knows two kinds:
`dependency`, directed and acyclic, read by `/next` and `blocked_by`;
`relates`, a reference with no scheduling meaning. A workspace declares link
types of a known kind with the level pairs they may join
([workspace-schema/design.md](workspace-schema/design.md) "Link types"). A
node's stage is derived, so a dependency on a node is a dependency on its
leaves.

### 3.11 Label

A **label** is a value of the built-in `labels` field, an enum that holds
many values. How a label is declared, applied or computed, and read, and how
undeclared labels are treated, are
[transition-requirements.md](transition-requirements.md).

### 3.12 Moment and handler

A **moment** is something the engine observes, such as a ticket entering or
leaving a column, a refusal, a write or a read. A **handler** binds `on` a
list of moments and `where` a selector to a body it runs at those moments; a
**workflow** is a named set of handlers a project adopts or declines as one
thing. Moments, handlers, workflows and how an override resolves are
[workflows.md](workflows.md). The shipped `orientation` handler on `read /`
is the entry point the managed block in `AGENTS.md` names
([guidance.md](guidance.md)).

### 3.13 Rewrite

A **rewrite** is an event that names the events it drops, the payloads it
replaces, or the ranges it squashes; history changes only through signed
rewrite events. Purge is the rewrite that drops one item's events, and
compaction is the rewrite that squashes. Rewrites, purge, squash windows and
how a receiver accepts a rewritten tree are
[event-store/design.md](event-store/design.md) "§events — the event model"
and "§store — git as the substrate".

### 3.14 Identity

Every event's attribution is `(checkout, principal, agent)`. The checkout id
is the 128-bit token in the checkout's private git dir, the thing claims and
writer refs key on. The principal is the person on whose authority the event
is written, and each clone signs with a key its principal delegates (§3.17).
The agent label is an optional opaque string the invoking harness supplies,
shown beside comments and history. Display names come from the policy's
member entries; what identity enters the store is the event-store design's
("§events — the event model"). `get /me` prints the three and the tiers
this principal holds.

### 3.15 The workspace file

The project's model is written in `.lit/workspace.toml`: the prefix,
`min-reader-version`, levels and their fields, columns and their requirements,
labels, checks, link types, `claim_ttl`, and per-node sections keyed by a node's
id. The file in git is the intent: a person edits it in an editor, git
versions and merges it, and the team reviews it however it reviews any change
to the repository. A write such as `set /columns/backlog/exit '[…]'` edits
the working-tree draft and says so in the echo; committing it is the person's
act, like any other file. The file itself never becomes an event.

The model in force is the **active version**: the last one applied with
`lit2 config commit`. Applying is an event that carries the version's bytes
and its git blob id and names the version it replaces, so the log is
self-sufficient and the file is never read to learn what is in force. Apply
refuses a version that does not parse, or whose column graph fails the check
of §3.5, naming every fault with its key path, and nothing overrides that
refusal. A draft in the working tree is not in force until it is applied,
so a draft that does not parse stops no command. `Parse` reads one version's
bytes into one typed schema. `lit2 admin init` writes the default profile,
`internal/assets/workspace/default.toml`, to `.lit/workspace.toml` when no
file exists, and applies the same bytes as version one at init in a store
no version is in force in, right after the enrollment (§3.17), so the file
and the active version share one blob id from the start; an existing file is
applied instead, and must parse. Every verb refuses a store with no version
in force, so every event a verb writes is written under one.

Every event carries `config-ref`, the git blob id of the active version its writer
ran under, in the code repository's object format; the write is judged under
that version, whose bytes the log carries in its apply. An apply's own
`config-ref` is the version in force when it was written; version one's is
its own blob. Which committed file an apply accepts, contending applies and
`config status` are [workspace-schema/design.md](workspace-schema/design.md)
"Applying a version".

What belongs in the file is decided by one test: a value is the workspace's
when two machines that disagree about it cannot both be right about the
backlog, and a person's own when they can. A level's name, which fields exist,
a column's requirements, which labels are declared, a link type's legal pairs,
`claim_ttl`, `min-reader-version`: each decides what a write may do or how a
fact reads, so each is in the file and in force only once applied. How a
machine syncs and caches is its own, and stays in that person's
configuration, as does a person's own layer of workflows
([workflows.md](workflows.md)); the record it produces is in the store
either way. The model is one document
rather than one table per concern because `Parse` then has a single boundary,
the applied version has the one blob id that `config-ref` names, and the
person edits one file in a syntax they already know.

### 3.16 Log, segment and ref

A writer's events are one append-only **log** under the common dir. Sync
seals the log into **segment** blobs and publishes one commit per writer to
`refs/lit/log/<checkout-id>`, a ref only its writer updates, so the remote
grows with writers rather than events. Logs, segments, writer and inbox
refs, and compaction are [event-store/design.md](event-store/design.md)
"§store — git as the substrate"; the snapshot is its "§cache — scaffolding,
never structure".

### 3.17 Principal and policy

A **principal** is a person's signing identity in the crypto suite (§7 E);
its fingerprint is the principal id, and names live in the policy's member
entry. `lit2 admin init` sets up the clone's keys silently and prints one
recovery phrase; each clone signs with a key its principal delegates. The
**policy** is the member list, the role definitions and a monotone version,
kept in the store as events. The first policy is the **enrollment**, and
each clone pins the one that founds its store from the committed
`.lit/founding`. Principals, delegated keys, succession, policy activation,
the owner threshold and the founding pin are
[access-control/design.md](access-control/design.md) "Write layer: signed
events, verified policy".

### 3.18 Capability

A capability is `(verb, path glob, binding)`: a verb, a glob over paths, and
a binding, currently `own`, `assignee`, `own-or-assignee` or `all`. A role
is a named bundle of capabilities. A column is no capability's condition;
the requirement gate holds where a status may move. Closing is moving: a move carries the fields its
column's entry needs, `set x/status '{column:obsolete reason:"…"}'`, in
its one transition, whose path is `x/status`, so whoever may move a ticket
may close it. The capability rule is the fold's (§3.20): a command folds
the events it would mint onto the writer's current fold, and if the fold
would carry any of them, it writes none, naming each capability of the
verb with what the write missed. Membership is paths too:
`add /members '{principal, role}'`, `set /members/<id>/role editor`,
`rm /members/<id>`, `set /policy/threshold 2`, each compiling to a proposal
and, where the writer's own signature suffices, its approval. Bindings,
roles and the default roles are
[access-control/design.md](access-control/design.md) "Capabilities and
roles".

### 3.19 Tier and envelope

Every prose field is an **envelope** `{suite, tier, epoch, nonce,
ciphertext, commitment}` from its first event, sealed at the verb layer, so
the layers below see opaque bytes (`INV:opaque-envelope`). A **tier** is a
named visibility group with an epoch key wrapped to each member's
encryption key in the policy. Each item and each comment carries `tier`, a
structural field. The backlog currently lists every item and redacts what
the reader cannot open, and one renderer produces every view and applies
that redaction. Envelopes, tiers, epochs, destroy and the default tier are
[access-control/design.md](access-control/design.md) "Read layer: tiers,
epochs, wrapped keys".

### 3.20 Verification

Validity is enforced by every verifier and by no trusted party.
Authorization is one fold rule: for each event in total order the fold
checks the signature against its principal's key and the intent against
the policy in force at the event's causal position, and an event that fails
is carried, counted on `/integrity` with its writer, and not applied. Every
receiver runs that rule over a writer's published segments as it accepts
them, and a write runs it over its own events before appending them. The
rule, and how events a revoked key back-dates are reported, are
[access-control/design.md](access-control/design.md) "Enforcement".

## 4. Functionality, composed

What an issue tracker must do, and which primitives do it. Each row composes
existing primitives where it can.

| the tracker must | composed from |
|---|---|
| file work, alone or in bulk, with defaults | `add <parent>/children '{…}'` or a list of them, at the level below the parent; the level's template fills absent fields and echoes each as a derivation |
| keep work out of the queue until it is ready | the initial column's `exit` list; `held` reports it, `/next` reads `held` |
| say what to work on | `/next` ([work-claims.md](work-claims.md) "Routing") |
| commit a checkout to a unit of work | the claim on the lane ([work-claims.md](work-claims.md)) |
| pick work inside one node | `<node>/next`: the same walk over the lanes whose head is under the node |
| sequence work | a lane, on any node, with members from anywhere; reorder with one `set` on the list; order lanes against each other with `set <lane>/rank` |
| track the steps inside one ticket | the checklist on the leaf; a column may require `checklist/pending = 0` on entry |
| make work wait on other work | a `blocks` link; `blockers` and `blocked_by` derive from links and lane position together |
| interrupt for an urgency | `priority: urgent` on a ticket ([work-claims.md](work-claims.md) "Routing") |
| move work through a process | transitions along `next`; requirements on the edges; stage gives start and done |
| review a plan before code | `plan` is a **reviewable**: two append-only lists, `revisions`, each carrying its content, and `reviews`, each naming the revision it judged and carrying how it was run, one line, the reviewer's result, the agent's response, and its verdict. `add x/plan/reviews '{of:3 how:"…" result:"…" response:"…" verdict:pass}'`. `plan/verdict` is the verdict of the last review naming the latest revision; the default profile's `impl/entry` requires `plan/verdict=pass` |
| send work back from review | `pr` is the same reviewable, a revision being a head commit; `add x/pr/reviews '{of:2 … verdict:fail}'`; the ticket stays in `review`; round derives from the count of fails; the default profile's `merge/entry` requires `pr/verdict=pass` |
| close without finishing, with a reason | terminal columns with an `entry` requirement for `status/of` or `status/reason`, fields the move itself carries: `set x/status '{column:wontfix reason:"…"}'` is one transition, and the next move replaces its fields; the default profile's are in [`default.toml`](../internal/assets/workspace/default.toml) |
| park, reopen | a transition into `initial` along the universal park edge |
| finish a container | derived: a node's stage is terminal when every child's is |
| tell an agent what to do now | the handler bound `in <column>`, which fires on `get x` and on `/next` |
| nudge without blocking | a requirement with `severity = "warning"`; a handler |
| customize the process per project or per node | sections of `.lit/workspace.toml`, at the root or keyed by a node's id; nearest ancestor wins per column and per handler; reviewed in a pull request like any change to how the team works |
| add a field, a level name, a link type | the same file: `[levels.1.fields.points]`, `[levels.2] name`, `[links.tests]`; no code |
| see why a ticket is where it is | `get x/history`; `get x/held`, which names each unmet test and the check it came from when it has one |
| undo | `set` back from the echo, or `get x/body/versions/N | set x/body -` |
| find things | selectors over any field, at any depth, across every node's dialect of a column name; below an item, over its comments and history; `--sort`, `--desc`, `--limit` |
| say why | `--reason "…"` on any write, carried on every event it mints and printed in `history` beside the change |
| delete and restore | `rm x`, which is `set x/deleted true`; `set x/deleted false`; a deleted item matches a selector only when it names `deleted` |
| take the backlog out, and bring it in | `lit2 admin export`: every event as one JSON document naming its schema version; `lit2 admin import`: the document replayed into a store as this checkout's writes, each carrying its source attribution and time as provenance; a create carries its id, so every item keeps its id |
| edit many things at once, safely | selectors and globs expand once; `--dry-run` prints the expansion and the diff; a refusal on one path writes nothing |
| work from several worktrees of one clone | one store under the common dir; each worktree its own checkout, ref and claims |
| work from several machines, offline | per-writer refs; background push and fetch; the fold settles concurrent edits; conflicts are values; `/integrity` lists advisories |
| know the view is stale or the push failed | evaluate compares local refs to remote-tracking refs and reads the last push outcome; one advisory line on read and write |
| file a bug with no clone | mint events, push to `refs/lit/inbox/<id>`; any clone's receive verifies, folds and anchors them |
| erase content | `rm x --purge`, a rewrite; every replica that syncs converges; `gc` reclaims |
| keep the store small and fast | the cache snapshot; compaction as a rewrite; budgets as targets |
| survive a binary that is too old | `INV:preserve-unknown`: unknown events carried and counted; mutations on their subjects refused; `min-reader-version` in the workspace file |
| say who may do what | the principal on every event; capabilities `(verb, path glob, binding)` in the policy; the capability rule refuses at the write; every receiver verifies before accepting |
| hide content from some readers | tiers; every prose field an envelope from its first event; `set x/tier internal`; structure stays visible |
| admit, change, remove a member | `add /members`, `set /members/<id>/role`, `rm /members/<id>`: proposal and approval events; the owner threshold where the change touches owners or widens a role |
| replace a lost key | a succession record, `add /members/<id>/succession <new-principal>` |

## 5. One command, start to finish

Every verb runs this pipeline on every call; the verb and the paths are
values `[LAW:dataflow-not-control-flow]`. Steps 2 through 5 are pure functions
of (events, clock, entropy, argv, writer); the rest is the hearth
`[LAW:effects-at-boundaries]`.

1. **Open.** Read every writer ref head and every open log under the
   common dir, this checkout's and its sibling worktrees'; load the cache
   snapshot keyed by `(fold-version, ref heads, log lengths)`. A miss or a
   torn snapshot is one refold; no lock.
2. **Fold** the events newer than the snapshot, from the sealed segments
   and the open log, verifying each (§3.20).
3. **Parse** the active workspace version the fold names (§3.15) into the
   schema value. A store with no applied version refuses every verb,
   naming `lit2 config commit`; nothing stands in for it.
4. **Address.** Parse each path into a typed address; expand selectors and
   globs once.
5. **Evaluate.** `get` computes its answer with evaluate, handed `now`. A write
   computes a description: the intents it mints, signed, then the requirement
   and capability verdicts judged against the state those intents fold onto,
   so a path the same command writes counts, the derivations, the echo.
   Refusal of any path refuses the whole.
6. **Perform.** Append the intents to this checkout's log, or
   decline because `--dry-run` was given. Update the snapshot.
7. **Speak.** Print the echo and run the handlers for the moments raised;
   under `--json`, one object keyed by path.
8. **Record** the command event: paths, expansion, verdicts, events, handlers
   fired and suppressed, cache hit or refold, timings, `dry_run`, `written`.
   One record per invocation, local, never synced `[LAW:nothing-unseen]`.
9. **Sync.** A write schedules a detached `lit2 admin sync` to seal this
   checkout's log and publish its commit, and to fetch and verify the
   others' (§3.20); a read starts the same when the last fetch is older
   than the debounce.

## 6. The parts, downhill

`[LAW:one-way-deps]` The import order is the `layers` block of
[ARCHITECTURE.md](../ARCHITECTURE.md); this drawing follows it, and the
architecture test fails when the two disagree.

```
 cli        argv → typed command; rendering; exit codes; --json
 guidance   moments → resolved handlers
 verbs      get/set/add/rm on typed addresses; requirements; verb → intents; echo
 address    path grammar; selectors; globs
 evaluate   derivations under the clock or the config: held, expiry, staleness, stage, rollup
 config     the active workspace version (§3.15) → one typed schema value; the field table
 field      field kinds and their value parsers; the built-in fields; the atom; the literal grammar
 fold       events → state; validation engine; rank; holders; conflicts
 events     record, codec, upcasters, ids, signature, envelope
 store      the append-only log; sealing into segments; one commit and one
            ref per writer, inbox refs; rewrites; snapshot; the background
            sync process
 git        go-git, in process, for objects and refs; the user's git binary
            for push and fetch, run only by the background sync process
```

Beside the stack, passed through by every layer: the command event, and the
composition root, the one place a store is opened and the clock, the writer
identity, the clone's signing key, the encryption key with the wrapped
tier keys, and the agent label are read. The checkout id lives in the
checkout's private git dir, `<git-dir>/lit/checkout/id`; key material lives
once per clone under `<git-common-dir>/lit/`, the signing key and the
encryption key in separate files, beside the store's root
`<git-common-dir>/lit/store/` and outside it, never in the store. Tests build the same stack over an
in-memory object store, a fixed clock and a fixed identity.

## 7. Decisions

Decided, with the section that carries each.

- **A. Hierarchy and order.** The tree groups and rolls up and orders
  nothing; levels are configured, with leaves at level 1; lanes live on any
  node, take members from anywhere, and are ranked in one order (§3.4,
  §3.7).
- **B. Language and git library.** Go, with go-git (Apache-2.0) in process
  for objects and refs (§6), and for reading the code repository's HEAD and
  history, never writing it. The first budget run measures
  go-git's object walk on the depth corpus.
- **C. `topic`.** A built-in settable field that groups items across the
  tree and renders in the id (§3.4, §3.2).
- **D. `archived`.** Not a field; a ticket out of the flow is in a terminal
  column, one that should not exist is deleted (tombstone) or purged
  (rewrite).
- **E. Crypto suite.** Go's standard library and its extended crypto
  packages: Ed25519 for signatures, X25519 for key wrapping, each its own
  key and never one converted into the other,
  ChaCha20-Poly1305 for envelopes, HKDF for the commitment subkey, with the
  age file format as the reference design. The suite is replaceable, and
  §3.19 and §8 make that structural: every signature and envelope names its
  suite.
- **F. Synthetic container names.** One home per node level, named in the
  workspace file (§3.4).
- **G. Transport.** The background sync process runs the user's own `git`
  for push and fetch; go-git does no network I/O. The git binary runs the
  credential helpers and honors `~/.ssh/config` and `insteadOf`.

## 8. Constraints on later work

- A new ticket fact that no existing event kind records is an event kind
  plus a fold rule and, if it needs the clock or the configuration, an
  evaluate rule. A new piece of the workspace's model is a key in
  `.lit/workspace.toml`.

## 9. Testing

Tests assert what an agent or a person can rely on after a command, in the
vocabulary of §3, rather than the shape of the code
`[LAW:behavior-not-structure]`; printed output is asserted where an agent
relies on it.

- Layers from events through verbs are pure and are tested in process over
  an in-memory object store, a fixed clock and a fixed identity: given these
  events and this command, this is refused for this reason, or these intents
  are minted and this state results.
- Determinism is tested as agreement between independent folds.
- The scenarios in [scenarios.md](scenarios.md) are the acceptance contract:
  each ends in what the agent sees and can do next.
- The budgets in [event-store/budgets.md](event-store/budgets.md) run in the
  harness as targets.
- One end-to-end smoke per release: a real repository, a real remote, two
  clones, the background sync.
- Tests use at most five levels of hierarchy.

## 10. Build order

The order is the slices of [build-map.md](build-map.md): the frame of the
house, then the walking skeleton with every layer below it, then
visibility, process, commitment, guidance, sharing, tending, and the surface
in full. The backlog's epics follow that map.
