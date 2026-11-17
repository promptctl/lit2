package verbs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// World is what a verb reads: the view of the folded state under the
// schema in force at now, so every read is evaluate's (architecture.md §3.3),
// and the workspace file as the root read it, which only the root's
// workspace line reads.
type World struct {
	evaluate.View
	File File
	// Me is the principal this checkout writes as, whose approval a
	// governed write gives when it alone suffices.
	Me events.PrincipalID
}

// Read is the values the row at path holds on node n: the one
// reader get and a check's tests call `[LAW:single-enforcer]`, but for a
// status get prints, which it reads as the view's Column so a name the
// graph no longer declares shows marked; both read evaluate.StatusIn. A stored
// field reads through stored, on an item or a comment alike; comments
// read the item's elements rm has not removed, since only a check's test
// reads a list here and get lists each element itself; children what is placed there; a derived
// field its derivation; a check its verdict. Only an item's table has the
// rows that are no stored field. A status the configuration cannot judge
// is its refusal, as a write's is.
func Read(w World, n address.Node, path field.Path, row config.Row) ([]string, error) {
	if c, ok := row.(config.CheckRow); ok {
		unmet, err := Judge(w, n.(address.Item).ID, c.Check.Tests)
		return verdict(unmet), err
	}
	d := row.(config.FieldRow).Decl
	switch {
	case d.Multi == field.Children:
		return children(w, n), nil
	case d.Writability == field.Governed:
		return governedReads[path](w, n), nil
	case d.Writability == field.Derived:
		derive, ok := derivations[path]
		if !ok {
			panic(fmt.Sprintf("verbs: %s is derived and has no derivation", path))
		}
		return derive(w, n)
	case d.Multi == field.Elements:
		var out []string
		for _, e := range elements(w.item(n.(address.Item).ID), events.MustName(string(path))) {
			if !e.deleted {
				out = append(out, e.text)
			}
		}
		return out, nil
	}
	_, in, facts := w.held(n)
	return stored(w.Schema(), facts, in, path, d)
}

// held is the node n names, which exists, as a write and a read of its
// stored fields take it: as its events name it, the ancestry it is judged
// under, which a comment reads through its item's, and its facts.
func (w World) held(n address.Node) (Existing, config.Ancestry, fold.Facts) {
	switch n := n.(type) {
	case address.Item:
		return Existing{At: events.Item{ID: n.ID}, Rendered: n.Rendered}, evaluate.ItemAncestry(w.State(), w.Schema(), n.ID), w.item(n.ID).Facts
	case address.Element:
		return Existing{At: events.Element{Of: n.Of, ID: n.ID}, Rendered: n.Rendered}, evaluate.ItemAncestry(w.State(), w.Schema(), n.Of), w.element(n).Facts
	case address.Root:
		// the root stores no field, so a write to one reaches compile's
		// gate, which refuses what it names as derived
		return Existing{At: events.Root{}, Rendered: "/"}, nil, fold.Facts{}
	case address.Home, address.Principal:
	}
	panic(fmt.Sprintf("verbs: %#v holds no stored field", n))
}

