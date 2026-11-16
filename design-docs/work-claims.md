# Work Claims: coordinating parallel checkouts over one shared backlog

Status: accepted design (validated in design dialogue, 2026-08-14). Written
tense-independent — it reads as a specification before implementation and as a
record of the architecture and its reasoning after. If behavior and this
document disagree and no superseding design exists, the divergence is a bug in
one of them, not an automatic win for either.

## Summary

One shared backlog serves many parallel lines of work: several worktrees on one
machine, several clones across machines, several people. Selection (`/next`)
answers "what should be worked now" — but with only global state to consult, it
gives every checkout the *same* answer, so parallel streams collide on a single
cursor and users fight over rank to steer it.

This design introduces the **claim**: a lane of work belongs, temporarily and
advisorily, to the **checkout** that most recently worked it. Claims are
**purely derived** from work events already in the shared store. They are
never stored, never synchronized as their own state, and never cleaned up —
there are no claim objects and no claim lifecycle. The entire state footprint
is: every event carries an opaque attribution, and each checkout keeps one
identity token in its private git directory. Who holds what, what is
contested, and where `/next` should route are all computed at read time from
facts the system already synchronizes.

Claiming is implicit — a transition out of the initial column on a ticket in a
lane claims the lane. Release is implicit — finishing the lane, parking a
member back into the initial column, or the claim expiring releases it — or
one explicit write, `rm <lane>/claim`, which the fold reads as ending the hold
until the next establishing act: a start or finish transition, or
`add <lane>/claim`, the take that moves work left mid-column to this checkout.
A released claim is gone: nothing records that it existed. Focus and unfocus are not modes: a checkout with live
claims is focused on them; a checkout without claims sees the global backlog.
The global case is the natural zero state, not a second layer.

## The problem

The backlog is one shared, synchronized truth with one rank order over its
lanes. That is a strength — every machine sees the same backlog — but it makes
"the top of the backlog" a single shared slot, and `/next` a single global
cursor. Two symptoms follow:

- **Parallel checkouts pull the wrong work.** A second worktree is opened to
  run a different epic in parallel. Any fresh agent session there follows the
  standard workflow and reads `/next` — which serves the *project's* next
  ticket, i.e. the other worktree's epic. Every new session must be re-briefed
  by hand, and an unbriefed one silently works the wrong stream.
- **Users fight over rank.** Two people each rank their own lanes to the top.
  Rank moves are intents, and the fold places both — both writes survive. The
  fight is not a merge bug and no representation change can fix it: two
  "before everything" intents are mutually exclusive *by meaning*. Each user
  reached for the only lever that steers `/next` — project-level priority — to
  express something that was never a priority statement at all: "I am working
  on these now."

The general question underneath both: **how do distributed checkouts of one
repository coordinate work such that the shared backlog stays meaningful?**
Any answer that lives only in local state fails the second symptom — other
machines must be able to see that work is taken. Any answer that mutates
shared priority fails the first — that is the current behavior.

## The shaping principle: preference is local, commitment is shared

An earlier draft of this design gave each worktree a locally-stored "focus" — 
a persistent per-checkout filter over the backlog. It treated stream selection
as a *preference*, and preferences are private, so the state went local. The
draft failed the distributed requirement: a teammate's clone cannot see local
state, so their `next` would still serve work another stream was deep into.

The correction inverts the premise. What a working checkout holds is not a
preference but a **commitment** — "this line of work is being executed here,
and it will be worked until it is done." A commitment is information every
other stream needs, so it must travel through the one channel all checkouts
share: the synchronized database. What remains genuinely local is only
*identity* — a checkout must recognize which commitments are its own.

Everything in the design follows from placing each fact where it is
authoritative: work records in the shared database, checkout identity in the
checkout, physical addresses on the machine that owns them, and the claim
itself nowhere — it is a reading, not a record.

## The model

### Vocabulary

- **Checkout** — one working directory of the repository: the primary clone or
  any linked worktree. The durable unit of a work stream. Sessions, agents,
  and humans come and go inside a checkout; the checkout persists. Informally:
  the *worktree is the identity*; the *workstream* (an epic or lane) is what
  it currently carries; the claim is the derived binding between the two.
