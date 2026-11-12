# lit v2: scenarios

Status: draft, 2026-11-05. Common situations walked through
[architecture.md](architecture.md) on paper. Each scenario names the need, the
primitives that meet it, what the agent or person can rely on at the end, and
any **strain**: a place where a primitive wanted a special case, or the design
was silent. Every strain ends in a resolution written back into the
architecture or a decision listed for Brandon. Outputs are illustrative;
the contract is the behavior stated under "relies on", never the text.

## Strain index

| id | scenario | strain | resolution |
|---|---|---|---|
| S-1 | 3 | a verdict with no `of` could pass a plan the reviewer never read | `of` is required on a plan verdict (architecture §3.6, §4) |
| S-2 | 3 | "the reviewer is not the author" compares two paths; no atom says it | not enforced by the engine; the `brief` handler says it; left as data |
| S-3 | 3, 11 | a held lane whose head is held traps the checkout | `/next` reports the held head and falls through, labeled (architecture §4) |
| S-4 | 2 | guidance at "an epic finishes" has no moment | a selector may read through `parent`; `(stage=terminal parent/stage=terminal)` on `enter *` |
| S-5 | 6 | two offline clones create a lane of the same name on one node | a lane is identified by its name on its node; `lane.create` is idempotent; members union in total order |
| S-6 | 7 | a column removed from the workspace file while a ticket, somewhere, holds it | the transition stands; tick reports the column undeclared; `/next` skips; `/integrity` lists it |
| S-7 | 9 | `add` with different values on different paths is N calls | `add` takes `path value` pairs like `set` |
| S-8 | 9 | N long bodies in one call need shell substitution | open; low frequency; a file form is a later addition if it bites |
| S-9 | 10 | a comment written concurrently with a purge vanishes | the fold drops it and counts it on `/integrity` as "events after purge" |
| S-10 | 13 | a rank intent whose anchor is gone when it arrives | the subject lands at the bottom of the rank order; `/integrity` names the intent |
| S-11 | 15 | `held` read by a principal without the tier key cannot evaluate a prose test | `held` reports `unknown (tier)` for that requirement; the writer always holds the key |

## 1. First contact in a fresh repository

**Need.** A person adopts lit2; an agent with no prior knowledge finds the
work and knows what to do.

**Walk.**

1. `lit2 admin init` mints the checkout id and the checkout's principal key,
   writes the managed block into `AGENTS.md` and `CLAUDE.md`, and creates the
   store under the common dir with zero events. There is no
   `.lit/workspace.toml`, so the workspace reads as the default profile: three level names, ten columns,
   two link types, one rule, four workflows, `claim_ttl`, and the two synthetic
   containers.
2. A fresh agent reads the managed block: work is tracked by lit2, run
   `lit2`, follow what it prints.
3. `lit2` is `get /`. The root prints its children (none), `lanes` (none),
   `columns` (10), `integrity ok`, `next (none)`. The `orientation/arrive`
   handler, bound `read /`, prints after it: what lit2 is, the four verbs,
   `get /next` to pick work, and that every node prints its guidance after
   itself.
4. `get /next` prints `(none)` and the count of lanes considered, zero, so an
   empty backlog and a backlog of held lanes do not look alike.

**Relies on.** From zero knowledge, two reads reach the loop. The default
profile is data that `get /columns` prints whole. Nothing was written.

**Strain.** None.

## 2. Filing, grooming, and the queue

**Need.** Work enters the backlog half-formed, stays out of the queue until
someone grooms it, and then is served in the order the groomer set.

**Walk.**

1. `add /children '{level: 2, title:"History reads come from one store"}'`
   creates a feature, a level-2 node, and homes it: the echo says
   `⇒ parent = unassigned`. Call it `m2q`.
2. `add m2q/children '[{title:…}, {title:…}, {title:…}]'` mints three tickets
   in `backlog`, each its own one-member lane, each without a body. The
   `capture/created` handler, `wrote */children`, prints once: the description
   says what and why; done-when is checkable.
3. `get /next` prints `(none)` and lists the three lanes passed over, each
   with its `held`: `backlog/exit  description empty; labels=ready absent on
   <id> and its ancestors`.
