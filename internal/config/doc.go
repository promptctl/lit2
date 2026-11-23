// Package config is .lit/workspace.toml as one typed schema value.
//
// Its layer: the active version (specifications/workspace-file.md, "Applying
// a version"), named at the Fold step (specifications/code-layout.md, "One
// command, start to finish") → one typed schema value. The model in force is the last version committed, or the
// shipped default profile when none is; Load pairs it with its blob id. Parse
// refuses, naming every fault, what the designs say it refuses, the column-graph
// check (specifications/columns-and-requirements.md, "Validity of a column
// graph") among them. It sits above fold because the fold reads no
// configuration, and above field, whose kinds and atoms it parses
// into (specifications/code-layout.md, "Units, downhill"). It owns the field table: the one answer to what a path names at
// a place, built-in fields, declared fields and checks alike.
package config
