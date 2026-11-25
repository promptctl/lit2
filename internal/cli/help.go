package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/verbs"
)

// Lit2 is lit2's command line, in the order --help prints it: the four
// verbs on paths, then admin, what keeps a checkout's store, and config,
// what applies the workspace's versions, both outside the everyday
// surface. A bare lit2 is get /. Its error is NewCommands's, a row this
// file got wrong, which a test that builds it catches.
func Lit2() (Commands, error) {
	return NewCommands([]Row{
		{Name: commandname.Get(), Operands: TakesPaths{Make: func(p []PathText, _ Flags) Command { return Get{paths: p} }}, Summary: "read the nodes at these paths"},
		{Name: commandname.Set(), Operands: TakesPairs{Make: func(p []Pair, f Flags) Command { return Set{pairs: p, flags: f} }}, DryRun: true, Summary: "replace the value at each path"},
		{Name: commandname.Add(), Operands: TakesPairs{Make: func(p []Pair, f Flags) Command { return Add{pairs: p, flags: f} }}, DryRun: true, Summary: "append each value to the list at its path"},
		{Name: commandname.Rm(), Operands: TakesPaths{Make: func(p []PathText, f Flags) Command { return Rm{paths: p, flags: f} }}, DryRun: true, Summary: "delete the nodes at these paths"},
		{Name: commandname.AdminInit(), Operands: TakesNothing{Make: func() Command { return AdminInit{} }}, Summary: "make this checkout a writer: its id, its principal, the workspace's first version"},
		{Name: commandname.AdminSync(), Operands: TakesNothing{Make: func() Command { return AdminSync{} }}, Summary: "publish this checkout's events and fetch and verify every writer's"},
		{Name: commandname.ConfigCommit(), Operands: TakesNothing{Make: func() Command { return ConfigCommit{} }}, Summary: "apply the committed " + verbs.WorkspacePath() + " as the next version"},
		{Name: commandname.ConfigStatus(), Operands: TakesNothing{Make: func() Command { return ConfigStatus{} }}, Summary: "the version in force, the file against it, and any contending"},
	}, Bare{Name: commandname.Get(), Operands: []string{"/"}, Shows: "the tree, the lanes in rank order, what is next, and the version in force"})
}

// Fields is the field table help prints: the one the version in force
// declares, or none, and why.
//
//sumtype:decl
type Fields interface{ fields() }

// ActiveFields is the version in force's field table.
type ActiveFields struct{ Table config.Table }

// NoFields is a run with no version to read fields from.
type NoFields struct{ Why string }

func (ActiveFields) fields() {}
func (NoFields) fields()     {}

// HelpText is --help: the verbs and each group, and the field table at each
// place, rendered from the rows the parser and the paths read, each field
// with its kind and the verbs that write it.
func HelpText(c Commands, fields Fields) string {
	sections := c.sections()
	var b strings.Builder
	b.WriteString("lit2 keeps this repository's backlog as a tree of nodes in its own git object database.\n\n")
	b.WriteString("usage: lit2 <verb> <operand>...")
	for _, s := range sections[1:] {
		fmt.Fprintf(&b, "   lit2 %s <command>", s.title)
	}
	fmt.Fprintf(&b, "   lit2 %s\n", helpFlag)
	fmt.Fprintf(&b, "lit2 alone is %s: %s\n", strings.Join(append(c.bare.Name.Words(), c.bare.Operands...), " "), c.bare.Shows)
	fmt.Fprintf(&b, "%s anywhere prints this; %s shows what a write would do; %s ends the flags; - is a value\n", helpFlag, dryRunFlag, endFlags)
	for _, s := range sections {
		fmt.Fprintf(&b, "\n%s:\n", s.title)
		for _, r := range s.rows {
			fmt.Fprintf(&b, "  %-14s %-15s %s\n", r.Name, synopsis(r.Operands), r.Summary)
		}
	}
	switch f := fields.(type) {
	case ActiveFields:
		fieldSections(&b, f.Table)
	case NoFields:
		fmt.Fprintf(&b, "\nfields: %s\n", f.Why)
	}
	return b.String()
}

// fieldSections is the field table at each place.
func fieldSections(b *strings.Builder, fields config.Table) {
	for _, p := range fields.Places() {
		fmt.Fprintf(b, "\nfields of %s:\n", config.PlaceName(p))
		rows := fields.Rows(p)
		var paths, kinds int
		for _, r := range rows {
			paths, kinds = max(paths, len(r.Path())), max(kinds, len(kindOf(r)))
		}
		for _, r := range rows {
			writers := strings.Join(verbs.VerbsOf(r), ", ")
			if no := verbs.Unwritten(r); no != nil {
				writers = no.Reason()
			}
			fmt.Fprintf(b, "  %-*s %-*s %s\n", paths, r.Path(), kinds, kindOf(r), writers)
		}
	}
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
		case field.Elements, field.Children, field.Principals:
			return "list-of-" + r.Decl.Kind.Name()
		}
		panic(fmt.Sprintf("cli: %d is not a Multiplicity", r.Decl.Multi))
	case config.CheckRow:
		return "check"
	}
	panic(fmt.Sprintf("cli: %T is not a Row", r))
}

// section is one block of --help: the verbs, or one group's commands.
type section struct {
	title string
	rows  []Row
}

// sections is c's rows by the group each name is in, in the order each
// group first appears: the verbs always first, then every group.
func (c Commands) sections() []section {
	all := []section{{title: "verbs"}}
	for _, r := range c.rows {
		g, in := r.Name.Group()
		if !in {
			all[0].rows = append(all[0].rows, r)
			continue
		}
		i := slices.IndexFunc(all[1:], func(s section) bool { return s.title == g })
		if i < 0 {
			all = append(all, section{title: g})
			i = len(all) - 2
		}
		all[i+1].rows = append(all[i+1].rows, r)
	}
	return all
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