4. The groomer writes the three bodies, then
   `add m2q/lanes/history '[s4l, 7ka, m3d]'`: one lane, three `lane.place`
   events, `⇒ 7ka/blocked_by = s4l`, `⇒ m3d/blocked_by = 7ka`. The three
   one-member lanes dissolve into it, echoed.
5. `add m2q/labels ready`. One write; every ticket under `m2q` now passes the
   `labels=ready` requirement through `scope = ancestors`.
6. `get /next` serves `s4l`, `m2q/lanes/history 1/3`, and prints `backlog`'s
   guidance after it.
7. A follow-up filed into `m2q` later, with a body, is pullable at birth; one
   filed without a body is held by `description` alone.

**Relies on.** "Ready to pull" is the initial column's exit list and nothing
else. A label on the node grooms the node. The queue is the rank order of
lanes; the groomer orders it with `set m2q/lanes/history/rank top` if this
work should jump the others.

**Strain S-4.** guidance.md names "when an epic finishes" as a moment of
capture, but moments are raised for tickets only. The epic's finish is the
last child's `enter <terminal>`; a handler can see it if its `where` selector
may read through `parent`: `on: [enter *]`, `where: (stage=terminal
parent/stage=terminal)`. Selectors already take paths relative to the ticket,
and `scope = ancestors` already walks up for requirements, so letting a
selector atom name `parent/<field>` adds no mechanism. Adopted.

## 3. One ticket, start to done

**Need.** An agent takes a ticket through the default process, is refused
where the process says so, and is handed the next ticket when it finishes.

**Walk.**

1. `set s4l/status plan`. `backlog/exit` evaluates five requirements, all met;
   the summary line prints the counts. Derivations: `⇒ s4l/claim = this
   checkout`, `⇒ m2q/lanes/history held by this checkout`. The
   `plan-review/plan` handler fires on `enter plan`.
2. `set s4l/status impl`. Refused: `impl/entry  block  plan/verdict=pass
   s4l/plan/verdict = (none)`; nothing written; the `plan-review/unreviewed`
   handler fires on `refused impl`.
3. `set s4l/plan - <<EOF …` writes v1; the echo prints the digest.
4. A reviewer in another checkout: `get s4l/plan` prints the digest beside
   the text. `add s4l/plan/verdicts '{pass of:3e1a9c0d laws:[…]}'`. The verdict
   binds to v1. `⇒ s4l/plan/verdict = pass`.
5. The author had saved v2 between the reviewer's read and the verdict. The
   `add` is refused: `s4l/plan moved to v2 (digest 7e0f15c2) since 3e1a9c0d`.
   The reviewer reads v2 and records the verdict against it.
6. `set s4l/status impl` lands. The `phases/impl` handler prints: when the PR
   opens, `set s4l/status review`.
7. `set s4l/status review`, then a reviewer `add s4l/verdicts '{fail
   findings:["…", "…"]}'`. The ticket stays in `review`; `⇒ s4l/verdict fail,
   round 2`. `get s4l` now prints the `phases/findings` handler, bound `in
   review` where `(verdict=fail)`.
8. `set s4l/status merge` is refused on `merge/entry verdict=pass`. After
   `add s4l/verdicts '{pass}'`, it lands.
9. `set s4l/status done 's4l/reason' "Merged in #12"`. One write, two paths.
   Derivations: `⇒ s4l/claim this checkout → (none)`, `⇒
   m2q/lanes/history head is now 7ka`, `⇒ 7ka/claim = this checkout`. The
   `capture/finished` handler fires. `get /next` serves `7ka`.

**Relies on.** Every refusal names the requirement, the value found, and
writes nothing. The claim follows the transitions with no claim command. The
next ticket arrives with the finishing write.

**Strain S-1.** If `of` were optional and defaulted to the current digest,
step 5's reviewer would have passed v2 unread. A verdict is a judgment of one
text, so its type carries the text's digest: `of` is required on a plan
verdict, refused when it is not the current digest. One more token for the
reviewer, who has the digest from the read in step 4. Adopted.

