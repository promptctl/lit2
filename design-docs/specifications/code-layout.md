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
  packages is linked.
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
tree. A unit imports only units below it in this order `[LAW:one-way-deps]`.
`root` and `command` sit beside the stack, importing from it with nothing in
it importing them; `scenarios` is test only.

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

Each module outside the standard library is imported by one unit.

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

### On disk

| scope | path | holds |
|---|---|---|
| per clone | `<git-common-dir>/lit/store/` | the store's root |
| per clone | `<git-common-dir>/lit/`, beside `store/` | the key material (`specifications/access.md`, "Identity") |
| per checkout | `<git-dir>/lit/checkout/id` | the checkout id (`specifications/events-and-store.md`, "Log, segments and refs") |
| per work tree | `.lit/workspace.toml` | the workspace file, committed with the code |
| per machine | `$XDG_STATE_HOME/lit/commands/<UTC date>.jsonl` (`~/.local/state` when unset) | the command log: one line per invocation in one `O_APPEND` write; every write removes day files older than 30 days |

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
