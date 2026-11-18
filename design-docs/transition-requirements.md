# Transition requirements: what a ticket needs to enter or leave a column

Status: draft, 2026-11-05.

The owner's ruling on the v1 grooming proposal (since removed): *"grooming: this isn't about
grooming, this is about the question of defining requirements for transitioning
in and out of a column/phase. also requires a design."* This is that design. It
assumes what `columns-and-phases.md` settles: column, phase and workflow state
are one concept, a **column**; a ticket's `status` holds its current column;
the engine interprets only a column's `initial` and `terminal` flags. It builds
on the four-verb surface of [path-addressed-cli.md](path-addressed-cli.md),
whose `gate` and `requires` keys it replaces with one shape.

## The model in one paragraph

A column declares, in the workspace configuration beside its flags and its
`next` list, two lists of **requirements**: `exit`, what a ticket needs before
it may leave along a forward edge, and `entry`, what it needs before it may
arrive by any edge. A requirement is one test over one path of the ticket, a
scope, and a severity. The tests are evaluated in exactly one place, the
transition write `set x/status <column>`, against the column graph the ticket
resolves. A blocking requirement that fails refuses the write and nothing is
written; an advisory one that fails is printed and the write proceeds. Every
other surface that talks about readiness, `/next` first among them, reports
what that one evaluation would say and enforces nothing.

## Entry and exit are both needed

Mechanically one side could carry everything, and each side fails a real case.
Entry alone cannot say "a ticket may not leave the backlog" once the backlog
has two forward destinations without writing the list twice, and the copies
drift `[LAW:one-source-of-truth]`. Exit alone cannot say "`dup` needs `status/of`" at
all: a terminal column is reachable from every column, so the requirement
would be copied onto every column's exit, and an exit list does not know which
destination the write is heading for. So a column has both, and the author
puts a requirement on the side that owns its reason: **exit says this column's
work is complete, whatever comes next; entry says the destination needs this,
whatever it came from.** On a straight board the two are one check on one move,
and the shipped default uses whichever reads as the reason.

The two edges the engine supplies without `next` listing them, closing into a
terminal column and parking into the initial one, carry the destination's
entry requirements and never the source's exit requirements. Exit requirements
guard the forward edges only. Closing and parking abandon a column's work
rather than complete it, and a workspace that could not close a backlog ticket
as `wontfix` until it had a description would be refusing the one write that
makes the description unnecessary. Creation is the transition into the initial
column and carries that column's entry requirements like any other arrival;
the shipped default declares none, so creation is free, and the rule that
required never means required to be created
([workspace-schema/design.md](workspace-schema/design.md) §Fields) is kept by
data rather than by a special case `[LAW:dataflow-not-control-flow]`.

## What a requirement is

A requirement is three values, no more:

- **`test`**: one atom of the selector grammar the CLI already has, written
  against a path relative to the ticket. The atoms are `p` (present: a scalar
  with a value, a list with an element), `!p` (absent), `p=v`, `p!=v` (the
  negation of `=`), `p~re`, and `p<v`, `p>v` on a number or date path. On a
  list path `=` and `~` hold when some element does, so `labels=ready` is
  "carries the label" and `labels!=needs-design` is "does not". `v` may be
  several values, `status=backlog,impl`: `=` holds when any matches and `!=`
  when none. Values are the CLI's values: a bare word or a quoted string; a
  date value is a date or time, or a duration before the evaluation's `now`,
  `updated<-6h`; every list exposes `count`, a derived number. There is no
  `and`, `or` or arithmetic inside an atom; a column's list is the
  conjunction, and that is the whole language.
  The selector in `get '(status=open type=bug)'` and a requirement's `test`
  are parsed by one grammar, so a reader learns it once
  `[LAW:one-source-of-truth]`.
- **`scope`**: `self`, the default, reads the path on the ticket. `ancestors`
  reads it across the ticket and its ancestors: a list path is the union of
  the lists up the chain, a scalar path is the nearest value going up. That
  is what lets a mark on the epic satisfy a requirement on its ticket, lets a
  `needs-design` on the epic hold every ticket under it, and lets a field
  declared on level 2 be required before any of its tickets move, with one
  resolution rule rather than three.
- **`severity`**: `error` or `warning`, and it has no default. Every declaration
  states its certainty, which is what the north star asks for
  ([project-intent.md](project-intent.md): tune the strength of an intervention
  to its certainty).

