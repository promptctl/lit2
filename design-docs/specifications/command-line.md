# Paths and the command line

Carries out `strategy/command-line.md`.

## Node and path

Every node has a path: an item, then `/` and a field, then a sub-id, as deep
as the tree goes. Item ids use `.` for children; `/` descends into a node's
fields. A path beginning with `/` is anchored at the workspace root. The two
separators cannot collide: `.` is part of an id and only ever precedes a
minted child id, `/` only ever precedes a field name. An item is addressed by
its own segment; how ids are minted, rendered and parsed is
`specifications/events-and-store.md`.

```
dee                     the ticket
dee/title               scalar
dee/body                scalar
dee/status              scalar, validated against the column graph
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
/lanes/dee              the one-member lane a ticket in no lane is
/integrity              a computed field on the root
```

### The root

Bare `lit2` is `get /`. The root prints its children, `lanes`, `columns`,
`integrity`, `next` and the `workspace` line, then the handlers bound
`read /`. Derived nodes on the root are read like any other path:

```
get /lanes                              the backlog: lanes in rank order
get /next                               the head this checkout should work
get rin/next                            the same walk, over the lanes whose head is under rin
get /integrity                          what the fold could not settle silently
get /me                                 this checkout, principal, agent label and tiers held
get /members                            the policy's members
get /columns                            the resolved column graph
get /workflows                          the resolved workflows
```

### Everything else is a path

Every tracker operation is a verb on a path:

```
set dee/status plan                     a transition; out of the initial column it starts the ticket
set dee/status '{column:dup of:4ou}'    a move carrying the fields its column's entry needs
set dee/deleted false                   restore
add /lanes/datetime/claim               take the lane
set dee/parent rin                      reparent
add dee/blocks 4ou                      a dependency
add rin/children '{title:…}'            file under rin, at the level below it
add rin/children '{title:… children:[{title:…}]}'  file with its own children
add /children '{title:…}'               file at the top level
get dee/history                         every change to dee, each beside its reason
get '(stage=active updated<-6h)'        in-flight work untouched for six hours
get '(text~"…")'                        search
```

Maintenance (init, sync, gc, export, import) is under `lit2 admin`, and
applying and inspecting configuration is `lit2 config commit` and
`lit2 config status`.

## Verbs

```
VERB  TARGET          DOES
get   path...         read. Many paths; shared ancestors printed once.
set   path value...   replace a scalar or a whole list. Pairs. Echoes old → new per path.
add   path value...   append to a list. Pairs. The system mints the id and echoes it.
rm    path...         delete a node: a ticket, a comment, a label, a dependency.
```

What each write mints is the verb-to-intent table in
`specifications/events-and-store.md`.

- `set` on a list replaces the list. `set` echoes every element it removed.
- `add` appends; a list value appends each element. `add` never takes an id
  for the element it creates: for comments and children the system mints
  the id and echoes `+ $/comments/43s`.
- `rm dee` is `set dee/deleted true`, a reversible tombstone, and
  `set dee/deleted false` restores it. `rm dee/comments/43s` is
  `set dee/comments/43s/deleted true`; `get dee/comments` still lists the
  comment, marked. A deleted item matches a selector only when the selector
  names `deleted`. `rm dee --purge` rewrites the item out of history.
- Undo is `set` back from the echo, or from a prose field's numbered
  versions: `get x/body/versions/N | set x/body -`.

Flags:

- `--reason "…"`, on every write: carried on every event the write mints, as
  an enveloped prose field, and printed in `get x/history` beside the
  change.
- On `get`: `--sort <path>` orders the matches by a scalar field, `--desc`
  reverses, `--limit N` keeps the first N. Without them matches print in
  backlog order.
- `--json`: see "Output".

### Argv

Argv is parsed with the standard library and a scan over the tables, the
verbs and each group's (`admin`, `config`): a flag is `--name` anywhere,
`--` ends the flags, `-` is a value.

## Values

The literal grammar, one for every value a verb takes:

- a bare word;
- a `"quoted string"`, in which `""` is a literal `"` and every other
  character is itself, a newline or a backslash included, so a regex or a
  path passes through the shell and lit2 untouched;
