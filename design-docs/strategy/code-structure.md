# Code structure and testing

How lit's code is shaped so the design stays small, and how it is proven.

## Diagnosis

Every concern a tracker has, storage, signatures, the model, the process,
rendering, pulls toward the command that first needs it, and a codebase built
that way grows a special case per command and a mode per feature. The design
depends on determinism, which any stray clock read, entropy source or I/O in
the wrong layer quietly breaks. And a suite that needs a real repository and a
real remote for every assertion is too slow to run on every change.

## Guiding policy

Every verb runs one pipeline, and the verb and its paths are values flowing
through it. The steps between opening the store and performing the write are
pure functions of events, clock, entropy, arguments and writer; effects
happen at the boundaries. The code is a stack of units, each importing only
units below it, and each third-party module is confined to one unit. Tests
assert what an agent or a person can rely on after a command, in the
vocabulary of the design, not the shape of the code.

## Actions

**Units downhill.** The import order is declared once, and no unit imports
one above it, including by an edge that closes no cycle.
Serves: Complexity.
Spec: `specifications/code-layout.md`.

**One composition root.** The store is opened, and the clock, the writer
identity, the keys and the agent label are read, in one place, and handed down
as values; the clock is read once per command and handed down as `now`.
Serves: Data, Complexity.
Spec: `specifications/code-layout.md`.

**Closed sets are types.** Sealed interfaces and enumerated kinds make a
missing case in a switch a lint failure, so adding a kind shows every place
that must handle it.
Serves: Complexity.
Spec: `specifications/code-layout.md`.

**How lit grows.** A new ticket fact that no event kind records is an event
kind plus a fold rule and, if it needs the clock, an
evaluate rule. What a new verb or a new prose field owes access control is in
`strategy/authority-and-visibility.md`.
Serves: Complexity; project-intent.md, "Flexibility is structural, not
anticipatory".
Spec: `specifications/code-layout.md`.

**Dependencies are named before they are taken.** Each module outside the
standard library is listed with the one unit that may import it, every one
permissively licensed. Prior art under a non-permissive license is
reimplemented from its behavior, never copied.
Serves: Operational, Complexity.
Spec: `specifications/code-layout.md`.

**Most behavior is tested in process.** The layers from events through verbs
are pure and are tested over the in-memory stack: given these events and this
command, this is refused for this reason, or these intents are minted and
this state results. Printed output is asserted where an agent relies on it.
Determinism is tested as agreement between independent folds, through the
state's own equality. Acceptance tests walk the situations an agent meets over
the root's twin, each ending in what the agent sees and can do next. One
end-to-end smoke per release runs a real repository, a real remote, two
clones and the background sync.
Serves: Complexity, Data.
Spec: `specifications/code-layout.md`, "Testing".
