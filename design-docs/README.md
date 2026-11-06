# Design docs

Start with `architecture.md`: the one strategy for building `lit2`, the
primitives, how the functionality composes from them, and the decisions still
open. `scenarios.md` walks the common situations through it and records where
it strained. Where `architecture.md` and another document here disagree,
`architecture.md` wins; its appendix lists each departure.

## Written for v2 (2026-11-05)

- `architecture.md`: the unified architecture and build order.
- `scenarios.md`: paper walk-throughs of sixteen common situations, with the
  strains they found and how each was resolved.
- `columns-and-phases.md`: column, phase and workflow state are one concept; the
  column is the one stored fact, labels are computed; rework is verdicts inside
  review. Owns the column graph at `/columns`.
- `transition-requirements.md`: entry and exit requirements on a column, one
  shape, one enforcer on the transition write. "Ready to pull" is the initial
  column's exit list.
- `workflows.md`: a workflow is a named unit of handlers; override is per
  field, nearest wins.
- `guidance.md` (provisional): what lit says to an agent and when. Consolidates
  three April 2026 v1 designs, since removed.
- `workspace-schema/design.md`: rewritten from the 2026-08-27 draft in
  standard tracker vocabulary.

## Carried from lit v1, unedited unless noted

Copied from `links-issue-tracker` at commit `3f84f1d3`, which includes the
2026-11-03 amendment to the event-store charter (rewriting history is
first-class).

- `path-addressed-cli.md` and `path-addressed-cli-examples.md` came from that
  repo's working tree; they were never committed there. The accepted CLI
  design; corrected in part by the v2 docs above, as its status line says.
- `custom-fields.md` and `custom-columns.md`: corrected in part by the v2 docs
  above, as their notes say.
- `MINIMAL-LIT-SKETCH-2026-09-29.md`: provisional; the v2 feature baseline.
- `project-intent.md`, `workspace-configuration.md`, `work-claims.md`,
  `schema-reversibility.md`, `event-store/`, `access-control/`.

Not carried as files, because they exist only as ticket text in the v1 store:
the three-level hierarchy model (epic `promptctl-hierarchy-6cqf`) and the plan
record model (epic `promptctl-plan-record-w3yg`).
