# Work Claims: coordinating parallel checkouts over one shared backlog

## Summary

One shared backlog serves many parallel lines of work: several worktrees on one
machine, several clones across machines, several people.

This design introduces the **claim**: a lane of work belongs, temporarily and
advisorily, to the **checkout** that holds it. A claim is recorded by claim
events in the shared store, and the fold computes the holder from them. Who
holds what, what is contested, and where `/next` should route are all computed
at read time.

Starting a ticket in a lane — a transition out of the initial column — writes
a claim event for the lane, and so does the explicit take, `add <lane>/claim`.
How a claim ends is "Release and abandonment".
Focus and unfocus are not modes: a checkout with live
claims is focused on them; a checkout without claims sees the global backlog.
The global case is the natural zero state, not a second layer.

## The problem

The backlog is one shared, synchronized truth with one rank order over its
lanes, which makes "the top of the backlog" a single shared slot. Two
symptoms follow:

- **Parallel checkouts pull the wrong work.** A second worktree is opened to
  run a different epic in parallel, and a fresh agent session there reads
  `/next`, which serves the other worktree's epic.
- **Users fight over rank.** Two people each rank their own lanes to the top.
  Rank moves are intents, and the fold places both — both writes survive. The
  fight is not a merge bug and no representation change can fix it: two
  "before everything" intents are mutually exclusive *by meaning*.

The general question underneath both: **how do distributed checkouts of one
repository coordinate work such that the shared backlog stays meaningful?**
Any answer that lives only in local state fails the second symptom — other
machines must be able to see that work is taken. Any answer that mutates
shared priority fails the first.

## The shaping principle: preference is local, commitment is shared

What a working checkout holds is a **commitment** — "this line of work is
being executed here." A commitment is information every
other stream needs, so it must travel through the one channel all checkouts
share: the synchronized database. What stays local is *identity* — a
checkout must recognize which commitments are its own.

Everything in the design follows from placing each fact where it is
authoritative: work records and claim events in the shared database, checkout
identity in the checkout, and physical addresses on the machine that owns
them.

## The model

### Vocabulary

- **Checkout** — one working directory of the repository: the primary clone or
  any linked worktree. The durable unit of a work stream. Sessions, agents,
  and humans come and go inside a checkout; the checkout persists. Informally:
  the *worktree is the identity*; the *workstream* (an epic or lane) is what
  it currently carries; the claim is the binding between the two.
- **Lane** — the unit of claiming and of serial work, defined under "Lanes" in
  path-addressed-cli.md.
- **Evidence** — events in the shared store attributed to a checkout: its
  claim events on a lane, and when it last touched the lane's members.
- **Claim** — the relation "lane L is held by checkout S," folded from claim
  events and recomputed on every read.
- **Freshness window (`claim_ttl`)** — the configured age beyond which evidence
  no longer supports a claim, declared in the workspace file and resolved by
  nearest ancestor.

### The claim predicate

A lane **L** is claimed by checkout **S** exactly when all of the following
hold:

1. **L is unfinished** — a member whose stage is not terminal remains in it.
2. **S wrote the latest claim event on L.** A claim event is written
   automatically when a member starts, a transition out of the initial
   column, and by the explicit take, `add <lane>/claim`. Finishing a ticket
   does not establish a claim. Grooming edits, comments, ranking, and
   label changes never establish a claim — a drive-by comment from another
   checkout must not capture a lane. "Latest" is the fold's total order,
   `(causal-position, writer, event id)`, so a takeover made on a clock that
   runs behind still outranks the start it took over. The fold computes the
   holder set in that order; `claim` is the last and `contested` the rest. A
   release, `rm <lane>/claim`, ends the hold until the next claim event, and
   so does parking a member back into the initial column.
3. **The claim is fresh.** Once established, *any* event by S on L's members
   refreshes it — ordinary working commentary keeps a claim alive through a
   long stretch on a single ticket. A claim whose newest event is older than
   `claim_ttl` no longer holds. Expiry is evaluate's: computed on every read
   against `now`, never stored.
4. **S is live, as far as this machine can tell.** See the liveness prune
   below. Liveness can only end a claim early; nothing keeps a claim past
   `claim_ttl`.

Derived annotations accompany the predicate:

- **Contested** — a second checkout also has live evidence on L (an offline
  race, or a takeover the previous holder has not yet seen). Contest is an
  annotation, not a state: routing stays deterministic (the latest
  claim event holds the claim), both sides are notified the next time
  they look, and sync reconciliation surfaces it for judgment.

There is no annotation for a claim whose evidence has aged past `claim_ttl`.
An expired claim is not a claim, and it is not the record of one either: the
lane derives **unclaimed**, exactly as a lane nobody ever started does, and no
surface says anything about the claim that used to be there.

