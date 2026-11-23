# A path-addressed CLI

The path grammar, hierarchy, lanes and rank are stated here; the model the paths
address is [architecture.md](architecture.md).

## The model

Every node has a path. Four verbs operate on paths.

### Paths

A path is a node, then `/`, then a field, then a sub-id, as deep as the tree goes.
Ticket ids use `.` for children; `/` descends into a node's fields. A path
beginning with `/` is anchored at the workspace root.

```
dee                     the ticket
dee/title               scalar
dee/body                scalar
dee/status              scalar, validated against the column graph (below)
dee/labels              list
dee/comments            list; elements have system-minted ids
dee/comments/43s        one comment
dee/comments/(by=claude_64e3)   one session's comments: a selector filters any step
dee/blocks              list of ticket ids
dee/parent              scalar ticket id
dee/claim               scalar, the lane's holder, not settable
dee/stage               scalar, derived from the column
792a.dee/body           a child's field: `.` for the child, `/` for the field
/columns                the root's column graph
/lanes/datetime         a lane at the root
/lanes/dee              the one-element lane a ticket in no lane is
/integrity              a computed field on the root
```

The two separators do different jobs and cannot collide: `.` is part of an id and
only ever precedes a minted child id, `/` only ever precedes a field name. A ticket is
addressed by its own segment, as the ids above are written; how ids are minted and
rendered is in [event-store/design.md](event-store/design.md).

Selectors, globs, lists, and objects are made of shell metacharacters, so on a command
line they are single-quoted: `get '(status=backlog)/title'`, `set 'rin.*/priority' urgent`.

`get` prints exactly these paths, with `$` standing for the ticket, so what is read
is what is typed:

```
$ = template-unfiled-792a.dee
$/title         dateInZone accepts zone names in any letter case
$/lane          datetime
$/comments/43s  [claude_64e3…] Facts from PR #43 …
```

`[LAW:parse-dont-validate]`: a path is parsed once into a typed address (node kind,
field kind, scalar-or-list, settable-or-derived) and every verb takes the address, not
the string. An address to a derived field is not a valid target for `set`, so
`set dee/claim x` fails at parse, not inside `set`.

### Verbs

```
VERB  TARGET          DOES
get   path...         read. Many paths; shared ancestors printed once.
set   path value...   replace a scalar or a whole list. Pairs. Echoes old → new per path.
add   path value...   append to a list. Pairs. The system mints the id and echoes it.
rm    path...         delete a node: a ticket, a comment, a label, a dep.
```

Maintenance (backup, sync, upgrade and the rest) is under `lit admin`, and activating
configuration is `lit config commit`.

Global on every write: `--dry-run` prints the same diff the write would echo. It
changes no ticket and appends nothing to the synced log; the local command log may
record it. A write computes a description of its change and hands it to the edge;
dry-run is the edge declining to perform it.
Also on every write: `--reason "…"`, carried on every event the write mints and
printed in `get x/history` beside the change, so the why travels with the what.
On `get`: `--sort <path>` orders the matches by a scalar field, `--desc` reverses,
`--limit N` keeps the first N; without them matches print in backlog order.

`set` on a list replaces the list. `add` appends; a list value appends each element.
`set` echoes every element it removed, so a
replacing typo is visible in the echo and reversible from it; `--dry-run` shows it
before it happens.

A write with many paths is one write: a selector or glob expands once, before
anything is written, and if any path is refused nothing is written. The refusal names
the path, the reason, and says so: `error (reason=gate): … ; nothing written`.

`add` never takes an id for the element it creates. For comments and children the
system mints the id and echoes `+ $/comments/43s`.

Comment removal is `rm dee/comments/43s`; ticket deletion is
`rm dee`, a reversible tombstone that is `set dee/deleted true`, and
`set dee/deleted false` restores it; `rm dee --purge` rewrites it out of the
history.

### Everything else is a path

```
set dee/status plan                     start
set dee/status impl                     phase --to impl
set dee/status done                     done
set dee/status '{column:dup of:4ou}'    close --resolution duplicate --of
set dee/status backlog                  open
set dee/deleted false                   restore
add /lanes/datetime/claim               start --take
set dee/parent rin                      parent set
add dee/blocks 4ou                      dep add
add rin/children '{title:…}'            new
add rin/children '{title:… children:[{title:…}]}'  new, with its own children
add /children '{title:…}'               new, top level
add dee/plan/reviews '{of:3 how:"…" result:"…" response:"…" verdict:fail}'  verdict, naming the revision it judged
get dee/plan/revisions/3                history --version plan@3
get '(stage=active updated<-6h)'        orphaned
get /lanes                              backlog
get /next                               next
get rin/next                            next, over the lanes whose head is under rin
get /integrity                          doctor
lit2 admin export                       export
lit2 admin import                       import
```