- `-` for stdin, once per command; for a list path, one element per line,
  so a `get` of ids pipes in;
- `[a, b]` for a list, `{k:v k:v}` for an object;
- `@id` for the addressed node's own id;
- inside one `add`, `@N` for the Nth object the value declares in document
  order, so `'[{title:A}, {title:B blocks:[@1]}]'` files two tickets with
  the edge between them in one call.

A selector or glob is never a value. Selectors, globs, lists and objects are
made of shell metacharacters, so on a command line they are single-quoted:
`get '(status=backlog)/title'`, `set 'rin.*/priority' urgent`. Unquoted in
zsh, they fail before lit2 runs.

### `@` references in prose

In prose, `@` before an id or `@N` is a reference: stored as `@<segments>`
and shown as `@` plus the item's current name, so `"see @2 for the API
shape"` in a batch reads `see @lit-ids-1f9.zc7 …` after a rename, and
survives being read, edited and set back. `@@` is a literal `@`; any other
`@` is itself, so an email or a decorator needs no escape. Doubling is the
one escape idiom: `""` in a quoted string, `@@` in prose.

## Selectors and globs

A **selector** is a query in parens that expands to ids, and the path
continues after it: `(status=backlog labels=bug)/priority`. It is a list of
atoms, space-separated, all of which must hold; the atoms and their verdicts
are `specifications/columns-and-requirements.md`, "The atoms". Below an item,
a selector filters its comments and history like any list:
`dee/comments/(by=claude_64e3)`.

A **glob** expands to the paths it matches: `rin.*` is every child of `rin`,
`*/title` every item's title. The same glob grammar binds handler hook events
(`specifications/workflows.md`).

## Bulk

A write with many paths is one write. It takes three escalating forms:

1. **Many paths.** `set dee/priority urgent b64/priority urgent`.
2. **Globs.** `add 'rin.*/labels' decompose`; `get '*/title'`;
   `set 'unfiled.*/parent' @id`.
3. **Selectors.** `set '(status=backlog labels=bug)/priority' urgent`.

A refusal of a bulk write lists the paths that would have landed. Bulk
filing is a list value on one `add`, each item's fields as keys.

## Output

Output is a contract `[LAW:nothing-unseen]`.

- `get` prints exactly the paths it read, with `$` standing for the ticket,
  so what is read is what is typed, and prints each shared ancestor once:

  ```
  $ = template-unfiled-792a.dee
  $/title         dateInZone accepts zone names in any letter case
  $/lane          datetime
  $/comments/43s  [claude_64e3…] Facts from PR #43 …
  ```

- `get` on one scalar prints the bare value, so it pipes. `get` on many
  paths or a glob prints `path<TAB>value`, one per line, so it can be
  grepped and fed back to `set`. A value holding a newline, a tab, a quote
  or an edge space prints as a quoted string, `""` for a quote and
  everything else verbatim; every other value prints bare. So a line splits
  on its first tab, a quoted value runs to its closing quote across as many
  lines as the body has, and the round trip holds for a body with no
  backslash anywhere for the shell to eat. The read–edit–write round trip
  works without JSON.
- Every write echoes the diff it made, one line per path:
  `dee/status backlog → plan`.
- Every derivation a write sets off is echoed with it:
  `⇒ dee/claim = this checkout`.
- `--dry-run` prints the expanded selector or glob first, then the same
  lines the write would echo, then `(dry run: nothing written)`. On
  `rm x --purge` it prints the subtree, each item's comment and edge counts,
  and any lane that will have no members.
- A refusal is `error (reason=<reason>): <path> …; nothing written`, then
  one line per cause.
- `--json` on one scalar prints the scalar; on many paths, one object keyed
  by path.

## The table is the schema

`lit2 --help` lists the verbs and every field from the field table: its
type, whether it is settable or derived, and what `set` validates.

Considered and rejected, because each makes the things around it expensive
to replace: a hand-written help text and a `status` subcommand; a `new`
subcommand with one flag per field; `show`, `ls`, `backlog` and `history` as
separate commands with their own output shapes; a `closed` flag and a
`close` command.
