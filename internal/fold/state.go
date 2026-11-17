package fold

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

// State is the folded backlog: the item table, the frontier and the
// integrity record. It is immutable; Step returns the next one, and the
// zero State is the fold of nothing.
type State struct {
	items     table[events.ItemID, *item, byItem]
	frontier  events.Frontier
	integrity integrity
}

// integrity is what the fold carried, left unapplied, or could not read:
// each a table keyed by its event, or by where a malformed record was
// read, and read in its order by the lists below.
type integrity struct {
	carried   table[events.EventID, carriedAt, byEvent]
	unapplied table[events.EventID, Unapplied, byEvent]
	malformed table[recordAt, Malformed, byRecord]
}

// recordAt is where a malformed record was read, and what it held. Two
// records of one batch can hold the same bytes, so their place is part of it.
type recordAt struct {
	ref    string
	at     store.Offset
	index  int
	digest [32]byte
}

func (c carriedAt) key() events.EventID { return c.carried.Event }
func (u Unapplied) key() events.EventID { return u.Stamp.Event }
func (m Malformed) key() recordAt {
	return recordAt{ref: m.Ref, at: m.At, index: m.Index, digest: m.Digest}
}

// carriedList is the carried events by total order.
func (in integrity) carriedList() []carriedAt {
	return in.carried.sorted(func(a, b carriedAt) int { return events.Compare(a.order, b.order) })
}

// unappliedList is the unapplied events by total order.
func (in integrity) unappliedList() []Unapplied {
	return in.unapplied.sorted(func(a, b Unapplied) int { return events.Compare(a.Stamp.Order, b.Stamp.Order) })
}

// malformedList is the malformed records by ref, offset, index and digest.
func (in integrity) malformedList() []Malformed { return in.malformed.sorted(compareMalformed) }

func compareMalformed(a, b Malformed) int {
	if c := cmp.Compare(a.Ref, b.Ref); c != 0 {
		return c
	}
	if c := cmp.Compare(a.At, b.At); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Index, b.Index); c != 0 {
		return c
	}
	return slices.Compare(a.Digest[:], b.Digest[:])
}

// carriedAt is a carried event and its place in the total order.
type carriedAt struct {
	order   events.OrderKey
	carried Carried
}

// Lookup is what the state holds of one id.
//
//sumtype:decl
type Lookup interface{ lookup() }

// Found is an id with facts: created, or written about before its create
// arrived.
type Found struct{ Item Item }

// NotFound is an id no folded event names.
type NotFound struct{}

func (Found) lookup()    {}
func (NotFound) lookup() {}

// Item is one item as folded.
type Item struct {
	ID     events.ItemID
	Origin Origin
	Level  LevelOf
	Facts  Facts
	// Elements are the item's created elements, in the total order of the
	// creates that placed each.
	Elements []Element
}

// Origin is who created a node.
//
//sumtype:decl
type Origin interface{ origin() }

// Created is a node's one create: where it put the node, and the segment
// it fixed at mint, which the node renders by (§3.2).
type Created struct {
	Stamp   Stamp
	In      Container
	Segment events.Segment
}

// Collided is a node two or more creates name: First is the first in
// total order, which places it, and Others the rest, in order.
type Collided struct {
	First  Created
	Others []Stamp
}

// Uncreated is a node facts arrived for that no folded event created.
type Uncreated struct{}

func (Created) origin()   {}
func (Collided) origin()  {}
func (Uncreated) origin() {}

// Container is where a created item sits in the tree.
//
//sumtype:decl
type Container interface{ container() }

// Under is a child's place, under its parent.
type Under struct{ Parent events.ItemID }

// Home is the synthetic home of the parentless items at level Holds, which
// tick names unfiled, unassigned or the root by the reader's file: no item
// is parentless `[LAW:types-are-the-program]`.
type Home struct{ Holds events.Level }

func (Under) container() {}
func (Home) container()  {}

// LevelOf is an item's level, derived from its parent chain: a child is
// one level below its parent, so no child stores a level (§3.4).
//
//sumtype:decl
type LevelOf interface{ levelOf() }

// Leveled is an item at Level.
type Leveled struct{ Level events.Level }

