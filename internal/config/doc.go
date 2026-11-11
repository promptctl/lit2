// Package config is .lit/workspace.toml as one typed schema value.
//
// Its layer in architecture.md §6: .lit/workspace.toml from the working tree
// → one typed schema value (§3.15, §5 step 2). The schema parse refuses, at
// the write, what the designs say it refuses, the column-graph check of §3.5
// among them. It sits above fold because the fold reads no configuration
// (decision H), and above field, whose kinds and atoms it parses into (D8).
package config
