# lit2: where it lives

[design-docs/architecture.md](design-docs/architecture.md) is what lit2 is;
this file is where it lives: which unit owns each concern, which unit may
depend on which, and the decisions the design documents left to the layout.
`internal/architecture` holds every import in the tree to this file.

```
CGO_ENABLED=0 go build ./cmd/lit2     # writes ./lit2
go test ./...
go tool golangci-lint run
```

## Units, downhill

`cmd/lit2` is the program and imports only `root`. Every other unit is a
directory under `internal/`, and a unit's subdirectories belong to it. A unit
imports only units below it in this block `[LAW:one-way-deps]`; `root` and
`command` sit beside the stack of architecture.md §6, importing from it with
nothing in it importing them, and `architecture` and `scenarios` are test
only. The rows from `cli` to `git` are §6's drawing, in its order.

```layers
architecture
scenarios
root
command
cli
guidance
verbs
address
tick
config
field
fold
events
store
git
reason
assets
```

Each line of the `dependencies` block is a module outside the standard
library and the one unit that may import it. A unit that takes a module
names it here first.

```dependencies
github.com/go-git/go-git/v5 git
github.com/go-git/go-billy/v5 git
filippo.io/edwards25519 events
github.com/BurntSushi/toml config
golang.org/x/mod config
```

## Concerns

Every `### 3.N` primitive and every numbered §5 step of architecture.md has a
row; the architecture test fails on one without.

| concern | unit | what of it lives there |
|---|---|---|
| §3.1 Node and path | `address`, `field` | the path grammar, selectors, globs and the field table in address; the atom selectors share in field |
| §3.2 Event and intent | `events`, `verbs` | the event record and ids in events; the verb-to-intent table in verbs |
| §3.3 Fold and tick | `fold`, `tick` | fold(events) → state; tick(state, config, now) → view |
| §3.4 Work item, level, tree | `config`, `fold`, `tick` | levels and homes declared in config; the tree folded; stage and rollup in tick |
| §3.5 Column and stage | `config`, `tick` | the column graph and its check in config's parse; stage and the undeclared column in tick |
| §3.6 Requirement | `verbs`, `field`, `config`, `tick` | evaluated by the one enforcer in verbs; the test is field's atom; checks expanded in config; `held` in tick |
| §3.7 Lane | `fold` | membership, order and the one rank order |
| §3.8 Claim | `fold`, `tick` | the holder set folded; expiry in tick |
| §3.9 Checklist | `fold`, `verbs` | items folded; the `set` list diff in verbs |
| §3.10 Link | `fold`, `config` | links folded; link types declared in config |
| §3.11 Label | `config`, `fold` | declared in config's `labels` list; applied as events and folded; computed by nothing |
| §3.12 Moment and handler | `guidance` | moments, handlers, workflows, their three layers |
| §3.13 Rewrite | `fold`, `store` | applied by the fold; segments re-sealed by store |
| §3.14 Identity | `root`, `events` | the checkout id, principal and agent label read in root; attribution carried in events |
| §3.15 The workspace file | `config`, `assets` | the one schema parse of an applied version, and the default profile init applies as version one; the shipped files and their `Asset` handles in assets |
| §3.16 Log, segment and ref | `store`, `git` | the log, segments, writer refs and inbox refs in store; objects and refs through git |
| §3.17 Principal and policy | `events`, `fold` | the key, signature and suite in events; the policy as events folded |
| §3.18 Capability | `verbs`, `fold` | the capability verdict at the one enforcer; roles from the folded policy |
| §3.19 Tier and envelope | `events`, `verbs` | the envelope and suite in events; prose sealed in verbs, so no layer below sees it |
| §3.20 Verification | `fold`, `store` | one fold rule via the suite in events; run by store at receive |
| §5.1 Open | `store` | ref heads, every open log under the common dir and the snapshot read |
| §5.2 Parse | `config` | the workspace file into the schema value |
| §5.3 Fold | `fold` | events newer than the snapshot, verified |
| §5.4 Address | `address` | paths into typed addresses; selectors and globs expanded once |
| §5.5 Evaluate | `verbs`, `tick` | the description of a write; the answer of a get |
| §5.6 Perform | `verbs`, `store` | the intents signed in verbs' perform step; appended to the log by store |
| §5.7 Speak | `cli`, `guidance` | the echo and its rendering; the handlers' bodies after the data |
| §5.8 Record | `command` | the command event and the command log |
| §5.9 Sync | `store`, `git` | the background sync process in store; push and fetch through the user's git |
| the order of the §5 steps | `root` | the dispatcher every invocation passes through |
| exit classes and statuses | `reason`, `cli` | the four classes in reason; each layer's failure names its own reasons; statuses in cli |
| checkout discovery | `git` | the work tree, private git dir and common dir |
| the one enforcer and the in-use check | `verbs` | requirements and capabilities judged once, at the write |
| the snapshot | `store`, `fold` | its bytes in store; its content and key in fold |
| `/next`, `/lanes`, `/integrity`, `/me` | `fold`, `tick`, `address`, `cli` | computed by fold and tick, named by address, rendered by cli |
| the managed block in AGENTS.md | `guidance`, `root` | its text in guidance; written by init in root |
| liveness of a worktree | `git`, `tick` | read by git; decided by tick |
| budgets | `fold`, `store` | benchmarks beside the code they measure |
| field kinds, value parsers, built-in fields, the atom | `field` | the field language config and address both read |
| scenarios.md as the acceptance contract | `scenarios` | each scenario walked over root's twin |

