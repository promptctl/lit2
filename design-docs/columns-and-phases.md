# Columns: the one concept behind status, phase and workflow state

Status: draft, 2026-11-05. Aligns [custom-columns.md](custom-columns.md),
the v1 phases-and-rework proposal (since removed) and
[path-addressed-cli.md](path-addressed-cli.md) on one concept, under the owner's
ruling: *"phases and rework are equivalent to custom columns. rework is not a
separate phase, it's just part of review. each column is a phase. this probably
requires a separate design doc to align them."*

## One concept

A **column** is a named node in a workflow. A ticket's `status` field holds the
name of the column it is in. What the three source documents call a phase, a
state, or a workflow state is a column; this document says that once and uses
only the word column from here on. The workflow node spells it the same way:
`/columns/<name>`. One concept, one word, in the path a reader types
and the field a reader gets back `[LAW:domain-language]`.

## The column is stored; everything else is computed

This is the tension between the two later documents, and it is settled here, not
split. [custom-columns.md](custom-columns.md) says a ticket's column is a
function of stored facts, configuration and the clock, written nowhere.
[path-addressed-cli.md](path-addressed-cli.md) says `set x/status <column>` is a
stored transition, validated against the resolved workflow at the write. The
CLI document is right about the column. The custom-columns document is right
about everything else.

**What `set x/status review` stores.** One event: this ticket moved from `impl`
to `review`, by this writer, at this time. The write is accepted only if
`review` is reachable from `impl` in the resolved workflow and the
requirements on that edge hold; a refusal names the reason and writes nothing. Nothing else is stored.
The claim the transition stamps is derived from the trail, not written by it.

**What a ticket's column is a function of.** The newest status transition in
its trail, and nothing else. Not configuration, not the clock, not another
ticket. The fold answers it; two clones with the same events agree on it with
no rule to evaluate.

**What tick computes.** Everything custom-columns.md computes except position:
labels, readiness, stage, claim expiry, the rollup of a level-2 or
level-3 item. `labels = tick(fold(events), config, now)`, writing nothing, cached if
wanted, never the source of truth. The rule type survives with one action:
a rule is `when <condition> [in <column>] then label`. There is no `place`, and
there is no `guidance` action either: conditional guidance is a handler with a
selector ([workflows.md](workflows.md)), so one mechanism prints after a ticket.

Why the column is the one thing not computed:

- `[LAW:one-source-of-truth]`. If a stored move and a rule can both decide the
  column, "which one is lying" has no answer. custom-columns.md's decision 2
  (drag versus rule) is that missing answer; with one authority the question
  does not arise.
- `[LAW:parse-dont-validate]`. The gate is checked once, on the transition, and
  the stored column is the stamp. Re-checking entry conditions on every read is
  validation that never ends, and custom-columns.md's own cost section says what
  it costs: replaying tick over history to learn whether a ticket is at a
  column's door or inside it.
- `[LAW:no-silent-failure]`, and the feedback stance in
  [project-intent.md](project-intent.md). A drag that is recorded and does not
  take effect is a write whose failure surfaces later, somewhere else. A refusal
  at the write, naming the gate and the fact it read, is the early feedback the
  intent asks for, and the echo `impl → review` is true at the moment it prints.

What custom-columns.md wanted from automatic settling is kept, as labels. A
ticket in `impl` whose blocker reopens stays in `impl` and reads
`impl  blocked (4ou)`; `/next` skips it; when the blocker closes the label is
gone on the next read, arrival order irrelevant, no merge rule. The ticket's
column is where its author put it; the label is what the facts say about it
there. A label names the rule that produced it, so "why does this show
`blocked`" is answered on the line, and "why is this ticket in `impl`" is
answered by `get x/history`, which makes custom-columns.md's `lit why`
unnecessary.

Two of custom-columns.md's constraints become structural. Rules never write
facts: there is no action that could. Rules never loop: a rule reads stored
facts, stored columns and the clock, never another rule's output, so its
decisions 3 and 5 (which configuration tick applies, how competing placements
resolve) have nothing left to decide. Labels under the current configuration,
columns from the trail.

## v1's three axes are one list of columns

v1 holds a status (`open`, `in_progress`, `closed`), one of `plan`, `impl`,
`review`, `merge` inside `in_progress`, and a resolution on `closed` (`done`,
`dup`, `superseded`, `obsolete`, `wontfix`). In v2 these are ten
columns in the default profile, and `in_progress` and `closed` are not among
them:

```
/columns/backlog     initial:true   next:[plan]    exit:[…]                 guidance:"…"
/columns/plan        next:[impl]    guidance:"…"
/columns/impl        next:[review]  entry:[plan/verdict=pass]  guidance:"…"
/columns/review      next:[merge]   guidance:"…"
/columns/merge       next:[done]    entry:[verdict=pass]       guidance:"…"
/columns/done        terminal:true
/columns/dup         terminal:true  entry:[of]
/columns/superseded  terminal:true  entry:[of]
/columns/obsolete    terminal:true  entry:[reason]
/columns/wontfix     terminal:true  entry:[reason]
/rules                        [{when:"blocks any stage!=terminal"  label:blocked}]
```

