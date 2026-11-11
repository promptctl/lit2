# lit2: the build map

The map the backlog is a view of. `architecture.md` says what lit2 is;
this says in what order it comes to exist. Every ticket in the tracker is
one story on this map, and a ticket that fits nowhere on it is a sign the
map is missing an activity.

## Frame

- **Who uses it:** an agent, or a person, working inside a git checkout;
  and a second lit2 on another machine or worktree that must see the same
  state.
- **What it can do afterward:** everything in `architecture.md` §4, built
  as verbs on paths that compile to intents, a pure fold, a read-time tick,
  and a committed workspace file; no second mechanism beside the substrate.
- **What it composes with:** git as it already is (the common dir, refs,
  the object database, remotes, worktrees); the shell and `--json` for
  agents; a TOML file a person edits and reviews in a pull request.
- **Where the leverage is:** the event record and the fold, then the
  path grammar and the field table. Once those are the right shape, every
  later capability is a fold rule, a tick rule, or a key in the workspace
  file, and no verb learns a new case.

## Backbone

What an agent does with lit2, in the order it happens:

**arrive → file → look → pick → work → finish → share → tend**

Open the checkout and learn where things stand; file work and groom it;
read any path; choose what to do now and commit to it; move the work
through the process and be told what the process wants; close it; let
every other replica see it; keep the store small, current and honest.

## Depth, by activity

Under each activity, the ways it could be built, highest leverage at the
top. The marked depth is the one we build. Nothing below the mark is built,
ever, because each of those makes the things around it expensive to replace.

**arrive**
- ▶ composition root reads the store, clock and identity once; `lit2 --help`
  and the parser are generated from one field table; bare `lit2` is `get /`
  and an orientation handler speaks after it
- a hand-written help text and a `status` subcommand

**file**
- ▶ four verbs on typed addresses, each write compiled to intents by one
  table; ids from `(writer, causal-position)`; templates and defaults as
  derivations the echo reports; bulk as a list value
- a `new` subcommand with one flag per field
- a `create` that stores a record as given

**look**
- ▶ every view is a path over `tick(fold(events), now)`: `/`, `x`,
  `x/field`, `x/history`, `/lanes`, `/next`; one renderer; `--json` keyed by
  path; selectors and globs expand once
- `show`, `ls`, `backlog`, `history` as separate commands with their own
  output shapes

**pick**
- ▶ frames with rank intents (`above`, `below`, `top`, `bottom`); lanes as
  ordered lists in the epic; the claim derived from the start transition;
  `/next` as one walk of the frames
- a stored `assignee` and a stored claim record, reconciled by hand

**work**
- ▶ columns in the workspace file; one enforcer on the transition write
  reading requirement lists; `stage`, `rollup`, `held` and rule labels in
  tick; verdicts as children on paths
- a status enum in code with a switch per transition

**finish**
- ▶ terminal columns with their own entry requirements; a container's stage
  derived from its children; nothing stored on close but the transition
- a `closed` flag and a `close` command

**share**
- ▶ one fast-forward ref per writing checkout; a detached background
  process pushes and fetches; inbox refs for storeless writers; convergence
  is the fold's determinism, tested as agreement
- a merged state file on one branch

**tend**
- ▶ rewrites as events (purge, squash, sweep); the snapshot keyed by
  `(fold-version, heads)`; unknown events carried and counted;
  `min-reader-version` in the workspace file; the workspace file's blob in
  every event commit; `/integrity` as a path
- a `gc` that edits history in place; configuration read from the code's
  own repository

## Features as stories

Each scenario in `scenarios.md` is a feature; each is a set of stories
above, and the slice it needs.

