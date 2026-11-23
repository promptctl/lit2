# Columns, stage and requirements

Carries out `strategy/process.md`.

## Column and status

A **column** is a named node in the column graph, `/columns/<name>`. A
ticket's `status` holds the name of the column it is in. A phase, a state or
a workflow state is a column. The default profile's columns and their
requirements are data in `internal/assets/workspace/default.toml`, and a workspace may rename, add or
remove any column subject to "Validity of a column graph".

**The column is stored.** `set x/status review` stores one transition event:
this ticket moved from `impl` to `review`, by this writer, at this time. The
write is accepted only if `review` is reachable from `impl` in the ticket's
resolved column graph and the requirements on that edge hold. A ticket's
column is a function of the newest status transition in its trail, and
nothing else; the fold answers it.

Everything but position is evaluate's (`specifications/fold.md`, "Two
functions").

Computing a ticket's column from facts, configuration and the clock, instead
of storing it, was considered and is not built (`strategy/process.md`,
"Diagnosis"). Recorded times are also not monotonic across writers, so two
implementations that settled precedence, configuration-version and
clock-ordering differently would give two boards from the same facts.

Settling is expressed as derived fields. A ticket in `impl` whose blocker
reopens stays in `impl`, and its `blockers` reads the blocker; `/next` passes
its lane over naming the reason; when the blocker closes, the field is empty
on the next read, with no merge rule and arrival order irrelevant.
`get x/held` names each unmet test and the check it came from, and
`get x/history` answers why a ticket is in its column.

## The graph and stage

A column node carries the flags `initial` and `terminal`, a `next` list, and
the requirement lists `entry` and `exit`.

**Edges.** A column some `next` names is entered only along those edges. A
column no `next` names is entered from every other column. A **forward edge**
is one a `next` list names. In the default profile this makes `done`
reachable only through `merge`, and `backlog` and the terminal columns other
than `done` reachable from anywhere: parking, reopening, and closing without
finishing. The flags are not consulted for edges; they give stage.

**Stage.** `x/stage` is derived from the flags on the ticket's column in its
resolved graph: `initial`, `active` (neither flag), or `terminal`. Start is
any transition out of `initial`, and it also writes a claim event
(`specifications/lanes-and-claims.md`). Done is any transition into a
`terminal` column. Creation is the transition into the initial column.

**Closing with a reason.** A move carries the fields its destination's entry
needs, in its one transition: `set x/status '{column:wontfix reason:"…"}'` or
`set x/status '{column:dup of:4ou}'`. `status/reason` and `status/of` are
those fields, read by requirements; the next move replaces them. The entry
requirement `status/of` on `dup` is what makes the canonical id mandatory.

**A node has a stage, not a column.** Only a leaf has a `status`. A node
has a derived `stage`: `terminal` when every child's stage is `terminal`,
`initial` when every child's is `initial` or it has no children, `active`
otherwise. Its derived `rollup` is the count of leaves under it per column.

```
$ lit2 get 792a/stage 792a/rollup
792a/stage    active
792a/rollup   backlog 1 · impl 2 · review 1 · done 4
```

Requirements bind to level 1 only, because level 1 is the only level with a
stored status; a node's facts reach requirements through
`scope = "ancestors"`.

## Nearest ancestor

A node may carry a `columns` section its tickets inherit, in the workspace
file's per-node section keyed by the node's id. Descendants resolve by
walking up. Resolution is per column and whole: the nearest ancestor that
declares a column supplies that column's flags, `next`, `entry` and `exit`,
and nothing is merged across levels. An epic that wants one more exit
requirement on `backlog` redeclares `backlog` with the full list. The command
event records which node's section each column resolved to.

An epic that adds a column edits the graph around it in its own node:

```
rin/columns/audit       next:[impl]
rin/columns/plan/next   [audit]
```

`rin`'s children resolve to the default graph with `audit` between `plan`
and `impl`, and the validity checks run on that resolved graph when either
line is applied. `(status=audit)` matches tickets holding it under any node
that defines it; `(stage=active)` matches them with every other in-flight
ticket, because stage comes from the resolved column's flags. A ticket in
`audit` is in flight, keeps its claim and holds its lane's head. Moving a
ticket under a node whose resolved graph lacks the column it holds is
refused at the write, the same check that refuses removing a held column.

## Validity of a column graph

A version's column graphs are checked when the version is applied, for every
node's resolved graph and against every live version
(`specifications/workspace-file.md`, "Applying a version"): one initial
column, a terminal column reachable, every column a `next` names present. A
version that leaves a held column unresolved is refused, naming the
`aliases` line that says where its tickets go.

The configuration parse refuses, naming the key: a `test` whose path names a
field declared on no level, a `severity`
missing or not one of the two, a `scope` not one of the two, and an `exit`
list on a terminal column, which has no forward edge and so could never be
evaluated.

**An undeclared column.** Two concurrent writers can still leave a ticket in
a column the applied version no longer declares for it. The transition
stands; evaluate reports `status: audit (undeclared)` with stage `active`;
`/next` passes the lane over naming the reason; `/integrity` lists it; and
the way out is a transition to a column that exists, which the edges into
columns no `next` names always offer.

