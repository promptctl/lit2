# Workflows, handlers and the guidance surfaces

Carries out `strategy/guidance.md` and `strategy/process.md`.

## Workflow and handler

A **workflow** is a named node holding handlers, `/workflows/<name>`. It is
the unit a project adopts, declines and reads about as one thing, and its
name is the name of a discipline: `plan-review`, `capture`. The column graph
is `/columns`; the word workflow means this unit.

A **handler** is one binding and one body, `/workflows/<name>/handlers/<id>`.
It binds to a moment lit observes and runs its body at that moment. It is
the unit that is overridden.

| handler field | type | meaning |
|---|---|---|
| `on` | list of moments | when it fires; at least one |
| `where` | selector | which subjects it fires for; empty matches every subject |
| `body` | text | what it runs at the moment |
| `enabled` | bool | whether it fires; true unless set |

| workflow field | type | meaning |
|---|---|---|
| `summary` | one line | the discipline, for the listing |
| `enabled` | bool | true unless set; off fires none of its handlers, whatever theirs say |
| `handlers` | map id → handler | the handlers |

## Moments

A moment is a kind and an argument. What happens at a moment is data
`[LAW:types-are-the-program]`. The kinds:

| moment | fires when | subject |
|---|---|---|
| `enter <column>` | a transition write landed and put the ticket in the column | the ticket |
| `leave <column>` | a transition write landed and took the ticket out of the column | the ticket |
| `in <column>` | a read printed a ticket in the column whole: `get x`, or `/next` serving it | the ticket |
| `refused <column>` | a transition into the column was refused by an entry or exit requirement | the ticket, unchanged |
| `wrote <glob>` | a write landed on a path matching the glob | the ticket owning the path; for `*/children`, the child created |
| `read <glob>` | a read printed a node matching the glob | the node read, when it is a ticket |

The argument is a glob in the command line's glob grammar: `enter *` is every
arrival, `wrote */comments` is every comment on any ticket.

`where` is a selector in the command line's grammar, evaluated against the
subject as the command leaves it: after the write for `enter`, `leave` and
`wrote`, unchanged for `refused`. A handler for finishing any ticket is
`on: [enter *]`, `where: (stage=terminal)`; one for an epic finishing is
`where: (stage=terminal parent/stage=terminal)`. A moment with no subject
ticket (`read /lanes`) has nothing for a selector to match, so only handlers
with an empty `where` fire there `[LAW:composability]`. Several handlers at
one moment run in address order, `<workflow>/<handler>` ascending.

The body is text with two substitutions: `{$}` is the subject's id, and
`{$/<field>}` is a field of the subject read after the write, such as
`{$/plan/verdict}`. A field not in the field table is refused when the body
is written.

## Where workflows live

The layers, farthest to nearest:

1. **shipped**: the workflows that come with lit2.
2. **user**: one person, every repository, in that person's own
   configuration.
3. **project**: `.lit/workspace.toml`, the file that holds the columns:
   `[workflows.<name>]` at the root, and in a node's per-node section for a
   workflow on that node. `/workflows/<name>` and `<node>/workflows/<name>`
   address them, and a write through the verbs edits the file's working-tree
   draft (`specifications/workspace-file.md`, "Applying a version").

Shipped workflows are on by default. Declining one is one write,
`set /workflows/plan-review/enabled false`. Authoring one is
`set /workflows/<name> '{…}'` and one `set` per handler. To run your own
version of a shipped discipline, turn the shipped one off and author yours
under another name.

### Resolution

A handler resolves whole: the nearest record that defines it, walking from
the ticket's own node up through its ancestors to the root, then the user
layer, then the shipped layer, supplies the whole handler, and nothing is
merged across records. A project that rewrites the `body` of
`phases/handlers/impl` holds a record of the whole handler, so a release that
changes the shipped handler does not reach it, and the override reads stale.

A handler whose `on` names a column absent from the node it resolves in is
legal, since the user layer spans repositories whose columns differ. It does
not fire, and `config status` reports it as a warning.

### The layers view

Every workflow and handler node has a derived child, `layers`, holding each
layer's raw record at that address: `…/handlers/impl/layers/shipped`,
`…/layers/user`, `…/layers/project`. The resolved path is the default write
target and writes the project layer. `…/layers/user/<f>` is the write target
for the user layer. `…/layers/shipped` is derived, so `set` on it fails at
parse. A `set` on one field of a resolved handler writes the project record of
the whole handler, starting from the resolved one.

`rm` on a resolved path removes the project record at that address and
nothing else; what remains is what the farther layers say. `rm` where no
project record exists is refused, naming `set …/enabled false` as the way to
turn something off. `rm …/layers/user/…` removes the user record the same
way.

### Staleness

When lit2 writes an override it records beside it the digest of the record
it shadowed in the next farther layer. An override is **stale** when that
layer's current record has a different digest, and **unchanged** when it
equals the record it shadows. Both are computed on read, never stored. A
record written by hand with no shadow digest reads `base unknown`.

## Column guidance