- **Lane** — the declared unit of serialization: an ordered list on any node,
  whose members are leaves from anywhere in the tree. Tickets in one lane are
  sequential, tickets in different lanes may proceed in parallel. A leaf in no
  declared lane is its own one-member lane; a node worked as a unit is a folder
  holding one lane. The lane is the claimable unit.
- **Evidence** — events in the shared store attributed to a checkout: which
  tickets it moved out of the initial column or into a terminal one, and when
  it last touched them.
- **Claim** — the derived relation "lane L is held by checkout S," recomputed
  from evidence on every read.
- **Freshness window (`claim_ttl`)** — the configured age beyond which evidence
  no longer supports a claim, declared in the workspace file and resolved by
  nearest ancestor.

### The claim predicate

A lane **L** is claimed by checkout **S** exactly when all of the following
hold:

1. **L is unfinished** — a member whose stage is not terminal remains in it.
2. **S produced the latest establishing event on L.** Only transitions
   establish or transfer a claim: a move out of the initial column and a move
   into a terminal one, on any member. Grooming edits, comments, ranking, and
   label changes never establish a claim — a drive-by comment from another
   checkout must not capture a lane. "Latest" is the fold's total order,
   `(causal-position, writer, event id)`, so a takeover made on a clock that
   runs behind still outranks the start it took over. The fold computes the
   holder set in that order; `claim` is the last and `contested` the rest. A
   release write, `rm <lane>/claim`, ends the hold until the next
   establishing act, and so does parking a member into the initial column;
   `add <lane>/claim` is an establishing act with no transition.
3. **The claim is fresh.** Once established, *any* event by S on L's members
   refreshes it — ordinary working commentary keeps a claim alive through a
   long stretch on a single ticket. A claim whose newest event is older than
   `claim_ttl` no longer holds. Expiry is tick's: computed on every read
   against `now`, never stored.
4. **S is live, as far as this machine can tell.** See the liveness prune
   below; machines that cannot check assume liveness and rely on freshness.
   The prune answers with what it found, not merely whether to void, and a
   worktree the holder has locked carries its claim past `claim_ttl` — leg 3
   defers to leg 4 in that one case, and in no other.

Derived annotations accompany the predicate:

- **Contested** — a second checkout also has live evidence on L (an offline
  race, or a takeover the previous holder has not yet seen). Contest is an
  annotation, not a state: routing stays deterministic (the latest
  establishing event holds the claim), both sides are notified the next time
  they look, and sync reconciliation surfaces it for judgment.
There is no annotation for a claim whose evidence has aged past `claim_ttl`.
An expired claim is not a claim, and it is not the record of one either: the
lane derives **unclaimed**, exactly as a lane nobody ever started does, and no
surface says anything about the claim that used to be there. The design once
carried a third standing here, *Stale*, described as "the record that a claim
existed", and every consumer gave it a meaning — a "(stale)" badge on the
claim line, a provenance printout on the start, a takeover verdict in
routing. That was a grade of claim wearing a disclaimer, and it produced the
report links-claims-y6yz: a lane whose claim had lapsed six hours earlier
still printed as claimed on every `/next`, and nothing the user could do
touched it, because there was nothing there to touch (owner ruling,
2026-09-26). See "An expired claim is no claim" under Routing.

A claim dissolves by the predicate ceasing to hold: the lane finishes, a
member is parked, the evidence ages out, or the holder's checkout is locally
known to be gone; or it is released by the one write. Nothing else is stored,
so nothing is transferred or cleaned up — and nothing is remembered.

### Identity and attribution

Each checkout holds a **checkout id** — a 128-bit opaque token — in the
checkout's private git directory, `<git-dir>/lit/checkout/id` (the same
per-worktree private area where git keeps HEAD; git guarantees one per worktree
and deletes it with the worktree). The token is write-once local state and is
never synchronized as configuration. It is the thing claims and writer refs key
on.

Every event carries an attribution triple: **(checkout, principal, agent)** —
the checkout's token, the key that signed the event, and an optional opaque
agent label. All three are opaque. Attribution is historical fact and is never
rewritten.

Sessions, agent labels, and user names play no role in claims. Many
sessions in one checkout are one claimant; a new session inherits its
checkout's claims with no re-briefing, which is precisely the behavior the
design exists to produce.

