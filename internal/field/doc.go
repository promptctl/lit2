// Package field is the field language config and address both stand on:
// field kinds with their value parsers, the built-in fields, the atom.
//
// Its layer in architecture.md §6: field kinds and their value parsers; the
// built-in fields; the atom (§3.1, §3.6). The atom is one test over one path
// of an item, the grammar a requirement's test and a selector share, so a
// reader learns it once. It sits under both config and address, so neither
// imports the other for it (D8).
package field
