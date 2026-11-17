package config

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
)

// Table is the field table of one version: every built-in field, every
// field a level declares, and every check, each one row. It is the one
// answer to what a path names at a place, which templates, tests, paths
// and help all read; if a field is not in it, it does not exist anywhere
// `[LAW:one-source-of-truth]`. Only Parse builds one.
type Table struct {
	rows   []Row
	levels []Level
}

// Row is one row of the table: a field, or a check.
//
//sumtype:decl
type Row interface {
	// Path is the name the row answers to.
	Path() field.Path
	row()
}

// FieldRow is a field: its path and its declaration.
type FieldRow struct {
	At   field.Path
	Decl field.FieldDecl
}

// CheckRow is a check, a derived row of every item: its value is whether
// its tests hold, which it carries.
type CheckRow struct{ Check Check }

func (r FieldRow) Path() field.Path { return r.At }
func (r CheckRow) Path() field.Path { return field.Path(r.Check.Name) }
func (FieldRow) row()               {}
func (CheckRow) row()               {}

// checkPlaces is where a check is a row: every item.
var checkPlaces = field.PlaceSet{Leaf: true, Node: true}

// at is whether r is a row at p.
func at(r Row, p field.Place) bool {
	switch r := r.(type) {
	case FieldRow:
		return r.Decl.At.Holds(p)
	case CheckRow:
		return checkPlaces.Holds(p)
	}
	panic(fmt.Sprintf("config: %T is not a Row", r))
}

// UnknownField is a path that names no row at its place. Known is every
// path that does, which is what help prints there.
type UnknownField struct {
	Path  field.Path
	Known []field.Path
}

// NotOnLevel is a path that names a row, but not at this place: status on
// a feature, children on a leaf.
type NotOnLevel struct {
	Path  field.Path
	Place field.Place
}

func (u UnknownField) Error() string {
	known := make([]string, len(u.Known))
	for i, k := range u.Known {
		known[i] = string(k)
	}
	return fmt.Sprintf("%s is no field here; the fields here are %s", u.Path, strings.Join(known, ", "))
}

func (n NotOnLevel) Error() string {
	return fmt.Sprintf("%s is a field, but not of %s", n.Path, PlaceName(n.Place))
}

// PlaceName is a place as a refusal and help name it.
func PlaceName(p field.Place) string {
	switch p := p.(type) {
	case field.Root:
		return "the root"
	case field.Home:
		return fmt.Sprintf("a home, which holds level %d's items", p.Level.Int())
	case field.Leaf:
		return "a leaf item, at level 1"
	case field.Node:
		return fmt.Sprintf("an item at level %d", p.Level.Int())
	case field.Comment:
		return "a comment"
	case field.MemberEntry:
		return "a member of the policy"
	}
	panic(fmt.Sprintf("config: %T is not a Place", p))
}

// Find is the row path names at p, the one lookup and the one place rule
// a template, a path and a child object's key all ask
// `[LAW:single-enforcer]`.
func (t Table) Find(p field.Place, path field.Path) (Row, error) {
	named := false
	for _, r := range t.rows {
		if r.Path() != path {
			continue
		}
		if at(r, p) {
			return r, nil
		}
		named = true
	}
	if named {
		return nil, NotOnLevel{Path: path, Place: p}
	}
	var known []field.Path
	for _, r := range t.Rows(p) {
		known = append(known, r.Path())
	}
	return nil, UnknownField{Path: path, Known: known}
}

// Named is every field path names at an item of any level: what a test's
// atom reads, since its scope can reach items of every level, and a
// comment's fields are no item's. None is a path no item's field takes.
func (t Table) Named(path field.Path) []field.FieldDecl {
	var out []field.FieldDecl
	for _, r := range t.rows {
		if f, ok := r.(FieldRow); ok && f.At == path && f.Decl.At.OfItems() {
			out = append(out, f.Decl)
		}
	}
	return out
}

// Rows is the rows at p, by path.
func (t Table) Rows(p field.Place) []Row {
	var out []Row
	for _, r := range t.rows {
		if at(r, p) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Row) int { return cmp.Compare(a.Path(), b.Path()) })
	return out
}

// Places is every place the version has, top first: the root, then each
// level from the top, its home if it declares one and then its items, a
// comment, which an item of any level holds, and last a member of the
// policy, which the root holds.
func (t Table) Places() []field.Place {
	out := []field.Place{field.Root{}}
	for n := len(t.levels); n >= 1; n-- {
		l, _ := events.ParseLevel(n)
		if t.levels[n-1].Home != "" {
			out = append(out, field.Home{Level: l})
		}
		out = append(out, field.ItemPlace(l))
	}
	return append(out, field.Comment{}, field.MemberEntry{})
}

// Origins is how many rows each origin gives: built-in fields, declared
// fields, and checks.
func (t Table) Origins() (builtin, declared, checks int) {
	for _, r := range t.rows {
		switch r := r.(type) {
		case FieldRow:
			if r.Decl.Origin == field.Builtin {
				builtin++
			} else {
				declared++
			}
		case CheckRow:
			checks++
		}
	}
	return builtin, declared, checks
}
