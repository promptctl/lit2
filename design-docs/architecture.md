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
| one ref per writing checkout, replaced only by its writer | writes that never contend; push and fetch that never merge; no locks anywhere; nothing on the remote grows with commands | coordination before a write: two offline writers can both do a thing |
| a deterministic fold with a total order `(causal-position, writer)` | the same state on every machine from the same events; concurrent contradictions settled identically everywhere | reading a clock, so any "how long ago" question is answered after the fold |
| read-time evaluation with `now` | labels, expiry, staleness, held, computed on every read, never stored, so they settle as facts change | a stored reaction to a condition, which would outlive the condition |
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
| `add <node>/lanes/datetime '[dee, b64]'` | `lane.create`, then `lane.place {lane, item, after}` per member |
| `set <node>/lanes/datetime '[b64, dee, fw9]'` | one `lane.place` per member whose predecessor changed |
| `add x/checklist '["…", "…"]'` / `set x/checklist/<id>/state done` | `checklist.add {item, id, text}` / `checklist.set {item, id, state, reason}` |
| `set <lane>/rank '{above: y}'`, `'{below: y}'`, `top`, `bottom` | `rank.place {lane, above \| below \| top \| bottom}` |
| `set /columns/impl/entry '[…]'` | no event: an edit to `.lit/workspace.toml` in the working tree, echoed as such |
| `rm x` | `item.delete {item}`, a reversible tombstone |
| `rm x --purge` | a rewrite naming the item |

Ids are opaque: the project prefix from the workspace file, then a hash of
`(writer, causal-position)`, children as `<parent>.<hash>`. Two offline
writers cannot collide, and a short id resolves by unique suffix.

### 3.3 Fold and tick

`fold(events) → state`: items, fields, comments, labels, links,
lanes, their order and their rank, checklists, lane holders, and every
unresolved concurrent contradiction as a value on the thing it concerns.
`tick(state, config, now) → view`: computed labels, `held`, claim
expiry, staleness, stage, rollup. Every read is `tick(fold(events), now)`;
every write is evaluated against the same. Neither function performs I/O.
The fold reads no configuration: an event's legality under the model was
judged by the one enforcer at its write, under the writer's file, and the
reader's file is applied by tick, which is where a column or field the
reader's model no longer declares is reported (§3.5).

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
below the parent; `add /children` with no level creates at the top.

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

A resolved column graph is checked on every write that changes it: one
`initial`, at least one `terminal`, every `next` target present, no column
removed while a ticket in its subtree holds it. Two concurrent writers can
still produce a ticket in a column its ancestors no longer declare; the fold
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
establishing transition; parking a member into `initial` and finishing the
lane's last member end it too. Expiry is tick's: `claim_ttl`, resolved by
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
default profile's terminal columns require `checklist/pending = 0` on
entry, so finishing a leaf with pending items is refused and names them.
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

### 3.11 Rule

A **rule** is `{where: <selector>, label: <name>}` in `/rules`. Tick evaluates
every rule on every read and the matching tickets carry the label, computed,
never stored, so it is gone the read after its condition stops holding and
needs no merge. The default profile ships one: `blocked` where a blocker's
stage is not terminal.

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

### 3.14 Identity

Every event's attribution is `(checkout, principal, agent)`. The checkout id
is the 128-bit token in the checkout's private git dir, the thing claims and
writer refs key on. The principal is the key that signed the event (§3.17),
minted at init and kept beside the checkout id, never in the store. The
agent label is an optional opaque string the invoking harness supplies,
shown beside comments and history. No usernames, hostnames or paths enter
the store; display names come from the policy's member entries. `get /me`
prints the three and the tiers this principal holds.

### 3.15 The workspace file

The workspace's model is one committed file, `.lit/workspace.toml`: levels and
their fields, columns and their requirements, link types, rules, the project
layer of workflows, `claim_ttl`, the project prefix, `min-reader-version`.
Per-node overrides are sections keyed by the node's id in the same file. A
person edits it in an editor and reviews it in a pull request; git versions
and merges it. The four verbs still read it as nodes, `get /columns`, and a
write such as `set /columns/backlog/exit '[…]'` edits the file in the working
tree and says so in the echo; committing it is the person's act, like any
other file. It never becomes an event.

lit2 runs inside a checkout, and the working tree's copy of the file is the
model, like every tool that reads its configuration from the repository, so an
edit can be tried before it is committed. A repository with no file reads as
the shipped default profile.

Every event carries `config-ref`, the blob hash of the file its writer read,
as provenance: it names the version the enforcer judged the write under,
and it resolves from the code repository's history when that version was
committed. The fold reads no configuration (§3.3), so nothing but the hash
travels with the events, and a version tried in the working tree and never
committed costs nothing and is remembered by nothing but the hash on the
events written under it. The schema parse refuses, at the
write, what the designs say it refuses. Two people who edit the file
concurrently meet in a git merge, which is where people already resolve
concurrent edits to a file.

