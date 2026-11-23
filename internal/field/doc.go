// Package field is the field language config and address both stand on:
// field kinds with their value parsers, the built-in fields and where each
// exists, the rules for writing one, the atom, and the literal grammar a
// command value is read by.
//
// Its layer: field kinds and their value parsers; the built-in fields; the
// atom (specifications/command-line.md, "Selectors and globs";
// specifications/columns-and-requirements.md, "The atoms"). The atom is one test over one path
// of an item, the grammar a requirement's test and a selector share, so a
// reader learns it once. It sits under both config and address, so neither
// imports the other for it (specifications/code-layout.md, "Units, downhill").
package field
