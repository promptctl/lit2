# Custom fields, declared per level

Status: draft / proposal. Written 2026-10-27 for owner review. The ask, in the
owner's words: *"I have a user who wants to define a specific hierarchy and
that would include specific required fields that you would define on each
level of the hierarchy. The hierarchy part fits already. What we need is the
custom fields and required fields."* The hierarchy is the levels of
[workspace-schema/design.md](workspace-schema/design.md); this document is the
fields half of it. The workspace file that declares fields, and how a version
of it comes into force, is [architecture.md](architecture.md) §3.15.

## What a field is

A ticket's fields are the field table's rows: the built-in fields the engine
exposes and the fields the workspace file declares per level. The table is
what gives each field its type, whether it is settable or derived, what `set`
accepts, its history, and its `--help` line, so a field cannot be written
without being recorded, and a field not in the table does not exist.

"Required" is a requirement on a column, not a flag on a field. The shipped
default's `ready` check is the example: an exit requirement on the initial
column that keeps a ticket with no description, no `labels=ready`, or a
blocker out of the queue
([transition-requirements.md](transition-requirements.md)). A requirement's
test reaches every ticket its scope names, regardless of level, and `Parse`
refuses a test that names a path no level or built-in declares.

## Declaring a field

A field is declared on a level, in the workspace file, under that level's
`fields` table:

```toml
[levels.1]
name = "ticket"

[levels.2]
name = "epic"
home = "unfiled"

[levels.2.fields.target_release]
kind = "pattern"
pattern = '^\d+\.\d+$'

[levels.1.fields.points]
kind = "enum"
values = ["1", "2", "3", "5", "8"]
```

A field has a name, a kind, and the kind's parameters. The kinds are five:
`text`, `number`, `date`, `enum` with its values, `pattern` with its regular
expression. A built-in field exists on every level with its built-in kind and
cannot be redeclared with another.

A field declared on level 2 exists on epics and not on tickets. A name is
unique within a level; the same name on two levels is two fields. The parse
refuses a declaration that breaks any of this, naming the key, when the
version is applied.

## Storing a value

A field set is an event; the fold holds the current value. The value recorded
is the canonical encoding of the parsed value: a number as its decimal text, a
date as `YYYY-MM-DD`, an enum member and a pattern match as given.
`get x/history` shows a custom field set under the field's declared name,
`points: - → 5`, exactly as it shows `priority: normal → urgent`. Prose-shaped
custom fields are deliberately not offered; `body` and `prompt` exist for
prose, and every prose field is an envelope.

The parse is one boundary. Raw text crosses it at `set` and `add`, and comes
back as a typed value or a refusal that names the field, the kind and the
offending input. The store persists only what passed, and nothing downstream
re-checks `[LAW:parse-dont-validate]`. The definitions the parse reads are the
active workspace version, so every checkout parses, renders and judges a value
with the same definitions.

## Surfaces

`set x/points 5` writes a declared field; a name not declared on the item's
level is refused, listing the names that are. `get x` prints every declared
field that has a value, one path per line; `get x/points` returns the value
bare, so the round trip into `set` works for custom names as it does for
built-in ones. A selector reads a custom field like any other, `(points=8)`.
Bulk filing under `--json` takes the fields as keys of each item.

## Changing a declaration later

Declarations change with the workspace file, so a field can be added, moved
onto or off a requirement, and removed. Removing one does not delete values:
values of an undeclared field stay in the fold and `get x` prints them marked
undeclared, so nothing a person typed vanishes because a definition changed
`[LAW:nothing-unseen]`. Changing a kind leaves existing values as they are; a
value that no longer parses under the new kind is listed on `/integrity` with
the item, the field and the value, and is never rewritten. Both are
advisories to a human, not repairs.

## Scope

In: fields and their kinds, the parse boundary, storage, history, the
surfaces above. The hierarchy, link types and templates are
[workspace-schema/design.md](workspace-schema/design.md); columns and
requirements are [columns-and-phases.md](columns-and-phases.md) and
[transition-requirements.md](transition-requirements.md).

## Decisions for the owner

1. The five kinds and no sixth. A `user` or `issue` reference kind would want
   its own validation against the store and is left out until asked for.
