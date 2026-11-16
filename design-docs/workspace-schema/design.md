# Workspace schema: one declared configuration for levels, fields, columns, link types and templates

Status: draft, rewritten 2026-11-05 for lit v2 from the 2026-08-27 draft,
amended 2026-11-11 for configured levels and 2026-11-16 to take in the fields
design. The hierarchy, the fields, the link types and the templates are
stated below; columns and their transitions are
[columns-and-phases.md](../columns-and-phases.md),
[custom-columns.md](../custom-columns.md) and
[transition-requirements.md](../transition-requirements.md); how a version of
the file comes into force is [architecture.md](../architecture.md) §3.15.

## The shape in one paragraph

A workspace's domain model is one declared configuration, the **workspace
schema**: the hierarchy levels and their names, the fields each level
carries and which are required, the columns a work item moves through and the
requirements on each transition, the link types that may join items and at
which levels, and the template each level applies to a new item. An engine
interprets it and ships no vocabulary of its own: no level name, link type,
column or template is a constant in code. What lit does out of the box is the
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
| issue type, work item type | a hierarchy level: one to five, roles fixed in code, names in configuration | this document |
| issue type hierarchy | the levels and exact containment | this document |
| field, field configuration | a field declared per level with a kind | this document |
| required field | an entry or exit requirement on a column that names the field | [transition-requirements.md](../transition-requirements.md) |
| status, status category | a column and its stage; phase, column and workflow state are one concept | [columns-and-phases.md](../columns-and-phases.md), [custom-columns.md](../custom-columns.md) |
| workflow (statuses and transitions), conditions and validators | columns with entry and exit requirements | [transition-requirements.md](../transition-requirements.md) |
| issue link type (Blocks, Relates) | a link type with a kind and the level pairs it may join | this document |
| work item template (Azure DevOps), description template (GitLab) | a creation template per level | this document |
| the schemes assigned to a project | the default profile | this document |
| automation rule | a workflow: a named unit of handlers. In lit v2 the word means this, never a status graph | [workflows.md](../workflows.md) |

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
set /levels/2/name feature                    rename level 2; every reader prints the new name
set /levels/1/fields/points '{kind:enum values:[1,2,3,5,8]}'
set /links/blocks/between '[[1,1],[2,2]]'
set /levels/1/template '{priority:normal}'
rm /links/tests
get /levels /links --json                     the schema as one object keyed by path
```

## Hierarchy

One to five levels, numbered from 1 at the bottom, roles fixed in code,
names in configuration; the default profile declares three, `story`,
`feature`, `epic`. Level 1 is the leaf: the only level with a stored
status, a claim and a checklist, and the only level `/next` serves. Every
level above it is a node: no column of its own, only a stage and a rollup
derived from its children. Containment is exact: a parent is exactly one
level up, and every node level names a synthetic home (`unfiled` at 2,
`unassigned` at 3 by default) that an item created without a parent lands
in. The tree orders nothing; order is the lane's, and rank is on lanes only
([path-addressed-cli.md](../path-addressed-cli.md) §Lanes). v1's issue
`type` is retired: level replaces it, and bug, feature and chore are labels.

A level is addressed by its number and its name is display, so
`set /levels/2/name theme` renames without a migration and `(level=2)`
selects the same items under any name. The number of levels is a schema
edit like any other.

## Fields

A ticket's fields are the field table's rows: the built-in fields the engine
exposes and the fields the workspace file declares per level. The table gives
each field its type, whether it is settable or derived, what `set` accepts,
its history and its `--help` line, so a field cannot be written without being
recorded, and a field not in the table does not exist.

A field is declared on a level, under that level's `fields` table:

```toml
[levels.2.fields.target_release]
kind = "pattern"
pattern = '^\d+\.\d+$'

