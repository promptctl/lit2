# Identity, access and encryption

Carries out `strategy/authority-and-visibility.md`.

## Identity

Every event's attribution is `(checkout, principal, agent)`:

- **checkout**: the 128-bit token in the checkout's private git dir, which
  claims and writer refs key on. It says where the work happened.
- **principal**: the person on whose authority the event is written. There
  is one principal per person, not per clone.
- **agent**: an optional opaque label the invoking harness supplies, shown
  beside comments and history.

The principal id is the fingerprint of the principal's public signing key:
opaque, stable, and the only identity form that enters the store. No
username, hostname or session id is stored. Display names live in the
policy's member entries and are rendered at display time. `own` reads the
principal on a node's create event. Ids carry nothing of a title, topic or
creator (`specifications/events-and-store.md`, "Ids").

`get /me` prints the three and the tiers this principal holds.

A store is opened through the principal the clone signs for and the tier
keys it holds. Key material lives once per clone under `<git-common-dir>/lit/`,
beside the store's root and outside it, never in the store; the signing key
and the encryption key are separate files.

## Crypto suite

Go's standard library and its extended crypto packages: Ed25519 for
signatures, X25519 for key wrapping, each its own key and never one
converted into the other, ChaCha20-Poly1305 for envelopes, HKDF for the
commitment subkey. The age file format is the reference design. The suite
is replaceable: every signature and every envelope names its suite, so a
second suite can coexist with the first without a schema change.

## Principals and keys

**Signing key.** A principal is a signing key of the suite. Every event is
signed, from the first event of every store, by its principal or by a key
the principal delegated (the signed header is
`specifications/events-and-store.md`, "Event model").

**Encryption key.** A principal signs; it never decrypts. Each member entry
also carries a separate X25519 encryption key, signed by the principal, and
tier keys are wrapped to it. This is the OpenPGP encryption-subkey and
Signal signed-prekey split. Because the two secrets are separate files, a
process can hold a member's read without its signing authority, and the
signing key can sit behind a keychain or token while reads go on. The
encryption key rotates by a new signed entry; the principal's identity does
not change.

**Recovery phrase.** `lit2 admin init` sets up the keys silently and prints
one recovery phrase, with one sentence saying what losing it costs. The
phrase restores both keys, each derived from the phrase's seed under its
own domain-separation label.

**Delegated keys.** A principal can delegate a signing key. The delegation
is a signed record naming the delegate's public key, a capability set and
an expiry. The capability set is a subset of the delegator's own
`(verb, glob, binding)` triples. It is an ordinary write by the delegator
under `/members/<its id>/delegations`, and since it grants nothing the
delegator lacks, it needs no approval.

- An event signed by a delegate cites its delegation. The fold applies it
  only if it falls inside both the delegation's scope and the delegator's
  capabilities at the event's position.
- Attribution is the delegator, with the delegate key recorded beside it,
  so `own` follows the delegator.
- The delegation ends at its expiry, or earlier when the delegator or a
  `member.admin` holder removes it. Neither rotates anything, because a
  delegate holds no tier keys by default. A delegate that must read gets
  its own encryption key and an ordinary read grant, which is forward-only
  like every read grant: expiry ends its writes, and only rotation ends its
  reads of new content.
- Each clone signs with a key its person's principal delegates to it; a
  checkout is told apart by its checkout id. A contributor can also be
  given a key scoped to one ticket's subtree.

Precedent: OpenSSH certificates (short-lived keys signed by a holder, with
principals and expiry) and TUF delegated roles.

**Succession.** A principal id is permanent, so key rotation and device
replacement go through a succession record linking the old principal id to
the new: `add /members/<id>/succession <new-principal>`. Own-bindings and
tier entitlements follow the link. A succession takes over everything the
old principal held, so its authorization has two cases:

- **Old key still held:** its own signature plus one `member.admin`
  countersign. The old key's holder re-wraps every epoch it holds to the
  new key in the same write.
