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
	creates  []creation // by total order
	life     Life
	facts    Facts
	elements []*element // by name
}

// creation is one create of an item and where it places the item.
type creation struct {
	stamp Stamp
	in    Container
}

// element is one element's record.
type element struct {
	name    events.Segment
	creates []listing // by total order
	facts   Facts
}

// listing is one create of an element and the list it shows in.
type listing struct {
	stamp Stamp
	in    events.Name
}

func newItem(id events.ItemID) *item { return &item{id: id, life: Live{}} }

func (r *item) origin() Origin {
	if len(r.creates) == 0 {
		return Uncreated{}
	}
	first := Created{Stamp: r.creates[0].stamp, In: r.creates[0].in}
	if len(r.creates) == 1 {
		return first
	}
	others := make([]Stamp, len(r.creates)-1)
	for i, c := range r.creates[1:] {
		others[i] = c.stamp
	}
	return Collided{First: first, Others: others}
}

func (el *element) origin(of events.ItemID) Origin {
	if len(el.creates) == 0 {
		return Uncreated{}
	}
	first := Created{Stamp: el.creates[0].stamp, In: Under{Parent: of}}
	if len(el.creates) == 1 {
		return first
	}
	others := make([]Stamp, len(el.creates)-1)
	for i, c := range el.creates[1:] {
		others[i] = c.stamp
	}
	return Collided{First: first, Others: others}
}

