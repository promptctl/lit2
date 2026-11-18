package fold

import (
	"fmt"
	"slices"
	"sync"

	"github.com/promptctl/lit2/internal/events"
)

// checked is an event and its signature's verdict. Only check makes one,
// so no event reaches step unjudged `[LAW:parse-dont-validate]`.
type checked struct {
	event   events.Event
	verdict events.SignatureVerdict
}

// check judges each event's signature, in order, by the Env's suites, on its
// workers. A verdict reads only the event's bytes, so the result does not
// depend on the schedule.
func check(e Env, evs []events.Event) []checked {
	cs := make([]checked, len(evs))
	for i, ev := range evs {
		cs[i].event = ev
	}
	per := (len(cs) + e.workers - 1) / e.workers
	var wg sync.WaitGroup
	for chunk := range slices.Chunk(cs, max(per, 1)) {
		wg.Go(func() {
			for i := range chunk {
				ev := chunk[i].event
				chunk[i].verdict = e.suites.Check(ev.Header().Attribution().Principal, ev.SignedBytes(), ev.Signature())
			}
		})
	}
	wg.Wait()
	return cs
}

// Extended is a fold extended by events: the state after them, and what
// the fold did with each, in order.
type Extended struct {
	State    State
	Outcomes []Outcome
}

// Extend folds evs, in the order given, onto s by the rule Replay applies
// to every event `[LAW:single-enforcer]`: what a writer runs on the events
// it minted before it appends them, so the verdict at the write is the one
// every replica reaches.
func Extend(e Env, s State, evs []events.Event) Extended {
	out := Extended{State: s}
	for _, c := range check(e, evs) {
		var o Outcome
		out.State, o = e.registry.step(out.State, c)
		out.Outcomes = append(out.Outcomes, o)
	}
	return out
}

// Outcome is what the fold did with one event.
//
//sumtype:decl
type Outcome interface{ outcome() }

// Applied is an event whose changes the fold applied, judged Under the
// policy version in force at its position, its Intent allowed By Grant.
type Applied struct {
	Under  events.PolicyVersion
	Intent Intent
	Grant  Grant
}

// CarriedFor is an event the fold carried, Why.
type CarriedFor struct{ Why Reason }

// LeftFor is a verified event the fold left unapplied, Why.
type LeftFor struct{ Why UnappliedWhy }

func (Applied) outcome()    {}
func (CarriedFor) outcome() {}
func (LeftFor) outcome()    {}

// step folds one checked event into s, in this order, the first failure
// its outcome (§3.20): a signature that does not verify is carried; an
// event the policy cannot judge from its header is carried; a verified
// event whose payload no rule applies is carried when no member wrote it,
// and observed and left unapplied otherwise; its intent is judged under
// the policy in force, and a refused one carried; the rest are observed
// and applied. A carried event moves neither the frontier nor the names.
func (r Registry) step(s State, c checked) (State, Outcome) {
	e, h := c.event, c.event.Header()
	switch c.verdict {
	case events.Valid:
	case events.Invalid:
		return s.carry(e, BadSignature{}), CarriedFor{Why: BadSignature{}}
	case events.UnknownSuite:
		return s.carry(e, UnknownSuite{}), CarriedFor{Why: UnknownSuite{}}
	default:
		panic(fmt.Sprintf("fold: %s is not a verdict Check gives", c.verdict))
	}
	if why := s.policy.admits(e.ID(), h); why != nil {
		return s.carry(e, why), CarriedFor{Why: why}
	}
	st := stampOf(e)
	changes, intent, why := r.changes(st, h, e.Body())
	if why != nil {
		// with no policy in force only the founding is judged, and its
		// writer is the founder
		if p := s.policy.active; p != nil {
			if _, listed := p.Policy.Role(st.By.Principal); !listed {
				return s.carry(e, NotAMemberWrote{}), CarriedFor{Why: NotAMemberWrote{}}
			}
		}
		s.frontier = s.frontier.Observe(h)
		s.policy = s.policy.cites(h)
		return s.leave(Unapplied{Stamp: st, Kind: h.Kind(), About: h.About(), Why: why}), LeftFor{Why: why}
	}
	under := s.policy.version()
	grant, refused := s.ruling(st, intent)
	if refused != nil {
		return s.carry(e, refused), CarriedFor{Why: refused}
	}
	s.frontier = s.frontier.Observe(h)
	s.policy = s.policy.cites(h)
	for _, ch := range changes {
		s = s.apply(st, ch)
	}
	switch in := intent.(type) {
	case PathWrite:
	case Governed:
		s = s.applyPolicy(st, in.Act)
	}
	return s, Applied{Under: under, Intent: intent, Grant: grant}
}