## Decisions

The decisions the layout needed and the design documents did not make.

- **D1 Module path** `github.com/promptctl/lit2`: v1's org, the binary's name.
- **D2 Go and tooling.** `go 1.26`; the one third-party module linked is
  go-git, with its go-billy filesystem, in `git` alone, for lit's objects
  and refs (architecture.md decision B); none of its transport packages is
  imported. Tests are
  the standard `testing` package. Lint is golangci-lint v2 as a `tool`
  directive: `default: standard` plus exhaustive and gochecksumtype, and
  gofmt; sealed interfaces marked `//sumtype:decl` and typed-integer enums
  make a missing case a lint failure.
- **D3 Build** `CGO_ENABLED=0 go build ./cmd/lit2`, one command, a static
  binary; no Makefile.
- **D4 Argv** is the standard library and a scan over the two tables: a flag
  is `--name` anywhere, `--` ends the flags, `-` is a value.
- **D5 The one-way-deps check** is `internal/architecture`, over the
  `layers` block above: Go refuses cycles, not an upward edge that closes
  none.
- **D6 `now` and entropy.** The root reads the clock once at the start and
  hands that instant down as `now`; the second reading is only the
  duration. Entropy is an `io.Reader` in the process: `crypto/rand.Reader`,
  seeded ChaCha8 in the twin.
- **D7 Agent label** `LIT_AGENT`: empty is no label, anything else is the
  label, opaque.
- **D8 `config` above `fold`, `field` below `config`.** The fold reads no
  configuration (decision H); config and address both stand on field's
  kinds and atoms, so neither imports the other for them.
- **D9 Exit statuses**: 0 ok; 1 refused; 64 usage (`EX_USAGE`); 70 internal
  (`EX_SOFTWARE`); 78 environment (`EX_CONFIG`). Any other status means lit2
  died unrecorded. stdout carries only the answer asked for; stderr only the
  error line and the unrecorded line.
- **D10 Checkout discovery in process**: walk up to `.git`, a directory or a
  `gitdir:` file, then `commondir`; no git binary, no go-git, no `GIT_DIR`.
- **D11 On disk.** Per clone, `<git-common-dir>/lit/`: `store/`, the store's
  root, and `principal`, beside the store and outside it. Per checkout,
  `<git-dir>/lit/checkout/`: `id`, 32 lowercase hex digits and a newline,
  which git deletes with the worktree.
- **D12 Command log** `$XDG_STATE_HOME/lit/commands/<UTC date>.jsonl`
  (`~/.local/state` when unset): one line per invocation in one `O_APPEND`
  write, per machine; every write removes day files older than 30 days.
- **D13 No OTLP**: no network on a foreground path, so the file is the one
  export edge, and a failed write is said on stderr.
- **D14 Rows with no body yet** parse, appear in help, and end internal (70)
  naming the ticket that builds them, from one unexported function in
  `root` that the lint forces out once the last body is bound. Bare `lit2`
  is help.
- **D15 This file** is the codemap at the root; its concern table places
  what the design leaves unnamed.