- **Old key claimed lost:** the owner-set threshold. The record authorizes
  the transfer; each tier's restoration is then an ordinary grant by any
  current member of that tier, and the new principal's access to a tier is
  pending until one of them re-wraps. The signing owners need not hold the
  keys.

A succession for an owner's key always requires the threshold. The record
moves no key material itself.

## Enrollment and joining

The first policy is the **enrollment**, signed by its founding owner at
`lit2 admin init`. Verification seeds there and never walks before it.
`lit2 admin init` writes the signed enrollment first and then applies the
default profile as version one, because every event must be signed by an
enrolled principal.

The clone, never the events, names which enrollment founds its store. The
founding init names its enrollment in the committed `.lit/founding` and pins
it beside the clone's key. A joining clone adopts the enrollment
`.lit/founding` names, pins it once, and pushes a signed membership request;
init tells the joiner to ask the owner to approve. Every fold is seeded with
the pin. Any other enrollment, such as one of two unsynced clones each
initializing, is carried, and its principal requests membership of the store
the file names.

The owner admits with `add /members '{principal, role}'`. With one owner the
threshold is one, so the proposal and its approval are one command. Once the
admission syncs, the tier's epoch keys are wrapped to the joiner and the
backlog reads in full. Key exchange, wrapping and epochs are invisible to
both.

## The policy

The policy is the member list (principal, display name, signing key,
encryption key, role, and the tier keys wrapped to that member), the role
definitions, and a monotonically increasing version. It lives in the store
as events: `policy.propose` carries the new document and `policy.approve`
cites its hash. A version activates at the causal position where the
approvals the previous version requires are all present.

Membership verbs are paths, each compiling to a proposal and, where the
writer's own signature suffices, its approval:

```
add /members '{principal, role}'
set /members/<id>/role editor
rm /members/<id>
set /policy/threshold 2
```

**What a signature covers.** The verifier decides which approvals a change
needs by diffing it, never by the signer's framing:

- A `member.admin` signature alone covers changes to non-owner rows.
- Any diff to the set of principals holding the owner role (add, remove,
  promote, demote) needs the owner-set threshold. A policy update that adds
  an owner under an ordinary signature is invalid.
- Any change that widens a role's capability bundle needs the threshold,
  because role definitions live in the same document and widening a role
  would otherwise grant owner-equivalent control without touching the owner
  set. A `policy.admin` signature alone covers edits that widen nothing:
  narrowing a bundle, renaming, reordering.

**The owner threshold.** Owner-set changes follow TUF root rotation: the new
document carries signatures from a threshold of the old owners and of the
new. The threshold defaults to 1 while there is one owner and to a majority
of owners once there are more; `set /policy/threshold <n>` sets it
explicitly. For removal and demotion of an owner, the old-owner quorum
excludes the owner being removed or demoted, so a two-owner workspace can
shed an unresponsive or compromised owner.

**Policy version on events.** Every event carries the policy version its
writer ran under. An event citing a lower version than its writer's previous
event fails verification: a writer that has seen a newer policy cannot author
under an older one. Whatever an event cites, it is judged against the
version in force at its position, which is what makes a demotion bite.

## Capabilities and roles

A capability is a triple **(verb, path glob, binding)**:

- verb: `get`, `set`, `add` or `rm`;
- glob: the scope, a subtree, a level or a field;
- binding, currently: `own` (the principal on the create event of the node
  the path names, an item or a comment), `assignee` (the principal holding
  the claim on the node's lane, as the fold computes it,
  `specifications/lanes-and-claims.md`), `own-or-assignee`, or `all`.

How a `get` denial differs from the tier keys is
`strategy/authority-and-visibility.md`, "Actions".

Every mutation needs a capability:

| write | capability |
|---|---|
| filing | `add */children` |
| a transition | `set */status` |
| a comment | `add */comments` |
| re-tiering | `set */tier` |
| membership | `add /members`, `set /members/*/role`, `rm /members/*` |
| the threshold | `set /policy/threshold` |
| erasure | `rm * --purge`, `rm * --destroy` |
| a rewrite of history | the rewrite event the verb mints |

