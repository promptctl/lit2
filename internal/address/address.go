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

// Node is what a path names before its field: the root, a home, an item,
// or an element of an item's list.
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

// Element is a created element of item Of's list, such as a comment: a
// node of its own, with fields, as a set's member is not. Rendered is its
// path, the item's, the list's and its own segment.
type Element struct {
	Of       events.ItemID
	ID       events.ElementID
	Rendered string
}

func (Root) node()    {}
func (Home) node()    {}
func (Item) node()    {}
func (Element) node() {}

// Target is what of a node a path names: the whole node, one row of it,
// or one member of a set.
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

// Member is one member of a set, by its name: a value, which holds no
// fields.
type Member struct {
	Path field.Path
	Decl field.FieldDecl
	Name events.Name
}

func (Whole) target()  {}
func (Field) target()  {}
func (Member) target() {}

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
	case Element:
		b.WriteString(n.Rendered)
	}
	switch t := a.Target.(type) {
	case Whole:
		if b.Len() == 0 {
			b.WriteString("/")
		}
	case Field:
		fmt.Fprintf(&b, "/%s", t.Path)
	case Member:
		fmt.Fprintf(&b, "/%s/%s", t.Path, t.Name)
	}
	return b.String()
}

// Parse reads one path against the state and the schema:
//
//	path  := '/' steps? | node ('/' steps)?
//	node  := home-name | id
//	steps := field ('/' member | '/' element ('/' steps)?)?
//
// It is the one maker of an Address. A field may itself hold a slash, as
// pr/verdict does, so the field is the longest run of steps the table has
// a row for, and the step after it a key. An element's key names a node
// of its own, whose steps are read at its place in turn. Its refusal says
// why the path names nothing.
func Parse(st fold.State, schema config.Schema, text string) (Address, Refusal) {
	if text == "" {
		return Address{}, Syntax{Text: text, Why: "a path is / or names a home or an item"}
	}
	if text != "/" && strings.HasSuffix(text, "/") || strings.Contains(text, "//") {
		return Address{}, Syntax{Text: text, Why: "a path has no empty step and does not end in /"}
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
	return target(schema.Table(), a, it, steps)
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

// target reads the steps after a's node against the table at its place.
// When no run of the steps is a row here, the refusal is the longest run
// that is a field at another place, or else the first step's.
func target(t config.Table, a Address, it fold.Item, steps string) (Address, Refusal) {
	r, refusal := t.Find(a.Place, field.Path(steps))
	if refusal == nil {
		a.Target = Field{Path: field.Path(steps), Row: r}
		return a, nil
	}
	for i := strings.LastIndex(steps, "/"); i >= 0; i = strings.LastIndex(steps[:i], "/") {
		path := field.Path(steps[:i])
		r, err := t.Find(a.Place, path)
		if err == nil {
			return keyed(t, a, it, path, r, steps[i+1:])
		}
		if _, elsewhere := refusal.(config.NotOnLevel); !elsewhere {
			refusal = err
		}
	}
	return Address{}, found(refusal)
}

// keyed reads the steps after row r of a's node: a member of a set, which
// is the last step, or an element of a list, whose own steps follow it.
func keyed(t config.Table, a Address, it fold.Item, path field.Path, r config.Row, steps string) (Address, Refusal) {
	row, isField := r.(config.FieldRow)
	if !isField {
		return Address{}, NoElement{Path: path, Holds: "a verdict"}
	}
	key, rest, deeper := strings.Cut(steps, "/")
	switch row.Decl.Multi {
	case field.Set:
		n, err := events.ParseName(key)
		if err != nil {
			return Address{}, Syntax{Text: key, Why: fmt.Sprintf("a member of %s is a name: %v", path, err)}
		}
		if deeper {
			return Address{}, NoElement{Path: path + "/" + field.Path(key), Holds: "a name"}
		}
		a.Target = Member{Path: path, Decl: row.Decl, Name: n}
		return a, nil
	case field.Elements:
		short, err := events.ParseShortID(key, events.Prefix{})
		if err != nil || strings.Contains(key, ".") {
			return Address{}, Syntax{Text: key, Why: fmt.Sprintf("an element of %s is named by its id or its one segment", path)}
		}
		id, seg, err := resolveElement(it, events.MustName(string(path)), short)
		if err != nil {
			return Address{}, lookup(err)
		}
		el := Address{Node: Element{Of: it.ID, ID: id, Rendered: fmt.Sprintf("%s/%s/%s", Address{Node: a.Node, Target: Whole{}}, path, seg)}, Place: field.Comment{}, Target: Whole{}}
		if !deeper {
			return el, nil
		}
		return target(t, el, it, rest)
	case field.One, field.Children:
		return Address{}, NoElement{Path: path, Holds: row.Decl.Multi.String()}
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
