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

// recordAt is where a malformed record was read, and what it held.
type recordAt struct {
	ref    string
	at     store.Offset
	digest [32]byte
}

func (c carriedAt) key() events.EventID { return c.carried.Event }
func (u Unapplied) key() events.EventID { return u.Stamp.Event }
func (m Malformed) key() recordAt       { return recordAt{ref: m.Ref, at: m.At, digest: m.Digest} }

// carriedList is the carried events by total order.
func (in integrity) carriedList() []carriedAt {
	return in.carried.sorted(func(a, b carriedAt) int { return events.Compare(a.order, b.order) })
}

// unappliedList is the unapplied events by total order.
func (in integrity) unappliedList() []Unapplied {
	return in.unapplied.sorted(func(a, b Unapplied) int { return events.Compare(a.Stamp.Order, b.Stamp.Order) })
}

// malformedList is the malformed records by ref, offset and digest.
func (in integrity) malformedList() []Malformed { return in.malformed.sorted(compareMalformed) }

func compareMalformed(a, b Malformed) int {
	if c := cmp.Compare(a.Ref, b.Ref); c != 0 {
		return c
	}
	if c := cmp.Compare(a.At, b.At); c != 0 {
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
	Life   Life
	Facts  Facts
	// Elements are the item's created elements, in creation order.
	Elements []Element
}

// Origin is who created a node.
//
//sumtype:decl
type Origin interface{ origin() }

// Created is a node's one create.
type Created struct {
	Stamp Stamp
	In    Container
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

// Orphaned is an item whose chain reaches Missing, an id nobody created.
// Missing may be the item itself: a Found item whose Origin is Uncreated,
// which facts name but nothing created, reads Level Orphaned{Missing: its
// own id}.
type Orphaned struct{ Missing events.ItemID }

// UnderLeaf is an item whose chain reaches a child of Leaf, a level-1 item,
// which holds no children.
type UnderLeaf struct{ Leaf events.ItemID }

func (Leveled) levelOf()   {}
func (Orphaned) levelOf()  {}
func (UnderLeaf) levelOf() {}

// Life is whether an item is deleted.
//
//sumtype:decl
type Life interface{ life() }

// Live is an item no delete names.
type Live struct{}

// Tombstoned is a deleted item, By its first delete in total order; every
// fact stays.
type Tombstoned struct{ By Stamp }

func (Live) life()       {}
func (Tombstoned) life() {}

// Element is one created element of an item, such as a comment.
type Element struct {
	Name events.Segment
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
	At Target
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
// newer binary that has it verifies it when it replays, under its own Tag.
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

// Malformed is a record with no readable header, where it was read.
type Malformed struct {
	Ref    string
	At     store.Offset
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
		if len(r.creates) > 0 {
			out = append(out, s.itemOf(r))
		}
	}
	return out
}

// Children is the items created in c, in creation order.
func (s State) Children(c Container) []events.ItemID {
	var in []*item
	s.items.each(func(r *item) {
		if len(r.creates) > 0 && r.creates[0].in == c {
			in = append(in, r)
		}
	})
	slices.SortFunc(in, func(a, b *item) int { return events.Compare(a.creates[0].stamp.Order, b.creates[0].stamp.Order) })
	ids := make([]events.ItemID, len(in))
	for i, r := range in {
		ids[i] = r.id
	}
	return ids
}

// Names is every name a folded event made or named, created or not, so
// Mint draws none of them again; carried events name none.
func (s State) Names() events.Names {
	var b events.NamesBuilder
	// an id names every ancestor on its path, created here or not, so a
	// clone that folded a child but not its parent never mints the parent's
	// id afresh
	var path func(id events.ItemID)
	path = func(id events.ItemID) {
		segs := id.Segments()
		b.Add(events.Children{Parent: id.Parent()}, segs[len(segs)-1])
		switch p := id.Parent().(type) {
		case events.Under:
			path(p.ID)
		case events.Top:
		}
	}
	add := func(a events.About) {
		switch a := a.(type) {
		case events.Item:
			path(a.ID)
		case events.Element:
			path(a.Of)
			b.Add(events.Elements{Of: a.Of}, a.Name)
		case events.Root:
		}
	}
	s.items.each(func(r *item) {
		add(events.Item{ID: r.id})
		for _, el := range r.elements {
			add(events.Element{Of: r.id, Name: el.name})
		}
	})
	s.integrity.unapplied.each(func(u Unapplied) { add(u.About) })
	return b.Names()
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
		if len(r.creates) > 0 {
			switch l := it.Level.(type) {
			case Leveled:
			case Orphaned:
				out = append(out, Advisory{At: at, Is: l})
			case UnderLeaf:
				out = append(out, Advisory{At: at, Is: l})
			}
		}
		out = append(out, r.facts.advisories(at)...)
		for _, el := range r.elements {
			eat := OnElement{Of: r.id, Name: el.name}
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
	slices.SortFunc(rs, func(a, b *item) int { return cmp.Compare(a.id.String(), b.id.String()) })
	return rs
}

// itemOf is the record r as an Item, its level derived.
func (s State) itemOf(r *item) Item {
	it := Item{ID: r.id, Origin: r.origin(), Level: s.levelOf(r.id), Life: r.life, Facts: r.facts}
	for _, el := range r.elements {
		if len(el.creates) > 0 {
			it.Elements = append(it.Elements, Element{Name: el.name, In: el.creates[0].in, Origin: el.origin(r.id), Facts: el.facts})
		}
	}
	slices.SortStableFunc(it.Elements, func(a, b Element) int {
		return events.Compare(createdOf(a.Origin).Stamp.Order, createdOf(b.Origin).Stamp.Order)
	})
	return it
}

// levelOf is id's level, from its parent chain: an id with no create is
// the chain's missing link.
func (s State) levelOf(id events.ItemID) LevelOf {
	r, ok := s.items.get(id)
	if !ok || len(r.creates) == 0 {
		return Orphaned{Missing: id}
	}
	switch c := r.creates[0].in.(type) {
	case Home:
		return Leveled{Level: c.Holds}
	case Under:
		switch up := s.levelOf(c.Parent).(type) {
		case Leveled:
			if below, ok := up.Level.Below(); ok {
				return Leveled{Level: below}
			}
			return UnderLeaf{Leaf: c.Parent}
		case Orphaned:
			return up
		case UnderLeaf:
			return up
		}
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
