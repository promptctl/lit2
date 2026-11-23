# Transition requirements: what a ticket needs to enter or leave a column

This design defines what a ticket needs to move into or out of a column. Columns are
defined in [columns-and-phases.md](columns-and-phases.md), and it builds on the
four-verb surface of [path-addressed-cli.md](path-addressed-cli.md).

## The model in one paragraph

A column declares, in the workspace configuration beside its flags and its
`next` list, two lists of **requirements**: `exit`, what a ticket needs before
it may leave along a forward edge, and `entry`, what it needs before it may
arrive by any edge. A requirement is a test over a path of the ticket, or a named
check that groups tests, with a scope and a severity. The tests are evaluated as a
gate at the transition write `set x/status <column>`, against the column graph the
ticket resolves. A blocking requirement that fails refuses the write and nothing is
written; an advisory one that fails is printed and the write proceeds. Every
other surface that talks about readiness, `/next` first among them, reports
what that evaluation would say and enforces nothing.

## Entry and exit are both needed

Put a requirement on the side that owns its reason: **exit says this column's
work is complete, whatever comes next; entry says the destination needs this,
whatever it came from.** On a straight board the two are one check on one move,
and the shipped default uses whichever reads as the reason.

The two edges the engine supplies without `next` listing them, closing into a
terminal column and parking into the initial one, carry the destination's
entry requirements and never the source's exit requirements. Closing and parking
abandon a column's work rather than complete it, and a workspace that could not
close a backlog ticket as `wontfix` until it had a description would be refusing
the one write that makes the description unnecessary. Creation is the transition
into the initial column and carries that column's entry requirements like any other
arrival; the shipped default declares none, so creation is free.

## What a requirement is

A requirement has three values:

- **`test`**: one atom of the selector grammar the CLI already has, written
  against a path relative to the ticket, or a `check` naming a group of atoms
  declared under `[checks]`. The atoms are `p` (present: a scalar
  with a value, a list with an element), `!p` (absent), `p=v`, `p!=v` (the
  negation of `=`), `p~re`, and `p<v`, `p>v` on a number or date path. On a
  list path `=` and `~` hold when some element does, so `labels=ready` is
  "carries the label" and `labels!=needs-design` is "does not". `v` may be
  several values, `status=backlog,impl`: `=` holds when any matches and `!=`
  when none. Values are the CLI's values: a bare word or a quoted string; a
  date value is a date or time, or a duration before the evaluation's `now`,
  `updated<-6h`; every list exposes `count`, a derived number. An atom has no
  `and`, `or` or arithmetic; a column's list is the conjunction.
  The selector in `get '(status=backlog labels=bug)'` and a requirement's `test`
  are parsed by one grammar.
- **`scope`**: `self`, the default, reads the path on the ticket. `ancestors`
  reads it across the ticket and its ancestors: a list path is the union of
  the lists up the chain, a scalar path is the nearest value going up. That
  is what lets a mark on the epic satisfy a requirement on its ticket, lets a
  `needs-design` on the epic hold every ticket under it, and lets a field
  declared on level 2 be required before any of its tickets move.
- **`severity`**: `error` or `warning`, and it has no default.

Other needs are met by atoms reading a path the ticket already exposes: a field
present or matching is `points` or `target_release~'^\d+\.\d+$'`; a label present is
`labels=ready`; a passing review of the latest plan revision is `plan/verdict=pass`,
because `plan/verdict` is the derived field
[path-addressed-cli.md](path-addressed-cli.md) defines; the original a duplicate's
move names is `status/of`; a dependency closed is `!blockers`, where `blockers` is a
derived list, the members of `blocked_by` (the lane predecessor, and every ticket
whose `blocks` names this one) that are not in a terminal column.

A label is declared in the workspace file's top-level `labels` list and is applied
to an item, `add x/labels ready`, or computed from a `where` selector. It reads as
`labels=<name>` in a selector or a requirement; a test naming an undeclared label is
refused at parse, and a stored label that a later version stops declaring stays
stored and `/integrity` reports it.

Someone writes the fact (a field, a label, a verdict, a comment) and a requirement
reads it, or the engine exposes a new derived field in the field table, which is
engine code in one listed place, and a requirement reads that. The checklist exposes
`checklist/pending`, its count of pending items, and the requirement is
`checklist/pending=0`.

The configuration parse refuses, naming the key, a `test` whose path names a
field declared on no level, a `severity` that is missing or not one of the two,
a `scope` that is not one of the two, and an `exit` list on a terminal column,
which has no forward edge and so could never be evaluated. A path declared on
some level but not on this ticket's level reads as absent; that is what makes
`scope = "ancestors"` on a level-2 field work.

## The declaration

Requirements live on the columns of `.lit/workspace.toml`, the same file
`/columns` reads and whose working-tree draft it writes, so whether the author edits
TOML or runs `add /columns/backlog/exit '{…}'`, the version in force is the one
applied with `config commit` ([workspace-schema/design.md](workspace-schema/design.md)).
The shipped default is `internal/assets/workspace/default.toml`.

