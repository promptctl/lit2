# Columns: status, phase and workflow state

## One concept

A **column** is a named node in the column graph. A ticket's `status` field holds the
name of the column it is in. A phase, a state, or a workflow state is a column. The
graph spells it the same way: `/columns/<name>`.

## The column is the stored position; derived values are computed

`set x/status <column>` stores a transition validated against the resolved column
graph at the write. The column is stored, and the values derived from it are
computed. The last section records a model that computes the column instead.

**What `set x/status review` stores.** One event: this ticket moved from `impl`
to `review`, by this writer, at this time. The write is accepted only if
`review` is reachable from `impl` in the resolved column graph and the
requirements on that edge hold ([transition-requirements.md](transition-requirements.md)).
A start, a transition out of the initial column, also writes a claim event
([work-claims.md](work-claims.md)).

**What a ticket's column is a function of.** The newest status transition in
its trail, and nothing else. The fold answers it.

**What evaluate computes.** Everything but position:
`held`, stage, claim expiry, staleness, the rollup of a node above level 1.
`view = evaluate(fold(events), config, now)`, writing nothing, cached if wanted,
never the source of truth. What the facts say about a ticket is a derived
field such as `blockers` or `held`, or a label declared with a `where`
selector ([transition-requirements.md](transition-requirements.md)), which evaluate computes as it computes any
derived field. Conditional guidance is a handler with a selector
([workflows.md](workflows.md)).

The column is stored rather than computed because if a stored move and a rule could
both decide it, which one is right would have no answer.

Settling is expressed as derived fields. A ticket in `impl` whose blocker reopens
stays in `impl` and its `blockers` reads `4ou`; `/next` passes its lane over naming
the reason; when the blocker closes the field is empty on the next read, arrival
order irrelevant, no merge rule. `get x/held` names each unmet test, and
`get x/history` answers why a ticket is in its column.

## v1's three axes are one list of columns

v1 holds a status (`open`, `in_progress`, `closed`), one of `plan`, `impl`,
`review`, `merge` inside `in_progress`, and a resolution on `closed` (`done`,
`dup`, `superseded`, `obsolete`, `wontfix`). In v2 these are columns in the default
profile, `internal/assets/workspace/default.toml`, and `in_progress` and `closed` are
not among them.

`entry` and `exit` are lists of requirements
([transition-requirements.md](transition-requirements.md)). `in_progress` is
`stage=active`; `closed` is `stage=terminal`; v1's `open` is the default profile's
initial column, named `backlog`. The four names inside `in_progress` are columns,
resolutions are terminal columns, and the entry requirement `status/of` on `dup` is
what makes the canonical id mandatory. A lane listing prints the column, `dee  impl
this checkout`, because `in_progress` is not a value `status` can hold, and
`review`'s forward edges are `[merge]`, for the reason in the rework section.

## Stage: what the engine reads from a column's flags

A column node carries `initial` and `terminal`, the two flags the engine reads.
`x/stage` is derived from the flags on the ticket's column in its resolved
column graph: `initial`, `active` (neither flag), or `terminal`. Start, done and
`/next` read a column through its stage:

- Start is any transition out of `initial`; done is any transition into a
  `terminal`.
- A lane's head is its first member whose stage is not `terminal`; `/next`
  serves heads by the lane rule in [work-claims.md](work-claims.md).

Which columns a transition may enter follows from `next`: **a column some `next`
names is entered only along those edges; a column no `next` names is entered from
every other column.** In the default profile that makes `done` reachable only through
`merge`, and `backlog`, `dup`, `superseded`, `obsolete` and `wontfix` reachable from
anywhere, which is parking, reopening, and closing without finishing. The flags are
not consulted for edges; they give stage.

`entry` and `exit` requirements are checked as
[transition-requirements.md](transition-requirements.md) defines, and a column's
guidance is the handlers bound to it ([workflows.md](workflows.md)).

