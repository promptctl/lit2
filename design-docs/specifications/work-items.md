# Work items and the tree

Carries out `strategy/process.md` and `strategy/configuration.md`.

## Levels and the tree

A **work item** is a node of the tree at a **level**. The workspace file
declares the levels, numbered from 1 at the bottom, each with a name. An
item at level 1 is a **leaf**; an
item above level 1 is a **node**, whose `stage` and `rollup` derive from its
children (`specifications/columns-and-requirements.md`, "The graph and
stage").

A level is addressed by its number and its name is display. Levels are
numbered 1 to n, each with a non-empty name, the names distinct.

The workspace file's schema is read and written with the four verbs as root
nodes:

```
/levels/<n>               name, home, fields, template
/levels/<n>/fields/<f>    kind, parameters
/levels/<n>/template      defaults applied to a new item
/links/<name>             kind, between
/columns                  the column graph
/workflows/<name>         a project workflow; <node>/workflows/<name> on a node
```

```
get /levels                                   the hierarchy as configured
set /levels/2/name feature                    rename level 2
set /levels/1/fields/points '{kind:enum values:[1,2,3,5,8]}'
set /links/blocks/between '[[1,1],[2,2]]'
set /levels/1/template '{priority:normal}'
rm /links/tests
get /levels /links --json                     the schema as one object keyed by path
```

**Containment is exact.** A parent is exactly one level up. Every level
above 1 names a synthetic **home**, and an item created without a parent
lands in its level's home: `add /children '{level: 2, title:"…"}'` echoes
`⇒ <id>/parent = unfiled`. A one-level workspace is a flat list of leaves
under the root.

**Filing.** `add <parent>/children '{…}'`, or a list of objects, creates
items at the level below the parent; a value may nest `children`.

Deletion is `rm x`, a reversible tombstone, and `rm x --purge`, a rewrite
(`specifications/command-line.md`, "Verbs").

## Templates

A template is the default values for a new item: an object at
`/levels/<n>/template` whose keys are settable fields declared on that level,
built-in or custom. `add <parent>/children '{…}'` applies the child level's
template: each field absent from the value is filled from the template and
echoed as a derivation, and a field present in the value wins. Every value
in a template parses under its field's kind.

```
set /levels/1/template '{priority:normal labels:[needs-grooming]}'
add rin/children '{title:"Zone names match in any letter case"}'
+ rin.k3f
⇒ rin.k3f/priority = normal            /levels/1/template
⇒ rin.k3f/labels = [needs-grooming]     /levels/1/template
```

The default profile ships every level's template empty. A required field
that is empty is visibly missing and holds the ticket at the initial column's
exit; a skeleton reads as filled and defeats that signal.

## Fields

A ticket's fields are the rows of the field table
(`specifications/command-line.md`, "The table is the schema"): the built-in
fields the engine exposes and the fields the workspace file declares per
level.

A declared field has a name, a kind and the kind's parameters:

```toml
[levels.2.fields.target_release]
kind = "pattern"
pattern = '^\d+\.\d+$'

[levels.1.fields.points]
kind = "enum"
values = ["1", "2", "3", "5", "8"]
```

The kinds are `text`, `number`, `date`, `enum` with its `values`, and
`pattern` with its regular expression. A field declared on level 2 exists on
level-2 items and not on leaves. A name is unique within a level, and the
same name on two levels is two fields. A built-in field exists on every level
with its built-in kind and cannot be redeclared with another. Custom fields
are not prose-shaped: the prose fields, every one an envelope, are listed in
`specifications/access.md`, "Tiers and envelopes". Whether a field is
required, and before which transition, is a requirement on a column
(`specifications/columns-and-requirements.md`).

Raw text crosses the parse at `set` and `add` and comes back as a typed value
or a refusal naming the field, the kind and the offending input; the store
persists only what passed. The definitions the parse reads are the active
version's. The value recorded is the canonical encoding of the parsed value:
a number as its decimal text, a date as `YYYY-MM-DD`, an enum member and a
pattern match as given. `get x/history` shows a field change under the
field's declared name, `points: - → 5`.

`set x/points 5` writes a declared field; a name not declared on the item's
level is refused, listing the names that are. `get x` prints every declared
field that has a value, one path per line; `get x/points` returns the value
bare, so the round trip into `set` works for custom names as for built-in
ones; a selector reads a custom field like any other, `(points=8)`; bulk
filing takes the fields as keys of each item.

Changing a kind leaves existing values as they are; a
value that no longer parses under the new kind is listed on `/integrity` with
the item, the field and the value.

**`topic`** is a built-in settable field: one short word that groups items
across the tree. The create supplies it, or lit2 derives one from the title
and echoes the derivation. A child's default is its parent's topic.
`(topic=cli)` selects on it, and it renders in the id
(`specifications/events-and-store.md`, "Ids").

**`priority`** is a field a ticket carries; `priority: urgent` is the urgent
interrupt `/next` serves (`specifications/lanes-and-claims.md`, "Routing").

## Links

A **link** is a typed edge between items. A link type is a named node under
`/links` with a `kind` and `between`, the ordered `[from, to]` level pairs it
may join. The engine knows two kinds: `dependency`, directed and acyclic, read
by `/next` and by the derived `blocked_by`; and `relates`, a reference with no
scheduling meaning. A link type's `between` is non-empty and names declared
levels, and link type names are unique. A workspace adds a link type of either
kind under a new name, `set /links/tests '{kind:relates between:[[1,1]]}'`; a
kind is a behavior and a link type is an instance of one. The default
profile's link types are in `internal/assets/workspace/default.toml`; its
`blocks` joins only same-level items, and a workspace that wants cross-level
dependencies writes the pair into `between`.

`add dee/blocks 4ou` says `dee` blocks `4ou`. Legality is per level pair and
explicit: `add x/blocks y` is refused when `[level(x), level(y)]` is not in
`/links/blocks/between`, naming the pair and the pairs that are. A dependency
`add` that would close a cycle is refused, naming the cycle. Both are checked
on the write, and a cycle that sync forms from two offline adds is detected
again after sync.

`blocked_by` and `blockers` derive from links and lane position together
(`specifications/columns-and-requirements.md`, "Derived fields a requirement
reads").

An edge to a deleted ticket is listed on `/integrity`.

## Labels

A **label** is a value of the built-in `labels` field, an enum that holds many
values. A label is declared in the workspace file's top-level `labels` list.
It is applied to an item, `add x/labels ready`, removed with
`rm x/labels/<name>`, or computed from a `where` selector, which evaluate
computes as it computes any derived field. It reads as `labels=<name>` in a
selector or a requirement. A test naming an undeclared label is refused at
parse. The engine reserves no label.

## Checklists

A leaf carries a **checklist**, `x/checklist`: items with a minted id, a text,
and one of three states, `pending`, `done`, or `abandoned` with an optional
reason. Checklist items are not work items: they have no level, lane, claim,
link or column. `done` and `abandoned` are final. An item is removed only by a
rewrite, so a finished leaf shows long after what was done, what was abandoned
and why.

- `add x/checklist '["…", "…"]'` mints items.
- `set x/checklist/<id>/state done` moves one.
- `set x/checklist '[…]'` replaces the list and the verb layer diffs it:
  items whose text it knows keep their id and state, new text is added, and
  vanished text is abandoned.

The fold merges per item, and a final state wins over `pending`. No event per
check enters history beyond the item events themselves; the leaf's
transitions carry the counts. The checklist exposes `checklist/pending`, its
count of pending items, so a column that requires `checklist/pending=0` on
entry refuses to finish a leaf with pending items and names them. The default
profile declares no such requirement. Item text is prose and an envelope; the
`set` diff matches items on the envelope's commitment.
