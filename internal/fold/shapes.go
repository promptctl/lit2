package fold

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/promptctl/lit2/internal/events"
)

// The shapes: every kind of fact merges in exactly one of them, and every
// shape merges in any order and is idempotent on the event id, so a fold
// is a function of the event set, not of its arrival `[LAW:one-source-of-truth]`:
//   - origin: the creates of a node, the first in total order its creator;
//   - life: the first delete in total order;
//   - the register: a multi-value register, whose live writes are those no
//     write names in Over;
//   - the set: an observed-remove set, whose live adds are those no remove
//     names in Observed.
// Every record is immutable: a merge copies what it changes.

// item is one item's record.
type item struct {
	id       events.ItemID
	creates  createSet
	life     Life
	facts    Facts
	elements table[events.ElementID, *element, byElement]
}

// createSet, listSet and writeSet hold a node's creates and a register's
// writes, each keyed by the event that made it.
type (
	createSet = table[events.EventID, creation, byEvent]
	listSet   = table[events.EventID, listing, byEvent]
	writeSet  = table[events.EventID, candidate, byEvent]
)

// creation is one create of an item: where it places the item, and the
// segment it fixed.
type creation struct {
	stamp   Stamp
	in      Container
	segment events.Segment
}

// element is one element's record.
type element struct {
	id      events.ElementID
	creates listSet
	facts   Facts
}

// listing is one create of an element: the list it shows in, and the
// segment it fixed.
type listing struct {
	stamp   Stamp
	in      events.Name
	segment events.Segment
}

func newItem(id events.ItemID) *item { return &item{id: id, life: Live{}} }

func (r *item) key() events.ItemID { return r.id }

func (r *item) origin() Origin {
	return origin(r.sortedCreates(), func(c creation) (Container, events.Segment) { return c.in, c.segment })
}

func (el *element) origin(of events.ItemID) Origin {
	return origin(el.sortedCreates(), func(l listing) (Container, events.Segment) { return Under{Parent: of}, l.segment })
}

// made is a create of a node, or a write: its stamp.
type made interface{ made() Stamp }

func (c creation) made() Stamp  { return c.stamp }
func (l listing) made() Stamp   { return l.stamp }
func (c candidate) made() Stamp { return c.stamp }

func (c creation) key() events.EventID  { return c.stamp.Event }
func (l listing) key() events.EventID   { return l.stamp.Event }
func (c candidate) key() events.EventID { return c.stamp.Event }

// byMade orders records by their stamps' total order.
func byMade[R made](a, b R) int { return events.Compare(a.made().Order, b.made().Order) }

// sortedCreates is r's creates in total order: the first places the item.
func (r *item) sortedCreates() []creation { return r.creates.sorted(byMade[creation]) }

// firstCreate is r's first create in total order, the one that places it. A
// caller checks r.creates.empty() first.
func (r *item) firstCreate() creation {
	c, found := r.creates.least(byMade[creation])
	if !found {
		panic("fold: firstCreate of an item with no create")
	}
	return c
}

// sortedCreates is el's creates in total order: the first places the element.
func (el *element) sortedCreates() []listing { return el.creates.sorted(byMade[listing]) }

// firstCreate is el's first create in total order. A caller checks
// el.creates.empty() first.
func (el *element) firstCreate() listing {
	l, found := el.creates.least(byMade[listing])
	if !found {
		panic("fold: firstCreate of an element with no create")
	}
	return l
}

// origin is the origin of a node made by creates, in total order: the
// first places it, in the container and with the segment placed names.
func origin[C made](creates []C, placed func(C) (Container, events.Segment)) Origin {
	if len(creates) == 0 {
		return Uncreated{}
	}
	in, seg := placed(creates[0])
	first := Created{Stamp: creates[0].made(), In: in, Segment: seg}
	if len(creates) == 1 {
		return first
	}
	others := make([]Stamp, len(creates)-1)
	for i, c := range creates[1:] {
		others[i] = c.made()
	}
	return Collided{First: first, Others: others}
}

func (r *item) withCreate(c creation) *item {
	next := *r
	next.creates = r.creates.set(c)
	return &next
}

func (r *item) withDelete(st Stamp) *item {
	next := *r
	switch l := r.life.(type) {
	case Live:
		next.life = Tombstoned{By: st}
	case Tombstoned:
		if events.Compare(st.Order, l.By.Order) < 0 {
			next.life = Tombstoned{By: st}
		}
	}
	return &next
}

func (r *item) withFacts(f func(Facts) Facts) *item {
	next := *r
	next.facts = f(r.facts)
	return &next
}

// withElement is r with its element id changed by f, made first if r has
// none of that id.
func (r *item) withElement(id events.ElementID, f func(*element) *element) *item {
	next := *r
	el, found := r.elements.get(id)
	if !found {
		el = &element{id: id}
	}
	next.elements = r.elements.set(f(el))
	return &next
}

