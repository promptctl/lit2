# Budgets

Carries out `strategy/performance.md`.

The figures here are targets. A target may become a gate for v1.0; none is a
gate for every release. A change to a figure is made here.

## Reference fleet

The scale lit2 is sized against is the set of lit v1 stores on one
developer machine, and "10x" means ten times these:

| measurement | value |
|---|---|
| stores | 52 |
| aggregate store disk | 901 MB |
| largest single store | 190 MB |

The harness's first run measures what the reference fleet does not yet
give: total tickets and events per store, and events per week of agent
churn. The corpora are scaled from those measurements.

## Corpora

Every row below runs against one of two corpora, both shaped like real
stores rather than uniform random events.

- **Depth corpus.** A store shaped like the reference fleet's largest, at
  ten times its event count, preserving its event-type mix, rank-intent
  density and prose sizes. The command budgets run against it on a machine
  concurrently running the breadth fleet's background sync load, so command
  latency is measured under both axes at once. The per-store storage rows
  run against it too.
- **Breadth fleet.** Ten times the reference store count, with each store's
  history also scaled ten times, preserving the shape of the measured size
  distribution (most stores small), not ten copies of the largest. The
  machine-wide rows, and the background load behind the depth corpus's
  command runs, come from it.

## Command budgets

At the depth corpus. Every command row is a total wall-time ceiling,
inclusive of all components.

| operation | target | notes |
|---|---|---|
| any command, warm cache | < 300 ms | p95; the everyday case |
| any command, cold cache (incremental fold from the snapshot) | < 1 s | |
| full refold from genesis | < 1 s | a fold-version bump can put it inside any command; sets the bar for the codec and the snapshot |
| first-adopt verification compute | < 1 s | the riskiest target: adoption verifies the full history |
| staleness check (read all writer ref heads) | < 20 ms | a component inside the command ceilings, not an addition to them |
| background receive, per occasion | ≤ 1 CPU-second | |
| machine-wide background CPU | ≤ 5% of one core, averaged over any 60 s window | at the breadth fleet |

## Storage and repository-impact budgets

At 10x. These are ceilings on rate and footprint. Rewrites such as
compaction can shrink a store, but no target here depends on one running.

| quantity | target | notes |
|---|---|---|
| event store disk per store, at 10x history | ≤ ~19 MB, one tenth of the reference fleet's largest store at 1x | a hypothesis the harness checks first |
| aggregate lit disk per machine, at 10x on both axes | ≤ the reference fleet's aggregate | at the breadth fleet |
| change to any git operation the developer runs in the code repository (`git status`, `git fetch`, `git gc`, `git log --all`, …): behavior, output or latency | none: indistinguishable from a control clone without lit | at the depth corpus |
| dead-checkout writer refs remaining after maintenance | 0: refs present are live checkouts plus unswept inbox refs | at the depth corpus, replaying churn |

## The harness

- The reference fleet's figures are input, not targets. A row meets its
  target or it does not; the harness reports every row.
- The first run also measures go-git's object walk on the depth corpus.
- The command budgets run in CI as regression checks, reported against
  their targets.