**Local liveness prune** (the `git worktree prune` pattern): when deriving
claims, a machine enumerates its own live worktrees and their checkout ids.
Evidence from this store whose checkout id matches no live local worktree
comes from a checkout that no longer exists — it is treated as void,
immediately, on this machine. Remote machines cannot see this filesystem, so
for them the same claim ends by aging out. The asymmetry is deliberate and
honest: deletion is a local fact, and only its owner can observe it instantly.
A different clone on the same machine has its own store and is never pruned by
this one.

The enumeration answers with more than a yes or no. The finding is one of
four: *gone* (this machine enumerated and the worktree is absent — the void
above), *present*, *locked* (`git worktree lock`), or *unprovable* (another
clone, which names no worktree here to look for). Two of
the four decide anything. *Gone* voids. *Locked* is the one finding that
outranks the clock: it carries the claim past `claim_ttl`, so the lane is **held** —
routed around and gated exactly as a fresh one is, with no separate word for
it. Every other signal here says a worktree EXISTS, which a dead session leaves
behind just as readily; a lock is the holder speaking, and nobody sets it by
walking away. Mere presence deliberately does not sustain a claim — a worktree
outliving its session is ordinary, and letting that hold a lane would strand
every uncleaned tree's work with the age-out that exists to release it never
firing. A lock never outranks proven absence in the other direction either:
git withholds `prunable` from a locked record, so a stream missing from the
listing is gone whatever it once claimed about itself, and reading it the
other way would make a lock an unkillable claim.

*Present* and *unprovable* change nothing: past `claim_ttl`, with no lock, the claim has
expired and the lane is unclaimed whatever this machine can see of the
worktree. The enumeration once decided what an expired claim was called —
"stale, worktree present" against "orphaned" — which presupposed that an
expired claim was something to describe (links-claims-2wk2). It is not, so
the finding is read for the two decisions above and nothing else.

### Granularity: why the lane

The claim unit is the lane because the lane is already the system's declared
unit of serialization, and "what must be sequential" and "what one stream
holds" are the same fact — a serial line of work in two hands is a merge
conflict in waiting.

- A conventional single-lane epic claims wholesale: start one ticket, hold
  the epic. This is the common case and the degenerate one — no lanes
  declared, one lane claimed.
- A deliberately multi-lane epic is claimable lane by lane. A catch-all epic
  (independent bugs, say) is never monopolized because one bug was started.
  Several checkouts may legitimately share one epic across lanes.
- A cross-epic dependency pulled onto a stream's path produces evidence in
  *that ticket's own lane* — working a dependency never swallows the
  neighboring epic.

### GRANULARITY RULING (owner, 2026-08-24)

**The rule, in the owner's words**: "ALL LANES IN AN EPIC SHOULD BE SURFACED
BEFORE ANY LANE FROM THE NEXT EPIC." One checkout works an entire epic to
completion before moving on, no exceptions. Never more epics in progress than
worktrees.

**Why this is the rule and not a tuning knob**: v1's selector tried to
approximate it with a stable partial sort gated by a flag, promoting leaves
under *any* in-progress epic. That mechanism is self-defeating under exactly
the failure it exists to prevent: each hop to a new epic enlarges the promoted
set, weakening the bias for the *next* hop, so six in-progress epics in one
checkout is the loop's fixed point, not a pathology of it. The fix cannot be
another nudge in the same shape; it has to fall out of the ordering, so every
in-epic leaf precedes every out-of-epic leaf as residue of the structure rather
than as a rule someone can forget to apply.

**How the model meets it.** The lane is the unit a checkout commits to, and a
node worked as a unit is one lane: an epic's leaves in one ordered list on the
epic. Holding that lane is holding the epic, and `/next` serves the held lane's
head until the lane has no member left whose stage is not terminal. An epic
whose work may be shared declares several lanes and is held lane by lane. The
ruling is therefore a question of how a workspace declares its lanes, not a
routing step.

## Routing

`/next` is the walk of the lane order under the claims:

1. **The checkout's own held lanes come first**, in rank order: each lane's
   head, the first member whose stage is not terminal. A head whose own `held`
   is non-empty — a blocker still open, an unmet exit requirement — is printed
   with its reasons and passed.
2. **Then the first lane head in rank order that is not held elsewhere and
   whose `held` is empty**, labeled as outside the commitment, so the
   commitment is visible before it is made rather than discovered after. A
   checkout holding no live claims reaches this directly: unfocus is the zero
   state, not a hop through step 1.
