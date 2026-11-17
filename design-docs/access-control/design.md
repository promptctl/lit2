# Access control: the design

Status: draft. Answers [charter.md](charter.md); evidence in
[research/competitors.md](research/competitors.md) (cited below as *R§*) and
[research/lit-feature-surface.md](research/lit-feature-surface.md) (cited as
*F§*). Written 2026-08-25 against lit v1's store; the mechanics below are the
event store's, as [architecture.md](../architecture.md) §3.14 and §3.17–§3.20
carry them. The policy design — principals, capabilities, roles, tiers,
epochs, keys — is the one those sections state. Sections marked **OPEN** are
decisions a later session must close; everything else is a position this
document commits to.

## The shape in one paragraph

Two independent layers over the event store. The **write layer** signs every
event with its principal's key and puts the policy in the store as events;
every replica verifies a writer's published segments before accepting them,
and the fold applies no event whose signer lacked the capability for its
intent — enforcement by every verifier instead of any server, the model
Radicle proves viable over a git substrate (R II) and TUF supplies the role
machinery for (R IV). The **read layer** assigns each item and each comment a
named visibility tier; every prose field is an envelope encrypted under a
content key wrapped to the tier's current epoch key, and epoch keys are
wrapped to each member's public key in the policy — the SOPS recipient
pattern for grants (R IV), the Megolm/Sender-Keys epoch pattern for
revocation (R IV), key destruction for erasure (R III). The layers compose but
neither needs the other: the write layer alone meets the charter's RBAC bar;
the read layer alone meets the confidentiality bar. Both ship in the first
release ([requirements.md](../requirements.md) §1).

Two boundaries are fixed by physics, not by this design, and everything else
is stated in their terms:

- **Forgery is preventable; denial is only recoverable.** No client-side
  scheme can stop a holder of git push access from deleting or overwriting a
  writer's ref. What it can guarantee is that no such act ever becomes
  *accepted history*: honest replicas detect it, refuse it, and re-publish
  from any replica. (Monero's miners can censor; they cannot forge.)
- **Read grants are forward-only.** A key once given cannot be taken back for
  content it already covered. Revocation means new epochs for the survivors;
  every studied system — Megolm, Signal sender keys, SOPS, git-crypt — has
  exactly this property (R IV). Consequently there is no cryptographic Deny:
  the policy language is additive grants plus rotation, full stop (R V-f).

## 1. Write layer: signed events, verified policy

### Principals

A principal is a signing key of the crypto suite (architecture.md §7 E:
Ed25519 in the first suite, every signature naming its suite). Its public
key's fingerprint is the principal id — opaque, stable, and the only identity
form that enters the store. This extends the opaque-discriminator invariant
of [work-claims.md](../work-claims.md) to actor identity: every event's
attribution is the triple (checkout, principal, agent), where the checkout
id says *where* work happened, the principal *on whose authority*, and the
agent label is an optional opaque string the invoking harness supplies.
Human-readable names live in the policy's member entry (display name, key,
role), rendered at display time. A principal id is permanent, so key
rotation and device replacement need a **succession record** linking old
principal id to new — own-bindings and tier entitlements follow the link. Its
authorization has two tiers, because a succession is a takeover of
everything the old principal held: when the old key still exists, its own
signature plus one `member.admin` countersign suffices (the principal
consents); when the old key is claimed lost, the record requires the
**owner-set threshold** — the same quorum as owner rotation — because "their
key is gone, reassign it to this new key" is precisely the single-signer
takeover the threshold exists to stop. A succession for an *owner's* key
always requires the threshold. The record itself moves no key material —
epoch keys are wrapped per member public key (§2), so someone holding the
plaintext must actively re-wrap to the new key. Who does is split the same
way: in the consenting case the old key's holder re-wraps every epoch they
hold in the same write; in the lost-key case the threshold-signed record
*authorizes* the transfer, and each tier's restoration is then performed as
an ordinary grant by any current member of that tier — the signing owners
need not hold the keys themselves — with the new principal's access to a
tier pending until some member of it performs the re-wrap. A key lost with
no succession issued leaves the old principal's own-scoped rights stranded
until an owner reassigns them (a documented limit, §3).

Precedent: Radicle's did:key node identities (R II); the keyring-carries-
metadata pattern from SOPS (R IV).

### The policy document

