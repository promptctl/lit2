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
only. The rows from `cli` to `git` are §6's drawing, in its order; `files`,
below them, is the files of one directory: the identity files, the
founding pin, the work tree's `.lit` and the store's logs are each read and
written through a port of it, while the command log and lit's repository,
which git writes, are not.

```layers
architecture
scenarios
root
command
cli
guidance
verbs
address
evaluate
config
field
fold
events
store
git
files
reason
assets
panics
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
| §3.1 Node and path | `address`, `config`, `field` | the path grammar, selectors and globs in address; the field table in config, the one answer to what a path names at a place; the atom selectors share and the literal grammar in field |
| §3.2 Event and intent | `events`, `verbs` | the event record and ids in events; the verb-to-intent table in verbs |
| §3.3 Fold and evaluate | `fold`, `evaluate` | fold(events) → state; evaluate(state, config, now) → view |
| §3.4 Work item, level, tree | `config`, `fold`, `evaluate` | levels and homes declared in config; the tree folded; stage and rollup in evaluate |
| §3.5 Column and stage | `config`, `evaluate` | the column graph and its check in config's parse; stage and the undeclared column in evaluate |
| §3.6 Requirement | `verbs`, `field`, `config`, `evaluate` | evaluated by the one enforcer in verbs; the test is field's atom; checks expanded in config; `held` in evaluate |
| §3.7 Lane | `fold` | membership, order and the one rank order |
| §3.8 Claim | `fold`, `evaluate` | the holder set folded; expiry in evaluate |
| §3.9 Checklist | `fold`, `verbs` | items folded; the `set` list diff in verbs |
| §3.10 Link | `fold`, `config` | links folded; link types declared in config |
| §3.11 Label | `config`, `fold` | declared in config in the top-level `labels` list; applied as events and folded |
| §3.12 Moment and handler | `guidance` | moments, handlers, workflows, their three layers |
| §3.13 Rewrite | `fold`, `store` | applied by the fold; segments re-sealed by store |
| §3.14 Identity | `root`, `events` | the checkout id, principal and agent label read in root; attribution carried in events |
| §3.15 The workspace file | `config`, `fold`, `evaluate`, `verbs`, `root`, `assets` | the one schema parse of an applied version in config; the applies and which version is in force folded; the §3.5 check against every live version in evaluate; the one judge of an apply, `Admit`, and the board's `workspace` line in verbs; `config commit`, `config status` and init's version one in root; the default profile in assets |
| §3.16 Log, segment and ref | `store`, `git` | the log, segments, writer refs and inbox refs in store; objects and refs written by the git binary and read by go-git |
| §3.17 Principal and policy | `events`, `fold` | the key, signature and suite in events; the policy as events folded |
| §3.18 Capability | `verbs`, `fold` | the capability verdict in fold's step, the one enforcer, over roles from the folded policy; the default roles, membership paths compiled to proposals and a refusal's rendering in verbs |
| §3.19 Tier and envelope | `events`, `verbs` | the envelope and suite in events; prose sealed in verbs, so no layer below sees it |
| §3.20 Verification | `fold`, `store` | one fold rule, signature then policy, in fold's step via the suite in events; run by store at receive |
| §5.1 Open | `store` | ref heads, every open log under the common dir and the snapshot read |
| §5.2 Fold | `fold` | events newer than the snapshot, verified |
| §5.3 Parse | `config` | the active workspace version the fold names, into the schema value |
| §5.4 Address | `address` | paths into typed addresses; selectors and globs expanded once |
| §5.5 Evaluate | `verbs`, `evaluate` | the description of a write; the answer of a get |
| §5.6 Perform | `verbs`, `store` | the intents signed in verbs' perform step; appended to the log by store |
| §5.7 Speak | `cli`, `guidance` | the echo and its rendering; the handlers' bodies after the data |
| §5.8 Record | `command` | the command event and the command log |
| §5.9 Sync | `store`, `git` | the background sync process in store; push and fetch through the user's git |
| the order of the §5 steps | `root` | the dispatcher every invocation passes through |
| exit classes and statuses | `reason`, `cli` | the four classes in reason; each layer's failure names its own reasons; statuses in cli |
| checkout discovery | `git` | the work tree, private git dir and common dir |
| the code repository's HEAD and history | `git` | the blob HEAD holds at a path and whether one was ever there, read, never written |
| the one enforcer and the in-use check | `verbs`, `fold` | requirements judged once by verbs' gate, at the write; capabilities by fold's step, which every replica runs and a write runs over its own events before appending |
| the snapshot | `store`, `fold` | its bytes in store; its content and key in fold |
| `/next`, `/lanes`, `/integrity`, `/me` | `fold`, `evaluate`, `address`, `cli` | computed by fold and evaluate, named by address, rendered by cli |
| the managed block in AGENTS.md | `guidance`, `root` | its text in guidance; written by init in root |
| liveness of a worktree | `git`, `evaluate` | read by git; decided by evaluate |
| budgets | `fold`, `store` | benchmarks beside the code they measure |
| field kinds, value parsers, built-in fields, the atom | `field` | the field language config and address both read |
| `@` references in prose | `verbs`, `cli` | normalized to `@<segments>` in verbs before the envelope seals; rendered as `@` plus the current name by cli at Speak |
| scenarios.md as the acceptance contract | `scenarios` | each scenario walked over root's twin |
| a test's hold on the panic it names | `panics` | a precondition's panic identified by its error, any other panicked again; imported by tests alone |

## Decisions

The decisions the layout needed and the design documents did not make.

- **D1 Module path** `github.com/promptctl/lit2`: v1's org, the binary's name.
- **D2 Go and tooling.** `go 1.26`; the third-party modules are the ones
  the `dependencies` block names, each in one unit: go-git with its go-billy
  filesystem in `git` alone, to read lit's objects and refs in process
  (architecture.md decision B), through its storage and object packages,
  so none of its transport packages is linked; edwards25519 in
  `events` for the signature check; the TOML parser and `x/mod` in
  `config`. Tests are
  the standard `testing` package. Lint is golangci-lint v2 as a `tool`
  directive: `default: standard` plus exhaustive and gochecksumtype, and
  gofmt; sealed interfaces marked `//sumtype:decl` and typed-integer enums
  make a missing case a lint failure.
