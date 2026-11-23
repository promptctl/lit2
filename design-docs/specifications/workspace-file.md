# The workspace file

Carries out `strategy/configuration.md`.

A workspace's model is one declared configuration, `.lit/workspace.toml`,
committed in the repository with the code. The model in force is the
**active version**, the last one applied into the log with
`lit2 config commit`. The file itself never becomes an event; an apply
carries its bytes.

## Schema

The file declares:

| key | what it declares | meaning defined in |
|---|---|---|
| `prefix` | the project prefix ids render under | `specifications/events-and-store.md`, "Ids" |
| `min-reader-version` | the oldest binary that may read or write the store | `specifications/events-and-store.md`, "Unknown events" |
| `levels.<n>` | the hierarchy levels: `name`, and `home` above level 1 | `specifications/work-items.md` |
| `levels.<n>.fields.<f>` | a custom field: its kind and the kind's parameters | `specifications/work-items.md` |
| `levels.<n>.template` | defaults applied to a new item at that level | `specifications/work-items.md` |
| `links.<name>` | a link type: `kind` and `between` | `specifications/work-items.md` |
| `labels` | the declared labels, a top-level list | `specifications/work-items.md` |
| `checks.<name>` | a named list of tests | `specifications/columns-and-requirements.md` |
| `columns.<name>` | the column graph: flags, `next`, `entry`, `exit` | `specifications/columns-and-requirements.md` |
| `workflows.<name>` | the project layer of workflows and handlers | `specifications/workflows.md` |
| `claim_ttl` | the claim freshness window | `specifications/lanes-and-claims.md` |
| `nodes."<id>"` | a per-node section keyed by a node's id, carrying `columns`, `workflows` or `claim_ttl` for that node and its descendants | the spec of each key it carries |

Every key is also a node under the root, read and written with the four
verbs (`specifications/work-items.md`, "Levels and the tree").

## Applying a version

`lit2 config commit` applies a committed blob of the file as the next active
version. It refuses a dirty or untracked file.

An apply is an event that carries the version's bytes and its git blob id
and names, by event id, the applies it replaces, as any register write names
its parents. Every event's `config-ref` names the active version its writer
ran under, and the write is judged under that version
(`specifications/fold.md`). An apply's own `config-ref` is the version in
force when it was written; version one's is its own blob.

The refusal of a store with no version in force names `lit2 config commit`.

**Init.** `lit2 admin init` writes the default profile to
`.lit/workspace.toml` when no file exists and applies the same bytes as
version one, so the file and the active version share one blob id from the
start. An existing file is applied instead, and must parse. Where the apply
falls in init is `specifications/access.md`, "Enrollment and joining".

**Contending applies.** The live applies are those no later apply names;
the active version is the last live one in total order. Live applies holding
two or more blobs are contending, a conflict value on the workspace in the
fold; one blob applied twice, as two inits of the default are, is agreement.
While they contend, every command runs on the active version, the board line
and `config status` name each contending blob and its writer, and the next
`config commit` names every live apply, which resolves them. Before sync
each writer sees only its own apply.

**Drift.** The file and the active version drift apart whenever one is
edited or the other applied. `config status` reports the drift and whether
the active blob is in HEAD's history, and the board carries one line for it.
The board compares git's id for the working file's bytes with the active
blob rather than parsing the file.

Rejected: reading the configuration from the code repository at command
time. Because the log carries the applied version's bytes, every verifier
can read the model in force.

## Validation

**One parse of the whole document, at apply.** `lit2 config commit` parses
the version it applies into one typed value. A refusal names every fault
with its key path and applies nothing, and nothing overrides it:

```
error (reason=schema): /links/blocks/between [1,4] names no level; nothing applied
```

`Parse` reads one version's bytes into one typed schema. It is a pure
function with no I/O, and every command runs it over the active version, so
every checkout of the repository holds the same typed schema
`[LAW:parse-dont-validate]`.

The parse checks every rule the levels, fields, link types and templates
(`specifications/work-items.md`) and the columns, checks and requirements
(`specifications/columns-and-requirements.md`, "Validity of a column
graph") state, with the column-graph check run against every live version,
so a ticket moved into a column only a contending version declares is
protected too.

**The configuration check.** `config status` reports, beside drift and
contention, the warnings a valid version can still carry, such as a handler
bound to a column the workspace lacks.

## The default profile

The default profile is `internal/assets/workspace/default.toml`, a data file
in the same grammar.
