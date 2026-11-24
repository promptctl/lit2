package address

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/field"
)

// Refusal is why a path names nothing. Each names only paths, never a
// value, so the command event records it whole.
//
//sumtype:decl
type Refusal interface {
	error
	// Reason is the refusal's wire name.
	Reason() string
	refusal()
}

// Syntax is a path the grammar does not read, and why: an error that
// carries the refusal of the step that did not read.
type Syntax struct {
	Text string
	Why  error
}

// NotFound is an id that names no node.
type NotFound struct{ ID string }

// Ambiguous is an id that names two or more nodes, each listed by its full
// id: two writers offline can fix one segment for two nodes (§3.2).
type Ambiguous struct {
	ID         string
	Candidates []string
}

// UnknownField is a step no row takes at the node's place.
type UnknownField struct{ config.UnknownField }

// NotOnLevel is a step that names a row, but not at the node's place.
type NotOnLevel struct{ config.NotOnLevel }

// Unplaced is an item the fold cannot place, so it has no fields: its
// chain is broken or runs below a leaf. Repairing it is integrity's.
type Unplaced struct{ Item, Why string }

// NoElement is a key under a row that holds no members and no elements.
type NoElement struct {
	Path  field.Path
	Holds string
}

func (n NotFound) Error() string { return fmt.Sprintf("%s names nothing", n.ID) }
func (a Ambiguous) Error() string {
	return fmt.Sprintf("%s names %d nodes: %s", a.ID, len(a.Candidates), strings.Join(a.Candidates, ", "))
}
func (s Syntax) Error() string { return fmt.Sprintf("%q is not a path: %v", s.Text, s.Why) }
func (s Syntax) Unwrap() error { return s.Why }
func (u Unplaced) Error() string {
	return fmt.Sprintf("%s has no place in the tree: %s", u.Item, u.Why)
}
func (n NoElement) Error() string {
	return fmt.Sprintf("%s holds %s, so nothing below it has a name", n.Path, n.Holds)
}

func (Syntax) Reason() string       { return "syntax" }
func (NotFound) Reason() string     { return "not_found" }
func (Ambiguous) Reason() string    { return "ambiguous" }
func (UnknownField) Reason() string { return "unknown_field" }
func (NotOnLevel) Reason() string   { return "not_on_level" }
func (Unplaced) Reason() string     { return "unplaced" }
func (NoElement) Reason() string    { return "names_no_element" }

func (Syntax) refusal()       {}
func (NotFound) refusal()     {}
func (Ambiguous) refusal()    {}
func (UnknownField) refusal() {}
func (NotOnLevel) refusal()   {}
func (Unplaced) refusal()     {}
func (NoElement) refusal()    {}
