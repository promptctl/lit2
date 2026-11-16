# Workspace configuration: the file is the intent, the applied version is the model in force

Status: draft / proposal. Written 2026-10-27 for owner review. This is the
foundation the four designs written the same day stand on
([custom-fields.md](custom-fields.md), and three since superseded by
[transition-requirements.md](transition-requirements.md), [workflows.md](workflows.md) and [columns-and-phases.md](columns-and-phases.md)),
and it answers one rule the owner set that day: *"Whatever we did today should
be cross-repo capable."* The storage model is
[architecture.md](architecture.md) §3.15; this document records the
classification and the reasons.

## The rule, made concrete

Two replicas of one store must compute the same backlog. A replica is a clone
with the store's events and nothing else promised: it may not hold the
repository's working tree at the same commit, or at all, when it folds. So
anything lit needs in order to say what a backlog *is* has to be in the log,
or two replicas holding the same events compute two different answers.

Level names are the first case: a replica without the file would render `L2`
where the repository says `epic`. Which labels are strategies is the second:
a report could not tell a strategy label from any other label. Custom field
definitions ([custom-fields.md](custom-fields.md)) are a third. The owner's
ruling is that this class of divergence is not acceptable, so the question
this document settles is: which configuration is workspace truth, and where
does it live so every replica sees it.

## Workspace truth versus local preference

A configuration value is **workspace truth** when two machines that disagree
about it cannot both be right about the backlog. It is a **local preference**
when they can. The test is that sentence and nothing else.

| Value | Class | Reason |
|---|---|---|
| the `ready` check, an exit requirement on the initial column | workspace truth | decides whether a ticket is ready |
| levels and their names | workspace truth | the name of a level is a fact about the backlog, not about a machine |
| `[strategies]` | workspace truth | which labels are strategies decides what a report counts |
| custom field definitions | workspace truth | which fields exist, their kinds, which are required |
| columns, their requirements, link types, labels, `claim_ttl`, `min-reader-version` | workspace truth | each decides what a write may do or how a fact reads |
| workflows | workspace truth (see [workflows.md](workflows.md), which supersedes the earlier local-preference row) | handlers bind to columns and nodes, which are store nodes |
| how this machine syncs, caches and speaks | local preference | how this machine behaves; the record it produces is in the store either way |

Everything in the first class is in the workspace file and in force only once
applied into the log. Everything in the second class stays in the person's own
configuration. A new setting is classified at birth by the same test, in the
ticket that adds it.

## The design: one document, in git as the intent, applied into the log

The workspace model is written in `.lit/workspace.toml`, one TOML document
with every section above. The file in git is the **intent**: a person edits
it in an editor, git versions and merges it, and the team reviews it however
it reviews any change to the repository. A write such as `set
/columns/backlog/exit '[…]'` edits the working-tree draft and says so in the
echo; committing it is the person's act, like any other file. The file itself
never becomes an event.

The model in force is the **active version**: the last one applied with `lit2
config commit`. Applying is an event that carries the version's bytes and its
git blob id and names the version it replaces, so the log is self-sufficient
and the file is never read to learn what is in force; a replica holding the
same events has the same model without having the file. Apply names a
committed blob and refuses a dirty or untracked file, and refuses a version
that does not parse or whose column graph fails its check, naming every fault
with its key path. `lit2 admin init` writes the default profile to the file
when none exists and applies the same bytes as version one, the first event of
every store, so there is no store without an active version and no version 0.

One document rather than one table per concern, for three reasons. It gives
the parse a single boundary: `Parse` reads one version's bytes into one typed
schema value once, and every consumer takes fields off that value
(`[LAW:parse-dont-validate]`). It gives the applied version one blob id, which
is what every event records as `config-ref`. And it keeps the human's
experience whole: the document is a file in their repository, in the syntax
they already know.

The document's sections are owned by the designs that need them: levels and
`[strategies]` by the hierarchy work, `[levels.<n>.fields.<name>]` by custom
fields, columns by [columns-and-phases.md](columns-and-phases.md). This
document owns the container and the rules here, not the sections.

## Reading and writing it

Every command runs on the active version, read from the fold, so every
replica evaluates the same definitions. A draft in the working tree is not in
force until it is applied, so a draft that does not parse stops no command.
The file and the active version drift apart whenever one is edited or the
other applied; `lit2 config status` reports the drift, and whether the active
blob is in HEAD's history, and the board carries one line for it. No other
command mentions it.

Two people who edit the file concurrently meet in a git merge, which is where
people already resolve concurrent edits to a file. Two applies that name one
parent are a conflict value on the workspace in the fold, resolved by an apply
that names both: git merges the file, lit merges which version is in force.

## Observability

Every event carries `config-ref`, the blob id of the active version its writer
ran under; the command event records the version a run read and, for an
apply, the version from and to `[LAW:nothing-unseen]`.
