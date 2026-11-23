# Workspace schema: one declared configuration for levels, fields, columns, link types and templates

The hierarchy, the fields, the link types and the templates are
stated below; columns and their transitions are
[columns-and-phases.md](../columns-and-phases.md) and
[transition-requirements.md](../transition-requirements.md); how a version of
the file comes into force is [architecture.md](../architecture.md) §3.15.

## The shape in one paragraph

A workspace's domain model is one declared configuration, the **workspace
schema**: the hierarchy levels and their names, the fields each level
carries, the columns a work item moves through and the
requirements on each transition, the link types that may join items and at
which levels, and the template each level applies to a new item. An engine
interprets it; what code may know of it is in [architecture.md](../architecture.md).
What lit does out of the box is the
**default profile**, a data file in the same grammar that expresses lit's
discipline, and a workspace that wants `feature → story → task` with story
points edits data, not code. The schema is the workspace file,
`.lit/workspace.toml`, committed in the repository as the intent; the version in
force is the one applied into the log with `lit2 config commit`
([architecture.md](../architecture.md) §3.15). It is read with the four verbs of
the [path-addressed CLI](../path-addressed-cli.md), and a verb write to a schema
node edits the working-tree draft.

## In an administrator's vocabulary

| Jira / Azure DevOps term | In lit v2 | Owned by |
|---|---|---|
| issue type, work item type | a hierarchy level: roles fixed in code, names in configuration | this document |
| issue type hierarchy | the levels and exact containment | this document |
| field, field configuration | a field declared per level with a kind | this document |
| required field | an entry or exit requirement on a column that names the field | [transition-requirements.md](../transition-requirements.md) |
| status, status category | a column and its stage | [columns-and-phases.md](../columns-and-phases.md) |
| workflow (statuses and transitions), conditions and validators | columns with entry and exit requirements | [transition-requirements.md](../transition-requirements.md) |
| issue link type (Blocks, Relates) | a link type with a kind and the level pairs it may join | this document |
| work item template (Azure DevOps), description template (GitLab) | a creation template per level | this document |
| the schemes assigned to a project | the default profile | this document |
| automation rule | a workflow: a named unit of handlers | [workflows.md](../workflows.md) |

## The schema as nodes

The schema is a set of root nodes. Each has an owner; this document owns the
container, the levels' names, the link types and the templates.

```
/levels/1                 name:story        fields:{…}   template:{…}
/levels/2                 name:feature      home:unfiled     fields:{…}   template:{…}
/levels/3                 name:epic         home:unassigned  fields:{…}   template:{…}
/levels/<n>/fields/<f>    kind, parameters                   this document
/levels/<n>/template      defaults applied to a new item     this document
/links/blocks             kind:dependency   between:[[1,1],[2,2]]
/links/relates            kind:relates      between:[[1,1],[1,2],…,[3,3]]
(columns and transitions)  node named by columns-and-phases.md
```

A write to one of these nodes goes through the path parser and edits the
working-tree draft of the workspace file, saying so in the echo; the draft is in
force once it is committed and applied, and the apply runs the schema parse
below over the whole document. Reads print paths, so what is read is what is
typed:

```
get /levels                                   the hierarchy as configured
set /levels/2/name feature                    rename level 2
set /levels/1/fields/points '{kind:enum values:[1,2,3,5,8]}'
set /links/blocks/between '[[1,1],[2,2]]'
set /levels/1/template '{priority:normal}'
rm /links/tests
get /levels /links --json                     the schema as one object keyed by path
```

## Hierarchy

Levels are numbered from 1 at the bottom, roles fixed in code, names in
configuration; the default profile declares three, `story`, `feature`, `epic`.
Level 1 is the leaf and every level above it is a node
([columns-and-phases.md](../columns-and-phases.md)). Containment is exact: a parent
is exactly one level up, and every node level names a synthetic home (`unfiled` at
2, `unassigned` at 3 by default) that an item created without a parent lands in.
Order is the lane's ([path-addressed-cli.md](../path-addressed-cli.md)). Kinds of
work such as bug and chore are labels.

