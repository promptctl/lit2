# Workflows: named units of handlers

A workflow is how a project tells an agent what to do at the moments lit can
see. This document settles what a workflow is made of, where one lives, how it
is layered and overridden, and how it relates to columns. It builds on
[columns-and-phases.md](columns-and-phases.md) and
[transition-requirements.md](transition-requirements.md).

## Two words

A **workflow** is a named node holding handlers, `/workflows/<name>`. It is the
unit a project adopts, declines and reads about as one thing, and its name is
the name of a discipline: `plan-review`, `capture`.

A **handler** is one binding and one body, `/workflows/<name>/handlers/<id>`.
It binds to a moment lit observes and runs its body at that moment. It is the
unit that is overridden.

The graph of columns that
[path-addressed-cli.md](path-addressed-cli.md) called "the workflow" is
`/columns` under [columns-and-phases.md](columns-and-phases.md); the word
workflow is reserved for the unit above.

## A handler

| field | type | meaning |
|---|---|---|
| `on` | list of moments | when it fires; at least one |
| `where` | selector | which subjects it fires for; empty matches every subject |
| `body` | text | what it runs at the moment |
| `enabled` | bool | whether it fires; true unless set |

### Moments

A moment is a kind and an argument. The engine knows these six kinds, and what
happens at one of them is data `[LAW:types-are-the-program]`.

| moment | fires when | subject |
|---|---|---|
| `enter <column>` | a transition write landed and put the ticket in the column | the ticket |
| `leave <column>` | a transition write landed and took the ticket out of the column | the ticket |
| `in <column>` | a read printed a ticket in the column whole: `get x`, or `/next` serving it | the ticket |
| `refused <column>` | a transition into the column was refused by an entry or exit requirement | the ticket, unchanged |
| `wrote <glob>` | a write landed on a path matching the glob | the ticket owning the path; for `*/children`, the child created |
| `read <glob>` | a read printed a node matching the glob | the node read, when it is a ticket |

The argument is a glob in the CLI's glob grammar: `enter *` is every arrival,
`wrote */comments` is every comment on any ticket.

`where` is a selector in the CLI's own grammar, evaluated against the subject as
the command leaves it: after the write for `enter`, `leave` and `wrote`,
unchanged for `refused`. A handler for finishing any ticket is `on: [enter *]`,
`where: (stage=terminal)`; one for a labelled ticket is
`where: (labels~needs-design)`. A moment with no subject ticket (`read /lanes`)
has nothing for a selector to match, so only handlers with an empty `where`
fire there. `[LAW:composability]`: the binding and the filter are the glob and
the selector the CLI already has, not a grammar of their own. Several handlers at
one moment run in address order, `<workflow>/<handler>` ascending.

### What it reads

The body is prose with two substitutions: `{$}` is the subject's id and
`{$/<field>}` is a field of the subject, read after the write:
`{$/plan/verdict}`, `{$/status}`. A field not in the field table is refused
when the body is written.

## A workflow

| field | type | meaning |
|---|---|---|
| `summary` | one line | the discipline, for the listing |
| `enabled` | bool | true unless set; off fires none of its handlers, whatever theirs say |
| `handlers` | map id → handler | the handlers |

Shipped workflows are on by default. Declining one is one write,
`set /workflows/plan-review/enabled false`. Authoring one is `set
/workflows/<name> {…}` and one `set` per handler. To run your own version of a
shipped discipline, turn the shipped one off and author yours under another name.

## Where workflows live

The layers, farthest to nearest:

1. **shipped**: the workflows that come with lit.
2. **user**: one person, every repository.
3. **project**: `.lit/workspace.toml`, the same file that holds the columns:
   `[workflows.<name>]` at the root, and in a per-node section keyed by a
   node's id for a workflow on an epic, the way the file lets any node carry
   `columns` or `claim_ttl`. The paths `/workflows/<name>` and
   `<node>/workflows/<name>` address those sections; a write through the
   verbs edits the working-tree draft, and the version in force is the one
   applied with `config commit`.

### Resolution

A handler resolves whole: the nearest record that defines it, walking from the
ticket's own node up through its ancestors to the root, then the user layer, then
the shipped layer, supplies every field of the handler, and nothing is merged
across records. A project that rewrites the `body` of `phases/handlers/impl` holds
a record of the whole handler, so a release that changes the shipped handler does
not reach it, and the override reads stale (below).

A handler record carries `on` and `body`. A handler whose `on` names a column
absent from the node it is resolved in is legal, since the user layer spans
repositories whose columns differ. It does not fire, and the configuration check
reports it as a warning.

### The layers view

Every workflow and handler node has a derived child, `layers`, holding each
layer's raw record at that address: `…/handlers/impl/layers/shipped`,
`…/layers/user`, `…/layers/project`. The resolved path is the default write
target and writes the project layer, the draft of `.lit/workspace.toml`, as
every model path does; `…/layers/user/<f>` is the write target for the user
layer; `…/layers/shipped` is derived, so `set` on it fails at parse like
`set x/claim` does. A `set` on one field of a resolved handler writes the project
record of the whole handler, starting from the resolved one.

`rm` on a resolved path removes the project record at that address and nothing
else; what remains is what the farther layers say. `rm` where no project record
exists is refused, naming `set …/enabled false` as the way to turn something
off. `rm …/layers/user/…` removes the user record the same way.

