// Package fold is events into state, the same everywhere.
//
// Its layer: events → state; validation engine; rank; holders; conflicts
// (specifications/fold.md, "Two functions"; specifications/access.md,
// "Verification"; the Fold step of specifications/code-layout.md, "One
// command, start to finish"). fold(events) → state is deterministic under the total order of
// events.OrderKey, (causal position, writer, event id), as events.Compare
// orders it; the fold defines no order of its own. Two folds of one event
// set, in any arrival order, yield equal state, since every shape merges in
// any order. It reads no clock and no configuration. Replay
// reads every event, Check verifies every signature, and Step applies one
// checked event by the rule Replay uses.
package fold
