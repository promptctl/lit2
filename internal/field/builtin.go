package field

import (
	"fmt"

	"github.com/promptctl/lit2/internal/events"
)

// Path names a field of an item by its slash path: `description`,
// `plan/verdict`. It is the name a test's atom reads and a template's key
// names.
type Path string

// Origin is who declared a field: the binary, or the workspace file.
type Origin uint8

const (
	Builtin Origin = iota + 1
	Declared
)

// Writability is whether a person may write a field. It is independent
// of Origin: a built-in field can be settable, and a declared one derived
// `[LAW:types-are-the-program]`.
type Writability uint8

const (
	Settable Writability = iota + 1
	Derived
)

// Multiplicity is how a field holds its values: one value; a set of
// members, each a name; an item's elements, such as its comments, each
// keyed by the id minted for it; or the items placed below a node.
type Multiplicity uint8

const (
	One Multiplicity = iota + 1
	Set
	Elements
	Children
)

func (m Multiplicity) String() string {
	switch m {
	case One:
		return "one value"
	case Set:
		return "a set"
	case Elements:
		return "a list of elements"
	case Children:
		return "the items below"
	}
	panic(fmt.Sprintf("field: %d is not a Multiplicity", uint8(m)))
}

// FieldDecl describes one field, built-in or declared, in one shape: what it
// holds and how many, who declared it, whether a person can write it, and
// where it exists.
type FieldDecl struct {
	Kind        Kind
	Multi       Multiplicity
	Origin      Origin
	Writability Writability
	At          Places
}

// Place is where a node sits, which decides the fields it has: the root,
// a level's home, a leaf item, an item above the leaf, or a comment. An
// address derives it once from the node it names.
//
//sumtype:decl
type Place interface{ place() }

// Root is the workspace root, /.
type Root struct{}

// Home is the home a level declares, which holds that level's items.
type Home struct{ Level events.Level }

// Leaf is an item at level 1.
type Leaf struct{}

// Node is an item above level 1.
type Node struct{ Level events.Level }

// Comment is an element of an item's comments: a node of its own, with
// fields, as a set's member is not.
type Comment struct{}

func (Root) place()    {}
func (Home) place()    {}
func (Leaf) place()    {}
func (Node) place()    {}
func (Comment) place() {}

// ItemPlace is the place of an item at level l: a leaf at level 1, a node
// above it. It is the one reading of a level as a place
// `[LAW:one-source-of-truth]`.
func ItemPlace(l events.Level) Place {
	if _, below := l.Below(); below {
		return Node{Level: l}
	}
	return Leaf{}
}

// Places is the places a field exists at.
//
//sumtype:decl
type Places interface {
	Holds(Place) bool
	places()
}

// PlaceSet is a built-in field's places: every place of each kind it names.
type PlaceSet struct{ Root, Home, Leaf, Node, Comment bool }

// OnLevel is a declared field's place: the items of the level that declares
// it.
type OnLevel struct{ Level events.Level }

func (PlaceSet) places() {}
func (OnLevel) places()  {}

// Holds is whether p is of a kind the set names.
func (s PlaceSet) Holds(p Place) bool {
	switch p.(type) {
	case Root:
		return s.Root
	case Home:
		return s.Home
	case Leaf:
		return s.Leaf
	case Node:
		return s.Node
	case Comment:
		return s.Comment
	}
	panic(fmt.Sprintf("field: %T is not a Place", p))
}

// Holds is whether p is an item of the level.
func (o OnLevel) Holds(p Place) bool {
	switch p := p.(type) {
	case Leaf:
		_, below := o.Level.Below()
		return !below
	case Node:
		return p.Level == o.Level
	case Root, Home, Comment:
		return false
	}
	panic(fmt.Sprintf("field: %T is not a Place", p))
}

// DerivedField is a write of a field the engine computes.
type DerivedField struct{}

func (DerivedField) Error() string { return "it is derived: the engine computes it" }

// NotKeyable is a template default or a child object's key naming a field
// that holds neither one value nor a set.
type NotKeyable struct{ Multi Multiplicity }

func (n NotKeyable) Error() string {
	return fmt.Sprintf("it holds %s, and a template or a child's key sets one value or a set's members", n.Multi)
}

// Writable is whether a person may write d: the one rule a write, a
// template default and help all ask `[LAW:single-enforcer]`. Where d
// exists is the table's to say.
func Writable(d FieldDecl) error {
	if d.Writability == Derived {
		return DerivedField{}
	}
	return nil
}

// Keyable is whether a template default or a key of a child object may
// name d: they set a register or a set's members, never a list of
// elements or the items below `[LAW:single-enforcer]`.
func Keyable(d FieldDecl) error {
	if d.Multi != One && d.Multi != Set {
		return NotKeyable{Multi: d.Multi}
	}
	return nil
}

// The built-in fields' places.
var (
	items      = PlaceSet{Leaf: true, Node: true}
	leaves     = PlaceSet{Leaf: true}
	containers = PlaceSet{Root: true, Home: true, Node: true}
	comments   = PlaceSet{Comment: true}
	removable  = PlaceSet{Leaf: true, Node: true, Comment: true}
)

// The built-in fields the verbs name: the title every new item carries,
// and the flag rm sets on any node it removes.
const (
	Title   Path = "title"
	Deleted Path = "deleted"
)

// Builtins is the built-in field table. Its one input is the workspace's
// declared labels as an enum, so `labels` reads the declared value set and
// nothing else. A zero Enum is no labels declared.
func Builtins(labels Enum) map[Path]FieldDecl {
	verdict, _ := NewEnum([]string{"pass", "fail"})
	deleted, _ := NewEnum([]string{"true", "false"})
	stored := func(k Kind, m Multiplicity, at Places) FieldDecl {
		return FieldDecl{Kind: k, Multi: m, Origin: Builtin, Writability: Settable, At: at}
	}
	derived := func(k Kind, m Multiplicity) FieldDecl {
		return FieldDecl{Kind: k, Multi: m, Origin: Builtin, Writability: Derived, At: items}
	}
	return map[Path]FieldDecl{
		Title:          stored(Text{}, One, items),
		"description":  stored(Text{}, One, items),
		"reason":       stored(Text{}, One, items),
		"labels":       stored(labels, Set, items),
		Deleted:        stored(deleted, One, removable),
		"status":       stored(Column{}, One, leaves),
		"comments":     stored(Text{}, Elements, items),
		"body":         stored(Text{}, One, comments),
		"children":     stored(ItemRef{}, Children, containers),
		"level":        derived(Level{}, One),
		"parent":       derived(ItemRef{}, One),
		"of":           derived(Text{}, Set),
		"blockers":     derived(Text{}, Set),
		"pr/verdict":   derived(verdict, One),
		"plan/verdict": derived(verdict, One),
	}
}
