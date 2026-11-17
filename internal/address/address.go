// Package address is what can be named: the path grammar, and later
// selectors and globs.
//
// Its layer in architecture.md §6: path grammar; selectors; globs (§3.1,
// §5 step 4). A path parses once into a typed address and every verb
// takes the address `[LAW:parse-dont-validate]`. Which fields a node has
// is the field table's, config's: a path reads it and never keeps its own.
package address

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// Address is a path parsed against the state and the table: the node it
// names, that node's place, and what of the node it names. Only Parse
// makes one.
type Address struct {
	Node   Node
	Place  field.Place
	Target Target
}

// Node is what a path's first step names: the root, a home, or an item.
//
//sumtype:decl
type Node interface{ node() }

// Root is the workspace root, /.
type Root struct{}

// Home is a level's declared home, by its name.
type Home struct {
	Level events.Level
	Name  string
}

// Item is a created item, and its path as it renders.
type Item struct {
	ID       events.ItemID
	Rendered string
}

func (Root) node() {}
func (Home) node() {}
func (Item) node() {}

// Target is what of a node a path names: the whole node, one row of it,
// or one element of a row that holds members or elements.
//
//sumtype:decl
type Target interface{ target() }

// Whole is the node itself.
type Whole struct{}

// Field is one row of the node.
type Field struct {
	Path field.Path
	Row  config.Row
}

// Element is one member of a set, or one element of a list such as a
// comment.
type Element struct {
	Path field.Path
	Decl field.FieldDecl
	Key  Key
}

func (Whole) target()   {}
func (Field) target()   {}
func (Element) target() {}

// Key is how an element is named: a set's member by its name, or a list's
// element by its id.
//
//sumtype:decl
type Key interface{ key() }

// Member is a set's member.
type Member struct{ Name events.Name }

// ElementKey is one element of a list, and its segment as it renders.
type ElementKey struct {
	ID       events.ElementID
	Rendered string
}

func (Member) key()     {}
func (ElementKey) key() {}

// String is the address as it renders: the canonical spelling of the node
// and of what it names, whatever the path typed.
func (a Address) String() string {
	var b strings.Builder
	switch n := a.Node.(type) {
	case Root:
	case Home:
		b.WriteString(n.Name)
	case Item:
		b.WriteString(n.Rendered)
	}
	switch t := a.Target.(type) {
	case Whole:
		if b.Len() == 0 {
			b.WriteString("/")
		}
	case Field:
		fmt.Fprintf(&b, "/%s", t.Path)
	case Element:
		fmt.Fprintf(&b, "/%s/", t.Path)
		switch k := t.Key.(type) {
		case Member:
			b.WriteString(k.Name.String())
		case ElementKey:
			b.WriteString(k.Rendered)
		}
	}
	return b.String()
}

// Parse reads one path against the state and the schema:
//
//	path  := '/' steps? | node ('/' steps)?
//	node  := home-name | id
//	steps := field ('/' key)?
//
// It is the one maker of an Address. A field may itself hold a slash, as
// pr/verdict does, so the whole of the steps is a field when the table has
// one by that path, and its last step a key otherwise. Its refusal says
// why the path names nothing.
func Parse(st fold.State, schema config.Schema, text string) (Address, Refusal) {
	if text == "" {
		return Address{}, Syntax{Text: text, Why: "a path is / or names a home or an item"}
	}
	head, steps, hasSteps := strings.Cut(text, "/")
	var a Address
	var it fold.Item
	if head == "" {
		a.Node, a.Place = Root{}, field.Root{}
		hasSteps = steps != ""
	} else {
		var r Refusal
		if a.Node, a.Place, it, r = node(st, schema, head); r != nil {
			return Address{}, r
		}
	}
	if !hasSteps {
		a.Target = Whole{}
		return a, nil
	}
	if steps == "" {
		return Address{}, Syntax{Text: text, Why: "a path does not end in /"}
	}
	t, r := target(schema.Table(), a.Place, it, steps)
	if r != nil {
		return Address{}, r
	}
	a.Target = t
	return a, nil
}