**Strain S-2.** "The reviewer is not the plan's author" compares the verdict's
writer with the plan version's writer. No requirement atom compares two
paths, and adding one grows the test grammar for one rule. Not enforced by
the engine; the `brief` handler states it; a workspace that wants it enforced
is the signal to revisit. Left as data.

**Strain S-3.** Suppose `7ka` carried a `blocks` edge from a ticket in another
epic that is still open. Its `held` is non-empty. transition-requirements.md
recommends `/next` print the head and serve nothing else; work-claims.md
records the owner's ruling of 2026-09-29 that a checkout whose held work is
all blocked is never trapped and leaving is never silent. The ruling wins:
`/next` prints the held head with its `held` lines, then falls through the
rank order to the next eligible lane and labels it as outside the held lane.
Starting it holds a second lane; a checkout may hold several, and `/next`
serves held lanes in rank order first. Adopted in architecture §4.

## 4. The plan changes after it passed

**Need.** A plan edited after review does not silently carry a stale pass,
and does not strand the ticket.

**Walk.** `s4l` is in `impl` on a passed v2. `set s4l/plan -` writes v3;
`⇒ s4l/plan/verdict (none), was pass on v2`. The ticket stays in `impl`: a
requirement is evaluated at the transition and never again. At `set
s4l/status review`, `review/entry` carries `plan/verdict=pass` at `advise`,
so the write lands with one advisory line naming the digest the verdict was
on. A workspace that wants a re-review blocks it by setting that
requirement's strength to `block` or adding it to `impl/exit`.

**Relies on.** The fact is on the record and in the echo; the strength of the
nudge is one word in data.

**Strain.** None.

## 5. Two worktrees, one clone

**Need.** Two agents in two worktrees of one clone pull different work
without being briefed, and never silently duplicate.

**Walk.**

1. Both run `get /next` within a second. Both read the same store under the
   common dir; neither holds a lane; both are served `s4l`.
2. A runs `set s4l/status plan`; the commit lands on A's ref.
3. B runs `set s4l/status plan` a moment later. B's open reads the ref heads,
   sees A's new head, folds A's event, and finds the lane held by A. Refused:
   `s4l is the head of m2q/lanes/history, held by <A> · 2s`. Nothing written.
   B's `get /next` now passes that lane over, naming A, and serves the next.
4. The genuine race: both write within the same open. Both commits land on
   their own refs; no lock, no loss. The next fold on either side computes
   the lane's holders as `{A, B}`; `claim` is whichever the total order puts
   last and `contested` names the other. Both agents' next command prints the
   advisory from `/integrity`, and the one named in `contested` runs
   `rm m2q/lanes/history/claim` or keeps working, which is a human's call.

**Relies on.** The common case is a refusal with a name; the rare case is a
visible contest, never a lost write and never a lock.

**Strain.** None. The holder set is what makes step 4 representable.

## 6. Two machines, offline

**Need.** Two clones work for a day without a network and converge with
nothing lost and every disagreement visible.

**Walk.**

1. Both start the same lane head: as scenario 5 step 4, with the contest
   visible after the first fetch on each side.
2. Both edit `rin.2ae/body`. The fold holds both candidates on the field;
   `get rin.2ae/body` prints both, each with writer and time; `set
   rin.2ae/body -` by either side mints a `field.set` that names both
   candidates in its `over` (the writer folded both before writing), and the
   fold reads it as the resolution. A write that sorts after both but names
   neither is a third candidate. No "diverged" state exists.
3. Both file children under `rin`. Two children, two opaque ids, both
   survive; neither is renumbered because nothing is numbered.
4. Both rank a different lane to the top. Two `rank.place` intents in the
   one order; the fold applies them in total order; the later stands. The
   result is one order on every machine. Neither needed to do this: a claim
   says "I am working this" without touching rank.
5. Both create `rin/lanes/datetime`, one with `[dee, b64]`, the other with
   `[dee, fw9]`.

**Relies on.** Every concurrent write is kept; every contradiction is a value
somebody can read; `/integrity` is the one list of them.