[levels.1.fields.points]
kind = "enum"
values = ["1", "2", "3", "5", "8"]
```

A field has a name, a kind, and the kind's parameters. The kinds are five:
`text`, `number`, `date`, `enum` with its `values`, `pattern` with its
regular expression. A field declared on level 2 exists on epics and not on
tickets; a name is unique within a level, and the same name on two levels is
two fields. A built-in field exists on every level with its built-in kind and
cannot be redeclared with another. Prose-shaped custom fields are not
offered: `body` and `prompt` are the prose fields, and every prose field is an
envelope ([architecture.md](../architecture.md) §3.19). Whether a field is
required, and before which transition, is a requirement on a column
([transition-requirements.md](../transition-requirements.md)), never a flag on
the field, so required never means required to be created.

The parse is one boundary. Raw text crosses it at `set` and `add` and comes
back as a typed value or a refusal naming the field, the kind and the
offending input; the store persists only what passed, and nothing downstream
re-checks `[LAW:parse-dont-validate]`. The definitions the parse reads are
the active version, so every checkout parses, renders and judges a value with
the same definitions. The value recorded is the canonical encoding of the
parsed value: a number as its decimal text, a date as `YYYY-MM-DD`, an enum
member and a pattern match as given. A field set is an event and the fold
holds the current value; `get x/history` shows it under the field's declared
name, `points: - → 5`, as it shows `priority: normal → urgent`.

`set x/points 5` writes a declared field; a name not declared on the item's
level is refused, listing the names that are. `get x` prints every declared
field that has a value, one path per line; `get x/points` returns the value
bare, so the round trip into `set` works for custom names as for built-in
ones; a selector reads a custom field like any other, `(points=8)`; bulk
filing takes the fields as keys of each item. The template below may name any
field declared on the level.

Declarations change with the file. Removing a field does not delete its
values: they stay in the fold and `get x` prints them marked undeclared, so
nothing a person typed vanishes because a definition changed
`[LAW:nothing-unseen]`. Changing a kind leaves existing values as they are; a
value that no longer parses under the new kind is listed on `/integrity` with
the item, the field and the value, and is never rewritten. Both are
advisories to a person, not repairs.

## Columns and transitions

The 2026-08-27 draft declared a status graph per type: statuses, each in one
of three status categories, and transitions with validators. That half is gone. A column is a name
in configuration, a ticket's column is the one stored fact about its position
while labels are declared in the workspace file and applied by people, and
what Jira calls a status category is the column's stage
([columns-and-phases.md](../columns-and-phases.md),
[custom-columns.md](../custom-columns.md)). What Jira calls conditions and
validators are the entry and exit requirements on a column
([transition-requirements.md](../transition-requirements.md)). This document
does not define a column or a requirement.

## Link types

A link type is a named node under `/links` with a `kind` and `between`, the
ordered `[from, to]` level pairs it may join. The engine knows two kinds and no
more: `dependency`, directed and acyclic, read by `/next` and by the derived
`x/blocked_by`; and `relates`, a reference with no scheduling meaning. A
workspace adds a link type of either kind under a new name,
`set /links/tests '{kind:relates between:[[1,1]]}'`, and cannot add a kind:
a kind is a behavior, a link type is an instance of one
`[LAW:one-type-per-behavior]`.

**Legality is per level pair and explicit.** `add x/blocks y` is refused when
`[level(x), level(y)]` is not in `/links/blocks/between`, naming the pair and
the pairs that are. There is no `any`; a link type that joins every pair lists
every pair. A dependency `add` that would close a cycle is refused naming the
cycle. Both checks run in the engine, once, on the write
`[LAW:single-enforcer]`.

**The default profile joins same-level items only.** `blocks` between
`[n,n]` for every declared level; `relates` between every pair. A node's
status is derived, so a dependency that crosses levels says nothing a
same-level one does not: "epic A before story B" is B's epic blocked by A,
or B in a lane behind A's leaves. "Only part of epic A before B" is a leaf
dependency from B to that part, which the path-addressed CLI admits; the
2026-08-27 draft's answer, split A, is no longer forced. A workspace that
wants cross-level dependencies writes the pair into `between`.

**Removing a link type keeps its links.** `rm /links/<name>` refuses new links
of that name; existing ones stay, `get` prints them under the name marked
undeclared, and `/integrity` counts them, the stance Fields above takes for a
removed field `[LAW:nothing-unseen]`.

## Templates

A template is what Azure DevOps calls a work item template and GitLab a
description template: default values for a new item. It is an object at
`/levels/<n>/template` whose keys are fields declared on that level, built-in
or custom, and whose values are defaults. `add <parent>/children '{…}'` applies
the child level's template: each field absent from the value is filled from the
template and echoed as a derivation, and a field present in the value wins.
There is no other input to a template, no variables and no logic.

```
set /levels/1/template '{priority:normal labels:[needs-grooming]}'
add rin/children '{title:"Zone names match in any letter case"}'
+ rin.k3f
⇒ rin.k3f/priority = normal            /levels/1/template
⇒ rin.k3f/labels = [needs-grooming]     /levels/1/template
```

A template is not a rendering template. `get` prints paths, by
[path-addressed-cli.md](../path-addressed-cli.md) §Output, and the guidance a
reader sees after an item lives on the column nodes. The 2026-08-27 draft's
per-type `show:` references are gone.

The default profile ships every level's template empty. A required field that is
empty is visibly missing and holds the ticket at the initial column's exit
([transition-requirements.md](../transition-requirements.md)); a skeleton
reads as filled and defeats that signal.

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
`[LAW:parse-dont-validate]`. Every command carries that value; nothing
downstream re-checks a name, a pair or a kind.

**What this document's parts must satisfy.** Levels: 1 to n with n at most
5, each with a non-empty name, the names distinct, every level above 1
with a home name. Link types: a kind from the two, a non-empty `between`
whose entries name declared levels, names unique. Templates:
every key a field declared on that level, every value parsing under the field's
kind. Fields: a kind from the five with its parameters, names unique within
the level, no built-in name redeclared. What a column and a requirement must
satisfy is [columns-and-phases.md](../columns-and-phases.md) and
[transition-requirements.md](../transition-requirements.md).

**A schema write never rewrites items.** Values and links a change leaves
undeclared stay, are shown as undeclared and are counted by `/integrity`; the
engine reports, a person decides. The schema holds data only: no script, no
hook, no user-supplied code runs inside the engine, because the fold that
replays events must be deterministic
([event-store design](../event-store/design.md)).

## The default profile

The default profile is one TOML file, `internal/assets/workspace/default.toml`,
embedded in the binary and read by the parser above and by nothing else.
`lit2 admin init` writes it to `.lit/workspace.toml` when no file exists and
applies the same bytes as version one, the first event of every store; an
existing file is applied instead.

```toml
[levels.1]
name = "story"

