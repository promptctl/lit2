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

lit2 keeps a repository's backlog as a tree of nodes in a git object database
of its own under the repository's git common dir, adding nothing to the code
repository's refs or objects, synced through the remote the code already
uses, with no server. An agent works it with four verbs on paths: `get`, `set`,
`add`, `rm`. Every write is one or more immutable events appended to the
writing checkout's own ref; the state everyone reads is a deterministic fold
of every checkout's events, so two machines holding the same events agree on
the backlog with nothing to merge. The workspace's model, its levels, fields,
columns, transition requirements, link types, and the guidance it speaks to
an agent, is a file in the repository, applied into the same store as a
version (§3.15); lanes and everything else a tracker holds are events in it,
written with the same four verbs. The engine knows a small fixed set of
things and the content of none of them.

## 2. The substrate and what it can do natively

Every primitive in §3 is chosen because the substrate does it for free. Where
a wanted behavior is not on this list, the design composes it from things that
are, rather than adding a mechanism beside the substrate.

| capability | what it gives | what it cannot give |
|---|---|---|
| append-only, content-addressed events | an immutable record; a history that is the audit log; undo as another event | in-place change: a correction is a new event, a removal is a rewrite |
| one ref per writing checkout, replaced only by its writer | writes that never contend; push and fetch that never merge; no locks anywhere; nothing on the remote grows with commands | coordination before a write: two offline writers can both do a thing |
| a deterministic fold with a total order `(causal-position, writer, event id)` | the same state on every machine from the same events; concurrent contradictions settled identically everywhere | reading a clock, so any "how long ago" question is answered after the fold |
| read-time evaluation with `now` | expiry, staleness, held, computed on every read, never stored, so they settle as facts change | a stored reaction to a condition, which would outlive the condition |
| committed configuration files, versioned by git | the model as data a person edits, reviews in a pull request and diffs; every version addressable by blob hash in the code repository's history | a model that changes without someone's edit |
| rewrites as events | purge, squash, compaction, erasure, converging on every replica | reaching copies that never sync again (charter #7) |
| a git remote as a dumb replicated store | sync through the channel the code already uses; remote filing from a storeless machine | enforcement by the remote; a server of any kind |
| a signature on every event and opaque prose envelopes | every verifier enforces who may write what, with no server; content hidden from readers without the key; policy as events in the same store | hiding that an item exists (the visible skeleton) |

Two consequences shape everything above the substrate. Because the fold has
no clock, every derivation is sorted into one of two homes: the **fold**
(a pure function of events) or **tick** (a pure function of folded state,
config and `now`). And because no write is coordinated, every write is an
**intent** that stays meaningful whatever happened concurrently: `place x
above y`, not `x is at position 4`.

## 3. The primitives

Each primitive is one type, one job, and a stated set of things the engine
reads from it. The content is the workspace's. The engine ships no column
name, no level name, no label; those are the default profile, data a workspace may rewrite `[LAW:types-are-the-program]`.

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
of an epic. A selector may stand at any step of a path and filters that
step's children by their fields: `x/comments/(by=claude_5c44)` is one
session's comments on `x`, and `'(status=backlog)/history/(by=claude_5c44)'`
its edits across the backlog. A selector matches items in every column and at
every level, and a deleted item only when it names `deleted`. `text` is the
one field nothing stores: `text~re` reads an item's prose fields and its
comments' bodies together, as display renders them. Matches print in backlog order, lanes by rank and
members in lane order; `--sort <path>` orders them by a scalar field, `--desc`
reverses, and `--limit N` keeps the first N. `set` and `add` both take `path
value` pairs; a list value on `add` appends each element. `@` in a value
refers to an item. Where an id is expected it is the id itself, and inside
one `add`, `@N` names the Nth object the value declares, counted in document
order through nested `children`, so items filed together can link to each
other: `add m2q/children '[{title:"parser"}, {title:"renderer"
blocks:[@1]}]'`. Every id is minted before any field resolves, so a forward
reference is fine; `@N` past the count is refused at parse naming the count,
and a self or cross-level reference is refused by the checks every link
meets. In prose the same sigil is a stable spelling of an id. A write
normalizes `@` followed by an id in any form parsing accepts, or `@N` inside
an add, to `@<segments>` before the prose is sealed, refuses one that names
no item as `blocks` would, takes `@@` as a literal `@`, and leaves every
other `@` alone, so an email or a decorator passes untouched. Display
renders `@<segments>` as `@` plus the item's current rendering (§3.2), sigil
kept, so a body still names the item the day after its topic changes, and an
agent that reads a body, edits it and sets it back writes a reference, not
dead text. A reference carries no type and no direction and is not a link
(§3.10); `text~` reads the rendered prose, so a mention is found by the
item's current topic as well as its segments.

### 3.2 Event and intent

An event is `(type, schema-version, subject, payload, attribution, config-ref,
policy-version, causal-position, recorded-at, signature-slot)` as the
event-store design defines it; the signature covers the header, which holds
a salted hash of the payload, so a rewrite replaces or drops the payload and
the signature, the event id and every name stay. The verb layer compiles every write into intent events; the four
verbs are the interface, the events are the memory, and the two are not the
same shape:

| on the command line | events minted |
|---|---|
| `set x/title "…"` | `field.set {item, field, value, over}`, `over` the candidates the writer saw and replaces |
| `set x/status review` | `transition {item, from, to}` |
| `add x/labels ready` / `rm x/labels/ready` | `label.add {item, label}` / `label.rm {item, label, observed}`, `observed` the adds the writer saw |
| `set x/labels '[a, b]'` | one `label.rm` per element removed, one `label.add` per element added |
| `add x/comments "…"` | `comment.add {item, id, body}` |
| `add <node>/lanes/datetime '[dee, b64]'` | `lane.create`, then `lane.place {lane, item, after}` per member |
| `set <node>/lanes/datetime '[b64, dee, fw9]'` | one `lane.place` per member whose predecessor changed |
| `add x/checklist '["…", "…"]'` / `set x/checklist/<id>/state done` | `checklist.add {item, id, text}` / `checklist.set {item, id, state, reason}` |
| `set <lane>/rank '{above: y}'`, `'{below: y}'`, `top`, `bottom` | `rank.place {lane, above \| below \| top \| bottom}` |
| `set /columns/impl/entry '[…]'` | no event: an edit to `.lit/workspace.toml` in the working tree, echoed as such |
| `rm x`, which is `set x/deleted true` | `field.set {item, deleted, true}`, a reversible tombstone; `set x/deleted false` restores |
| `add <lane>/claim` / `rm <lane>/claim` | `claim.take {lane}` / `claim.release {lane}` |
| any write with `--reason "…"` | the reason on every event the write mints, an enveloped prose field |
| `rm x --purge` | a rewrite naming the item |

Ids are opaque. An id, an item's or an element's such as a comment's, is 128
random bits minted at its create and carried in the create event; the fold
reads it from there, so where the event lands in a log is no part of
identity, and a create replayed into another log keeps its id. An id is rendered by its **segment**, a short form
fixed at mint: the shortest prefix of the id that no id this writer has
folded shares, as wide as the namespace's fill needs, so a later item cannot
make yesterday's `1f9` ambiguous; the later mint goes wider instead. Two
writers offline can fix one segment for two items; after sync that segment
resolves to neither, lit2 lists both, and each item keeps its id. A child
renders as `<parent>.<segment>`, `1f9.zc7`, from its parent link, so where
an item sits is display, not identity. The project prefix and the item's
`topic` (§3.4) are not part of the id either: the rendering is
`<prefix>-<topic>-<segments>`, `lit-bootstrap-1f9.zc7`, the prefix a label
the active version (§3.15) supplies and the topic the item's own field, so a
reader can tell otherwise arbitrary segments apart and changing either
rewrites nothing; a reference in prose is stored as `@<segments>` and
rendered the same way (§3.1), so prose follows a rename too. Parsing accepts the segments bare, or under any prefix a
version in this store's log has ever declared and any topic the item has ever
carried, and refuses a prefix none has, naming it as another project's, or a
topic the item never had, naming its current rendering.
The total order is `(causal-position, writer, event id)`: the event id, the
record's content hash, settles two events of one writer at one position.

### 3.3 Fold and tick

`fold(events) → state`: items, fields, comments, labels, links,
lanes, their order and their rank, checklists, lane holders, the revisions
and reviews of each reviewable (§4), the applied workspace versions (§3.15),
and every
unresolved concurrent contradiction as a value on the thing it concerns.
`tick(state, config, now) → view`: `held`, claim
expiry, staleness, stage, rollup, the labels declared with `where` (§3.11). Every read is `tick(fold(events), now)`;
every write is evaluated against the same. Neither function performs I/O.
The fold reads no configuration: an event's legality under the model was
judged by the one enforcer at its write, under the active version its writer
ran under, and tick applies the active version its reader ran under, which is
where a column or field that version no longer declares is reported (§3.5).

Determinism is a behavior: two folds of one event set, in any arrival order,
from genesis or from a snapshot, yield equal state. It is tested as that
equality, through the state's own equality and never through a pinned
serialization `[LAW:behavior-not-structure]`.

### 3.4 Work item, level, tree

A **work item** is a node of the tree at a **level**. The workspace file
declares the levels, one to five, numbered from 1 at the bottom, each with
a name; the default profile ships `story`, `feature`, `epic`. Code knows
numbers and roles, never names. An item at level 1 is a **leaf**: it has a
stored column, is claimed through its lane, carries a checklist, and is what
`/next` serves. An item above level 1 is a **node**: it derives `stage` from
its children (`terminal` when all are, `initial` when all are or it is
empty, else `active`) and `rollup`, the count of leaves per column, and is
finished or reopened only through its children. Leaf or node is decided by
the level, never by whether children exist. A one-level workspace is a flat
list of leaves under the root.

Containment is exact: a parent is one level up, and the root's children are
top-level items. Every node level names a synthetic home in the workspace
file (`unfiled` at level 2, `unassigned` at level 3 in the default profile),
so an item created without a parent, `add /children '{level: 1, …}'`, lands
in the home above it and that home in the home above, and every leaf has a
full ancestry. The homes are shown, never hidden, and nothing in the engine
handles a parentless case. `add <parent>/children` creates at the level
below the parent; `add /children` with no level creates at the top; a child
object may itself carry `children`, minted at the level below it, so a node
and its tree are one call. `set x/parent y` moves an item: `y` must be one
level up and its resolved column graph must declare `x`'s column, or the
write is refused naming which.

Every item carries four built-in fields beside its title and prose. `id`,
derived, its own id. `level`, derived, which a selector reads by number or by
the active version's name for it, `(level=2)` or `(level=epic)`. `topic`,
settable, one short word that groups items across the tree: the create
supplies it or lit2 derives one from the title and echoes the derivation, and
`(topic=cli)` selects on it; a child's default is its parent's topic, a
top-level item's is derived from its title, and the topic renders in the id
(§3.2). `deleted`, settable: `rm x` is `set x/deleted true`, a reversible
tombstone, and `set x/deleted false` restores the item.

The tree groups, scopes and rolls up. It orders nothing: no item has a rank,
and a node is a folder. Order is the lane's (§3.7). The backlog is the
rank order of lanes; `get /lanes` prints it.

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

A resolved column graph is checked when a version is applied (§3.15): one
`initial`, at least one `terminal`, every `next` target present, no column
removed while a ticket in its subtree holds it. Two concurrent writers can
still produce a ticket in a column its ancestors no longer declare; the fold
keeps the transition, tick reports the column as undeclared with stage
`active`, `/next` passes the lane over naming the reason, `/integrity` lists
it, and the universal edges always offer a way out. Any node may carry `columns`;
a ticket resolves its graph by nearest ancestor, per column name, so an epic
adds an `audit` column by declaring it and pointing `plan/next` at it.

### 3.6 Requirement

A **requirement** is one entry of a column's `entry` or `exit` list: a `test`
and a `severity` (`error` or `warning`, no default). A test is one selector
atom over one path of the ticket with a `scope` (`self` or `ancestors`), or
the name of a **check**: a named list of tests under `[checks.<name>]`. The
atoms are `p`, `!p`, `p=v`, `p!=v`, `p~re`, and `p<v`, `p>v` on a number or
date path. `v` may be several values, `status=backlog,impl`: `=` holds when
any matches and `!=` when none. A date value is a date or time, or a duration
before the evaluation's `now`, `updated<-6h`; every list exposes `count`, a
derived number, so `comments/count>2` is an atom too. `Parse` expands every check name, so the list the
enforcer reads is flat, and each test keeps the name of the check it was written
in. One pure function evaluates the flat list at exactly one place, the
transition write: the source's `exit` list on a forward edge, the destination's
`entry` list on every arrival, all of them, so a refusal names everything unmet
at once `[LAW:single-enforcer]`. `error` refuses and writes nothing; `warning`
prints and proceeds. The same evaluation, run as a dry run of each forward edge,
is the derived field `held`.

Anything a workspace wants to require is a fact something wrote (a field, a
label, a review) or a derived field the engine exposes (`blockers`,
`plan/verdict`, `pr/verdict`), read by a requirement. The test grammar does not
grow.

### 3.7 Lane

A **lane** is an ordered list node, `<node>/lanes/<name>` on any node
including the root, whose members are leaves from anywhere in the tree; its
order is sequence, and a member's `blocked_by` derives from the member
before it. A leaf is in at most one lane and its `lane` field is derived
from membership; a leaf in no declared lane is its own one-member lane, so
parallel costs nothing and serial is one `add`. A node worked in sequence is
a folder holding one lane; a sequence that crosses nodes is a lane whose
members do.

**Rank is on lanes and on nothing else.** There is one rank order, over
every lane in the workspace, folded from `rank.place {lane, above | below |
top | bottom}` intents; a new lane ranks last; reranking a lane moves its
members together, in order. An intent whose anchor no longer exists when it
arrives places the subject at the bottom and is named on `/integrity`. A
lane is identified by its name on its node; `lane.create` is idempotent on
that pair, so two offline writers who create the same lane fold to one lane
whose members are placed in total order, with an advisory. The lane is also
the unit a checkout holds (§3.8). Its head is its first member whose stage
is not `terminal`. `/next` is the head of the lane this checkout holds, else
the head of the top-ranked lane (§4).

### 3.8 Claim

A **claim** is derived, never written. A lane is held by the checkout that
made its latest establishing transition, out of `initial` or into a
`terminal`, on any member; the fold computes the holder set in total order,
`claim` is the last, and `contested` is the rest. Release is a write,
`rm <lane>/claim`, which the fold reads as ending the hold until the next
establishing act; taking is one too, `add <lane>/claim`, which the fold reads
as this checkout's establishing act, so work another checkout left in the
middle of a column changes hands in one echoed write; parking a member into
`initial` and finishing the lane's last member end a hold too. Expiry is tick's: `claim_ttl`, resolved by
nearest ancestor, measured from the holder's newest event on any member; an
expired claim is no claim and prints as none. A ticket's `claim` reads its
lane's, so finishing one member hands the checkout the next with no second
action and no event on the next ticket.

### 3.9 Checklist

A leaf carries a **checklist**, `x/checklist`: items with a minted id, a
text, and one of three states, `pending`, `done`, `abandoned` with an
optional reason. Checklist items are not work items: no level, no lane, no
claim, no link, no column. `done` and `abandoned` are final and there is no
delete, so a finished leaf shows six months later exactly what was done and
what was abandoned and why. `add x/checklist '[…]'` mints items;
`set x/checklist/<id>/state done` moves one; `set x/checklist '[…]'`
replaces the list and the verb layer diffs it: items whose text it knows
keep their id and state, new text is added, vanished text is abandoned,
which is what lets a harness hook push an agent's whole TODO list on every
change with no agent effort. The fold merges per item and a final state
wins over `pending`. No event per check is folded into history beyond the
item events themselves; the leaf's transitions carry the counts. The
column may require `checklist/pending = 0` on entry, so finishing a leaf with
pending items is refused and names them; the shipped default does not.
Item text is prose and an envelope (§3.19); the `set` diff matches on the
commitment.

### 3.10 Link

A **link** is a typed edge between items. The engine knows two kinds and no
more: `dependency`, directed and acyclic, read by `/next` and `blocked_by`;
`relates`, a reference with no scheduling meaning. A workspace declares link
types under `/links` with a kind and the `[from, to]` level pairs they may
join, adds one with a new name, and cannot add a kind. The default profile
joins same-level items with `blocks` and any pair with `relates`; a node's
stage is derived, so a dependency on a node is a dependency on its leaves.

### 3.11 Label

A **label** is a name a person applies, `add x/labels ready`, declared in the
workspace file's top-level `labels` list before anything uses it: the list is
the value set of the built-in `labels` field, an enum that holds many values.
An item carries exactly the labels written to it. A label reads as
`labels=<name>` in a selector or a requirement, and a test naming an undeclared
label is refused at parse. A stored label that a later version stops declaring
stays stored, and `/integrity` reports it. What the facts already say is a
selector, not a label: `get '(stage=active updated<-6h)'` is the orphaned
tickets. The shipped default declares `ready`, `needs-design` and `external`.

### 3.12 Moment and handler

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

### 3.13 Rewrite

A **rewrite** is an event that names the events it drops, the payloads it
replaces, or the ranges it squashes. The fold applies it on arrival; each
writer then re-seals its segments without the rewritten material and
replaces its ref (§3.16), and a receiver accepts the new tree when the
events it lacks are the ones rewrite events name. Purge is the rewrite that drops one item's events; the fold
also drops events on that item that arrive after the purge from writers who
had not seen it, and counts them on `/integrity` with their writer, so a lost
comment is named to the one who wrote it. Compaction is the rewrite that
squashes. Nothing else removes anything.

A squash, wherever it runs, keeps and drops by address and nothing else. Of
the writes to one register address in a window, the last survives at its own
position and the rest go; adds and removes on one set address net out to the
last or to nothing; an appended element is a new address, so every revision
and every review survives. A window is bounded by the item's create and its
transitions: the create is kept, and a transition is never squashed and
nothing squashes across one, so the state an item was in when it moved stays
as the enforcer judged it. Positions are
deleted, never renumbered.

### 3.14 Identity

Every event's attribution is `(checkout, principal, agent)`. The checkout id
is the 128-bit token in the checkout's private git dir, the thing claims and
writer refs key on. The principal is the key that signed the event (§3.17),
minted at init and kept once per clone in the common dir, beside the store
and never in it (§6). The agent label is an optional opaque string the
invoking harness supplies, shown beside comments and history. No usernames, hostnames or paths enter
the store; display names come from the policy's member entries. `get /me`
prints the three and the tiers this principal holds.

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
names a committed blob and refuses a dirty or untracked file; a squash merge
changes no blob id, so how the file reached its branch is immaterial. Apply
refuses a version that does not parse, or whose column graph fails the check
of §3.5, naming every fault with its key path, and nothing overrides that
refusal; a later lit2 is expected to say which edit would clear each fault, or
to make it. A draft in the working tree is not in force until it is applied,
so a draft that does not parse stops no command. `Parse` reads one version's
bytes into one typed schema. `lit2 admin init` writes the default profile,
`internal/assets/workspace/default.toml`, to `.lit/workspace.toml` when no
file exists, and applies the same bytes as version one at init, right after
the enrollment (§3.17), so the file and the active version share one blob id
from the start; an existing file is applied instead, and must parse. Every
later event can rely on an active version existing.

Every event carries `config-ref`, the git blob id of the active version its writer
ran under, in the code repository's object format. It is provenance: it names the
version the enforcer judged the write under, and it resolves from the repository's
history once that version is committed. The fold reads no configuration (§3.3), so
nothing but the id travels with the events. Two people who edit the file
concurrently meet in a git merge, which is where people already resolve concurrent
edits to a file. Two applies that name one parent are a conflict value on the
workspace in the fold, resolved by an apply that names both: git merges the
file, lit merges which version is in force. The file and the active version
drift apart whenever one is edited or the other applied; `config status`
reports the drift, and whether the active blob is in HEAD's history, and the
board carries one line for it. No other command mentions it.

What belongs in the file is decided by one test: a value is the workspace's
when two machines that disagree about it cannot both be right about the
backlog, and a person's own when they can. A level's name, which fields exist,
a column's requirements, which labels are declared, a link type's legal pairs,
`claim_ttl`, `min-reader-version`: each decides what a write may do or how a
fact reads, so each is in the file and in force only once applied. How a
machine syncs, caches and speaks is its own, and stays in that person's
configuration; the record it produces is in the store either way. A new
setting is classified at birth by the same test. The model is one document
rather than one table per concern because `Parse` then has a single boundary,
the applied version has the one blob id that `config-ref` names, and the
person edits one file in a syntax they already know.

Workflows have three layers and nothing else does: shipped in the binary, the
person's own `$XDG_CONFIG_HOME/lit/config.toml` across every repository, and
the project's workspace file. Nearest wins per field.

### 3.16 Log, segment and ref

A writer's events are one append-only **log** under the common dir, one
record per event in the order minted. A command appends to it and touches
nothing else in the store; a crash mid-append loses at most the record being
written. Sync first squashes this checkout's unpublished events by the rule
of §3.13, which changes nothing already published and so is not a rewrite,
then seals the log into **segment** blobs of bounded size and
publishes one commit per writer: its tree holds the writer's segments. The ref
`refs/lit/log/<checkout-id>` is replaced on each push, never advanced,
because only its writer updates it; the events are the history, the ref has
none. So the remote holds, per live writer, one commit, one tree and a few
segment blobs, and a push sends the delta
of the open segment. Object count tracks writers and segments; the number of
events never reaches git. An inbox ref is the same shape with one segment
and no checkout behind it. The fold reads sealed segments plus the open log,
and the snapshot remembers, per writer, the segment and offset it has
consumed, so an incremental fold reads only what was appended. Compaction
(§3.13) is what bounds a segment set over a project's life: a squash
re-seals history into fewer, smaller segments on every replica.

### 3.17 Principal and policy

A **principal** is a signing key of the crypto suite (§7 E). Its fingerprint is the principal id, the
only identity form that enters the store; names live in the policy's member
entry. `lit2 admin init` mints the checkout's principal silently and prints
one recovery phrase. Every event is signed by its principal over the header
and a salted hash of the payload (§3.2). A **succession** record links an
old principal id to a new one: signed by the old key and one `member.admin`
when the old key still exists, by the owner threshold when it is lost.

The **policy** is the member list (principal, role, and the wrapped tier
keys per member), the role definitions, and a monotone version. It lives in
the store as events: `policy.propose` carries the new document and
`policy.approve` cites its hash; a version activates at the causal position
where the approvals the previous version requires are all present. A change
that touches the owner set or widens any role's capabilities requires the
owner threshold, one while there is one owner and a majority once there are
more, with the owner being removed excluded from the quorum that removes
them; every other change needs one `member.admin` or `policy.admin`
signature. The verifier decides which case applies by diffing the granted
surface, never by the change's framing. The first policy is the
**enrollment**, the first event of a workspace, signed by its founding
owner; verification seeds there and never walks before it. A checkout that
joins a remote already holding an enrollment adopts it and pushes a signed
membership request; two enrollments meeting on one remote is a
contradiction the fold reports, and the later one in total order yields.

### 3.18 Capability

A capability is `(verb, path glob, binding)`: one of the four verbs, a glob
over paths, and a binding of `own`, `assignee`, `own-or-assignee` or `all`.
`own` binds to the item's creating principal; `assignee` to the principal
holding its lane's claim. A role is a named bundle of capabilities, and the
default profile ships the access-control design's four, observer,
contributor, editor and owner, spelled as path globs: contributor is
`add */children`, `set own/*`, `set */status` out of the initial column,
`add */comments`, `rm own/*` at level 1, the leaf. The enforcer that judges a
transition also judges, for every intent a command would mint, the writer's
principal against the policy version in force at the writer's current fold;
refusal of any path refuses the whole. Membership is paths too:
`add /members '{principal, role}'`, `set /members/<id>/role editor`,
`rm /members/<id>`, `set /policy/threshold 2`, each compiling to a proposal
and, where the writer's own signature suffices, its approval.

### 3.19 Tier and envelope

Every prose field is an **envelope** `{suite, tier, epoch, nonce,
ciphertext, commitment}` from its first event, and no layer below the verbs ever sees
prose (`INV:opaque-envelope`). A **tier** is a named visibility group with
an epoch key wrapped to each member's public key in the policy. A workspace
starts with one tier, encrypted, every member entitled; a project that
wants its backlog world-readable sets the default tier to plaintext, the
documented public mode. Each item and each comment carries `tier`, a
structural field, a comment's defaulting to its item's. Each encrypted item
and comment has its own content key wrapped under its tier's epoch, so
`rm x --destroy` shreds the key and the content is unrecoverable on every
replica. Granting a tier wraps all its epochs to the joiner; removing a
member rotates the tier to a new epoch for the survivors, and the removed
member keeps what they could already read. Structure stays visible to
every member: the backlog lists every item and redacts what the reader
cannot open. The commitment lets the fold decide that two concurrent
writes of one prose field are the same text without reading either.

### 3.20 Verification

The model is Monero's, as the access-control charter fixes it: validity is
enforced by every verifier and by no trusted party, a reading key is
separate from a writing key, and the person uses none of it knowingly.
Authorization is one fold rule. For each event in total order the fold
checks the signature against its principal's key and the intent against
the policy in force at the event's causal position; an event that fails is
carried, counted on `/integrity` with its writer, and never applied, and an
event whose cited policy version is lower than its writer's previous one
fails the same way. Every receiver runs that rule over a writer's published
segments before accepting them, so a forged or unauthorized publication
never reaches a replica's fold and the local copy stays; a valid replica
re-publishes over a vandalized remote. A revoked principal that stays
offline from its revocation can mint events whose positions sort before it;
the fold applies them, as it applies any offline work made before the news
arrived, and reports them with their `recorded-at` so an owner can judge
and rewrite. That residual is the seam the carried design leaves open,
resolved here as a deterministic rule plus an advisory rather than a rule
keyed to what a receiver happens to know.

## 4. Functionality, composed

What an issue tracker must do, and which primitives do it. The test for each
row is that no primitive learned a new case.

| the tracker must | composed from |
|---|---|
| file work, alone or in bulk, with defaults | `add <parent>/children '{…}'` or a list of them, at the level below the parent; the level's template fills absent fields and echoes each as a derivation |
| keep work out of the queue until it is ready | the initial column's `exit` list: a description present, `labels=ready` on the ticket or an ancestor, no blocker; `held` reports it, `/next` reads `held` |
| say what to work on | `/next`: the heads of the lanes this checkout holds, in rank order; a held head whose own `held` is non-empty is printed with its reasons and passed; then the first lane head in rank order that is not held elsewhere and whose `held` is empty, labeled as outside the commitment; every lane passed over is named with its reason |
| commit a checkout to a unit of work | the claim on the lane, derived from the start transition; `/next` serves the held lane first; leaving is `rm <lane>/claim`, taking over is `add <lane>/claim` |
| pick work inside one node | `<node>/next`: the same walk over the lanes whose head is under the node |
| sequence work | a lane, on any node, with members from anywhere; reorder with one `set` on the list; order lanes against each other with `set <lane>/rank` |
| track the steps inside one ticket | the checklist on the leaf; `checklist/pending = 0` on entry to a terminal column |
| make work wait on other work | a `blocks` link; `blockers` and `blocked_by` derive from links and lane position together |
| interrupt for an urgency | `priority: urgent` on a ticket; `/next` serves an urgent head ranked above the held lane |
| move work through a process | transitions along `next`; requirements on the edges; stage gives start and done |
| review a plan before code | `plan` is a **reviewable**: two append-only lists, `revisions`, each carrying its content, and `reviews`, each naming the revision it judged and carrying how it was run, one line, the reviewer's result, the agent's response, and its verdict. `add x/plan/reviews '{of:3 how:"…" result:"…" response:"…" verdict:pass}'`, `of` required. `plan/verdict` is the verdict of the last review naming the latest revision; `impl/entry` requires `plan/verdict=pass` |
| send work back from review | `pr` is the same reviewable, a revision being a head commit; `add x/pr/reviews '{of:2 … verdict:fail}'`; the ticket stays in `review`; round derives from the count of fails; `merge/entry` requires `pr/verdict=pass` |
| close without finishing, with a reason | terminal columns `dup`, `superseded`, `obsolete`, `wontfix`, each with an `entry` requirement for `of` or `reason` |
| park, reopen | a transition into `initial`, reachable from everywhere; the claim ends |
| finish a container | derived: a node's stage is terminal when every child's is |
| tell an agent what to do now | the handler bound `in <column>` prints after the ticket on `get x` and on `/next` |
| nudge without blocking | a requirement with `severity = "warning"`; a handler |
| customize the process per project or per node | sections of `.lit/workspace.toml`, at the root or keyed by a node's id; nearest ancestor wins per column, per handler field; reviewed in a pull request like any change to how the team works |
| add a field, a level name, a link type | the same file: `[levels.1.fields.points]`, `[levels.2] name`, `[links.tests]`; no code |
| see why a ticket is where it is | `get x/history`; `get x/held`, which names each unmet test and the check it came from when it has one |
| undo | `set` back from the echo, or `get x/body/versions/N | set x/body -`; every version is in the history |
| find things | selectors over any field, at any depth, across every node's dialect of a column name; below an item, over its comments and history; `--sort`, `--desc`, `--limit` |
| say why | `--reason "…"` on any write, carried on every event it mints and printed in `history` beside the change |
| delete and restore | `rm x`, which is `set x/deleted true`; `set x/deleted false`; a deleted item matches a selector only when it names `deleted` |
| take the backlog out, and bring it in | `lit2 admin export`: every event as one JSON document naming its schema version; `lit2 admin import`: the document replayed into a store as this checkout's writes, each carrying its source attribution and time as provenance; a create carries its id, so every item keeps its id |
| edit many things at once, safely | selectors and globs expand once; `--dry-run` prints the expansion and the diff; a refusal on one path writes nothing |
| work from several worktrees of one clone | one store under the common dir; each worktree its own checkout, ref and claims |
| work from several machines, offline | per-writer refs; background push and fetch; the fold settles concurrent edits; conflicts are values; `/integrity` lists advisories |
| know the view is stale or the push failed | tick compares local refs to remote-tracking refs and reads the last push outcome; one advisory line on read and write |
| file a bug with no clone | mint events, push to `refs/lit/inbox/<id>`; any clone's receive folds and anchors them |
| erase content | `rm x --purge`, a rewrite; every replica converges; `gc` reclaims |
| keep the store small and fast | the cache snapshot; compaction as a rewrite; budgets as gates |
| survive a binary that is too old | `INV:preserve-unknown`: unknown events carried and counted; mutations on their subjects refused; `min-reader-version` in the workspace file |
| say who may do what | the principal on every event; capabilities `(verb, path glob, binding)` in the policy; the one enforcer refuses at the write; every receiver verifies before accepting |
| hide content from some readers | tiers; every prose field an envelope from its first event; `set x/tier internal`; structure stays visible |
| admit, change, remove a member | `add /members`, `set /members/<id>/role`, `rm /members/<id>`: proposal and approval events; the owner threshold where the change touches owners or widens a role |
| replace a lost key | a succession record, `add /members/<id>/succession <new-principal>` |

Three functions the source documents asked about and this table answers
without a new mechanism: "ready to pull" is the initial column's exit list;
"rework" is a failing review inside `review`; "orphaned" is the selector
`(stage=active updated<-6h)`.

## 5. One command, start to finish

Every verb runs this pipeline on every call; the verb and the paths are
values `[LAW:dataflow-not-control-flow]`. Steps 2 through 5 are pure functions
of (events, clock, argv, writer); the rest is the hearth
`[LAW:effects-at-boundaries]`.

1. **Open.** Read every writer ref head and every open log under the
   common dir, this checkout's and its sibling worktrees'; load the cache
   snapshot keyed by `(fold-version, ref heads, log lengths)`. A miss or a
   torn snapshot is one refold; no lock.
2. **Fold** the events newer than the snapshot, from the sealed segments
   and the open log, verifying each (§3.20).
3. **Parse** the active workspace version the fold names (§3.15) into the
   schema value.
4. **Address.** Parse each path into a typed address; expand selectors and
   globs once.
5. **Evaluate.** `get` computes its answer with tick and the clock. A write
   computes a description: the intents it would mint, the requirement and
   capability verdicts, the derivations, the echo. Refusal of any path
   refuses the whole.
6. **Perform.** Sign and append the intents to this checkout's log, or
   decline because `--dry-run` was given. Update the snapshot by atomic rename.
7. **Speak.** Print the echo, then the handlers' bodies for the moments
   raised, after the data; under `--json`, one object keyed by path.
8. **Record** the command event: paths, expansion, verdicts, events, handlers
   fired and suppressed, cache hit or refold, timings, `dry_run`, `written`.
   One record per invocation, local, never synced `[LAW:nothing-unseen]`.
9. **Sync.** A write schedules a detached `lit2 admin sync` to seal this
   checkout's log and publish its commit, and to fetch and verify the
   others' (§3.20); a read starts the same when the last fetch is older
   than the debounce. Nothing in the foreground waits on the network.

## 6. The parts, downhill

`[LAW:one-way-deps]` Each layer depends only on the layers below it. The
order the architecture test enforces on every import is the `layers` block
of [ARCHITECTURE.md](../ARCHITECTURE.md); this drawing follows it, and the
same test fails when the two disagree.

```
 cli        argv → typed command; rendering; exit codes; --json
 guidance   moments → resolved handlers → text after the data
 verbs      get/set/add/rm on typed addresses; requirements; verb → intents; echo
 address    path grammar; selectors; globs; the field table
 tick       clocked derivations: held, expiry, staleness, stage, rollup
 config     the active workspace version (§3.15) → one typed schema value
 field      field kinds and their value parsers; the built-in fields; the atom
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
identity, the principal's key with its wrapped tier keys, and the agent
label are read. The checkout id lives in the checkout's private git dir,
`<git-dir>/lit/checkout/id`; key material lives once per clone at
`<git-common-dir>/lit/principal`, beside the store's root
`<git-common-dir>/lit/store/` and outside it, never in the store and never
behind a prompt. Tests build the same stack over an
in-memory object store, a fixed clock and a fixed identity.

## 7. Decisions

Decided, with the section that carries each. Everything not listed is
decided in the body.

- **A. Hierarchy and order.** The tree groups and rolls up and orders
  nothing; levels are configured, one to five, with leaves at level 1;
  lanes live on any node, take members from anywhere, and are the only
  thing ranked, in one order (§3.4, §3.7). This is
  [path-addressed-cli.md](path-addressed-cli.md) §Lanes as written, with the
  v1 hierarchy epic's three-level cap dropped: that cap existed to keep
  scheduling frames from nesting, and nothing in the tree schedules.
- **B. Language and git library.** Go, with go-git (Apache-2.0) in process
  for objects and refs (§6). The first budget run measures go-git's object
  walk on the depth corpus.
- **C. `topic`.** A built-in settable field, one short word that groups
  items across the tree and renders in the id, kept from v1 because a
  reader cannot tell `1f9.zc7` from `2ae.k3f` without it: the create
  supplies it, or a child takes its parent's and a top-level item one derived
  from its title, and `set x/topic` changes it. The id is the segments, so
  editing a topic rewrites nothing and an old rendering still parses (§3.2).
- **D. `archived`.** Not a field; a ticket out of the flow is in a terminal
  column, one that should not exist is deleted (tombstone) or purged
  (rewrite).
- **E. Crypto suite.** Go's standard library and its extended crypto
  packages: Ed25519 for signatures, X25519 for key wrapping,
  ChaCha20-Poly1305 for envelopes, HKDF for the commitment subkey, with the
  age file format as the reference design. The suite is replaceable, and
  §3.19 and §8 make that structural: every signature and envelope names its
  suite. FIPS-validated modules are a packaging question for the
  organizations that need them, not a design one.
- **F. Synthetic container names.** One home per node level, named in the
  workspace file: `unfiled` at level 2 and `unassigned` at level 3 in the
  default profile (§3.4).
- **G. Transport.** The background sync process runs the user's own `git`
  for push and fetch; go-git never touches the network. The git binary
  runs the credential helpers, honors `~/.ssh/config` and `insteadOf`, and
  speaks every host's dialect, which is what v1 does today and why its
  authentication works. Event-store charter #4 admits this: its reasons,
  licensing and foreground latency, do not reach a background push through
  the user's git, since nothing is linked and no foreground path waits on it.
- **H. Configuration and the fold.** The fold reads no configuration;
  `config-ref` is provenance (§3.3, §3.15).

## 8. Constraints on later work

- A new ticket fact is an event kind plus a fold rule and, if it has a clock,
  a tick rule. A new piece of the model is a key in `.lit/workspace.toml`.
  Never a table, never a second store.
- Every mutating verb on a path has a capability `(verb, path glob,
  binding)`; what cannot be spelled as a path write cannot be authorized,
  and a path write with no capability does not ship.
- Every prose field is an envelope below the verb layer from its first event.
- Every signature and every envelope names its crypto suite, and the suite
  is one value behind one interface in the events layer, so a second suite
  coexists with the first and a policy version can name the suite new
  events must use.
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

The order is the slices of [build-map.md](build-map.md): the frame of the
house, then the walking skeleton with every layer below it in its final
shape, then visibility, process, commitment, guidance, sharing, tending, and
the surface in full. The backlog is a view of that map.