3. **Every lane passed over is named with its reason** — held by another
   checkout, its head held by a blocker, its column undeclared — so the
   diagnostic never recommends what the transition would refuse and never
   asserts a standing it did not read. Exhaustion is loud and not a wall: a
   checkout whose every held lane is blocked is told so and served the next
   lane in rank order beside the reason (owner ruling, links-next-5sxz,
   2026-09-29; scenarios.md strain S-3). Leaving is never silent.
4. **An urgent head ranks above the held lane**: `priority: urgent` on a
   ticket serves its lane's head first, labeled as the interrupt it is.

Two rules hold across the walk:

- **What a pick says.** `/next` reports a pick; the transition takes it. So a
  pick whose start *would* establish a claim says so in the conditional,
  naming the write: "`set B.1/status plan` claims lane one of epic B". Nothing
  is said in the perfect tense. A line that reports a start or a claim
  describes a side effect a read does not have, and an agent that believes it
  holds a claim it does not either works unclaimed or spends a turn proving
  the tool wrong — both observed (links-next-output-5aee). A lane of one is not
  named at all; there is nothing to say about it that the ticket has not
  already said.
- **Lanes another checkout holds are routed around, not hidden.** `/next`
  passes them, naming the reason; `get /lanes` shows everything with its
  holder. Visibility is not pullability.

**An expired claim is no claim.** A lane whose claim has aged past
`claim_ttl` is admitted on the same terms as a lane that never carried one,
and a lane another checkout holds is not. That distinction is the entire
content of the rule. What makes an in-flight ticket takeable is that nobody
holds its lane. The older exclusion — an aged-out lane withheld exactly as a
live foreign hold is — is retired because it was never coherent, not because a
tradeoff shifted. A claim is evidence of ownership, and expiry is the evidence
running out; letting an expired claim veto a ticket means trusting the claim
over the proof that the claim is dead (owner ruling, links-claims-1b0p,
2026-09-03). An expired claim on the checkout's *own* lane is no different:
the checkout holds nothing, and the lane is reached by rank like any other. It
was once read the other way — the age of your own lane as evidence you stepped
away from work still yours, handed back to resume — and under that reading a
checkout that finished one ticket of an epic and walked away was routed back to
that epic ahead of the entire backlog for as long as the epic stayed open; with
one checkout in the repository, forever, since nothing it could do released a
claim that no longer existed (owner ruling, links-claims-em7h, 2026-09-26). The
step between those two rulings — an expired claim admitted, but still printed,
still called a takeover, still gating the start with a provenance line — is
retired with links-claims-y6yz: an expired claim is over, not demoted, and
nothing about it is shown. Overriding a claim that is live is a deliberate
transition on a member of that lane. A claim is a well-founded default, never
a lock.

**Contested lanes** keep deterministic routing: the latest establishing event
in total order holds, `contested` names the rest, and the other party's
`/next` stands down and says why.

Priority composes predictably: urgency reorders *within* eligible sets, and
claims define eligibility. An urgent ticket landing in a held lane hoists for
its holder, not for everyone.

## Release and abandonment

A claim ends four ways:

1. **Completion** — the lane finishes; the predicate no longer holds anywhere.
2. **Parking** — a member is moved back into the initial column.
3. **Release** — the one explicit write, `rm <lane>/claim`, read by the fold
   as ending the hold until the next establishing act, a transition or an
   `add <lane>/claim`.
4. **Age-out** — the safety net for silent abandonment anywhere: evidence
   older than `claim_ttl` stops supporting the claim.

The common case performs none of the deliberate ones. Agents do not announce
that they are stopping — they just stop — so the design never depends on a
release being written; age-out is what the common case relies on, and the
release write exists for the checkout that knows it is leaving.

Abandonment needs no bookkeeping of its own because **everything worth
knowing about an abandoned lane is in the shared events**, and none of it is
in the claim:

- *Nothing completed, claim expired* — the lane is unclaimed, and `/next`
  offers its tickets like any other; a ticket left in progress is offered as
  abandoned work in flight, and the ticket's own history names whoever
  started it.