## Requirements

A column declares two lists of requirements: `exit`, what a ticket needs
before it may leave along a forward edge, and `entry`, what it needs before it
may arrive by any edge. On a straight board the two are one check on one move, and a declaration uses
whichever states the reason.

An edge no `next` lists, closing into a terminal column or parking into the
initial one, carries the destination's entry requirements and never the
source's exit requirements: a workspace that could not close a backlog
ticket as `wontfix` until it had a description would refuse the one write
that makes the description unnecessary. Creation carries the initial
column's entry requirements like any other arrival.

A requirement has three values:

- **`test`**: one selector atom over a path relative to the ticket, or
  `check`, naming a group of tests declared under `[checks]`.
- **`scope`**: `self`, the default, reads the path on the ticket. `ancestors`
  reads it across the ticket and its ancestors: a list path is the union of
  the lists up the chain, a scalar path is the nearest value going up. That is
  what lets a label on the epic satisfy a requirement on its ticket, lets
  `needs-design` on the epic hold every ticket under it, and lets a field
  declared on level 2 be required before any of its tickets move.
- **`severity`**: `error` or `warning`, with no default.

A path declared on some level but not on this ticket's level reads as
absent; that is what makes `scope = "ancestors"` on a level-2 field work.

### The atoms

The selector in `get '(status=backlog labels=bug)'`, a handler's `where` and
a requirement's `test` are parsed by one grammar. The atoms are:

| atom | holds when |
|---|---|
| `p` | present: a scalar with a value, a list with an element |
| `!p` | absent |
| `p=v` | equal; on a list path, some element is |
| `p!=v` | the negation of `=` |
| `p~re` | matches the regular expression; on a list path, some element does |
| `p<v`, `p>v` | on a number or date path |

- `p` is a path relative to the item. A selector may read through `parent`:
  `(stage=terminal parent/stage=terminal)` matches tickets whose epic is
  finished.
- `v` is a value of the command line's literal grammar
  (`specifications/command-line.md`, "Values"). It may be several values,
  `status=backlog,impl`: `=` holds when any matches and `!=` when none.
- A date value is a date or time, or a duration before the evaluation's
  `now`: `updated<-6h`.
- Every list exposes `count`, a derived number.
- `labels=ready` is "carries the label" and `labels!=needs-design` is "does
  not".
- `status=<name>` matches the item's resolved column name, so
  `(status=review)` matches review tickets at every depth, across every
  node's dialect of the column graph.
- An atom has no `and`, `or` or arithmetic; a selector's atoms and a
  column's list are each a conjunction.

**The verdict.** An atom evaluates to met, unmet, or `unknown (tier
<name>)`: a test that reads a prose field by content cannot be evaluated by
a reader without the tier's key, and reads as unknown, never as met or
unmet. `held` reports such a requirement that way.

### What a requirement reads

Someone writes a fact (a field, a label, a verdict, a comment) and a
requirement reads it, or the engine exposes a derived field in the field
table, which is engine code in one listed place, and a requirement reads
that. Examples:

- a field present or matching: `points`, `target_release~'^\d+\.\d+$'`
- a label: `labels=ready`
- a passing review of the latest plan revision: `plan/verdict=pass`
- the original a duplicate's move names: `status/of`
- no open dependency: `!blockers`
- no pending checklist item: `checklist/pending=0`

### Derived fields a requirement reads

- **`blocked_by`**: the ticket's lane predecessor and every ticket whose
  `blocks` names this one. Derived from position and links, never stored.
- **`blockers`**: the members of `blocked_by` not in a terminal column.
- **`plan/verdict`**, **`pr/verdict`**: see "Review and rework".

### The declaration

Requirements live on the columns of `.lit/workspace.toml`, edited in TOML or
through the verbs (`specifications/workspace-file.md`, "Applying a version").
As paths, `/columns/backlog/exit` is a list whose elements are objects,
edited with `add` and `rm` like any list:

```
add /columns/backlog/exit '{test:points severity:warning}'
+ /columns/backlog/exit/5
```

## The requirement gate

Requirements are evaluated by one pure function, from the ticket's folded
state, the resolved column graph, the edge and the caller's `now`, to a list
of verdicts, one per requirement ("The atoms"). It reads no clock and
performs no I/O.

`held`, `--dry-run` and `/next` call the same function. The transition write
is the only caller that acts on its result. It evaluates the source column's
exit list when the edge is forward and the destination's entry list always,
every requirement with no short-circuit, so a refusal names everything unmet
at once. If any `error` requirement is unmet, the write is refused and nothing
is written:

```
$ lit2 set dee/status plan
error (reason=gate): dee/status backlog → plan refused by backlog/exit; nothing written
  error  description     empty
  error  labels=ready    absent on dee and its ancestors (unfiled)
A ticket leaves the backlog once it says what to build and someone has put its
epic in order and marked it ready. …
```

