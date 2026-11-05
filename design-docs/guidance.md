# Guidance: what lit says to an agent, and when

Status: provisional, 2026-11-05.

This document consolidates three v1 designs, `agent-native-guidance-proposal.md`,
`preparing-the-next-loop.md` and `agent-enablement-onboarding.md`, into the one
statement of how an agent discovers lit v2 and how lit speaks to it, and it stands
until a v2 design supersedes it. The owner's ruling on the three: *"guidance docs: we
should have docs for all of these things, but they dont need to be these docs.
rewrite the content into single guidance file and label it 'provisional'"*.

The mechanism that produces guidance is specified in [workflows.md](workflows.md): a
workflow is a named unit of handlers, and a handler binds to a moment and produces
guidance. This document says what that guidance is for, which moments exist, and in
what register the text is written. It does not redesign the mechanism.

## What guidance is

Guidance is prose a human wrote, printed by lit at a moment in an agent's work, so
that the agent reads it at exactly the moment it applies. Nothing executes: no
script, no hook, no side effect. The text is the actuator, because the reader is an
agent and an agent acts on what it reads. That is the whole idea, and it is what
separates lit's workflows from a tracker's automation rules.

Guidance is data, not code. The set of moments at which lit can speak is small and
fixed, and the text spoken at each is a value that flows through them. A new
discipline, a project's own process, a different model's needs: each is met by
editing the data, never by a branch in lit. [project-intent.md](project-intent.md)
names this as the project's source of flexibility: make the intervention points a
stable substrate and make the intervention policy data. `[LAW:dataflow-not-control-flow]`.

In v2 the data lives in one place, a node in the workspace tree of
[path-addressed-cli.md](path-addressed-cli.md): a workflow's handlers. A handler
binds to a moment lit observes, entering or being in a column, a refused
transition, a write or a read on a path, and produces text; workflows are edited
with the same four verbs as everything else ([workflows.md](workflows.md)). A
column's guidance, the "what should I do now" prose that reading a ticket prints
after it, is derived from the handlers bound to that column, so it cannot
describe a column that does not exist `[LAW:one-source-of-truth]`
([columns-and-phases.md](columns-and-phases.md) settles that column, phase and
state are one concept, called a column).

Guidance prints after the data it belongs to, never interleaved with it, on the same
stream the agent is already reading. An agent that pipes a scalar gets the scalar.

## How an agent first meets lit

A fresh agent in a repository it has never seen learns that lit exists from the
managed block lit writes into `AGENTS.md` and `CLAUDE.md`. That block is short and
names one entry point. Everything else is reachable from that entry point by reading
paths; nothing has to be memorized from the block.

The discovery path from there is a walk through the tree:

1. The entry point says what lit is, the four verbs, and where the order of work is
   read: `get /next`.
2. `get /next` prints the one ticket this checkout should work, and after it the
   guidance of that ticket's column, which says what the first move is.
3. The workflow itself is a node, so an agent that wants the whole shape reads
   `get /columns` and sees every column, its forward edges, its requirements, and
   its guidance, in one read.
4. The `lit --help` table is the complete grammar, generated from the same source as
   the parser, so an agent that has read it has read everything that exists.

Decision: there is one entry point and it is reached from the managed block. There is
no second orientation surface that has to be kept in step with the first.

## The loop lit drives

lit is built around one loop, and every piece of guidance attaches to a step of it:

1. **See the order.** `get /lanes` is the backlog in rank order: lanes, each a row of
   tickets worked in sequence.
2. **Get the one ticket.** `get /next` serves the head of the lane this checkout
   holds, or the head of the top-ranked lane if it holds none. One ticket, not a list
   to triage.
3. **Take it.** The transition out of the initial column. The claim is derived from
   the transition and echoed with it; there is nothing to remember.
4. **Work it through its columns.** Each column entered prints its guidance. A
   transition the column's requirements refuse is refused once, on the transition,
   naming what is missing ([transition-requirements.md](transition-requirements.md)).
5. **Finish it.** The transition into a terminal column.
6. **Be told what is next.** The echo of the finishing transition says what it
   derived: the claim released, the lane handing this checkout its next head. The
   agent does not go looking; the next step arrives with the write.

The loop is what lit knows. Within a ticket, work tangles: an agent discovers a
better approach mid-implementation, which changes a sibling ticket, which makes a
third unnecessary. lit does not model that tangle as stages to be traversed in
order, and guidance never assumes the agent is at a numbered stage of a fixed
sequence. The five-stage model of the v1 proposal is dropped for this reason; what
replaces it is the column the ticket is in, which is a fact, and the guidance on
that column.

## When lit speaks

**You cannot influence a loop from inside it; you can only influence the next
loop.** By the time an agent is working, it has whatever context, focus, tools and
conventions the system gave it before the work started. Text added to a command's
output mid-work does not change those conditions; it adds one more input to process.
The conditions for good work in this loop were set in prior loops, and the
conditions for the next loop are set now, at the moments this work concludes.