- *Unmerged work in flight* — deliberately outside the tracker's knowledge:
  branches and pull requests belong to git and the forge. The agent picking
  up an abandoned in-progress ticket checks for unmerged work before
  proceeding; the quickstart says so. Judgment stays with the caller closest
  to the context.
- *Half the lane completed, then abandoned* — completed tickets are completed
  in the shared store; the claim carried no work-state of its own, so
  picking the lane up inherits a half-done lane with nothing to transfer and
  nothing lost.

## Finding a claimant

Interrogation has two tiers, and only one requires locality:

- **The dossier travels with the data.** The evidence *is* the shared
  store: which tickets the claimant started and finished, when it last
  acted. Any machine renders "claimed: checkout 7f3a (elsewhere) · 2h ago ·
  pgct.11 in progress, 5/9 done" — most of what "who has this and how is it
  going" means, available everywhere.
- **The address is resolved where it exists.** For claimants that are live
  worktrees of this store on this machine, the liveness enumeration also
  yields a path and branch at render time: "claimed here: ../links-wt-pgct
  (links-sync-pgct.11)". A human or an agent can walk over — open the
  worktree, read its log, inspect the in-progress ticket. No interrogation
  protocol; a resolvable address. Remote claimants render as the opaque
  discriminator plus the dossier — on their end, it's just claimed — and the
  human coordination handle for remote conversation is the work's own name,
  as it would be anyway: "who's got pgct?"

## Distribution, races, and failure modes

Claims ride the existing sync channel because their inputs are ordinary work
records; there is no side channel to drift and no claim-specific merge logic.
With an eager (on-change) sync cadence, claim propagation is near-real-time
when online.

Two partitioned checkouts can start work in the same lane; nothing can
prevent that without locks, and the design refuses locks — under partition an
advisory system must choose availability. The race resolves into the
**contested** annotation at reconciliation: both parties' evidence lands,
routing follows the latest establishing event, both sides get told, and the
sync report surfaces the lane for judgment — consistent with the system's
standing philosophy that sync conflicts are resolved by the agent's judgment,
not by silent policy. The worst case under partition is *visible duplicate
effort*, never blocked work — the correct failure mode for coordination
metadata.

Cold start is trivial: every event carries its attribution from the store's
first event, so there is no unattributed history to derive claims from.

## The privacy invariant

**Only opaque discriminators enter the shared store.** No hostnames, no
usernames, no directory names, no paths, no device details — the store syncs
to shared remotes and may itself be public. Display names live in the
policy's member entries and are rendered from there. Resolution from
discriminator to physical context (path, branch, machine) happens only at
render time, only on the machine that owns that context. Nothing needs to be
redacted because nothing identifying is ever copied away from its owner.

This is the locality principle finishing its own thought: each fact lives
where it is authoritative and therefore travels nowhere.

## State inventory

- **Shared, synchronized**: events, each carrying its attribution triple
  (checkout, principal, agent). Nothing else is added.
- **Local, per checkout**: one write-once checkout id in the checkout's
  private git directory.
- **Derived at read time**: claims, freshness, staleness, contest, liveness,
  routing, and every rendering of "who has what."

There is no shared mutable state between worktrees outside the store — the
store is the one concurrency-safe shared medium, and this design adds no
second one. There are no claim tables, no registries, no lockfiles, no
sidecar maps.

## Relationship to adjacent mechanisms

- **Ticket execution and session identity**: the unit of *ticket execution*
  is the agent session, recorded as the agent label on each event. This design
  adds a second, coarser level: the session executes a ticket; the **checkout
  carries a workstream**. Both are true at once, and claims are derived from
  checkout attribution precisely because sessions are too ephemeral to carry a
  multi-session line of work. There are no heartbeats and no liveness probes
  here, only staleness heuristics — the same family as orphan detection,
  applied one level up (lane instead of ticket, `claim_ttl` instead of the
  orphan threshold).
- **Rank** remains the shared default agenda — what a zero-context checkout
  should do — and ranking a lane to the top returns to being a genuine
  project-priority statement, because "I'm working this" no longer needs it.
  Companion decision, accepted with this design: **a new lane ranks last**
  rather than first, so filing work stops being an implicit claim on the
  project's attention.
- **Lanes** gain a second consumer: the same declaration that orders work now
  scopes possession of it. One vocabulary for both facts, by design.

