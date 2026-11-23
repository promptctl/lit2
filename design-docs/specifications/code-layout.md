# Code layout

Carries out `strategy/code-structure.md`.

```
CGO_ENABLED=0 go build ./cmd/lit2     # writes ./lit2
go test ./...
go tool golangci-lint run
```

## Module and tooling

- Module path `github.com/promptctl/lit2`; `go 1.26`.
- Go, with go-git (Apache-2.0) in process for lit's objects and refs and for
  reading the code repository's HEAD and history, never writing it. go-git is
  used through its storage and object packages, so none of its transport
  packages is linked. The first budget run measures go-git's object walk on
  the depth corpus.
- Tests are the standard `testing` package.
- Lint is golangci-lint v2 as a `tool` directive: `default: standard` plus
  `exhaustive` and `gochecksumtype`, and gofmt. Sealed interfaces marked
  `//sumtype:decl` and typed-integer enums make a missing case in a switch
  over them a lint failure.

## Units, downhill

`cmd/lit2` is the program and imports only `root`. Every other unit is a
directory under `internal/`, every directory under `internal/` is a unit in
this table, and a unit's subdirectories belong to it; `testdata` and names
starting with `.` or `_`, which the go tool passes by, are no part of the
tree. A unit imports only units below it in this order
`[LAW:one-way-deps]`; Go refuses cycles, not an upward edge that closes
none. `root` and `command` sit beside the stack, importing from it with
nothing in it importing them; `scenarios` is test only.

| unit | what it is |
|---|---|
| `scenarios` | test only: acceptance scenarios, each walked over root's twin |
| `root` | the composition root and the dispatcher every invocation passes through |
| `command` | the command event and the command log |
| `cli` | argv → typed command; rendering; exit statuses; `--json` |
| `guidance` | moments → resolved handlers |
| `verbs` | `get`/`set`/`add`/`rm` on typed addresses; the requirement gate; verb → intents; the echo |
| `address` | path grammar; selectors; globs |
| `evaluate` | derivations under the clock or the configuration: `held`, claim expiry, staleness, stage, rollup |
| `config` | the active workspace version → one typed schema value; the field table |
| `field` | field kinds and their value parsers; the built-in fields; the atom; the literal grammar |
| `fold` | events → state; the capability rule; rank; holders; conflicts |
| `events` | record, codec, upcasters, ids, signature, envelope |
| `store` | the append-only log; sealing into segments; one commit and one ref per writer, inbox refs; rewrites; the snapshot; the background sync process |
| `git` | go-git, in process, for objects and refs; the user's `git` binary for push and fetch, run only by the background sync process |
| `reason` | the exit classes; each layer's failure names its own reasons |
| `assets` | every shipped file, the default profile among them |

`config` sits above `fold` and `field` below `config`: config and address
both stand on field's kinds and atoms, so neither imports the other for them.

Every file under `cmd/` and `internal/`, tests included, also keeps these:

- **`os.Exit` only in `cmd/lit2`.**
- **Embedded files only in `internal/assets`.** A `//go:embed` anywhere else
  would ship a file the assets unit does not name `[LAW:one-source-of-truth]`.
- **No network.** No file imports `net`, a `net/` package or `crypto/tls`;
  the user's `git` does all transport (`specifications/events-and-store.md`,
  "Sync").
- **Only `events`, `root`, `verbs` and `cli` open an envelope.** Only they
  name `events.Open`, `events.Keyring` or `events.NewKeyring`, or dot-import
  `events` (`INV:opaque-envelope`): events defines them, root reads the keys,
  verbs seal and open prose, and cli renders it.

## Dependencies

Each module outside the standard library is imported by one unit. A unit
that takes a new module names it here first.

| module | unit |
|---|---|
| `github.com/go-git/go-git/v5` | `git` |
| `github.com/go-git/go-billy/v5` | `git` |
| `filippo.io/edwards25519` | `events` |
| `github.com/BurntSushi/toml` | `config` |
| `golang.org/x/mod` | `config` |

## The composition root

