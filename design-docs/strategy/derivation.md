# Derivation

How every machine reads the same backlog from the same events, and where each
derived value is computed.

## Diagnosis

No write is coordinated. Two machines write offline for a day and then meet;
each must end with the same backlog as the other, with nothing lost and every
disagreement visible. A value stored by a writer that some rule could also
compute has two homes, and when they disagree nothing says which is right. A
write that records a result ("position 4") instead of an intent becomes false
the moment a concurrent write lands. Wall clocks on different machines skew,
so any order built on them differs between machines. And many answers depend
on the present (has a claim lapsed, has a ticket gone untouched), which no
stored value can keep current.

## Guiding policy

Store only facts, as intents, and derive everything else with pure functions
sorted into two homes: the **fold**, a function of events alone, and
**evaluate**, a function of folded state, the configuration in force and the
reader's `now`. Every read is evaluate over the fold, and every write is
judged against the same view.

## Actions

**One total order with no wall clock.** The fold applies events in an order
every machine computes identically from the events themselves, so the same
event set gives equal state in any arrival order, from genesis or from a
snapshot. An event minted because its writer saw another always sorts after
it, so a decision made in view of the state it changes is the one that
stands. Between genuinely concurrent events the order is deterministic
arbitration: lit promises convergence there, not recency.
Serves: Data.
Spec: `specifications/fold.md`; the order's inputs are in
`specifications/events-and-store.md`.

**Events carry intent, not derived state.** A write records what the writer
meant in terms that stay meaningful whatever happened concurrently: place
this lane above that one, replace the values I saw with this one. The verb
layer compiles each write into such intents.
Serves: Data.
Spec: `specifications/events-and-store.md`, "Intent".

**Wall time is data, never order.** Every event carries its writer's own
clock reading. Durations read it; which event superseded which never does.
When a question asks *when*, its answer is the largest recorded time among
the events that qualify. A writer's clock running ahead or behind is
surfaced as an advisory rather than trusted or hidden.
Serves: Data.
Spec: `specifications/fold.md`.

**Time enters at the edges.** Expiry, staleness, `held`, stage, rollup and
computed labels are evaluate's, computed on every read against `now`, so they
settle as facts change and the fold's output stays cacheable. A read writes no
event.
Serves: Data, Performance.
Spec: `specifications/fold.md`.

**Contradictions are values.** The one class of contradiction only concurrent
offline writers produce, two individually legal writes that are jointly
contradictory, is settled by a deterministic rule for that structure and held
as a value on the thing it concerns, with an advisory. Concurrent rewrites of
prose are not settled by any rule: both texts are kept and addressable, and
the resolution is another write from whoever judges it. `/integrity` is the
one list of what the fold could not settle silently.
Serves: Data.
Spec: `specifications/fold.md`.

**The fold is total.** Every event in the readable set is applied or
explicitly surfaced. An event a reader cannot interpret is carried byte for
byte and counted, never dropped or guessed at, and mutations that would race
it are refused; failure to read is never treated as absence. A workspace can
gate writers too old to read what its store holds.
Serves: Data.
Spec: `specifications/events-and-store.md`; the gate is in
`specifications/workspace-file.md`.

**Writes are judged at the write.** The requirement gate and the capability
rule judge a write against the writer's own current fold and the workspace
version it ran under, and the event names that version. The fold applies
every verified event as written, so nothing a writer did legally is undone by
a rule that arrived later.
Serves: Data.
Spec: `specifications/columns-and-requirements.md`, `specifications/access.md`.

**Determinism has no exceptions.** Nothing but the events enters the fold,
and a change to any fold rule is a new fold version, so no machine mixes two
folds' outputs.
Serves: Data.
Spec: `specifications/fold.md`.
