# Event store: budgets and baselines

Status: baselines measured 2026-08-25 on the lit v1 fleet; targets set by
owner directive; scale projections await the harness. This file is the single
home for every **measured baseline and derived budget figure** in the
event-store design — prose in charter.md/design.md cites those rather than
restating them. The governing directive is **charter.md hard constraint 3** —
the figures live there, and this file only turns them into measurable rows. A
budget change is an owner decision recorded here, not a drift.

## Measured baselines (the lit v1 fleet, 2026-08-25, the owner's machine)

The v1 fleet is the scale lit2 is sized against; "10x" means ten times these.

| Measurement | Value | Provenance |
|---|---|---|
| lit stores under ~/code | 52 unique store paths (57 pre-dedup; owner counts ~30 projects) | walk of `git-common-dir/links/dolt`, deduplicated by resolved real path |
| Aggregate store disk | ~0.9 GB (901 MB deduplicated; the naive 57-dir sum reads 1.1 GB) | `du -sm` over the 52 unique real paths |
| Largest single store | 190 MB (links-issue-tracker) | `du -sh` |
| Active backlog rows (links-issue-tracker) | ~123 | `lit backlog \| wc -l` |
| Total tickets / total mutations per store | **unmeasured** — harness's first job | — |
| Events per week of agent churn | **unmeasured** — harness's first job | — |

The 10x corpus has **two components**, defined here once (per-row notes may
add detail but never substitute a different construction):

- **Depth corpus** — the real fleet's largest store replayed, then
  synthesized to 10x its event count preserving its shape (event-type mix,
  rank-intent density, prose sizes). Per-command benchmarks (the Command
  budgets table) run against this — **on a machine concurrently running the
  breadth fleet's background sync load**, so command latency is measured
  under both axes at once, not on a quiet machine. The per-store rows of the
  Storage table (per-store disk, the git-impact check, the dead-refs
  predicate under churn replay) also run against the depth corpus, being
  properties of one repo keyed to the largest-store baseline.
- **Breadth fleet** — 10x the measured store count, **with each store's
  history also scaled ×10**, preserving the measured size distribution's
  *shape* (most stores small), not 10x copies of the largest. Machine-wide
  benchmarks (aggregate disk, background stampede, and the background load
  behind the depth corpus's command runs) come from this, so they test the
  genuinely compounded both-axes case.

## Command budgets (at 10x depth corpus, on this class of machine)

Every command row is a **total wall-time ceiling, inclusive of all
components** — the staleness row profiles a component *inside* those
ceilings, not an addition to them; there is no reading of this table under
which per-component greens can sum past a command ceiling. "Compute" excludes
network transfer only where physics demands it (first-clone download); per
`[INV:no-foreground-network]` no other command touches the network in the
foreground at all.

| Operation | Budget | Notes |
|---|---|---|
| Any command, warm cache | < 300 ms | the everyday case; p95 |
| Any command, cold cache (incremental fold from snapshot) | < 1 s | |
| Full refold from genesis (fold-version bump; worst case) | < 1 s | can occur inside any command; sets the codec + snapshot bar |
| First-adopt verification compute | < 1 s | the riskiest budget; adoption verifies the full history (design §sync), so this is what that verification must meet |
| Staleness check (read all writer ref heads) | < 20 ms | component profile within the command ceilings above |
| Background receive/mirror, per occasion | ≤ 1 CPU-second | and machine-wide background CPU ≤ 5% of one core averaged over any 60 s window on the breadth fleet |

## Storage and repo-impact budgets (at 10x)

These are ceilings on rate and footprint. Rewrites (charter #7) can shrink a
store — compaction is one — but no budget here depends on one running.

| Quantity | Budget | Notes |
|---|---|---|
| Event store disk per store at 10x history | ≤ ~19 MB: one tenth of what v1's largest store spent at 1x | a hypothesis the harness verifies first; a miss is a design problem to solve, not a note |
| Aggregate lit disk per machine at 10x both axes | ≤ the measured aggregate in the baselines table | checked against the breadth fleet |
| Change to any git operation the developer runs in the code repo (`git status`, `git fetch`, `git gc`, `git log --all`, …): behavior, output, or latency | none: indistinguishable from a control clone without lit | charter #9 |
| Dead-checkout writer refs remaining after maintenance | 0 (refs present = live checkouts + unswept inbox refs) | countable predicate; dead-ref growth is a hygiene bug the harness asserts away |

## Harness contract

The load harness is the design's instrument, not an afterthought:

- **Measures first.** Its first run produces the unmeasured baseline rows
  above from the real v1 fleet before any synthetic scaling.
- **Replays reality.** Both corpus components are built from replays of real
  v1 stores per the corpus definition above — never from uniform random
  events.
- **Gates releases.** "Budgets passing" means every row of the two budget
  tables above green (the baselines table is descriptive input, not a gate).
  A red row stops the release until the design meets it.
- **Runs in CI.** The per-command budgets are regression checks in CI, so
  the charter #3 directive is enforced by the build, not by memory.
