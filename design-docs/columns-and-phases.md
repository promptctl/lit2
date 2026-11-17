# Columns: the one concept behind status, phase and workflow state

Status: draft, 2026-11-05; amended 2026-11-17 to carry the computed-position
model as a future to explore. Aligns two earlier proposals, computed columns
and phases-and-rework, both since removed, with
[path-addressed-cli.md](path-addressed-cli.md) on one concept, under the owner's
ruling: *"phases and rework are equivalent to custom columns. rework is not a
separate phase, it's just part of review. each column is a phase. this probably
requires a separate design doc to align them."*

## One concept

A **column** is a named node in the column graph. A ticket's `status` field holds the
name of the column it is in. What the three source documents call a phase, a
state, or a workflow state is a column; this document says that once and uses
only the word column from here on. The graph spells it the same way:
`/columns/<name>`. One concept, one word, in the path a reader types
and the field a reader gets back `[LAW:domain-language]`.

## The column is stored; everything else is computed

Two models were on the table, and the choice is settled here, not split. One
computes a ticket's column from stored facts, configuration and the clock and
writes it nowhere. The other, [path-addressed-cli.md](path-addressed-cli.md),
stores `set x/status <column>` as a transition validated against the resolved
column graph at the write. This design stores the column and computes
everything else. The computed model is kept in the last section as a future to
explore, not decided against.

**What `set x/status review` stores.** One event: this ticket moved from `impl`
to `review`, by this writer, at this time. The write is accepted only if
`review` is reachable from `impl` in the resolved column graph and the
requirements on that edge hold; a refusal names the reason and writes nothing. Nothing else is stored.
The claim the transition establishes is derived from the trail, not written by it.

**What a ticket's column is a function of.** The newest status transition in
its trail, and nothing else. Not configuration, not the clock, not another
ticket. The fold answers it; two clones with the same events agree on it with
no rule to evaluate.

**What evaluate computes.** Everything but position:
`held`, stage, claim expiry, staleness, the rollup of a node above level 1.
`view = evaluate(fold(events), config, now)`, writing nothing, cached if wanted,
never the source of truth. What the facts say about a ticket is a derived
field such as `blockers` or `held`, or a label declared with a `where`
selector (architecture.md §3.11), which evaluate computes as it computes any
derived field; a plain label is exactly what was written to the item.
Conditional guidance is a handler with a selector
([workflows.md](workflows.md)), so one mechanism prints after a ticket.

Why the column is the one thing not computed:

- `[LAW:one-source-of-truth]`. If a stored move and a rule can both decide the
  column, "which one is lying" has no answer. The computed model's open
  question of a drag against a rule is that missing answer; with one authority
  the question does not arise.
- `[LAW:parse-dont-validate]`. The requirements are checked once, on the transition, and
  the stored column is the stamp. Re-checking entry conditions on every read is
  validation that never ends, and the computed model pays for it by replaying
  evaluate over history to learn whether a ticket is at a column's door or inside
  it.
- `[LAW:no-silent-failure]`, and the feedback stance in
  [project-intent.md](project-intent.md). A drag that is recorded and does not
  take effect is a write whose failure surfaces later, somewhere else. A refusal
  at the write, naming the requirement and the fact it read, is the early feedback the
  intent asks for, and the echo `impl → review` is true at the moment it prints.

What the computed model wanted from automatic settling is kept, as derived
fields. A ticket in `impl` whose blocker reopens stays in `impl` and its
`blockers` reads `4ou`; `/next` passes its lane over naming the reason; when the
blocker closes the field is empty on the next read, arrival order irrelevant, no
merge rule. The ticket's column is where its author put it; the derived field is
what the facts say about it there. `get x/held` names each unmet test, so "why
is this ticket stuck" is answered on the line, and "why is this ticket in
`impl`" is answered by `get x/history`, so no `why` command is needed.

