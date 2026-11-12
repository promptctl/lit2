package fold

import (
	"cmp"
	"fmt"
	"slices"
	"sync"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

// Located is an event and where the walk read it: the stream's ref and the
// offset of its frame in the stream's log.
type Located struct {
	Event events.Event
	Ref   store.Ref
	At    store.Position
}

// Checked is an event and its signature's verdict. Only Check makes one,
// so no event reaches Step unjudged `[LAW:parse-dont-validate]`.
type Checked struct {
	event   events.Event
	verdict events.SignatureVerdict
}

// Event is the event checked.
func (c Checked) Event() events.Event { return c.event }

// Verdict is its signature's verdict.
func (c Checked) Verdict() events.SignatureVerdict { return c.verdict }

// Check judges each event's signature, in order: Valid when t trusts it,
// below a mark of its stream under the same log and not excepted, and
// otherwise by e.Suites, on e.Workers goroutines. A verdict reads only the
// event's bytes, so the result does not depend on the schedule.
func Check(e Env, evs []Located, t Trust) []Checked {
	cs, _ := check(e, evs, t)
	return cs
}

// check is Check and how many verdicts it trusted.
func check(e Env, evs []Located, t Trust) ([]Checked, int) {
	if e.Workers < 1 {
		// [LAW:no-silent-failure] the root sets GOMAXPROCS; none is a caller's bug
		panic(fmt.Sprintf("fold: an Env with %d workers", e.Workers))
	}
	marks := map[store.Ref]store.Position{}
	for _, m := range t.Marks {
		marks[m.Ref] = m.Upto
	}
	excepted := map[events.EventID]bool{}
	for _, id := range t.Exceptions {
		excepted[id] = true
	}
	cs := make([]Checked, len(evs))
	var verify []int
	for i, l := range evs {
		cs[i].event = l.Event
		m, marked := marks[l.Ref]
		if marked && m.Log == l.At.Log && l.At.At < m.At && !excepted[l.Event.ID()] {
			cs[i].verdict = events.Valid
			continue
		}
		verify = append(verify, i)
	}
	var wg sync.WaitGroup
	per := (len(verify) + e.Workers - 1) / e.Workers
	for chunk := range slices.Chunk(verify, max(per, 1)) {
		wg.Go(func() {
			for _, i := range chunk {
				ev := cs[i].event
				cs[i].verdict = e.Suites.Check(ev.Header().Attribution().Principal, ev.SignedBytes(), ev.Signature())
			}
		})
	}
	wg.Wait()
	return cs, len(evs) - len(verify)
}

// Step folds one checked event into s: the one walk, which Genesis, Extend
// and every later reader of events share `[LAW:single-enforcer]`. An event
// whose signature does not verify is carried and never applied, and moves
// neither the frontier nor the names; a verified one is observed, then
// applied by its kind's rule, or left unapplied with why.
func Step(r Registry, s State, c Checked) State {
	next, _ := r.step(s, c)
	return next
}

// outcome is what Step did with one event.
//
//sumtype:decl
type outcome interface{ outcome() }

type applied struct{}
type carriedFor struct{ why Reason }
type unappliedFor struct{ why UnappliedWhy }

func (applied) outcome()      {}
func (carriedFor) outcome()   {}
func (unappliedFor) outcome() {}

func (r Registry) step(s State, c Checked) (State, outcome) {
	e, h := c.event, c.event.Header()
	switch c.verdict {
	case events.Valid:
	case events.Invalid:
		return s.carry(e, BadSignature{}), carriedFor{why: BadSignature{}}
	case events.UnknownSuite:
		return s.carry(e, UnknownSuite{}), carriedFor{why: UnknownSuite{}}
	default:
		panic(fmt.Sprintf("fold: %s is not a verdict Check gives", c.verdict))
	}
	s.frontier = s.frontier.Observe(h)
	st := stampOf(e)
	changes, why := r.changes(st, h, e.Body())
	if why != nil {
		return s.leave(Unapplied{Stamp: st, Kind: h.Kind(), About: h.About(), Why: why}), unappliedFor{why: why}
	}
	for _, ch := range changes {
		s = s.apply(st, ch)
	}
	return s, applied{}
}

// changes is what an event with header h and body b changes, or why it
// changes nothing.
func (r Registry) changes(st Stamp, h events.Header, b events.Body) ([]Change, UnappliedWhy) {
	var p events.Payload
	switch b := b.(type) {
	case events.Readable:
		p = b.Payload
	case events.Unread:
		return nil, Unread{Why: b.Why}
	case events.Erased:
		return nil, Erased{}
	case events.Uncommitted:
		return nil, Uncommitted{}
	}
	k, ok := r.kinds[h.Kind()]
	if !ok {
		// decoded by another table than this registry's
		return nil, Unread{Why: events.UnknownKind{}}
	}
	at, ok := k.aim.target(h.About())
	if !ok {
		return nil, k.aim.misaddressed()
	}
	switch a := k.apply(st, at, p).(type) {
	case Changes:
		for _, ch := range a.List {
			if c, ok := ch.(CreateItem); ok {
				if _, placed := containerOf(c.ID, c.Place); !placed {
					return nil, misplaced(c.ID)
				}
			}
		}
		return a.List, nil
	case Misaddressed:
		return nil, a
	}
	panic(fmt.Sprintf("fold: %s's rule applies nothing", h.Kind()))
}

// containerOf is where a create of id placed by p puts it: a top-level id
// states its level and a child states none, since its parent fixes it, so
// the two agree or the create is misaddressed.
func containerOf(id events.ItemID, p events.Placement) (Container, bool) {
	switch parent := id.Parent().(type) {
	case events.Top:
		at, ok := p.(events.AtLevel)
		return Home{Holds: at.Level}, ok
	case events.Under:
		_, ok := p.(events.BelowParent)
		return Under{Parent: parent.ID}, ok
	}
	panic(fmt.Sprintf("fold: %T is not a Parent", id.Parent()))
}

func misplaced(id events.ItemID) Misaddressed {
	switch id.Parent().(type) {
	case events.Top:
		return Misaddressed{Want: "a top-level id's create states its level"}
	case events.Under:
		return Misaddressed{Want: "a child's create states no level: its parent's fixes it"}
	}
	panic(fmt.Sprintf("fold: %T is not a Parent", id.Parent()))
}

// apply merges one change, made by st, into s.
func (s State) apply(st Stamp, c Change) State {
	switch c := c.(type) {
	case CreateItem:
		in, placed := containerOf(c.ID, c.Place)
		if !placed {
			panic(fmt.Sprintf("fold: a create of %s the step did not place", c.ID))
		}
		return s.update(c.ID, func(r *item) *item { return r.withCreate(creation{stamp: st, in: in}) })
	case CreateElement:
		return s.update(c.Of, func(r *item) *item {
			return r.withElement(c.Name, func(el *element) *element { return el.withListing(listing{stamp: st, in: c.In}) })
		})
	case Write:
		return s.facts(c.At, func(f Facts) Facts { return f.write(st, c.Field, c.Value, c.Over) })
	case Add:
		return s.facts(c.At, func(f Facts) Facts { return f.add(st, c.Set, c.Member) })
	case Remove:
		return s.facts(c.At, func(f Facts) Facts { return f.remove(c.Set, c.Member, c.Observed) })
	case Tombstone:
		return s.update(c.Item, func(r *item) *item { return r.withDelete(st) })
	}
	panic(fmt.Sprintf("fold: %T is not a Change", c))
}

// facts is s with at's facts changed by f.
func (s State) facts(at Target, f func(Facts) Facts) State {
	switch at := at.(type) {
	case OnItem:
		return s.update(at.ID, func(r *item) *item { return r.withFacts(f) })
	case OnElement:
		return s.update(at.Of, func(r *item) *item {
			return r.withElement(at.Name, func(el *element) *element { return el.withFacts(f) })
		})
	}
	panic(fmt.Sprintf("fold: %T is not a Target", at))
}

// update is s with id's record changed by f; an id with no record yet is
// one with no facts, uncreated.
func (s State) update(id events.ItemID, f func(*item) *item) State {
	r := s.items.get(id)
	if r == nil {
		r = newItem(id)
	}
	s.items = s.items.set(f(r))
	return s
}

func (s State) carry(e events.Event, why Reason) State {
	h := e.Header()
	c := carriedAt{order: e.Order(), carried: Carried{Event: e.ID(), Writer: h.Attribution().Checkout, Principal: h.Attribution().Principal, RecordedAt: h.RecordedAt(), Reason: why}}
	s.integrity.carried = byOrder(s.integrity.carried, c, func(c carriedAt) events.OrderKey { return c.order })
	return s
}

func (s State) leave(u Unapplied) State {
	s.integrity.unapplied = byOrder(s.integrity.unapplied, u, func(u Unapplied) events.OrderKey { return u.Stamp.Order })
	return s
}

func (s State) malformed(m Malformed) State {
	i, found := slices.BinarySearchFunc(s.integrity.malformed, m, compareMalformed)
	if !found {
		s.integrity.malformed = inserted(s.integrity.malformed, i, m)
	}
	return s
}

func compareMalformed(a, b Malformed) int {
	if c := cmp.Compare(a.Ref, b.Ref); c != 0 {
		return c
	}
	if c := cmp.Compare(a.At, b.At); c != 0 {
		return c
	}
	return slices.Compare(a.Digest[:], b.Digest[:])
}
