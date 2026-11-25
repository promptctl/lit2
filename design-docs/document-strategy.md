# How our documents work

This describes a stragegy for documenting a project in such a way that we capture the intent and reasoning without duplicating info, encoding contraditions, or being unnecessarily prescriptive.

When documents are not useful or are counterproductive, we rewrite the documents.

## What documents are for

A document exists so that we can answer fundamental questions about our project: What are we building?  How should it work?  How is it built?  Who is it for?  How do they use it?  

## The problem this answers

How do we prevent documents from becoming multiple contradictory layers of wrong information.

## Altitude

Documents work at three altitudes. Each answers different questions, and
an item belongs at exactly one of them.

**Goals** say what lit is for and what success looks like to the people and
agents who use it. A goal describes an outcome and is silent about the
means. This is the military idea of *commander's intent*: state the purpose
and the objectives, so whoever acts can choose the method and adapt it when
circumstances change. A good goal can be reached more than one way.

**Strategy** documents each take one topic, such as storage, sync,
visibility or the command line, and say how lit intends to reach the goals
that topic serves. Critically, a strategy answers the "how" for a goal, and the "why" for a decision.

Richard Rumelt's *Good Strategy / Bad Strategy* gives the
shape of a strategy worth writing:
- a diagnosis of what actually makes the problem hard;
- a guiding policy for dealing with that difficulty;
- coherent actions that follow from the policy and reinforce one another.

Rumelt's warning applies directly. Bad strategy is goals restated as
strategy, fluff that sounds like insight, or a list of desirable things
that never confronts the hard part. Each strategy item names the goal it
serves. It doesn't restate the goal, because a strategy is a means to a goal, but one which still has flexibility to be achieved multiple ways.  A strategy is not an end unto itself, nor does it specify the low level mechanics of implementing that stategy.  

For example, 'using functional programming paradigms' is a strategy.  It could be done in service of multiple goals, such as 'writing clean code' or 'making code composable and modular'.  Using map, filter, and reduce would be implementations of that strategy.  The division between goal and strategy is flexible, but a strategy is always in service of a goal, and every specification will have a strategy as the the answer it it's "why".

**Specifications** sit below strategy, and only where a choice is consequential
and its reasoning isn't evident from the strategy: which library, which
encoding, which algorithm. A specification says what was chosen, what else was
seriously considered, and why this choice won. It cites the strategy it
carries out.

The test for where something belongs is the why/how ladder, from Jens
Rasmussen's means-ends abstraction hierarchy. Ask "why?" of an item and the
answer is the level above it. Ask "how?" and the answer is the level below.
An item that answers its own "why" and "how" is sitting
at two altitudes at once, and should be split. John Ousterhout puts the
same insight for software layers in *A Philosophy of Software Design*:
different layer, different abstraction. A layer that repeats the one above
it adds cost and no meaning.

## One home for each piece of knowledge

Each fact, rule, specification and reason is stated once, in the document and at
the altitude where it belongs. Everything else refers to it by name and
doesn't paraphrase it. This is Hunt and Thomas's original meaning of DRY:
every piece of knowledge has a single, unambiguous, authoritative
representation. The principle is about knowledge, not text. Two documents
explaining one rule in different words still hold two copies of it.

David Parnas made the related point about modules: put each specification that
is likely to change behind one boundary, so that changing it touches one
place. Documents are no different. A rule that lives in one place can be
changed or removed in one edit.

References point at names, not positions. "Charter item 9" breaks silently
when an item above it is removed, while a name keeps pointing at the same
thing. A reason lives with the thing it justifies. When a reason is
repeated elsewhere to make that document self-contained, the repetition
becomes the copy nobody updates.

## Documents say what is true now

Documents are always the source of truth.  They must be updated to reflect decisions, and remain small enough to be read quickly and thoroughly.  History can be found in git.  

Immutable decision records and appended amendment trails are left out deliberately: a superseded record that stays in place is a second source that now says something false, and every reader has to work out which version still holds.

When two documents contradict each other, the contradiction is surfaced and
resolved as soon as possible, never silently ignored.  Work does not proceed while a contradiction in documents exists. 

There is no precedence order, because with one home per fact there is nothing to rank. A contradiction means a fact has two homes, and the fix is to give it one.

## Say only what should be decided

Every line steers its reader, and an agent takes a stated constraint as
binding however casually it was written. So a document defines only what
has actually been decided, or open questions. Any topic can be ruled for, ruled against, or
left unsaid, and leaving it unsaid is **very often the right decision**. Removing a rule removes
it; an opposite rule doesn't take its place. Open questions and edge cases
aren't invented to look thorough, because each one creates a topic that then
demands a ruling. Specifications intentionally left open can be stated as 'Specification: Open'.  Specifications that are waiting for a decision can be marked "Undecided".

## Prohibition on the users words

**Do not use the user's verbatim quotes as design content.** Pasting what the user said into a design doc, at any level, is not design work. It restates the request without deciding anything. A quote is an input to the design: it tells you what the user wants, not how to deliver it. Leaning on quotes hides gaps, ambiguities and unexamined assumptions behind the user's own authority, so the doc looks complete when the real thinking hasn't been done. Instead, read each quote for its underlying intent and turn that into concrete Goals with success criteria. Then work out the Strategy and Technical Design yourself: weigh the alternatives, make the tradeoffs explicit, and justify each choice on its merits. If a requirement is unclear or seems to conflict with another, flag it as an open question rather than papering over it with the user's words.

## Drawn from

- Richard Rumelt, *Good Strategy / Bad Strategy*: strategy as diagnosis,
  guiding policy and coherent action, and how to recognize the imitation.
- Commander's intent, from mission-type command: state purpose and end
  state, and leave the method to whoever acts.
- Jens Rasmussen's means-ends abstraction hierarchy: each level is the
  "why" of the level below it and the "how" of the level above it.
- John Ousterhout, *A Philosophy of Software Design*: different layer,
  different abstraction; a layer that repeats another is a cost.
- Andrew Hunt and David Thomas, *The Pragmatic Programmer*: DRY as a single
  authoritative representation of each piece of knowledge.
- David Parnas, "On the Criteria To Be Used in Decomposing Systems into
  Modules": hide each likely-to-change decision behind one boundary.
