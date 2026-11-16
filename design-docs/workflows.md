# Workflows: named units of handlers

Status: draft, 2026-11-05.

A workflow is how a project tells an agent what to do at the moments lit can
see. This document settles what a workflow is made of, where one lives, how it
is layered and overridden, how it relates to columns, and what v2 ships. It
assumes [columns-and-phases.md](columns-and-phases.md) (a ticket's `status` is
its column; the engine interprets only `initial` and `terminal`) and
[transition-requirements.md](transition-requirements.md) (entry and exit
requirements, checked once on the transition write). It replaces the v1
workflow-sets proposal, since removed.

The owner's rulings it carries out: *"workflows come along, but let's design
them from the beginning that a workflow itself can consist of multiple pieces.
workflows definitely exist in v2. call an individual 'piece' of a workflow a
'handler'."* On v1's shape: *"Right now the feature is cool but it feels a
little too flexible. Like it's not clear how to use it."* And from the v1
epic: *"Overrides are fully supported. We shouldn't be building features that
just won't work if a user decides to override a template."* and *"standardize
on workflows for everything so we don't have a separate system for one use
case."*

## Two words

A **workflow** is a named node holding handlers, `/workflows/<name>`. It is the
unit a project adopts, declines and reads about as one thing, and its name is
the name of a discipline: `plan-review`, `capture`.

A **handler** is one binding and one body, `/workflows/<name>/handlers/<id>`.
It binds to a moment lit observes and prints guidance at that moment. It is the
unit that is overridden.

There is no third thing. The graph of columns that
[path-addressed-cli.md](path-addressed-cli.md) called "the workflow" is
`/columns` under [columns-and-phases.md](columns-and-phases.md); the word
workflow is reserved for the unit above.

## A handler

| field | type | meaning |
|---|---|---|
| `on` | list of moments | when it fires; at least one |
| `where` | selector | which subjects it fires for; empty matches every subject |
| `body` | text | what it prints |
| `enabled` | bool | whether it fires; true unless set |

### Moments

A moment is a kind and an argument. The kinds are closed: the engine knows
these six and nothing else, and everything that happens at one of them is data
`[LAW:types-are-the-program]`. This is
[project-intent.md](project-intent.md)'s substrate: the intervention points are
the engine's, the policy at each point flows through as data.

| moment | fires when | subject |
|---|---|---|
| `enter <column>` | a transition write landed and put the ticket in the column | the ticket |
| `leave <column>` | a transition write landed and took the ticket out of the column | the ticket |
| `in <column>` | a read printed a ticket in the column whole: `get x`, or `/next` serving it | the ticket |
| `refused <column>` | a transition into the column was refused by an entry or exit requirement | the ticket, unchanged |
| `wrote <glob>` | a write landed on a path matching the glob | the ticket owning the path; for `*/children`, the child created |
| `read <glob>` | a read printed a node matching the glob | the node read, when it is a ticket |

The argument is a glob in the CLI's glob grammar: `enter *` is every arrival,
`wrote */comments` is every comment on any ticket. There is no "command"
moment, because a v2 command is a verb on a path and `wrote` and `read` are the
verbs. `lit admin` commands raise no moments.

`where` is a selector in the CLI's own grammar, evaluated against the subject as
the command leaves it: after the write for `enter`, `leave` and `wrote`,
unchanged for `refused`. A handler for finishing any ticket is `on: [enter *]`,
`where: (stage=terminal)`; one for a labelled ticket is
`where: (labels~needs-design)`. A moment with no subject ticket (`read /lanes`)
has nothing for a selector to match, so only handlers with an empty `where`
fire there. `[LAW:composability]`: the binding and the filter are the glob and
the selector the CLI already has, not a grammar of their own.

### What it produces

Text, on the stream the command's output is on, printed after the node it
belongs to and never interleaved with data (the Output rules in
[path-addressed-cli.md](path-addressed-cli.md)). Under `--json` the same text
is a `guidance` array of `{handler, text}` on the object. Several handlers at
one moment print in address order, `<workflow>/<handler>` ascending.

A handler produces nothing else: no transition, no write, no subprocess, no
exit code. Enforcement is the column's requirements
([transition-requirements.md](transition-requirements.md)); a handler speaks, a
requirement refuses `[LAW:decomposition]`. The cut is what makes the owner's
override ruling hold: a project that rewrites every body cannot weaken a
requirement, because no body ever enforced one.
[guidance.md](guidance.md) reaches the same line from the other side: what can
be mechanical enforcement is, and guidance is what remains.

### What it reads