`root` is the one place a store is opened and the clock, the writer
identity, the clone's signing key, the encryption key with the wrapped tier
keys, and the agent label are read; every other layer is handed them.
Outside `root`, no file but a test reads the process's world: the clock and
its timers (`time.Now`, `Since`, `Until`, `After`, `Tick`, `Sleep`,
`NewTimer`, `NewTicker`, `AfterFunc`, `Local`); the environment, host,
process and user ids, user and temporary directories and the executable
(`os`); the standard streams; `os.Args`, which `cmd/lit2` alone may also
read; entropy (`crypto/rand`); the standard logger (`log`, and `fmt`'s
`Print`, `Printf` and `Println`); `os/user`; and the shared source behind
`math/rand`'s and `math/rand/v2`'s top-level functions. A dot import of one
of these packages counts as a read.

- **`now`.** The root reads the clock once at the start and hands that
  instant down as `now`; a second reading is only the command's duration.
- **Entropy** is an `io.Reader` in the process: `crypto/rand.Reader`, and
  seeded ChaCha8 in the twin.
- **Agent label** is `LIT_AGENT`: empty is no label, anything else is the
  label, opaque.
- **Rows with no body yet** parse, appear in help, and end internal (70)
  naming the ticket that builds them, from one unexported function in `root`
  that the lint forces out once the last body is bound.

### On disk

| scope | path | holds |
|---|---|---|
| per clone | `<git-common-dir>/lit/store/` | the store's root |
| per clone | `<git-common-dir>/lit/`, beside `store/` | the key material (`specifications/access.md`, "Identity") |
| per checkout | `<git-dir>/lit/checkout/id` | the checkout id (`specifications/events-and-store.md`, "Log, segments and refs") |
| per work tree | `.lit/workspace.toml` | the workspace file, committed with the code |
| per machine | `$XDG_STATE_HOME/lit/commands/<UTC date>.jsonl` (`~/.local/state` when unset) | the command log: one line per invocation in one `O_APPEND` write; every write removes day files older than 30 days |

## Concerns

Which unit owns each concern. Every primitive and every step of "One
command, start to finish" has a row, every unit has a row, and every row
names only units in "Units, downhill".

| concern | unit | what of it lives there |
|---|---|---|
| node and path | `address`, `config`, `field` | the path grammar, selectors and globs in address; the field table in config, the one answer to what a path names at a place; the atom selectors share and the literal grammar in field |
| event and intent | `events`, `verbs` | the event record and ids in events; the verb-to-intent table in verbs |
| fold and evaluate | `fold`, `evaluate` | fold(events) → state; evaluate(state, config, now) → view |
| work item, level, tree | `config`, `fold`, `evaluate` | levels and homes declared in config; the tree folded; stage and rollup in evaluate |
| column and stage | `config`, `evaluate` | the column graph and its check in config's parse; stage and the undeclared column in evaluate |
| requirement | `verbs`, `field`, `config`, `evaluate` | evaluated by the requirement gate in verbs; the test is field's atom; checks expanded in config; `held` in evaluate |
| lane | `fold` | membership, order and the one rank order |
| claim | `fold`, `evaluate` | the holder set folded; expiry in evaluate |
| checklist | `fold`, `verbs` | items folded; the `set` list diff in verbs |
| link | `fold`, `config` | links folded; link types declared in config |
| label | `config`, `fold`, `evaluate` | declared in config in the top-level `labels` list; applied as events and folded; computed from a `where` selector in evaluate |
| moment and handler | `guidance` | moments, handlers, workflows and their layers |
| rewrite | `fold`, `store` | applied by the fold; segments re-sealed by store |
| identity | `root`, `events` | the checkout id, principal and agent label read in root; attribution carried in events |
| the workspace file | `config`, `fold`, `evaluate`, `verbs`, `root`, `assets` | the one schema parse of an applied version in config; the applies and which version is in force folded; the column-graph check against every live version in evaluate; the one judge of an apply, `Admit`, and the board's `workspace` line in verbs; `config commit`, `config status` and init's version one in root; the default profile in assets |
| log, segment and ref | `store`, `git` | the log, segments, writer refs and inbox refs in store; objects and refs through git |
| principal and policy | `events`, `fold` | the key, signature and suite in events; the policy as events folded |
| capability | `verbs`, `fold` | the capability rule in fold's step, over roles from the folded policy; the default roles, membership paths compiled to proposals and a refusal's rendering in verbs |
| tier and envelope | `events`, `verbs` | the envelope and suite in events; prose sealed in verbs, so no layer below sees it |
| verification | `fold`, `store` | one fold rule, signature then policy, in fold's step via the suite in events; run by store at receive |
| Open | `store` | ref heads, every open log under the common dir and the snapshot read |
| Fold | `fold` | events newer than the snapshot, verified |
| Parse | `config` | the active workspace version the fold names, into the schema value |
| Address | `address` | paths into typed addresses; selectors and globs expanded once |
| Evaluate | `verbs`, `evaluate` | the description of a write; the answer of a get |
| Perform | `verbs`, `store` | the intents signed in verbs' perform step; appended to the log by store |
| Speak | `cli`, `guidance` | the echo and its rendering; the handlers that fire |
| Record | `command` | the command event and the command log |
| Sync | `store`, `git` | the background sync process in store; push and fetch through the user's git |
| the order of the steps | `root` | the dispatcher every invocation passes through |
| exit classes and statuses | `reason`, `cli` | the four classes in reason; each layer's failure names its own reasons; statuses in cli |
| checkout discovery | `git` | the work tree, private git dir and common dir |
| the code repository's HEAD and history | `git` | the blob HEAD holds at a path and whether one was ever there, read, never written |
| the requirement gate, the capability rule and the in-use check | `verbs`, `fold` | requirements judged by verbs' gate, at the write; capabilities by fold's step, which every replica runs and a write runs over its own events before appending |
| the snapshot | `store`, `fold` | its bytes in store; its content and key in fold |
| `/next`, `/lanes`, `/integrity`, `/me` | `fold`, `evaluate`, `address`, `cli` | computed by fold and evaluate, named by address, rendered by cli |
| the managed block in `AGENTS.md` | `guidance`, `root` | its text in guidance; written by init in root |
| liveness of a worktree | `git`, `evaluate` | read by git; decided by evaluate |
| budgets | `fold`, `store` | benchmarks beside the code they measure |
| field kinds, value parsers, built-in fields, the atom | `field` | the field language config and address both read |
| `@` references in prose | `verbs`, `cli` | normalized to `@<segments>` in verbs before the envelope seals; rendered as `@` plus the current name by cli at Speak |
| acceptance scenarios | `scenarios` | each scenario walked over root's twin |