// Orphaned is an item whose chain reaches Missing, an id with no create
// before its child's in total order: nobody created it, or a create sorting
// after the child's, which no honest mint makes, since a writer names a
// parent it has folded and Lamport's rule mints past it. Missing may be
// the item itself: a Found item whose Origin is Uncreated, which facts name
// but nothing created, reads Level Orphaned{Missing: its own id}.
type Orphaned struct{ Missing events.ItemID }

// UnderLeaf is an item whose chain reaches a child of Leaf, a level-1 item,
// which holds no children.
type UnderLeaf struct{ Leaf events.ItemID }

func (Leveled) levelOf()   {}
func (Orphaned) levelOf()  {}
func (UnderLeaf) levelOf() {}

// Element is one created element of an item, such as a comment.
type Element struct {
	ID events.ElementID
	// In is the list it shows in, the first create's.
	In     events.Name
	Origin Origin
	Facts  Facts
}

// TextField is a prose field: a multi-value register, whose concurrent
// texts coexist until a write names every one in Over.
//
//sumtype:decl
type TextField interface{ textField() }

// PlainField is a structural field: it settles to its last live value in
// total order, and names every other live value it overruled.
//
//sumtype:decl
type PlainField interface{ plainField() }

// Unset is a field no live write holds.
type Unset struct{}

// Agreed is a prose field whose live writes all hold one text.
type Agreed struct {
	Body events.Envelope
	By   []Stamp
}

// Conflicted is a prose field whose live writes hold two or more texts.
type Conflicted struct{ Candidates []Candidate }

// Settled is a structural field's value, By its last live write in total
// order; Overruled is every other live value, each a contradiction no
// write resolved, and each an advisory.
type Settled struct {
	Value     string
	By        Stamp
	Overruled []Candidate
}

func (Unset) textField()      {}
func (Agreed) textField()     {}
func (Conflicted) textField() {}
func (Unset) plainField()     {}
func (Settled) plainField()   {}

// Candidate is one value of a field and the live writes that hold it, in
// total order.
type Candidate struct {
	Value events.Value
	By    []Stamp
}

// Member is one member of a set and its live adds, in total order.
type Member struct {
	Name  events.Name
	Added []Stamp
}

// Advisory is a contradiction or a gap the fold names on the node it
// concerns, the list /integrity shows.
type Advisory struct {
	At Node
	Is Concern
}

// Concern is what an advisory says.
//
//sumtype:decl
type Concern interface{ concern() }

// Conflict is a field holding values no write resolved: a prose field
// Conflicted, or a structural one that overruled a value.
type Conflict struct{ Field events.Name }

// MixedForms is a field holding live values in both forms; which form is
// right is the reader's file's to say, in tick.
type MixedForms struct{ Field events.Name }

func (Conflict) concern()   {}
func (MixedForms) concern() {}
func (Collided) concern()   {}
func (Orphaned) concern()   {}
func (UnderLeaf) concern()  {}
func (Uncreated) concern()  {}

// Integrity is every event the fold carried, every event it left
// unapplied, and every record it could not read, each in its order.
type Integrity struct {
	Carried   []Carried
	Unapplied []Unapplied
	Malformed []Malformed
}

// Carried is an event the fold kept and never applied, with its writer
// and why.
type Carried struct {
	Event      events.EventID
	Writer     store.CheckoutID
	Principal  events.PrincipalID
	RecordedAt time.Time
	Reason     Reason
}

// Reason is why an event was carried.
//
//sumtype:decl
type Reason interface{ reason() }

// BadSignature is a signature that does not verify against its principal.
type BadSignature struct{}

// UnknownSuite is a signature of a suite this binary does not have; a
// newer binary that has it verifies it when it replays.
type UnknownSuite struct{}

func (BadSignature) reason() {}
func (UnknownSuite) reason() {}

// Unapplied is a verified event whose facts the fold did not apply.
type Unapplied struct {
	Stamp Stamp
	Kind  events.Kind
	About events.About
	Why   UnappliedWhy
}

// UnappliedWhy is why a verified event was not applied.
//
//sumtype:decl
type UnappliedWhy interface{ why() }

// Unread is a payload this binary cannot read (INV:preserve-unknown).
type Unread struct{ Why events.UnreadWhy }

// Erased is a payload a rewrite dropped.
type Erased struct{}

// Uncommitted is a payload its header does not commit to.
type Uncommitted struct{}

func (Unread) why()       {}
func (Erased) why()       {}
func (Uncommitted) why()  {}
func (Misaddressed) why() {}

