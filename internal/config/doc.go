// Package config is .lit/workspace.toml as one typed schema value.
//
// Its layer in architecture.md §6: the active version (§3.15, §5 step 2) → one
// typed schema value. The model in force is the last version committed, or the
// shipped default profile when none is; Load pairs it with its blob id. Parse
// refuses, naming every fault, what the designs say it refuses, the column-graph
// check of §3.5 among them. It sits above fold because the fold reads no
// configuration (decision H), and above field, whose kinds and atoms it parses
// into (D8).
package config