The policy is the member list (principal, role, and the wrapped tier keys per
member — §2), the role definitions, and a monotonically increasing version.
It lives in the store as events: `policy.propose` carries the new document
and `policy.approve` cites its hash; a version activates at the causal
position where the approvals the previous version requires are all present.
Ordinary membership changes are signed by any principal holding the
`member.admin` capability — and "ordinary" is decided by the verifier, not
the signer's framing: **any diff to the set of principals holding the owner
role — add, remove, promote, demote — routes through the threshold check, no
exceptions.** A `member.admin` signature alone covers only non-owner rows; a
policy update that quietly adds a new owner under an ordinary signature is
invalid on its face. The same threshold gates **any change that widens a
role's capability bundle**, because role definitions live in this same
document and relabeling is otherwise a complete bypass: a single owner key
could grant "editor" the admin capabilities and promote an accomplice into
it, achieving owner-equivalent control without the owner set ever changing.
A lone `policy.admin` signature therefore covers only edits that widen
nothing — narrowing a bundle, renaming, reordering; the verifier decides
which by diffing the granted surface, not by trusting the change's framing,
exactly as with owner-set diffs. Changes to the owner set follow TUF root
rotation: the new owner list must carry signatures from a threshold of the
old owners **and** the new (R IV). The threshold defaults to 1 while the
owner set has one member — the solo case must stay frictionless, and a sole
owner has no quorum to defend anyway — and to a **majority of owners** the
moment it has more, so in a multi-owner workspace a single compromised owner
key cannot hand the owner set to itself by default, rather than only after
someone opts in. One asymmetry for the two verbs that strip an owner —
*removal* and *demotion*: the old-owner quorum excludes the owner being
removed or demoted — otherwise majority-of-2 is 2 and a two-owner workspace
could never shed, or even demote, a compromised or unresponsive owner. The
symmetric consequence is stated plainly rather than hidden: in a two-owner
workspace either owner can remove or demote the other, first mover wins; an
owner-versus-owner dispute is a human matter, and every replica retains the
pre-removal history. Setting the threshold explicitly remains available
(`set /policy/threshold 2`; the 120% primitive).

### Capabilities and roles

A capability is a triple: **(verb, path glob, binding)** — one of the four
verbs `get`, `set`, `add`, `rm`; a glob over paths; and a binding from the
declared enum **own | assignee | own-or-assignee | all**. Verb-split
permissions, own-vs-all splits, and relative principals are the three
dimensions that recur across the market's permission-rich products (R V-a).
`own` binds to the creating principal of the node the path names, an item
or a comment, read from its create event's attribution; `assignee` to the principal holding its lane's claim
([work-claims.md](../work-claims.md)); and the disjunction is first-class
because the market's dynamic principals are. The glob names the scope: a
subtree, a level, a field. "Can't delete epics" is `rm` granted on paths at
level 1 only, a path restriction, not an ownership one.

Every mutation is a path write, so every mutation is spelled by a capability
and nothing escapes the vocabulary: filing is `add */children`, a transition
is `set */status`, a comment is `add */comments`, re-tiering is `set */tier`,
membership is `add /members`, `set /members/*/role`, `rm /members/*`, the
threshold is `set /policy/threshold`, erasure is `rm * --purge` and
`rm * --destroy`, and a rewrite of history is the rewrite event the verb
mints. Bulk forms fan out to the per-path check, so a selector or a `--json`
list grants nothing the single write does not, and refusal of any path refuses
the whole. What cannot be spelled as a path write cannot be authorized, and a
path write with no capability does not ship (architecture.md §8).

Roles are named bundles of capabilities. Four defaults, matching the ladder
R V-a documents for the market's role-based products (ADO's namespace-ACL
model is the structural outlier there; the claim is R V-a's qualified one,
not restated stronger here), spelled as path globs in the default profile:

| Role | Capabilities (beyond the previous row) |
|---|---|
| observer | `get` (holds the tier keys granted to them; no write verbs) |
| contributor | `add */children`; `set */status` bound `all` (pulling the top of the shared queue regardless of creator *is* the agent workflow; a column is no capability's condition, since only the writer's file declares columns and no verifier could judge one, so the workflow gate holds where a status may move); `add own/*`; `set own/*`; `add */comments`; `rm own/comments/*`; `rm own/*` at level 1 |
| editor | `set */*` and `rm */*` bound `all` at level 1 and above; `set */rank`; `add */blocks`, `add */relates`; `set */tier` |
| owner | everything, including `rm * --purge`, `rm * --destroy`, `member.admin`, `policy.admin`, `set /policy/*` |

