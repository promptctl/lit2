package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/fold"
)

// unreadEnroll is an enrollment written by a newer release, whose payload
// this release cannot read.
type unreadEnroll struct{}

func (unreadEnroll) Kind() events.Kind            { return events.Enroll{}.Kind() }
func (unreadEnroll) Schema() events.SchemaVersion { return 1 }
func (unreadEnroll) AppendTo(w *wire.Writer)      { w.Bytes(1, []byte("Not A Name!")) }

func TestAProposalThatChangesNothingIsCarriedWhoeverMakesIt(t *testing.T) {
	w := newWorld(t)
	same := propose(w.base, w.base.Policy().(fold.Enrolled).Policy.MemberMap(), events.Majority{})
	es := w.mint(c, w.base, root(same), root(events.Approve{Proposal: same.Digest()}))
	s := w.state()
	wantCarried(t, s, es[0], fold.Inadmissible{Why: "it changes nothing"})
	if v := version(t, s); v != 2 {
		t.Errorf("a stranger's empty proposal left version %d in force, want 2", v)
	}
}

func TestAnApprovalOfAProposalAnotherActivationPassedIsCarried(t *testing.T) {
	w := soloWorld(t)
	raise := propose(w.base, w.base.Policy().(fold.Enrolled).Policy.MemberMap(), events.Fixed{N: 1})
	w.mint(a, w.base, root(raise))
	demote := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner}, events.Majority{})
	w.mint(a, w.state(), root(demote), root(events.Approve{Proposal: demote.Digest()}))
	late := w.mint(a, w.state(), root(events.Approve{Proposal: raise.Digest()}))[0]
	s := w.state()
	wantCarried(t, s, late, fold.Inadmissible{Why: "it approves a change to version 2; version 3 is in force"})
	if v := version(t, s); v != 3 {
		t.Errorf("the stale approval left version %d in force, want 3", v)
	}
}

func TestARemovedMembersOldRequestStaysAdmittedAndANewOneIsListed(t *testing.T) {
	w := newWorld(t)
	w.mint(c, w.base, root(events.Request{}))
	in := w.state().Policy().(fold.Enrolled).Policy.MemberMap()
	in[keyC.Principal()] = contributor
	admit := propose(w.state(), in, events.Majority{})
	w.mint(a, w.state(), root(admit), root(events.Approve{Proposal: admit.Digest()}))
	asMember := w.mint(c, w.state(), root(events.Request{}))[0]
	wantCarried(t, w.state(), asMember, fold.Inadmissible{Why: "its writer is a member"})
	delete(in, keyC.Principal())
	remove := propose(w.state(), in, events.Majority{})
	w.mint(a, w.state(), root(remove), root(events.Approve{Proposal: remove.Digest()}))
	if rs := w.state().Requests(); len(rs) != 0 {
		t.Fatalf("after C's removal the requests are %#v; want C's admitted request gone", rs)
	}
	again := w.mint(c, w.state(), root(events.Request{}))[0]
	if rs := w.state().Requests(); len(rs) != 1 || rs[0].Stamp.Event != again.ID() {
		t.Errorf("C's new request lists as %#v", rs)
	}
}

func TestAFoundingEnrollmentThisReleaseCannotReadStillFoundsTheStore(t *testing.T) {
	w := unfounded(t)
	first := w.mint(founder, fold.State{}, root(unreadEnroll{}))[0]
	second := w.mint(b, w.state(), root(events.Enroll{Owner: owner, Roles: roles}))[0]
	s := w.state()
	wantCarried(t, s, second, fold.EnrollmentYielded{To: first.ID()})
	if _, ok := s.Policy().(fold.Unenrolled); !ok {
		t.Errorf("a later enrollment founded the store over one this release cannot read: %#v", s.Policy())
	}
}

func TestAContributorRemovesALabelFromItsOwnEpic(t *testing.T) {
	w := soloWorld(t)
	epic := idOf(w.mint(b, w.base, create(events.AtLevel{Level: level(2)}))[0])
	w.mint(b, w.state(), events.Act{About: events.Item{ID: epic}, Payload: events.MemberAdd{Field: labels, Member: name("urgent")}})
	rm := w.mint(b, w.state(), events.Act{About: events.Item{ID: epic}, Payload: events.MemberRm{Field: labels, Member: name("urgent")}})[0]
	wantApplied(t, w.state(), rm)
}