The body is prose with two substitutions: `{$}` is the subject's id and
`{$/<field>}` is a field of the subject, read after the write:
`{$/plan/verdict}`, `{$/status}`. A field not in the field table is refused when
the body is written, so no body references a field that does not exist
`[LAW:parse-dont-validate]`. A body that needs a fact about the ticket names
the field, never copies the value, so it cannot go stale. There is no other
input: no environment, no clock, no files.

## A workflow

| field | type | meaning |
|---|---|---|
| `summary` | one line | the discipline, for the listing |
| `enabled` | bool | true unless set; off fires none of its handlers, whatever theirs say |
| `handlers` | map id → handler | the handlers |

Adopting a shipped workflow is nothing: it is on. Declining it is one write,
`set /workflows/plan-review/enabled false`. Authoring one is `set
/workflows/<name> {…}` and one `set` per handler. There is no "replace a
workflow": to run your own version of a shipped discipline, turn the shipped
one off and author yours under another name. `[LAW:no-mode-explosion]`: a
replace would be a third setting between inherit and author, and two writes
already say it.

## Where workflows live

Three layers, farthest to nearest:

1. **shipped**: compiled into the binary.
2. **user**: `$XDG_CONFIG_HOME/lit/config.toml`, tables
   `[workflows.<name>]` and `[workflows.<name>.handlers.<id>]`; one person,
   every repository.
3. **project**: nodes in the store. `/workflows` at the root, and
   `<node>/workflows` on any node, the way
   [path-addressed-cli.md](path-addressed-cli.md) lets any node carry `lanes`
   or `claim_ttl`.

A workflow is workspace truth. [workspace-configuration.md](workspace-configuration.md)'s
test asks whether two machines that disagree can both be right about the
backlog; a workflow refines the test one step, because two clones with
different handlers are running two processes on one backlog, and the owner's
requirement is that every clone runs the same one. The deciding fact is what a
workflow binds to: its moments name columns and its project layer can sit on
an epic, and both are store nodes. A workflow kept in a file outside the store
would reference nodes it cannot see from a place the ancestor rule cannot
reach. That document's table row placing "workflow set adoption" in the project
config file is superseded. The user layer is a file because it spans
repositories and no store holds it.

### Resolution

Every field of every workflow and handler takes its value from the nearest
record that sets it, walking from the ticket's own node up through its
ancestors to the root, then the user layer, then the shipped layer.

The rule is per field, not per handler, and that is the whole override design.
A project that rewrites the `body` of `phases/handlers/impl` holds a record
that says only `body`; `on` and `enabled` still come from the shipped layer,
so a release that rebinds the handler carries the project's text with it, and
a release that improves `phases/handlers/review` reaches the project untouched.
v1's override was a whole-file copy that froze every field at once; here the
smallest override is one field and nothing larger exists
`[LAW:one-source-of-truth]`.

A resolved handler must end with `on` and `body`. A record for a handler no
farther layer defines is refused at the write unless it carries both; v1 loaded
an inert definition and warned, v2 does not let one exist
`[LAW:parse-dont-validate]`. A handler whose `on` names a column absent from
the frame it is resolved in is legal (the user layer spans repositories whose
columns differ) and the listing reports it as `unbound`.

### The layers view

Every workflow and handler node has a derived child, `layers`, holding each
layer's raw record at that address: `…/handlers/impl/layers/shipped`,
`…/layers/user`, `…/layers/project`. The resolved path is the default write
target and writes the project layer, as every path does; `…/layers/user/<f>`
is the write target for the user layer; `…/layers/shipped` is derived, so
`set` on it fails at parse like `set x/claim` does.

`rm` on a resolved path removes the project record at that address and nothing
else; what remains is what the farther layers say. `rm` where no project record
exists is refused, naming `set …/enabled false` as the way to turn something
off. `rm …/layers/user/…` removes the user record the same way.

### Staleness

When lit writes an override of a field it records beside it the digest of the
value it shadowed in the next farther layer. An override is **stale** when that
layer's current value has a different digest: the shipped text moved under a
project rewrite. It is **unchanged** when it equals the value it shadows, an
override doing nothing. Both are computed on read, never stored
`[FRAMING:representation]`; a record written by hand with no shadow digest
reads `base unknown`.

## Columns, the quickstart, and the managed files

**Column guidance is handlers.** [path-addressed-cli.md](path-addressed-cli.md)
put a `guidance` field on the column node and had `get x` print it after the
ticket. That text is a handler bound `in <column>`, and `/columns/<c>/guidance`
is derived: the bodies of the enabled handlers bound `in <c>`, in address
order, for the frame it is read in. It is not settable; the write is `set
/workflows/<w>/handlers/<h>/body`. One mechanism prints after a ticket, and
the column still answers "what do I do here" at the address the CLI doc gave
it.