// Malformed is a record with no readable header, where it was read: its
// ref, its frame's offset, and its place in that frame's batch.
type Malformed struct {
	Ref    string
	At     store.Offset
	Index  int
	Digest [32]byte
	Why    string
}

// Item is what the state holds of id.
func (s State) Item(id events.ItemID) Lookup {
	r, ok := s.items.get(id)
	if !ok {
		return NotFound{}
	}
	return Found{Item: s.itemOf(r)}
}

// Items is every created item, by id.
func (s State) Items() []Item {
	var out []Item
	for _, r := range s.records() {
		if !r.creates.empty() {
			out = append(out, s.itemOf(r))
		}
	}
	return out
}

// Children is the items placed in c, in creation order: a child is under
// its parent only when the parent's create sorts first, as levelOf and
// Path read the chain, so no walk down the tree meets a cycle.
func (s State) Children(c Container) []events.ItemID {
	var in []*item
	s.items.each(func(r *item) {
		if !r.creates.empty() && s.placedIn(r.firstCreate(), c) {
			in = append(in, r)
		}
	})
	slices.SortFunc(in, func(a, b *item) int {
		return events.Compare(a.firstCreate().stamp.Order, b.firstCreate().stamp.Order)
	})
	ids := make([]events.ItemID, len(in))
	for i, r := range in {
		ids[i] = r.id
	}
	return ids
}

// Container is the container id is placed in, as Children lists it: its
// home, or its parent when the parent's create sorts first, as levelOf
// and Path read the chain. An id with no create, or whose parent link is
// no link, is in none. Every step up a chain of Containers goes to an
// earlier create, so a walk up ends, a forged cycle included; it is the
// one reading of a parent link `[LAW:single-enforcer]`.
func (s State) Container(id events.ItemID) Container {
	c, ok := s.placing(id, nil)
	if !ok || !s.placedIn(c, c.in) {
		return nil
	}
	return c.in
}

// Known is every id a folded event made or named, created or not, with
// each namespace's created count, so Mint widens past all of them;
// carried events name none.
func (s State) Known() events.Known {
	var b events.KnownBuilder
	saw := func(a events.About) {
		switch a := a.(type) {
		case events.Item:
			b.Saw(a.ID)
		case events.NewItem:
			b.Saw(a.ID)
		case events.Element:
			b.Saw(a.Of)
			b.Saw(a.ID)
		case events.NewElement:
			b.Saw(a.Of)
			b.Saw(a.ID)
		case events.Root:
		}
	}
	s.items.each(func(r *item) {
		b.Saw(r.id)
		r.creates.each(func(c creation) {
			switch in := c.in.(type) {
			case Under:
				b.Saw(in.Parent)
			case Home:
			}
		})
		if !r.creates.empty() {
			b.Created(r.firstCreate().ns)
		}
		r.elements.each(func(el *element) {
			b.Saw(el.id)
			if !el.creates.empty() {
				b.Created(events.ElementOf{Item: r.id})
			}
		})
	})
	s.integrity.unapplied.each(func(u Unapplied) { saw(u.About) })
	return b.Known()
}

// Frontier is the highest position among the verified events folded.
func (s State) Frontier() events.Frontier { return s.frontier }

// Integrity is the state's integrity record.
func (s State) Integrity() Integrity {
	in := Integrity{Unapplied: s.integrity.unappliedList(), Malformed: s.integrity.malformedList()}
	for _, c := range s.integrity.carriedList() {
		in.Carried = append(in.Carried, c.carried)
	}
	return in
}

// Advisories is every contradiction and gap in the state, by the node it
// concerns.
func (s State) Advisories() []Advisory {
	var out []Advisory
	for _, r := range s.records() {
		at := OnItem{ID: r.id}
		it := s.itemOf(r)
		switch o := it.Origin.(type) {
		case Created:
		case Collided:
			out = append(out, Advisory{At: at, Is: o})
		case Uncreated:
			out = append(out, Advisory{At: at, Is: o})
		}
		if !r.creates.empty() {
			switch l := it.Level.(type) {
			case Leveled:
			case Orphaned:
				out = append(out, Advisory{At: at, Is: l})
			case UnderLeaf:
				out = append(out, Advisory{At: at, Is: l})
			}
		}
		out = append(out, r.facts.advisories(at)...)
		for _, el := range r.sortedElements() {
			eat := OnElement{Of: r.id, ID: el.id}
			switch o := el.origin(r.id).(type) {
			case Created:
			case Collided:
				out = append(out, Advisory{At: eat, Is: o})
			case Uncreated:
				out = append(out, Advisory{At: eat, Is: o})
			}
			out = append(out, el.facts.advisories(eat)...)
		}
	}
	return out
}

