// Package fold is events into state, the same everywhere.
//
// Its layer in architecture.md §6: events → state; validation engine; rank;
// holders; conflicts (§3.3, §3.4, §3.7, §3.8, §3.10, §3.13, §3.20, §5 step
// 3). fold(events) → state is deterministic under the total order of
// events.OrderKey, (causal position, writer, event id), as events.Compare
// orders it; the fold defines no order of its own. Two folds of one event
// set, in any arrival order, yield equal state, since every shape merges in
// any order. It reads no clock and no configuration (decision H). Replay
// reads every event, Check verifies every signature, and Step applies one
// checked event; every caller folds through it.
package fold