Two constraints the computed model needed become structural. Derivations never
write facts: there is no action that could. Derivations never loop: a derived
field reads stored facts, stored columns and `now`, never another rule's
output, so which configuration evaluate applies and how competing placements
resolve have nothing left to decide. Derived fields under the active version,
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
/columns/merge       next:[done]    entry:[pr/verdict=pass]    guidance:"…"
/columns/done        terminal:true
/columns/dup         terminal:true  entry:[of]
/columns/superseded  terminal:true  entry:[of]
/columns/obsolete    terminal:true  entry:[reason]
/columns/wontfix     terminal:true  entry:[reason]
```

`entry` and `exit` are lists of requirements, the one shape
[transition-requirements.md](transition-requirements.md) defines and whose full
default declaration it holds. `in_progress` is `stage=active`; `closed` is
`stage=terminal`; v1's `open` is the default profile's initial column, named
`backlog`. The four names inside `in_progress` are columns, resolutions are
terminal columns, and the entry requirement `of` on `dup` is what makes the
canonical id mandatory. A lane listing prints the column, `dee  impl  this
checkout`, because `in_progress` is not a value `status` can hold, and
`review`'s forward edges are `[merge]`, for the reason in the rework section.

## Stage: the only thing the engine knows about a column

A column node carries `initial` and `terminal`, the two flags the engine reads.
`x/stage` is derived from the flags on the ticket's column in its resolved
column graph: `initial`, `active` (neither flag), or `terminal`. Everything the
engine does with a column it does through the stage:

- Start is any transition out of `initial`; done is any transition into a
  `terminal`.
- A transition out of `initial` or into a `terminal` makes its checkout the
  holder of the ticket's lane; parking a member into `initial`, finishing the
  lane's last member, and `rm <lane>/claim` end the hold.
- A lane's head is its first member whose stage is not `terminal`; `/next`
  serves heads by the lane rule and passes over one whose `held` is non-empty.
  It reads no column name.
- The stage of a node above level 1 is derived from its children's stages
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

`next`, `entry`, `exit` and `guidance` are data the engine applies
without knowing what any column means. `exit` requirements are checked on every
forward transition out of the column, never on the universal edges, so closing
as `wontfix` from `plan` needs no plan verdict; `entry` requirements are checked
on every arrival, which is how `dup` demands `of`
([transition-requirements.md](transition-requirements.md)). `guidance` is
printed after the ticket when the ticket is in the column, and is derived from
the handlers bound to that column ([workflows.md](workflows.md)). A handler's
moment names a column; one naming a column that does not exist in the node is
refused at the write, so guidance cannot describe a column that is not there.
The engine is not told that `plan` is planning or `review` is reviewing; it
knows there is a graph with one entrance, some exits, predicates on edges,
fields on entry, and text per node.

A resolved column graph is checked on every write that changes it: exactly one
`initial` column, at least one `terminal`, every `next` target present, and no
column removed while a ticket in the node's subtree holds it (`error (reason=in_use)`,
naming the tickets). Two concurrent writers can still leave a ticket in a
column its ancestors no longer declare: the fold keeps the transition, evaluate
reports the column as undeclared with stage `active`, `/next` passes the lane
over naming the reason, `/integrity` lists it, and the universal edges always
offer a way out.

## Rework is inside review

Work comes back from review when a reviewer records findings. Under the owner's
ruling the ticket does not move: it is in `review` before the findings and in
`review` after them, and it leaves `review` forward only when a review passes.
The phases-and-rework proposal's shapes A (a fifth column) and B
(`impl` entered from `review`, derived) are both superseded; there is no return
to `impl` in the default profile because returning is not what happens.

**What is recorded.** The review itself. `x/pr` is a reviewable, the same type
as `x/plan` `[LAW:one-type-per-behavior]`: two append-only lists, `revisions`,
each a head commit, and `reviews`, each naming the revision it judged (`of`,
required) and carrying how it was run, the reviewer's result, the agent's
response and a `pass` or `fail` verdict, with writer and time, id minted by the
system.

```
$ lit add s4l/pr/reviews '{of:1 how:"/code-review high" result:"…" verdict:fail}'
+ s4l/pr/reviews/9k2  fail, of:1
⇒ s4l/pr/verdict  (none) → fail   round 2
```

`x/pr/verdict` is derived: the verdict of the last review naming the latest
revision, or `(none)`. The round is derived: one more than the number of `fail`
reviews, so the first trip through review is round 1 and each fail opens the
next. Nothing records "rework" and nothing asks the author to; the review is
the whole record, and the author's answer to it is the `response` on that
review, a push recorded as the next revision, and the next review. A new
revision empties the derived verdict until a review names it.

**What the agent sees.** `get s4l` on the second trip:

```
$ = orange-history-4y3.s4l
$/status    review   round 2 · pr/verdict fail (40m ago)
$/claim     this checkout
$/pr/reviews/9k2   [claude_64e3…] of:1 fail: …

