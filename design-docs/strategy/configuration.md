# Configuration

How a workspace declares its own model, and how every machine agrees on the
model in force.

## Diagnosis

A workspace's levels, fields, columns, requirements, labels and link types
decide what a write may do and how a fact reads, so two machines that
disagree about them disagree about the backlog. Yet the model must be data a
person edits without a code change, and a change to it is a change to how the
team works, which deserves the same review as any change to the repository.
People edit it concurrently. A half-finished edit must not stop anyone's
commands. Every verifier must be able to read the model a write was judged
under, long after the file has moved on. And some settings, such as how a
machine syncs or a person's own workflows, are nobody else's business.

## Guiding policy

The model is one committed file: the intent, edited in an editor, versioned
and merged by git, reviewed however the team reviews any change. What is in
force is a version of that file applied into the store as an event that
carries its bytes, so the log alone says what was in force for every write.
What belongs in the file is decided by one test: a value is the workspace's
when two machines that disagree about it cannot both be right about the
backlog, and a person's own when they can.

## Actions

**The file is the intent; the applied version is the law.** A write to a
model path edits the working-tree draft and says so; committing it is the
person's act, and applying it is an explicit step. A draft is not in force
until applied, so a draft that does not parse stops no command, and every
verb refuses a store with no version in force.
Serves: Operations, Data.
Spec: `specifications/workspace-file.md`, "Applying a version".

**Every write names the version it ran under.** Each event carries the id of
the version its writer had in force, and the log carries that version's
bytes, so every verifier can read the model a write was judged under.
Serves: Data.
Spec: `specifications/events-and-store.md`, `specifications/workspace-file.md`.

**Git merges the file; lit merges which version is in force.** Concurrent
edits meet in a git merge, where people already resolve concurrent edits to a
file. Concurrent applies are a conflict value in the fold, visible on the
board, and the next apply that names them all resolves it.
Serves: Data, Operations.
Spec: `specifications/workspace-file.md`, "Applying a version".

**One document, one parse.** The model is one document rather than one table
per concern, so the parse has a single boundary, the applied version has one
id, and the person edits one file in a syntax they already know. The apply
parses the whole document and refuses, naming every fault with its key path,
so a cross-reference is caught whichever key an edit touched.
Serves: Complexity, Operations.
Spec: `specifications/workspace-file.md`, "Validation".

**The engine knows roles; the file supplies names.** Code knows level
numbers and the roles of a leaf and a node, never their names, so renaming a
level needs no migration and a selector on a level works under any name. The
default profile is a data file in the same grammar, applied at init, and a
workspace that wants a different hierarchy, fields or columns edits data.
Serves: Operations; project-intent.md, "Flexibility is structural, not
anticipatory".
Spec: `specifications/workspace-file.md`, "The default profile";
`specifications/work-items.md`.

**A schema change never rewrites items.** Values, links and labels a change
leaves undeclared stay, are shown as undeclared and are counted on
`/integrity`; the engine reports and a person decides. Nothing a person typed
vanishes because a definition changed.
Serves: Data.
Spec: `specifications/workspace-file.md`, `specifications/work-items.md`.

**Personal settings stay personal.** How a machine syncs and caches, and a
person's own layer of workflows, live in that person's configuration; the
records they produce are in the store either way.
Serves: Operations.
Spec: `specifications/workflows.md`.