## Alternatives considered and rejected

- **Per-checkout persistent "focus" state** (the previous draft of this
  design): treats stream selection as local preference; invisible to other
  clones, so it cannot make the shared backlog meaningful under distribution.
  Superseded by the commitment inversion.
- **An environment variable selecting the focused epic**: does not survive
  into fresh sessions without external plumbing, and is invisible state; at
  most an override layer, never a foundation.
- **Epic order as local-only state** (shared backlog genuinely unordered
  across epics): deletes real shared knowledge — projects do have a priority
  — and makes every machine's backlog display permanently divergent,
  undermining the one-consistent-view property that is the tool's core
  strength.
- **Local rank overlays** (a per-machine reordering consulted before shared
  rank): two truths about order, with every view obliged to disclose which
  one it is showing; claims deliver the needed 90% with a binary,
  self-explaining concept.
- **Written claim rows**: explicit claim state created on start and retired
  on completion or expiry. Buys pre-claiming and instant explicit release at
  the cost of a lifecycle, cleanup paths, and a second representation of
  who-works-what that can drift from the work records. Rejected for the
  derived form, which cannot drift from evidence because it *is* a reading
  of evidence.
- **Epic-granularity claims**: one started bug in a catch-all epic would
  monopolize twenty unrelated ones. The lane is the honest unit.
- **Session-bound claims**: every new session would start unclaimed — the
  original failure mode, rebuilt.
- **Stream identity derived from the claimed epic's name**: names the holder
  after the held, which destroys exactly the distinction the system exists
  to draw — two streams erroneously in one lane would be indistinguishable,
  and contested-claim detection collapses. Identity that changes with its
  referent is not identity.
- **Hostname / path / directory-name attribution** for friendly display:
  leaks device details into a shared, potentially public database; replaced
  by opaque discriminators plus render-time local resolution, which lost no
  functionality.
- **A shared local registry file** (per-repo JSON map of streams): shared
  mutable state between worktrees outside the database — reinventing,
  without the database's locking and merge machinery, exactly the problems
  the database exists to solve.
- **Locks / enforcement / heartbeats**: a claim that blocks others is wrong
  under partition (two offline clones cannot consult each other's locks) and
  wrong by philosophy — the system surfaces and advises; agents and humans
  decide. This boundary was settled before this design and is preserved by
  it.
- **Intent-replay (CRDT-style) rank merging**: heavier merge machinery
  cannot help, because two "mine first" intents are semantically exclusive —
  no merge honors both. The fix was never in the merge; it was in scoping
  the intent.

## Parameters and open edges

- **Freshness window `claim_ttl`**: default 6 hours, declared in the workspace
  file at the root or on any node, nearest ancestor winning. Repositories
  where humans idle over weekends may prefer ~72h; agent-heavy repositories
  may tighten it further. It was 24 hours until the routing defect in
  links-claims-1b0p was fixed: while every consumer read a stale claim of a
  checkout's own differently, the long window was armor against that
  inconsistency rather than a considered reading of when a claim goes cold.
  With the defect gone, the own-lane reading corrected in links-claims-em7h,
  and the expired standing removed in links-claims-y6yz, expiry means "no
  longer a claim, and nothing to say about it", and 6 hours of lane-wide
  silence — measured from the holder's newest event on any member, so
  finishing a ticket restarts the clock — is the weaker claim of activity it
  was always meant to be. `claim_ttl` equals the orphaned-ticket threshold in
  value only: separate policies over separate subjects (an abandoned claim vs.
  an abandoned ticket), derived separately, deliberately not backed by a
  shared constant.
- **Takeover confirmation shape** for fresh-claim override (interactive
  confirm vs. explicit flag for non-interactive agents): decide at
  implementation; the invariant is that overriding fresh evidence is always
  a deliberate act.

## Alignment with project intent

The project's bias is toward less: additions must earn their place against a
largely feature-complete tool. Claims add one derived concept and one local
token; no new shared state, no new commands for the common path, and no
ceremony — the mechanism is invisible exactly when there is nothing to
coordinate (a solo checkout behaves as before, plus stickiness to its own
epic). It serves the intent's autonomous default directly: a fresh agent
session in any checkout pulls the right work with zero re-briefing, because
the checkout — not the session, not a human's memory — carries the
commitment.