// sortedElements is r's elements in id order: a table holds no order, so
// a reader that needs one sorts what it reads.
func (r *item) sortedElements() []*element {
	return r.elements.sorted(func(a, b *element) int { return cmp.Compare(a.id.String(), b.id.String()) })
}

func (el *element) withListing(l listing) *element {
	next := *el
	next.creates = el.creates.set(l)
	return &next
}

func (el *element) withFacts(f func(Facts) Facts) *element {
	next := *el
	next.facts = f(el.facts)
	return &next
}

// Facts is a node's registers and sets, by name.
type Facts struct {
	fields table[string, register, byName]
	sets   table[string, set, byName]
}

// register is one field's multi-value register.
type register struct {
	name    events.Name
	live    writeSet // every write; liveWrites says which hold
	retired idSet    // every write some write named in Over
}

// candidate is one write of a field.
type candidate struct {
	stamp Stamp
	value events.Value
}

// set is one observed-remove set.
type set struct {
	name    events.Name
	members table[string, tally, byName] // by member name
}

// tally is one member's adds and the adds removed from it.
type tally struct {
	name    events.Name
	adds    addSet // every add of the member; liveAdds says which hold
	removed idSet  // every add some remove named
}

// idSet is a set of event ids, keyed by id, so each id it takes is one path copy.
type idSet = table[events.EventID, idRec, byEvent]

// addSet is a member's adds, keyed by the event that made each.
type addSet = table[events.EventID, addRec, byEvent]

// idRec is one id of an idSet.
type idRec struct{ id events.EventID }

func (r idRec) key() events.EventID { return r.id }

// addRec is one add of an addSet.
type addRec struct{ stamp Stamp }

func (r addRec) made() Stamp { return r.stamp }

func (r addRec) key() events.EventID { return r.stamp.Event }

// write is f with field written, by the write st of v replacing over. Over
// retires the writes it names in either form, so one write settles a
// field whose kind changed.
func (f Facts) write(st Stamp, field events.Name, v events.Value, over []events.EventID) Facts {
	r := f.register(field)
	r.retired = withIDs(r.retired, over)
	if !hasID(r.retired, st.Event) {
		r.live = r.live.set(candidate{stamp: st, value: v})
	}
	next := f
	next.fields = f.fields.set(r)
	return next
}

// add is f with st's add of member to set.
func (f Facts) add(st Stamp, s, member events.Name) Facts {
	return f.tally(s, member, func(t tally) tally {
		if !hasID(t.removed, st.Event) {
			t.adds = t.adds.set(addRec{stamp: st})
		}
		return t
	})
}

// remove is f with the adds observed of member removed from set. The adds
// stay in the table; liveAdds leaves out the ones named here.
func (f Facts) remove(s, member events.Name, observed []events.EventID) Facts {
	return f.tally(s, member, func(t tally) tally {
		t.removed = withIDs(t.removed, observed)
		return t
	})
}

// tally is f with member's tally in set changed by change.
func (f Facts) tally(s, member events.Name, change func(tally) tally) Facts {
	st := f.setNamed(s)
	t, found := st.members.get(member.String())
	if !found {
		t = tally{name: member}
	}
	st.members = st.members.set(change(t))
	next := f
	next.sets = f.sets.set(st)
	return next
}

// Text is field n as prose: its live prose writes, grouped by text.
func (f Facts) Text(n events.Name) TextField {
	cs := f.register(n).prose()
	switch len(cs) {
	case 0:
		return Unset{}
	case 1:
		return Agreed{Body: cs[0].Value.(events.Prose).Body, By: cs[0].By}
	}
	return Conflicted{Candidates: cs}
}

// Plain is field n as a structural value: its last live plain write in
// total order, and every other live value it overruled.
func (f Facts) Plain(n events.Name) PlainField { return f.register(n).settled() }

// liveOf is the records of t that no removed id names, by total order: the
// one rule every observed-remove or multi-value shape applies. The filter
// runs before the sort, so the sort sees only the survivors.
func liveOf[R interface {
	record[events.EventID]
	made
}](t table[events.EventID, R, byEvent], removed idSet) []R {
	var out []R
	t.each(func(r R) {
		if !hasID(removed, r.key()) {
			out = append(out, r)
		}
	})
	slices.SortFunc(out, byMade[R])
	return out
}

// liveWrites is the register's live writes: those no write names in Over.
func (r register) liveWrites() []candidate { return liveOf(r.live, r.retired) }