guidance (review, pr/verdict=fail): address every finding or say on the thread
why not; do not widen the change; …
```

The second line of guidance is a shipped handler bound `in review` with the
selector `(pr/verdict=fail)` ([workflows.md](workflows.md)). On round 1 the
selector does not match and only `review`'s own guidance prints. That is the
hook the phases-and-rework proposal said could not be derived; it is a handler
reading a stored fact.

**What differs between the first trip and the second.** Stored: the first trip
has one transition `impl → review` and no reviews; the second has the same one
transition and one `fail` review. Printed: `round 1` with no verdict line, then
`round 2 · pr/verdict fail` with the conditional guidance. A selector finds the
rework in flight with `(status=review pr/verdict=fail)`, and a report counts
time in rounds above 1 from the review timestamps on the trail.

**The forward edge.** `merge` carries the entry requirement `pr/verdict=pass`:

```
$ lit set s4l/status merge
error (reason=gate): s4l/status review → merge refused by merge/entry; nothing written
  block  pr/verdict=pass   s4l/pr/verdict = fail (round 2)

$ lit add s4l/pr/revisions 7e0f15c2
+ s4l/pr/revisions/2
$ lit add s4l/pr/reviews '{of:2 how:"/code-review high" result:"…" verdict:pass}'
+ s4l/pr/reviews/c3q  pass, of:2
⇒ s4l/pr/verdict  fail → pass
$ lit set s4l/status merge
s4l/status  review → merge
```

The plan gets the same treatment for free: `plan` is the same reviewable, a
`fail` there keeps the ticket in `plan`, and the round counts the same way.
That answers phases-and-rework.md's second decision without a second mechanism.

## Nothing is built in; a node above level 1 has a stage, not a column

The engine ships no column names. The ten columns above and their requirements
are the default profile, data `lit admin init` writes to `.lit/workspace.toml`
and applies as version one; a workspace edits the file, or its working-tree
draft through the four verbs, and applies it with `config commit`. A workspace
may rename, add or remove any column subject to the checks in the stage section.

Only a level-1 item, a leaf, has a `status`. A node above level 1 (the default
profile's `feature` and `epic`) has no column to be in: it has a derived
`stage`, `terminal` when every child's stage is `terminal`, `initial` when
every child's is `initial` or it has no children, `active` otherwise, and a
derived `rollup`, the count of leaves under it per column. `set epic/status`
fails at parse as an address to a field the level does not have, which is the
CLI document's "an epic's status is derived from its children" made exact: it is
finished or reopened only through them, and what it shows is their columns, not
one of its own.

```
$ lit get 792a/stage 792a/rollup
792a/stage    active
792a/rollup   backlog 1 · impl 2 · review 1 · done 4
```

`(status=review)` therefore matches level-1 items only; `(stage=active)` matches
every level.

## Nearest ancestor: a column added on an epic

A node's column graph resolves by walking up, per column name: a column defined at
the nearest ancestor shadows the same name above it, and columns it does not
define are inherited. An epic that adds a column edits the graph around it in
its own node:

```
rin/columns/audit       next:[impl]   guidance:"…"
rin/columns/plan/next   [audit]
```

The resolved graph for `rin`'s children is the default with `audit` between
`plan` and `impl`; the resolution checks run on that resolved graph when either
line is written. For selectors, `audit` is a column name like any other:
`(status=audit)` matches tickets holding it under any node that defines it, and
`(stage=active)` matches them along with every other in-flight ticket, because
stage is derived from the resolved column's flags, not from a global list. For
the queue, nothing changes: a ticket in `audit` is in flight, keeps its claim,
and holds its lane's head, since `/next` reads stage and `held` only. Moving a
ticket under a node whose resolved graph lacks the column it holds is
refused at the write, the same check that refuses removing a held column.

## Settled with the owner

The column graph's node is `/columns`, settled on 2026-11-05: the owner reserved
the word workflow for a named unit of handlers ([workflows.md](workflows.md)),
so the graph of columns cannot share it.

A review names what it judged: `of` is required and names a revision of the
reviewable, and a PR's revision is the head commit the author records with
`add x/pr/revisions`. lit sees no pushes, so a push the author does not record
is not a revision.

## A future to explore: position computed by rules

Not decided against. The proposal this document superseded on 2026-11-05
computed a ticket's column rather than storing it, and the owner wants that
option kept open, so the model is stated here in enough detail to pick up.

**The model.** A column is a name in configuration and nothing else. Behavior
comes from rules of one kind: when a condition holds for a ticket entering,
leaving or sitting in a column, produce an action, and the action places the
ticket in a column, shows a label or prints guidance. A ticket's column is not
stored. Every read computes the board, `board = evaluate(state, config, now)`, and
writes nothing; a ticket sits in the first column in configuration order until
a drag or a rule puts it elsewhere.

```toml
[[rule]]
when = "blockers not closed"
entering = "doing"
then = { place = "backlog" }

