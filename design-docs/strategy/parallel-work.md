# Parallel work: lanes and claims

How many checkouts work one shared backlog without pulling the same work or
fighting over its order.

## Diagnosis

The backlog is one shared, synchronized truth with one rank order, which makes
the top of the backlog a single shared slot. Two symptoms follow. Parallel
checkouts pull the wrong work: a second worktree opened to run another epic
reads `/next` and is served the first worktree's epic. And people fight over
rank: two people each rank their own lanes to the top, both intents survive,
and no representation can fix it, because two "before everything" intents
are mutually exclusive by meaning. Any answer that lives only in local state
is invisible to other machines, and any answer that mutates shared priority
destroys what priority means. Locks cannot help, because two offline clones
cannot consult each other. And agents do not announce that they are
stopping; they just stop.

## Guiding policy

Preference is local; commitment is shared. What a working checkout holds is a
commitment, "this line of work is being executed here", and every other
checkout needs it, so it travels through the one channel all checkouts share:
the store. What stays local is identity, so a checkout recognizes its own
commitments, and physical addresses, on the machine that owns them. Rank
stays the shared agenda, what a zero-context checkout should do, and ranking
a lane to the top is once more a statement of project priority, because
"I'm working this" no longer needs it.

## Actions

**The tree groups; the lane orders.** Hierarchy groups, scopes and rolls up
and orders nothing. A lane is an ordered list on any node, with members from
anywhere, and it is both the unit of serial work and the unit a checkout
holds. A ticket in no declared lane is its own lane. An epic worked in
sequence holds one lane; an epic with parallel streams holds several.
Serves: Operations.
Spec: `specifications/lanes-and-claims.md`, `specifications/work-items.md`.

**Rank is on lanes, as relative intents.** Lanes are ranked against each other
in one order by intents such as "above that lane", so two checkouts reranking
offline each still mean what they said when their logs meet.
Serves: Data, Operations.
Spec: `specifications/events-and-store.md`, "Rank".

**The checkout is the claimant.** Sessions, agents and people come and go
inside a checkout and the checkout persists, so claims key on the checkout: a
new session inherits its checkout's claims with no re-briefing, and many
sessions in one checkout are one claimant. Claims keyed to sessions would
leave every new session unclaimed.
Serves: Operations; project-intent.md, "What \"effective\" looks like
concretely".
Spec: `specifications/lanes-and-claims.md`.

**Claims are explicit events.** Starting a ticket writes a claim on its lane,
and so does an explicit take; grooming, comments, ranking and labels never
do, so a drive-by edit from another checkout cannot capture a lane. The fold
computes the holder from those events, and evaluate decides whether the claim
still holds.
Serves: Operations, Data.
Spec: `specifications/lanes-and-claims.md`.

**A claim ends without anyone announcing it.** A claim is bounded by a
freshness window measured from the holder's newest event on the lane, so
ordinary work keeps it alive and abandonment needs no release. Evidence that
the holder is gone, such as a deleted worktree or a session provider
reporting no live session, ends it early; nothing extends it. An expired claim
is no claim: the lane is admitted like one nobody ever started, because
letting an expired claim veto work means trusting the claim over the proof
that it is dead.
Serves: Operations.
Spec: `specifications/lanes-and-claims.md`.

**A claim is not a lock.** Starting in a lane another checkout holds is
refused locally, naming the holder, with the explicit take offered; the
refusal reads only this replica and never consults another machine. Under
partition two checkouts can both start, and the race becomes a visible
contest when their events meet: routing stays deterministic, both sides are
told, and the worst case is visible duplicate effort, never blocked work.
Serves: Operations, Data.
Spec: `specifications/lanes-and-claims.md`.

**`/next` works an epic to completion.** It serves the checkout's own held
lanes first, and every lane of the epic the checkout is already in before any
lane of another epic, so no more epics are in progress than there are
worktrees. Lanes another checkout holds are routed around and named, not hidden;
an urgent head interrupts only among lanes this checkout may take, so urgency
never crosses another checkout's hold.
Serves: Operations.
Spec: `specifications/lanes-and-claims.md`.

**A pick says what its start would do.** `/next` is a read and takes nothing;
when the start it suggests would establish a claim it says so in the
conditional, naming the write, so an agent never believes it holds a claim it
does not.
Serves: Operations.
Spec: `specifications/lanes-and-claims.md`.

**No second shared medium.** Claims ride the existing sync as ordinary events;
there are no claim tables, registries, lockfiles or sidecar maps between
worktrees, and no claim-specific merge logic.
Serves: Complexity.
Spec: `specifications/lanes-and-claims.md`.

**The dossier travels; the address is local.** Any machine can say who holds
a lane, how long ago they acted and how far the lane has got, from the store
alone. For a holder that is a live worktree on this machine, the path and
branch are resolved too, so a person or an agent can walk over and look.
Serves: Operations.
Spec: `specifications/lanes-and-claims.md`.

**Session providers are plugins.** Naming a session and reporting which
checkouts have a live one are external commands in the git credential-helper
style, so lit works with any agent tool and with none.
Serves: Operations; project-intent.md, "What \"effective\" looks like
concretely".
Spec: `specifications/lanes-and-claims.md`.
