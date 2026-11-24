// Package guidance is what lit2 says after the data.
//
// Its layer: hook events → resolved handlers → text after the data
// (specifications/workflows.md, "Workflow and handler"; the Speak step of
// specifications/code-layout.md, "One command, start to finish"). A handler
// binds on a list of hook events, where a selector, and holds a body of
// prose; handlers resolve per field from the ticket's node up through its
// ancestors, then the user layer, then the shipped layer. A handler produces text and nothing else. The text of
// the managed block in AGENTS.md lives here; init writes it.
package guidance
