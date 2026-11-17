package fold

import (
	"fmt"
	"slices"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/store"
)

// PolicyInForce is the policy the fold reached: none before the
// enrollment, and after it the version whose approvals arrived last.
//
//sumtype:decl
type PolicyInForce interface{ policyInForce() }

// Unenrolled is a store with no enrollment folded.
type Unenrolled struct{}

// Enrolled is the policy in force, Since the event that activated it.
type Enrolled struct {
	Policy events.Policy
	Since  Stamp
}

func (Unenrolled) policyInForce() {}
func (Enrolled) policyInForce()   {}

// policyState is the policy folded so far: the enrollment that founded
// the store, the version in force, every proposal with its approvals,
// every membership request, and the version each writer last cited. The
// fold walks the total order, so the version in force when an event is
// judged is the one folded before it (§3.20).
type policyState struct {
	// founding is the first enrollment in total order, which every later
	// one yields to, read or not: a replica that cannot read it founds on
	// no other; nil before it.
	founding *events.EventID
	// active is the version in force; nil before the enrollment, and on a
	// replica that cannot read it.
	active   *Enrolled
	pending  table[events.Digest, pendingRec, byDigest]
	requests table[events.PrincipalID, requestRec, byPrincipal]
	cited    table[store.CheckoutID, citedRec, byCheckout]
}

type pendingRec struct {
	digest    events.Digest
	propose   events.Propose
	next      events.Policy
	need      Requirement
	approvals []events.PrincipalID
	by        Stamp
	activated bool
}

// requestRec is a principal's request, admitted once a version lists them.
type requestRec struct {
	principal events.PrincipalID
	stamp     Stamp
	admitted  bool
}

type citedRec struct {
	checkout store.CheckoutID
	version  events.PolicyVersion
}

func (r pendingRec) key() events.Digest      { return r.digest }
func (r requestRec) key() events.PrincipalID { return r.principal }
func (r citedRec) key() store.CheckoutID     { return r.checkout }

// The policy kinds, by the kind each payload names.
var (
	enrollKind = events.Enroll{}.Kind()
)

// version is the version in force, and 1 before the enrollment: the
// version the enrollment founds and cites.
func (p policyState) version() events.PolicyVersion {
	if p.active == nil {
		return 1
	}
	return p.active.Policy.Version()
}

// admits is why an event with header h cannot be judged, from its header
// alone, or nil: verification seeds at the first enrollment and never
// walks before it, a later enrollment yields to it, and a writer cites
// neither a version not yet in force nor one older than it last cited.
func (p policyState) admits(h events.Header) Reason {
	enrolling := h.Kind() == enrollKind
	switch {
	case enrolling && p.founding != nil:
		return EnrollmentYielded{To: *p.founding}
	case !enrolling && p.active == nil:
		return NoPolicyInForce{}
	case h.Policy() > p.version():
		return CitesFutureVersion{Cited: h.Policy(), InForce: p.version()}
	}
	if last, ok := p.cited.get(h.Attribution().Checkout); ok && h.Policy() < last.version {
		return VersionRegressed{Cited: h.Policy(), Previous: last.version}
	}
	return nil
}

// cites is p with the version h cites recorded as its writer's last.
func (p policyState) cites(h events.Header) policyState {
	w := h.Attribution().Checkout
	if last, ok := p.cited.get(w); ok && last.version >= h.Policy() {
		return p
	}
	p.cited = p.cited.set(citedRec{checkout: w, version: h.Policy()})
	return p
}

// judge is the policy rule's verdict on act, made by st, or nil when
// it applies: a proposal needs the version in force as its base, a policy
// NewPolicy accepts that differs from it, a writer granted every path its
// diff writes, and a requirement its approvers can meet; an approval, a
// proposal pending on the version in force and an approver its
// requirement names; a request, a writer no version lists. An enrollment
// certifies itself.
func (s State) judge(st Stamp, act PolicyAct) Reason {
	p := s.policy.active
	who := st.By.Principal
	switch ch := act.(type) {
	case EnrollPolicy:
		return nil
	case RequestMembership:
		if _, listed := p.Policy.Role(who); listed {
			return Inadmissible{Why: "its writer is a member"}
		}
		return nil
	case ProposePolicy:
		if ch.Propose.Base != p.Policy.Version() {
			return Inadmissible{Why: fmt.Sprintf("it changes version %d; version %d is in force", ch.Propose.Base, p.Policy.Version())}
		}
		next, err := p.Policy.Next(ch.Propose.Members, ch.Propose.Threshold)
		if err != nil {
			return Inadmissible{Why: err.Error()}
		}
		diff := Diff(p.Policy, next)
		if len(diff) == 0 {
			return Inadmissible{Why: "it changes nothing"}
		}
		for _, w := range diff {
			if r, refused := s.authorize(p.Policy, who, w).(Refused); refused {
				return Unauthorized{Intent: w, Refused: r}
			}
		}
		if q, ok := Require(p.Policy, next).(OwnerQuorum); ok && q.Need > len(q.Eligible) {
			return Inadmissible{Why: fmt.Sprintf("it needs %d owners' approvals and keeps %d owners to give them; strip one owner at a time", q.Need, len(q.Eligible))}
		}
		return nil
	case ApprovePolicy:
		rec, ok := s.policy.pending.get(ch.Proposal)
		if !ok {
			return UnknownProposal(ch)
		}
		if rec.propose.Base != p.Policy.Version() {
			return Inadmissible{Why: fmt.Sprintf("it approves a change to version %d; version %d is in force", rec.propose.Base, p.Policy.Version())}
		}
		if !s.approves(p.Policy, rec.need, who) {
			return NotAnApprover(ch)
		}
		return nil
	}
	panic(fmt.Sprintf("fold: %T is not a PolicyAct", act))
}

