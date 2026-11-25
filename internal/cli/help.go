package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/verbs"
)

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
	verbRows, groups := c.sections()
	var b strings.Builder
	b.WriteString("lit2 keeps this repository's backlog as a tree of nodes in its own git object database.\n\n")
	b.WriteString("usage: lit2 <verb> <operand>...")
	for _, g := range groups {
		fmt.Fprintf(&b, "   lit2 %s <command>", g.word)
	}
	fmt.Fprintf(&b, "   lit2 %s\n", helpFlag)
	fmt.Fprintf(&b, "lit2 alone is %s: %s\n", strings.Join(c.bareWords, " "), c.bare.Shows)
	for _, f := range theFlags() {
		fmt.Fprintf(&b, "%s %s; ", f.name, f.does)
	}
	fmt.Fprintf(&b, "%s ends the flags; - is a value\n", endFlags)
	rowLines(&b, "verbs", verbRows)
	for _, g := range groups {
		rowLines(&b, g.word, g.rows)
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

// group is one group's commands, as --help prints them.
type group struct {
	word string
	rows []Row
}

// sections is c's rows by where each name is: the verbs, and each group
// in the order it first appears.
func (c Commands) sections() (verbRows []Row, groups []group) {
	for _, r := range c.rows {
		word, in := r.Name.Group()
		if !in {
			verbRows = append(verbRows, r)
			continue
		}
		i := slices.IndexFunc(groups, func(g group) bool { return g.word == word })
		if i < 0 {
			groups, i = append(groups, group{word: word}), len(groups)
		}
		groups[i].rows = append(groups[i].rows, r)
	}
	return verbRows, groups
}

// rowLines is one block of --help: its title, then a line per row.
func rowLines(b *strings.Builder, title string, rows []Row) {
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, r := range rows {
		fmt.Fprintf(b, "  %-14s %-15s %s\n", r.Name, synopsis(r.Operands), r.Summary)
	}
}

// synopsis is how --help spells an operand shape.
func synopsis(o Operands) string {
	switch o.(type) {
	case TakesPaths, TakesPathsDryRun:
		return "path..."
	case TakesPairsDryRun:
		return "path value..."
	case TakesNothing:
		return ""
	}
	panic(fmt.Sprintf("cli: %T is not Operands", o))
}
