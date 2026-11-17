package cli

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/verbs"
)

// Verbs is the verb table: the four verbs on paths.
func Verbs() Table {
	return Table{rows: []Row{
		{Name: "get", Operands: TakesPaths{Make: func(p []PathText, _ Flags) Command { return Get{paths: p} }}, Summary: "read the nodes at these paths"},
		{Name: "set", Operands: TakesPairs{Make: func(p []Pair, f Flags) Command { return Set{pairs: p, flags: f} }}, DryRun: true, Summary: "replace the value at each path"},
		{Name: "add", Operands: TakesPairs{Make: func(p []Pair, f Flags) Command { return Add{pairs: p, flags: f} }}, DryRun: true, Summary: "append each value to the list at its path"},
		{Name: "rm", Operands: TakesPaths{Make: func(p []PathText, f Flags) Command { return Rm{paths: p, flags: f} }}, DryRun: true, Summary: "delete the nodes at these paths"},
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

// HelpText is --help: the two tables, and the field table at each place,
// rendered from the rows the parser and the paths read, each field with
// its kind and the verbs that write it.
func HelpText(verbTable, admin Table, fields config.Table) string {
	var b strings.Builder
	b.WriteString("lit2 keeps this repository's backlog as a tree of nodes in its own git object database.\n\n")
	b.WriteString("usage: lit2 <verb> <operand>...   lit2 admin <command>   lit2 --help\n")
	b.WriteString("lit2 alone is get /: the tree, the lanes in rank order, and what is next\n")
	b.WriteString("--help anywhere prints this; --dry-run shows what a write would do; -- ends the flags; - is a value\n")
	section(&b, "verbs", "", verbTable)
	section(&b, "admin", adminWord+" ", admin)
	for _, p := range fields.Places() {
		fmt.Fprintf(&b, "\nfields of %s:\n", config.PlaceName(p))
		rows := fields.Rows(p)
		var paths, kinds int
		for _, r := range rows {
			paths, kinds = max(paths, len(r.Path())), max(kinds, len(kindOf(r)))
		}
		for _, r := range rows {
			writers := strings.Join(verbs.VerbsOf(r), ", ")
			if writers == "" {
				writers = "derived"
			}
			fmt.Fprintf(&b, "  %-*s %-*s %s\n", paths, r.Path(), kinds, kindOf(r), writers)
		}
	}
	return b.String()
}

// kindOf is a row's kind as help names it: a field's kind and how many
// it holds, or a check.
func kindOf(r config.Row) string {
	switch r := r.(type) {
	case config.FieldRow:
		switch r.Decl.Multi {
		case field.One:
			return r.Decl.Kind.Name()
		case field.Set:
			return "set-of-" + r.Decl.Kind.Name()
		case field.Elements, field.Children:
			return "list-of-" + r.Decl.Kind.Name()
		}
		panic(fmt.Sprintf("cli: %d is not a Multiplicity", r.Decl.Multi))
	case config.CheckRow:
		return "check"
	}
	panic(fmt.Sprintf("cli: %T is not a Row", r))
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