The closed set is the atoms. The other candidate kinds are all instances of
them reading a path the ticket already exposes: a field present or matching is
`points` or `target_release~'^\d+\.\d+$'`; a label present is `labels=ready`;
a passing review of the latest plan revision is `plan/verdict=pass`, because
`plan/verdict` is the derived field architecture.md §4 defines as the verdict of
the last review naming the latest revision, or nothing; the original a duplicate's move names is `status/of`; a
dependency closed is `!blockers`, where `blockers` is a derived list, the
members of `blocked_by` (the lane predecessor, and every ticket whose `blocks`
names this one) that are not in a terminal column.

Anything else is expressed as data, never as a new atom
`[LAW:types-are-the-program]`. Either someone writes the fact (a field, a label,
a verdict, a comment) and a requirement reads it, or the engine exposes a new
derived field in the field table, which is engine code in one listed place, and
a requirement reads that. The checklist exposes `checklist/pending`, its count
of pending items, and the requirement is `checklist/pending=0`. The test
grammar does not grow.

The configuration parse refuses, naming the key, a `test` whose path names a
field declared on no level, a `severity` that is missing or not one of the two,
a `scope` that is not one of the two, and an `exit` list on a terminal column,
which has no forward edge and so could never be evaluated. A path declared on
some level but not on this ticket's level reads as absent; that is what makes
`scope = "ancestors"` on a level-2 field work.

## The declaration

Requirements live on the columns of `.lit/workspace.toml`, the same file
`/columns` reads and whose working-tree draft it writes, so there is one source
whether the author edits TOML or runs `add /columns/backlog/exit '{…}'`; either
way the version in force is the one applied with `config commit`. The
shipped default, in full (the file is `internal/assets/workspace/default.toml`):

```toml
prefix = "lit"
claim_ttl = "6h"
labels = ["ready", "needs-design", "external"]

[levels.1]
name = "story"

[levels.2]
name = "feature"
home = "unfiled"

[levels.3]
name = "epic"
home = "unassigned"

[checks.ready]
tests = [
  { test = "description" },
  { test = "labels=ready", scope = "ancestors" },
  { test = "labels!=needs-design", scope = "ancestors" },
  { test = "labels!=external", scope = "ancestors" },
  { test = "!blockers" },
]

[links.blocks]
kind = "dependency"
between = [[1, 1], [2, 2], [3, 3]]

[links.relates]
kind = "relates"
between = [[1, 1], [1, 2], [1, 3], [2, 1], [2, 2], [2, 3], [3, 1], [3, 2], [3, 3]]

[columns.backlog]
initial = true
next = ["plan"]
exit = [{ check = "ready", severity = "error" }]

[columns.plan]
next = ["impl"]

[columns.impl]
next = ["review"]
entry = [{ test = "plan/verdict=pass", severity = "error" }]

[columns.review]
next = ["merge"]
entry = [{ test = "plan/verdict=pass", severity = "warning" }]

[columns.merge]
next = ["done"]
entry = [{ test = "pr/verdict=pass", severity = "error" }]

[columns.done]
terminal = true

[columns.dup]
terminal = true
entry = [{ test = "status/of", severity = "error" }]

[columns.superseded]
terminal = true
entry = [{ test = "status/of", severity = "error" }]

[columns.obsolete]
terminal = true
entry = [{ test = "status/reason", severity = "error" }]

[columns.wontfix]
terminal = true
entry = [{ test = "status/reason", severity = "error" }]
```

`merge`'s entry requirement reads `pr/verdict`, the verdict of the last review
naming the PR's latest revision as [columns-and-phases.md](columns-and-phases.md)
defines it; a `fail` keeps the ticket in `review`, which is where rework
happens, so `review` has no backward edge. The advisory on `review` is the one
place the default nudges rather than refuses: appending a plan revision empties
the derived verdict, so a ticket arriving at review with a plan revised since
its review is worth a line, and an agent that revised the plan for a reason it
can defend proceeds.

As paths, the same declaration is `/columns/backlog/exit`, a list
whose elements are objects, edited with `add` and `rm` like any list:

```
add /columns/backlog/exit '{test:points severity:warning}'
+ /columns/backlog/exit/5
```

## One enforcer

Requirements are evaluated by one pure function, from the ticket's folded
state, the resolved column graph and the edge, to a list of verdicts, one per
requirement. It reads no clock and performs no I/O
`[LAW:effects-at-boundaries]`. The transition write is the only caller that
acts on its result: it evaluates the source column's exit list when the edge is
forward and the destination's entry list always, all of them, with no
short-circuit, so a refusal names everything unmet at once and an agent fixes
it in one pass. If any blocking requirement is unmet the write is refused and
nothing is written, including the claim the transition would have stamped
`[LAW:single-enforcer]`. The refusal is the CLI's refusal shape:

```
$ lit set dee/status plan
error (reason=gate): dee/status backlog → plan refused by backlog/exit; nothing written
  error  description     empty
  error  labels=ready    absent on dee and its ancestors (unfiled)
A ticket leaves the backlog once it says what to build and someone has put its
epic in order and marked it ready. …
```

The first line names the path, the edge and the side that refused; one line per
unmet requirement gives its severity, its test, and the value found, so the
agent is not sent back to `get`; then the ticket's current column's guidance,
the same text `get dee` prints, because the column that holds a ticket is the
one that says how to leave it. A bulk write, `set 'rin.*/status' plan`,
expands first and is one write: every refused path is listed and nothing is
written for any of them. `--dry-run` prints exactly this and performs nothing,
which is how an agent asks "could I?" without trying.

A requirement is evaluated at the transition and never again. A ticket whose
column's requirements stop holding afterwards, a description deleted, a verdict
emptied by a new plan revision, stays where it is; the facts are in the history and in
any selector that asks. Nothing re-evaluates a requirement on read: what the
facts say about a ticket afterwards is a derived field such as `held`, or a
label declared with a `where` selector (architecture.md §3.11). Computing a
ticket's position from rules on every read is a future
[columns-and-phases.md](columns-and-phases.md) keeps open; this design keeps
the sibling's stored `status` and puts the rule at the one write that changes
it.

## Severity

`error` is the refusal above. `warning` lets the write through and prints the
unmet requirement with the echo, in the same line shape, under the derivations:

```
$ lit set dee/status review
dee/status impl → review
  warning review/entry  plan/verdict=pass   verdict is empty; plan revised since the review of revision 3
  requirements: 1 evaluated · 0 met · 1 unmet (warning) · 0 unmet (error)
```

Nothing more is asked of the agent: no `--reason`, no acknowledgement flag. A
nudge that demands a receipt is a block wearing a softer name, and the record
of the advisory, in the echo and the command event, is the feedback the north
star wants placed where the agent can act on it. Which requirements are which
is the author's call per declaration, and the default above shows the split:
the invariants the workflow exists for (a plan passed before building, a
canonical id on a duplicate, a ticket groomed before it is pulled) are `error`;
a heuristic the agent may overrule with reason is `warning`.

## Ready to pull

The owner's motivating case: *"when things go into the backlog, they're not
ready to pull yet. They have to be groomed and then they're ready to pull."*
In this design "ready to pull" is not a feature. It is the initial column's
exit list, as the default above declares it: a description, the `ready` label
on the ticket or an ancestor, no `needs-design` or `external` on the ticket or
an ancestor, no open blocker. It is declared on exit rather than on the first
active column's entry because its reason is the ticket's fitness to leave,
whatever it goes to next, and an epic that adds an `audit` column before
`plan` must not have to copy the list.

Who satisfies it: whoever grooms. Grooming an epic ends with
`add <epic>/labels ready`, and because the requirement's scope is `ancestors`
every ticket under the epic passes that requirement in one write, and a
follow-up filed into the groomed epic later passes it at birth, which is the
behavior the house workflow depends on. A ticket with no epic carries its own
label. Removing the label from the epic holds every ticket under it again, and
the label's arrival and removal are in the history with a writer and a time.

What becomes of grooming.md's two pieces: the `ready-to-pull` label is carried
as data and only as data. There is no reserved label, no constant in code, no
`ungroomed` classifier kind; the name `ready` appears once, in the default
profile's `test`, and a workspace that wants `groomed` or `ready-to-pull`
edits the string. `lit groom` is dropped. Its purpose, printing what grooming
means here, is the initial column's `guidance`, which `get <ticket>` prints for
any ticket sitting there, which every refusal and every held `/next` ends with,
and which `get /columns/backlog/guidance` reads on demand. There is
no event to fire because there is no command; the prose lives on the column it
describes `[LAW:one-source-of-truth]`.

How the queue treats a ticket that fails: `/next` is a read, so it enforces
nothing; it reports the evaluation the start write would make. A derived field
carries that report on every ticket: **`x/held`** is the unmet blocking
requirements on each forward edge out of the ticket's current column, keyed by
destination, empty when some forward edge is open and empty on a terminal
column. It is the dry-run of the forward transition exposed as a path, so
`get '(held)'` lists everything stuck and `get dee/held` says why one ticket is.
A lane head with a non-empty `held` is held, and the lane with it, since lane
order is sequence.