[[rule]]
when = "no update in 72h"
in = "doing"
then = { place = "backlog" }
```

**What it buys.** Tickets settle. A written move is a stored reaction that
outlives its condition: a demotion written because a blocker reopened survives
the blocker closing again, and moves cascade. A computed position has neither
problem; when the blocker closes the ticket is back on the next read, arrival
order irrelevant, no merge rule. Entry rules hold a ticket at the door until
they pass and nothing has to drag it again. A drag is a fact rules read, never
an override, so a column whose only rules are labels always accepts one.

**What it costs.** Position stops being a command. Whether a ticket is at a
column's door or inside it depends on whether its entry rules passed since the
drag, so a read replays evaluate over past moments for every ticket shown and every
ticket their rules read; a cache makes that affordable and is never the source
of truth. "Why is this ticket in backlog" becomes the common question and wants
a command that prints each rule that placed it and the facts it read.

**What it would have to settle before it could be built.** A drag against a
rule: the proposal recommended rules win, with the drag labeled with why,
because an absence such as "no PR" has no event to be newer than. Competing and
looping rules: first match in configuration order, placement repeated until
nothing moves, a loop stopped at the first column reached and labeled. Which
configuration evaluate applies: the current one, so editing a rule re-settles every
ticket, a different choice from validation's. Which past moments the replay
visits and which `now` each uses, given that recorded times are not monotonic
across writers. Two implementations that choose differently give two boards
from the same facts, so all of it is fixed first.

**Where today's design already meets it.** `view = evaluate(fold(events), config,
now)` is the evaluation it needs. A label with a `where` selector
(architecture.md §3.11) is its label action; `held`, `blockers` and staleness
are its conditions, as derived fields; a handler with a selector is its
guidance action. The step this design did not take is a rule whose action is
`place`, and the question it leaves is what a computed column means beside the
stored `status`, or instead of it.
