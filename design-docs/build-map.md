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
  as verbs on paths that compile to signed intents, a pure fold that
  verifies, a read-time tick, a policy in the store and a committed
  workspace file; no second mechanism beside the substrate.
- **What it composes with:** git as it already is (the common dir, refs,
  the object database, remotes, worktrees); the shell and `--json` for
  agents; a TOML file a person edits and reviews in a pull request.
- **Where the leverage is:** the event record and the fold, then the
  path grammar and the field table. Once those are the right shape, every
  later capability is a fold rule, a tick rule, a capability in the
  policy, or a key in the workspace file, and no verb learns a new case.

## Backbone

What an agent does with lit2, in the order it happens:

**arrive → file → look → pick → work → finish → share → admit → tend**

Open the checkout and learn where things stand; file work and groom it;
read any path; choose what to do now and commit to it; move the work
through the process and be told what the process wants; close it; let
every other replica see it; admit people and decide what each may do and
read; keep the store small, current and honest.

## Depth, by activity

Under each activity, the ways it could be built, highest leverage at the
top. The marked depth is the one we build. Nothing below the mark is built,
ever, because each of those makes the things around it expensive to replace.

**arrive**
- ▶ composition root reads the store, clock, identity and keys once;
  `lit2 --help` and the parser are generated from one field table; bare
  `lit2` is `get /` and an orientation handler speaks after it; init mints
  the principal and enrolls or joins
- a hand-written help text and a `status` subcommand

**file**
- ▶ four verbs on typed addresses, each write compiled to signed intents by
  one table; ids random, carried in the create event, their segment fixed at mint; templates and defaults
  as derivations the echo reports; bulk as a list value
- a `new` subcommand with one flag per field
- a `create` that stores a record as given

**look**
- ▶ every view is a path over `tick(fold(events), now)`: `/`, `x`,
  `x/field`, `x/history`, `/lanes`, `/next`, `/members`, `/me`; one
  renderer that redacts what the reader cannot open; `--json` keyed by
  path; selectors and globs expand once
- `show`, `ls`, `backlog`, `history` as separate commands with their own
  output shapes

**pick**
- ▶ lanes on any node with members from anywhere; one rank order over
  lanes with relative intents (`above`, `below`, `top`, `bottom`); the
  claim derived from the start transition; `/next` as one walk of the lane
  order
- a rank on every item, resolved per container, with the container doubling
  as the lane
- a stored `assignee` and a stored claim record, reconciled by hand

**work**
- ▶ columns in the workspace file; one enforcer on the write reading
  requirement lists and the writer's capabilities; `stage`, `rollup`,
  `held` in tick; labels declared in the workspace file, applied by people or
  computed by tick from a `where` selector; `plan` and `pr` as reviewables, their
  revisions and reviews as lists on paths; the checklist on the leaf, its
  final state kept
- a status enum in code with a switch per transition; permission checks
  scattered per command

**finish**
- ▶ terminal columns with their own entry requirements; a container's stage
  derived from its children; nothing stored on close but the transition
- a `closed` flag and a `close` command

**share**
- ▶ a local append-only log per writing checkout, sealed into a few
  segment blobs and published as one commit on a ref only that writer
  replaces; every receiver verifies a publication before folding it; a
  detached background process pushes and fetches; inbox refs for storeless
  writers; convergence is the fold's determinism, tested as agreement
- one commit per command on a chained ref (grows with every mutation ever)
- a merged state file on one branch

**admit**
- ▶ a principal key per checkout; the policy as events in the store with a
  monotone version and an owner threshold; capabilities as `(verb, path
  glob, binding)`; roles as bundles in the default profile; tiers with
  epoch keys wrapped to members; authorization as one fold rule; the
  Monero model throughout, every verifier enforcing and no trusted party
- a members file with usernames and a flag per permission; a server that
  checks

**tend**
- ▶ rewrites as events (purge, squash, sweep, destroy); the snapshot keyed
  by `(fold-version, ref heads, log lengths)`; unknown and unverified events carried and
  counted; `min-reader-version` in the workspace file; `/integrity` as a path
- a `gc` that edits history in place; configuration read from the code's
  own repository

## Scenarios as stories

Each scenario in `scenarios.md` is a set of stories above, and the slice
it needs.

| scenario | stories it composes | slice |
|---|---|---|
| 1 first contact | arrive, file, look at marked depth; guidance for the orientation text | 1, 5 |
| 2 filing and grooming | file, look; requirements for "ready"; levels and templates | 1, 3 |
| 3 start to done | work, finish; claims; the reviewable's revisions and reviews; column guidance | 3, 4, 5 |
| 4 plan changes after pass | the reviewable: a new revision empties the derived verdict until a review names it | 3 |
| 5 two worktrees | one store under the common dir; per-checkout refs and claims | 1, 4 |
| 6 two machines offline | share; conflicts as values; staleness; `/integrity` | 1, 6 |
| 7 process changes under running work | `config-ref` as provenance; the enforcer at the write; undeclared-column rule | 1, 3 |
| 8 epic with its own process | per-epic sections; nearest ancestor per column and handler field | 3, 5 |
| 9 grooming at scale | selectors in full; `--dry-run` over an expansion | 8 |
| 10 purge while others write | rewrites; the receiver's rule | 7 |
| 11 dead worktree holding a lane | claim expiry in tick; `rm <lane>/claim` | 4 |
| 12 old binary meets newer events | unknown events preserved; `min-reader-version` | 1 (codec), 7 |
| 13 scale | snapshot; budgets harness; the depth corpus | 1, 7 |
| 14 a human at the terminal | rendering; the field table as help | 1 |
| 15 access control | admit at marked depth; verification in share; capabilities in work's enforcer; tiers in look and file | 1, 2 |
| 16 fold-version bump | snapshot keyed by fold-version | 1, 7 |