A column is no capability's condition; where a status may move is the
requirement gate's (`specifications/columns-and-requirements.md`). Closing
is a move that carries the fields its column's entry needs, so whoever may
move a ticket may close it.

**The capability rule at the write.** The capability rule is the fold's. A
command folds the events it would mint onto the writer's current fold; if the
fold would carry any of them, the command writes none and names each
capability of the verb with what the write missed. Bulk forms fan out to the
per-path check, so a selector or a `--json` list grants nothing the single
write does not, and refusal of any path refuses the whole.

**Default roles.** Roles are named bundles of capabilities, spelled as path
globs in the default profile. A workspace may change any of them.

| Role | Capabilities beyond the previous row |
|---|---|
| observer | `get`, holding the tier keys granted to them; no write verbs |
| contributor | `add */children`; `set */status` bound `all`; `add own/*`; `set own/*`; `add */comments`; `rm own/*/*`; `rm own/*` at level 1 |
| editor | `set */*` and `rm */*` bound `all` at level 1 and above; `set */rank`; `add */blocks`, `add */relates`; `set */tier` |
| owner | everything, including `rm * --purge`, `rm * --destroy`, `member.admin`, `policy.admin`, `set /policy/*` |

A contributor's `set */status` is bound `all` because pulling the top of the
shared queue regardless of creator is the agent workflow. A contributor
cannot delete another's ticket (`own` on `rm`) or an epic, even its own (the
level-1 glob).

Per-subtree delegation is a glob rooted at a node, TUF's delegated-paths
primitive. The default profile ships none.

## Verification

The rule, for each event in total order:

1. Check the signature against its principal's key (or its delegation, as
   above).
2. Check the intent against the policy in force at the event's causal
   position.

An event that fails is carried, counted on `/integrity` with its writer,
and not applied. Every receiver runs the rule over a writer's published
segments as it accepts them, and a write runs it over its own events before
appending them. A forged or unauthorized publication is held and its events
carried unapplied, so two clones holding the same events fold equal, and
the local copies stay. A modified client that appends a refused event
anyway publishes a segment every receiver refuses; `/integrity` names the
writer and the event.

A remote serving a writer's history older than, or diverging from, the one
already verified is refused, not adopted.

**Removal and offline work.** After `rm /members/<id>`, the removed
principal's events after the removal's causal position fail verification. A
principal that stays offline from its removal can mint events whose
positions sort before it; the fold applies them, as it applies any offline
work made before the news arrived, and lists them with their `recorded-at`
for an owner to judge and rewrite.

## Tiers and envelopes

**Granularity.** The unit of policy is the tier on an item or comment; the
unit of encryption is the prose field.

- Every item carries `tier`, a structural field. It governs the item's prose
  fields: `title`, `body`, `prompt`, every plan revision, every checklist
  item's text, and every reason on it: a `--reason`, a transition's reason,
  a checklist state's reason.
- Every comment carries its own `tier`, defaulting to its item's, so a
  single comment can be restricted, as Jira restricted comments and GitLab
  internal notes do.
- Structural fields (ids, status, level, rank, lane, timestamps, links,
  labels) stay plaintext: every member sees that a ticket exists, where it
  ranks and how it relates.

Claims, rank, lane sequencing and stage read structural fields and whole
member and child sets, so they work the same for every reader. The backlog
currently lists every item and redacts what the reader cannot open; one
renderer produces every view and applies that redaction. A reader without
the key sees the skeleton: id, column, lane, labels, links, and an envelope
where the prose is.

**The envelope.** Every prose field is `{suite, tier, epoch, nonce,
ciphertext, commitment}` from its first event, sealed and opened at the
verb layer, so the fold, the store, the cache and sync see opaque bytes
(`INV:opaque-envelope`). `@` references are normalized before the envelope
seals. The nonce is fresh per encryption. The encryption key is the item's
or comment's **content key**, independent randomness generated at creation
and stored wrapped under the tier's epoch key named by `epoch`; the wrapped
copy rides the item or comment, not each envelope.

