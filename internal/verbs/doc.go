// Package verbs is get, set, add and rm compiled to intents under the one
// enforcer.
//
// Its layer in architecture.md §6: get/set/add/rm on typed addresses;
// requirements; verb → intents; echo (§3.2, §3.6, §3.9, §3.18, §5 steps 5
// and 6). A write computes a description: the intents it would mint, the
// requirement and capability verdicts, the derivations, the echo; refusal of
// any path refuses the whole `[LAW:single-enforcer]`. The perform step signs
// the intents through the suite in events; the in-use check on a column runs
// here.
package verbs