A level is addressed by its number and its name is display, so
`set /levels/2/name theme` renames without a migration and `(level=2)`
selects the same items under any name.

## Fields

A ticket's fields are the field table's rows: the built-in fields the engine
exposes and the fields the workspace file declares per level. The table gives
each field its type, whether it is settable or derived, what `set` accepts,
its history and its `--help` line, so a field cannot be written without being
recorded.

A field is declared on a level, under that level's `fields` table:

```toml
[levels.2.fields.target_release]
kind = "pattern"
pattern = '^\d+\.\d+$'

[levels.1.fields.points]
kind = "enum"
values = ["1", "2", "3", "5", "8"]
```

A field has a name, a kind, and the kind's parameters. The kinds are
`text`, `number`, `date`, `enum` with its `values`, `pattern` with its
regular expression. A field declared on level 2 exists on epics and not on
tickets; a name is unique within a level, and the same name on two levels is
two fields. A built-in field exists on every level with its built-in kind and
cannot be redeclared with another. Prose-shaped custom fields are not
offered: `body` and `prompt` are the prose fields, and every prose field is an
envelope ([architecture.md](../architecture.md) §3.19). Whether a field is
required, and before which transition, is a requirement on a column
([transition-requirements.md](../transition-requirements.md)).

Raw text crosses the parse at `set` and `add` and comes back as a typed value or
a refusal naming the field, the kind and the offending input; the store persists
only what passed `[LAW:parse-dont-validate]`. The definitions the parse reads are
the active version. The value recorded is the canonical encoding of the
parsed value: a number as its decimal text, a date as `YYYY-MM-DD`, an enum
member and a pattern match as given. A field set is an event and the fold
holds the current value; `get x/history` shows it under the field's declared
name, `points: - → 5`, as it shows `priority: normal → urgent`.

`set x/points 5` writes a declared field; a name not declared on the item's
level is refused, listing the names that are. `get x` prints every declared
field that has a value, one path per line; `get x/points` returns the value
bare, so the round trip into `set` works for custom names as for built-in
ones; a selector reads a custom field like any other, `(points=8)`; bulk
filing takes the fields as keys of each item.

Declarations change with the file. Removing a field does not delete its
values: they stay in the fold and `get x` prints them marked undeclared, so
nothing a person typed vanishes because a definition changed
`[LAW:nothing-unseen]`. Changing a kind leaves existing values as they are; a
value that no longer parses under the new kind is listed on `/integrity` with
the item, the field and the value, and is never rewritten.

## Columns and transitions

A column and a ticket's position are
[columns-and-phases.md](../columns-and-phases.md), and what Jira calls a status
category is the column's stage. What Jira calls conditions and validators are the
entry and exit requirements on a column
([transition-requirements.md](../transition-requirements.md)). A label is declared
in the workspace file and applied to an item or computed from a `where` selector
([transition-requirements.md](../transition-requirements.md)). This document does
not define a column or a requirement.

## Link types

A link type is a named node under `/links` with a `kind` and `between`, the
ordered `[from, to]` level pairs it may join. The engine knows two kinds:
`dependency`, directed and acyclic, read by `/next` and by the derived
`x/blocked_by`; and `relates`, a reference with no scheduling meaning. A
workspace adds a link type of either kind under a new name,
`set /links/tests '{kind:relates between:[[1,1]]}'`; a kind is a behavior, a
link type is an instance of one `[LAW:one-type-per-behavior]`.

**Legality is per level pair and explicit.** `add x/blocks y` is refused when
`[level(x), level(y)]` is not in `/links/blocks/between`, naming the pair and
the pairs that are. A dependency `add` that would close a cycle is refused naming
the cycle. Both are checked on the write, and a cycle that sync forms from two
offline adds is detected again after sync.

**The default profile joins same-level items with `blocks`.** `blocks` between
`[n,n]` for every declared level; `relates` between every pair. A dependency
connects prerequisites across lanes, and between leaves it says what a lane cannot.
A workspace that wants cross-level dependencies writes the pair into `between`.