`next` and `backlog` are derived nodes on the root, like `/integrity`: `/lanes`
read in rank order *is* the backlog, and `/next` is the head the lane rule in
[work-claims.md](work-claims.md) selects for this checkout.

A long text field (`body`, `prompt`) is a node. `dee/plan` is a reviewable: two
append-only lists, `dee/plan/revisions`, each a text, and `dee/plan/reviews`, each
naming the revision it judged in `of`, which is required. `dee/plan` reads the latest revision with its number,
`dee/plan/revisions/3` reads one. `dee/plan/verdict` is derived: the verdict of the
last review naming the latest revision, or nothing. Appending a revision empties the
derived verdict until a review names it.

Values: a bare word, a `"quoted string"` in which `""` is a literal `"` and every
other character is itself, a newline or a backslash included, so a regex or a path
passes through the shell and lit untouched and doubling is the one escape idiom
beside `@@`, `-` for
stdin (once per command; for a list path, one element per line, so a `get` of ids
pipes in), `[a, b]` for a list, `{k:v k:v}` for an object, `@id` for the
addressed node's own id, and inside one `add`, `@N` for the Nth object the value
declares in document order, so `'[{title:A}, {title:B blocks:[@1]}]'` files two
tickets with the edge between them in one call. In prose, `@` before an id or `@N`
is a reference: stored as `@<segments>`, shown as `@` plus the item's current name,
so `"see @2 for the API shape"` in a batch reads `see @lit-ids-1f9.zc7 …` after a
rename and survives being read, edited and set back. `@@` is a literal `@`; any
other `@` is itself, so an email or a decorator needs no escape. A selector or
glob is never a value.

### The column graph is a node

The graph lives at `/columns` and is read with the same
verbs; a `set` on it edits the working-tree `.lit/workspace.toml`, and the edit is
in force once `lit config commit` applies it. The default graph ships in
`internal/assets/workspace/default.toml`, and columns, start, done and stage are
defined in [columns-and-phases.md](columns-and-phases.md).

What follows from `set x/status <s>`, validated against the resolved column graph, is
defined where each lives: the claim on the ticket's lane in
[work-claims.md](work-claims.md), requirements in
[transition-requirements.md](transition-requirements.md), and guidance, the handlers
bound to the column, in [workflows.md](workflows.md).

### Hierarchy: nearest ancestor wins

Any node may carry configuration, such as `columns`, `lanes` or `claim_ttl`, that
its descendants resolve by walking up. Columns resolve as in
[transition-requirements.md](transition-requirements.md) and handlers as in
[workflows.md](workflows.md):

```
/columns                        default for everything
rin/columns/audit               this epic adds an 'audit' column before impl
```

`get '(status=review)'` matches review tickets at every depth, because the selector
matches the ticket's resolved column name.

## Lanes

**A lane is an ordered list node, and it is cross-cutting.** It lives anywhere (the
root, an epic, any node) and references tickets anywhere. Order is the sequence.
A ticket belongs to at most one lane, so adding it to another moves it, and the echo
shows the removal from the old one; its `lane` field is derived from membership.
Creating a lane at an existing (node, name) is idempotent, so concurrent creation
yields one lane.

```
/lanes/datetime  = [dee, b64, fw9]
b64/blocked_by   = dee                 derived from position; never stored
```

**A ticket in no lane is its own lane,** at `/lanes/<id>`, so it has a rank path like
any other. Serial is one bulk add of ids:

```
add /lanes/datetime '[dee, b64, fw9]'
```

A new lane ranks last.

**Rank is on lanes, not tickets.** A ticket has no rank of its own; it has a position
in its lane, and the lane has a rank. Rank is written as a relation, `set /lanes/datetime/rank '{above: /lanes/help}'`,
`'{below: …}'`, `top` or `bottom`, so two checkouts reranking offline each still mean
what they said when the logs merge. An intent whose anchor no longer exists when it
arrives places the subject at the bottom and is named on `/integrity`. Reordering *within* a lane is one edit on the
list — `set /lanes/datetime '[b64, dee, fw9]'` — echoed as a diff, not N per-ticket
moves.

**Dependencies are the cross-cut.** A lane says "these, in this order." A dep
says "that thing, over there, first." A dependency may connect two tickets in one epic.

**Epics are folders.** An epic worked in sequence is a folder holding one lane. An
epic with parallel streams holds several.