The first line names the path, the edge and the side that refused; one line
per unmet requirement gives its severity, its test and the value found, so the
agent is not sent back to `get`; then the guidance of the ticket's current
column, the one that says how to leave it.

A `warning` lets the write through and prints the unmet requirement with the
echo, in the same line shape, under the derivations. It needs no
acknowledgement.

```
$ lit2 set dee/status review
dee/status impl → review
  warning review/entry  plan/verdict=pass   verdict is empty; plan revised since the review of revision 3
  requirements: 1 evaluated · 0 met · 1 unmet (warning) · 0 unmet (error)
```

A ticket whose column's requirements stop holding afterwards, a description
deleted or a verdict emptied by a new plan revision, stays where it is; the
facts are in the history, in any selector that asks, and in derived fields
such as `held`. A transition judged under the writer's active version stands
after a later version adds a requirement it would have failed
(`specifications/fold.md`, "Writes are judged at the write").

## held

**`x/held`** is the unmet `error` requirements on each forward edge out of the
ticket's current column, keyed by destination: empty when some forward edge is
open, and empty on a terminal column. It is the dry run of the forward
transition exposed as a path, so `get '(held)'` lists the tickets whose
requirements hold them and `get dee/held` says why one is held. How `/next`
treats a held head is `specifications/lanes-and-claims.md`, "Routing".

## Ready to pull

In the default profile, readiness to pull is the initial column's exit list,
the `ready` check: a description, the `ready` label on the ticket or an
ancestor, no `needs-design` or `external` on the ticket or an ancestor, no
open blocker. It is declared on exit rather than on the first active column's
entry because its reason is the ticket's fitness to leave, whatever it goes to
next, so an epic that adds an `audit` column before `plan` does not copy the
list.

Grooming an epic ends with `add <epic>/labels ready`. Because the
requirement's scope is `ancestors`, every ticket under the epic passes it in
one write, and a follow-up filed into the groomed epic later passes it at
birth. Removing the label from the epic holds every ticket under it that does
not carry its own. The label is data, and a workspace that wants `groomed`
edits the string. What grooming means is the initial column's guidance.

## Review and rework

`x/plan` and `x/pr` are **reviewables**: two append-only lists,
`revisions` and `reviews`. A plan revision is a text; a PR revision is a head
commit the author records with `add x/pr/revisions <commit>`. Each review
names the revision it judged in `of`, which is required, and carries how it
was run (`how`), the reviewer's `result`, the agent's `response`, and a
`verdict` of `pass` or `fail`:

```
$ lit2 add s4l/pr/reviews '{of:1 how:"/code-review high" result:"…" verdict:fail}'
+ s4l/pr/reviews/9k2  fail, of:1
⇒ s4l/pr/verdict  (none) → fail   round 2
```

`x/plan` reads the latest revision with its number; `x/plan/revisions/3`
reads one. **`verdict`** is derived: the verdict of the last review naming the
latest revision, or nothing. Appending a revision empties it until a review
names the new one. A review of an older revision lands and derives nothing,
and the echo names the latest revision and the one last reviewed. `of` is
required because a review defaulting to the latest revision could pass one
the reviewer never read.

**Rework is inside review.** A ticket comes back from review when a reviewer
records findings, and it does not move: it is in `review` before and after a
`fail`, and leaves forward when a review passes. The round is derived: one
more than the number of `fail` reviews. There is no rework record: the review
is the record, and the author's answer is the `response` on it, a push
recorded as the next revision, and the next review. `get x` on a second trip
prints `review   round 2 · pr/verdict fail` and the handlers bound
`in review` whose `where` matches, such as `(pr/verdict=fail)`. A selector
finds rework in flight with `(status=review pr/verdict=fail)`, and a report
counts time in rounds above 1 from the review timestamps on the trail.

In the default profile, `merge`'s entry requires `pr/verdict=pass`, `impl`'s
entry requires `plan/verdict=pass`, and `review`'s entry carries
`plan/verdict=pass` as a warning, so a ticket arriving at review with its
plan revised since the review gets one line, and an agent that revised the
plan for a reason it can defend proceeds. A workspace that wants a re-review
sets that requirement's severity to `error`. A plan `fail` keeps the ticket
in `plan`, and the round counts the same way.

## Observability

Every transition echo ends with one summary line, printed when every count is
zero too, because a transition with no requirements declared and one whose
requirements were not evaluated must not look alike `[LAW:nothing-unseen]`:

```
dee/status backlog → plan   ⇒ dee/claim = this checkout
  requirements: 5 evaluated · 5 met · 0 unmet (warning) · 0 unmet (error)
```

Above the summary is one line per unmet requirement, advisory or blocking,
with its severity, the column and side that declared it, its test and the
value found. The refusal is the same lines under the `error` header.

The command event for a transition records, per requirement evaluated: the
column and side, the test, the scope, the severity, the node it resolved
from, the outcome, and the value read, with `absent` as a value; the totals
the echo prints; and `written: false` on a refusal. The event for a
configuration write that touches a column's lists records the count of
requirements before and after. In the local command log, a `--dry-run`
records the same event with `dry_run: true`.