## One command, start to finish

Every verb runs this pipeline on every call; the verb and the paths are
values `[LAW:dataflow-not-control-flow]`. Fold through Evaluate are pure
functions of (events, clock, entropy, argv, writer); the rest is the edge
`[LAW:effects-at-boundaries]`.

1. **Open.** Read every writer ref head and every open log under the common
   dir, this checkout's and its sibling worktrees'; load the snapshot keyed
   by `(fold-version, ref heads, log lengths)`. A miss or a torn snapshot is
   one refold; no lock.
2. **Fold** the events newer than the snapshot, from the sealed segments and
   the open log, verifying each.
3. **Parse** the active workspace version the fold names into the schema
   value. A store with no applied version refuses every verb, naming
   `lit2 config commit`; nothing stands in for it.
4. **Address.** Parse each path into a typed address; expand selectors and
   globs once.
5. **Evaluate.** `get` computes its answer with evaluate, handed `now`. A
   write computes a description: the intents it mints, signed, then the
   requirement and capability verdicts judged against the state those
   intents fold onto, so a path the same command writes counts, the
   derivations, the echo. Refusal of any path refuses the whole.
6. **Perform.** Append the intents to this checkout's log, or decline
   because `--dry-run` was given. Update the snapshot.
7. **Speak.** Print the echo and run the handlers for the moments raised;
   under `--json`, one object keyed by path.
8. **Record** the command event: paths, expansion, verdicts, events,
   handlers fired and suppressed, cache hit or refold, timings, `dry_run`,
   `written`. One record per invocation, in the command log, local, never
   synced `[LAW:nothing-unseen]`.
9. **Sync.** A write schedules a detached `lit2 admin sync` to seal this
   checkout's log and publish its commit, and to fetch and verify the
   others'; a read starts the same when the last fetch is older than the
   debounce.

## Testing

How lit is tested is `strategy/code-structure.md`
`[LAW:behavior-not-structure]`. Concretely:

- Determinism is tested as `specifications/fold.md`, "The contract" says.
- The acceptance scenarios are `internal/scenarios`.
- The budgets in `specifications/budgets.md` run in the harness.
- No test reads, parses or depends on a Markdown file.
- Tests use at most five levels of hierarchy.

## Build order

The order is slices (`strategy/code-structure.md`). Within a slice the
highest-leverage story is built to full depth before anything that depends
on it.

**Slice 0, the frame of the house.** The units above, the composition root,
the command event, the harness, `lit2 --help` from the field table.

