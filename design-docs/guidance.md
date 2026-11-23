# Guidance: what lit says to an agent, and when

Guidance is prose printed by lit at a moment in an agent's work, so that the agent
reads it at the moment it applies. Guidance is a workflow's handlers: lit defines
the moments, and the text at each is workspace data. Mechanism details are
maintained in [workflows.md](workflows.md), and a column's guidance is the handlers
bound to that column ([columns-and-phases.md](columns-and-phases.md)). This document
says what guidance is for and in what register the text is written.

## How an agent first meets lit

An agent learns that lit exists from the managed block lit writes into `AGENTS.md`
and `CLAUDE.md`. That block is short and names one entry point. Everything else is
reachable from the entry point by reading paths.

The discovery path from there is a walk through the tree:

1. The entry point, a bare `lit2`, runs `get /`, and its `read /` handler orients
   the agent: what lit is, the four verbs, and where the order of work is read:
   `get /next`.
2. `get /next` prints the one ticket this checkout should work, and the guidance
   of that ticket's column, which says what the first move is.
3. The workflow itself is a node, so an agent that wants the whole shape reads
   `get /columns` and sees every column, its forward edges, its requirements, and
   its guidance.
4. The `lit --help` table is generated from the same source as the parser
   ([path-addressed-cli.md](path-addressed-cli.md)).

## The loop lit drives

lit is built around one loop, and guidance attaches to its steps:

1. **See the order.** `get /lanes` is the backlog in rank order: lanes, each a row of
   tickets worked in sequence.
2. **Get the one ticket.** `get /next` serves one pick ([work-claims.md](work-claims.md)).
3. **Take it.** The transition out of the initial column. It writes the claim, and
   the write echoes it.
4. **Work it through its columns.** Entering a column prints its guidance. A
   transition the column's requirements refuse is refused once, on the transition,
   naming what is missing ([transition-requirements.md](transition-requirements.md)).
5. **Finish it.** The transition into a terminal column.
6. **Be told what is next.** The echo of the finishing transition says what it
   derived, such as the lane's next head for this checkout.

Within a ticket, work tangles: an agent discovers a better approach
mid-implementation, which changes a sibling ticket, which makes a third
unnecessary. Guidance keys on the ticket's column, not on a numbered stage of a
fixed sequence.

## When lit speaks

Guidance is most useful at three kinds of moment:

- **Moments of capture**, when a ticket finishes, when an epic finishes, when a
  session ends. Guidance here asks for the deposit: the sibling that just became
  actionable or obsolete, the follow-up that surfaced, the pattern that recurred and
  wants to become a skill or a convention.
- **Moments of creation**, when a ticket or an epic is added. Guidance here asks the
  creator to invest while context is highest: what "done" means in terms the
  consumer can check, which concepts the work concerns, how it relates to its
  siblings. Prefer descriptions that survive a refactor of the code they concern:
  they state what and why, and leave the code to be read from the code.
- **Moments of pruning**, when work is closed without finishing. Guidance here asks
  why this was not the right work, because that answer is data for whoever next
  selects.

Shipped handlers avoid nudges to verify, plan, focus or think carefully; prompts
that ask the agent to halt and reason about adjacent state; restatement of
principles the agent's own instructions already carry; and text whose value is a
hypothesis nobody measured.

The shape of a good handler's text is: *you just did X; the next concrete thing the
system needs from you, while context is fresh, is Y.*

## What is not guidance

Two things look like guidance and belong elsewhere:

- **Invariants** are requirements, not prose. A ticket that may not leave a column
  until a verdict reads pass is a requirement on the transition, refused with the
  reason ([transition-requirements.md](transition-requirements.md)).
- **Derived state** is echoed, not advised. A write prints every derivation it
  triggered.

## The register

Two decisions govern how guidance text is written.

**Strength matches certainty.** Loud and blocking for an invariant that must never
break; quiet and advisory for a heuristic the agent may overrule with reason. The
loud end is a requirement that refuses the transition, and the quiet end is a
column's guidance. Guidance text itself is therefore always advisory, and uniform
maximum volume is its own kind of noise. ([project-intent.md](project-intent.md).)

**Declarative facts and consequences, not coercive imperatives.** Guidance states
what is true and what follows: "this write is idempotent and safe to repeat" rather
than "do NOT ask the user"; "a purge reaches every store lit syncs with and cannot
be undone" rather than "NEVER purge without permission." Text lit did not write, a
ticket body or a remote's error, is quoted as data and marked as such, never
rendered where it could read as lit's own instruction.