Workflows have three layers and nothing else does: shipped in the binary, the
person's own `$XDG_CONFIG_HOME/lit/config.toml` across every repository, and
the project's workspace file. Nearest wins per field.

### 3.16 Log, segment and ref

A writer's events are one append-only **log** under the common dir, one
record per event in the order minted. A command appends to it and touches
nothing else in the store; a crash mid-append loses at most the record being
written. Sync seals the log into **segment** blobs of bounded size and
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
| commit a checkout to a unit of work | the claim on the lane, derived from the start transition; `/next` serves the held lane first; leaving is `rm <lane>/claim` |
| sequence work | a lane, on any node, with members from anywhere; reorder with one `set` on the list; order lanes against each other with `set <lane>/rank` |
| track the steps inside one ticket | the checklist on the leaf; `checklist/pending = 0` on entry to a terminal column |
| make work wait on other work | a `blocks` link; `blockers` and `blocked_by` derive from links and lane position together |
| interrupt for an urgency | `priority: urgent` on a ticket; `/next` serves an urgent head ranked above the held lane |
| move work through a process | transitions along `next`; requirements on the edges; stage gives start and done |
| review a plan before code | `plan` is a versioned text node; `add x/plan/verdicts '{pass of:<digest> laws:[…]}'` names the digest the reviewer read, `of` required, refused if the plan has moved since; `impl/entry` requires `plan/verdict=pass` |
| send work back from review | `add x/verdicts '{fail findings:[…]}'`; the ticket stays in `review`; round derives from the count of fails; `merge/entry` requires `verdict=pass` |
| close without finishing, with a reason | terminal columns `dup`, `superseded`, `obsolete`, `wontfix`, each with an `entry` requirement for `of` or `reason` |
| park, reopen | a transition into `initial`, reachable from everywhere; the claim ends |
| finish a container | derived: a node's stage is terminal when every child's is |
| tell an agent what to do now | the handler bound `in <column>` prints after the ticket on `get x` and on `/next` |
| nudge without blocking | a requirement with `strength = advise`; a handler |
| customize the process per project or per node | sections of `.lit/workspace.toml`, at the root or keyed by a node's id; nearest ancestor wins per column, per handler field; reviewed in a pull request like any change to how the team works |
| add a field, a level name, a link type | the same file: `[levels.1.fields.points]`, `[levels.2] name`, `[links.tests]`; no code |
| see why a ticket is where it is | `get x/history`; `get x/held`; a rule's label names the rule |
| undo | `set` back from the echo, or `get x/body/versions/N | set x/body -`; every version is in the history |
| find things | selectors over any field, at any depth, across every node's dialect of a column name |
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
"rework" is a verdict inside `review`; "orphaned" is the selector
`(stage=active updated<-6h)`.

## 5. One command, start to finish

Every verb runs this pipeline on every call; the verb and the paths are
values `[LAW:dataflow-not-control-flow]`. Steps 2 through 5 are pure functions
of (events, clock, argv, writer); the rest is the hearth
`[LAW:effects-at-boundaries]`.

1. **Open.** Read every writer ref head and this checkout's open log; load
   the cache snapshot keyed by `(fold-version, ref heads, log length)`. A
   miss or a torn snapshot is one refold; no lock.
2. **Parse** `.lit/workspace.toml` as this reader sees it (§3.15) into the
   schema value.
3. **Fold** the events newer than the snapshot, from the sealed segments
   and the open log, verifying each (§3.20).
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

`[LAW:one-way-deps]` Each layer depends only on the layers below it.