A column's guidance is the handlers bound `in <column>`.
`/columns/<c>/guidance` is derived: the bodies of the enabled handlers bound
`in <c>`, in address order, for the node it is read in. It is not settable;
the write is `set /workflows/<w>/handlers/<h>/body`. A column record carries
no guidance of its own.

## Shipped workflows

Undecided: the shipped list and names. The current proposal is four, each a
discipline a project declines with one write.

- **`orientation`**: how to work here.
  - `arrive`, `read /`: what lit2 is, the four verbs, `get /next` to pick
    work, where the verb table is, that every node prints its guidance after
    itself, and that selectors and globs are single-quoted.
  - `integrity`, `read /integrity`: resolve what you can yourself before
    paging the owner.
- **`phases`**: which write records the next move.
  - `impl`, `enter impl` and `in impl`: when the PR opens,
    `add {$}/pr/revisions <head>` and `set {$}/status review`.
  - `review`, `enter review` and `in review`: when review is done and the
    merge is under way, `set {$}/status merge`.
  - `findings`, `in review` where `(pr/verdict=fail)`: address every finding
    or say on the thread why not.
  - `merge`, `enter merge` and `in merge`: once merged, `set {$}/status done`.
- **`plan-review`**: a plan is written, proven and reviewed before code.
  - `plan`, `enter plan` and `in plan`: core first, prove each fact against
    the real system, record with `add {$}/plan/revisions -`, hand it to a
    reviewer with a fresh context.
  - `brief`, `read */plan`: the reviewer's brief: load the laws, judge the
    whole mechanism at this revision, record one review with
    `add {$}/plan/reviews`, naming the revision in `of`.
  - `unreviewed`, `refused impl`: the plan has no passing review of its
    latest revision; have it reviewed, or revise it and have the new revision
    reviewed.
- **`capture`**: prepare the next loop.
  - `created`, `wrote */children`: the description says what and why and
    survives a refactor; done-when is checkable.
  - `finished`, `enter *` where `(stage=terminal)`: update the related
    tickets with what this work taught.

## Writing one

A project workflow `review` with two handlers, then one shipped handler
turned off without touching its neighbours:

```
set /workflows/review '{summary:"what a review is in this repository"}'
set /workflows/review/handlers/entered '{on:[enter review, in review] body:-}' <<'EOT'
{$} is in review. Run the review cycle on its PR and push the fixes; move it
with `set {$}/status merge` only when the final pass is clean.
EOT
set /workflows/review/handlers/not-yet '{on:[refused done] body:"{$} does not finish from {$/status}: it leaves through review. `set {$}/status review` first."}'

set /workflows/phases/handlers/review/enabled false
```

Each `set` echoes the record it wrote; the last echoes
`enabled true → false (shadows shipped)`.

## Inspection

`get /workflows` lists every resolved workflow in the node it is run from,
one line per workflow and per handler: name, on or off, the moments and
selector, then the layer that supplied it and, where it applies, `stale`,
`unchanged` or `base unknown`:

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

`get /workflows/plan-review/handlers/plan` prints the handler whole, with the
layer that supplied it, then the body. `get …/plan/layers` prints the raw
record at each layer, which is how the shipped text behind a stale override
is read. `--json` on any of these prints the same, keyed by path.

`--dry-run` on a write prints, after the diff and the derivations the write
would echo, the handlers that would fire, each body headed by its handler's
address.

## The guidance surfaces

**The managed block.** `lit2 admin init` writes a block into `AGENTS.md` and
`CLAUDE.md`, files lit2 does not own. Its content is one pointer carrying no
policy: work here is tracked by lit2, run `lit2` first and follow what it
prints. `lit2 admin` installs the block and the pre-push hook.

**The quickstart is a workflow.** Bare `lit2` in a workspace is `get /`: the
root prints its children, `lanes`, `columns`, `integrity`, `next`, and the
`workspace` line naming the version in force and whether the file matches it.
The handler bound `read /` prints after it. Help topics are handlers too,
bound `wrote */children`, `enter *` with `(stage=terminal)`,
`read /integrity` and `read */plan`.

The discovery path from the managed block is a walk through the tree:

1. Bare `lit2` runs `get /`, and its `read /` handler orients the agent.
2. `get /next` prints the one ticket this checkout should work and the
   guidance of that ticket's column, which says what the first move is.
3. `get /columns` prints every column, its forward edges, its requirements
   and its guidance.
4. `lit2 --help` is the verb table and the field table, generated from the
   source the parser is (`specifications/command-line.md`,
   "The table is the schema").

## Observability

The command's event records, under `handlers`:

- `moments`: the moments the command raised, `[]` when none.
- `fired`: per handler that fired, its address, the moment it matched, the
  layer that supplied it, and whether it is stale.
- `suppressed`: per handler that matched a moment and did not fire: address,
  moment, and one reason, `workflow-off@<layer>`, `handler-off@<layer>` or
  `where-unmatched`.

`moments: []` and `fired: []` are different facts and both are written
`[LAW:nothing-unseen]`.
