# Access control: the design

Answers [charter.md](charter.md); evidence in
[research/competitors.md](research/competitors.md) (cited below as *R§*) and
[research/lit-feature-surface.md](research/lit-feature-surface.md) (cited as
*F§*). Sections marked **OPEN** are undecided.

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
wrapped to each member's encryption key in the policy — the SOPS recipient
pattern for grants (R IV), the Megolm/Sender-Keys epoch pattern for
revocation (R IV), key destruction for erasure (R III). Both ship in the
first release ([requirements.md](../requirements.md)).

Two boundaries frame the rest:

- **Forgery is preventable; denial is only recoverable.** No client-side
  scheme can stop a holder of git push access from deleting or overwriting a
  writer's ref. What it can guarantee is that no such act becomes
  *accepted history*: honest replicas detect it and refuse it.
- **Read grants are forward-only.** A key once given cannot be taken back for
  content it already covered. Revocation means new epochs for the survivors,
  as in Megolm, Signal sender keys, SOPS and git-crypt (R IV). Consequently
  there is no cryptographic Deny: the policy language is additive grants plus
  rotation (R V-f).

## 1. Write layer: signed events, verified policy

### Principals

A principal is a signing key of the crypto suite (architecture.md §7 E:
Ed25519 in the first suite, every signature naming its suite). Its public
key's fingerprint is the principal id — opaque, stable, and the only identity
form that enters the store. Every event's
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
epoch keys are wrapped per member encryption key (§2), so someone holding the
plaintext must actively re-wrap to the new key. Who does is split the same
way: in the consenting case the old key's holder re-wraps every epoch they
hold in the same write; in the lost-key case the threshold-signed record
*authorizes* the transfer, and each tier's restoration is then performed as
an ordinary grant by any current member of that tier — the signing owners
need not hold the keys themselves — with the new principal's access to a
tier pending until some member of it performs the re-wrap. A key lost with
no succession issued leaves the old principal's own-scoped rights stranded
until an owner reassigns them (a documented limit, §3).

**A principal signs; it never decrypts.** Each member entry also carries
an **encryption key**: a separate X25519 key, signed by the principal, and
the key tier keys are wrapped to (§2). This is the OpenPGP
encryption-subkey and Signal signed-prekey split. The two secrets are held
in separate files, so a process can hold a member's read without its
signing authority. The signing key can sit behind a keychain or token while
reads go on. The encryption key rotates with a new signed entry, and the
principal's identity does not change. The recovery phrase restores both,
each derived from the phrase's seed under its own domain-separation label.

**Delegated keys.** A principal can delegate a signing key. The delegation
is a signed record that names the delegate's public key, a capability set,
and an expiry. The capability set is a subset of the delegator's own
(verb, glob, binding) triples. The record is an ordinary write by the
delegator under `/members/<its id>/delegations`, and it grants nothing the
delegator lacks, so it needs no approval. An event signed by a delegate
cites its delegation. The fold applies the event only if it falls inside
both the delegation's scope and the delegator's capabilities at the event's
position. Attribution is the delegator, with the delegate key recorded
beside it, so `own` follows the delegator. The delegation ends at its
expiry, or earlier when the delegator or a `member.admin` removes it.
Neither rotates anything, because a delegate holds no tier keys by default.
A delegate that must read gets its own encryption key and an ordinary read
grant to it. Like every read grant, that one is forward-only: expiry ends
the delegate's writes, and only rotation ends its reads of new content.
A principal is one per person, not per clone: each clone signs with a key
its person's principal delegates to it, and a checkout is told apart by its
checkout id. A contributor can also be given a key scoped to one ticket's
subtree. Precedent: OpenSSH
certificates (short-lived keys signed by a holder, with principals and
expiry) and TUF delegated roles (R IV).

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
role — add, remove, promote, demote — routes through the threshold check.** A `member.admin` signature alone covers only non-owner rows; a
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
moment it has more. One asymmetry for the two verbs that strip an owner —
*removal* and *demotion*: the old-owner quorum excludes the owner being
removed or demoted — otherwise majority-of-2 is 2 and a two-owner workspace
could never shed, or even demote, a compromised or unresponsive owner. Setting the threshold explicitly remains available
(`set /policy/threshold 2`; the 120% primitive).

### Capabilities and roles

