# The process: columns, requirements and workflows

How lit moves work through a process that each project, and each epic, defines
for itself.

## Diagnosis

Every team's process differs, and an epic often needs its own step that the
rest of the workspace does not. A status enum in code, with a switch per
transition and permission checks scattered per command, makes each of those a
code change. Status, phase, resolution and workflow state tend to become
separate axes that must be kept consistent with each other. The process also
changes while work is running, on machines that have not yet seen the change.
Some rules are invariants that must never break, and others are heuristics an
agent may overrule with reason; one mechanism at one volume serves neither.
And a ticket's position can be either stored or computed from rules, but not
both: if a stored move and a rule could both decide it, which one is right has
no answer.

## Guiding policy

The process is data a generic engine interprets. One concept, the
**column**, stands for status, phase and workflow state; a ticket's column is
stored, and everything else about it is derived. The engine reads only a
column's flags, its forward edges and its requirement lists. Invariants are
requirements, checked loudly at the transition write; heuristics are
handlers, which speak quietly at the moments they apply. Strength matches
certainty.

## Actions

**The engine ships no content.** No column, level or label name is in code.
What the engine reads from a column is its stage (initial, active, terminal)
and its edges, so start, done and the queue work under any names.
Serves: Operations; project-intent.md, "Flexibility is structural, not
anticipatory".
Spec: `specifications/columns-and-requirements.md`.

**A requirement reads a fact; someone writes the fact.** Anything a workspace
wants to require is a field, a label, a review or another written fact, or a
derived field the engine exposes, and a requirement reads it through the
selector grammar. A new need is a new fact or a new derived field, not a new
kind of rule.
Serves: Operations, Complexity.
Spec: `specifications/columns-and-requirements.md`.

**Requirements gate only the transition write.** The requirement gate is one
pure function, and the transition write is the only caller that acts on it.
A ticket whose requirements stop holding afterwards stays where it is, and
the facts are in its history and its derived fields. `held` and `/next`
report what the gate would say and enforce nothing.
Serves: Data, Operations.
Spec: `specifications/columns-and-requirements.md`.

**Each requirement sits on the side that owns its reason.** Exit says this
column's work is complete, whatever comes next; entry says the destination
needs this, whatever it came from.
Serves: Operations.
Spec: `specifications/columns-and-requirements.md`.

**Readiness is the initial column's exit.** Grooming is part of the
configurable process: what groomed means is declared, and a mark on an epic
can satisfy a requirement for every ticket under it, so one write grooms an
epic and a follow-up filed into it later passes at birth.
Serves: Operations.
Spec: `specifications/columns-and-requirements.md`.

**Review is a record, not a move.** A plan and a pull request are reviewables:
revisions, and reviews that each name the revision they judged. A failed
review leaves the ticket where it is and the review is the rework record, so
rework needs no column and no record of its own.
Serves: Operations.
Spec: `specifications/columns-and-requirements.md`.

**Nodes roll up; leaves move.** Only a leaf has a column. A container's stage
and rollup derive from its children, so finishing a container stores nothing.
Serves: Operations, Data.
Spec: `specifications/columns-and-requirements.md`.

**A process change is data on one node.** Any node may carry its own columns
and workflows, and descendants resolve by nearest ancestor, whole per column
and whole per handler, with nothing merged across levels. A move that would
leave a ticket in a column its new ancestors lack is refused.
Serves: Operations.
Spec: `specifications/columns-and-requirements.md`, `specifications/workflows.md`.

**Running work survives a process change.** A ticket left in a column no
version declares keeps its transition; it is reported as undeclared, passed
over by `/next` and listed on `/integrity`.
Serves: Data.
Spec: `specifications/columns-and-requirements.md`.

**One mechanism for everything the process says.** A workflow is a named set
of handlers a project adopts or declines as one thing; a handler binds moments
the engine observes, filtered by the CLI's own selector, to a body it runs at
those moments. A column's guidance, the orientation on a bare `lit`, and help
topics are all handlers. lit defines the moment kinds; what happens at each
moment is data.
Serves: Operations, Complexity.
Spec: `specifications/workflows.md`.

**Override without breaking what is built on it.** Workflows come in layers,
shipped, a person's own, and the project's, and the nearest record that
defines a handler supplies it whole. An override records what it shadowed, so
a shipped change under a project rewrite reads as stale rather than vanishing.
A handler bound to a column a workspace lacks is legal, does not fire, and the
configuration check reports it.
Serves: Operations.
Spec: `specifications/workflows.md`.

**Labels are declared, then applied or computed.** A label is workspace data,
applied by a write or computed from a selector; kinds of work such as bug and
chore are labels.
Serves: Operations.
Spec: `specifications/work-items.md`.