// stored is a stored register's or set's values in facts: prose opened,
// a plain value as its canonical text, a status through the graph the
// item is judged under, a set as its live members. A write reads the same
// function on its subject's facts, so what it compares against is what get
// prints `[LAW:one-source-of-truth]`; a status both read through
// evaluate.StatusIn, get marking a name the graph no longer declares.
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
	case field.Elements, field.Children, field.Principals:
		panic(fmt.Sprintf("verbs: %s holds %s, which no register stores", path, d.Multi))
	}
	if _, column := d.Kind.(field.Column); column {
		eff, err := evaluate.Effective(schema, in)
		if err != nil {
			return nil, err
		}
		return []string{evaluate.StatusIn(eff, facts)}, nil
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
// wrote it, its text, and whether rm removed it.
type element struct {
	key, by, text string
	deleted       bool
}

// line is the element as a list prints it: a removed one is still listed,
// marked, since filtering is a selector's (architecture.md).
func (e element) line() string {
	if e.deleted {
		return fmt.Sprintf("(deleted) [%s] %s", e.by, e.text)
	}
	return fmt.Sprintf("[%s] %s", e.by, e.text)
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
		out = append(out, element{key: c.Segment.String(), by: writer(c.Stamp.By), text: text, deleted: evaluate.Removed(e.Facts)})
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

// children is what is placed at n, as a path names each.
func children(w World, n address.Node) []string {
	var out []string
	for _, c := range below(w, n) {
		out = append(out, name(c))
	}
	return out
}

// below is the nodes placed at n, deleted items included: filtering is a
// selector's (architecture.md). The root holds the declared homes and the
// parentless items of every level that declares none; a home its level's
// parentless items; an item the items below it.
func below(w World, n address.Node) []address.Node {
	var ids []events.ItemID
	var out []address.Node
	switch n := n.(type) {
	case address.Root:
		for l := w.Schema().Levels(); l >= 1; l-- {
			lv, _ := w.Schema().Level(l)
			level, _ := events.ParseLevel(l)
			if lv.Home != "" {
				out = append(out, address.Home{Level: level, Name: lv.Home})
				continue
			}
			ids = append(ids, w.State().Children(fold.Home{Holds: level})...)
		}
	case address.Home:
		ids = w.State().Children(fold.Home{Holds: n.Level})
	case address.Item:
		ids = w.State().Children(fold.Under{Parent: n.ID})
	case address.Element, address.Principal:
		panic(fmt.Sprintf("verbs: %T's table has no children row", n))
	}
	for _, id := range ids {
		out = append(out, w.node(id))
	}
	return out
}

// node is item id as an address names it.
func (w World) node(id events.ItemID) address.Item {
	return address.Item{ID: id, Rendered: w.render(id)}
}

// name is a home or an item as a path names it.
func name(n address.Node) string {
	switch n := n.(type) {
	case address.Home:
		return n.Name
	case address.Item:
		return n.Rendered
	case address.Root, address.Element, address.Principal:
	}
	panic(fmt.Sprintf("verbs: %#v is placed nowhere", n))
}

// tree is the nodes below n, at path, depth first, each a line at the
// children row of the node holding it, valued as a member of a view: the
// tree a whole read of a container shows, where a field read of children
// lists names that pipe. A leaf holds nothing below it.
func tree(w World, n address.Node, path string, children field.Path) ([]line, *refusal) {
	var out []line
	for _, c := range below(w, n) {
		v, err := member(w, c)
		if err != nil {
			return nil, refuse("configuration", "%v", err)
		}
		out = append(out, line{at: path + "/" + string(children), value: v})
		if it, ok := c.(address.Item); ok && w.Leaf(it.ID) {
			continue
		}
		deeper, no := tree(w, c, name(c), children)
		if no != nil {
			return nil, no
		}
		out = append(out, deeper...)
	}
	return out, nil
}

// summary is a node as one line of a view reads it, its parts joined by
// ` · `: removed, when rm removed it or a node above it; a leaf's column, or a node's stage
// and rollup; then its title. A home has no stage row and reads as a home.
func summary(w World, n address.Node) (string, error) {
	it, isItem := n.(address.Item)
	if !isItem {
		return "home", nil
	}
	var parts []string
	facts := w.item(it.ID).Facts
	if w.Gone(it.ID) {
		parts = append(parts, "(deleted)")
	}
	if w.Leaf(it.ID) {
		c, err := w.Column(it.ID)
		if err != nil {
			return "", err
		}
		parts = append(parts, column(c))
	} else {
		for _, derive := range []func(World, address.Node) ([]string, error){stage, rollup} {
			vs, err := derive(w, n)
			if err != nil {
				return "", err
			}
			parts = append(parts, vs...)
		}
	}
	parts = append(parts, texts(facts.Text(events.MustName(string(field.Title))))...)
	return strings.Join(parts, " · "), nil
}

// member is a node as one entry of a view: its name, then its summary.
func member(w World, n address.Node) (string, error) {
	s, err := summary(w, n)
	if err != nil {
		return "", err
	}
	return name(n) + " · " + s, nil
}

// lane is a lane as /lanes lists it: its members, in order.
func lane(w World, l fold.Lane) (string, error) {
	members := make([]string, len(l.Members))
	for i, id := range l.Members {
		v, err := member(w, w.node(id))
		if err != nil {
			return "", err
		}
		members[i] = v
	}
	return strings.Join(members, ", "), nil
}

// why is a reason a head is passed over, as /next names it.
func why(y evaluate.Why) string {
	switch y := y.(type) {
	case evaluate.Undeclared:
		return fmt.Sprintf("its column %s is undeclared", y.Column)
	}
	panic(fmt.Sprintf("verbs: %T is not a Why", y))
}

// derivations is every derived built-in's reading. A link or a review is
// not folded at this slice, so of and blockers are truthfully empty and
// the verdicts truthfully unset; a test holds every Derived row to having
// one here. Stage, rollup and next are evaluate's, read through the view,
// and lanes the fold's; a graph the configuration refuses is their
// refusal.
var derivations = map[field.Path]func(World, address.Node) ([]string, error){
	"members/requests": requestsRow,
	"policy":           policyRow,
	"policy/proposals": proposalsRow,
	"integrity":        integrityRow,
	"level": ofItem(func(_ World, it fold.Item) []string {
		if l, ok := it.Level.(fold.Leveled); ok {
			return []string{strconv.Itoa(l.Level.Int())}
		}
		return nil
	}),
	"parent":       ofItem(func(w World, it fold.Item) []string { return []string{w.container(w.State().Container(it.ID))} }),
	"of":           ofItem(func(World, fold.Item) []string { return nil }),
	"blockers":     ofItem(func(World, fold.Item) []string { return nil }),
	"pr/verdict":   ofItem(func(World, fold.Item) []string { return nil }),
	"plan/verdict": ofItem(func(World, fold.Item) []string { return nil }),
	"by": func(w World, n address.Node) ([]string, error) {
		c, _ := fold.CreateOf(w.element(n.(address.Element)).Origin) // an element a path names is created
		return []string{writer(c.Stamp.By)}, nil
	},
	"stage":     stage,
	"rollup":    rollup,
	"workspace": workspace,
	"lanes": func(w World, _ address.Node) ([]string, error) {
		lanes := w.State().Lanes()
		out := make([]string, len(lanes))
		for i, l := range lanes {
			v, err := lane(w, l)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	},
	"next": func(w World, _ address.Node) ([]string, error) {
		n, err := w.Next()
		if err != nil {
			return nil, err
		}
		var out []string
		switch h := n.Head.(type) {
		case evaluate.Served:
			v, err := member(w, w.node(h.ID))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		case evaluate.NoHead:
		}
		for _, p := range n.Passed {
			out = append(out, fmt.Sprintf("passed %s: %s", w.render(p.Head), why(p.Why)))
		}
		return append(out, fmt.Sprintf("lanes considered: %d", n.Considered)), nil
	},
}

// stage is an item's stage, evaluate's.
func stage(w World, n address.Node) ([]string, error) {
	s, err := w.Stage(n.(address.Item).ID)
	if err != nil {
		return nil, err
	}
	return []string{s.String()}, nil
}

// rollup is a node's count of leaves per column, evaluate's, and nothing
// when it has none.
func rollup(w World, n address.Node) ([]string, error) {
	counts, err := w.Rollup(n.(address.Item).ID)
	if err != nil || len(counts) == 0 {
		return nil, err
	}
	parts := make([]string, len(counts))
	for i, c := range counts {
		parts[i] = fmt.Sprintf("%s %d", column(c.Column), c.N)
	}
	return []string{strings.Join(parts, " · ")}, nil
}

// column is a column as a reader is shown it: a name the graph no longer
// declares marked so (scenarios.md S-6).
func column(c evaluate.Column) string {
	if c.Declared {
		return c.Name
	}
	return c.Name + " (undeclared)"
}

// ofItem is a derivation of an item's that cannot fail, read on the item n
// names: only an item's table has its row.
func ofItem(derive func(World, fold.Item) []string) func(World, address.Node) ([]string, error) {
	return func(w World, n address.Node) ([]string, error) { return derive(w, w.item(n.(address.Item).ID)), nil }
}

// container is where a thing sits, as a path names it: an item, a home, or
// the root, which holds the items of a level with no home.
func (w World) container(c fold.Container) string {
	switch c := c.(type) {
	case fold.Under:
		return w.render(c.Parent)
	case fold.Home:
		if lv, ok := w.Schema().Level(c.Holds.Int()); ok && lv.Home != "" {
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
	return events.Render(w.Schema().Prefix(), id, w.State().Path(id))
}

// item is the folded item an address resolved, which exists.
func (w World) item(id events.ItemID) fold.Item {
	return w.State().Item(id).(fold.Found).Item
}

// element is the folded element an address resolved, which exists.
func (w World) element(n address.Element) fold.Element {
	for _, e := range w.item(n.Of).Elements {
		if e.ID == n.ID {
			return e
		}
	}
	panic(fmt.Sprintf("verbs: %s is no element of its item", n.Rendered))
}

// Unmet is a test that does not hold on an item, the item whose value
// decided it, and that value, so a refusal names what was found and where.
type Unmet struct {
	Test  config.Test
	On    events.ItemID
	Found []string
}

// Judge is the tests of tests that do not hold on item id: the one
// evaluation of a flat test list, which a check's read and the transition
// gate call `[LAW:single-enforcer]`. A self test reads the item; an
// ancestors test the item and each ancestor its one ancestry names, a
// positive test holding when one of them holds it and a negative one when
// all do. A test that reads a status the configuration cannot judge is its
// refusal.
func Judge(w World, id events.ItemID, tests []config.Test) ([]Unmet, error) {
	var unmet []Unmet
	for _, t := range tests {
		scope := []events.ItemID{id}
		if t.Scope == config.Ancestors {
			scope = nil
			for _, k := range evaluate.ItemAncestry(w.State(), w.Schema(), id) {
				if it, ok := k.Item(); ok {
					scope = append(scope, it)
				}
			}
		}
		held, on, found, err := holds(w, t.Atom, scope)
		if err != nil {
			return nil, err
		}
		if !held {
			unmet = append(unmet, Unmet{Test: t, On: on, Found: found})
		}
	}
	return unmet, nil
}

// negative is whether atom forbids a value, and so must hold on every item
// of an ancestors scope rather than on one.
func negative(atom field.Atom) bool {
	switch atom.Op().(type) {
	case field.Absent, field.NotEquals:
		return true
	}
	return false
}

// holds is whether atom holds over scope, whose first item is the one
// judged: on one item for a positive atom, on every item for a negative
// one; and the item whose value decided it, with that value: the first
// item that breaks a negative atom, and otherwise the one judged. An item
// whose place has no field at the atom's path holds nothing there.
func holds(w World, atom field.Atom, scope []events.ItemID) (bool, events.ItemID, []string, error) {
	neg := negative(atom)
	var first []string
	for i, id := range scope {
		it := w.item(id)
		l, placed := it.Level.(fold.Leveled)
		if !placed {
			continue
		}
		place := field.ItemPlace(l.Level)
		var kind field.Kind = field.Text{}
		var found field.Found
		if r, err := w.Schema().Table().Find(place, atom.Path()); err == nil {
			if f, ok := r.(config.FieldRow); ok {
				kind = f.Decl.Kind
			}
			if found, err = Read(w, address.Item{ID: id, Rendered: w.render(id)}, atom.Path(), r); err != nil {
				return false, events.ItemID{}, nil, err
			}
		}
		if i == 0 {
			first = found
		}
		if bool(atom.Holds(kind, found)) != neg {
			return !neg, id, found, nil
		}
	}
	return neg, scope[0], first, nil
}

// verdict is a check's value: holds, or each test that does not, named by
// the check that declared it.
func verdict(unmet []Unmet) []string {
	if len(unmet) == 0 {
		return []string{"holds"}
	}
	var out []string
	for _, u := range unmet {
		out = append(out, fmt.Sprintf("unmet %s (%s)", u.Test.Atom, u.Test.DeclaredIn))
	}
	return out
}
