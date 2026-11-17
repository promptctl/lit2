// Package evaluate is the derivations that need the clock or the configuration.
//
// Its layer in architecture.md §6: derivations under the clock or the
// configuration: labels, held, expiry, staleness, stage, rollup (§3.3, §3.4,
// §3.5, §3.8, §3.11). evaluate(state, config, now) → view is a pure function
// of folded state, the reader's configuration and now, performing no I/O; the
// fold reads no clock and evaluate is handed now (§8). Liveness, which git
// reads, is decided here.
package evaluate