**Reading a lane.** `get /lanes/datetime` prints the members in order, one per line,
with each member's status and claim, so an agent standing in a lane sees the whole
row and where it is in it:

```
/lanes/datetime  rank 4   claim this checkout
  dee   impl
  b64   backlog
  fw9   backlog
```

### What `next` serves

`/next` serves by the lane rule in [work-claims.md](work-claims.md). Reranking never
changes who holds a lane.

**Urgent is a priority.** A P0 carries `priority: urgent`; how `/next` serves an urgent
head is in [work-claims.md](work-claims.md).

## Bulk

Bulk edit is almost more common than single edit, comments aside. It falls out of
paths in three escalating forms:

1. **Many paths.** `set dee/priority urgent b64/priority urgent`. Fine for a handful.
2. **Globs.** `add 'rin.*/labels' decompose` — every child of the epic. `get '*/title'`
   across the backlog. `set 'unfiled.*/parent' @id` would mean the node's own id; `@id`
   is the sigil for the node's own id. `rm /lanes/unfiled` dissolves the lane and every member becomes its own.
3. **Selectors.** `set '(status=backlog labels=bug)/priority' urgent`. A query in parens
   expands to ids and the path continues. `--dry-run` shows the expanded list before
   it commits. A selector may read through `parent`:
   `(stage=terminal parent/stage=terminal)` matches tickets whose epic is finished.

## Output

`[LAW:nothing-unseen]` and the CLI binding on stdout semantics: output is a contract.

- Every write echoes the diff it made, one line per path: `dee/status backlog → plan`.
- Every derivation a write triggers is echoed with it: `⇒ dee/claim = this checkout`.
- `--dry-run` prints the expanded selector or glob first, then the same lines the
  write would echo, then `(dry run: nothing written)`.
- `get` on one scalar prints the bare value, so it pipes. `get` on many paths or a
  glob prints `path<TAB>value`, one per line, so it can be grepped and fed back to
  `set`; a value holding a newline, a tab, a quote or an edge space prints as a
  quoted string, `""` for a quote and everything else verbatim, and every other
  value bare, so a line splits on its first tab, a quoted value runs to its closing
  quote across as many lines as the body has, and the round trip holds for a body
  with no backslash anywhere for the shell to eat. The read–edit–write round trip
  works without JSON; `--json` prints one object keyed by path.
- `get` prints each shared ancestor once.

## The table is the schema

The verb table is the source the parser, the help text, the completions, and the
quickstart's command section are generated from. What is left for prose is workflow
advice — when to transition, what a good body looks like — and that lives in handlers
([workflows.md](workflows.md)).

Help also lists every field from the field registry
([workspace-schema/design.md](workspace-schema/design.md)): its type, whether it is
settable or derived, and what `set` validates.

## What is removed

lit v1 commands, and where each went:

| was | is |
|---|---|
| `start`, `done`, `close`, `open`, `phase` | `set x/status` |
| `verdict --plan <digest> --law …` | `add x/plan/reviews '{of:N …}'`; the review names the revision it judged |
| `history`, `history --version plan@N` | `get x/history`, `get x/plan/revisions/N` |
| `ls --search`, `search` | `get '(text~"…")'` |
| `quickstart <stage>`, `workflows show` | the handlers bound to the column, `get x/columns` |
| `show --field body`, `--body-file -` | `get x/body`, a `-` value reads stdin |
| `followup --on x` | `add <x's parent>/children {…}` |
| `start --take` | `add <lane>/claim` |
| `comment add` / `comment rm` | `add x/comments` / `rm x/comments/<id>` |
| `label add` / `label rm` | `add x/labels` / `rm x/labels/<name>` |
| `parent set` | `set x/parent` |
| `dep add --from --to --type blocks` | `add x/blocks` |
| `rank --top/--above` | `set /lanes/<lane>/rank top`, `'{above: <lane>}'` (rank is on lanes) |
| `new`, `followup` | `add <parent>/children '{…}'` |
| `children` | `get x/children` |
| `orphaned`, `backlog`, `next`, `ls` | `get '(selector)'` |
| `doctor`, `doctor --fix` | `get /integrity` |
| `workflows edit` | `set /columns/…` edits `.lit/workspace.toml`; `lit config commit` applies it |
| `delete`, `restore` | `rm x`, a reversible tombstone; `set x/deleted false` |
| `archive` | a terminal column: `set x/status '{column:obsolete reason:"…"}'` |
| `purge` | `rm x --purge` |
| `update --lane` | lane membership; `x/lane` is derived |
| `init`, `init --prefix` | `lit admin init`; the prefix is a line in `.lit/workspace.toml` |
