package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

func TestARestoreIsJudgedAsTheRmItUndoes(t *testing.T) {
	w := soloWorld(t)
	epic := idOf(w.mint(b, w.base, create(events.AtLevel{Level: level(2)}))[0])
	w.mint(a, w.state(), set(epic, fold.Deleted, plain("true")))
	restore := w.mint(b, w.state(), set(epic, fold.Deleted, plain("false")))[0]
	if why, ok := carried(w.state(), restore); !ok {
		t.Error("a contributor restored the epic an owner removed")
	} else if u, unauthorized := why.(fold.Unauthorized); !unauthorized || u.Intent.Verb != events.Rm {
		t.Errorf("the restore is carried for %#v, want its rm unauthorized", why)
	}
}

// TestAProposalNamesItsBaseByContent: two activations made on one base
// before they met are two versions of one number; a proposal written over
// the one total order passes changes neither.
func TestAProposalNamesItsBaseByContent(t *testing.T) {
	w := soloWorld(t)
	in := w.base.Policy().(fold.Enrolled).Policy
	withC := in.MemberMap()
	withC[keyC.Principal()] = contributor
	p := propose(w.base, withC, events.Majority{})
	w.mint(a, w.base, root(p), root(events.Approve{Proposal: p.Digest()}))
	q := propose(w.base, in.MemberMap(), events.Fixed{N: 1})
	w.mint(a2, w.base, root(q), root(events.Approve{Proposal: q.Digest()}))
	qNext, err := in.Next(q.Members, q.Threshold)
	if err != nil {
		t.Fatal(err)
	}
	r := events.Propose{Base: qNext.Digest(), Members: in.MemberMap(), Threshold: events.Majority{}}
	over := w.mint(a2, w.state(), root(r))[0]
	s := w.state()
	if got := s.Policy().(fold.Enrolled).Policy; got.Digest() == qNext.Digest() {
		t.Fatalf("a2's activation sorts first; the test needs a's: %v", got)
	}
	wantCarried(t, s, over, fold.Inadmissible{Why: "it changes a policy other than version 3, the one in force"})
	if _, ok := s.Policy().(fold.Enrolled).Policy.Role(keyC.Principal()); !ok {
		t.Error("the proposal over the other version 3 undid C's admission")
	}
}