A resolved column graph is validated whenever it changes
([workspace-schema/design.md](workspace-schema/design.md)). Two concurrent writers
can still leave a ticket in a column its ancestors no longer declare: the fold keeps
the transition, evaluate reports the column as undeclared with stage `active`,
`/next` passes the lane over naming the reason, `/integrity` lists it, and the
universal edges offer a way out.

## Rework is inside review

Work comes back from review when a reviewer records findings. In the default profile
the ticket does not move: it is in `review` before the findings and in `review` after
them, and it leaves `review` forward when a review passes.

**What is recorded.** The review itself. `x/pr` is a reviewable, the same type
as `x/plan` ([path-addressed-cli.md](path-addressed-cli.md)): its `revisions` are
head commits, and each review names the revision it judged (`of`) and carries how it
was run, the reviewer's result, the agent's response and a `pass` or `fail` verdict.
A PR's revision is the head commit the author records with `add x/pr/revisions`.

```
$ lit add s4l/pr/reviews '{of:1 how:"/code-review high" result:"…" verdict:fail}'
+ s4l/pr/reviews/9k2  fail, of:1
⇒ s4l/pr/verdict  (none) → fail   round 2
```

The round is derived: one more than the number of `fail` reviews, so the first trip
through review is round 1 and each fail opens the next. There is no rework record:
the review is the record, and the author's answer to it is the `response` on that
review, a push recorded as the next revision, and the next review.

**What the agent sees.** `get s4l` on the second trip:

```
$ = orange-history-4y3.s4l
$/status    review   round 2 · pr/verdict fail (40m ago)
$/claim     this checkout
$/pr/reviews/9k2   [claude_64e3…] of:1 fail: …

guidance (review, pr/verdict=fail): address every finding or say on the thread
why not; …
```

The second line of guidance is a shipped handler bound `in review` with the
selector `(pr/verdict=fail)` ([workflows.md](workflows.md)). On round 1 the
selector does not match and only `review`'s own guidance prints.

**What differs between the first trip and the second.** In this example, stored:
the first trip has one transition `impl → review` and no reviews; the second has the
same one transition and one `fail` review. Printed: `round 1` with no verdict line,
then `round 2 · pr/verdict fail` with the conditional guidance. A selector finds the
rework in flight with `(status=review pr/verdict=fail)`, and a report counts
time in rounds above 1 from the review timestamps on the trail.

**The forward edge.** In the default profile `merge` carries the entry requirement
`pr/verdict=pass`:

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

In the default profile the plan gets the same treatment: `plan` is the same
reviewable, a `fail` there keeps the ticket in `plan`, and the round counts the same
way.

## A node above level 1 has a stage, not a column

The engine ships no column names. The default profile's columns and their
requirements are data `lit admin init` writes to `.lit/workspace.toml`
and applies as version one; a workspace edits the file, or its working-tree
draft through the four verbs, and applies it with `config commit`. A workspace
may rename, add or remove any column subject to the validation in
[workspace-schema/design.md](workspace-schema/design.md).

Only a level-1 item, a leaf, has a `status`. A node above level 1 (the default
profile's `feature` and `epic`) has no column to be in: it has a derived
`stage`, `terminal` when every child's stage is `terminal`, `initial` when
every child's is `initial` or it has no children, `active` otherwise, and a
derived `rollup`, the count of leaves under it per column.

```
$ lit get 792a/stage 792a/rollup
792a/stage    active
792a/rollup   backlog 1 · impl 2 · review 1 · done 4
```

## Nearest ancestor: a column added on an epic

A node's column graph resolves by nearest ancestor, per column name, as
[transition-requirements.md](transition-requirements.md) defines. An epic that adds
a column edits the graph around it in its own node:

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
the queue, a ticket in `audit` is in flight, keeps its claim, and holds its lane's
head. Moving a ticket under a node whose resolved graph lacks the column it holds is
refused at the write, the same check that refuses removing a held column.

## Position computed by rules

Specification: Open. A model that computes a ticket's column from facts,
configuration and the clock, instead of storing it, is not built. Recorded times are
not monotonic across writers, and two implementations that settle its precedence,
configuration-version and clock-ordering rules differently give two boards from the
same facts, so those rules are fixed before it is built.