In the default, `merge`'s entry requirement reads `pr/verdict`, the verdict of the
last review naming the PR's latest revision; a `fail` keeps the ticket in `review`,
which is where rework happens ([columns-and-phases.md](columns-and-phases.md)).
`review`'s entry carries `plan/verdict=pass` as a warning: appending a plan revision
empties the derived verdict, so a ticket arriving at review with a plan revised since
its review is worth a line, and an agent that revised the plan for a reason it can
defend proceeds.

As paths, the same declaration is `/columns/backlog/exit`, a list
whose elements are objects, edited with `add` and `rm` like any list:

```
add /columns/backlog/exit '{test:points severity:warning}'
+ /columns/backlog/exit/5
```

## The requirement gate

Requirements are evaluated by one pure function, from the ticket's folded
state, the resolved column graph, the edge and the caller's `now`, to a list of
verdicts, one per requirement. It reads no clock and performs no I/O. `held`,
`--dry-run` and `/next` call the same function. The transition write is the only
caller that acts on its result: it evaluates the source column's exit list when the
edge is forward and the destination's entry list always, all of them, with no
short-circuit, so a refusal names everything unmet at once and an agent fixes
it in one pass. If any blocking requirement is unmet the write is refused and
nothing is written. The refusal is the CLI's refusal shape:

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
agent is not sent back to `get`; then the guidance of the ticket's current column,
the one that says how to leave it.

A requirement is checked as a gate only at the transition write. A ticket whose
column's requirements stop holding afterwards, a description deleted, a verdict
emptied by a new plan revision, stays where it is; the facts are in the history and
in any selector that asks, and in derived fields such as `held`.

## Severity

`error` is the refusal above. `warning` lets the write through and prints the
unmet requirement with the echo, in the same line shape, under the derivations:

```
$ lit set dee/status review
dee/status impl → review
  warning review/entry  plan/verdict=pass   verdict is empty; plan revised since the review of revision 3
  requirements: 1 evaluated · 0 met · 1 unmet (warning) · 0 unmet (error)
```

A warning needs no acknowledgement. Authors choose the severity per declaration.

## Ready to pull

In the default profile, readiness to pull is the initial
column's exit list: a description, the `ready` label on the ticket or an ancestor,
no `needs-design` or `external` on the ticket or an ancestor, no open blocker. It is
declared on exit rather than on the first active column's entry because its reason
is the ticket's fitness to leave, whatever it goes to next, and an epic that adds an
`audit` column before `plan` does not have to copy the list.

Who satisfies it: whoever grooms. Grooming an epic ends with
`add <epic>/labels ready`, and because the requirement's scope is `ancestors`
every ticket under the epic passes that requirement in one write, and a
follow-up filed into the groomed epic later passes it at birth. Removing the label
from the epic holds every ticket under it that does not carry its own, and the
label's arrival and removal are in the history with a writer and a time.

The label is data: there is no reserved label and no constant in code, and a
workspace that wants `groomed` or `ready-to-pull` edits the string. What grooming
means here is the initial column's guidance, which `get /columns/backlog/guidance`
reads on demand.

How the queue treats a ticket that fails: `/next` is a read, so it enforces
nothing; it reports the evaluation the start write would make. A derived field
carries that report on every ticket: **`x/held`** is the unmet blocking
requirements on each forward edge out of the ticket's current column, keyed by
destination, empty when some forward edge is open and empty on a terminal
column. It is the dry-run of the forward transition exposed as a path, so
`get '(held)'` lists the tickets whose requirements hold them and `get dee/held`
says why one ticket is held. How `/next` treats a held lane head is in
[work-claims.md](work-claims.md):

```
$ lit get /next
fw9   /lanes/parser   rank 2   backlog
held above it:
  /lanes/datetime  b64   plan ← backlog/exit  description empty; labels=ready absent on b64 and its ancestors (rin)
```

`get /lanes` prints `held` beside each head the same way.

## v1's holds, mapped

- `ready.required_fields` is an exit requirement on the initial column, one
  per field: `test = "<field>"`, `severity = "error"`. A field is required by a
  requirement on a column, and a field declaration carries no `required` flag
  ([workspace-schema/design.md](workspace-schema/design.md)); a field a workspace
  wants before `merge` rather than before `plan` is required on `merge.entry`.
- `needs-design` and `external` are the two `labels!=` requirements, scoped to
  ancestors.
- An open dependency, an inherited dependency, and an earlier sibling in the
  lane are `!blockers`.

## Requirements and the hierarchy

Requirements bind to level 1 only, because level 1 is the only level with a
stored status ([columns-and-phases.md](columns-and-phases.md)); a node's facts reach
requirements through `scope = "ancestors"`.

A node carries a `columns` section its tickets inherit, and descendants resolve by
walking up. Resolution is per column and whole: the nearest ancestor that declares a
column at all supplies that column's flags, `next`, `entry`, `exit` and `guidance`,
and nothing is merged across levels. An epic that wants one more exit requirement
on `backlog` redeclares `backlog` with the full list. The command event records which
node's section each column resolved to.

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
requirements before and after. In the local command log, a `--dry-run` records the
same event with `dry_run: true`.