### Identity and attribution

Each checkout has a **checkout id**, and every event is attributed to
`(checkout, principal, agent)`, as the event model in event-store/design.md
defines. The checkout id is what claims key on.

Many sessions in one checkout are one claimant; a new session inherits its
checkout's claims with no re-briefing, which is precisely the behavior the
design exists to produce.

**Local liveness prune** (the `git worktree prune` pattern): when deriving
claims, a machine enumerates its own live worktrees and their checkout ids.
Evidence from this store whose checkout id matches no live local worktree
comes from a checkout that no longer exists — it is treated as void,
immediately, on this machine. Remote machines cannot see this filesystem, so
for them the same claim ends by aging out.
A different clone on the same machine has its own store and is never pruned by
this one.

**Session providers** are plugins, external commands in the git
credential-helper style, so lit works with any agent tool and with none:

- `identify` names the session a lit process runs in; its answer is the
  event's agent label. `LIT_AGENT` is the built-in provider.
- `live` names the checkouts with a live session on this machine. When the
  provider that wrote a checkout's newest event reports no live session
  there, its claim ends at once; otherwise `claim_ttl` decides.
- A lane's sessions are the agent labels on its members' events.

### Working an epic to completion

The claim unit is the lane. `/next` surfaces every lane of the epic a
checkout is already in before any lane of another epic, and a checkout works
its epic to completion before moving on, so no more epics are in progress than
there are worktrees.

This is a property of the ordering, not a tuning knob: every in-epic leaf
precedes every out-of-epic leaf as residue of the structure rather than as a
rule someone can forget to apply.

**How the model meets it.** The lane is the unit a checkout commits to, and a
node worked as a unit is one lane: an epic's leaves in one ordered list on the
epic. Holding that lane is holding the epic, and `/next` serves the held lane's
head until the lane has no member left whose stage is not terminal. An epic
whose work may be shared declares several lanes and is held lane by lane. The
behavior is therefore a question of how a workspace declares its lanes, not a
routing step.

## Routing

`/next` is the walk of the lane order under the claims:

1. **The checkout's own held lanes**, in rank order: each lane's
   head, the first member whose stage is not terminal. A head whose own `held`
   is non-empty — a blocker still open, an unmet exit requirement — is printed
   with its reasons and passed.
2. **Then the first lane head in rank order that is not held elsewhere and
   whose `held` is empty**, labeled as outside the commitment, so the
   commitment is visible before it is made rather than discovered after. A
   checkout holding no live claims reaches this directly: unfocus is the zero
   state, not a hop through step 1.
3. **Every lane passed over is named with its reason** — held by another
   checkout, its head held by a blocker, its column undeclared. A
   checkout whose every held lane is blocked is told so and served the next
   lane in rank order beside the reason.
4. **An urgent head is served ahead of the held lanes, among lanes this
   checkout may take**: `priority: urgent` on a ticket serves its lane's head
   first, labeled as an interrupt. Claims define which lanes are eligible and
   urgency reorders within them, so urgency never crosses another checkout's
   hold.

Two rules hold across the walk:

- **What a pick says.** `/next` reports a pick; the transition takes it. So a
  pick whose start *would* establish a claim says so in the conditional,
  naming the write: "`set B.1/status plan` claims lane one of epic B". A line
  that reports a start or a claim the read did not make describes a side
  effect a read does not have, and an agent that believes it holds a claim it
  does not either works unclaimed or spends a turn proving the tool wrong. A
  lane of one is not named at all; there is nothing to say about it that the
  ticket has not already said.
- **Lanes another checkout holds are routed around, not hidden.** `/next`
  passes them, naming the reason; `get /lanes` shows everything with its
  holder. Visibility is not pullability.

**An expired claim is no claim.** A lane whose claim has aged past
`claim_ttl` is admitted on the same terms as a lane that never carried one,
and a lane another checkout holds is not. What makes an in-flight ticket
takeable is that nobody holds its lane. A claim is evidence of ownership, and
expiry is the evidence running out; letting an expired claim veto a ticket
means trusting the claim over the proof that the claim is dead. An expired
claim on the checkout's *own* lane is no different: the checkout holds
nothing, and the lane is reached by rank like any other.

**A claim is not a lock.** Starting in a lane another checkout holds is
refused locally, naming the holder, and the explicit take, `add <lane>/claim`,
is offered. The refusal reads only this replica's fold and never consults
another machine.

**Contested lanes** keep deterministic routing: the latest claim event
in total order holds, `contested` names the rest, and the other party's
`/next` stands down and says why.

## Release and abandonment

