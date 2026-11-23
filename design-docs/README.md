# Design docs

Start with `architecture.md`: the strategy for building `lit2`, the
primitives, how the functionality composes from them, and the decisions it
records. `scenarios.md` walks the common situations through it. The other
documents are the detailed designs it composes.

- `project-intent.md`: what lit is for.
- `requirements.md`: what lit2 must do and must not do, stated from what lit
  is for.
- `architecture.md`: the unified architecture and build order.
- `scenarios.md`: paper walk-throughs of common situations.
- `build-map.md`: the capability map the backlog is a view of; the slices,
  the walking skeleton first.
- `columns-and-phases.md`: column, phase and workflow state are one concept; the
  column is a ticket's stored position, a label is applied or computed; rework
  is reviews on the PR's revisions inside `review`.
- `transition-requirements.md`: entry and exit requirements on a column, one
  shape, checked on the transition write. In the default profile, "ready to
  pull" is the initial column's exit list.
- `workflows.md`: a workflow is a named unit of handlers; override is per
  handler, nearest wins.
- `guidance.md`: what lit says to an agent and when.
- `workspace-schema/design.md`: levels, fields, link types and templates.
- `path-addressed-cli.md`: the path-addressed command line.
- `work-claims.md`: claims and how `/next` serves parallel checkouts.
- `event-store/`: the event store's charter, design and budgets.
- `access-control/`: access control's charter and design.