`entry` and `exit` are lists of requirements, the one shape
[transition-requirements.md](transition-requirements.md) defines and whose full
default declaration it holds; the CLI document's `gate` and `requires` keys are
those lists. `in_progress` is `stage=active`; `closed` is `stage=terminal`; v1's
`open` is the default profile's initial column, named `backlog`. The CLI
document's treatment is otherwise confirmed: the four names inside `in_progress`
are columns, resolutions are terminal columns, and the entry requirement `of` on
`dup` is what makes the canonical id mandatory.
Two lines in it are corrected. The lane listing under "Reading a lane" prints
`dee  in_progress  this checkout`; it prints the column, `dee  impl  this
checkout`, because `in_progress` is not a value `status` can hold. And
`review`'s forward edges are `[merge]`, not `[merge, impl]`, for the reason in
the rework section.

## Stage: the only thing the engine knows about a column

A column node carries `initial` and `terminal`, the two flags the engine reads.
`x/stage` is derived from the flags on the ticket's column in its resolved
workflow: `initial`, `active` (neither flag), or `terminal`. Everything the
engine does with a column it does through the stage:

- Start is any transition out of `initial`; done is any transition into a
  `terminal`.
- A transition stamps a claim when the ticket has none and clears it on
  entering `initial` or a `terminal`.
- A lane's head is its first member whose stage is not `terminal`; `/next`
  serves heads by the lane rule and skips one a label blocks. It reads no
  column name.
- A level-2 or level-3 item's stage is derived from its children's stages
  (below).

Which columns a transition may enter follows from `next` alone, and this is the
one place the CLI document's text and its own examples disagree: it says every
terminal column is reachable from everywhere, and its example refuses
`plan → done` because `done` is not in `plan/next`. The rule that makes both
true: **a column some `next` names is entered only along those edges; a column
no `next` names is entered from every other column.** In the default profile
that makes `done` reachable only through `merge`, and `backlog`, `dup`,
`superseded`, `obsolete` and `wontfix` reachable from anywhere, which is parking,
reopening, and closing without finishing. The flags are not consulted for
edges; they give stage and nothing else.

`next`, `entry`, `exit`, `guidance` and `rules` are data the engine applies
without knowing what any column means. `exit` requirements are checked on every
forward transition out of the column, never on the universal edges, so closing
as `wontfix` from `plan` needs no plan verdict; `entry` requirements are checked
on every arrival, which is how `dup` demands `of`
([transition-requirements.md](transition-requirements.md)). `guidance` is
printed after the ticket when the ticket is in the column, and is derived from
the handlers bound to that column ([workflows.md](workflows.md)). A rule's `in`
names a column, and a handler's moment does; either naming a column that does
not exist in the frame is refused at the write, so neither a label nor guidance
can describe a column that is not there. The engine is not told that `plan` is
planning or `review` is reviewing; it knows there is a graph with one entrance,
some exits, predicates on edges, fields on entry, and text per node.

A resolved workflow is checked on every write that changes it: exactly one
`initial` column, at least one `terminal`, every `next` target present, and no
column removed while a ticket in the frame holds it (`error (reason=in_use)`,
naming the tickets). A stored column that its workflow does not define is
therefore unrepresentable, and no reader needs a fallback for it
`[LAW:types-are-the-program]`.

## Rework is inside review

Work comes back from review when a reviewer records findings. Under the owner's
ruling the ticket does not move: it is in `review` before the findings and in
`review` after them, and it leaves `review` forward only when a review passes.
The phases-and-rework proposal's shapes A (a fifth column) and B
(`impl` entered from `review`, derived) are both superseded; there is no return
to `impl` in the default profile because returning is not what happens.

**What is recorded.** The review itself. `x/verdicts` is a list on the ticket,
the same element type as `x/plan/verdicts` `[LAW:one-type-per-behavior]`: a
`pass` or `fail` with findings, writer and time, id minted by the system.

```
$ lit add s4l/verdicts '{fail findings:["…", "…"]}'
+ s4l/verdicts/9k2  fail, 2 findings
⇒ s4l/verdict  (none) → fail   round 2
```

`x/verdict` is derived: the newest verdict, or `(none)`. The round is derived:
one more than the number of `fail` verdicts, so the first trip through review
is round 1 and each fail opens the next. Nothing records "rework" and nothing
asks the author to; the reviewer's verdict is the whole record, and the
author's answer to it is a comment, a push, and the next verdict. lit sees no
pushes, so nothing empties a verdict but a newer verdict.

**What the agent sees.** `get s4l` on the second trip:

```
$ = orange-history-4y3.s4l
$/status    review   round 2 · verdict fail (2 findings, 40m ago)
$/claim     this checkout
$/verdicts/9k2   [claude_64e3…] fail: …

guidance (review, verdict=fail): address every finding or say on the thread
why not; do not widen the change; …
```

The second line of guidance is a shipped handler bound `in review` with the
selector `(verdict=fail)` ([workflows.md](workflows.md)). On round 1 the
selector does not match and only `review`'s own guidance prints. That is the
hook the phases-and-rework proposal said could not be derived; it is a handler
reading a stored fact.

**What differs between the first trip and the second.** Stored: the first trip
has one transition `impl → review` and no verdicts; the second has the same one
transition and one `fail` verdict. Printed: `round 1` with no verdict line, then
`round 2 · verdict fail` with the conditional guidance. A selector finds the
rework in flight with `(status=review verdict=fail)`, and a report counts time
in rounds above 1 from the verdict timestamps on the trail.

**The forward edge.** `merge` carries the entry requirement `verdict=pass`:

```
$ lit set s4l/status merge
error (reason=gate): s4l/status review → merge refused by merge/entry; nothing written
  block  verdict=pass   s4l/verdict = fail (round 2)

$ lit add s4l/verdicts '{pass}'
+ s4l/verdicts/c3q  pass
⇒ s4l/verdict  fail → pass
$ lit set s4l/status merge
s4l/status  review → merge
```

The plan gets the same treatment for free: `plan/verdicts` already exists, a
`fail` there keeps the ticket in `plan`, and the round counts the same way.
That answers phases-and-rework.md's second decision without a second mechanism.

## Nothing is built in; levels 2 and 3 have a stage, not a column

The engine ships no column names. The ten columns above, their gates and the
two rules are the default profile, data written by `lit admin init` and edited
with the four verbs like any other node. A workspace may rename, add or remove
any of them subject to the checks in the stage section.

Only a level-1 item has a `status`. A level-2 item (today's epic) has no column
to be in: it has a derived `stage`, `terminal` when every child's stage is
`terminal`, `initial` when every child's is `initial` or it has no children,
`active` otherwise, and a derived `rollup`, the count of children per column.
A level-3 item derives both the same way over its level-2 children's stages and
rollups. `set epic/status` fails at parse as an address to a field the level
does not have, which is the CLI document's "an epic's status is derived from its
children" made exact: it is finished or reopened only through them, and what it
shows is their columns, not one of its own.

```
$ lit get 792a/stage 792a/rollup
792a/stage    active
792a/rollup   open 1 · impl 2 · review 1 · done 4
```

`(status=review)` therefore matches level-1 items only; `(stage=active)` matches
every level.

## Nearest ancestor: a column added on an epic

A node's workflow resolves by walking up, per column name: a column defined at
the nearest ancestor shadows the same name above it, and columns it does not
define are inherited. An epic that adds a column edits the graph around it in
its own frame:

```
rin/columns/audit       next:[impl]   guidance:"…"
rin/columns/plan/next   [audit]
```

The resolved workflow for `rin`'s children is the default with `audit` between
`plan` and `impl`; the resolution checks run on that resolved graph when either
line is written. For selectors, `audit` is a column name like any other:
`(status=audit)` matches tickets holding it in any frame that defines it, and
`(stage=active)` matches them along with every other in-flight ticket, because
stage is derived from the resolved column's flags, not from a global list. For
the queue, nothing changes: a ticket in `audit` is in flight, keeps its claim,
and holds its lane's head, since `/next` reads stage and labels only. Moving a
ticket into a frame whose resolved workflow lacks the column it holds is
refused at the write, the same `in_use` check from the other side.

## Corrections to the sibling documents

- custom-columns.md: the `place` action and the computed position are removed;
  its decisions 2, 3 and 5 dissolve; its decision 4 (phases become columns) is
  yes, and decision 1 (named groups) is answered by stage.
- phases-and-rework.md: neither A nor B; rework stays in `review`, recorded as
  verdicts.
- path-addressed-cli.md: `/workflow/states` is spelled `/columns`;
  the lane listing prints the column, not `in_progress`; `review/next` is
  `[merge]`; reachability follows from `next` alone, which is what its own
  `plan → done` example already assumes; levels 2 and 3 have `stage` and
  `rollup`, no `status`.

## Decisions for the owner

The column graph's node is `/columns`, settled on 2026-11-05: the owner reserved
the word workflow for a named unit of handlers ([workflows.md](workflows.md)),
so the graph of columns cannot share it.

1. **Whether a verdict on the work names what it judged.** Recommended: an
   optional `of` on the verdict (a commit or PR reference), recorded and printed,
   checked by nothing, because lit cannot see the code. The alternative is to
   record a digest of the work and empty the verdict when it changes, as the
   plan does; that requires lit to be told about every push.