A claim ends on any of these:

1. **Completion** — the lane is done; the predicate no longer holds anywhere.
2. **Release** — the explicit write `rm <lane>/claim`, read by the fold
   as ending the hold until the next claim event, a start or an
   `add <lane>/claim`.
3. **Parking** — a member moved back into the initial column ends the hold.
4. **Age-out** — evidence older than `claim_ttl` stops supporting the claim.
   `claim_ttl` is a hard upper bound, measured from the holder's newest event
   on the lane.
5. **Liveness** — the local liveness prune or a `live` session provider ends
   the claim early. Liveness never extends a claim.

Agents do not announce that they are stopping — they just stop — so the
design does not depend on a release being written; age-out and liveness are
what the common case relies on, and the release write exists for the
checkout that knows it is leaving.

Abandonment needs no bookkeeping of its own:

- *Nothing completed, claim expired* — the lane is unclaimed, and `/next`
  offers its tickets like any other; a ticket left in progress is offered as
  abandoned work in flight, and the ticket's own history names whoever
  started it.
- *Unmerged work in flight* — branches and pull requests live in git and the
  forge. The agent picking
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
  protocol; a resolvable address. Remote claimants render by principal name,
  from the policy's member entries, plus the dossier, and the
  human coordination handle for remote conversation is the work's own name,
  as it would be anyway: "who's got pgct?"

## Distribution, races, and failure modes

Claim events ride the existing sync channel as ordinary events; there is no
side channel to drift and no claim-specific merge logic.
With an eager (on-change) sync cadence, claim propagation is near-real-time
when online.

Two partitioned checkouts can start work in the same lane: the local refusal
sees only the events its replica holds, and under partition an advisory
system chooses availability. The race resolves into the
**contested** annotation at reconciliation: both parties' evidence lands,
routing follows the latest claim event, both sides get told, and the
sync report surfaces the lane for judgment — consistent with the system's
standing philosophy that sync conflicts are resolved by the agent's judgment,
not by silent policy. The worst case under partition is *visible duplicate
effort*, never blocked work — the correct failure mode for coordination
metadata.

## State inventory

- **Shared, synchronized**: claim events — the claim written on start, the
  take `add <lane>/claim`, and the release `rm <lane>/claim` — and
  `claim_ttl` in the workspace configuration.
- **Local, per checkout**: the checkout id.
- **Local, per machine**: the liveness inputs — the worktree list and the
  session providers' answers.
- **Derived at read time**: the holder, freshness, contest, liveness,
  routing, and every rendering of "who has what."

There is no shared mutable state between worktrees outside the store — the
store is the one concurrency-safe shared medium, and this design adds no
second one. There are no claim tables, no registries, no lockfiles, no
sidecar maps.

## Relationship to adjacent mechanisms

- **Ticket execution and session identity**: the unit of *ticket execution*
  is the agent session, recorded as the agent label on each event. This design
  adds a second, coarser level: the session executes a ticket; the **checkout
  carries a workstream**. Both are true at once, and claims key on
  checkout attribution because sessions are too ephemeral to carry a
  multi-session line of work.
- **Rank** remains the shared default agenda — what a zero-context checkout
  should do — and ranking a lane to the top returns to being a genuine
  project-priority statement, because "I'm working this" no longer needs it.

## Alternatives considered and rejected

- **Per-checkout persistent "focus" state**: treats stream selection as local
  preference; invisible to other clones, so it cannot make the shared backlog
  meaningful under distribution.
- **An environment variable selecting the focused epic**: does not survive
  into fresh sessions without external plumbing, and is invisible state; at
  most an override layer, never a foundation.
- **Session-bound claims**: every new session would start unclaimed — the
  original failure mode, rebuilt.
- **A shared local registry file** (per-repo JSON map of streams): shared
  mutable state between worktrees outside the database — reinventing,
  without the database's locking and merge machinery, exactly the problems
  the database exists to solve.
- **Locks**: a claim that blocks others across machines is wrong under
  partition, because two offline clones cannot consult each other's locks.

## Parameters

- **Freshness window `claim_ttl`**: default 6 hours, declared in the workspace
  file at the root or on any node, nearest ancestor winning. Repositories
  where humans idle over weekends may prefer ~72h; agent-heavy repositories
  may tighten it further. `claim_ttl` equals the orphaned-ticket threshold in
  value only: separate policies over separate subjects (an abandoned claim vs.
  an abandoned ticket), derived separately, deliberately not backed by a
  shared constant.

## Alignment with project intent

Claims serve the intent's autonomous default directly: a fresh agent
session in any checkout pulls the right work with zero re-briefing, because
the checkout — not the session, not a human's memory — carries the
commitment.
