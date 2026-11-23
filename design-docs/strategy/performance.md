# Performance

How lit keeps every command fast enough to call in a loop as history grows.

## Diagnosis

State is a fold over every event ever written, and a naive read refolds all
of them, so cost grows with history rather than with active work. Every
command also opens every writer's log and ref, and the store must be verified
in full the first time a clone adopts it, which is the most expensive thing
lit ever does. One machine holds many stores, each syncing in the background,
so background work compounds across them. And rank is the most contended
object in the system, reordered constantly by the whole fleet, which makes it
the hottest fold path.

## Guiding policy

Make the cost of a command track what changed since it last ran: read from a
disposable snapshot and fold only the newer events, and keep history bounded
by rewrites that compact it.

## Actions

**A disposable snapshot.** Reads come from a local materialized state that
remembers how far into each writer's events it has folded, so a command folds
only the delta. It is scaffolding, never structure: any process may delete it
at any moment and the only cost is one refold. Correctness never depends on it.
Serves: Performance, Data.
Spec: `specifications/events-and-store.md`, "Cache".

**A fold change is paid once per store.** The snapshot is keyed by the fold
version, so a release that changes a fold rule makes every snapshot miss
once; the first command in each store refolds from genesis and rewrites it.
Nothing runs at install time.
Serves: Performance.
Spec: `specifications/events-and-store.md`, "Cache".

**History stays bounded.** Compaction is a rewrite that squashes ranges
within windows that keep each item's state as it was judged at every move,
so cost can track active work rather than all history ever written. No
storage budget depends on compaction running.
Serves: Performance, Impact on git.
Spec: `specifications/events-and-store.md`, "Rewrite".

**Reads are walks of folded state.** The snapshot holds the folded lane
order, so a new rank intent applies to a known order, and `/next` is one walk
of the lane order.
Serves: Performance.
Spec: `specifications/fold.md`, `specifications/events-and-store.md`.

**Background work is debounced and bounded.** Sync runs in a detached process
a write schedules, and a read starts one only when the last fetch is older
than the debounce, so many stores on one machine do not stampede.
Serves: Performance.
Spec: `specifications/events-and-store.md`, "Sync".

**Budgets are measured on realistic corpora.** Uniform random events would
flatter the fold, so the harness runs against corpora shaped like real stores
at ten times their depth and breadth, and command latency is measured on a
machine running a full fleet's background sync, not on a quiet one.
Serves: Performance.
Spec: `specifications/budgets.md`, which holds the figures, their standing
as targets, the corpora and the harness contract.
