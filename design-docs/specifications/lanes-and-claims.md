# Lanes, claims and next

Carries out `strategy/parallel-work.md`.

## Lanes

A **lane** is an ordered list node, `<node>/lanes/<name>`, on any node
including the root. Its members are leaves from anywhere in the tree, and its
order is sequence. A ticket belongs to at most one lane: adding it to another
moves it, and the echo shows the removal from the old one. A ticket's `lane`
field is derived from membership. A ticket in no declared lane is its own
one-member lane at `/lanes/<id>`, so it has a rank path like any other.

```
add /lanes/datetime '[dee, b64, fw9]'       create, one place per member
set /lanes/datetime '[b64, dee, fw9]'       reorder: one edit, echoed as a diff
rm /lanes/unfiled                            dissolve: every member becomes its own lane
```

Adding one-member lanes' tickets to a lane dissolves those lanes, echoed. A
lane's position gives `blocked_by`: in `[dee, b64, fw9]`, `b64/blocked_by` is
`dee` (`specifications/columns-and-requirements.md`, "Derived fields a
requirement reads"). A lane says "these, in this order"; a dependency says
"that thing, over there, first", and may connect two tickets in one epic.

**Identity.** A lane is identified by its name on its node. Creating a lane
at an existing `(node, name)` is idempotent. Two offline creations of one
lane fold to one lane whose members from both creations are placed in total
order, with an advisory naming both creators: `[dee, b64]` and `[dee, fw9]`
fold to `[dee, b64, fw9]` on every machine.

**Head.** A lane's head is its first member whose stage is not `terminal`.
A lane is unfinished while it has one.

**Rank is on lanes, not tickets.** A ticket has a position in its lane, and
the lane has a rank in one order over all lanes. Rank is written as a
relation, `set /lanes/datetime/rank '{above: /lanes/help}'`, `'{below: …}'`,
`top` or `bottom`; how the fold applies rank intents is
`specifications/events-and-store.md`, "Rank". A new lane ranks last. Reranking
never changes who holds a lane.

**Reading.** `get /lanes` read in rank order is the backlog, with each lane's
holder and the `held` of each head. `get /lanes/datetime` prints the members
in order with each member's status, and the lane's claim:

```
/lanes/datetime  rank 4   claim this checkout
  dee   impl
  b64   backlog
  fw9   backlog
```

## Checkouts and claims

A **checkout** is one working directory of the repository: the primary clone
or a linked worktree. It is the durable unit of a work stream. Sessions,
agents and humans come and go inside it. Each checkout has a checkout id, and
every event is attributed to `(checkout, principal, agent)`
(`specifications/access.md`, "Identity"). Claims key on the checkout id.

A **claim** is a checkout's hold on a lane, recorded by claim events in the
shared store. The fold computes the holder from them; evaluate applies
`claim_ttl` and liveness. A ticket's `claim` reads its lane's and is not
settable.

**Claim events.**

- A start, a transition out of the initial column, writes a claim event for
  the ticket's lane, and the echo shows it: `⇒ dee/claim = this checkout`.
- The explicit take, `add <lane>/claim`, writes one.
- The release, `rm <lane>/claim`, ends the hold until the next claim event.

Finishing a ticket does not establish a claim, and neither do grooming edits,
comments, ranking or label changes, so a drive-by comment from another
checkout cannot capture a lane. The lane stays held when its head finishes.

### The claim predicate

A claim by checkout S on lane L needs all of these:

1. **L is unfinished.**
2. **S wrote the latest claim event on L.** "Latest" is the fold's total
   order, `(causal-position, writer, event id)`, so a takeover made on a
   clock that runs behind still outranks the start it took over. The fold
   computes the holder set in that order: `claim` is the last, `contested`
   the rest.
3. **The claim is fresh.** Any event by S on L's members refreshes it, so
   working commentary keeps a claim alive through a long stretch on one
   ticket. A claim whose holder's newest event on the lane is older than
   `claim_ttl` no longer holds. Expiry is computed on every read against
   `now` and never stored.
4. **S is live, as far as this machine can tell** ("Liveness").

**Contested** is an annotation, not a state: a second checkout also has live
evidence on L, from an offline race or a takeover the previous holder has not
seen. Routing stays deterministic, both sides are told the next time they
look, and the sync report surfaces the lane for judgment. A takeover before
expiry, `add <lane>/claim`, is a visible write; the previous holder is
`contested` until its claim expires, and the head changes hands whatever
column it sits in.

**An expired claim is no claim.** A lane whose claim has aged past
`claim_ttl` derives unclaimed, exactly as a lane nobody started, and no
surface reports the claim that used to be there. It is admitted to routing on
the same terms; a checkout's own expired claim is no different. During the
window, `get <ticket>/claim` shows the holder and the expiry.

**`claim_ttl`** is declared in the workspace file at the root or on any node,
nearest ancestor winning; the default profile's value is in
`internal/assets/workspace/default.toml`.

### How a claim ends

1. **Completion**: the lane has no unfinished member.
2. **Release**: `rm <lane>/claim`.
3. **Parking**: a member moved back into the initial column.
4. **Age-out**: `claim_ttl` is a hard upper bound, measured from the
   holder's newest event on the lane.
5. **Liveness**: the local liveness prune or a `live` session provider ends
   the claim early. Liveness never extends a claim.

The release exists for the checkout that knows it is leaving.

### Liveness

**The local prune.** When deriving claims, a machine enumerates its own live
worktrees and their checkout ids, in the `git worktree prune` pattern.
Evidence from this store whose checkout id matches no live local worktree
comes from a checkout that no longer exists and is void, immediately, on this
machine. Remote machines cannot see this filesystem, so for them the claim
ends by age-out. A different clone on the same machine has its own store and
is never pruned by this one.

**Session providers** are external commands in the git credential-helper
style, so lit2 works with any agent tool and with none:

- `identify` names the session a lit2 process runs in; its answer is the
  event's agent label. `LIT_AGENT` is the built-in provider.
- `live` names the checkouts with a live session on this machine. When the
  provider that wrote a checkout's newest event reports no live session
  there, its claim ends at once; otherwise `claim_ttl` decides.
- A lane's sessions are the agent labels on its members' events.

### Starting in a held lane

Starting a ticket in a lane another checkout holds is refused locally,
naming the holder, and the explicit take is offered:

```
s4l is the head of m2q/lanes/history, held by <A> · 2s
```

## Routing

`/next` is the walk of the lane order under the claims. `<node>/next` is the
same walk over the lanes whose head is under the node.

1. **The checkout's own held lanes**, in rank order: each lane's head. A head
   whose `held` is non-empty is printed with its reasons and passed.
2. **Then every other lane of the epic the checkout is already in**, in rank
   order, before any lane of another epic: the first such head that is not
   held elsewhere and whose `held` is empty.
3. **Then the first lane head in rank order that is not held elsewhere and
   whose `held` is empty**, labeled as outside the commitment. A checkout
   holding no live claims reaches this directly.
4. **Every lane passed over is named with its reason**: held by another
   checkout, its head held, its column undeclared, or a member carrying
   events from a newer lit2 (unreadable). A checkout whose every held lane is
   blocked is told so and served the next lane in rank order beside the
   reason.
5. **An urgent head is served ahead of the held lanes, among lanes this
   checkout may take**: `priority: urgent` on a ticket serves its lane's head
   first, labeled as an interrupt. Claims define which lanes are eligible and
   urgency reorders within them, so urgency never crosses another checkout's
   hold.

**What a pick says.** `/next` reports a pick; the transition takes it. A pick
whose start would establish a claim says so in the conditional, naming the
write: "`set B.1/status plan` claims lane one of epic B". A lane of one is not
named. Serving a ticket prints its column's guidance, the handlers bound
`in <column>`.

```
$ lit2 get /next
fw9   /lanes/parser   rank 2   backlog
held above it:
  /lanes/datetime  b64   plan ← backlog/exit  description empty; labels=ready absent on b64 and its ancestors (rin)
```

**Routed around, not hidden.** Lanes another checkout holds are passed with
the reason; `get /lanes` shows everything with its holder. When nothing is
served, `/next` prints `(none)` and the count of lanes considered, so an
empty backlog and a backlog of held lanes do not look alike. The finishing
transition's echo names the lane's next head for this checkout:
`⇒ m2q/lanes/history head is now 7ka, held by this checkout`.

## Finding a claimant

- **The dossier travels with the data.** Any machine renders the evidence:
  `claimed: checkout 7f3a (elsewhere) · 2h ago · pgct.11 in progress, 5/9
  done`.
- **The address is resolved where it exists.** For a claimant that is a live
  worktree of this store on this machine, the liveness enumeration yields a
  path and branch at render time: `claimed here: ../links-wt-pgct
  (links-sync-pgct.11)`. A remote claimant renders by principal name, from
  the policy's member entries, plus the dossier.

## Considered and rejected

- **Per-checkout persistent focus state**: invisible to other clones, so it
  cannot keep the shared backlog meaningful under distribution.
- **An environment variable selecting the focused epic**: does not survive
  into fresh sessions without external plumbing, and is invisible state.
- **A shared local registry file of streams**: shared mutable state between
  worktrees outside the store, without its merge machinery.
- **A rank on every item, resolved per container, with the container
  doubling as the lane**, and **a stored `assignee` and a stored claim
  record, reconciled by hand**: each makes the things around it expensive to
  replace.

## Observability

The command event for `/next` records every lane considered, each head's
`held` lines, which lane was served or that none was, and the count of held
lanes, zero included `[LAW:nothing-unseen]`.