// node reads a path's first step: a declared home's name, or an item's id.
func node(st fold.State, schema config.Schema, head string) (Node, field.Place, fold.Item, Refusal) {
	for n := 2; n <= schema.Levels(); n++ {
		if lv, _ := schema.Level(n); lv.Home == head {
			l, _ := events.ParseLevel(n)
			return Home{Level: l, Name: head}, field.Home{Level: l}, fold.Item{}, nil
		}
	}
	short, err := events.ParseShortID(head, schema.Prefix())
	if err != nil {
		return nil, nil, fold.Item{}, Syntax{Text: head, Why: err.Error() + ", nor a home"}
	}
	id, err := Resolve(st, short)
	if err != nil {
		return nil, nil, fold.Item{}, lookup(err)
	}
	it := st.Item(id).(fold.Found).Item
	rendered := events.Render(schema.Prefix(), id, st.Path(id))
	switch l := it.Level.(type) {
	case fold.Leveled:
		return Item{ID: id, Rendered: rendered}, field.ItemPlace(l.Level), it, nil
	case fold.Orphaned:
		return nil, nil, fold.Item{}, Unplaced{Item: rendered, Why: fmt.Sprintf("its parent chain reaches %s, which nothing created before it", l.Missing)}
	case fold.UnderLeaf:
		return nil, nil, fold.Item{}, Unplaced{Item: rendered, Why: fmt.Sprintf("its parent chain runs below %s, a leaf", l.Leaf)}
	}
	panic(fmt.Sprintf("address: %T is not a LevelOf", it.Level))
}

// target reads the steps after the node against the table at its place.
func target(t config.Table, p field.Place, it fold.Item, steps string) (Target, Refusal) {
	whole := field.Path(steps)
	r, err := t.Find(p, whole)
	if err == nil {
		return Field{Path: whole, Row: r}, nil
	}
	i := strings.LastIndex(steps, "/")
	if i < 0 {
		return nil, found(err)
	}
	path, key := field.Path(steps[:i]), steps[i+1:]
	if r, err = t.Find(p, path); err != nil {
		return nil, found(err)
	}
	row, isField := r.(config.FieldRow)
	if !isField {
		return nil, NoElement{Path: path, Holds: "a verdict"}
	}
	switch row.Decl.Multi {
	case field.Set:
		n, err := events.ParseName(key)
		if err != nil {
			return nil, Syntax{Text: key, Why: fmt.Sprintf("a member of %s is a name: %v", path, err)}
		}
		return Element{Path: path, Decl: row.Decl, Key: Member{Name: n}}, nil
	case field.Elements:
		short, err := events.ParseShortID(key, events.Prefix{})
		if err != nil || strings.Contains(key, ".") {
			return nil, Syntax{Text: key, Why: fmt.Sprintf("an element of %s is named by its id or its one segment", path)}
		}
		k, err := resolveElement(it, events.MustName(string(path)), short)
		if err != nil {
			return nil, lookup(err)
		}
		return Element{Path: path, Decl: row.Decl, Key: k}, nil
	case field.One, field.Children:
		return nil, NoElement{Path: path, Holds: row.Decl.Multi.String()}
	}
	panic(fmt.Sprintf("address: %d is not a Multiplicity", row.Decl.Multi))
}

// found is the table's refusal as a path's.
func found(err error) Refusal {
	switch e := err.(type) {
	case config.UnknownField:
		return UnknownField{e}
	case config.NotOnLevel:
		return NotOnLevel{e}
	}
	panic(fmt.Sprintf("address: Find answered %T", err))
}

// lookup is resolve's refusal as a path's.
func lookup(err error) Refusal {
	switch e := err.(type) {
	case NotFound:
		return e
	case Ambiguous:
		return e
	}
	panic(fmt.Sprintf("address: resolve answered %T", err))
}