```
 cli        argv → typed command; rendering; exit codes; --json
 guidance   moments → resolved handlers → text after the data
 verbs      get/set/add/rm on typed addresses; requirements; verb → intents; echo
 address    path grammar; selectors; globs; the field table
 tick       clocked derivations: labels, held, expiry, staleness, stage, rollup
 fold       events → state; validation engine; rank; holders; conflicts
 config     .lit/workspace.toml from the working tree → one typed schema value
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
label are read. Key material lives beside the checkout id in the private
git dir, never in the store and never behind a prompt. Tests build the same stack over an
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
- **C. `topic`.** Not a field; ids are opaque with the project prefix, and
  an area of focus is a label (appendix).
- **D. `archived`.** Not a field; a ticket out of the flow is in a terminal
  column, one that should not exist is deleted (tombstone) or purged
  (rewrite) (appendix).
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
  authentication works. Event-store charter #4 is amended by this
  (appendix): its reasons, licensing and foreground latency, do not reach a
  background push through the user's git, since nothing is linked and no
  foreground path waits on it.
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

Each step leaves every layer below it in its final shape; step 2 is the
walking skeleton.

1. **Skeleton.** The layout of §6, the composition root, the command event,
   the in-memory store, the harness, `lit2 --help` from the field table.
   (ticket lit-bootstrap-1f9.438)
2. **Events, principal and fold, first facts.** Items, fields, comments,
   labels; ids; the principal minted at init and every event signed; the
   local log and the snapshot; `add /children`, `get`, `set`, `add`, `rm`;
   `lit2 admin init`.
3. **Policy.** Enrollment at init; the policy document as events; roles
   and capabilities as path globs in the default profile; the enforcer's
   capability verdict; membership verbs and the owner threshold;
   verification as a fold rule and at receive.
4. **Config and columns.** `.lit/workspace.toml` from the working tree, the
   schema parse, `config-ref` on events as provenance; levels and
   synthetic homes; columns, stage, rollup; transitions with the one
   enforcer; the checklist and its terminal requirement; the default profile.
5. **Tiers.** Envelopes encrypted under the default tier; content keys and
   wrapped epoch keys; `tier` on items and comments; `set x/tier`; grant
   on admit and rotation on removal; `rm x --destroy`; succession;
   `get /me`.
6. **Lanes, rank, claims, next.** Lanes on nodes and the one rank order;
   rank intents; holders and expiry; `/lanes`, `/next`, `held`, rules.
7. **Guidance.** Workflows, handlers, the three layers, moments, the managed
   block, `--dry-run` printing guidance.
8. **Sync.** Sealing the log into segments, the writer's commit and ref,
   the background process, verification at receive and re-publishing over
   a vandalized remote, inbox refs, staleness, `/integrity`.
9. **Rewrites.** Purge, squash, sweep, the receiver's rule; the budgets
   harness and the depth corpus.
10. **Selectors in full, `--json`.**

## Appendix: where this departs from a source document

One line each, so a reader of the older document knows the position moved.

- event-store design §events: no fold-derived child ordinals; ids are the
  opaque form only.
- event-store design §fold: determinism is asserted as state equality, not
  pinned bytes.
- event-store design §security-and-config (birth requirement 6),
  workspace-configuration.md, workflows.md decision 1: the configuration is a
  committed file in the repository, not a stream in the store. Git supplies
  the versioning, signing and review the config stream was to provide, and
  events cite the file's blob hash. The access-control policy document, when
  it comes, is read against this.
- path-addressed-cli.md: kept as written for hierarchy, lanes and rank;
  `rank` takes relative forms that are the events' own shape.
- work-claims.md: the routing steps collapse into the walk of the lane
  order; the holder set replaces the single claim scalar; everything else
  stands.
- columns-and-phases.md, transition-requirements.md: a verdict carries `of`,
  the digest it judged; the checklist's open count is `checklist/pending`.
- workspace-schema design and the v1 hierarchy epic
  `promptctl-hierarchy-6cqf`: levels are configured, one to five, not three
  fixed in code (decision A). Exact containment, the synthetic homes, the
  checklist and the retirement of `type` stand as that epic records them.
- custom-fields.md, workspace-schema: `topic` and `type` are not fields;
  `archived` is not a field (decisions C, D).
- schema-reversibility.md: the two rules survive as `INV:preserve-unknown`
  and the release-only raise of event schema versions; the SQL mechanism
  does not.
- MINIMAL-LIT-SKETCH: lanes, claims and priority are kept, as the later
  designs require; its eleven guarantees are all rows of §4.
- workspace-configuration.md, custom-fields.md: there is no reader of a store
  from outside its checkout (`--at`, `stores --counts`); lit2 runs in a
  checkout.
- event-store design §store: a command appends to a local log, not a
  commit; a writer publishes one commit whose tree is its sealed segments and
  replaces its ref on push; nothing on the remote grows with commands, and
  the number of events never reaches git. The sweep keeps its purpose with
  one tree to anchor instead of a chain.
- event-store charter #4: transport through the user's git in the background
  process (decision G); go-git touches objects and refs only.
- event-store design `INV:config-versioned`: the fold reads no
  configuration; `config-ref` is provenance, the enforcer judges a write
  under the writer's file and tick judges state under the reader's; a
  writer's tree carries no configuration (decision H).
- access-control design: nothing in it is later; the write layer and the
  encrypted default tier are in the first release (requirements.md §1).
  Capabilities name path globs rather than the v1 command table's verbs;
  the policy document is events in the store, not a Dolt object; replayed-
  commit re-signing does not arise because nothing is replayed; `start`
  recording the assignee becomes the `assignee` binding reading the lane's
  claim holder.