**Strain S-5.** Step 5. A lane's address is its user-chosen name on its
node, so two creations collide on the address. A rule the fold can apply
identically everywhere: `lane.create` is idempotent on `(node, name)`, and
members from both creations are placed in total order, so the lane folds to
`[dee, b64, fw9]` on both machines with an advisory naming both creators.
This is the same rule as two `label.add` of one label. Adopted.

## 7. The process changes under running work

**Need.** Changing the process while others are mid-ticket produces a state
every machine agrees on, and the change itself is reviewed like any change to
how the team works.

**Walk.**

1. A edits `.lit/workspace.toml` to add `[[columns.backlog.exit]] test =
   "points" strength = "block"`, tries it in the working tree, commits, opens
   a pull request. The diff is the change.
2. B, on a branch without it, runs `set b64/status plan`. B's file has no
   such requirement; the write lands, citing the blob hash of B's file.
3. After both sync events and merge the file, the transition stands: it
   was judged at B's write under B's file, the event names that file's
   blob, and the fold applies it as written; `b64` is in `plan` with
   no `points`. A requirement guards a transition, and this one happened
   before the requirement existed anywhere B could see.
4. Two people edit the file concurrently. They meet in a git merge conflict
   on the file, which is where concurrent edits to a file are already
   resolved, with the full diff in front of them.
5. A's merged file removes `rin`'s `audit` column while `rin.4ou`, started on
   another machine, sits in `audit`. The local `in_use` check saw no such
   ticket at edit time.

**Relies on.** Nothing a writer did legally is undone by a later rule; the
process is versioned, diffed and reviewed by git; and nothing is hidden:
`/integrity` lists the ticket in a column its ancestors no longer declare.

**Strain S-6.** columns-and-phases.md says a stored column its workflow does
not define is unrepresentable. With the model in a file that merges
independently of the events, it is representable, and the engine needs one
rule for it. The rule follows the existing stance for an undeclared field
value: the transition stands, tick reports `status: audit (undeclared)`,
`stage` reads `active` (an undeclared column has neither flag), `/next`
passes the lane over and says so, `/integrity` lists it, and the way out is a
transition to a column that exists, which the universal edges always offer.
Adopted.

## 8. An epic with its own process and its own guidance

**Need.** One epic needs a law audit between plan and implementation, and
its own text for that step, without touching the rest of the workspace.

**Walk.**

1. In `.lit/workspace.toml`, under `[epics.rin]`: `columns.audit.next =
   ["impl"]` and `columns.plan.next = ["audit"]`. The same edit through the
   verbs is `set rin/columns/audit '{next:[impl]}' rin/columns/plan/next
   '[audit]'`, which edits the file and echoes that it did. The parse checks
   the resolved graph for `rin`'s tickets: one initial, terminal reachable,
   every `next` present. The change is committed and reviewed with the diff
   in view.
2. In the same section, a workflow `audit` with one handler: `on = ["enter
   audit", "in audit"]`, `body = "Run the law audit on the diff."`. Handlers
   resolve from the ticket up, so the body prints for `rin`'s tickets and no
   others.
3. `set rin.4ou/status audit` from `plan` lands; `set rin.4ou/status impl`
   from `plan` is refused, `plan/next = [audit]`.
4. `(status=audit)` matches `rin`'s tickets; `(stage=active)` matches them
   with every other in-flight ticket.
5. `set rin.4ou/parent other` while in `audit` is refused: `other`'s
   resolved graph has no `audit`.

**Relies on.** A process change is data on one node; no other ticket or
handler changed.

**Strain.** None.

## 9. A grooming pass at scale

**Need.** One session rewrites thirty descriptions, closes five, merges four,
and reorders, with every change visible before and after.

**Walk.**

1. `get '(status=backlog)/title' '(status=backlog)/lane'`: two lines per
   ticket, no epic bodies reprinted.
2. `set '(parent=5k5 status=backlog)/status' obsolete '(parent=5k5
   status=backlog)/reason' "iTerm2 restore dropped" --dry-run` prints the
   expansion and the diff; without the flag it is one write.
3. One path of three is refused (a ticket in `impl` cannot enter `done`);
   nothing is written for any of them; the refusal lists the two that would
   have landed.