- **D3 Build** `CGO_ENABLED=0 go build ./cmd/lit2`, one command, a static
  binary; no Makefile.
- **D4 Argv** is the standard library and a scan over the tables, the verbs
  and each group's (`admin`, `config`): a flag is `--name` anywhere, `--`
  ends the flags, `-` is a value.
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
- **D10 Checkout discovery by git**: `git rev-parse` from the working
  directory, under the process's environment, so `GIT_DIR`,
  `GIT_CEILING_DIRECTORIES` and safe.directory count as they do for git.
- **D11 On disk.** Per clone, `<git-common-dir>/lit/`: `store/`, the store's
  root, and `principal`, beside the store and outside it. Per checkout,
  `<git-dir>/lit/checkout/`: `id`, 32 lowercase hex digits and a newline,
  which git deletes with the worktree. Per work tree, `.lit/workspace.toml`,
  committed with the code, which only init writes, and only when absent.
  Each of these directories, and the store's `log/`, is read and written
  through its own `files` port over a base git or the work tree names:
  every element of the port's directory is checked from that base, in
  turn, and must be the real directory at its name, never a symlink; a
  file's name is one path element, and the file must be a regular file. So
  no port reaches another's files. Only the store opens `log/`. `repo/` is
  git's, opened by its path, and on no port.
- **D12 Command log** `$XDG_STATE_HOME/lit/commands/<UTC date>.jsonl`
  (`~/.local/state` when unset): one line per invocation in one `O_APPEND`
  write, per machine; every write removes day files older than 30 days.
- **D13 No OTLP**: no network on a foreground path, so the file is the one
  export edge, and a failed write is said on stderr.
- **D14 Rows with no body yet** parse, appear in help, and end internal (70)
  naming the ticket that builds them, from one unexported function in
  `root` that the lint forces out once the last body is bound. Bare `lit2`
  is `get /`.
- **D15 This file** is the codemap at the root; its concern table places
  what the design leaves unnamed.