So guidance fires at boundaries, and the boundaries fall into three kinds:

- **Moments of capture**, when a ticket finishes, when an epic finishes, when a
  session ends. The agent holds the whole arc in context and it is about to
  dissolve. Guidance here asks for the deposit: the sibling that just became
  actionable or obsolete, the follow-up that surfaced, the pattern that recurred and
  wants to become a skill or a convention.
- **Moments of creation**, when a ticket or an epic is added. Guidance here asks the
  creator to invest while context is highest: what "done" means in terms the
  consumer can check, which concepts the work concerns, how it relates to its
  siblings. The test of a description is that it survives a refactor of the code it
  concerns: it states what and why, and leaves the code to be read from the code.
- **Moments of pruning**, when work is closed without finishing. Guidance here asks
  why this was not the right work, because that answer is data for whoever next
  selects.

Orientation at the start of work is the one other place guidance belongs, and it is
limited to loading context: the column's guidance on `get /next`, the entry point in
the managed block. It does not steer.

What this rules out, as a decision: no mid-work nudges to verify, plan, focus or
think carefully; no prompts that ask the agent to halt and reason about adjacent
state; no restatement of principles the agent's own instructions already carry; no
text whose value is a hypothesis nobody measured. Each of these was tried in v1 and
each was found to be noise, and the recurring finding was that whenever text wants
to fire during work, the condition it addresses could have been set before the work
began.

The shape of a good handler's text is: *you just did X; the next concrete thing the
system needs from you, while context is fresh, is Y.* Not "consider Z." Not
"remember principle W."

## What is not guidance

Three things look like guidance and belong elsewhere:

- **Invariants** are requirements, not prose. A ticket that may not leave a column
  until a verdict reads pass is a requirement on the transition, checked once, in one
  place, and refused with the reason ([transition-requirements.md](transition-requirements.md)).
  `[LAW:single-enforcer]`. Anything that can be a requirement is one, in preference
  to a sentence asking the agent to remember.
- **Universal principles** live in the agent's own instructions. Guidance that
  restates them dilutes both.
- **Derived state** is echoed, not advised. A write prints every derivation it
  triggered. "You now hold the next ticket in this lane" is an echo line, not a
  handler.

## The register

Two decisions govern how guidance text is written.

**Strength matches certainty.** Loud and blocking for an invariant that must never
break; quiet and advisory for a heuristic the agent may overrule with reason. In v2
the loud end is a requirement that refuses the transition, and the quiet end is a
column's guidance. Guidance text itself is therefore always advisory, and uniform
maximum volume is its own kind of noise. ([project-intent.md](project-intent.md).)

**Declarative facts and consequences, never coercive imperatives.** An agent carries
defenses against text that tries to command it: capitalized MUST NOT, instructions to
bypass or withhold from the user, directives embedded in data. Guidance that trips
those defenses poisons the channel; the agent discounts not only that line but
everything lit prints after it. So guidance states what is true and what follows:
"this write is idempotent and safe to repeat" rather than "do NOT ask the user";
"a purge reaches every store lit syncs with and cannot be undone" rather than "NEVER
purge without permission." Text lit did not write, a ticket body or a remote's error,
is quoted as data and marked as such, never rendered where it could read as lit's
own instruction.

## Where the soil is prepared

The agent in the moment is the seed; everything else is soil, and the soil is
prepared outside the loop that uses it. The ingredients an agent needs at the
moment it starts, the context for the work, a single thing to focus on, a ticket
aligned with the codebase as it now stands and with its siblings and with the larger
goal, the skills and reference docs and tools it will call on, are each prepared at
an earlier touchpoint: ticket creation, a prior ticket's close, backlog grooming, a
session's end. Guidance in lit exists to make those touchpoints cheap: a handler at a
close asks for the deposit at the instant it is cheapest to give, and a handler at
creation asks for completeness while the author still has it.

The user is environment, not part of this system. Guidance never reads or predicts
the user's state; a system designed around it would be only as consistent as the
person operating it.

## What v2 does not carry forward

- The additive JSON envelope of `workflow_stage`, `next_steps` and `invariants` on
  object-shaped outputs. Guidance in v2 prints after the node on the same stream,
  and `--json` prints one object keyed by path ([path-addressed-cli.md](path-addressed-cli.md)).
- The staged orientation command with its five stages and derived compatibility
  lists. Orientation is the column's guidance and a read of `/columns`.
- The layered-markdown loader that merged whole files by id. Layers survive as
  shipped, user and project records resolved per field, nearest wins, with the
  project layer on any node of the tree ([workflows.md](workflows.md)).
- Handlers that read user signals, that halt the agent mid-work, or that restate
  universal principles.