// approves is whether who may approve toward r under prev: an eligible or
// incoming owner for a quorum, a member prev grants every target for one
// holder.
func (s State) approves(prev events.Policy, r Requirement, who events.PrincipalID) bool {
	switch r := r.(type) {
	case OwnerQuorum:
		return slices.Contains(r.Eligible, who) || slices.Contains(r.Incoming, who)
	case OneHolder:
		return s.holdsEvery(prev, who, r.Targets)
	}
	panic(fmt.Sprintf("fold: %T is not a Requirement", r))
}

func (s State) holdsEvery(prev events.Policy, who events.PrincipalID, targets []PathWrite) bool {
	for _, w := range targets {
		if _, ok := s.authorize(prev, who, w).(Granted); !ok {
			return false
		}
	}
	return true
}

// applyPolicy is s with the judged policy act, made by st, folded:
// the enrollment seeds version one; a proposal waits for its approvals;
// an approval that completes its proposal's requirement activates the next
// version, admitting each request of a member it lists; a request waits,
// replacing one its writer made before a removal.
func (s State) applyPolicy(st Stamp, act PolicyAct) State {
	p := s.policy
	switch ch := act.(type) {
	case EnrollPolicy:
		p.founding, p.active = &st.Event, &Enrolled{Policy: ch.Enroll.Policy(st.By.Principal), Since: st}
	case ProposePolicy:
		d := ch.Propose.Digest()
		if _, ok := p.pending.get(d); ok {
			break
		}
		// judge accepted it, so it is a policy
		next, _ := p.active.Policy.Next(ch.Propose.Members, ch.Propose.Threshold)
		p.pending = p.pending.set(pendingRec{digest: d, propose: ch.Propose, next: next, need: Require(p.active.Policy, next), by: st})
	case ApprovePolicy:
		rec, _ := p.pending.get(ch.Proposal)
		if !slices.Contains(rec.approvals, st.By.Principal) {
			rec.approvals = append(slices.Clone(rec.approvals), st.By.Principal)
			p.pending = p.pending.set(rec)
		}
		if s.satisfied(p.active.Policy, rec.need, rec.approvals) {
			p.active = &Enrolled{Policy: rec.next, Since: st}
			rec.activated = true
			p.pending = p.pending.set(rec)
			for _, m := range rec.next.Members() {
				if r, ok := p.requests.get(m.Principal); ok {
					r.admitted = true
					p.requests = p.requests.set(r)
				}
			}
		}
	case RequestMembership:
		if r, ok := p.requests.get(st.By.Principal); !ok || r.admitted {
			p.requests = p.requests.set(requestRec{principal: st.By.Principal, stamp: st})
		}
	default:
		panic(fmt.Sprintf("fold: %T is not a PolicyAct", act))
	}
	s.policy = p
	return s
}

// Requirement is the approvals a change to the policy needs, of the
// version it replaces.
//
//sumtype:decl
type Requirement interface{ requirement() }

// OwnerQuorum is Need approvals among Eligible, and every Incoming owner's
// own: a change to the owner set or the threshold.
type OwnerQuorum struct {
	Eligible []events.PrincipalID
	Incoming []events.PrincipalID
	Need     int
}

// OneHolder is one approver granted every path of Targets: any other
// change.
type OneHolder struct{ Targets []PathWrite }

func (OwnerQuorum) requirement() {}
func (OneHolder) requirement()   {}