4. `set lq3.9od/body - 2d80.d66/status dup 2d80.d66/of lq3.9od < merged.md`:
   the canonical body and the duplicate's close in one write.
5. Eleven different grooming notes on eleven tickets.

**Relies on.** Expansion once, all or nothing, dry run first, and the echoes
are the grooming report.

**Strain S-7.** Step 5 is eleven `add` calls because `add` takes one value.
`set` already takes `path value` pairs; `add` taking the same pairs removes
the asymmetry and no grammar: `add a/comments "…" b/comments "…"`. The
one-path-many-elements form stays a list value. Adopted.

**Strain S-8.** Six bodies from six files in one call: `-` reads stdin once.
The examples document notes it. Frequency in the transcripts is low and the
one-body form is the common case. Open; a `@file` value form is the natural
addition if it bites, and nothing in the grammar forecloses it.

## 10. Purge while others still write

**Need.** Content that must go is gone on every replica, and nothing that
happened around it is silently lost.

**Walk.**

1. A runs `rm 3aj --purge --dry-run`: the subtree, each item's comment and
   edge counts, the lane that will have no members. Then without the flag: a
   rewrite event on A's ref naming the items.
2. B, offline, had commented on `3aj.8k1` an hour earlier and comments again
   after. After sync, the fold drops every event whose subject is a purged id,
   both of B's comments included; `3aj.8k1/blocks/x` on another item folds to
   no edge; a mention in prose stays prose.
3. Each checkout, on its next write, rebuilds its own ref without the purged
   events and pushes the new head as a compare-and-swap. A receiver accepts
   the non-fast-forward move because the rewrite accounts for every missing
   event. Objects die at the next `gc`.

**Relies on.** One command, one echo, convergence everywhere; the purged id
is never reused because ids derive from writer and position.

**Strain S-9.** B's second comment was written to an item B still saw. It is
dropped, correctly, and B should learn that. The fold counts events whose
subject is purged and arrived after the purge, and `/integrity` lists them
with the writer, so B's next command names its own lost comment. Adopted.

## 11. A dead worktree holding a lane

**Need.** A lane whose holder vanished returns to the pool without a human
cleaning up, and a live holder is never robbed by a clock.

**Walk.**

1. Worktree W starts `9u9`, then is deleted. Its ref remains.
2. On the same machine, tick's liveness prune finds W absent in `git worktree
   list` and treats the hold as void now; `/next` on a sibling worktree serves
   the lane.
3. On another machine, the hold persists until `claim_ttl` has passed since
   W's last event, then prints as none. `get 9u9/claim` during the window
   shows the holder and the expiry.
4. Taking over before expiry is a deliberate write: `rm <lane>/claim`, echoed,
   then the transition.
5. A later sweep anchors W's ref under a live ref and deletes it; a W that
   reappears with commits past the anchor is alive and kept.

**Relies on.** No heartbeat, no release verb in the common path, and the
override of fresh evidence is always a visible act.

**Strain.** None beyond S-3, which applies here if the dead lane's head is
also blocked.

## 12. An old binary meets newer events

**Need.** A laptop running last month's lit2 neither corrupts nor misreads a
store that newer clients have written to.

**Walk.**

1. A newer client introduces an event kind, and the team raises
   `min-reader-version` in the workspace file.
2. The old binary fetches. It carries the unknown events byte for byte, folds
   the rest, and prints one line: `3 events from a newer lit2 (min reader
   vX) not applied`.
3. `/next` lists any lane with a member carrying unread events as
   unreadable and skips it. `set` on such an item is refused naming the
   version. Reads of other items proceed.
4. Once `min-reader-version` exceeds the binary's own, every command refuses
   with the version and the remedy.

**Relies on.** Failure to read is never treated as absence.

**Strain.** None.

## 13. Scale

**Need.** A store ten times the largest in the fleet answers every command
without wasted work.

**Walk.** 10,000 items, 200,000 events, 400 lanes. `get '*/title'` is one
walk of folded state. `/next` is a walk of the lane order, linear in lanes.
Tick evaluates each rule's selector over every ticket on every read, linear in
tickets times rules. Every read replays all 200,000 events and checks every
signature, so the codec and the fold's data structures make that one pass.

**Relies on.** A replay with no quadratic step.

**Strain S-10.** A `rank.place {above: y}` arriving after lane `y` was
dissolved or purged. The fold needs one rule: an intent whose anchor no
longer exists places the subject at the bottom of the order and
`/integrity` names the intent. Deterministic, loses nothing the writer could
have meant, and closes design.md's OPEN #2. Adopted.

## 14. A human at the terminal

**Need.** A person with no agent reads the backlog, reorders it, and fixes a
mistake.

**Walk.** `lit2` prints the root and the orientation handler. `lit2 --help`
is the verb table and the field table. `lit2 get /lanes` is the backlog in
served order. `lit2 set m2q/rank 1` reads as a position and compiles to the
relative intent; the echo shows the resulting order. `lit2 set '(…)'`
unquoted in zsh fails before lit2 runs, and the orientation text says to
quote. A wrong title is undone from its own echo. `--json` on one scalar
prints the scalar; on many paths, one object.

**Relies on.** The surface is one table; nothing is reachable only by a flag
the table does not show.

**Strain.** None.

## 15. Access control from the first event

**Need.** A contributor may file and work tickets but not delete epics or
other people's tickets; some tickets are readable only by some members; a
modified client cannot forge any of it; no server.

**Walk.**

1. `lit2 admin init` on a fresh repository mints the checkout's principal,
   prints one recovery phrase, and writes the enrollment: the first policy,
   one owner, one tier, encrypted, as the first signed event. Every event
   after it is signed and cites the policy version in force.
2. A second person clones, runs `lit2 admin init`, and the checkout finds
   the enrollment on the remote, adopts it, and pushes a signed membership
   request. The owner runs `add /members '{principal: <id>, role:
   contributor}'`; the proposal and its approval are one command because
   one owner is the threshold. The joiner's next sync wraps the tier's
   epochs to them and their backlog reads in full.
