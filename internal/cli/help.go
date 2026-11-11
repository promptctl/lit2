package cli

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/address"
)

// Verbs is the verb table: the four verbs on paths.
func Verbs() Table {
	return Table{rows: []Row{
		{Name: "get", Operands: TakesPaths{Make: func(p []PathText) Command { return Get{paths: p} }}, Summary: "read the nodes at these paths"},
		{Name: "set", Operands: TakesPairs{Make: func(p []Pair) Command { return Set{pairs: p} }}, Summary: "replace the value at each path"},
		{Name: "add", Operands: TakesPairs{Make: func(p []Pair) Command { return Add{pairs: p} }}, Summary: "append each value to the list at its path"},
		{Name: "rm", Operands: TakesPaths{Make: func(p []PathText) Command { return Rm{paths: p} }}, Summary: "delete the nodes at these paths"},
	}}
}

// Admin is the admin table: what keeps a checkout's store, outside the
// everyday surface.
func Admin() Table {
	return Table{rows: []Row{
		{Name: "init", Operands: TakesNothing{Make: func() Command { return AdminInit{} }}, Summary: "make this checkout a writer: its id, its principal, enrollment or joining"},
		{Name: "sync", Operands: TakesNothing{Make: func() Command { return AdminSync{} }}, Summary: "publish this checkout's events and fetch and verify every writer's"},
	}}
}

// HelpText is --help: the two tables and the field table, rendered from the
// same rows the parser reads.
func HelpText(verbs, admin Table, fields address.FieldTable) string {
	var b strings.Builder
	b.WriteString("lit2 keeps this repository's backlog as a tree of nodes in its own git object database.\n\n")
	b.WriteString("usage: lit2 <verb> <operand>...   lit2 admin <command>   lit2 --help\n")
	b.WriteString("--help anywhere prints this; -- ends the flags; - is a value\n")
	section(&b, "verbs", "", verbs)
	section(&b, "admin", adminWord+" ", admin)
	b.WriteString("\nfields:\n")
	for _, f := range fields.Rows() {
		fmt.Fprintf(&b, "  %-12s %s\n", f.Name, f.Help)
	}
	return b.String()
}

func section(b *strings.Builder, title, prefix string, t Table) {
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, r := range t.rows {
		fmt.Fprintf(b, "  %-12s %-15s %s\n", prefix+r.Name, synopsis(r.Operands), r.Summary)
	}
}

// synopsis is how --help spells an operand shape.
func synopsis(o Operands) string {
	switch o.(type) {
	case TakesPaths:
		return "path..."
	case TakesPairs:
		return "path value..."
	case TakesNothing:
		return ""
	}
	panic(fmt.Sprintf("cli: %T is not Operands", o))
}