// Diff is every path the change from prev to next writes, in principal
// order: a member added, removed or moved to another role, and the
// threshold. It is read from the two policies, never from the command that
// proposed the change (§3.17).
func Diff(prev, next events.Policy) []PathWrite {
	var out []PathWrite
	was, is := prev.MemberMap(), next.MemberMap()
	everyone := append(prev.Members(), next.Members()...)
	slices.SortFunc(everyone, func(a, b events.Membership) int { return events.ComparePrincipals(a.Principal, b.Principal) })
	everyone = slices.CompactFunc(everyone, func(a, b events.Membership) bool { return a.Principal == b.Principal })
	for _, m := range everyone {
		before, had := was[m.Principal]
		after, has := is[m.Principal]
		id := m.Principal.String()
		switch {
		case !had:
			out = append(out, PathWrite{Verb: events.Add, At: OfRoot{}, Field: []string{"members"}})
		case !has:
			out = append(out, PathWrite{Verb: events.Rm, At: OfRoot{}, Field: []string{"members", id}})
		case before != after:
			out = append(out, PathWrite{Verb: events.Set, At: OfRoot{}, Field: []string{"members", id, "role"}})
		}
	}
	if prev.Threshold() != next.Threshold() {
		out = append(out, PathWrite{Verb: events.Set, At: OfRoot{}, Field: []string{"policy", "threshold"}})
	}
	return out
}

// Require is what the change from prev to next needs: the owner quorum
// when the owner set or the threshold differs, by comparing the two
// policies; one holder of every path the diff writes otherwise. The quorum
// is prev's threshold of prev's owners, counted one fewer when the change
// removes or demotes any: the owner being removed is not in the quorum
// that removes them (§3.17), and stripping several at once lowers the
// count by that one alone. The owners the change keeps approve, or prev's
// when it keeps none, so a sole owner hands over by signing.
func Require(prev, next events.Policy) Requirement {
	was, is := prev.Owners(), next.Owners()
	if slices.Equal(was, is) && prev.Threshold() == next.Threshold() {
		return OneHolder{Targets: Diff(prev, next)}
	}
	var kept, incoming []events.PrincipalID
	for _, o := range was {
		if slices.Contains(is, o) {
			kept = append(kept, o)
		}
	}
	for _, o := range is {
		if !slices.Contains(was, o) {
			incoming = append(incoming, o)
		}
	}
	counted, eligible := len(was), kept
	if len(kept) < len(was) {
		counted--
	}
	if len(kept) == 0 {
		eligible = was
	}
	need := counted/2 + 1
	if f, ok := prev.Threshold().(events.Fixed); ok {
		need = max(1, min(int(f.N), counted))
	}
	return OwnerQuorum{Eligible: eligible, Incoming: incoming, Need: need}
}

// Satisfied is whether approvers meet r under prev.
func (s State) satisfied(prev events.Policy, r Requirement, approvers []events.PrincipalID) bool {
	switch r := r.(type) {
	case OwnerQuorum:
		n := 0
		for _, o := range r.Eligible {
			if slices.Contains(approvers, o) {
				n++
			}
		}
		for _, o := range r.Incoming {
			if !slices.Contains(approvers, o) {
				return false
			}
		}
		return n >= r.Need
	case OneHolder:
		for _, a := range approvers {
			if s.holdsEvery(prev, a, r.Targets) {
				return true
			}
		}
		return false
	}
	panic(fmt.Sprintf("fold: %T is not a Requirement", r))
}

// Approves is whether who's approval counts toward the change from the
// policy in force to next: what a writer asks before it approves its own
// proposal.
func (s State) Approves(next events.Policy, who events.PrincipalID) bool {
	in := s.policy.active.Policy
	return s.approves(in, Require(in, next), who)
}

// Policy is the policy in force.
func (s State) Policy() PolicyInForce {
	if s.policy.active == nil {
		return Unenrolled{}
	}
	return *s.policy.active
}

// Cites is the policy version a writer cites: the version in force, and 1
// before the enrollment, the enrollment's own.
func (s State) Cites() events.PolicyVersion { return s.policy.version() }

// Proposal is one proposal folded: what it proposes, what it still needs,
// who approved it, and whether its base is no longer in force.
type Proposal struct {
	Digest    events.Digest
	Base      events.PolicyVersion
	Next      events.Policy
	Need      Requirement
	Approvals []events.PrincipalID
	By        Stamp
	Stale     bool
}

// Proposals is every proposal not yet activated, in total order: those
// waiting on approvals, and those whose base another activation passed.
func (s State) Proposals() []Proposal {
	var out []Proposal
	for _, r := range s.policy.pending.sorted(func(a, b pendingRec) int { return events.Compare(a.by.Order, b.by.Order) }) {
		if !r.activated {
			out = append(out, Proposal{Digest: r.digest, Base: r.propose.Base, Next: r.next, Need: r.need,
				Approvals: slices.Clone(r.approvals), By: r.by, Stale: r.propose.Base != s.policy.version()})
		}
	}
	return out
}

// Request is a principal asking to be a member, and its request.
type Request struct {
	Principal events.PrincipalID
	Stamp     Stamp
}

// Requests is every request no version has admitted, in total order.
func (s State) Requests() []Request {
	var out []Request
	for _, r := range s.policy.requests.sorted(func(a, b requestRec) int { return events.Compare(a.stamp.Order, b.stamp.Order) }) {
		if !r.admitted {
			out = append(out, Request{Principal: r.principal, Stamp: r.stamp})
		}
	}
	return out
}
