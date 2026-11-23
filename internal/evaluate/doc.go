// Package evaluate is the derivations that need the clock or the configuration.
//
// Its layer: derivations under the clock or the configuration: labels, held,
// expiry, staleness, stage, rollup (specifications/fold.md, "Two functions"). evaluate(state, config, now) → view is a pure function
// of folded state, the reader's configuration and now, performing no I/O; the
// fold reads no clock and evaluate is handed now (specifications/fold.md,
// "Time enters at the edges"). Liveness, which git
// reads, is decided here.
package evaluate