| scenario | stories it composes | slice |
|---|---|---|
| 1 first contact | arrive, file, look at marked depth; guidance for the orientation text | 1, 4 |
| 2 filing and grooming | file, look; requirements for "ready"; levels and templates | 1, 2 |
| 3 start to done | work, finish; claims; verdicts; column guidance | 2, 3, 4 |
| 4 plan changes after pass | versioned text nodes; verdicts citing a digest | 2 |
| 5 two worktrees | one store under the common dir; per-checkout refs and claims | 1, 3 |
| 6 two machines offline | share; conflicts as values; staleness; `/integrity` | 1, 5 |
| 7 process changes under running work | the workspace file cited by blob; undeclared-column rule | 1, 2 |
| 8 epic with its own process | per-epic sections; nearest ancestor per column and handler field | 2, 4 |
| 9 grooming at scale | selectors in full; `--dry-run` over an expansion | 7 |
| 10 purge while others write | rewrites; the receiver's rule | 6 |
| 11 dead worktree holding a lane | claim expiry in tick; `rm <lane>/claim` | 3 |
| 12 old binary meets newer events | unknown events preserved; `min-reader-version` | 1 (codec), 6 |
| 13 scale | snapshot; budgets harness; the depth corpus | 1, 6 |
| 14 a human at the terminal | rendering; the field table as help | 1 |
| 15 access control later | signature slot and envelopes present from the first event | 1 (slots), later |
| 16 fold-version bump | snapshot keyed by fold-version | 1, 6 |

## Slices

Each slice is the whole backbone, thinner at the top and thicker toward
the bottom. Within a slice the highest-leverage story is built to full
depth before anything that depends on it.

**Slice 0, the frame of the house.** The layout of `architecture.md` §6,
the composition root, the command event, the harness, `lit2 --help` from
the field table. One ticket, `lit-bootstrap-1f9.438`.

**Slice 1, the walking skeleton.** Every activity at a depth the next slice
can build on, and the two risks of the design settled: that one event set
folds to one state on every machine, and that per-writer refs on a plain
remote carry it there. Built to full depth here because everything stands
on them: the store, the event record and codec, the workspace file's
mechanics (parse, cite by blob, carry the blob), the fold and its total
order, the path grammar and field table, the verb-to-intent table. Built at
working minimum: columns from the default profile with empty requirement
lists, `stage` and `rollup` in tick, `/next` as the first non-terminal
ticket in frame order, `lit2 admin sync` run by hand. The slice is done
when the walk below narrates in two clones of one remote:

```
A: lit2 admin init; lit2 --help; lit2
A: lit2 add unfiled/children '{title: "…"}'   → an id
A: lit2 set <id>/status impl                 → a transition along next
A: lit2 admin sync
B: lit2 admin sync; lit2 get /next           → the same ticket, in impl
B: lit2 set <id>/status done; lit2 admin sync
A: lit2 admin sync; lit2 get <id>/stage      → terminal; A and B fold equal
```

**Slice 2, process.** Requirements and `held`; levels with their fields
and templates; links and rules; per-epic sections and the nearest-ancestor
rule; versioned text nodes, verdicts, terminal columns with reasons;
history and undo. Scenarios 2, 3, 4, 7, 8.

**Slice 3, commitment.** Frames and rank intents; lanes; claims, expiry
and release; `/lanes`, `/next` in full, the urgent interrupt; blockers from
links and lane position. Scenarios 3, 5, 11.

**Slice 4, guidance.** Workflows, handlers and their triggers; the three
layers; column guidance after the data; orientation on `read /`;
`--dry-run` printing what would be said. Scenarios 1, 3, 8, 14.

**Slice 5, sharing.** The detached sync process and its debounce; inbox
refs and filing without a clone; staleness and the last push outcome as
advisories; `/integrity`. Scenario 6.

**Slice 6, tending.** Rewrites and the receiver's rule; `gc`; unknown
events and `min-reader-version`; the fold-version bump; the budgets harness
and the depth corpus. Scenarios 10, 12, 13, 16.

**Slice 7, the surface in full.** Selectors with every atom and
`parent/`; globs; list diffs on `set`; `--json` keyed by path. Scenario 9.

End game, from stories that will already exist and nothing new: a
`(stage=active updated<-6h)` view as a saved selector in the workspace
file; a handler on `wrote */status` that files a follow-up; access control
as capabilities over the same paths, once the slots are in use.