3. The contributor runs `rm <epic>`. The enforcer refuses it at the write:
   `rm own/*` at level 1 is the contributor's capability and this path is
   neither. The refusal names the capability it needed. A modified client
   that appends the event anyway publishes a segment every receiver refuses
   before folding; the local copies stay, `/integrity` names the writer and
   the event, and a valid replica re-publishes over the remote.
4. `set x/tier internal` on a ticket re-keys its current prose under the
   `internal` tier's epoch; the ticket's comments keep their own tiers. A
   member without the key sees the skeleton: id, column, lane, labels,
   links, and an envelope where the body is.
5. `rm /members/<id>` rotates every tier the member held to a new epoch for
   the survivors and says plainly that they keep what they could read.
   Their events after the removal's causal position fail verification; any
   they minted offline before it apply and are listed with their
   `recorded-at` for the owner to judge.
6. `get '(text~"powerd")'` on a reader without a key searches what it can
   decrypt and says how many envelopes it could not open.

**Relies on.** Signatures, envelopes and the policy are in the event shape
and the fold from the first event, so nothing is retrofitted. Every
capability is a verb on a path, so the policy vocabulary is the surface.

**Strain S-11.** A requirement whose `test` reads a prose field by content,
`description~'Done when'`, cannot be evaluated by a reader lacking the tier's
key. The writer always holds the key for prose it writes, so the transition
write is unaffected; `held`, computed by any reader, reports that
requirement as `unknown (tier internal)` rather than met or unmet. A new
value in the verdict's result type, not a branch. Adopted as a constraint in
architecture §8.

## 16. A fold-version bump across the fleet

**Need.** A release that changes a fold rule reaches fifty stores on one
machine without a slow morning.

**Walk.** Every read replays every event, so the first command in each store
folds under the new version with nothing to invalidate. Nothing runs at
install time.

**Relies on.** Every read being a replay: no folded state is kept between
commands.

**Strain.** None.

## What the scenarios did not reach

Listed so the gap is visible, not to rule on them: the Jira bridge as a
principal; witness cosigning; the user-layer workflow file's own concurrency
(it is one person's file); the inbox ref's abuse surface; the harness's
corpus construction. Each is a later scenario when its work is pulled.
