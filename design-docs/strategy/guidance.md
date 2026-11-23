# Guidance

What lit says to an agent, when, and in what register.

## Diagnosis

The moments when the next loop could be made cheaper (project-intent.md,
"Feedback over control"), when a ticket is created, finished or abandoned, pass
unremarked unless something speaks at them. But text that speaks too often or
too loudly is noise: nudges to be careful, restated principles, imperatives
in capitals. An agent learns to discount them, and they crowd out the line
that mattered. Text lit did not write, a ticket body or a remote's error, can
also be mistaken for lit's own instruction.

## Guiding policy

Guidance is prose lit prints at a moment in an agent's work, so the agent
reads it when it applies. lit defines the moments; the text at each is
workspace data, carried by handlers (`strategy/process.md`). Guidance is
always advisory: an invariant is a requirement, refused with its reason, and
derived state is echoed, not advised. Guidance states facts and their
consequences rather than coercive imperatives.

## Actions

**One entry point.** The managed block lit writes into the agent instruction
files carries no policy; it says work here is tracked by lit, run `lit` first
and follow what it prints. Everything else is reachable from that entry point
by reading paths: the orientation names where the order of work is read, the
pick prints its column's guidance, and the whole process is a node an agent
can read.
Serves: Operations; project-intent.md, "Feedback over control".
Spec: `specifications/workflows.md`, "The guidance surfaces".

**Guidance keys on the ticket's column, not on a fixed sequence.** Within a
ticket work tangles, so what lit says follows where the ticket is, not a
numbered stage.
Serves: Operations.
Spec: `specifications/workflows.md`.

**Speak where the next loop is cheapest to improve.** Guidance is most useful
at three kinds of moment. At *capture*, when a ticket or an epic finishes or a
session ends, it asks for the deposit: the sibling that became actionable or
obsolete, the follow-up that surfaced, the pattern that recurred. At
*creation*, it asks the creator to invest while context is highest: what done
means in terms the consumer can check, what the work concerns, how it relates
to its siblings, in a description that states what and why and survives a
refactor of the code. At *pruning*, when work closes without finishing, it
asks why this was not the right work, because that answer is data for
whoever next selects.
Serves: Operations; project-intent.md, "Nudge, don't shove — and never break
the system's own behavior".
Spec: `specifications/workflows.md`, "Shipped workflows".

**A good handler names the next concrete thing.** Its shape is: you just did
X; the next concrete thing the system needs from you, while context is fresh,
is Y. Shipped handlers avoid nudges to verify, plan, focus or think
carefully; prompts that ask the agent to halt and reason about adjacent
state; restatements of principles the agent's own instructions already carry;
and text whose value is a hypothesis nobody measured.
Serves: project-intent.md, "Nudge, don't shove — and never break the system's
own behavior".

**Foreign text is marked as data.** Text lit did not write is quoted and
marked as such, never rendered where it could read as lit's own instruction.
Serves: Operations.
