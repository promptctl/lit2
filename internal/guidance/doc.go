// Package guidance is what lit2 says after the data.
//
// Its layer in architecture.md §6: moments → resolved handlers → text after
// the data (§3.12, §5 step 7). A handler binds on a list of moments, where a
// selector, and holds a body of prose; handlers resolve per field from the
// ticket's node up through its ancestors, then the user layer, then the
// shipped layer. A handler produces text and nothing else (§8). The text of
// the managed block in AGENTS.md lives here; init writes it.
package guidance
