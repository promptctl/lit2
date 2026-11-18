package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// TestAnEnrollmentSortingFirstIsNotTheFounding: a writer picks its own
// checkout id, so its enrollment sorts before the founder's; the fold's
// founding, never the order, says which founds the store.
func TestAnEnrollmentSortingFirstIsNotTheFounding(t *testing.T) {
	w := unfounded(t)
	usurper := writer{checkout: store.CheckoutID{}, signer: keyB.Signer()}
	first := w.mint(usurper, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))[0]
	founding := w.found(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))
	if events.Compare(first.Order(), founding.Order()) >= 0 {
		t.Fatal("the usurper's enrollment sorts after the founding; the test needs it first")
	}
	s := w.state()
	wantCarried(t, s, first, fold.NotTheFounding{})
	if owners := s.Policy().(fold.Enrolled).Policy.Owners(); len(owners) != 1 || owners[0] != keyA.Principal() {
		t.Errorf("the owners are %v, want the founder A alone", owners)
	}
}

func TestAFoldNamingNoFoundingFoundsOnNoEnrollment(t *testing.T) {
	w := unfounded(t)
	e := w.mint(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))[0]
	s := w.state()
	wantCarried(t, s, e, fold.NotTheFounding{})
	if _, ok := s.Policy().(fold.Unenrolled); !ok {
		t.Errorf("the policy is %#v, want none", s.Policy())
	}
}

func TestAFoundingTheStoreDoesNotHoldIsAbsent(t *testing.T) {
	w := unfounded(t)
	w.founding = fold.FoundedBy{Enrollment: events.EventID{1}}
	e := w.mint(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))[0]
	s := w.state()
	wantCarried(t, s, e, fold.NotTheFounding{})
	if got, ok := s.Policy().(fold.FoundingAbsent); !ok || got.Enrollment != (events.EventID{1}) {
		t.Errorf("the policy is %#v, want the founding absent", s.Policy())
	}
}