**The quickstart is a workflow.** Bare `lit` in a workspace is `get /`, and a
handler bound `read /` prints what v1's router printed. v1's topics were
guidance at moments v1 could not bind, so they were a second system; v2 binds
them (`wrote */children`, `enter *` with `(stage=terminal)`, `read /integrity`,
`read */plan`) and they are handlers. `quickstart --eject` and `--refresh` go
with the files they copied.

**The AGENTS.md / CLAUDE.md block and the pre-push hook are not workflows.** A
handler prints at a moment lit observes. The block is written into a file lit
does not own, by `lit admin init`, and is read at a moment lit never sees, the
start of a session; its content is one pointer carrying no policy: work here is
tracked by lit, run `lit` first and follow what it prints. Policy begins at
`read /`, inside workflows. The hook runs a sync, which is an effect, and
handlers produce none `[LAW:effects-at-boundaries]`. Both belong to `lit
admin`.

## Shipped workflows

Four, each a discipline a project declines with one write. Bodies are v1's
with their commands rewritten to the path grammar.

- **`orientation`**: how to work here.
  - `arrive`, `read /`: what lit is, `get /next` to pick work, every node
    prints its guidance after itself, where the verb table is.
  - `integrity`, `read /integrity`: resolve what you can yourself before
    paging the owner.
- **`phases`**: which write records the next move.
  - `impl`, `enter impl` and `in impl`: when the PR opens, `set {$}/status
    review`.
  - `review`, `enter review` and `in review`: when review is done and the
    merge is under way, `set {$}/status merge`.
  - `findings`, `in review` where `(pr/verdict=fail)`: address every finding or
    say on the thread why not; do not widen the change. This is the rework
    guidance of [columns-and-phases.md](columns-and-phases.md): a handler with
    a selector, not a rule.
  - `merge`, `enter merge` and `in merge`: once merged, `set {$}/status done`.
- **`plan-review`**: a plan is written, proven and reviewed before code.
  - `plan`, `enter plan` and `in plan`: v1's plan text: core first, prove each
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
  - `finished`, `enter *` where `(stage=terminal)`: v1's post-close reminder:
    update the related tickets with what this work taught.

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

`get /workflows` lists every resolved workflow in the frame it is run from,
one line per workflow and per handler: name, on or off, the moments and
selector, then the origin. The origin is `shipped` when every field is
shipped, else each overridden field with its layer and, where it applies,
`stale`, `unchanged` or `base unknown`:

```
/workflows/phases        on   which write records the next move     shipped
  impl                   on   enter impl, in impl                   shipped
  review                 off  enter review, in review               enabled:project
  merge                  on   enter merge, in merge                 shipped
/workflows/plan-review   on   a plan is written, proven and reviewed before code   shipped
  plan                   on   enter plan, in plan                   body:project stale
  brief                  on   read */plan                           shipped
  unreviewed             on   refused impl                          shipped
/workflows/review        on   what a review is in this repository   project
  entered                on   enter review, in review               project
  not-yet                on   refused done                          project
```

`get /workflows/plan-review/handlers/plan` prints the handler whole, each field
with its layer, then the body. `get …/plan/layers` prints the raw record at
each layer, which is how the shipped text behind a stale override is read; the
old `workflows show` and `quickstart <stage>` are both this read. `--json` on
any of these prints the same, keyed by path.

`--dry-run` on a write prints, after the diff and the derivations the write
would echo, the guidance that would print, each body headed by its handler's
address. That is v1's `workflows dry-run` with the real write as the
hypothetical.

## Observability

The command's event records, under `handlers`:

- `moments`: the moments the command raised, `[]` when none.
- `fired`: per handler that printed: its address, the moment it matched, the
  layer each field came from, and its stale fields.
- `suppressed`: per handler that matched a moment and did not print: address,
  moment, and one reason: `workflow-off@<layer>`, `handler-off@<layer>`, or
  `where-unmatched`.

`moments: []` and `fired: []` are different facts and both are written
`[LAW:nothing-unseen]`. v1's firing-trace files are gone; the event is the
record `[LAW:one-source-of-truth]`.

## Gone from v1

Frontmatter markdown files and their three directories; the three activation
dimensions (`labels`, `states`, `events`), which are `on` and `where`; the
`lit workflows` family (overview, `show`, `edit`, `dry-run`); `quickstart`,
its eight assets, `--eject` and `--refresh`; `<id>` substitution, which is
`{$}`; firing-trace files.

## Decisions for the owner

1. The project layer's home. Recommended: the store, as above. The alternative
   is a committed `.lit/workflows.toml`, which keeps
   [workspace-configuration.md](workspace-configuration.md)'s table as drafted
   and loses workflows on epics and lit-recorded shadow digests.
2. The shipped list and names above.