**Removing a link type keeps its links.** `rm /links/<name>` refuses new links
of that name; existing ones stay, `get` prints them under the name marked
undeclared, and `/integrity` counts them, the stance Fields above takes for a
removed field `[LAW:nothing-unseen]`.

## Templates

A template is what Azure DevOps calls a work item template and GitLab a
description template: default values for a new item. It is an object at
`/levels/<n>/template` whose keys are settable fields declared on that level,
built-in or custom, and whose values are defaults. `add <parent>/children '{…}'` applies
the child level's template: each field absent from the value is filled from the
template and echoed as a derivation, and a field present in the value wins.

```
set /levels/1/template '{priority:normal labels:[needs-grooming]}'
add rin/children '{title:"Zone names match in any letter case"}'
+ rin.k3f
⇒ rin.k3f/priority = normal            /levels/1/template
⇒ rin.k3f/labels = [needs-grooming]     /levels/1/template
```

A template is not a rendering template.

The default profile ships every level's template empty. A required field that is
empty is visibly missing and holds the ticket at the initial column's exit
([transition-requirements.md](../transition-requirements.md)); a skeleton
reads as filled and defeats that signal.

## Applying a version

Apply names a committed blob and refuses a dirty or untracked file; a squash
merge changes no blob id, so how the file reached its branch is immaterial.

Two people who edit the file concurrently meet in a git merge, which is where
people already resolve concurrent edits to a file. An apply names the applies
it replaces by event id, as any register write names its parents. The live
applies are those no later apply names; the active version is the last live
one in total order. Live applies holding two or more blobs are contending, a
conflict value on the workspace in the fold, and one blob applied twice, as
two inits of the default are, is agreement. While they contend every command
runs on the active version, the board line and `config status` name each
contending blob and its writer, and the next `config commit` names every live
apply, which resolves them: git merges the file, lit merges which version is
in force. Before sync each writer sees only its own. The column-graph check
("Validating the schema") runs against every
live version, so a ticket moved into a column only the losing version declares
is protected too: a version that leaves a held column unresolved is refused
with the `aliases` line that says where its tickets go.

The file and the active version drift apart whenever one is edited or the
other applied; `config status` reports the drift, and whether the active blob
is in HEAD's history, and the board carries one line for it. The board
compares git's id for the working file's bytes with the active blob rather
than parsing the file.

## Validating the schema

**One parse, the whole document, at apply.** `lit2 config commit` parses the
version it applies into one typed value, so a cross-reference is caught
whichever key the edit touched: a link pair naming level 4, a template key
naming a field the level does not declare. A refusal names every fault with
its key path and applies nothing:

```
error (reason=schema): /links/blocks/between [1,4] names no level; nothing applied
```

The parse is a pure package with no I/O, and every command runs it over the
active version, so every checkout of the repository holds the same typed schema
`[LAW:parse-dont-validate]`.

Levels: 1 to n, each with a non-empty name, the names distinct, every level above 1
with a home name. Link types: a kind from the two, a non-empty `between`
whose entries name declared levels, names unique. Templates:
every key a field declared on that level, every value parsing under the field's
kind. Fields: a declared kind with its parameters, names unique within
the level, no built-in name redeclared. What a column and a requirement must
satisfy is [columns-and-phases.md](../columns-and-phases.md) and
[transition-requirements.md](../transition-requirements.md).

**A schema write never rewrites items.** Values and links a change leaves
undeclared stay, are shown as undeclared and are counted by `/integrity`; the
engine reports, a person decides. No user-supplied code runs in the fold, because
the fold that replays events must be deterministic
([event-store design](../event-store/design.md)).

## The default profile

The default profile is `internal/assets/workspace/default.toml`, and how
`lit2 admin init` applies it is in [event-store/design.md](../event-store/design.md).

**Acceptance.** A workspace administrator can rename a level, add or remove a
field or a link type, change the level pairs a link type joins, and change a
level's template with `set`, `add` and `rm` on the schema nodes, with no code
change.
