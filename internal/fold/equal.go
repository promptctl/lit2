package fold

import (
	"slices"

	"github.com/promptctl/lit2/internal/events"
)

// Equal is the state's own equality, shape by shape: two folds agree when
// every item, every fact with its stamps, every applied version, the
// frontier and the integrity record agree. Prose compares by commitment and never by bytes, so
// determinism is tested through this and never through a serialization
// `[LAW:behavior-not-structure]`.
func (s State) Equal(o State) bool {
	if s.frontier != o.frontier || !s.integrity.equal(o.integrity) || !s.workspace.equal(o.workspace) || !s.policy.equal(o.policy) {
		return false
	}
	a, b := s.records(), o.records()
	return slices.EqualFunc(a, b, (*item).equal)
}

func (r *item) equal(o *item) bool {
	return r.id == o.id &&
		slices.EqualFunc(r.sortedCreates(), o.sortedCreates(), func(a, b creation) bool { return a.stamp.equal(b.stamp) && a.in == b.in && a.segment == b.segment }) &&
		r.facts.equal(o.facts) &&
		slices.EqualFunc(r.sortedElements(), o.sortedElements(), (*element).equal)
}

func (el *element) equal(o *element) bool {
	return el.id == o.id &&
		slices.EqualFunc(el.sortedCreates(), o.sortedCreates(), func(a, b listing) bool { return a.stamp.equal(b.stamp) && a.in == b.in && a.segment == b.segment }) &&
		el.facts.equal(o.facts)
}

func (s Stamp) equal(o Stamp) bool {
	return s.Event == o.Event && s.Order == o.Order && s.By == o.By && s.RecordedAt.Equal(o.RecordedAt)
}

func (f Facts) equal(o Facts) bool {
	return slices.EqualFunc(f.registers(), o.registers(), func(a, b register) bool {
		return a.name == b.name && slices.Equal(sortedIDs(a.retired), sortedIDs(b.retired)) &&
			slices.EqualFunc(a.liveWrites(), b.liveWrites(), func(x, y candidate) bool { return x.stamp.equal(y.stamp) && x.says(y) })
	}) && slices.EqualFunc(f.setsByName(), o.setsByName(), func(a, b set) bool {
		return a.name == b.name && slices.EqualFunc(a.members.sorted(byKey[string, tally]), b.members.sorted(byKey[string, tally]), func(x, y tally) bool {
			return x.name == y.name && slices.Equal(sortedIDs(x.removed), sortedIDs(y.removed)) && slices.EqualFunc(x.liveAdds(), y.liveAdds(), Stamp.equal)
		})
	})
}

// says is whether c and o say one thing: one value, and for a status the
// same fields its move carried.
func (c candidate) says(o candidate) bool {
	return valueEqual(c.value, o.value) && SameArrival(c.fields, o.fields)
}

// SameArrival is whether two moves carried the same fields, each in name
// order as a transition holds them.
func SameArrival(a, b []events.TransitionField) bool {
	return slices.EqualFunc(a, b, func(x, y events.TransitionField) bool { return x.Field == y.Field && valueEqual(x.Value, y.Value) })
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
	return slices.EqualFunc(in.carriedList(), o.carriedList(), func(a, b carriedAt) bool {
		return a.order == b.order && a.carried.Event == b.carried.Event && a.carried.Writer == b.carried.Writer &&
			a.carried.Principal == b.carried.Principal && a.carried.RecordedAt.Equal(b.carried.RecordedAt) && ReasonEqual(a.carried.Reason, b.carried.Reason)
	}) && slices.EqualFunc(in.unappliedList(), o.unappliedList(), func(a, b Unapplied) bool {
		return a.Stamp.equal(b.Stamp) && a.Kind == b.Kind && a.About == b.About && whyEqual(a.Why, b.Why)
	}) && slices.Equal(in.malformedList(), o.malformedList())
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

// ReasonEqual is whether two carried reasons say the same: a refusal
// compares by its intent's path and each miss's capability and failures,
// which hold lists, and every other reason by value.
func ReasonEqual(a, b Reason) bool {
	u, ok := a.(Unauthorized)
	if !ok {
		return a == b
	}
	v, ok := b.(Unauthorized)
	return ok && u.Intent.String() == v.Intent.String() && u.Refused.As == v.Refused.As &&
		slices.EqualFunc(u.Refused.Misses, v.Refused.Misses, func(x, y Miss) bool {
			return x.Capability.String() == y.Capability.String() && slices.EqualFunc(x.Failed, y.Failed, failureEqual)
		})
}

func failureEqual(a, b Failure) bool {
	c, ok := a.(ConditionUnmet)
	if !ok {
		return a == b
	}
	d, ok := b.(ConditionUnmet)
	return ok && c.Condition == d.Condition
}

func (p policyState) equal(o policyState) bool {
	if (p.active == nil) != (o.active == nil) {
		return false
	}
	if (p.founding == nil) != (o.founding == nil) || p.founding != nil && *p.founding != *o.founding {
		return false
	}
	if p.active != nil && (!policyEqual(p.active.Policy, o.active.Policy) || !p.active.Since.equal(o.active.Since)) {
		return false
	}
	byDigest := func(a, b pendingRec) int { return slices.Compare(a.digest[:], b.digest[:]) }
	return slices.EqualFunc(p.pending.sorted(byDigest), o.pending.sorted(byDigest), func(x, y pendingRec) bool {
		return x.digest == y.digest && x.by.equal(y.by) && x.activated == y.activated && slices.Equal(sortedPrincipals(x.approvals), sortedPrincipals(y.approvals))
	}) && slices.EqualFunc(p.requests.sorted(byPrincipalKey), o.requests.sorted(byPrincipalKey), func(x, y requestRec) bool {
		return x.principal == y.principal && x.stamp.equal(y.stamp) && x.admitted == y.admitted
	}) && slices.Equal(p.cited.sorted(byCheckoutKey), o.cited.sorted(byCheckoutKey))
}

func policyEqual(a, b events.Policy) bool {
	return a.Version() == b.Version() && a.Owner() == b.Owner() && slices.Equal(a.Members(), b.Members()) &&
		a.Threshold() == b.Threshold()
}

func byPrincipalKey(a, b requestRec) int { return events.ComparePrincipals(a.principal, b.principal) }

func byCheckoutKey(a, b citedRec) int { return slices.Compare(a.checkout[:], b.checkout[:]) }

func sortedPrincipals(ps []events.PrincipalID) []events.PrincipalID {
	return slices.SortedFunc(slices.Values(ps), events.ComparePrincipals)
}