**The commitment** is a deterministic keyed hash of the plaintext, keyed by
an HKDF subkey of the epoch key in force at write time. It makes equality of
content decidable without decryption: the fold compares commitments, not
envelope bytes, because fresh nonces make two writes of the same text differ
byte for byte. Equal commitments are one value; a divergent pair of
concurrent prose writes is a conflict value, resolved by a write from a
principal holding the tier. A checkout that cannot read the tier reports
which tier is needed and leaves the conflict for a keyholder.

Chosen: an epoch-keyed commitment. Considered: a stable per-tier commitment
key, which survives rotation. The stable key would give every removed member
a permanent oracle for testing guesses against future content; the
epoch-keyed one only loses equality detection across a rotation, so an edit
pair straddling a membership change can show a spurious prose conflict that
a keyholder confirms. The remaining cost, that anyone can see two same-epoch
encrypted values are equal, is the trade git-crypt makes for diffability,
paid here only for whole field values.

**Reading without the key.** A replica lacking a tier key folds the same
state as one holding it, and never mistakes unreadable for absent. A
selector atom over a prose field by content is `unknown (tier <name>)`
(`specifications/columns-and-requirements.md`, "The atoms"). A search over
prose searches what the reader can decrypt and says how many envelopes it
could not open.

## Keys, grants, rotation and erasure

**Epoch keys.** Per tier, a symmetric epoch key. Every epoch's key, current
and historical, is wrapped to each entitled member's encryption key in the
policy, the SOPS recipient pattern.

**Grant.** Granting read wraps all of the tier's epoch keys to the new
member, so a joiner reads the tier's whole existing backlog.

**Removal** rotates every tier the member held to a new epoch wrapped to the
survivors only, the Megolm and Signal sender-keys pattern. Old epochs are not
re-encrypted. The output says the removed member keeps what they could
already read.

**Destroy.** `rm x --destroy` crypto-shreds content keys. Content keys are
independent randomness, never derived from the epoch key, because a derived
key is a function of a secret every tier member still holds and destroying
it destroys nothing. Each encrypted item and each of its comments carries its
own content key, because comments tier independently of their item; destroying
an item shreds its key and all of its comments' keys, whatever their tiers.
Destroy is a rewrite that drops the wrapped copies on every replica that
syncs, and no holder of any epoch key can reconstruct them.

**Default tier.** Every workspace has a default tier that all members hold.
A new workspace starts with one tier, encrypted, all members entitled. A
project that wants its backlog world-readable, as lit's own repo does, sets
the default tier to plaintext.

## Changing a tier

`set x/tier <tier>`, on an item or a comment. Members of the tier see nothing
different; others see the skeleton.

- **Narrowing** restricts only the future: existing history stays under the
  old tier.
- **Widening** mints a fresh content key for the item, re-encrypts its
  current field values under it, and wraps it to the destination tier's
  epoch key. The old content key and the history it encrypts stay under the
  old tier, because re-wrapping the old key would expose the history too.
  The widening write is performed by a principal holding both tiers.

## Documented limits

- **Destroy removes recovery, not confirmation of a guess.** The commitment
  outlives the shred and its key is an epoch subkey members still hold, so a
  tier member can test a guessed plaintext against a destroyed field.
- **A lost signing key without a succession** leaves own-scoped rights on the
  old principal until an owner reassigns them.
- **Key loss is data loss.** Every tier key is held by all its members, so
  any survivor can re-wrap for a recovered member; the fatal case is a sole
  member losing both key and phrase, which the printed phrase exists to
  prevent.

## Undecided: policy growth

The policy is O(members × epochs ever minted), with wrapped keys for every
historical epoch carried by every replica. Its form must state a bound or a
compaction strategy, or show why churn keeps it small.
