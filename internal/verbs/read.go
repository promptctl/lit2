package verbs

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/tick"
)

// World is what a verb reads: the folded state, judged under the schema
// in force.
type World struct {
	State  fold.State
	Schema config.Schema
}

// Read is the values the row at path holds on node n, at place p: the one
// reader get and a check's tests call `[LAW:single-enforcer]`. A stored
// field reads through stored; comments read the item's elements; children
// what is placed there; a derived field its derivation; a check its
// verdict. A status the configuration cannot judge is its refusal, as a
// write's is.
func Read(w World, n address.Node, p field.Place, path field.Path, row config.Row) ([]string, error) {
	if c, ok := row.(config.CheckRow); ok {
		unmet, err := Judge(w, n.(address.Item).ID, c.Check.Tests)
		return verdict(unmet), err
	}
	d := row.(config.FieldRow).Decl
	if d.Multi == field.Children {
		return children(w, n), nil
	}
	it := w.item(n.(address.Item).ID)
	if d.Writability == field.Derived {
		derive, ok := derivations[path]
		if !ok {
			panic(fmt.Sprintf("verbs: %s is derived and has no derivation", path))
		}
		return derive(w, it), nil
	}
	if d.Multi == field.Elements {
		var out []string
		for _, e := range elements(it, events.MustName(string(path))) {
			out = append(out, e.text)
		}
		return out, nil
	}
	return stored(w.Schema, it.Facts, tick.ItemAncestry(w.State, w.Schema, it.ID), path, d)
}

// effective is the graph an item is judged under, or the configuration's
// refusal of it: the one call a read and a write make, so a fault in the
// configuration refuses both alike.
func effective(schema config.Schema, in config.Ancestry) (config.Effective, error) {
	eff, err := schema.Effective(in)
	var refused *config.ResolutionRefused
	if err != nil && !errors.As(err, &refused) {
		panic(fmt.Sprintf("verbs: Effective answered %v", err))
	}
	return eff, err
}

// stored is a stored register's or set's values in facts: prose opened,
// a plain value as its canonical text, a status through the graph the
// item is judged under, a set as its live members. A write reads the same
// function on its subject's facts, so what it compares against is what get
// prints `[LAW:one-source-of-truth]`.
func stored(schema config.Schema, facts fold.Facts, in config.Ancestry, path field.Path, d field.FieldDecl) ([]string, error) {
	n := events.MustName(string(path))
	switch d.Multi {
	case field.Set:
		var out []string
		for _, m := range facts.Members(n) {
			out = append(out, m.Name.String())
		}
		return out, nil
	case field.One:
	default:
		panic(fmt.Sprintf("verbs: %s holds %s, which no register stores", path, d.Multi))
	}
	if _, column := d.Kind.(field.Column); column {
		eff, err := effective(schema, in)
		if err != nil {
			return nil, err
		}
		return []string{tick.StatusIn(eff, facts)}, nil
	}
	if _, prose := d.Kind.(field.Text); prose {
		return texts(facts.Text(n)), nil
	}
	switch f := facts.Plain(n).(type) {
	case fold.Unset:
		return nil, nil
	case fold.Settled:
		return []string{f.Value}, nil
	}
	panic(fmt.Sprintf("verbs: %T is not a PlainField", facts.Plain(n)))
}

// texts is a prose field's live texts, opened.
func texts(f fold.TextField) []string {
	switch f := f.(type) {
	case fold.Unset:
		return nil
	case fold.Agreed:
		return []string{open(f.Body)}
	case fold.Conflicted:
		var out []string
		for _, c := range f.Candidates {
			out = append(out, open(c.Value.(events.Prose).Body))
		}
		return out
	}
	panic(fmt.Sprintf("verbs: %T is not a TextField", f))
}

// open reads an envelope: its text, or what kept it closed.
func open(e events.Envelope) string {
	switch o := events.Open(e, events.NewKeyring()).(type) {
	case events.Text:
		return o.Text
	case events.Locked:
		return fmt.Sprintf("(locked under tier %v)", o.Tier)
	case events.Unopenable:
		return "(a form this lit2 cannot open)"
	}
	panic("verbs: Open answered no Opened")
}

// element is one created element of a list: its key as it renders, who
// wrote it, and its text.
type element struct {
	key, by, text string
}

// elements is the created elements of it in the list n, in the order their
// creates sort.
func elements(it fold.Item, n events.Name) []element {
	var out []element
	for _, e := range it.Elements {
		c, ok := fold.CreateOf(e.Origin)
		if !ok || e.In != n {
			continue
		}
		text := ""
		if t := texts(e.Facts.Text(fold.Body)); len(t) > 0 {
			text = t[0]
		}
		out = append(out, element{key: c.Segment.String(), by: writer(c.Stamp.By), text: text})
	}
	return out
}

// writer is who wrote an event, as a reader names them: the principal,
// and the agent the harness labeled it with.
func writer(a events.Attribution) string {
	if l, ok := a.Agent.(events.Labeled); ok {
		return a.Principal.String() + " via " + l.String()
	}
	return a.Principal.String()
}

