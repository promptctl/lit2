# Event store: budgets and baselines

Status: baselines measured 2026-08-25; targets set by owner directive; scale
projections await the harness. This file is the single home for every
**measured baseline and derived budget figure** in the event-store design —
prose in charter.md/design.md cites those rather than restating them. The two
governing directives are **charter.md hard constraint 3's** — the figures
live there, and this file only turns them into measurable rows. A budget
change is an owner decision recorded here, not a drift.

## Measured baselines (2026-08-25, this machine)

| Measurement | Value | Provenance |
|---|---|---|
| lit stores under ~/code | 52 unique store paths (57 pre-dedup; owner counts ~30 projects) | walk of `git-common-dir/links/dolt`, deduplicated by resolved real path |
| Aggregate Dolt store disk | ~0.9 GB (901 MB deduplicated; the naive 57-dir sum reads 1.1 GB) | `du -sm` over the 52 unique real paths |
| Largest single store | 190 MB (links-issue-tracker) | `du -sh` |
| Active backlog rows (this repo) | ~123 | `lit backlog \| wc -l` |
| Total tickets / total mutations per store | **unmeasured** — harness's first job | — |
| Events per week of agent churn | **unmeasured** — harness's first job | — |

The 10x corpus has **two components**, defined here once (per-row notes may
add detail but never substitute a different construction):

- **Depth corpus** — the real fleet's largest store replayed, then
  synthesized to 10x its event count preserving its shape (event-type mix,
  rank-intent density, prose sizes). The per-store rows of the
  Storage table (per-store disk, the git-impact check, the dead-refs
  predicate under churn replay) also run against the depth corpus, being
  properties of one repo keyed to the largest-store baseline.
- **Breadth fleet** — 10x the measured store count, **with each store's
  history also scaled ×10**, preserving the measured size distribution's
  *shape* (most stores small), not 10x copies of the largest. Machine-wide
  benchmarks (aggregate disk) come from this, so they test the
  genuinely compounded both-axes case.

## Storage and repo-impact budgets (at 10x)

These are ceilings on rate and footprint. Rewrites (charter #7) can shrink a
store — compaction is one — but no budget here depends on one running.

| Quantity | Budget | Notes |
|---|---|---|
| Event store disk per store at 10x history | ≤ 1/10 of today's Dolt store at 1x (i.e. ≤ ~19 MB where Dolt spends 190 MB) | hypothesis to verify in S1; if missed, it is a design problem to solve before S2, not a note |
| Aggregate lit disk per machine at 10x both axes | ≤ the measured aggregate in the baselines table | checked against the breadth fleet |
| Change to any git operation the developer runs in the code repo (`git status`, `git fetch`, `git gc`, `git log --all`, …): behavior, output, or latency | none: indistinguishable from a control clone without lit | charter #10 |
| Dead-checkout writer refs remaining after maintenance | 0 (refs present = live checkouts + unswept inbox refs) | countable predicate; dead-ref growth is a hygiene bug the harness asserts away |

## Harness contract

The load harness is the migration's instrument, not an afterthought:

- **Measures first.** Its first run produces the unmeasured baseline rows
  above from the real fleet before any synthetic scaling.
- **Replays reality.** Both corpus components are built from real-store
  replays per the corpus definition above — never from uniform random
  events.
- **Gates the migration.** The gate condition is **owned by design
  §migration** (S1 row) and its wording governs: the oracle diff empty over
  sustained real use, and budgets passing at 10x — where "budgets passing"
  means every row of the storage table above green (the baselines table
  is descriptive input, not a gate). A red row stops the flip; the fix
  happens while Dolt is still authoritative and stopping is free.
