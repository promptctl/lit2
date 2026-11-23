# Authority and visibility

How lit enforces who may write what, and who may read what, with no server.

## Diagnosis

The remote is a dumb store. Anyone with push access can publish anything to
it, delete a writer's ref or overwrite it, and no server stands between them
and every other clone. Yet a contributor must be unable to delete an epic or
another person's ticket, and a ticket must be private from people who can
read the repository. Every guarantee therefore has to be one each client can
check for itself. Two limits follow from that. No client-side scheme can stop
a holder of push access from deleting history. And a key once given cannot be
taken back for content it already covered, so a read grant is forward-only.
Encryption also collides with convergence: two fresh encryptions of the same
text differ, so a fold comparing bytes would see a conflict that is not there.

## Guiding policy

Two independent layers over the event store, each enforced by every verifier
and trusted to no party. The **write layer** signs every event and keeps the
policy in the store as events; every replica judges each event against the
policy in force at its position. The **read layer** encrypts prose under keys
shared within named tiers; ciphertext is the synced representation, and
encryption and decryption happen only at the verb boundary. Prefer
established patterns, for the crypto and for the role vocabulary. Where
access control would need an overly complicated implementation or data model,
prefer redesigning lit's data model over weakening the access-control
architecture.

## Actions

**Signed from the first event.** Every event in every store is signed, and
every signature names its suite so a second suite can coexist with the first.
Serves: Operations, Data.
Spec: `specifications/access.md`.

**Authorization is a fold rule.** For each event in total order, the fold
checks the signature and the intent against the policy in force at that
position; an event that fails is carried, counted with its writer, and not
applied. Every receiver runs the rule over a publication before accepting it,
and a writer runs it over its own events before appending them, so a refusal
reaches the person at the write.
Serves: Operations, Data.
Spec: `specifications/access.md`, "Verification".

**Forgery is preventable; denial is only recoverable.** Honest replicas detect
and refuse a forged or rolled-back history, and the local fold is the defense
against a remote serving an older or diverging history. Deletion of a ref by
someone with push access is recovered by re-publishing, not prevented.
Serves: Data.
Spec: `specifications/access.md`.

**Verification seeds at a pinned founding.** Any writer can mint a founding
policy that sorts first, so the events cannot say which one founds a store.
The clone says it, from a committed pin, and verification never walks before
it.
Serves: Data.
Spec: `specifications/access.md`.

**The policy is data in the store.** Members, roles and a monotone version
are events, changed by proposal and approval. The verifier, not the signer's
framing, decides what a change touches: any change to who holds the owner
role, and any change that widens a role's capabilities, needs the owner
threshold. A writer that has seen a newer policy cannot author under an older
one.
Serves: Operations.
Spec: `specifications/access.md`.

**Permissions are paths.** A capability is a verb, a glob over the same paths
the command line uses, and a binding to the actor, so "can't delete epics" is
a path restriction and bulk writes grant nothing a single write does not.
Read is restricted two ways, which do different jobs: a `get` denial is
access policy that clients enforce, at any grain, with no new key structure;
the tier keys are the cryptographic boundary. Every mutating verb has a
capability.
Serves: Operations.
Spec: `specifications/access.md`.

**One principal per person.** A principal is a person's signing identity, and
each clone signs with a key its principal delegates to it, scoped and
expiring; a checkout is told apart by its checkout id. A principal signs and
never decrypts: its encryption key is a separate secret, so a process can
hold a member's read without its signing authority, and a CI principal can
write without reading.
Serves: Operations.
Spec: `specifications/access.md`.

**A tier hides content, not existence.** Structural fields stay plaintext for
every member, so rank, lanes, claims and stage keep working across tiers, and
nothing a replica computes depends on being able to read. Adding a structural
field adds metadata every repository reader can see, and the change that adds
it names that trade.
Serves: Operations, Data.
Spec: `specifications/access.md`.

**Every prose field is an envelope from its first event.** Layers below the
verbs see opaque bytes, so tiers change what the verb layer does and not what
the store, fold or sync do. Equality of content is decidable without
decryption, so two writes of the same text converge.
Serves: Data, Complexity.
Spec: `specifications/access.md`.

**Revocation is rotation.** Removing a member moves each tier it held to a new
epoch for the survivors; there is no cryptographic deny, and the policy
language is additive grants plus rotation.
Serves: Operations, Data.
Spec: `specifications/access.md`.

## What the user carries

One recovery phrase to store; an approval step when someone joins; conflicts
in restricted prose resolved only on machines holding the tier.
Serves: Operational.
Spec: `specifications/access.md`, "Documented limits".
