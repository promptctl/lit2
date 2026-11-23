# Fold and evaluate

Carries out `strategy/derivation.md`.

## Two functions

Every derived value has one of two homes.

- **`fold(events) → state`**, a pure function of events: items, fields,
  comments, labels applied, links, lanes, their order and their rank,
  checklists, lane holders, the revisions and reviews of each reviewable,
  the applied workspace versions and which is in force, and every unresolved
  concurrent contradiction as a value on the thing it concerns.
- **`evaluate(state, config, now) → view`**, a pure function of folded
  state, the active workspace version and `now`: `held`, claim expiry,
  staleness, stage, rollup, and the labels computed from a `where` selector.

Every read is `evaluate(fold(events), config, now)`, and every write is
judged against the same. Neither function performs I/O. Only facts are
stored: stage, `held`, a lane's holder, a verdict, `/next` and every other
derived value are computed on read, and a read writes no event.

## Order

The fold orders events by `(causal-position, writer-id, event-id)`, a total
order every machine computes identically, and applies them by pure per-type
rules. How `causal-position` advances is the event model's
(`specifications/events-and-store.md`).

Which event superseded which is the total order's question alone: a ticket's
column is its newest status transition in that order, and a lane's holder
its latest claim event. A question that asks *when*, such as a claim's last
refresh or a ticket's last touch, is answered by the largest `recorded-at`
among the events that qualify (`INV:wall-time-is-data`). The total order
cannot answer it, because between concurrent events it is a tiebreak, not a
sequence in time.

The fold may not be partitioned or parallelized by subject:
`place X above Y` concurrent with `place Y above X` has different subjects
and contradictory content, and only the one total order settles it
identically everywhere.

## The contract

`INV:deterministic-fold`: the same event set gives equal state on every
machine, in any arrival order, from genesis or from a snapshot. Two
properties make it:

1. **Total.** Every event in the readable set is applied or explicitly
   surfaced (`INV:preserve-unknown`); there is no quiet skip.
2. **Deterministic.** No clock reads, no map-iteration order, no filesystem
   or environment input. No user-supplied code runs in the fold.

The **fold version** is an explicit constant that bumps with any rule
change, which invalidates every snapshot, so no machine mixes two folds'
outputs. The snapshot's content and key are the fold's; its bytes are the
store's (`specifications/events-and-store.md`).

Determinism is tested as equality between independent folds of one event
set, through the state's own equality and never through a pinned
serialization.

## Time enters at the edges

The fold reads each event's `recorded-at` as input, like any payload field,
and keeps each writer's largest. A question whose answer depends on the
present, such as whether a ticket has gone untouched for 72 hours or whether
a claim's window has lapsed, is answered by evaluate, which takes the folded
state and the reader's `now` and writes nothing. The fold's output stays
cacheable because nothing in it depends on when it was computed.

Evaluate compares each writer's largest `recorded-at` to `now`. A writer
ahead of `now` by more than a tenth of the reader's freshness window is
surfaced as an advisory naming that writer and the reader's own checkout,
because a fast writer and a slow reader look identical from here: on a slow
machine every active writer appears ahead, and the advisory is how that
machine learns its own clock is behind.

## Writes are judged at the write

The active workspace version defines what a valid write is: levels, fields,
columns and their requirements. The requirement gate in the verbs judges a
write against the writer's own current fold and the version it ran under,
and refuses there; the event records that version as `config-ref`
(`INV:config-versioned`).
Verification and the capability rule are a step of the fold
(`specifications/access.md`, "Verification").

## Contradictions are values

Concurrent offline machines can write individually legal, jointly
contradictory facts. The fold settles each class by the deterministic rule
for that structure and holds the contradiction as a value on the thing it
concerns, with an advisory. `/integrity` is the one list of them, and each
line names the write that clears it.

- **A field.** Concurrent writes to one field are candidates the fold holds
  together; `get` prints each with its writer and time. A write names, in
  its `over`, the candidates it saw and replaces, so a `set` by a writer that
  folded both is the resolution, and a write that sorts after both but names
  neither is a third candidate. No diverged state exists.
- **Prose.** Concurrent rewrites of a title, a description or an agent
  prompt to different text are not settled by a rule: the field renders as
  conflicted with both texts addressable, and the resolution is another
  event from whoever judges the merge, a holder of the field's tier. The
  fold compares envelope commitments, not envelope bytes, so two writes of
  the same text are one value (`INV:opaque-envelope`). A checkout that
  cannot read the tier reports which tier is needed and leaves the conflict
  for a keyholder.
