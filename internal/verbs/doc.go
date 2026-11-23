// Package verbs is get, set, add and rm compiled to drafts under the one
// enforcer.
//
// Its layer: get/set/add/rm on typed addresses; requirements; verb →
// intents; echo (specifications/events-and-store.md, "Intent";
// specifications/columns-and-requirements.md, "The requirement gate"; the
// Evaluate and Perform steps of specifications/code-layout.md, "One command,
// start to finish"). An operand is expanded to writes, each a subject, a field row and
// a value, and each write compiles by its shape to the facts it would add:
// drafts, the changes the echo prints, and the targets it claims. A refusal
// of any operand refuses the command whole `[LAW:single-enforcer]`. Nothing
// here mints or appends: the root signs the drafts and stores them as one
// frame, then hands the minted events back to Echo, which names what is new.
package verbs