// records is every item record, by id: for the readers whose result is in
// that order; the rest read the table with each.
func (s State) records() []*item {
	var rs []*item
	s.items.each(func(r *item) { rs = append(rs, r) })
	slices.SortFunc(rs, func(a, b *item) int { return a.id.Compare(b.id) })
	return rs
}

// itemOf is the record r as an Item, its level derived.
func (s State) itemOf(r *item) Item {
	it := Item{ID: r.id, Origin: r.origin(), Level: s.levelOf(r.id), Facts: r.facts}
	for _, el := range r.sortedElements() {
		if !el.creates.empty() {
			it.Elements = append(it.Elements, Element{ID: el.id, In: el.firstCreate().in, Origin: el.origin(r.id), Facts: el.facts})
		}
	}
	slices.SortStableFunc(it.Elements, func(a, b Element) int {
		return events.Compare(createdOf(a.Origin).Stamp.Order, createdOf(b.Origin).Stamp.Order)
	})
	return it
}

// levelOf is id's level, from its parent chain: an id with no create
// placing it is the chain's missing link.
func (s State) levelOf(id events.ItemID) LevelOf { return s.levelBelow(id, nil) }

// levelBelow is levelOf, reading only a create of id that sorts before
// child, the create of the item below it.
func (s State) levelBelow(id events.ItemID, child *events.OrderKey) LevelOf {
	c, ok := s.placing(id, child)
	if !ok {
		return Orphaned{Missing: id}
	}
	switch in := c.in.(type) {
	case Home:
		return Leveled{Level: in.Holds}
	case Under:
		switch up := s.levelBelow(in.Parent, &c.stamp.Order).(type) {
		case Leveled:
			if below, ok := up.Level.Below(); ok {
				return Leveled{Level: below}
			}
			return UnderLeaf{Leaf: in.Parent}
		case Orphaned:
			return up
		case UnderLeaf:
			return up
		}
	}
	panic(fmt.Sprintf("fold: %s is in no container", id))
}

// placing is the create that places id, read from below: its first create,
// when it sorts before child. Following a link only toward earlier creates
// ends every walk up a chain, and a forged back-link, which could make a
// cycle, reads as a missing parent `[LAW:types-are-the-program]`.
func (s State) placing(id events.ItemID, child *events.OrderKey) (creation, bool) {
	r, ok := s.items.get(id)
	if !ok || r.creates.empty() {
		return creation{}, false
	}
	c := r.firstCreate()
	if child != nil && events.Compare(c.stamp.Order, *child) >= 0 {
		return creation{}, false
	}
	return c, true
}

// placedIn is whether create k places its item in c: a home always, a
// parent only when the parent's own placing create sorts before k.
func (s State) placedIn(k creation, c Container) bool {
	if k.in != c {
		return false
	}
	switch in := c.(type) {
	case Home:
		return true
	case Under:
		_, ok := s.placing(in.Parent, &k.stamp.Order)
		return ok
	}
	panic(fmt.Sprintf("fold: %T is not a Container", c))
}

// Path is the segments id renders by, the top-most first (§3.2): each
// item's own, fixed by its first create, up the chain levelOf walks. A
// chain broken above an item renders from the first item below the break,
// and an item with no create has no segments and renders by its full id.
func (s State) Path(id events.ItemID) []events.Segment { return s.pathBelow(id, nil) }

func (s State) pathBelow(id events.ItemID, child *events.OrderKey) []events.Segment {
	c, ok := s.placing(id, child)
	if !ok {
		return nil
	}
	switch in := c.in.(type) {
	case Home:
		return []events.Segment{c.segment}
	case Under:
		return append(s.pathBelow(in.Parent, &c.stamp.Order), c.segment)
	}
	panic(fmt.Sprintf("fold: %s is in no container", id))
}

// createdOf is the create that places a created node.
func createdOf(o Origin) Created {
	switch o := o.(type) {
	case Created:
		return o
	case Collided:
		return o.First
	case Uncreated:
	}
	panic("fold: an uncreated node has no create")
}
