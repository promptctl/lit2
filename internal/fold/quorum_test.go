package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// threeOwners is newWorld with C added as a third owner: version 3.
func threeOwners(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	all := propose(w.base, map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner, keyC.Principal(): owner}, events.Majority{})
	w.mint(a, w.base, root(all), root(events.Approve{Proposal: all.Digest()}))
	w.mint(b, w.state(), root(events.Approve{Proposal: all.Digest()}))
	w.mint(c, w.state(), root(events.Approve{Proposal: all.Digest()}))
	w.base = w.state()
	if v := version(t, w.base); v != 3 {
		t.Fatalf("adding C left version %d in force, want 3", v)
	}
	return w
}

func TestOneOwnerOfThreeCannotStripTheOtherTwoAtOnce(t *testing.T) {
	w := threeOwners(t)
	alone := propose(w.base, map[events.PrincipalID]events.Name{keyA.Principal(): owner}, events.Majority{})
	grab := w.mint(a, w.base, root(alone), root(events.Approve{Proposal: alone.Digest()}))[0]
	s := w.state()
	if why, ok := carried(s, grab); !ok {
		t.Fatal("one owner's proposal stripping the other two applied")
	} else if _, inadmissible := why.(fold.Inadmissible); !inadmissible {
		t.Errorf("the proposal stripping two owners is carried for %#v, want Inadmissible", why)
	}
	if v := version(t, s); v != 3 {
		t.Fatalf("one owner of three stripped the other two: version %d is in force", v)
	}
	// one at a time, the owner being removed is not counted, and the
	// quorum is still two of the two owners kept
	one := propose(s, map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyC.Principal(): owner}, events.Majority{})
	w.mint(a, s, root(one), root(events.Approve{Proposal: one.Digest()}))
	if v := version(t, w.state()); v != 3 {
		t.Fatalf("one owner of three removed another alone: version %d is in force", v)
	}
	w.mint(c, w.state(), root(events.Approve{Proposal: one.Digest()}))
	if v := version(t, w.state()); v != 4 {
		t.Errorf("the two owners kept removing the third left version %d in force, want 4", v)
	}
}

func TestAProposalOfAVersionNotInForceIsCarried(t *testing.T) {
	w := soloWorld(t)
	ahead := events.Propose{Base: events.Digest{3}, Members: map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner}, Threshold: events.Majority{}}
	e := w.mint(a, w.base, root(ahead))[0]
	wantCarried(t, w.state(), e, fold.Inadmissible{Why: "it changes a policy other than version 2, the one in force"})
	if ps := w.state().Proposals(); len(ps) != 0 {
		t.Errorf("a proposal of a version not in force is pending: %#v", ps)
	}
}

func TestACommentIsItsAuthorsWhoeverCreatedItsItem(t *testing.T) {
	w := soloWorld(t)
	join := propose(w.base, map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): contributor, keyC.Principal(): contributor}, events.Majority{})
	w.mint(a, w.base, root(join), root(events.Approve{Proposal: join.Digest()}))
	mine := idOf(w.mint(b, w.state(), create(events.AtLevel{Level: level(2)}))[0])
	note := w.mint(c, w.state(), comment(mine, "c's note"))[0]
	at := events.Element{Of: mine, ID: note.Header().About().(events.NewElement).ID}
	edit := func(by writer) events.Event {
		return w.mint(by, w.state(), events.Act{About: at, Payload: events.FieldSet{Field: bodyName, Value: prose(by.checkout.String())}})[0]
	}
	rm := func(by writer) events.Event {
		return w.mint(by, w.state(), events.Act{About: at, Payload: events.FieldSet{Field: fold.Deleted, Value: plain("true")}})[0]
	}
	itemsCreatorEdits, itemsCreatorRemoves := edit(b), rm(b)
	authorEdits, authorRemoves := edit(c), rm(c)
	s := w.state()
	for what, e := range map[string]events.Event{"edited": itemsCreatorEdits, "removed": itemsCreatorRemoves} {
		if why, ok := carried(s, e); !ok {
			t.Errorf("the item's creator %s another's comment", what)
		} else if _, unauthorized := why.(fold.Unauthorized); !unauthorized {
			t.Errorf("the item's creator's write to another's comment is carried for %#v, want Unauthorized", why)
		}
	}
	wantApplied(t, s, authorEdits)
	wantApplied(t, s, authorRemoves)
}

func TestAnEventNoRuleReadsByANonMemberIsCarried(t *testing.T) {
	w := newWorld(t)
	e := w.mint(c, w.base, events.Act{About: events.NewItem{ID: ghost("v"), Width: 3}, Payload: unknown{}})[0]
	s := w.state()
	wantCarried(t, s, e, fold.NotAMemberWrote{})
	if s.Frontier() != w.base.Frontier() {
		t.Error("a non-member's unread event moved the frontier")
	}
}