The charter's canonical example — a contributor who can create tickets but
cannot delete epics or others' tickets — is the contributor row: `own` on
`rm` withholds others' tickets, and the level-1 glob withholds epics even
from their own creator. (The two restrictions are independent, which is
exactly why the capability carries both a glob and a binding.) Note what
`rm x` means before it can be feared: a reversible tombstone. Irreversible
loss is the rewrite `rm x --purge`, and `rm x --destroy` shreds the content
key as well (§2), owner-only, mirroring the soft-delete/destroy split the
market enforces to varying depth — true recycle bins in three products,
harder-gated destruction nearly everywhere, GitLab the cautionary outlier
(R V-c's per-product breakdown).

Per-subtree delegation is a glob rooted at a node, the TUF delegated-paths
primitive (R IV); the default profile ships none.

### Enforcement

Every event is signed by its principal over the header, which holds a salted
hash of the payload, and carries the policy version its writer ran under.
Authorization is one fold rule (architecture.md §3.20): for each event in
total order the fold checks the signature against its principal's key and
the intent against the policy in force at the event's causal position; an
event that fails is carried, counted on `/integrity` with its writer, and
never applied. An event whose cited policy version is lower than its
writer's previous one fails the same way — monotonicity anchored per writer,
so a writer that has seen a newer policy cannot author under an older, more
permissive one afterward; a demoted principal's later events are judged
against the version in force at their position whatever they cite, which is
what makes the revocation bite.

Every receiver runs that rule over a writer's published segments as it
accepts them: a forged or unauthorized publication is held and its events
carried unapplied, so two clones holding the same events fold equal, and the
local copy stays; a valid replica re-publishes over a vandalized remote. The local fold is the rollback defense: a remote serving
an older-than-known or diverging history for a writer is refused, not
adopted (R IV). Witness cosigning against split-view (the remote showing
different members different histories) is acknowledged as the full CT-style
answer and deliberately deferred; the primitive slot exists, the feature
does not (120/80). To keep charter constraint 2 intact rather than quietly
loosened: a witness is split-view *detection*, never enforcement, and never
required — enforcement is client-side everywhere, always, and no lit feature
may depend on a witness existing.

**Enrollment — verification seeds at the first event.** The first policy is
the **enrollment**, the first event of a workspace, signed by its founding
owner at `lit2 admin init`; verification seeds there and never walks before
it, and a fresh workspace has no history before it. A checkout that joins a
remote already holding an enrollment adopts it and pushes a signed
membership request. Two enrollments meeting on one remote — two unsynced
clones each initializing — is a contradiction the fold reports; the later one
in total order yields, and its principal re-requests membership.

A revoked principal that stays offline from its revocation can mint events
whose positions sort before it; the fold applies them, as it applies any
offline work made before the news arrived, and reports them with their
`recorded-at` so an owner can judge and rewrite. That residual is a
deterministic rule plus an advisory, never a rule keyed to what a receiver
happens to know.

What the write layer deliberately does not do: prevent a pusher from
destroying a remote ref (recoverable, per the fixed boundary above), or
police reads (that is the read layer's job — a signature never hid anything).

## 2. Read layer: tiers, epochs, wrapped keys

### The granularity decision

The unit of *policy* is the tier on an item or comment; the unit of
*encryption* is the prose field. Concretely:

- Every item carries `tier`, a structural field. The tier governs the item's
  prose fields: `title`, `body`, `prompt`, every plan revision, every
  checklist item's text, and the reasons on its transitions and checklist
  states.
- Every comment carries its own `tier`, defaulting to its item's. So the
  charter's question — can a single comment be encrypted? — has a clean
  answer: **yes, because a comment is an element with its own tier**, and
  per-comment restriction is exactly the market's demand ceiling: Jira
  restricted comments, GitLab internal notes (R V-b). Nothing finer exists
  anywhere, and nothing finer is designed here.
- Structural fields — ids, status, level, rank, lane, timestamps, links,
  labels — stay plaintext. **A tier hides content, not existence.** Every
  workspace member sees that a ticket exists, where it ranks, and how it
  relates; only tier members read what it says.

The visible-skeleton rule is the load-bearing simplification, and it is what
keeps the strain points from becoming redesign-everything: claims derivation
reads only structural fields and whole-set evidence, so it works unchanged;
rank stays one order; lane sequencing and stage derivation keep reading full
member and child sets; the backlog lists every item and redacts the titles
the reader cannot open. Existence-hiding would break all four at once and is
explicitly out of scope — a workspace whose ticket *existence* is secret from
some members is two workspaces, and the one-repo constraint (charter #1)
already tells us that boundary is the repo itself.

Ciphertext envelope: `{suite, tier, epoch, nonce, ciphertext, commitment}`,
the form every prose field takes from its first event
(`INV:opaque-envelope`). The nonce is fresh per encryption. The **encryption
key is the item's or comment's content key** — the independent randomness the
destroy scheme below requires, stored wrapped under the tier's epoch key
named by `epoch`; the wrapped copy rides the item or comment, not each
envelope, so the envelope format needs no key slot of its own. It is never
an HKDF derivation of the epoch key — the destroy scheme below says why. HKDF
derivation from the epoch key is restricted to one job: the **commitment** —
a deterministic keyed hash of the plaintext, its key a domain-separated
subkey of **the epoch key in force at write time** — present so that
equality of content is decidable without decryption, which is what lets the
fold decide that two concurrent writes of one prose field are the same text
without reading either. Keying the commitment to the epoch is a deliberate
choice between two defects: a stable per-tier commitment key would survive
rotation but hand every removed member a permanent oracle for testing
plaintext guesses against *future* content, while an epoch-keyed commitment
merely loses equality detection *across* a rotation — so a concurrent edit
pair straddling a membership change can surface a spurious prose conflict,
rare (rotations happen on membership change) and safe (a keyholder confirms
the texts match). Confidentiality wins over a rare liveness annoyance. The
remaining cost: anyone can see that two same-epoch encrypted values are equal
(never what they are) — the same trade git-crypt makes for diffability
(R IV), paid here only at the granularity of whole field values. The envelope
is opaque bytes to every layer below the verbs — to the fold, to the store,
to sync — which is what makes the sync story below hold.

### Keys, grants, revocation, erasure

Per tier, a symmetric **epoch key**. Every epoch's key — current and
historical — is wrapped to each entitled member's public key in the policy
(SOPS recipient pattern, R IV). Granting read wraps **all of the tier's
epoch keys** to the new principal, so a joiner reads the tier's whole
existing backlog, which is what the onboarding flow (§5) promises;
revocation is asymmetric by nature — leavers keep old epochs, joiners get
them — and only the *future* is partitioned by rotation. This is the Monero
view-key separation: read capability granted without any write capability.
The converse also holds here — a CI principal can hold write verbs and zero
tier keys — though that half is this design's own key-type independence,
not a Monero precedent (Monero's spend side needs the view key in practice).
Removing a member rotates the tier to a new epoch wrapped to survivors only
(Megolm / Sender-Keys, R IV); old epochs are not re-encrypted, and the
removed member's continued ability to read what they could already read is
documented, not hidden. `rm x --destroy` is crypto-shredding **content
keys**, and a content key must be **independent randomness generated at
creation, stored wrapped under the tier's epoch key**, never an HKDF
derivation from the epoch key: a derived subkey is a deterministic public
function of a secret every tier member still holds, so "destroying" it
destroys nothing — any member recomputes it on demand. The unit is the item
or the comment, because comments tier independently of their item (§2
granularity): a comment encrypted under its *item's* key would be readable by
item-tier members whatever the comment's own tier said, so **each encrypted
item and every one of its comments carries its own content key**, and
destroying an item shreds the item's key *and all of its comments' keys*,
whatever tiers those comments sit in. Nothing of a destroyed item's content
survives in recoverable form; destroy is a rewrite that drops the wrapped
copies on every replica, and no holder of any epoch key can reconstruct
them. Two honest residuals. First, a replica that never syncs again keeps
what it had (event-store charter #7). Second, the envelope's commitment
outlives the shred and its key is an epoch subkey members still hold, so a
tier member can test a *guessed* plaintext against a destroyed field forever
— destroy removes recovery, not confirmation of a guess. This is the GDPR
Art. 17 and ITAR §120.54 story (R III), stated with its actual boundary.

Every workspace has a default tier that all members hold. **The default
posture for a new workspace is: one tier, encrypted, all members entitled.**
That single decision is what separates "repo reader" from "workspace member"
by default — the charter's adoption complaint — while costing a member
nothing: they hold the key, everything decrypts, lit looks exactly as it
would in plaintext. A project that wants its backlog world-readable (lit's
own repo does) sets the default tier to plaintext; that is the documented
public mode, not the default.

### Sync, merge, and the tier-blind replica

The rule that makes the whole read layer survive sync: **ciphertext is the
synced representation.** Encryption and decryption happen at the verb
boundary, never in the fold, the store, or sync. Consequences:

- A replica lacking a tier key folds the same state as one holding it, so
  nothing structural depends on being able to read; the fold never mistakes
  unreadable for absent.
- The fold compares envelope **commitments**, not envelope bytes — fresh
  nonces make byte comparison see divergence in two independent writes of
  the *same* text (two agents fixing the same typo would conflict forever).
  Equal commitments are one value; a genuinely divergent pair of concurrent
  prose writes is a conflict value on the field, resolved by a write from a
  principal holding the tier. A checkout that cannot read the conflicted tier
  cannot resolve it — it reports which tier is needed and leaves the conflict
  for a keyholder. An agent that can't read a ticket has no business merging
  its prose.
- A selector atom over a prose field by content is evaluated by its writer,
  who holds the key; a reader without it sees `unknown (tier)`, a third value
  of the verdict type, never a guess (architecture.md §8). Searchable
  encryption is a wheel this design refuses to invent (charter #5).

### What the model had to give

Charter rule 4 applied — places where lit's model, not the security model,
yielded:

1. **Ids are opaque.** An id is random bits minted at the create and carried
   in the create event, rendered by its segment under the project prefix
   (architecture.md §3.2); nothing of a title, a topic or a creator is
   embedded in it.
2. **Actor identity is the principal.** Attribution is (checkout, principal,
   agent); `own` reads the create event's principal, and no username or
   session id enters the store.
3. **No reader of a store from outside its checkout.** Every open goes through
   the checkout's own principal and the tier keys it holds; there is no
   filesystem-path trust path.
4. **`assignee` is the lane's claim holder.** The binding reads the derived
   claim (work-claims.md), so an agent who pulled a ticket it did not create
   answers to `assignee` with nothing written.

## 3. Foundational hard requirements

From the compliance analysis (R III, R V-f), split as the charter demands.

**Cannot be disabled** (present in every workspace, including solo plaintext
ones — these are what make the audit trail trustworthy at all):

- Unique cryptographic principal per actor (HIPAA 164.312(a) unique user
  identification, Required).
- Signed events and receive-time verification (NIST AC-3: enforcement must
  be technical, not procedural).
- The append-only signed history itself — the event log under signatures is
  the AU-9-protected audit log, and a stronger one than the incumbents'
  (GitHub: 180 days, no issue-create events; GitLab: no issue events at all —
  R I).
- Policy-version monotonicity per writer, and the refusal of a remote
  serving a writer's history older than, or diverging from, the one already
  verified.
- The tombstone-before-destroy ordering: no verb erases content that a lower
  verb couldn't recover, except `rm x --destroy`, which is owner-gated and
  logged.

**Can be configured or disabled:**

- Encryption itself (a public project runs a plaintext default tier).
- The role set and every capability binding (the four roles are defaults,
  not law).
- Owner-set threshold (defaults to 1 solo, majority once multi-owner).
- Witness cosigning, when it ships (detection only — enforcement never
  depends on it, per charter constraint 2).

**Documented limits, never papered over:** forward-only revocation; denial
recoverable-not-preventable; a removed member retains what they could read;
metadata (existence, structure, timing, actor principal ids) is visible to
every repo reader even under full encryption, as is equality of two
encrypted values (the commitment trade, §2); narrowing an item's tier
restricts only its future — every prior member of the old, broader tier
keeps the keys to its pre-narrowing history (§6 item 4); widening an item's
tier grants only its present — the destination tier's members read the
item's current field values, while its pre-widening history stays under the
old tier's keys (§6 item 4 again, the same forward-only coin from the other
side); and a member who loses a signing key without a succession record (§1)
leaves their own-scoped rights attached to the old principal until an owner
reassigns them. Compliance officers get these in writing; the design's
honesty here is a feature (R III's regimes all demand accurate control
descriptions, not perfect controls).

## 4. Forward constraints on lit's evolution

The charter asks not for feature prediction but for the boundaries this
design imposes. A future lit change stays compatible iff:

1. **New prose fields go through the envelope.** Any field that can carry
   user prose is an envelope from its first event.
2. **New mutating verbs on a path have a capability.** The policy vocabulary
   is `(verb, path glob, binding)`; a path write without a capability cannot
   ship.
3. **Shared structure may not depend on another tier's plaintext.** Rank,
   lanes, claims, stage — anything computed across items reads structural
   fields only. A feature needing cross-tier *content* is misdesigned.
4. **The synced representation is the protected representation.** No layer
   below the verbs ever sees plaintext; every escape hatch inherits
   protection for free.
5. **No feature may require a server.** Anything needing central state is
   out — or it is a witness: an optional external observer that detects
   equivocation and enforces nothing, which is why it does not breach the
   charter's no-server constraint. Enforcement never moves off the client.
6. **Structural fields are workspace-public by definition**, so adding a
   structural field is adding metadata every repo reader can see; that
   trade-off gets named in the adding change's design, every time.
7. **Detached processes get non-interactive key access.** The background
   sync runs unattended; key material lives once per clone beside the store
   and outside it (architecture.md §6), never behind a prompt. A future
   feature that would need an interactive unlock is misdesigned.

The known future directions survive these rules: a Jira bridge
([project-intent.md](../project-intent.md)) is a principal with an explicit
role and tier grants.

## 5. User-facing shape (broad strokes)

The transparency bar is Monero's: correct use requires zero cryptographic
knowledge. What that means concretely:

- **Solo user:** `lit2 admin init` mints the owner principal silently and
  prints one thing: a recovery phrase, with one sentence saying what losing
  it costs. Every command thereafter looks exactly like plaintext. This is
  the entire solo experience.
- **Adding someone:** the joiner runs `lit2 admin init` on their clone; it
  adopts the enrollment it finds, pushes a signed membership request, and
  says "ask the owner to approve." The owner runs
  `add /members '{principal, role}'`. Key exchange, wrapping, and epoch
  handling are invisible.
- **Restricting something:** `set x/tier internal`, on an item or a comment.
  Members of the tier see nothing different; others see the skeleton with
  redacted text.
- **Removing someone:** `rm /members/<id>` — rotation happens; the output
  states plainly that they keep what they could already read.

Burdens, honestly: one recovery phrase to store; an approval step when
someone joins; conflicts in restricted prose resolved only on machines
holding the tier. Risks, honestly: **key loss is the data loss** — mitigated
by every tier key being held by all its members (any one survivor re-wraps
for a recovered owner) and by the recovery phrase; the genuinely fatal case
is a sole member losing both key and phrase, which is the same fatality as
losing a password-manager vault, and is what the printed phrase exists to
prevent. Rollback or vandalism of the remote surfaces as a loud refusal with
a one-command repair (re-publish from any replica), not as silent divergence.

## 6. What the next sessions must close

1. **OPEN:** policy growth. The policy is O(members × epochs-ever-minted)
   with wrapped keys for every historical epoch, carried by every replica, so
   its form must state a bound or a compaction strategy (or argue concretely
   why churn keeps it small).
2. **OPEN:** the joiner-request flow's abuse surface (an unauthenticated
   inbox push as a membership request is a spam channel; probably fine,
   needs a look).
3. **OPEN:** verification cost at receive time on long histories
   (incremental verification from the snapshot should make steady-state
   cheap; the first clone is the expensive one).
4. `set x/tier` in both directions. Position: **narrowing** is future-only —
   existing history stays under the old tier, stated plainly, consistent
   with forward-only everything. **Widening** mints a fresh content key for
   the item, re-encrypts its *current* field values under it, and wraps it
   to the destination tier's epoch key (one item's worth of work, cheap),
   because "make this visible to more people" that leaves the content
   unreadable to them is a lie; the old content key and the *history* it
   encrypts stay under the old tier — re-wrapping the existing key would
   expose the history too — and this is documented as such (§3's limits
   list). **OPEN:** the widening write must be performed by a principal
   holding both tiers; what a checkout holding one of them sees when it is
   asked.