// ruling is the verdict of the policy in force on an event's intent: a
// path write by the writer's role, a policy act by the policy rule. An
// enrollment is judged by the policy rule alone, since no policy is in
// force before it.
func (s State) ruling(st Stamp, in Intent) (Grant, Reason) {
	switch in := in.(type) {
	case PathWrite:
		switch a := s.authorize(s.policy.active.Policy, st.By.Principal, in).(type) {
		case Granted:
			return a.By, nil
		case Refused:
			return nil, Unauthorized{Intent: in, Refused: a}
		}
	case Governed:
		if why := s.judge(st, in.Act); why != nil {
			return nil, why
		}
		return ByPolicyRule{}, nil
	}
	panic(fmt.Sprintf("fold: %T is not an Intent", in))
}

// changes is what an event with header h and body b changes and does, or
// why it changes nothing.
func (r Registry) changes(st Stamp, h events.Header, b events.Body) ([]Change, Intent, UnappliedWhy) {
	var p events.Payload
	switch b := b.(type) {
	case events.Readable:
		p = b.Payload
	case events.Unread:
		return nil, nil, Unread{Why: b.Why}
	case events.Erased:
		return nil, nil, Erased{}
	case events.Uncommitted:
		return nil, nil, Uncommitted{}
	}
	k, ok := r.kinds[h.Kind()]
	if !ok {
		// decoded by another table than this registry's
		return nil, nil, Unread{Why: events.UnknownKind{}}
	}
	at, ok := k.aim.target(h.About())
	if !ok {
		return nil, nil, k.aim.misaddressed()
	}
	applies, intent := k.apply(Fact[events.Payload]{Payload: p, Stamp: st, At: at})
	switch a := applies.(type) {
	case Changes:
		return a.List, intent, nil
	case Misaddressed:
		return nil, nil, a
	}
	panic(fmt.Sprintf("fold: %s's rule applies nothing", h.Kind()))
}

// containerOf is where a create placed at p puts its item: the home of
// its level, or under its parent.
func containerOf(p events.Placement) Container {
	switch p := p.(type) {
	case events.AtLevel:
		return Home{Holds: p.Level}
	case events.BelowParent:
		return Under{Parent: p.Parent}
	}
	panic(fmt.Sprintf("fold: %T is not a Placement", p))
}

// apply merges one change, made by st, into s.
func (s State) apply(st Stamp, c Change) State {
	switch c := c.(type) {
	case CreateItem:
		return s.update(c.New.ID, func(r *item) *item {
			return r.withCreate(creation{stamp: st, in: containerOf(c.Place), ns: events.NamespaceOf(c.Place), segment: c.New.Segment()})
		})
	case CreateElement:
		return s.update(c.New.Of, func(r *item) *item {
			return r.withElement(c.New.ID, func(el *element) *element {
				return el.withListing(listing{stamp: st, in: c.In, segment: c.New.Segment()})
			})
		})
	case Write:
		return s.facts(c.At, func(f Facts) Facts { return f.write(c.Field, candidate{stamp: st, value: c.Value}, c.Over) })
	case Move:
		return s.facts(c.At, func(f Facts) Facts {
			return f.write(Status, candidate{stamp: st, value: events.Plain{Canonical: c.To.String()}, fields: c.Fields}, c.Over)
		})
	case Add:
		return s.facts(c.At, func(f Facts) Facts { return f.add(st, c.Set, c.Member) })
	case Remove:
		return s.facts(c.At, func(f Facts) Facts { return f.remove(c.Set, c.Member, c.Observed) })
	case Apply:
		s.workspace = s.workspace.with(st, c)
		return s
	}
	panic(fmt.Sprintf("fold: %T is not a Change", c))
}

// facts is s with at's facts changed by f.
func (s State) facts(at Node, f func(Facts) Facts) State {
	switch at := at.(type) {
	case OnItem:
		return s.update(at.ID, func(r *item) *item { return r.withFacts(f) })
	case OnElement:
		return s.update(at.Of, func(r *item) *item {
			return r.withElement(at.ID, func(el *element) *element { return el.withFacts(f) })
		})
	}
	panic(fmt.Sprintf("fold: %T is not a Target", at))
}

// update is s with id's record changed by f; an id with no record yet is
// one with no facts, uncreated.
func (s State) update(id events.ItemID, f func(*item) *item) State {
	r, ok := s.items.get(id)
	if !ok {
		r = newItem(id)
	}
	s.items = s.items.set(f(r))
	return s
}

func (s State) carry(e events.Event, why Reason) State {
	h := e.Header()
	c := carriedAt{order: e.Order(), carried: Carried{Event: e.ID(), Writer: h.Attribution().Checkout, Principal: h.Attribution().Principal, RecordedAt: h.RecordedAt(), Reason: why}}
	s.integrity.carried = s.integrity.carried.set(c)
	return s
}

func (s State) leave(u Unapplied) State {
	s.integrity.unapplied = s.integrity.unapplied.set(u)
	return s
}

func (s State) malformed(m Malformed) State {
	s.integrity.malformed = s.integrity.malformed.set(m)
	return s
}