A capability is a triple: **(verb, path glob, binding)** — one of the verbs
`get`, `set`, `add`, `rm`; a glob over paths; and a binding, currently
**own | assignee | own-or-assignee | all**. A `get` denial is access policy
that clients enforce; the tier keys (§2) are the cryptographic boundary on
reads. Verb-split
permissions, own-vs-all splits, and relative principals are the three
dimensions that recur across the market's permission-rich products (R V-a).
`own` binds to the creating principal of the node the path names, an item
or a comment, read from its create event's attribution; `assignee` to the principal holding its lane's claim
([work-claims.md](../work-claims.md)); and the disjunction is first-class
because the market's dynamic principals are. The glob names the scope: a
subtree, a level, a field. "Can't delete epics" is `rm` granted on paths at
level 1 only, a path restriction, not an ownership one.

Every mutation needs a capability: filing is `add */children`, a transition
is `set */status`, a comment is `add */comments`, re-tiering is `set */tier`,
membership is `add /members`, `set /members/*/role`, `rm /members/*`, the
threshold is `set /policy/threshold`, erasure is `rm * --purge` and
`rm * --destroy`, and a rewrite of history is the rewrite event the verb
mints. Bulk forms fan out to the per-path check, so a selector or a `--json`
list grants nothing the single write does not, and refusal of any path refuses
the whole. A mutating write with no capability does not ship
(architecture.md §8).

Roles are named bundles of capabilities. Four defaults, matching the ladder
R V-a documents for the market's role-based products (ADO's namespace-ACL
model is the structural outlier there; the claim is R V-a's qualified one,
not restated stronger here), spelled as path globs in the default profile:

