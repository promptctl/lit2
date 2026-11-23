# The command line

How agents and people work a backlog through one small surface.

## Diagnosis

The primary user is an agent, which reads text, learns a tool from what the
tool prints, and pays for every command it has to memorize. A tracker's
surface tends to grow one command per feature, each with its own flags and
output shape, so every feature adds vocabulary and every output needs its own
parser. Bulk edits are almost as common as single ones, and a bulk edit that
half-lands or whose effect is invisible is worse than none. Under concurrent
writes, a command that says only "done" hides what it changed and what that
change set off.

## Guiding policy

Make everything a node with a path, and give the whole surface four verbs on
paths: `get`, `set`, `add`, `rm`. One table is the schema of the surface, and
the parser, the help, the completions and the quickstart's command section are
generated from it. Output is a contract: what is printed is what can be typed
back.

## Actions

**Paths parse once.** A path is parsed into a typed address that knows its
node, field, shape and whether it is settable, and every verb takes the
address, not the string. Writing a derived field fails at parse.
Serves: Operations, Complexity.
Spec: `specifications/command-line.md`.

**Tracker operations are paths, not commands.** Starting, closing, ranking,
taking a claim, linking and filing are writes to paths; `/next`, the backlog
and `/integrity` are derived nodes read with `get`. Maintenance sits under
`admin`, and activating configuration under `config`.
Serves: Operations, Complexity.
Spec: `specifications/command-line.md`.

**The verbs are the interface; the events are the memory.** One table
compiles each write into the intents it mints, so the surface can be shaped
for the reader while the events stay shaped for convergence
(`strategy/derivation.md`).
Serves: Data.
Spec: `specifications/events-and-store.md`, "Intent".

**One grammar for selecting.** A selector is the same atom set a requirement's
test uses, so a reader learns it once and a query, a bulk write and a column's
requirement read alike.
Serves: Operations, Complexity.
Spec: `specifications/command-line.md`.

**Bulk is one write.** Selectors and globs expand once, before anything runs;
if any path is refused nothing is written; a dry run shows the expansion and
the diff first and changes nothing in the synced log.
Serves: Operations.
Spec: `specifications/command-line.md`.

**Every write echoes what it did.** Each write prints the diff it made, path by
path, and every derivation it triggered, so a replacing typo is visible and
reversible from its own echo. A refusal names the path, the reason and
everything unmet at once, and says nothing was written, so an agent fixes it
in one pass. `--reason` travels with every event a write mints.
Serves: Operations; project-intent.md, "Feedback over control".
Spec: `specifications/command-line.md`.

**Text first, deterministic, round-trippable.** The default output is
human-readable text, because that is what a language model reads; it is
deterministic, a bare scalar pipes, and many paths print one per line in a
form `set` accepts back. Machine-readable output is secondary and keyed by
path.
Serves: Operations.
Spec: `specifications/command-line.md`.

**Names are display; ids are identity.** An id is opaque, and its rendering
adds the project prefix and the item's topic so a reader can tell items apart.
Renaming either rewrites nothing, and a reference in prose is stored by id so
it follows the rename.
Serves: Operations, Data.
Spec: `specifications/events-and-store.md`, "Ids".

**Nothing hides behind a flag.** Help lists every verb and every field with
its type, whether it is settable or derived, and what `set` accepts, so
nothing is reachable only by a flag the table does not show.
Serves: Operations.
Spec: `specifications/command-line.md`, "The table is the schema".

**Every invocation leaves a record.** Each command writes one local record of
what it expanded, judged, wrote, fired and how long it took, kept on the
machine and never synced, so what a command did can always be seen
afterwards.
Serves: Operations.
Spec: `specifications/code-layout.md`.