`/next` then serves the heads of the lanes this checkout holds, in rank order.
A held head is printed with its `held` lines and the column's guidance and
passed over; a checkout whose held work is all stuck is never trapped, so
`/next` then falls through the rank order to the first lane head that is not
held elsewhere and whose `held` is empty, labeled as outside the commitment.
Starting it holds a second lane; a checkout may hold several. Leaving a lane
on purpose is `rm` on the claim, a write, echoed. Every lane passed over is
named with its reason, so nothing ranked higher goes unseen
`[LAW:nothing-unseen]`:

```
$ lit get /next
fw9   /lanes/parser   rank 2   backlog
held above it:
  /lanes/datetime  b64   plan ← backlog/exit  description empty; labels=ready absent on b64 and its ancestors (rin)
```

`get /lanes` prints `held` beside each head the same way.

## v1's holds, mapped

The readiness classifier knew six reasons to hold a ticket. Each is a
requirement in the default above or a derivation it reads; none is engine code.

- `ready.required_fields` is an exit requirement on the initial column, one
  per field: `test = "<field>"`, `severity = "error"`. "Required means
  required to be pulled" is exactly "an exit requirement on the initial
  column"; a `required` flag on the field declaration would be a second place
  that rule lived, one that could not say which transition it guarded, so a
  declaration carries none ([workspace-schema/design.md](workspace-schema/design.md)
  §Fields), and a field a workspace wants before `merge` rather than before
  `plan` declares it on `merge.entry`. A field required on level 2 is the same
  requirement with `scope = "ancestors"`, which bites on the tickets under
  the epic, since level 2 has no stored status to transition.
- `needs-design` and `external` are the two `labels!=` requirements, scoped to
  ancestors. The constants are gone; a workspace that wants a third such label
  adds a line.
- An open dependency, an inherited dependency, and an earlier sibling in the
  lane are `!blockers`, since `blocked_by` already derives from lane position
  and from `blocks`.
- The `ungroomed` kind grooming.md proposed is `labels=ready`, above.

## Requirements and the hierarchy

Requirements bind to level 1 only, because level 1 is the only level with a
stored status and so the only level with a transition to write. A node above
level 1 derives its stage from its children and is never set, so it has
nothing to enter or leave; its facts reach requirements through
`scope = "ancestors"`.

A node carries requirements its tickets inherit the way
path-addressed-cli.md already says: it carries a `columns` section, and
descendants resolve by walking up to the nearest one. Resolution is per column
and whole: the nearest ancestor that declares a column at all supplies that
column's flags, `next`, `entry`, `exit` and `guidance`, and nothing is merged
across levels. An epic that wants one more exit requirement on `backlog`
redeclares `backlog` with the full list. Merging lists would need a rule for
order, for replacement and for removal, and every such rule is a second way
for the configuration to say one thing `[LAW:no-mode-explosion]`. The command
event records which node's section each column resolved to.

## Observability

Every transition echo ends with one summary line, and the line is printed when
every count is zero, because a transition with no requirements declared and a
transition whose requirements were not evaluated must not look alike
`[LAW:nothing-unseen]`:

```
dee/status backlog → plan   ⇒ dee/claim = this checkout
  requirements: 5 evaluated · 5 met · 0 unmet (warning) · 0 unmet (error)
```

Above the summary, one line per unmet requirement, advisory or blocking, with
its severity, the column and side that declared it, its test, and the value
found. The refusal is the same lines under the `error` header.

The command event for a transition records, per requirement evaluated: the
column and side, the test, the scope, the severity, the workflow node it
resolved from, the outcome, and the value read, with `absent` as a value; the
totals the echo prints; and `written: false` on a refusal. The event for `/next`
records every lane considered, each head's `held` lines, which lane was served
or that none was, and the count of held lanes, zero included. The event for a
configuration write that touches a column's lists records the count of
requirements before and after. `--dry-run` writes the same event with
`dry_run: true`, so the question an agent asked is on the record beside the
writes it made.

## Decisions for the owner

1. `!blockers` ships as `error`, inside `ready`, which backlog's exit checks at
   `error`. Recommended, because a lane's order and a `blocks` edge are both
   statements that this cannot be done before that. The alternative is
   `warning`, which lets an agent start the next lane member while the previous
   sits in review; either is one word in the default.
2. The `required` key on a field declaration is removed in favor of a
   requirement on the column where it bites. Recommended, for one source; the
   alternative keeps the key as shorthand that the configuration parse expands
   into an initial-column exit requirement, which is the same rule in two
   spellings.