| Role | Capabilities (beyond the previous row) |
|---|---|
| observer | `get` (holds the tier keys granted to them; no write verbs) |
| contributor | `add */children`; `set */status` bound `all` (pulling the top of the shared queue regardless of creator *is* the agent workflow; a move carries the fields its column's entry needs, so this is also who may close, architecture.md §3.18); `add own/*`; `set own/*`; `add */comments`; `rm own/*/*`; `rm own/*` at level 1 |
| editor | `set */*` and `rm */*` bound `all` at level 1 and above; `set */rank`; `add */blocks`, `add */relates`; `set */tier` |
| owner | everything, including `rm * --purge`, `rm * --destroy`, `member.admin`, `policy.admin`, `set /policy/*` |

The RBAC goal in [requirements.md](../requirements.md) — a contributor who can create tickets but
cannot delete epics or others' tickets — is the contributor row: `own` on
`rm` withholds others' tickets, and the level-1 glob withholds epics even
from their own creator. In the default roles, `rm x` is a reversible
tombstone. Irreversible loss is the rewrite `rm x --purge`, and
`rm x --destroy` shreds the content key as well (§2), owner-only by default,
mirroring the soft-delete/destroy split the
market enforces to varying depth — true recycle bins in three products,
harder-gated destruction nearly everywhere, GitLab the cautionary outlier
(R V-c's per-product breakdown).

Per-subtree delegation is a glob rooted at a node, the TUF delegated-paths
primitive (R IV); the default profile ships none.

### Enforcement

Every event is signed, by its principal or a key the principal delegated,
over the header, which holds a salted hash of the payload, and carries the
policy version its writer ran under. Authorization is the capability rule in
the fold, which the writer also runs before appending (architecture.md
§3.20): for each event in
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
local copy stays. The local fold is the rollback defense: a remote serving
an older-than-known or diverging history for a writer is refused, not
adopted (R IV).

**Enrollment — verification seeds at the founding.** The first policy is
the **enrollment**, signed by its founding owner at `lit2 admin init`;
verification seeds there and never walks before it. The clone names which
enrollment founds its store, never the events, since any writer can mint
one that sorts first: the founding init names its enrollment in the
committed `.lit/founding` and pins it beside the clone's key, and a clone
that joins adopts the one `.lit/founding` names, pins it once, and pushes a
signed membership request. Every fold is seeded with the pin; any other
enrollment, such as one of two unsynced clones each initializing, is
carried, and its principal requests membership of the store the file names.

A revoked principal that stays offline from its revocation can mint events
whose positions sort before it; the fold applies them, as it applies any
offline work made before the news arrived, and reports them with their
`recorded-at` so an owner can judge and rewrite. That residual is a
deterministic rule plus an advisory, not a rule keyed to what a receiver
happens to know.

## 2. Read layer: tiers, epochs, wrapped keys

### The granularity decision

The unit of *policy* is the tier on an item or comment; the unit of
*encryption* is the prose field. Concretely:

- Every item carries `tier`, a structural field. The tier governs the item's
  prose fields: `title`, `body`, `prompt`, every plan revision, every
  checklist item's text, and the reasons on its transitions and checklist
  states.
- Every comment carries its own `tier`, defaulting to its item's, so **a
  single comment can be encrypted**. Per-comment restriction is the market's
  demand ceiling: Jira restricted comments, GitLab internal notes (R V-b).
- Structural fields — ids, status, level, rank, lane, timestamps, links,
  labels — stay plaintext. **A tier hides content, not existence.** Every
  workspace member sees that a ticket exists, where it ranks, and how it
  relates; only tier members read what it says.

The visible skeleton keeps cross-item features working: claims derivation
reads only structural fields and whole-set evidence; rank stays one order;
lane sequencing and stage derivation keep reading full member and child sets;
the backlog currently lists every item and redacts the titles the reader
cannot open.

Ciphertext envelope: `{suite, tier, epoch, nonce, ciphertext, commitment}`,
the form every prose field takes from its first event
(`INV:opaque-envelope`). The nonce is fresh per encryption. The **encryption
key is the item's or comment's content key** — the independent randomness the
destroy scheme below requires, stored wrapped under the tier's epoch key
named by `epoch`; the wrapped copy rides the item or comment, not each
envelope, so the envelope format needs no key slot of its own. HKDF
derivation from the epoch key is used for the **commitment** —
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
the texts match). The remaining cost: anyone can see that two same-epoch encrypted values are equal
(never what they are) — the same trade git-crypt makes for diffability
(R IV), paid here only at the granularity of whole field values. The envelope
is opaque bytes to every layer below the verbs — to the fold, to the store,
to sync — which is what makes the sync story below hold.

### Keys, grants, revocation, erasure

Per tier, a symmetric **epoch key**. Every epoch's key — current and
historical — is wrapped to each entitled member's encryption key (§1) in the
policy (SOPS recipient pattern, R IV). Granting read wraps **all of the tier's
epoch keys** to the new member's encryption key, so a joiner reads the tier's whole
existing backlog, which is what the onboarding flow (§5) promises;
revocation is asymmetric by nature — leavers keep old epochs, joiners get
them — and only the *future* is partitioned by rotation. Read capability is
granted without any write capability. The converse also holds: a CI principal
can hold write verbs and zero tier keys. Both halves hold within one principal as well as
between principals, because its signing key and its encryption key are
separate secrets (§1).
Removing a member rotates the tier to a new epoch wrapped to survivors only
(Megolm / Sender-Keys, R IV); old epochs are not re-encrypted, and the
removed member's continued ability to read what they could already read is
a documented limit (§3). `rm x --destroy` is crypto-shredding **content
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
whatever tiers those comments sit in. Destroy is a rewrite that drops the
wrapped copies on every replica that syncs, and no holder of any epoch key
can reconstruct them. Two residuals. First, a replica that never syncs again
keeps what it had (the event-store design's erasure reaches). Second, the envelope's commitment
outlives the shred and its key is an epoch subkey members still hold, so a
tier member can test a *guessed* plaintext against a destroyed field forever
— destroy removes recovery, not confirmation of a guess. This is the GDPR
Art. 17 and ITAR §120.54 story (R III).

Every workspace has a default tier that all members hold. **The default
posture for a new workspace is: one tier, encrypted, all members entitled.**
A project that wants its backlog world-readable (lit's
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
  for a keyholder.
- A selector atom over a prose field by content is evaluated by its writer,
  who holds the key; a reader without it sees `unknown (tier)`, a third value
  of the verdict type, never a guess (architecture.md §8).

### What the model had to give

Places where lit's model, not the security model, yielded:

1. **Ids are opaque.** An id is random bits minted at the create and carried
   in the create event, rendered by its segment under the project prefix
   (architecture.md §3.2); nothing of a title, a topic or a creator is
   embedded in it.
2. **Actor identity is the principal.** Attribution is (checkout, principal,
   agent); `own` reads the create event's principal, and no username or
   session id enters the store.
3. **A store is opened through its principal.** Every open goes through the
   principal the clone signs for and the tier keys it holds.
4. **`assignee` is the lane's claim holder.** The binding reads the claim
   holder the fold computes (work-claims.md), so an agent who pulled a ticket
   it did not create answers to `assignee`.

## 3. Foundational hard requirements

From the compliance analysis (R III, R V-f).

**Cannot be disabled** (present in every workspace, including solo plaintext
ones):

- Unique cryptographic principal per actor (HIPAA 164.312(a) unique user
  identification, Required).
- Signed events and receive-time verification (NIST AC-3: enforcement must
  be technical, not procedural).
- History changes only through signed rewrite events — the event log under
  signatures is the AU-9-protected audit log (R I).
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

**Documented limits:** forward-only revocation; denial
recoverable-not-preventable; a removed member retains what they could read;
metadata (existence, structure, timing, actor principal ids) is visible to
every repo reader even under full encryption, as is equality of two
encrypted values (the commitment trade, §2); narrowing an item's tier
restricts only its future — every prior member of the old, broader tier
keeps the keys to its pre-narrowing history (§6, `set x/tier`); widening an item's
tier grants only its present — the destination tier's members read the
item's current field values, while its pre-widening history stays under the
old tier's keys (§6, `set x/tier`); and a member who loses a signing key without a succession record (§1)
leaves their own-scoped rights attached to the old principal until an owner
reassigns them.

## 4. Forward constraints on lit's evolution

A future lit change stays compatible iff:

1. **New prose fields go through the envelope.** Any field that can carry
   user prose is an envelope from its first event.
2. **New mutating verbs have a capability** (§1, Capabilities and roles).
3. **Shared structure does not depend on another tier's plaintext.** Rank,
   lanes, claims, stage — anything computed across items currently reads
   structural fields only.
4. **Structural fields are workspace-public by definition**, so adding a
   structural field is adding metadata every repo reader can see; that
   trade-off gets named in the adding change's design, every time.
5. **Key material stays out of the store.** It lives in each clone beside
   the store and outside it (architecture.md §6).

The known future directions survive these rules: a Jira bridge
([project-intent.md](../project-intent.md)) is a principal with an explicit
role and tier grants.

## 5. User-facing shape (broad strokes)

The charter's transparency constraint, concretely:

- **Solo user:** `lit2 admin init` mints the owner principal silently and
  prints a recovery phrase, with one sentence saying what losing it costs.
  Key handling is otherwise invisible, and commands look as they would with
  plaintext.
- **Adding someone:** the joiner runs `lit2 admin init` on their clone; it
  adopts the enrollment it finds, pushes a signed membership request, and
  says "ask the owner to approve." The owner runs
  `add /members '{principal, role}'`. Key exchange, wrapping, and epoch
  handling are invisible.
- **Restricting something:** `set x/tier internal`, on an item or a comment.
  Members of the tier see nothing different; others see the skeleton with
  redacted text.
- **Removing someone:** `rm /members/<id>` — rotation happens; the output
  says that they keep what they could already read.

Burdens: one recovery phrase to store; an approval step when
someone joins; conflicts in restricted prose resolved only on machines
holding the tier. Risks: **key loss is the data loss** — mitigated
by every tier key being held by all its members (any one survivor re-wraps
for a recovered owner) and by the recovery phrase; the genuinely fatal case
is a sole member losing both key and phrase, which is the same fatality as
losing a password-manager vault, and is what the printed phrase exists to
prevent.

## 6. Open questions and the tier change

1. **OPEN:** policy growth. The policy is O(members × epochs-ever-minted)
   with wrapped keys for every historical epoch, carried by every replica, so
   its form must state a bound or a compaction strategy (or argue concretely
   why churn keeps it small).
2. `set x/tier` in both directions. Position: **narrowing** is future-only —
   existing history stays under the old tier, consistent with forward-only
   read access. **Widening** mints a fresh content key for
   the item, re-encrypts its *current* field values under it, and wraps it
   to the destination tier's epoch key (one item's worth of work, cheap),
   because a widening that leaves the content unreadable to the new readers
   widens nothing; the old content key and the *history* it
   encrypts stay under the old tier — re-wrapping the existing key would
   expose the history too — and this is documented as such (§3's limits
   list). **OPEN:** the widening write must be performed by a principal
   holding both tiers; what a checkout holding one of them sees when it is
   asked.