**Slice 1, the walking skeleton.** Every activity (arrive, file, look, pick,
work, finish, share, admit, tend) at a depth the next slice can build on,
and the design's three risks settled.

- Built to full depth, because later slices stand on them: the log and its
  sealing into segments; the event record and codec with its signature; the
  principal and the policy as events; authorization as a fold rule and at
  receive; the workspace file's mechanics (the applied version, its bytes
  carried in the log, the default profile applied as version one at init,
  each event naming the version its writer ran under); the fold and its
  total order; the path grammar and field table; the verb-to-intent table.
- Built at working minimum: the default roles with the capability rule
  judging them, under the default profile's requirement lists; the default
  tier in plaintext mode with the envelope type in place; `stage` and
  `rollup` in evaluate; `/next` as the head of the top-ranked lane;
  `lit2 admin sync` run by hand.

The slice is done when this walk narrates in two clones of one remote:

```
A: lit2 admin init                          → principal, phrase, enrollment
A: lit2 --help; lit2
A: lit2 add unfiled/children '{title: "…", labels: [ready]}'
A: lit2 add <feature>/children '{title: "…", description: "…"}'  → an id, signed
A: lit2 set <id>/status impl                 → refused: impl is entered only from plan
A: lit2 set <id>/status plan                 → a transition along next, backlog/exit met
A: lit2 admin sync
B: lit2 admin init                          → joins; membership request pushed
A: lit2 admin sync; lit2 add /members '{principal: <B>, role: contributor}'
A: lit2 admin sync
B: lit2 admin sync; lit2 get /next           → the same ticket, in plan
B: lit2 set <id>/status impl                 → refused: impl/entry plan/verdict=pass, no review yet
B: lit2 set <id>/status '{column:obsolete reason:"…"}'; lit2 admin sync
B: lit2 rm <epic>                           → refused: a node; contributor has rm own/* at level 1
A: lit2 admin sync; lit2 get <id>/stage      → terminal; A and B fold equal
```

The walk runs under the default profile's lists, so it reaches `done` only
once slice 3's reviewables give `plan/verdict` and `pr/verdict` a value;
until then a ticket closes through a terminal column no `next` names.

**Slice 2, visibility.** The default tier encrypted; content keys and epoch
keys wrapped to members; `tier` on items and comments; `set x/tier`; grant
on admit, rotation on removal; `rm x --destroy`; succession; `get /me`;
redaction in every view.

**Slice 3, process.** Requirements and `held`; levels with their fields and
templates; the checklist and its terminal requirement; links; per-epic
sections and the nearest-ancestor rule; versioned text nodes, the
reviewables with their revisions and reviews, terminal columns with
reasons; history and undo.

**Slice 4, commitment.** Lanes on nodes and the one rank order; rank
intents; claims, expiry and release; `/lanes`, `/next` in full, the urgent
interrupt; blockers from links and lane position.

**Slice 5, guidance.** Workflows, handlers and their moments; the handler
layers; column guidance; orientation on `read /`; `--dry-run` showing what
handlers would do.

**Slice 6, sharing.** The detached sync process and its debounce; inbox
refs and filing without a clone; staleness and the last push outcome as
advisories; re-publishing over a vandalized remote; `/integrity`.

**Slice 7, tending.** Rewrites and the receiver's rule; `gc`; unknown
events and `min-reader-version`; the fold-version bump; the budgets harness
and the depth corpus.

**Slice 8, the surface in full.** Selectors with every atom and `parent/`;
globs; list diffs on `set`; `--json` keyed by path.

**End game**, mostly composed from stories that will already exist:
`orphaned` as a label with a `where` selector in the default profile; a
handler on `wrote */status` that files a follow-up; a CI principal with
write verbs and no tier keys; `admin export` and `admin import` of the whole
store as one versioned document.

### lit2's own backlog

The backlog holds the slices as features, tickets and chores, and features
and tickets each finish as something that can be used.

- **A feature** is usually an epic. Its title says what a person or an
  agent can do once its tickets are closed. Features are ranked in slice
  order, and a slice that holds several things a person would name
  separately is several features.
- **A ticket** is usually one usable part of a feature. Its title says what
  works when it closes. A part nothing can use alone, such as a path
  grammar with no verb that takes a path, is built in the ticket that first
  uses it.
- **A chore** is maintenance or enabling work, such as a rename or a moved
  file. It usually sits in the feature it serves, directly above the ticket
  that needs it.