// children is what is placed at n, deleted items included: filtering is a
// selector's (architecture.md). The root holds the declared homes and the
// parentless items of every level that declares none; a home its level's
// parentless items; an item the items below it.
func children(w World, n address.Node) []string {
	var ids []events.ItemID
	var out []string
	switch n := n.(type) {
	case address.Root:
		for l := w.Schema.Levels(); l >= 1; l-- {
			lv, _ := w.Schema.Level(l)
			if lv.Home != "" {
				out = append(out, lv.Home)
				continue
			}
			level, _ := events.ParseLevel(l)
			ids = append(ids, w.State.Children(fold.Home{Holds: level})...)
		}
	case address.Home:
		ids = w.State.Children(fold.Home{Holds: n.Level})
	case address.Item:
		ids = w.State.Children(fold.Under{Parent: n.ID})
	}
	for _, id := range ids {
		out = append(out, w.render(id))
	}
	return out
}

// derivations is every derived built-in's reading. A link, a lane or a
// review is not folded at this slice, so of and blockers are truthfully
// empty and the verdicts truthfully unset; a test holds every Derived
// row to having one here.
var derivations = map[field.Path]func(World, fold.Item) []string{
	"level": func(_ World, it fold.Item) []string {
		if l, ok := it.Level.(fold.Leveled); ok {
			return []string{strconv.Itoa(l.Level.Int())}
		}
		return nil
	},
	"parent":       func(w World, it fold.Item) []string { return []string{w.container(w.State.Container(it.ID))} },
	"of":           func(World, fold.Item) []string { return nil },
	"blockers":     func(World, fold.Item) []string { return nil },
	"pr/verdict":   func(World, fold.Item) []string { return nil },
	"plan/verdict": func(World, fold.Item) []string { return nil },
}

// container is where a thing sits, as a path names it: an item, a home, or
// the root, which holds the items of a level with no home.
func (w World) container(c fold.Container) string {
	switch c := c.(type) {
	case fold.Under:
		return w.render(c.Parent)
	case fold.Home:
		if lv, ok := w.Schema.Level(c.Holds.Int()); ok && lv.Home != "" {
			return lv.Home
		}
		return "/"
	case nil:
		return "/"
	}
	panic(fmt.Sprintf("verbs: %T is not a Container", c))
}

// render is an item as its path renders under the active prefix.
func (w World) render(id events.ItemID) string {
	return events.Render(w.Schema.Prefix(), id, w.State.Path(id))
}

// item is the folded item an address resolved, which exists.
func (w World) item(id events.ItemID) fold.Item {
	return w.State.Item(id).(fold.Found).Item
}

// Judge is the tests of tests that do not hold on item id: the one
// evaluation of a flat test list, which a check's read calls
// `[LAW:single-enforcer]`. A self test reads the item; an ancestors test
// the item and each ancestor its one ancestry names, a positive test
// holding when one of them holds it and a negative one when all do. A
// test that reads a status the configuration cannot judge is its refusal.
func Judge(w World, id events.ItemID, tests []config.Test) ([]config.Test, error) {
	var unmet []config.Test
	for _, t := range tests {
		scope := []events.ItemID{id}
		if t.Scope == config.Ancestors {
			scope = nil
			for _, k := range tick.ItemAncestry(w.State, w.Schema, id) {
				if it, ok := k.Item(); ok {
					scope = append(scope, it)
				}
			}
		}
		held, err := holds(w, t.Atom, scope)
		if err != nil {
			return nil, err
		}
		if !held {
			unmet = append(unmet, t)
		}
	}
	return unmet, nil
}

// holds is whether atom holds over scope: on one item for a positive
// atom, on every item for a negative one. An item whose place has no field
// at the atom's path holds nothing there.
func holds(w World, atom field.Atom, scope []events.ItemID) (bool, error) {
	_, negative := atom.Op().(field.Absent)
	if _, ne := atom.Op().(field.NotEquals); ne {
		negative = true
	}
	for _, id := range scope {
		it := w.item(id)
		l, placed := it.Level.(fold.Leveled)
		if !placed {
			continue
		}
		place := field.ItemPlace(l.Level)
		var kind field.Kind = field.Text{}
		var found field.Found
		if r, err := w.Schema.Table().Find(place, atom.Path()); err == nil {
			if f, ok := r.(config.FieldRow); ok {
				kind = f.Decl.Kind
			}
			if found, err = Read(w, address.Item{ID: id, Rendered: w.render(id)}, place, atom.Path(), r); err != nil {
				return false, err
			}
		}
		if bool(atom.Holds(kind, found)) != negative {
			return !negative, nil
		}
	}
	return negative, nil
}

// verdict is a check's value: holds, or each test that does not, named by
// the check that declared it.
func verdict(unmet []config.Test) []string {
	if len(unmet) == 0 {
		return []string{"holds"}
	}
	var out []string
	for _, t := range unmet {
		out = append(out, fmt.Sprintf("unmet %s (%s)", t.Atom, t.DeclaredIn))
	}
	return out
}