## Slices

Each slice is the whole backbone, thinner at the top and thicker toward
the bottom. Within a slice the highest-leverage story is built to full
depth before anything that depends on it.

**Slice 0, the frame of the house.** The layout of `architecture.md` §6,
the composition root, the command event, the harness, `lit2 --help` from
the field table.

**Slice 1, the walking skeleton.** Every activity at a depth the next slice
can build on, and the three risks of the design settled: that one event set
folds to one state on every machine, that one commit per writer on a plain
remote carries it there without the remote growing with commands, and that
every event is signed and verified with no server. Built to full depth here
because everything stands on them: the log and its sealing into segments,
the event record and codec with its signature, the principal and the policy
as events, authorization as a fold rule and at receive, the workspace
file's mechanics (the active version read by parse, the shipped default profile
applied as version one at init, `config-ref` as the blob id of that version), the fold and its total order, the path grammar and field table, the
verb-to-intent table. Built at working minimum: the default roles with the
enforcer judging capabilities, under the default profile's requirement lists, the default
tier in plaintext mode with the envelope type in place, `stage` and
`rollup` in tick, `/next` as the head of the top-ranked lane,
`lit2 admin sync` run by hand. The slice is done when the walk below
narrates in two clones of one remote:

```
A: lit2 admin init                          → principal, phrase, enrollment
A: lit2 --help; lit2
A: lit2 add unfiled/children '{title: "…"}'   → an id, signed
A: lit2 set <id>/status impl                 → a transition along next
A: lit2 admin sync
B: lit2 admin init                          → joins; membership request pushed
A: lit2 admin sync; lit2 add /members '{principal: <B>, role: contributor}'
A: lit2 admin sync
B: lit2 admin sync; lit2 get /next           → the same ticket, in impl
B: lit2 set <id>/status done; lit2 admin sync
B: lit2 rm <epic>                           → refused: a node; contributor has rm own/* at level 1
A: lit2 admin sync; lit2 get <id>/stage      → terminal; A and B fold equal
```

**Slice 2, visibility.** The default tier encrypted; content keys and
epoch keys wrapped to members; `tier` on items and comments; `set x/tier`;
grant on admit, rotation on removal; `rm x --destroy`; succession;
`get /me`; redaction in every view. Scenario 15.

**Slice 3, process.** Requirements and `held`; levels with their fields
and templates; the checklist and its terminal requirement; links; per-epic sections and the nearest-ancestor
rule; versioned text nodes, the reviewables with their revisions and
reviews, terminal columns with reasons; history and undo. Scenarios 2, 3,
4, 7, 8.

**Slice 4, commitment.** Lanes on nodes and the one rank order; rank
intents; claims, expiry and release; `/lanes`, `/next` in full, the urgent interrupt; blockers from
links and lane position. Scenarios 3, 5, 11.

**Slice 5, guidance.** Workflows, handlers and their triggers; the three
layers; column guidance after the data; orientation on `read /`;
`--dry-run` printing what would be said. Scenarios 1, 3, 8, 14.

**Slice 6, sharing.** The detached sync process and its debounce; inbox
refs and filing without a clone; staleness and the last push outcome as
advisories; re-publishing over a vandalized remote; `/integrity`.
Scenario 6.

**Slice 7, tending.** Rewrites and the receiver's rule; `gc`; unknown
events and `min-reader-version`; the fold-version bump; the budgets harness
and the depth corpus. Scenarios 10, 12, 13, 16.

**Slice 8, the surface in full.** Selectors with every atom and
`parent/`; globs; list diffs on `set`; `--json` keyed by path. Scenario 9.

End game, from stories that will already exist and nothing new: `orphaned`
as a label with a `where` selector in the default profile; a handler on
`wrote */status` that files a follow-up; a CI principal with write verbs and
no tier keys; `admin export` and `admin import` of the whole store as one
versioned document.

## The backlog

The tracker holds this map at three sizes, and each one finishes as
something that can be used.

- **The product** is the features, in order.
- **A feature** is an epic. Its title says what a person or an agent can
  do once its tickets are closed. Features run one after another in slice
  order, and a slice that holds several things a person would name
  separately is several features.
- **A ticket** is one usable part of a feature. Its title says what works
  when it closes. A part nothing can use alone, such as a path grammar
  with no verb that takes a path, is built in the ticket that first uses
  it.
- **A chore** fills a gap between two tickets: a rename, a moved file. It
  sits in the feature it serves, directly above the ticket that needs it.

A feature's tickets share one lane, so they run as a series in rank
order.
