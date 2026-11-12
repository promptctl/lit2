package fold

import (
	"slices"

	"github.com/promptctl/lit2/internal/events"
)

// Equal is the state's own equality, shape by shape: two folds agree when
// every item, every fact with its stamps, the frontier and the integrity
// record agree. Prose compares by commitment and never by bytes, so
// determinism is tested through this and never through a serialization
// `[LAW:behavior-not-structure]`.
func (s State) Equal(o State) bool {
	if s.frontier != o.frontier || !s.integrity.equal(o.integrity) {
		return false
	}
	a, b := s.records(), o.records()
	return slices.EqualFunc(a, b, (*item).equal)
}

func (r *item) equal(o *item) bool {
	return r.id == o.id &&
		slices.EqualFunc(r.creates, o.creates, func(a, b creation) bool { return a.stamp.equal(b.stamp) && a.in == b.in }) &&
		lifeEqual(r.life, o.life) && r.facts.equal(o.facts) &&
		slices.EqualFunc(r.elements, o.elements, (*element).equal)
}

func (el *element) equal(o *element) bool {
	return el.name == o.name &&
		slices.EqualFunc(el.creates, o.creates, func(a, b listing) bool { return a.stamp.equal(b.stamp) && a.in == b.in }) &&
		el.facts.equal(o.facts)
}

func (s Stamp) equal(o Stamp) bool {
	return s.Event == o.Event && s.Order == o.Order && s.By == o.By && s.RecordedAt.Equal(o.RecordedAt)
}

func lifeEqual(a, b Life) bool {
	switch a := a.(type) {
	case Live:
		_, ok := b.(Live)
		return ok
	case Tombstoned:
		t, ok := b.(Tombstoned)
		return ok && a.By.equal(t.By)
	}
	return false
}

func (f Facts) equal(o Facts) bool {
	return slices.EqualFunc(f.fields, o.fields, func(a, b register) bool {
		return a.name == b.name && slices.Equal(a.retired, b.retired) &&
			slices.EqualFunc(a.live, b.live, func(x, y candidate) bool { return x.stamp.equal(y.stamp) && valueEqual(x.value, y.value) })
	}) && slices.EqualFunc(f.sets, o.sets, func(a, b set) bool {
		return a.name == b.name && slices.EqualFunc(a.members, b.members, func(x, y tally) bool {
			return x.name == y.name && slices.Equal(x.removed, y.removed) && slices.EqualFunc(x.adds, y.adds, Stamp.equal)
		})
	})
}

func valueEqual(a, b events.Value) bool {
	switch a := a.(type) {
	case events.Prose:
		p, ok := b.(events.Prose)
		return ok && a.Body.Commitment() == p.Body.Commitment()
	case events.Plain:
		p, ok := b.(events.Plain)
		return ok && a.Canonical == p.Canonical
	}
	return false
}

func (in integrity) equal(o integrity) bool {
	return slices.EqualFunc(in.carried, o.carried, func(a, b carriedAt) bool {
		return a.order == b.order && a.carried.Event == b.carried.Event && a.carried.Writer == b.carried.Writer &&
			a.carried.Principal == b.carried.Principal && a.carried.RecordedAt.Equal(b.carried.RecordedAt) && a.carried.Reason == b.carried.Reason
	}) && slices.EqualFunc(in.unapplied, o.unapplied, func(a, b Unapplied) bool {
		return a.Stamp.equal(b.Stamp) && a.Kind == b.Kind && a.About == b.About && whyEqual(a.Why, b.Why)
	}) && slices.Equal(in.malformed, o.malformed)
}

func whyEqual(a, b UnappliedWhy) bool {
	u, ok := a.(Unread)
	if !ok {
		return a == b
	}
	v, ok := b.(Unread)
	if !ok {
		return false
	}
	switch w := u.Why.(type) {
	case events.Undecodable:
		x, ok := v.Why.(events.Undecodable)
		return ok && w.Err.Error() == x.Err.Error()
	case events.UnknownKind, events.NewerSchema:
		return u.Why == v.Why
	}
	return false
}