// settled is the register as a structural value, as Plain reads it.
func (r register) settled() PlainField {
	var last candidate
	held := false
	for _, c := range r.liveWrites() {
		if _, ok := c.value.(events.Plain); ok {
			last, held = c, true
		}
	}
	if !held {
		return Unset{}
	}
	settled := last.value.(events.Plain).Canonical
	return Settled{Value: settled, By: last.stamp, Overruled: r.plain(settled)}
}

// Members is set's members with a live add, by name.
func (f Facts) Members(s events.Name) []Member {
	st, found := f.sets.get(s.String())
	if !found {
		return nil
	}
	var out []Member
	for _, t := range st.members.sorted(byKey[string, tally]) {
		if adds := t.liveAdds(); len(adds) > 0 {
			out = append(out, Member{Name: t.name, Added: adds})
		}
	}
	return out
}

// liveAdds is the member's live adds, by total order: those no remove names.
func (t tally) liveAdds() []Stamp {
	var out []Stamp
	for _, a := range liveOf(t.adds, t.removed) {
		out = append(out, a.stamp)
	}
	return out
}

// register is field n's register, empty if f has none of that name.
func (f Facts) register(n events.Name) register {
	if r, found := f.fields.get(n.String()); found {
		return r
	}
	return register{name: n}
}

// setNamed is the set named s, empty if f has none of that name.
func (f Facts) setNamed(s events.Name) set {
	if st, found := f.sets.get(s.String()); found {
		return st
	}
	return set{name: s}
}

// registers is f's registers, by name.
func (f Facts) registers() []register { return f.fields.sorted(byKey[string, register]) }

// setsByName is f's sets, by name.
func (f Facts) setsByName() []set { return f.sets.sorted(byKey[string, set]) }

// prose is the register's live prose texts, each with the writes holding
// it, ordered by each text's first write: texts compare by commitment,
// never by opening them (INV:opaque-envelope).
func (r register) prose() []Candidate {
	var out []Candidate
	for _, c := range r.liveWrites() {
		p, ok := c.value.(events.Prose)
		if !ok {
			continue
		}
		i := slices.IndexFunc(out, func(o Candidate) bool { return o.Value.(events.Prose).Body.Commitment() == p.Body.Commitment() })
		if i < 0 {
			out = append(out, Candidate{Value: p})
			i = len(out) - 1
		}
		out[i].By = append(out[i].By, c.stamp)
	}
	return out
}

// plain is the register's live plain values other than settled, each with
// the writes holding it, ordered by each value's first write.
func (r register) plain(settled string) []Candidate {
	var out []Candidate
	for _, c := range r.liveWrites() {
		p, ok := c.value.(events.Plain)
		if !ok || p.Canonical == settled {
			continue
		}
		i := slices.IndexFunc(out, func(o Candidate) bool { return o.Value.(events.Plain).Canonical == p.Canonical })
		if i < 0 {
			out = append(out, Candidate{Value: p})
			i = len(out) - 1
		}
		out[i].By = append(out[i].By, c.stamp)
	}
	return out
}

// advisories is every field of f holding a contradiction, at at.
func (f Facts) advisories(at Node) []Advisory {
	var out []Advisory
	for _, r := range f.registers() {
		prose := r.prose()
		plain := r.settled()
		overruled := false
		if s, ok := plain.(Settled); ok {
			overruled = len(s.Overruled) > 0
		}
		if len(prose) > 1 || overruled {
			out = append(out, Advisory{At: at, Is: Conflict{Field: r.name}})
		}
		if _, unset := plain.(Unset); len(prose) > 0 && !unset {
			out = append(out, Advisory{At: at, Is: MixedForms{Field: r.name}})
		}
	}
	return out
}

func (r register) key() string            { return r.name.String() }
func (s set) key() string                 { return s.name.String() }
func (t tally) key() string               { return t.name.String() }
func (el *element) key() events.ElementID { return el.id }

// byKey orders records by their keys: the order a reader sorts a table into.
func byKey[K cmp.Ordered, R record[K]](a, b R) int { return cmp.Compare(a.key(), b.key()) }

func compareID(a, b events.EventID) int { return bytes.Compare(a[:], b[:]) }

// hasID is whether s holds id.
func hasID(s idSet, id events.EventID) bool {
	_, found := s.get(id)
	return found
}

// withIDs is s with every id in ids added, one path copy each.
func withIDs(s idSet, ids []events.EventID) idSet {
	for _, id := range ids {
		s = s.set(idRec{id: id})
	}
	return s
}

// sortedIDs is s's ids in byte order: the order equality compares them in,
// so two folds agree whatever shape their tables took.
func sortedIDs(s idSet) []events.EventID {
	rs := s.sorted(func(a, b idRec) int { return compareID(a.id, b.id) })
	out := make([]events.EventID, len(rs))
	for i, r := range rs {
		out[i] = r.id
	}
	return out
}