func (r *item) withCreate(c creation) *item {
	next := *r
	next.creates = byOrder(r.creates, c, func(c creation) events.OrderKey { return c.stamp.Order })
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

// withElement is r with its element name changed by f, made first if r
// has none of that name.
func (r *item) withElement(name events.Segment, f func(*element) *element) *item {
	next := *r
	i, found := slices.BinarySearchFunc(r.elements, name.String(), func(el *element, k string) int { return cmp.Compare(el.name.String(), k) })
	if found {
		next.elements = replacedAt(r.elements, i, f(r.elements[i]))
	} else {
		next.elements = inserted(r.elements, i, f(&element{name: name}))
	}
	return &next
}

func (el *element) withListing(l listing) *element {
	next := *el
	next.creates = byOrder(el.creates, l, func(l listing) events.OrderKey { return l.stamp.Order })
	return &next
}

func (el *element) withFacts(f func(Facts) Facts) *element {
	next := *el
	next.facts = f(el.facts)
	return &next
}

// Facts is a node's registers and sets, by name.
type Facts struct {
	fields []register // by name
	sets   []set      // by name
}

// register is one field's multi-value register.
type register struct {
	name    events.Name
	live    []candidate      // by total order
	retired []events.EventID // every write some write named in Over, sorted
}

// candidate is one live write of a field.
type candidate struct {
	stamp Stamp
	value events.Value
}

// set is one observed-remove set.
type set struct {
	name    events.Name
	members []tally // by member name
}

// tally is one member's adds and the adds removed from it.
type tally struct {
	name    events.Name
	adds    []Stamp          // live adds, by total order
	removed []events.EventID // every add some remove named, sorted
}

// write is f with field written, by the write st of v replacing over. Over
// retires the writes it names in either form, so one write settles a
// field whose kind changed.
func (f Facts) write(st Stamp, field events.Name, v events.Value, over []events.EventID) Facts {
	i, found := slices.BinarySearchFunc(f.fields, field.String(), byName[register])
	r := register{name: field}
	if found {
		r = f.fields[i]
	}
	retired := withIDs(r.retired, over)
	live := make([]candidate, 0, len(r.live)+1)
	for _, c := range r.live {
		if !hasID(retired, c.stamp.Event) {
			live = append(live, c)
		}
	}
	if !hasID(retired, st.Event) {
		live = byOrder(live, candidate{stamp: st, value: v}, func(c candidate) events.OrderKey { return c.stamp.Order })
	}
	r.live, r.retired = live, retired
	next := f
	if found {
		next.fields = replacedAt(f.fields, i, r)
	} else {
		next.fields = inserted(f.fields, i, r)
	}
	return next
}

// add is f with st's add of member to set.
func (f Facts) add(st Stamp, s, member events.Name) Facts {
	return f.tally(s, member, func(t tally) tally {
		if !hasID(t.removed, st.Event) {
			t.adds = byOrder(t.adds, st, func(s Stamp) events.OrderKey { return s.Order })
		}
		return t
	})
}

// remove is f with the adds observed of member removed from set.
func (f Facts) remove(s, member events.Name, observed []events.EventID) Facts {
	return f.tally(s, member, func(t tally) tally {
		t.removed = withIDs(t.removed, observed)
		t.adds = slices.DeleteFunc(slices.Clone(t.adds), func(a Stamp) bool { return hasID(t.removed, a.Event) })
		return t
	})
}

// tally is f with member's tally in set changed by change.
func (f Facts) tally(s, member events.Name, change func(tally) tally) Facts {
	i, found := slices.BinarySearchFunc(f.sets, s.String(), byName[set])
	st := set{name: s}
	if found {
		st = f.sets[i]
	}
	j, had := slices.BinarySearchFunc(st.members, member.String(), byName[tally])
	t := tally{name: member}
	if had {
		t = st.members[j]
	}
	if had {
		st.members = replacedAt(st.members, j, change(t))
	} else {
		st.members = inserted(st.members, j, change(t))
	}
	next := f
	if found {
		next.sets = replacedAt(f.sets, i, st)
	} else {
		next.sets = inserted(f.sets, i, st)
	}
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
func (f Facts) Plain(n events.Name) PlainField {
	r := f.register(n)
	var last candidate
	held := false
	for _, c := range r.live {
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
	i, found := slices.BinarySearchFunc(f.sets, s.String(), byName[set])
	if !found {
		return nil
	}
	var out []Member
	for _, t := range f.sets[i].members {
		if len(t.adds) > 0 {
			out = append(out, Member{Name: t.name, Added: slices.Clone(t.adds)})
		}
	}
	return out
}

func (f Facts) register(n events.Name) register {
	if i, found := slices.BinarySearchFunc(f.fields, n.String(), byName[register]); found {
		return f.fields[i]
	}
	return register{name: n}
}

// prose is the register's live prose texts, each with the writes holding
// it, ordered by each text's first write: texts compare by commitment,
// never by opening them (INV:opaque-envelope).
func (r register) prose() []Candidate {
	var out []Candidate
	for _, c := range r.live {
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
	for _, c := range r.live {
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
func (f Facts) advisories(at Target) []Advisory {
	var out []Advisory
	for _, r := range f.fields {
		prose := r.prose()
		plain := f.Plain(r.name)
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

// named is a record kept in a slice sorted by its name.
type named interface{ key() string }

func (r register) key() string { return r.name.String() }
func (s set) key() string      { return s.name.String() }
func (t tally) key() string    { return t.name.String() }

func byName[T named](t T, k string) int { return cmp.Compare(t.key(), k) }

// byOrder is s, sorted by total order, with v in its place, unless the
// event that made v is in s already: a merge is idempotent on the id.
func byOrder[T any](s []T, v T, order func(T) events.OrderKey) []T {
	i, found := slices.BinarySearchFunc(s, order(v), func(e T, k events.OrderKey) int { return events.Compare(order(e), k) })
	if found {
		return s
	}
	return inserted(s, i, v)
}

func compareID(a, b events.EventID) int { return bytes.Compare(a[:], b[:]) }

func hasID(s []events.EventID, id events.EventID) bool {
	_, found := slices.BinarySearchFunc(s, id, compareID)
	return found
}

// withIDs is the sorted union of s and ids.
func withIDs(s, ids []events.EventID) []events.EventID {
	for _, id := range ids {
		if i, found := slices.BinarySearchFunc(s, id, compareID); !found {
			s = inserted(s, i, id)
		}
	}
	return s
}

// inserted is s with v at i, in a new slice, since s may be shared by an
// older state.
func inserted[T any](s []T, i int, v T) []T {
	out := make([]T, len(s)+1)
	copy(out, s[:i])
	out[i] = v
	copy(out[i+1:], s[i:])
	return out
}

// replacedAt is s with v at i, in a new slice.
func replacedAt[T any](s []T, i int, v T) []T {
	out := make([]T, len(s))
	copy(out, s)
	out[i] = v
	return out
}