[levels.2]
name = "feature"
home = "unfiled"

[levels.3]
name = "epic"
home = "unassigned"

# [levels.<n>.fields.<name>]  Fields above; the default profile declares none

[links.blocks]
kind = "dependency"
between = [[1, 1], [2, 2], [3, 3]]

[links.relates]
kind = "relates"
between = [[1, 1], [1, 2], [1, 3], [2, 1], [2, 2], [2, 3], [3, 1], [3, 2], [3, 3]]

# columns and their requirements: columns-and-phases.md,
# transition-requirements.md
```

Templates are absent, which is empty. `get /levels /links` on a fresh store
prints this file as paths.

**Acceptance.** A workspace administrator can rename a level, add or remove a
field or a link type, change the level pairs a link type joins, and change a
level's template with `set`, `add` and `rm` on the schema nodes, with no code
change. The shipped defaults are that one TOML file read by the same parser, so
a line removed from the file is a behavior lit no longer has.

## What v1 did, the question it answered, and where v2 lands it

Behavioral compatibility with v1 is not a goal. Each v1 behavior answered a
question about making one autonomous agent work well; the question is binding,
the behavior is not.

| v1 behavior | The question it answered | Where v2 lands it |
|---|---|---|
| Agents flooded epics with `blocks` edges between siblings | How does an agent know ticket 3 follows ticket 2 without an edge per pair? | A lane is the sequence ([path-addressed-cli.md](../path-addressed-cli.md) §Lanes). `blocks` stays legal between leaves for the cross-cut a lane cannot say. |
| Epics were worked as a unit; agents should not cherry-pick across them | Where is the boundary of one piece of work? | The lane is the unit an agent commits to; `/next` keeps serving the lane this checkout holds, and a node worked as a unit is one lane. |
| Dependency edges between epics made sense; between tasks they were often noise | At which level does "finish A before starting B" carry information? | Link legality is declared per level pair under `/links`. The default joins same-level items at every level. |
| Epics had no status; state was the fold of their children | What does an agent do with an open but empty container? | Every node derives a stage and a rollup from its children, fixed in code. |
| `ready.required_fields`, name-only and readiness-only | How do half-described tickets stay out of the queue? | An exit requirement on the initial column, per field ([transition-requirements.md](../transition-requirements.md)). |
| Free-form `relates` edges | Tickets reference each other outside any dependency | The `relates` link type, legal between every level pair. |
| Issue `type`: task, bug, feature, chore, epic | Which items are containers, and what kind of work is this? | Level answers the first; labels answer the second. `type` is retired. |

## Decisions for the owner

1. **Default templates are empty.** Recommended, for the required-field signal
   above. The alternative ships a body skeleton on level 1, such as a
   done-when heading, and accepts that an untouched skeleton passes for a
   described ticket.
