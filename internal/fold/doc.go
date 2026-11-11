// Package fold is events into state, the same everywhere.
//
// Its layer in architecture.md §6: events → state; validation engine; rank;
// holders; conflicts (§3.3, §3.4, §3.7, §3.8, §3.10, §3.13, §3.20, §5 step
// 3). fold(events) → state is deterministic under the total order
// (causal-position, writer): two folds of one event set, in any arrival
// order, from genesis or from a snapshot, yield equal state. It reads no
// clock and no configuration (decision H). Verification is one fold rule;
// the snapshot's content and key are the fold's.
package fold