### Staleness

When lit writes an override it records beside it the digest of the
record it shadowed in the next farther layer. An override is **stale** when that
layer's current value has a different digest: the shipped text moved under a
project rewrite. It is **unchanged** when it equals the value it shadows, an
override doing nothing. Both are computed on read, never stored
`[FRAMING:representation]`; a record written by hand with no shadow digest
reads `base unknown`.

## Columns, the quickstart, and the managed files

**Column guidance is handlers.** A column's guidance is the handlers bound
`in <column>`, and `/columns/<c>/guidance` is derived: the bodies of the enabled
handlers bound `in <c>`, in address order, for the node it is read in. It is not
settable; the write is `set /workflows/<w>/handlers/<h>/body`.

**The quickstart is a workflow.** Bare `lit` in a workspace is `get /`, and a
handler bound `read /` orients the agent. Help topics are handlers too
(`wrote */children`, `enter *` with `(stage=terminal)`, `read /integrity`,
`read */plan`).

**The AGENTS.md / CLAUDE.md block and the pre-push hook.** The block is written
into a file lit does not own, by `lit admin init`; its content is one pointer
carrying no policy: work here is tracked by lit, run `lit` first and follow what
it prints. The block and the hook are installed by `lit admin`.

## Shipped workflows

Undecided: the shipped list and names. The current proposal is four, each a
discipline a project declines with one write.

- **`orientation`**: how to work here.
  - `arrive`, `read /`: what lit is, `get /next` to pick work, where the verb
    table is.
  - `integrity`, `read /integrity`: resolve what you can yourself before
    paging the owner.
- **`phases`**: which write records the next move.
  - `impl`, `enter impl` and `in impl`: when the PR opens, `set {$}/status
    review`.
  - `review`, `enter review` and `in review`: when review is done and the
    merge is under way, `set {$}/status merge`.
  - `findings`, `in review` where `(pr/verdict=fail)`: address every finding or
    say on the thread why not.
  - `merge`, `enter merge` and `in merge`: once merged, `set {$}/status done`.
- **`plan-review`**: a plan is written, proven and reviewed before code.
  - `plan`, `enter plan` and `in plan`: core first, prove each
    fact against the real system, record with `add {$}/plan/revisions -`, hand
    it to a reviewer with a fresh context.
  - `brief`, `read */plan`: the reviewer's brief: load the laws, judge the
    whole mechanism at this revision, record one review with `add
    {$}/plan/reviews`, naming the revision in `of`.
  - `unreviewed`, `refused impl`: the plan has no passing review of its
    latest revision; have it reviewed, or revise it and have the new revision
    reviewed.
- **`capture`**: prepare the next loop ([guidance.md](guidance.md)).
  - `created`, `wrote */children`: the description says what and why and
    survives a refactor; done-when is checkable.
  - `finished`, `enter *` where `(stage=terminal)`: update the related tickets with what this work taught.

## Writing one

A project workflow `review` with two handlers, then one shipped handler turned
off without touching its neighbours:

```
set /workflows/review '{summary:"what a review is in this repository"}'
set /workflows/review/handlers/entered '{on:[enter review, in review] body:-}' <<'EOT'
{$} is in review. Run the review cycle on its PR and push the fixes; move it
with `set {$}/status merge` only when the final pass is clean.
EOT
set /workflows/review/handlers/not-yet '{on:[refused done] body:"{$} does not finish from {$/status}: it leaves through review. `set {$}/status review` first."}'

set /workflows/phases/handlers/review/enabled false
```

Each `set` echoes the record it wrote; the last one echoes `enabled true →
false (shadows shipped)`.

## Inspection

`get /workflows` lists every resolved workflow in the node it is run from,
one line per workflow and per handler: name, on or off, the moments and
selector, then the origin. The origin is `shipped` when every field is
shipped, else the layer that supplied it and, where it applies,
`stale`, `unchanged` or `base unknown`:

```
/workflows/phases        on   which write records the next move     shipped
  impl                   on   enter impl, in impl                   shipped
  review                 off  enter review, in review               project
  merge                  on   enter merge, in merge                 shipped
/workflows/plan-review   on   a plan is written, proven and reviewed before code   shipped
  plan                   on   enter plan, in plan                   project stale
  brief                  on   read */plan                           shipped
  unreviewed             on   refused impl                          shipped
/workflows/review        on   what a review is in this repository   project
  entered                on   enter review, in review               project
  not-yet                on   refused done                          project
```

`get /workflows/plan-review/handlers/plan` prints the handler whole, with the layer
that supplied it, then the body. `get …/plan/layers` prints the raw record at
each layer, which is how the shipped text behind a stale override is read. `--json` on
any of these prints the same, keyed by path.

`--dry-run` on a write prints, after the diff and the derivations the write
would echo, the handlers that would fire, each body headed by its handler's
address.

## Observability

The command's event records, under `handlers`:

- `moments`: the moments the command raised, `[]` when none.
- `fired`: per handler that fired: its address, the moment it matched, the
  layer each field came from, and its stale fields.
- `suppressed`: per handler that matched a moment and did not fire: address,
  moment, and one reason: `workflow-off@<layer>`, `handler-off@<layer>`, or
  `where-unmatched`.

`moments: []` and `fired: []` are different facts and both are written
`[LAW:nothing-unseen]`.
